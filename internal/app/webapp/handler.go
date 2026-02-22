package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// Handler handles HTTP requests for the Mini App.
type Handler struct {
	userService    *user.Service
	accountingRepo domain.AccountingRepo
	botToken       string
	devMode        bool
}

// NewHandler creates a new Handler.
func NewHandler(userService *user.Service, accountingRepo domain.AccountingRepo, botToken string, devMode bool) (*Handler, error) {
	if devMode {
		slog.Warn("Running in dev mode — Telegram auth is disabled")
	}
	return &Handler{
		userService:    userService,
		accountingRepo: accountingRepo,
		botToken:       botToken,
		devMode:        devMode,
	}, nil
}

// RegisterRoutes registers HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleIndex)
	mux.HandleFunc("GET /api/auth", h.handleAuth)
	mux.HandleFunc("GET /api/users", h.handleGetUsers)
	mux.HandleFunc("POST /api/expense", h.handleCreateExpense)
	mux.HandleFunc("GET /health", h.handleHealth)

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
	w.Write([]byte("ok"))
}

// InitData expiration time for validation.
const initDataMaxAge = 24 * time.Hour

// AuthResponse is the response for the auth endpoint.
type AuthResponse struct {
	Authorized bool   `json:"authorized"`
	Nickname   string `json:"nickname,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (h *Handler) handleAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Dev mode: skip Telegram auth
	if h.devMode {
		json.NewEncoder(w).Encode(AuthResponse{
			Authorized: true,
			Nickname:   "dev",
		})
		return
	}

	// Get and validate Telegram initData
	initData := r.URL.Query().Get("init_data")
	if initData == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "missing init_data",
		})
		return
	}

	// Validate the initData signature
	telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
	if err != nil {
		slog.Warn("Invalid Telegram initData", "error", err)
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "invalid authentication",
		})
		return
	}

	user, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramData.UserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(AuthResponse{
			Authorized: false,
			Error:      "unauthorized user",
		})
		return
	}

	json.NewEncoder(w).Encode(AuthResponse{
		Authorized: true,
		Nickname:   user.Nickname,
	})
}

// UserInfo is a simplified user info for the frontend.
type UserInfo struct {
	Nickname   string `json:"nickname"`
	TelegramID int64  `json:"telegram_id"`
}

// UsersResponse is the response for the users endpoint.
type UsersResponse struct {
	Users []UserInfo `json:"users"`
}

func (h *Handler) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Dev mode: skip auth
	if !h.devMode {
		initData := r.URL.Query().Get("init_data")
		if initData == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing init_data"})
			return
		}

		telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
		if err != nil {
			slog.Warn("Invalid Telegram initData", "error", err)
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid authentication"})
			return
		}

		if !h.userService.IsAuthorized(telegramData.UserID) {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
	}

	allUsers, err := h.userService.GetAllUsers()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get users"})
		return
	}

	users := make([]UserInfo, len(allUsers))
	for i, u := range allUsers {
		users[i] = UserInfo{
			Nickname:   u.Nickname,
			TelegramID: u.TelegramID,
		}
	}

	json.NewEncoder(w).Encode(UsersResponse{Users: users})
}

type resultData struct {
	Success       bool
	Name          string
	Price         uint64
	Currency      string
	CategoryEmoji string
	TWDAmount     string
	Error         string
}

func (h *Handler) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderResult(w, r, resultData{Error: "無法解析表單"})
		return
	}

	// Validate Telegram initData (skip in dev mode)
	var paidByNotionID string
	if h.devMode {
		// In dev mode, use first available user
		allUsers, err := h.userService.GetAllUsers()
		if err != nil || len(allUsers) == 0 {
			paidByNotionID = ""
		} else {
			paidByNotionID = allUsers[0].NotionID
		}
		// Allow override from form
		if paidByStr := r.FormValue("paid_by"); paidByStr != "" {
			paidByTelegramID, err := strconv.ParseInt(paidByStr, 10, 64)
			if err == nil {
				if user, err := h.userService.GetUser(domain.GetUserRequest{TelegramID: &paidByTelegramID}); err == nil {
					paidByNotionID = user.NotionID
				}
			}
		}
	} else {
		initData := r.FormValue("init_data")
		if initData == "" {
			h.renderResult(w, r, resultData{Error: "無法取得使用者資訊"})
			return
		}

		telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
		if err != nil {
			slog.Warn("Invalid Telegram initData in expense creation", "error", err)
			h.renderResult(w, r, resultData{Error: "驗證失敗"})
			return
		}

		if !h.userService.IsAuthorized(telegramData.UserID) {
			h.renderResult(w, r, resultData{Error: "未授權的使用者"})
			return
		}

		paidByStr := r.FormValue("paid_by")
		if paidByStr != "" {
			paidByTelegramID, err := strconv.ParseInt(paidByStr, 10, 64)
			if err != nil {
				h.renderResult(w, r, resultData{Error: "付款人 ID 格式錯誤"})
				return
			}
			user, err := h.userService.GetUser(domain.GetUserRequest{
				TelegramID: &paidByTelegramID,
			})
			if err != nil {
				if errors.Is(err, domain.ErrUserNotFound) {
					h.renderResult(w, r, resultData{Error: "付款人不存在"})
					return
				}
				h.renderResult(w, r, resultData{Error: "伺服器錯誤"})
				return
			}
			paidByNotionID = user.NotionID
		} else {
			user, err := h.userService.GetUser(domain.GetUserRequest{
				TelegramID: &telegramData.UserID,
			})
			if err != nil {
				h.renderResult(w, r, resultData{Error: "使用者不存在"})
				return
			}
			paidByNotionID = user.NotionID
		}
	}

	name := r.FormValue("name")
	if name == "" {
		h.renderResult(w, r, resultData{Error: "請輸入消費名稱"})
		return
	}

	priceStr := r.FormValue("price")
	price, err := strconv.ParseUint(priceStr, 10, 64)
	if err != nil || price == 0 {
		h.renderResult(w, r, resultData{Error: "請輸入有效金額"})
		return
	}

	currencyStr := r.FormValue("currency")
	currency, err := domain.ParseCurrency(currencyStr)
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇幣別"})
		return
	}

	// Parse exchange rate (only for JPY)
	var exchangeRate decimal.Decimal
	if currency == domain.CurrencyJPY {
		exRateStr := r.FormValue("exchange_rate")
		if exRateStr != "" {
			exchangeRate, err = decimal.NewFromString(exRateStr)
			if err != nil {
				h.renderResult(w, r, resultData{Error: "匯率格式錯誤"})
				return
			}
		}
	}

	categoryStr := r.FormValue("category")
	category, err := domain.ParseCategory(categoryStr)
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇分類"})
		return
	}

	methodStr := r.FormValue("method")
	method, err := domain.ParsePaymentMethod(methodStr)
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇付款方式"})
		return
	}

	// Parse shopped_at date
	shoppedAt := time.Now()
	shoppedAtStr := r.FormValue("shopped_at")
	if shoppedAtStr != "" {
		parsed, err := time.Parse("2006-01-02", shoppedAtStr)
		if err != nil {
			h.renderResult(w, r, resultData{Error: "日期格式錯誤"})
			return
		}
		shoppedAt = parsed
	}

	// Create expense
	expense := &domain.Expense{
		Name:         name,
		Price:        price,
		Currency:     currency,
		ExchangeRate: exchangeRate,
		Category:     category,
		Method:       method,
		PaidByID:     paidByNotionID,
		ShoppedAt:    shoppedAt,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := h.accountingRepo.CreateExpense(ctx, expense); err != nil {
		slog.Error("Failed to create expense", "error", err)
		h.renderResult(w, r, resultData{Error: "新增失敗，請稍後再試"})
		return
	}

	// Compute TWD amount for display
	var twdAmount string
	if currency == domain.CurrencyJPY && !exchangeRate.IsZero() {
		twdDisplay := decimal.NewFromUint64(price).Mul(exchangeRate)
		twdAmount = twdDisplay.Round(0).String()
	}

	h.renderResult(w, r, resultData{
		Success:       true,
		Name:          name,
		Price:         price,
		Currency:      currencyStr,
		CategoryEmoji: category.Emoji(),
		TWDAmount:     twdAmount,
	})
}

func (h *Handler) renderResult(w http.ResponseWriter, r *http.Request, data resultData) {
	if err := components.Result(data.Success, data.Name, data.Price, data.Currency, data.CategoryEmoji, data.TWDAmount, data.Error).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render result", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

