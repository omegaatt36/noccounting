package webapp

import (
	"context"
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
	now            func() time.Time
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
		now:            time.Now,
	}, nil
}

// nowFor returns the current time in the caller's timezone (or an explicit tz
// parameter), normalized to UTC calendar coordinates so date comparisons match
// date-only ShoppedAt values.
func (h *Handler) nowFor(r *http.Request, caller int64) time.Time {
	var loc *time.Location
	if rawTZ := r.FormValue("tz"); rawTZ != "" {
		if parsed, err := time.LoadLocation(rawTZ); err == nil {
			loc = parsed
		}
	}
	if loc == nil && h.userService != nil {
		loc = h.userService.Timezone(caller)
	}
	if loc == nil {
		loc = user.DefaultTimezone()
	}
	clock := h.now
	if clock == nil {
		clock = time.Now
	}
	local := clock().In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
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
	mux.HandleFunc("GET /api/bootstrap", h.handleBootstrap)
	mux.HandleFunc("POST /api/trip", h.handleSelectTrip)
	mux.HandleFunc("GET /api/rates", h.handleGetRates)
	mux.HandleFunc("POST /api/expense", h.handleCreateExpense)
	mux.HandleFunc("PUT /api/expense", h.handleUpdateExpense)
	mux.HandleFunc("DELETE /api/expense", h.handleDeleteExpense)
	mux.HandleFunc("POST /api/receipt/analyze", h.handleAnalyzeReceipt)
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /ready", h.handleReady)
	mux.HandleFunc("GET /partial/dashboard", h.handleDashboardContent)
	mux.HandleFunc("GET /partial/settlement", h.handleSettlement)
	mux.HandleFunc("GET /partial/expenses", h.handleExpenseList)
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

// readyTimeout bounds the TREK check behind /ready so a hung TREK fails a probe
// instead of stalling it.
const readyTimeout = 5 * time.Second

// handleReady says whether this instance can do its job: /health only says the
// process is up, but with TREK unreachable every expense and dashboard request
// fails. It is a separate route so a TREK outage marks the instance unready
// without making a liveness probe restart it. Nothing about the failure is put
// in the response; it is logged.
func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	if _, err := h.tripService.List(ctx); err != nil {
		slog.Warn("Not ready: TREK could not be reached", "error", err)
		http.Error(w, "trek unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Warn("Failed to write ready response", "error", err)
	}
}

// InitData expiration time for validation.
const initDataMaxAge = 24 * time.Hour
