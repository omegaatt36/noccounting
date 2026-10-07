package webapp

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

func (h *Handler) handlePartialForm(w http.ResponseWriter, r *http.Request) {
	if err := components.ExpenseForm().Render(r.Context(), w); err != nil {
		slog.Error("Failed to render expense form", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func isWithinRange(t time.Time, from, to *time.Time) bool {
	if from != nil && t.Before(*from) {
		return false
	}
	if to != nil && t.After(*to) {
		return false
	}
	return true
}

// dashboardRequest is what every dashboard route needs to know about its
// request: who is asking, which trip they are looking at, and over which days.
type dashboardRequest struct {
	trip     domain.Trip
	rangeStr string
	from, to *time.Time
	filter   expense.ExpenseFilter
	ctx      context.Context
	cancel   context.CancelFunc
}

// openDashboardRequest identifies the caller and resolves the trip and the date
// range. It writes the HTTP error itself and returns false when it cannot.
func (h *Handler) openDashboardRequest(w http.ResponseWriter, r *http.Request) (*dashboardRequest, bool) {
	_, ok := h.requireAuth(w, r)
	if !ok {
		return nil, false
	}

	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "all"
	}
	from, to := parseDateRange(rangeStr, time.Now())

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	selected, err := h.tripFor(ctx, r)
	if err != nil {
		cancel()
		slog.Error("Failed to resolve the trip", "error", err)
		http.Error(w, "trip not found", http.StatusNotFound)
		return nil, false
	}

	return &dashboardRequest{
		trip:     selected,
		rangeStr: rangeStr,
		from:     from,
		to:       to,
		filter:   expense.ExpenseFilter{DateFrom: from, DateTo: to},
		ctx:      ctx,
		cancel:   cancel,
	}, true
}

func (h *Handler) handleDashboardContent(w http.ResponseWriter, r *http.Request) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	expenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, req.filter)
	if err != nil {
		slog.Error("Failed to query expenses", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	allUsers, err := h.userService.GetAllUsers()
	if err != nil {
		slog.Error("Failed to get users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	currency := req.trip.Currency
	dashboardData := aggregateDashboard(expenses, allUsers, currency)

	// compute previous period totals for TrendPct
	var trendPct int
	if req.rangeStr != "all" {
		prevFrom, prevTo := getPreviousDateRange(req.rangeStr, time.Now())

		// Merge query range to cover both current and previous periods in one call
		var mergedFilter expense.ExpenseFilter
		if prevFrom != nil {
			mergedFilter.DateFrom = prevFrom
		} else {
			mergedFilter.DateFrom = req.from
		}
		if req.to != nil {
			mergedFilter.DateTo = req.to
		} else {
			mergedFilter.DateTo = prevTo
		}

		mergedExpenses, _ := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, mergedFilter)

		var currExpenses, prevExpenses []domain.Expense
		for _, e := range mergedExpenses {
			if isWithinRange(e.ShoppedAt, req.from, req.to) {
				currExpenses = append(currExpenses, e)
			}
			if isWithinRange(e.ShoppedAt, prevFrom, prevTo) {
				prevExpenses = append(prevExpenses, e)
			}
		}

		dashboardData = aggregateDashboard(currExpenses, allUsers, currency)
		prevData := aggregateDashboard(prevExpenses, allUsers, currency)

		currTotal, _ := dashboardData.GrandTotal.Float64()
		prevTotal, _ := prevData.GrandTotal.Float64()

		if prevTotal > 0 {
			trendPct = int(math.Round((currTotal - prevTotal) / prevTotal * 100))
		}
	}

	var grandTotalConverted string
	if currency != domain.CurrencyTWD {
		rate, err := h.expenseService.FetchExchangeRate(req.ctx, currency, domain.CurrencyTWD)
		if err == nil && !rate.IsZero() {
			twdAmount := dashboardData.GrandTotal.Mul(rate).Round(0)
			grandTotalConverted = fmt.Sprintf("≈ %s", format.Money(domain.CurrencyTWD, twdAmount))
		}
	}

	view := components.DashboardView{
		TripTitle:           req.trip.Title,
		GrandTotal:          format.Money(currency, dashboardData.GrandTotal),
		GrandTotalConverted: grandTotalConverted,
		ItemCount:           dashboardData.ItemCount,
		TrendPct:            trendPct,
		DonutGradient:       BuildDonutGradient(dashboardData.ByCategory),
		DateRange:           req.rangeStr,
		Settlement:          h.settlementView(req.ctx, req.trip),
	}

	for _, stat := range dashboardData.ByCategory {
		view.Categories = append(view.Categories, components.CategoryBar{
			Slug:       string(stat.Category),
			Emoji:      stat.Emoji,
			Label:      format.CategoryLabel(stat.Category),
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(stat.Percentage),
		})
	}
	for _, stat := range dashboardData.ByMethod {
		view.Methods = append(view.Methods, components.MethodBar{
			Slug:       string(stat.Method),
			Emoji:      stat.Emoji,
			Label:      stat.Label,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(stat.Percentage),
		})
	}
	for _, stat := range dashboardData.ByDate {
		view.Dates = append(view.Dates, components.DateBar{
			Date:       stat.Date,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(stat.Percentage),
		})
	}
	for _, stat := range dashboardData.ByPayer {
		view.Payers = append(view.Payers, components.PayerBar{
			Name:       stat.Name,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(stat.Percentage),
		})
	}

	if err := components.DashboardContent(view).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render dashboard content", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// settlementView is where the trip's money stands, as TREK works it out. It is
// the part of the dashboard that does not depend on the date range, and a
// failure to read it leaves it out rather than the whole dashboard.
func (h *Handler) settlementView(ctx context.Context, selected domain.Trip) *components.SettlementView {
	settlement, err := h.expenseService.Settlement(ctx, selected)
	if err != nil {
		slog.Warn("Failed to read the settlement for the dashboard", "error", err)
		return nil
	}

	nicknames := h.nicknames()
	nameOf := func(id, fallback string) string {
		if nickname, ok := nicknames[id]; ok {
			return nickname
		}
		return fallback
	}

	view := &components.SettlementView{Unconverted: settlement.Unconverted}
	for _, balance := range settlement.Balances {
		view.Balances = append(view.Balances, components.BalanceRow{
			Name:   nameOf(balance.UserID, balance.Name),
			Amount: signedAmount(settlement.Currency, balance.Amount),
			Owed:   balance.Amount.IsPositive(),
		})
	}
	for _, transfer := range settlement.Transfers {
		view.Transfers = append(view.Transfers, components.TransferRow{
			From:   nameOf(transfer.FromID, transfer.FromName),
			To:     nameOf(transfer.ToID, transfer.ToName),
			Amount: format.Money(settlement.Currency, transfer.Amount),
		})
	}
	return view
}

// signedAmount writes a balance with its sign in front of the whole figure.
func signedAmount(currency domain.Currency, amount decimal.Decimal) string {
	if amount.IsPositive() {
		return "+" + format.Money(currency, amount)
	}
	return format.Money(currency, amount)
}

// handleExportCSV exports expenses as CSV with BOM and Chinese headers
func (h *Handler) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	expenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, req.filter)
	if err != nil {
		slog.Error("Failed to query expenses", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	nicknameMap := h.nicknames()

	// Sort expenses by date
	sort.Slice(expenses, func(i, j int) bool {
		return expenses[i].ShoppedAt.Before(expenses[j].ShoppedAt)
	})

	// Set response headers
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"noccounting-%s.csv\"", req.rangeStr))

	// Write BOM
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		slog.Error("Failed to write BOM", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	// Write header row; the last column is in the trip's own currency
	headerRow := []string{
		"日期",
		"品名",
		"金額",
		"幣別",
		"分類",
		"付款方式",
		"付款人",
		fmt.Sprintf("金額（%s）", req.trip.Currency),
	}
	if err := csvWriter.Write(headerRow); err != nil {
		slog.Error("Failed to write CSV header", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for _, expense := range expenses {
		payer := expense.PaidByID
		if nickname, ok := nicknameMap[expense.PaidByID]; ok {
			payer = nickname
		}

		row := []string{
			expense.ShoppedAt.Format("2006-01-02"),
			expense.Name,
			fmt.Sprintf("%d", expense.Price),
			expense.Currency.String(),
			format.CategoryLabel(expense.Category),
			format.PaymentLabel(expense.Method),
			payer,
			expense.TotalInBase(req.trip.Currency).Round(0).String(),
		}
		if err := csvWriter.Write(row); err != nil {
			slog.Error("Failed to write CSV row", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

func (h *Handler) handleCategoryDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("name")
	h.renderDetail(w, r, components.CategoryColor(slug), func(exp domain.Expense) bool {
		return string(exp.Category) == slug
	}, expense.ExpenseFilter{})
}

func (h *Handler) handleMethodDetail(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")

	var filter expense.ExpenseFilter
	if name != "" {
		method, err := domain.ParsePaymentMethod(name)
		if err != nil {
			http.Error(w, "unknown payment method", http.StatusBadRequest)
			return
		}
		filter.Method = &method
	}

	h.renderDetail(w, r, components.CategoryColor(""), func(exp domain.Expense) bool {
		return string(exp.Method) == name
	}, filter)
}

// renderDetail lists the expenses of the requested range that keep accepts,
// newest first, for the drill-down under a dashboard row.
func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, color string, keep func(domain.Expense) bool, extra expense.ExpenseFilter) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	filter := req.filter
	filter.Method = extra.Method

	expenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, filter)
	if err != nil {
		slog.Error("Failed to query expenses for a dashboard detail", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	currency := req.trip.Currency
	var items []components.ExpenseItem
	for _, exp := range expenses {
		if !keep(exp) {
			continue
		}

		// A foreign expense shows what was paid and what it came to.
		amount := format.Money(currency, exp.TotalInBase(currency))
		if exp.Currency != currency && !exp.ExchangeRate.IsZero() {
			amount = fmt.Sprintf("%s (%s)", format.Money(exp.Currency, exp.PriceDecimal()), amount)
		}

		items = append(items, components.ExpenseItem{
			ID:            exp.ID,
			TripID:        req.trip.ID,
			Name:          exp.Name,
			Date:          exp.ShoppedAt.Format("01/02"),
			AmountDisplay: amount,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Date > items[j].Date
	})

	if err := components.CategoryDetailPartial(items, color).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render a dashboard detail partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
