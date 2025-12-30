package webapp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

//go:embed templates/*.html
var templateFS embed.FS

// Handler handles HTTP requests for the Mini App.
type Handler struct {
	userService    *user.Service
	accountingRepo domain.AccountingRepo
	templates      *template.Template
	botToken       string
}

// NewHandler creates a new Handler.
func NewHandler(userService *user.Service, accountingRepo domain.AccountingRepo, botToken string) (*Handler, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Handler{
		userService:    userService,
		accountingRepo: accountingRepo,
		templates:      tmpl,
		botToken:       botToken,
	}, nil
}

// RegisterRoutes registers HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleIndex)
	mux.HandleFunc("GET /api/auth", h.handleAuth)
	mux.HandleFunc("GET /api/users", h.handleGetUsers)
	mux.HandleFunc("POST /api/expense", h.handleCreateExpense)
	mux.HandleFunc("GET /health", h.handleHealth)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "index.html", nil); err != nil {
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

	// Get and validate Telegram initData
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
	Success  bool
	Name     string
	Price    uint64
	Currency string
	Error    string
}

func (h *Handler) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderResult(w, resultData{Error: "無法解析表單"})
		return
	}

	// Validate Telegram initData
	initData := r.FormValue("init_data")
	if initData == "" {
		h.renderResult(w, resultData{Error: "無法取得使用者資訊"})
		return
	}

	telegramData, err := ValidateTelegramInitData(initData, h.botToken, initDataMaxAge)
	if err != nil {
		slog.Warn("Invalid Telegram initData in expense creation", "error", err)
		h.renderResult(w, resultData{Error: "驗證失敗"})
		return
	}

	// Verify the requester is authorized
	if !h.userService.IsAuthorized(telegramData.UserID) {
		h.renderResult(w, resultData{Error: "未授權的使用者"})
		return
	}

	// Parse paid_by (who paid for this expense)
	paidByStr := r.FormValue("paid_by")
	var paidByNotionID string
	if paidByStr != "" {
		paidByTelegramID, err := strconv.ParseInt(paidByStr, 10, 64)
		if err != nil {
			h.renderResult(w, resultData{Error: "付款人 ID 格式錯誤"})
			return
		}
		user, err := h.userService.GetUser(domain.GetUserRequest{
			TelegramID: &paidByTelegramID,
		})
		if err != nil {
			if errors.Is(err, domain.ErrUserNotFound) {
				h.renderResult(w, resultData{Error: "付款人不存在"})
				return
			}
			h.renderResult(w, resultData{Error: "伺服器錯誤"})
			return
		}

		paidByNotionID = user.NotionID
	} else {
		user, err := h.userService.GetUser(domain.GetUserRequest{
			TelegramID: &telegramData.UserID,
		})
		if err != nil {
			h.renderResult(w, resultData{Error: "使用者不存在"})
			return
		}

		paidByNotionID = user.NotionID
	}

	name := r.FormValue("name")
	if name == "" {
		h.renderResult(w, resultData{Error: "請輸入消費名稱"})
		return
	}

	priceStr := r.FormValue("price")
	price, err := strconv.ParseUint(priceStr, 10, 64)
	if err != nil || price == 0 {
		h.renderResult(w, resultData{Error: "請輸入有效金額"})
		return
	}

	currencyStr := r.FormValue("currency")
	currency, err := domain.ParseCurrency(currencyStr)
	if err != nil {
		h.renderResult(w, resultData{Error: "請選擇幣別"})
		return
	}

	// Parse exchange rate (only for JPY)
	var exchangeRate decimal.Decimal
	if currency == domain.CurrencyJPY {
		exRateStr := r.FormValue("exchange_rate")
		if exRateStr != "" {
			exchangeRate, err = decimal.NewFromString(exRateStr)
			if err != nil {
				h.renderResult(w, resultData{Error: "匯率格式錯誤"})
				return
			}
		}
	}

	categoryStr := r.FormValue("category")
	category, err := domain.ParseCategory(categoryStr)
	if err != nil {
		h.renderResult(w, resultData{Error: "請選擇分類"})
		return
	}

	methodStr := r.FormValue("method")
	method, err := domain.ParsePaymentMethod(methodStr)
	if err != nil {
		h.renderResult(w, resultData{Error: "請選擇付款方式"})
		return
	}

	// Parse shopped_at date
	shoppedAt := time.Now()
	shoppedAtStr := r.FormValue("shopped_at")
	if shoppedAtStr != "" {
		parsed, err := time.Parse("2006-01-02", shoppedAtStr)
		if err != nil {
			h.renderResult(w, resultData{Error: "日期格式錯誤"})
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
		h.renderResult(w, resultData{Error: "新增失敗，請稍後再試"})
		return
	}

	h.renderResult(w, resultData{
		Success:  true,
		Name:     name,
		Price:    price,
		Currency: currencyStr,
	})
}

func (h *Handler) renderResult(w http.ResponseWriter, data resultData) {
	if err := h.templates.ExecuteTemplate(w, "result.html", data); err != nil {
		slog.Error("Failed to render result", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
