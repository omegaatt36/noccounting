package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type Handler struct {
	userService    *user.Service
	expenseService *expense.Service
	tripService    *trip.Service
	botToken       string
	devMode        bool
}

func NewHandler(userService *user.Service, expenseService *expense.Service, tripService *trip.Service, botToken string, devMode bool) (*Handler, error) {
	if devMode {
		slog.Warn("Running in dev mode — Telegram auth is disabled")
	}
	return &Handler{
		userService:    userService,
		expenseService: expenseService,
		tripService:    tripService,
		botToken:       botToken,
		devMode:        devMode,
	}, nil
}

// The ways a caller can fail to be identified.
var (
	errMissingInitData = errors.New("missing init_data")
	errInvalidInitData = errors.New("invalid init_data")
	errUnauthorized    = errors.New("unauthorized")
)

// callerID is who is calling, as their Telegram id: init_data is read from the
// query or the form and its signature is checked against the bot token, so the
// id is the one Telegram vouched for. Dev mode skips the check and speaks for
// the first mapped user, which is also who the trip selection is kept for.
func (h *Handler) callerID(r *http.Request) (int64, error) {
	if h.devMode {
		slog.Warn("Dev mode: skipping auth check", "path", r.URL.Path)
		users, err := h.userService.GetAllUsers()
		if err != nil || len(users) == 0 {
			return 0, nil
		}
		return users[0].TelegramID, nil
	}

	initData := r.FormValue("init_data")
	if initData == "" {
		slog.Warn("Missing init_data", "path", r.URL.Path)
		return 0, errMissingInitData
	}

	telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
	if err != nil {
		slog.Warn("Invalid Telegram initData", "error", err, "path", r.URL.Path)
		return 0, errInvalidInitData
	}

	if !h.userService.IsAuthorized(telegramData.UserID) {
		slog.Warn("Unauthorized user", "user_id", telegramData.UserID, "path", r.URL.Path)
		return 0, errUnauthorized
	}

	return telegramData.UserID, nil
}

// requireAuth identifies the caller for a route that answers in plain text. On
// failure it writes the HTTP error and returns false.
func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) (int64, bool) {
	caller, err := h.callerID(r)
	switch {
	case err == nil:
		return caller, true
	case errors.Is(err, errMissingInitData):
		http.Error(w, "missing init_data", http.StatusForbidden)
	case errors.Is(err, errInvalidInitData):
		http.Error(w, "invalid authentication", http.StatusForbidden)
	default:
		http.Error(w, "unauthorized", http.StatusForbidden)
	}
	return 0, false
}

func (h *Handler) tripFor(ctx context.Context, r *http.Request) (domain.Trip, error) {
	raw := r.FormValue("trip_id")
	tripID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return domain.Trip{}, fmt.Errorf("%w: %q", trip.ErrTripNotFound, raw)
	}
	return h.tripService.Get(ctx, tripID)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleIndex)
	mux.HandleFunc("GET /api/auth", h.handleAuth)
	mux.HandleFunc("GET /api/users", h.handleGetUsers)
	mux.HandleFunc("GET /api/trips", h.handleGetTrips)
	mux.HandleFunc("POST /api/trip", h.handleSelectTrip)
	mux.HandleFunc("GET /api/members", h.handleGetMembers)
	mux.HandleFunc("GET /api/rates", h.handleGetRates)
	mux.HandleFunc("POST /api/expense", h.handleCreateExpense)
	mux.HandleFunc("DELETE /api/expense", h.handleDeleteExpense)
	mux.HandleFunc("POST /api/receipt/analyze", h.handleAnalyzeReceipt)
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /partial/form", h.handlePartialForm)
	mux.HandleFunc("GET /partial/dashboard", h.handleDashboardContent)
	mux.HandleFunc("GET /partial/dashboard/category", h.handleCategoryDetail)
	mux.HandleFunc("GET /partial/dashboard/method", h.handleMethodDetail)
	mux.HandleFunc("GET /api/export/csv", h.handleExportCSV)

	sub, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := components.Page(h.devMode).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render index", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Warn("Failed to write health response", "error", err)
	}
}

// InitData expiration time for validation.
const initDataMaxAge = 24 * time.Hour

type AuthResponse struct {
	Authorized bool   `json:"authorized"`
	Nickname   string `json:"nickname,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (h *Handler) handleAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.devMode {
		if err := json.NewEncoder(w).Encode(AuthResponse{
			Authorized: true,
			Nickname:   "dev",
		}); err != nil {
			slog.Warn("Failed to encode auth response", "error", err)
		}
		return
	}

	initData := r.URL.Query().Get("init_data")
	if initData == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "missing init_data",
		}); err != nil {
			slog.Warn("Failed to encode auth response", "error", err)
		}
		return
	}

	telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
	if err != nil {
		slog.Warn("Invalid Telegram initData", "error", err)
		w.WriteHeader(http.StatusForbidden)
		if encErr := json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "invalid authentication",
		}); encErr != nil {
			slog.Warn("Failed to encode auth response", "error", encErr)
		}
		return
	}

	user, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramData.UserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		if encErr := json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "unauthorized user",
		}); encErr != nil {
			slog.Warn("Failed to encode auth response", "error", encErr)
		}
		return
	}
	if err := json.NewEncoder(w).Encode(AuthResponse{
		Authorized: true,
		Nickname:   user.Nickname,
	}); err != nil {
		slog.Warn("Failed to encode auth response", "error", err)
	}
}

type UserInfo struct {
	Nickname   string `json:"nickname"`
	TelegramID int64  `json:"telegram_id"`
}

type UsersResponse struct {
	Users []UserInfo `json:"users"`
}

func (h *Handler) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !h.devMode {
		initData := r.URL.Query().Get("init_data")
		if initData == "" {
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "missing init_data"}); err != nil {
				slog.Warn("Failed to encode error response", "error", err)
			}
			return
		}

		telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
		if err != nil {
			slog.Warn("Invalid Telegram initData", "error", err)
			w.WriteHeader(http.StatusForbidden)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "invalid authentication"}); err != nil {
				slog.Warn("Failed to encode error response", "error", err)
			}
			return
		}

		if !h.userService.IsAuthorized(telegramData.UserID) {
			w.WriteHeader(http.StatusForbidden)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"}); err != nil {
				slog.Warn("Failed to encode error response", "error", err)
			}
			return
		}
	}

	allUsers, err := h.userService.GetAllUsers()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "failed to get users"}); encErr != nil {
			slog.Warn("Failed to encode error response", "error", encErr)
		}
		return
	}

	users := make([]UserInfo, len(allUsers))
	for i, u := range allUsers {
		users[i] = UserInfo{
			Nickname:   u.Nickname,
			TelegramID: u.TelegramID,
		}
	}
	if err := json.NewEncoder(w).Encode(UsersResponse{Users: users}); err != nil {
		slog.Warn("Failed to encode users response", "error", err)
	}
}
