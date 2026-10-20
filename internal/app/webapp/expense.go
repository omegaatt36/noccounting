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

// resultData is the toast an expense form gets back: what was done when it
// worked, or what failed, and why, when it did not.
type resultData struct {
	Success     bool
	Title       string
	Description string
}

// errExpenseFields is how a form field that cannot be read is told to the
// person: the message is theirs to act on, so it says what to fix.
type errExpenseFields string

func (e errExpenseFields) Error() string { return string(e) }

// expenseFields is what the expense form describes, read and checked.
type expenseFields struct {
	name         string
	price        uint64
	currency     domain.Currency
	exchangeRate decimal.Decimal
	category     domain.Category
	method       domain.PaymentMethod
	participants []string
	// shoppedAt is zero when the form names no day.
	shoppedAt time.Time
	// receiptToken names a scanned receipt's photo, which is uploaded when the
	// expense is saved.
	receiptToken string
	receiptItems []domain.ReceiptItem
}

type receiptItemWire struct {
	Name     string          `json:"name"`
	NameZH   string          `json:"name_zh,omitempty"`
	Price    int64           `json:"price"`
	Category domain.Category `json:"category,omitempty"`
}

// parseExpenseFields reads the expense form. The failure it returns is meant
// for the person.
func parseExpenseFields(r *http.Request, base domain.Currency) (expenseFields, error) {
	var fields expenseFields

	fields.name = strings.TrimSpace(r.PostFormValue("name"))
	if fields.name == "" {
		return fields, errExpenseFields("請輸入消費名稱")
	}

	priceDec, err := decimal.NewFromString(strings.TrimSpace(r.PostFormValue("price")))
	if err != nil || !priceDec.IsPositive() {
		return fields, errExpenseFields("請輸入有效金額")
	}
	priceUint := priceDec.Round(0)
	if !priceUint.IsPositive() || !priceUint.BigInt().IsUint64() {
		return fields, errExpenseFields("請輸入有效金額")
	}
	fields.price = priceUint.BigInt().Uint64()

	fields.currency, err = domain.ParseCurrency(r.PostFormValue("currency"))
	if err != nil {
		return fields, errExpenseFields("請選擇幣別")
	}

	// Only a foreign expense carries a rate; the form still posts the one it last
	// showed when the currency is switched back to the trip's own.
	if fields.currency != base {
		if raw := r.PostFormValue("exchange_rate"); raw != "" {
			fields.exchangeRate, err = decimal.NewFromString(raw)
			if err != nil {
				return fields, errExpenseFields("匯率格式錯誤")
			}
		}
	}

	fields.category, err = domain.ParseCategory(r.PostFormValue("category"))
	if err != nil {
		return fields, errExpenseFields("請選擇分類")
	}

	fields.method, err = domain.ParsePaymentMethod(r.PostFormValue("method"))
	if err != nil {
		return fields, errExpenseFields("請選擇付款方式")
	}

	if raw := r.PostFormValue("shopped_at"); raw != "" {
		fields.shoppedAt, err = time.Parse(time.DateOnly, raw)
		if err != nil {
			return fields, errExpenseFields("日期格式錯誤")
		}
	}

	fields.participants = participantsOf(r)
	fields.receiptToken = strings.TrimSpace(r.PostFormValue("receipt_token"))
	if rawItems := strings.TrimSpace(r.PostFormValue("receipt_items")); rawItems != "" {
		var wire []receiptItemWire
		if err := json.Unmarshal([]byte(rawItems), &wire); err == nil {
			for _, item := range wire {
				fields.receiptItems = append(fields.receiptItems, domain.ReceiptItem{
					Name:     item.Name,
					NameZH:   item.NameZH,
					Price:    item.Price,
					Category: item.Category,
				})
			}
		}
	}
	return fields, nil
}

// paidByFor is the backend user an expense is paid by: the person the form
// names, or the caller when it names none. The second result is the failure to
// tell the person.
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

	paidByStr := r.PostFormValue("paid_by")
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

