package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/omegaatt36/noccounting/internal/service/trip"
)

type resultData struct {
	Success     bool
	Title       string
	Description string
	Error       string
}

func (h *Handler) paidByFor(r *http.Request, caller int64) (string, string) {
	var fallback string
	if h.devMode {
		if users, err := h.userService.GetAllUsers(); err == nil && len(users) > 0 {
			fallback = users[0].BackendUserID
		}
	} else {
		u, err := h.userService.GetUser(domain.GetUserRequest{TelegramID: &caller})
		if err != nil {
			return "", "使用者不存在"
		}
		fallback = u.BackendUserID
	}

	paidByStr := r.FormValue("paid_by")
	if paidByStr == "" {
		return fallback, ""
	}

	paidByTelegramID, err := strconv.ParseInt(paidByStr, 10, 64)
	if err != nil {
		if h.devMode {
			return fallback, ""
		}
		return "", "付款人 ID 格式錯誤"
	}
	u, err := h.userService.GetUser(domain.GetUserRequest{TelegramID: &paidByTelegramID})
	if err != nil {
		if h.devMode {
			return fallback, ""
		}
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", "付款人不存在"
		}
		return "", "伺服器錯誤"
	}
	return u.BackendUserID, ""
}

func (h *Handler) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderResult(w, r, resultData{Error: "無法解析表單"})
		return
	}

	caller, err := h.callerID(r)
	switch {
	case err == nil:
	case errors.Is(err, errMissingInitData):
		h.renderResult(w, r, resultData{Error: "無法取得使用者資訊"})
		return
	case errors.Is(err, errInvalidInitData):
		h.renderResult(w, r, resultData{Error: "驗證失敗"})
		return
	default:
		h.renderResult(w, r, resultData{Error: "未授權的使用者"})
		return
	}

	paidBy, failure := h.paidByFor(r, caller)
	if failure != "" {
		h.renderResult(w, r, resultData{Error: failure})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	selected, err := h.tripFor(ctx, r)
	if err != nil {
		if errors.Is(err, trip.ErrNoTrip) || errors.Is(err, trip.ErrTripNotFound) {
			h.renderResult(w, r, resultData{Error: "找不到這趟旅行，請重新整理後再試"})
			return
		}
		slog.Error("Failed to resolve the trip", "error", err)
		h.renderResult(w, r, resultData{Error: "讀取旅行失敗，請稍後再試"})
		return
	}

	name := r.FormValue("name")
	if name == "" {
		h.renderResult(w, r, resultData{Error: "請輸入消費名稱"})
		return
	}

	priceStr := strings.TrimSpace(r.FormValue("price"))
	priceDec, err := decimal.NewFromString(priceStr)
	if err != nil || !priceDec.IsPositive() {
		h.renderResult(w, r, resultData{Error: "請輸入有效金額"})
		return
	}
	priceUint := priceDec.Round(0)
	if !priceUint.IsPositive() || !priceUint.BigInt().IsUint64() {
		h.renderResult(w, r, resultData{Error: "請輸入有效金額"})
		return
	}
	price := priceUint.BigInt().Uint64()

	currency, err := domain.ParseCurrency(r.FormValue("currency"))
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇幣別"})
		return
	}

	// Only a foreign expense carries a rate; the form still posts the one it last
	// showed when the currency is switched back to the trip's own.
	var exchangeRate decimal.Decimal
	if currency != selected.Currency {
		if exRateStr := r.FormValue("exchange_rate"); exRateStr != "" {
			exchangeRate, err = decimal.NewFromString(exRateStr)
			if err != nil {
				h.renderResult(w, r, resultData{Error: "匯率格式錯誤"})
				return
			}
		}
	}

	category, err := domain.ParseCategory(r.FormValue("category"))
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇分類"})
		return
	}

	method, err := domain.ParsePaymentMethod(r.FormValue("method"))
	if err != nil {
		h.renderResult(w, r, resultData{Error: "請選擇付款方式"})
		return
	}

	shoppedAt := time.Now()
	if shoppedAtStr := r.FormValue("shopped_at"); shoppedAtStr != "" {
		parsed, err := time.Parse("2006-01-02", shoppedAtStr)
		if err != nil {
			h.renderResult(w, r, resultData{Error: "日期格式錯誤"})
			return
		}
		shoppedAt = parsed
	}

	expense := &domain.Expense{
		Name:           name,
		Price:          price,
		Currency:       currency,
		ExchangeRate:   exchangeRate,
		Category:       category,
		Method:         method,
		PaidByID:       paidBy,
		ParticipantIDs: participantsOf(r),
		ShoppedAt:      shoppedAt,
	}

	if err := h.expenseService.CreateExpense(ctx, selected, expense); err != nil {
		slog.Error("Failed to create expense", "error", err)
		if errors.Is(err, domain.ErrNotOnTrip) {
			h.renderResult(w, r, resultData{Error: "付款人或分攤的人不在這趟旅行裡"})
			return
		}
		h.renderResult(w, r, resultData{Error: "新增失敗，請稍後再試"})
		return
	}

	description := format.Money(currency, decimal.NewFromUint64(price))
	if currency != selected.Currency && !exchangeRate.IsZero() {
		description += fmt.Sprintf("（≈ %s）", format.Money(selected.Currency, expense.TotalInBase(selected.Currency)))
	}

	h.renderResult(w, r, resultData{
		Success:     true,
		Title:       fmt.Sprintf("%s %s", format.CategoryEmoji(category), name),
		Description: description,
	})
}

