package webapp

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
)

type DashboardData struct {
	// Currency is the trip's currency, which every amount below is in.
	Currency   domain.Currency
	GrandTotal decimal.Decimal
	ItemCount  int
	ByCategory []CategoryStat
	ByMethod   []MethodStat
	ByDate     []DailyStat
	ByPayer    []PayerStat
	DateRange  string
}

type CategoryStat struct {
	Category   domain.Category
	Emoji      string
	Amount     decimal.Decimal
	Percentage float64
}

// MethodStat is the spending paid one way. Method is empty for an expense that
// says nothing about how it was paid, such as one entered in TREK's own UI.
type MethodStat struct {
	Method     domain.PaymentMethod
	Emoji      string
	Label      string
	Amount     decimal.Decimal
	Percentage float64
}

type DailyStat struct {
	Date       string // format: "M/D" e.g. "2/21"
	ISODate    string // format: "YYYY-MM-DD" e.g. "2026-02-21"
	Amount     decimal.Decimal
	Percentage float64
}

type PayerStat struct {
	ID         string
	Name       string
	Amount     decimal.Decimal
	Percentage float64
}

func percentage(amount, total decimal.Decimal) float64 {
	if total.IsZero() {
		return 0
	}
	share, _ := amount.Div(total).Mul(decimal.NewFromInt(100)).Float64()
	return share
}

func aggregateDashboard(expenses []domain.Expense, users []domain.User, base domain.Currency) DashboardData {
	result := DashboardData{
		Currency:   base,
		GrandTotal: decimal.NewFromInt(0),
		ItemCount:  len(expenses),
		ByCategory: []CategoryStat{},
		ByMethod:   []MethodStat{},
		ByDate:     []DailyStat{},
		ByPayer:    []PayerStat{},
	}

	if len(expenses) == 0 {
		return result
	}

	// Build backendUserID to nickname map
	nicknameMap := make(map[string]string)
	for i := range users {
		nicknameMap[users[i].BackendUserID] = users[i].Nickname
	}

	categoryMap := make(map[domain.Category]decimal.Decimal)
	methodMap := make(map[domain.PaymentMethod]decimal.Decimal)
	dateMap := make(map[time.Time]decimal.Decimal)
	payerMap := make(map[string]decimal.Decimal)
	payerIDMap := make(map[string]string)

	for _, expense := range expenses {
		amount := expense.TotalInBase(base)
		result.GrandTotal = result.GrandTotal.Add(amount)

		categoryMap[expense.Category] = categoryMap[expense.Category].Add(amount)
		methodMap[expense.Method] = methodMap[expense.Method].Add(amount)

		// Date aggregation by full calendar day so cross-year trips sort chronologically.
		day := time.Date(expense.ShoppedAt.Year(), expense.ShoppedAt.Month(), expense.ShoppedAt.Day(), 0, 0, 0, 0, time.UTC)
		dateMap[day] = dateMap[day].Add(amount)

		// Payer aggregation (use nickname if available, otherwise the backend user id)
		nickname, ok := nicknameMap[expense.PaidByID]
		if !ok {
			nickname = expense.PaidByID
		}
		payerMap[nickname] = payerMap[nickname].Add(amount)
		if expense.PaidByID != "" {
			payerIDMap[nickname] = expense.PaidByID
		}
	}

	// ByCategory follows the domain's own category order, the same order /today
	// renders, so the donut and the legend cannot disagree with it.
	for _, category := range domain.CategoryValues() {
		amount := categoryMap[category]
		if amount.IsZero() {
			continue
		}
		result.ByCategory = append(result.ByCategory, CategoryStat{
			Category:   category,
			Emoji:      format.CategoryEmoji(category),
			Amount:     amount,
			Percentage: percentage(amount, result.GrandTotal),
		})
	}

	// ByMethod follows the domain's order too, with what no method was recorded
	// for last.
	for _, method := range domain.PaymentMethodValues() {
		if amount := methodMap[method]; !amount.IsZero() {
			result.ByMethod = append(result.ByMethod, MethodStat{
				Method:     method,
				Emoji:      format.PaymentEmoji(method),
				Label:      format.PaymentLabel(method),
				Amount:     amount,
				Percentage: percentage(amount, result.GrandTotal),
			})
		}
	}
	if amount := methodMap[""]; !amount.IsZero() {
		result.ByMethod = append(result.ByMethod, MethodStat{
			Emoji:      "❔",
			Label:      "未標示",
			Amount:     amount,
			Percentage: percentage(amount, result.GrandTotal),
		})
	}

	// Build ByDate, sorted chronologically
	type dateEntry struct {
		day    time.Time
		amount decimal.Decimal
	}
	var dateEntries []dateEntry

	for day, amount := range dateMap {
		dateEntries = append(dateEntries, dateEntry{
			day:    day,
			amount: amount,
		})
	}

	sort.Slice(dateEntries, func(i, j int) bool {
		return dateEntries[i].day.Before(dateEntries[j].day)
	})

	for _, entry := range dateEntries {
		result.ByDate = append(result.ByDate, DailyStat{
			Date:       formatDate(entry.day),
			ISODate:    entry.day.Format(time.DateOnly),
			Amount:     entry.amount,
			Percentage: percentage(entry.amount, result.GrandTotal),
		})
	}

	// Build ByPayer, sorted by amount descending
	for payer, amount := range payerMap {
		result.ByPayer = append(result.ByPayer, PayerStat{
			ID:         payerIDMap[payer],
			Name:       payer,
			Amount:     amount,
			Percentage: percentage(amount, result.GrandTotal),
		})
	}

	// Payers come out of a map, so equal amounts need a tie-break or the order
	// changes from one load to the next.
	slices.SortFunc(result.ByPayer, func(a, b PayerStat) int {
		return cmp.Or(b.Amount.Cmp(a.Amount), cmp.Compare(a.Name, b.Name))
	})

	return result
}

// formatDate formats time.Time to "M/D" string (e.g., "2/21")
func formatDate(t time.Time) string {
	return t.Format("1/2")
}

func BuildDonutGradient(categories []CategoryStat) string {
	var segments []string
	cum := 0.0
	for _, cat := range categories {
		color := components.CategoryColor(string(cat.Category))
		end := cum + cat.Percentage
		segments = append(segments,
			fmt.Sprintf("%s %.1f%% %.1f%%", color, cum, end))
		cum = end
	}

	if len(segments) == 0 {
		return "conic-gradient(var(--border) 0% 100%)"
	}

	return "conic-gradient(" + strings.Join(segments, ", ") + ")"
}

// rangeDays is how many days a range covers, today included. Anything else,
// "all" included, covers no particular days.
func rangeDays(rangeStr string) (int, bool) {
	switch rangeStr {
	case "today":
		return 1, true
	case "3d":
		return 3, true
	case "7d":
		return 7, true
	}
	return 0, false
}

// dayWindow is the days days that end back days before today, from the start of
// the first to the end of the last.
func dayWindow(now time.Time, days, back int) (*time.Time, *time.Time) {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1 + back))
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, -back)
	return &from, &to
}

// parseDateRange is the days a range covers, or nil for all of them.
func parseDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	days, ok := rangeDays(rangeStr)
	if !ok {
		return nil, nil
	}
	return dayWindow(now, days, 0)
}

// getPreviousDateRange is the period of the same length just before a range,
// to compare it with, or nil when the range has no such period.
func getPreviousDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	days, ok := rangeDays(rangeStr)
	if !ok {
		return nil, nil
	}
	return dayWindow(now, days, days)
}