// openExpenseRequest is what filing and editing an expense both start with:
// the form read, the caller identified and the trip it is for resolved. It
// renders the failure itself, under title, and returns false when there is one.
func (h *Handler) openExpenseRequest(w http.ResponseWriter, r *http.Request, title string) (caller int64, selected domain.Trip, ok bool) {
	fail := func(message string) {
		h.renderResult(w, r, resultData{Title: title, Description: message})
	}

	if err := r.ParseForm(); err != nil {
		fail("無法解析表單")
		return 0, domain.Trip{}, false
	}

	caller, err := h.callerID(r)
	switch {
	case err == nil:
	case errors.Is(err, errMissingInitData):
		fail("無法取得使用者資訊")
		return 0, domain.Trip{}, false
	case errors.Is(err, errInvalidInitData):
		fail("驗證失敗")
		return 0, domain.Trip{}, false
	default:
		fail("未授權的使用者")
		return 0, domain.Trip{}, false
	}

	selected, err = h.tripFor(r.Context(), r)
	if err != nil {
		if errors.Is(err, trip.ErrNoTrip) || errors.Is(err, trip.ErrTripNotFound) {
			fail("找不到這趟旅行，請重新整理後再試")
		} else {
			slog.Error("Failed to resolve the trip", "error", err)
			fail("讀取旅行失敗，請稍後再試")
		}
		return 0, domain.Trip{}, false
	}
	return caller, selected, true
}

func (h *Handler) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	const failed = "新增失敗"
	fail := func(message string) { h.renderResult(w, r, resultData{Title: failed, Description: message}) }

	caller, selected, ok := h.openExpenseRequest(w, r, failed)
	if !ok {
		return
	}

	paidBy, message := h.paidByFor(r, caller)
	if message != "" {
		fail(message)
		return
	}

	fields, err := parseExpenseFields(r, selected.Currency)
	if err != nil {
		fail(err.Error())
		return
	}
	if fields.shoppedAt.IsZero() {
		now := h.nowFor(r, caller)
		fields.shoppedAt = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}

	// The same form sent again, because its answer was lost, files nothing new.
	requestID := strings.TrimSpace(r.PostFormValue("request_id"))
	if len(requestID) > maxRequestIDLen {
		fail("無法解析表單")
		return
	}
	if prev, first := h.submissions.begin(requestID); !first {
		if !prev.done {
			fail("上一次送出還在處理中，請稍後到明細確認")
			return
		}
		prev.result.Description += "（先前已記錄，重複送出已略過）"
		h.renderResult(w, r, prev.result)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	expense := &domain.Expense{
		Name:           fields.name,
		Price:          fields.price,
		Currency:       fields.currency,
		ExchangeRate:   fields.exchangeRate,
		Category:       fields.category,
		Method:         fields.method,
		PaidByID:       paidBy,
		ParticipantIDs: fields.participants,
		ShoppedAt:      fields.shoppedAt,
		ReceiptItems:   fields.receiptItems,
	}
	receiptNote := h.attachReceipt(ctx, selected, fields.receiptToken, expense)

	if err := h.expenseService.CreateExpense(ctx, selected, expense); err != nil {
		h.submissions.abandon(requestID)
		slog.Error("Failed to create expense", "error", err)
		if errors.Is(err, domain.ErrNotOnTrip) {
			fail("付款人或分攤的人不在這趟旅行裡")
			return
		}
		fail("新增失敗，請稍後再試")
		return
	}
	h.receipts.discard(fields.receiptToken)

	result := resultData{
		Success:     true,
		Title:       fmt.Sprintf("%s %s", format.CategoryEmoji(fields.category), fields.name),
		Description: amountSummary(selected.Currency, expense) + receiptNote,
	}
	if requestID != "" {
		h.submissions.finish(requestID, result)
	}
	h.renderResult(w, r, result)
}

// maxRequestIDLen bounds the request id a form may send; the page sends a UUID.
const maxRequestIDLen = 64

// attachReceipt uploads the photo a scanned receipt left in the stash and links
// it to the expense. It says, in words to show the person, when it could not.
func (h *Handler) attachReceipt(ctx context.Context, trip domain.Trip, token string, expense *domain.Expense) string {
	if token == "" {
		return ""
	}

	image, fileID, found := h.receipts.get(token)
	if !found {
		return "\n⚠️ 收據圖片已過期，沒有附上"
	}
	if fileID == "" {
		var err error
		if fileID, err = h.expenseService.UploadReceiptImage(ctx, trip, image); err != nil {
			slog.Warn("Failed to upload the receipt image; saving the expense without it", "error", err)
			return "\n⚠️ 收據圖片上傳失敗，沒有附上"
		}
		h.receipts.setFile(token, fileID)
	}
	expense.ReceiptURL = fileID
	return ""
}

