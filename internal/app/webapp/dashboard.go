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

// MethodStat's Method is empty for an expense that records no method.
type MethodStat struct {
	Method     domain.PaymentMethod
	Emoji      string
	Label      string
	Amount     decimal.Decimal
	Percentage float64
}

type DailyStat struct {
	Date       string // M/D
	ISODate    string // YYYY-MM-DD
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

const unnamedPayer = "未標示"

func nicknamesOf(users []domain.User) map[string]string {
	names := make(map[string]string, len(users))
	for _, u := range users {
		names[u.BackendUserID] = u.Nickname
	}
	return names
}

// aggregateDashboard takes names from backend user id to display name; an unknown payer shows by id.
func aggregateDashboard(expenses []domain.Expense, names map[string]string, base domain.Currency) DashboardData {
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

	categoryMap := make(map[domain.Category]decimal.Decimal)
	methodMap := make(map[domain.PaymentMethod]decimal.Decimal)
	dateMap := make(map[time.Time]decimal.Decimal)
	payerMap := make(map[string]decimal.Decimal)

	for _, expense := range expenses {
		amount := expense.TotalInBase(base)
		result.GrandTotal = result.GrandTotal.Add(amount)

		categoryMap[expense.Category] = categoryMap[expense.Category].Add(amount)
		methodMap[expense.Method] = methodMap[expense.Method].Add(amount)

		// Date aggregation by full calendar day so cross-year trips sort chronologically.
		day := time.Date(expense.ShoppedAt.Year(), expense.ShoppedAt.Month(), expense.ShoppedAt.Day(), 0, 0, 0, 0, time.UTC)
		dateMap[day] = dateMap[day].Add(amount)

		// Payers are told apart by id: two people can go by the same name.
		payerMap[expense.PaidByID] = payerMap[expense.PaidByID].Add(amount)
	}

	// ByCategory follows the domain's category order, the same as /today, so the donut and legend cannot disagree with it.
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

	for id, amount := range payerMap {
		name, ok := names[id]
		switch {
		case id == "":
			name = unnamedPayer
		case !ok:
			name = id
		}
		result.ByPayer = append(result.ByPayer, PayerStat{
			ID:         id,
			Name:       name,
			Amount:     amount,
			Percentage: percentage(amount, result.GrandTotal),
		})
	}

	// Payers come out of a map, so equal amounts need a tie-break or the order
	// changes from one load to the next.
	slices.SortFunc(result.ByPayer, func(a, b PayerStat) int {
		return cmp.Or(b.Amount.Cmp(a.Amount), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})

	return result
}

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

func dayWindow(now time.Time, days, back int) (*time.Time, *time.Time) {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1 + back))
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, -back)
	return &from, &to
}

func parseDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	days, ok := rangeDays(rangeStr)
	if !ok {
		return nil, nil
	}
	return dayWindow(now, days, 0)
}

func getPreviousDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	days, ok := rangeDays(rangeStr)
	if !ok {
		return nil, nil
	}
	return dayWindow(now, days, days)
}
