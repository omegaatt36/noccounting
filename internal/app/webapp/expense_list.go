package webapp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
)

// expenseEdit is an expense as the form is filled from it when a row of the
// list is picked. It travels in the row, so editing needs no second request.
type expenseEdit struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Price        uint64 `json:"price"`
	Currency     string `json:"currency"`
	ExchangeRate string `json:"exchange_rate"`
	Category     string `json:"category"`
	Method       string `json:"method"`
	// PaidBy is the Telegram id the payer picker knows the payer by, empty for a
	// payer outside the user mapping, whom the form then leaves as they are.
	PaidBy       string   `json:"paid_by"`
	Participants []string `json:"participants"`
	ShoppedAt    string   `json:"shopped_at"`
}

var weekdays = [...]string{"日", "一", "二", "三", "四", "五", "六"}

// handleExpenseList lists the expenses of the trip that match the filters of
// the list tab, a day at a time, newest first.
func (h *Handler) handleExpenseList(w http.ResponseWriter, r *http.Request) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	query := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(query.Get("q")))
	categoryName, methodName := query.Get("category"), query.Get("method")
	paidByID := strings.TrimSpace(query.Get("paid_by"))
	dateStr := strings.TrimSpace(query.Get("date"))

	filter := req.filter
	if methodName != "" {
		method, err := domain.ParsePaymentMethod(methodName)
		if err != nil {
			http.Error(w, "unknown payment method", http.StatusBadRequest)
			return
		}
		filter.Method = &method
	}
	if categoryName != "" {
		if _, err := domain.ParseCategory(categoryName); err != nil {
			http.Error(w, "unknown category", http.StatusBadRequest)
			return
		}
	}
	if paidByID != "" {
		filter.PaidByID = &paidByID
	}
	if dateStr != "" {
		day, err := time.Parse(time.DateOnly, dateStr)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		endOfDay := day.Add(24*time.Hour - time.Nanosecond)
		filter.DateFrom = &day
		filter.DateTo = &endOfDay
	}

	expenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, filter)
	if err != nil {
		slog.Error("Failed to query expenses for the list", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// A list the filters emptied is told apart from a trip nobody has spent on yet.
	filtered := req.rangeStr != "all" || search != "" || categoryName != "" || methodName != "" || paidByID != "" || dateStr != ""
	var kept []domain.Expense
	for _, exp := range expenses {
		if categoryName != "" && string(exp.Category) != categoryName {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(exp.Name), search) {
			continue
		}
		kept = append(kept, exp)
	}
	slices.SortStableFunc(kept, func(a, b domain.Expense) int {
		return b.ShoppedAt.Compare(a.ShoppedAt)
	})

	view := h.expenseListView(kept, req.trip.Currency)
	view.Filtered = filtered

	if err := components.ExpenseList(view).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render the expense list", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// expenseListView groups expenses, which are already in the order to show them
// in, by the day they were made.
func (h *Handler) expenseListView(expenses []domain.Expense, base domain.Currency) components.ExpenseListView {
	users, err := h.userService.GetAllUsers()
	if err != nil {
		slog.Warn("Failed to read the users for the expense list", "error", err)
	}
	nicknames := map[string]string{}
	telegramIDs := map[string]string{}
	for _, u := range users {
		nicknames[u.BackendUserID] = u.Nickname
		telegramIDs[u.BackendUserID] = fmt.Sprint(u.TelegramID)
	}

	view := components.ExpenseListView{Count: len(expenses)}
	var (
		day   *components.ExpenseDay
		total decimal.Decimal
		last  time.Time
	)
	closeDay := func() {
		if day != nil {
			day.Total = format.Money(base, total)
			view.Days = append(view.Days, *day)
		}
	}
	for _, exp := range expenses {
		if day == nil || !sameDay(last, exp.ShoppedAt) {
			closeDay()
			day = &components.ExpenseDay{Label: dayLabel(exp.ShoppedAt)}
			total = decimal.Zero
			last = exp.ShoppedAt
		}
		total = total.Add(exp.TotalInBase(base))

		// A foreign expense shows what was paid and what it came to.
		amount := format.Money(base, exp.TotalInBase(base))
		rate := ""
		if exp.Currency != base && !exp.ExchangeRate.IsZero() {
			amount = fmt.Sprintf("%s (%s)", format.Money(exp.Currency, exp.PriceDecimal()), amount)
			rate = exp.ExchangeRate.String()
		}

		meta := []string{}
		if payer := nicknames[exp.PaidByID]; payer != "" {
			meta = append(meta, payer)
		}
		if exp.Method != "" {
			meta = append(meta, format.PaymentLabel(exp.Method))
		}

		edit, err := json.Marshal(expenseEdit{
			ID:           exp.ID,
			Name:         exp.Name,
			Price:        exp.Price,
			Currency:     exp.Currency.String(),
			ExchangeRate: rate,
			Category:     string(exp.Category),
			Method:       string(exp.Method),
			PaidBy:       telegramIDs[exp.PaidByID],
			Participants: exp.ParticipantIDs,
			ShoppedAt:    exp.ShoppedAt.Format(time.DateOnly),
		})
		if err != nil {
			slog.Error("Failed to encode an expense for editing", "error", err, "id", exp.ID)
			continue
		}

		day.Rows = append(day.Rows, components.ExpenseRow{
			Emoji:  format.CategoryEmoji(exp.Category),
			Name:   exp.Name,
			Meta:   strings.Join(meta, " · "),
			Amount: amount,
			Edit:   string(edit),
		})
	}
	closeDay()
	return view
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func dayLabel(t time.Time) string {
	return fmt.Sprintf("%s（週%s）", t.Format("01/02"), weekdays[t.Weekday()])
}