// handleUpdateExpense rewrites an expense with what the form says. It starts
// from the expense as it is stored, so what the form has no field for (a
// receipt and its lines) is kept; a form that names no payer or day keeps those
// too.
func (h *Handler) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	const failed = "儲存失敗"
	fail := func(message string) { h.renderResult(w, r, resultData{Title: failed, Description: message}) }

	caller, selected, ok := h.openExpenseRequest(w, r, failed)
	if !ok {
		return
	}

	fields, err := parseExpenseFields(r, selected.Currency)
	if err != nil {
		fail(err.Error())
		return
	}

	paidBy := ""
	if r.PostFormValue("paid_by") != "" {
		var message string
		if paidBy, message = h.paidByFor(r, caller); message != "" {
			fail(message)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	expense, err := h.expenseService.GetExpense(ctx, selected, r.PostFormValue("expense_id"))
	if err != nil {
		if errors.Is(err, domain.ErrExpenseNotFound) {
			fail("找不到這筆消費，可能已經被刪除")
			return
		}
		slog.Error("Failed to read the expense to update", "error", err)
		fail("讀取消費失敗，請稍後再試")
		return
	}

	expense.Name = fields.name
	expense.Price = fields.price
	expense.Currency = fields.currency
	expense.ExchangeRate = fields.exchangeRate
	expense.Category = fields.category
	expense.Method = fields.method
	expense.ParticipantIDs = fields.participants
	if paidBy != "" {
		expense.PaidByID = paidBy
	}
	if !fields.shoppedAt.IsZero() {
		expense.ShoppedAt = fields.shoppedAt
	}

	if err := h.expenseService.UpdateExpense(ctx, selected, expense); err != nil {
		slog.Error("Failed to update expense", "error", err)
		switch {
		case errors.Is(err, domain.ErrNotOnTrip):
			fail("付款人或分攤的人不在這趟旅行裡")
		case errors.Is(err, domain.ErrUnsupportedSplit):
			fail("這筆消費使用自訂分攤，請到 TREK 修改")
		default:
			fail("儲存失敗，請稍後再試")
		}
		return
	}

	h.renderResult(w, r, resultData{
		Success:     true,
		Title:       "已更新 " + fields.name,
		Description: amountSummary(selected.Currency, expense),
	})
}

// amountSummary is what an expense came to: what was paid and, for a foreign
// one with a rate, what that is in the trip's currency.
func amountSummary(base domain.Currency, e *domain.Expense) string {
	summary := format.Money(e.Currency, e.PriceDecimal())
	if e.Currency != base && !e.ExchangeRate.IsZero() {
		summary += fmt.Sprintf("（≈ %s）", format.Money(base, e.TotalInBase(base)))
	}
	return summary
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

	w.WriteHeader(http.StatusNoContent)
}

type analyzeReceiptResponse struct {
	Success      bool                 `json:"success"`
	Summary      string               `json:"summary,omitempty"`
	Currency     domain.Currency      `json:"currency,omitempty"`
	Total        uint64               `json:"total,omitempty"`
	Category     domain.Category      `json:"category,omitempty"`
	Method       domain.PaymentMethod `json:"method,omitempty"`
	ReceiptToken string               `json:"receipt_token,omitempty"`
	ReceiptItems []receiptItemWire    `json:"receipt_items,omitempty"`
	Error        string               `json:"error,omitempty"`
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

	var wireItems []receiptItemWire
	for _, item := range analysis.Items {
		wireItems = append(wireItems, receiptItemWire{
			Name:     format.ReceiptName(item),
			Price:    item.Price,
			Category: item.Category,
		})
	}

	_ = json.NewEncoder(w).Encode(analyzeReceiptResponse{
		Success:      true,
		Summary:      analysis.Summary,
		Currency:     analysis.Currency,
		Total:        analysis.Total,
		Category:     analysis.Category,
		Method:       analysis.PaymentMethod,
		ReceiptToken: h.receipts.put(imgBytes),
		ReceiptItems: wireItems,
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
	if err := components.Result(data.Success, data.Title, data.Description).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render result", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
