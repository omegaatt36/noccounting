package webapp

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

func isWithinRange(t time.Time, from, to *time.Time) bool {
	if from != nil && t.Before(*from) {
		return false
	}
	if to != nil && t.After(*to) {
		return false
	}
	return true
}

type dashboardRequest struct {
	trip     domain.Trip
	rangeStr string
	now      time.Time
	from, to *time.Time
	filter   expense.ExpenseFilter
	ctx      context.Context
	cancel   context.CancelFunc
}

func (h *Handler) openDashboardRequest(w http.ResponseWriter, r *http.Request) (*dashboardRequest, bool) {
	caller, ok := h.requireAuth(w, r)
	if !ok {
		return nil, false
	}

	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "all"
	}
	now := h.nowFor(r, caller)
	from, to := parseDateRange(rangeStr, now)

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
		now:      now,
		from:     from,
		to:       to,
		filter:   expense.ExpenseFilter{DateFrom: from, DateTo: to},
		ctx:      ctx,
		cancel:   cancel,
	}, true
}

// payerNames: nicknames win over the names TREK has; an unreadable roster leaves nicknames only.
func (h *Handler) payerNames(ctx context.Context, selected domain.Trip) map[string]string {
	names := map[string]string{}
	members, err := h.expenseService.Members(ctx, selected)
	if err != nil {
		slog.Warn("Failed to read the trip members for the payers' names", "error", err)
	}
	for _, member := range members {
		names[member.ID] = member.Name
	}
	for id, nickname := range h.nicknames() {
		names[id] = nickname
	}
	return names
}

func (h *Handler) handleDashboardContent(w http.ResponseWriter, r *http.Request) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	names := h.payerNames(req.ctx, req.trip)

	currency := req.trip.Currency
	var (
		dashboardData DashboardData
		trendPct      int
	)

	prevFrom, prevTo := getPreviousDateRange(req.rangeStr, req.now)
	if prevFrom == nil && prevTo == nil {
		expenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, req.filter)
		if err != nil {
			slog.Error("Failed to query expenses", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		dashboardData = aggregateDashboard(expenses, names, currency)
	} else {
		// Merge query range to cover both current and previous periods in one call
		mergedFilter := expense.ExpenseFilter{DateFrom: prevFrom, DateTo: req.to}
		if mergedFilter.DateFrom == nil {
			mergedFilter.DateFrom = req.from
		}
		if mergedFilter.DateTo == nil {
			mergedFilter.DateTo = prevTo
		}

		mergedExpenses, err := h.expenseService.QueryExpensesWithFilter(req.ctx, req.trip, mergedFilter)
		if err != nil {
			slog.Error("Failed to query expenses", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		var currExpenses, prevExpenses []domain.Expense
		for _, e := range mergedExpenses {
			if isWithinRange(e.ShoppedAt, req.from, req.to) {
				currExpenses = append(currExpenses, e)
			}
			if isWithinRange(e.ShoppedAt, prevFrom, prevTo) {
				prevExpenses = append(prevExpenses, e)
			}
		}

		dashboardData = aggregateDashboard(currExpenses, names, currency)
		prevData := aggregateDashboard(prevExpenses, names, currency)

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
	}

	for _, stat := range dashboardData.ByCategory {
		view.Categories = append(view.Categories, components.CategoryBar{
			Slug:       string(stat.Category),
			Emoji:      stat.Emoji,
			Label:      format.CategoryLabel(stat.Category),
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(math.Round(stat.Percentage)),
		})
	}
	for _, stat := range dashboardData.ByMethod {
		view.Methods = append(view.Methods, components.MethodBar{
			Slug:       string(stat.Method),
			Emoji:      stat.Emoji,
			Label:      stat.Label,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(math.Round(stat.Percentage)),
		})
	}
	for _, stat := range dashboardData.ByDate {
		view.Dates = append(view.Dates, components.DateBar{
			Date:       stat.Date,
			ISODate:    stat.ISODate,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(math.Round(stat.Percentage)),
		})
	}
	for _, stat := range dashboardData.ByPayer {
		view.Payers = append(view.Payers, components.PayerBar{
			ID:         stat.ID,
			Name:       stat.Name,
			Amount:     format.Money(currency, stat.Amount),
			Percentage: int(math.Round(stat.Percentage)),
		})
	}

	if err := components.DashboardContent(view).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render dashboard content", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleSettlement does not depend on a date range: it is everything owed so far.
func (h *Handler) handleSettlement(w http.ResponseWriter, r *http.Request) {
	req, ok := h.openDashboardRequest(w, r)
	if !ok {
		return
	}
	defer req.cancel()

	settlement, err := h.expenseService.Settlement(req.ctx, req.trip)
	if err != nil {
		slog.Error("Failed to read the settlement", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	nicknames := h.nicknames()
	nameOf := func(id, fallback string) string {
		if nickname, ok := nicknames[id]; ok {
			return nickname
		}
		return fallback
	}

	view := components.SettlementView{TripTitle: req.trip.Title, Unconverted: settlement.Unconverted}
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

	if err := components.SettlementContent(view).Render(r.Context(), w); err != nil {
		slog.Error("Failed to render the settlement", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func signedAmount(currency domain.Currency, amount decimal.Decimal) string {
	if amount.IsPositive() {
		return "+" + format.Money(currency, amount)
	}
	return format.Money(currency, amount)
}

// csvText defuses a cell a spreadsheet would read as a formula (leading = + - @); names can come from a scanned receipt.
func csvText(text string) string {
	if text != "" && strings.ContainsAny(text[:1], "=+-@\t\r") {
		return "'" + text
	}
	return text
}

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

	payerNames := h.payerNames(req.ctx, req.trip)

	sort.Slice(expenses, func(i, j int) bool {
		return expenses[i].ShoppedAt.Before(expenses[j].ShoppedAt)
	})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"noccounting-%s.csv\"", req.rangeStr))

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
		if nickname, ok := payerNames[expense.PaidByID]; ok {
			payer = nickname
		}

		row := []string{
			expense.ShoppedAt.Format("2006-01-02"),
			csvText(expense.Name),
			fmt.Sprintf("%d", expense.Price),
			expense.Currency.String(),
			format.CategoryLabel(expense.Category),
			format.PaymentLabel(expense.Method),
			csvText(payer),
			expense.TotalInBase(req.trip.Currency).Round(0).String(),
		}
		if err := csvWriter.Write(row); err != nil {
			slog.Error("Failed to write CSV row", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}