func (h *Handler) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	expenseID := r.URL.Query().Get("id")
	if expenseID == "" {
		expenseID = r.FormValue("id")
	}
	if expenseID == "" {
		http.Error(w, "missing expense id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	selected, err := h.tripFor(ctx, r)
	if err != nil {
		slog.Error("Failed to resolve trip for expense deletion", "error", err)
		http.Error(w, "trip not found", http.StatusNotFound)
		return
	}

	if err := h.expenseService.DeleteExpense(ctx, selected, expenseID); err != nil {
		slog.Error("Failed to delete expense", "error", err, "id", expenseID)
		http.Error(w, "failed to delete expense", http.StatusInternalServerError)
		return
	}

	// The row swap only removes the line item; the totals, settlement, and
	// category bars on the dashboard are now stale, so ask for a redraw.
	w.Header().Set("HX-Trigger", "dashboard-refresh")
	w.WriteHeader(http.StatusOK)
}

type analyzeReceiptResponse struct {
	Success  bool                 `json:"success"`
	Summary  string               `json:"summary,omitempty"`
	Currency domain.Currency      `json:"currency,omitempty"`
	Total    uint64               `json:"total,omitempty"`
	Category domain.Category      `json:"category,omitempty"`
	Method   domain.PaymentMethod `json:"method,omitempty"`
	Error    string               `json:"error,omitempty"`
}

func (h *Handler) handleAnalyzeReceipt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, ok := h.requireAuth(w, r); !ok {
		return
	}

	if !h.expenseService.HasReceiptAnalyzer() {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
			Success: false,
			Error:   "收據分析功能尚未啟用",
		})
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
			Success: false,
			Error:   "無法讀取上傳檔案",
		})
		return
	}

	file, _, err := r.FormFile("receipt")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
			Success: false,
			Error:   "缺少收據圖片",
		})
		return
	}
	defer file.Close()

	imgBytes, err := io.ReadAll(file)
	if err != nil || len(imgBytes) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
			Success: false,
			Error:   "讀取收據圖片失敗",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	analysis, err := h.expenseService.AnalyzeReceipt(ctx, imgBytes)
	if err != nil {
		slog.Error("Receipt analysis failed in webapp", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
			Success: false,
			Error:   "收據辨識失敗，請手動輸入",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
		Success:  true,
		Summary:  analysis.Summary,
		Currency: analysis.Currency,
		Total:    analysis.Total,
		Category: analysis.Category,
		Method:   analysis.PaymentMethod,
	})
}

func participantsOf(r *http.Request) []string {
	var participants []string
	for _, id := range r.Form["participants"] {
		if id = strings.TrimSpace(id); id != "" {
			participants = append(participants, id)
		}
	}
	return participants
}

func (h *Handler) renderResult(w http.ResponseWriter, r *http.Request, data resultData) {
	if err := components.Result(data.Success, data.Title, data.Description, data.Error).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render result", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
