package webapp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/app/webapp/components"
	"github.com/shopspring/decimal"
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
	Amount     decimal.Decimal
	Percentage float64
}

type PayerStat struct {
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
	dateMap := make(map[string]decimal.Decimal)
	payerMap := make(map[string]decimal.Decimal)

	for _, expense := range expenses {
		amount := expense.TotalInBase(base)
		result.GrandTotal = result.GrandTotal.Add(amount)

		categoryMap[expense.Category] = categoryMap[expense.Category].Add(amount)
		methodMap[expense.Method] = methodMap[expense.Method].Add(amount)

		// Date aggregation (format: "M/D")
		dateStr := formatDate(expense.ShoppedAt)
		dateMap[dateStr] = dateMap[dateStr].Add(amount)

		// Payer aggregation (use nickname if available, otherwise the backend user id)
		nickname, ok := nicknameMap[expense.PaidByID]
		if !ok {
			nickname = expense.PaidByID
		}
		payerMap[nickname] = payerMap[nickname].Add(amount)
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
		date   string
		time   time.Time
		amount decimal.Decimal
	}
	var dateEntries []dateEntry

	for dateStr, amount := range dateMap {
		t, _ := parseDateString(dateStr)
		dateEntries = append(dateEntries, dateEntry{
			date:   dateStr,
			time:   t,
			amount: amount,
		})
	}

	sort.Slice(dateEntries, func(i, j int) bool {
		return dateEntries[i].time.Before(dateEntries[j].time)
	})

	for _, entry := range dateEntries {
		result.ByDate = append(result.ByDate, DailyStat{
			Date:       entry.date,
			Amount:     entry.amount,
			Percentage: percentage(entry.amount, result.GrandTotal),
		})
	}

	// Build ByPayer, sorted by amount descending
	for payer, amount := range payerMap {
		result.ByPayer = append(result.ByPayer, PayerStat{
			Name:       payer,
			Amount:     amount,
			Percentage: percentage(amount, result.GrandTotal),
		})
	}

	sort.Slice(result.ByPayer, func(i, j int) bool {
		return result.ByPayer[i].Amount.GreaterThan(result.ByPayer[j].Amount)
	})

	return result
}

// formatDate formats time.Time to "M/D" string (e.g., "2/21")
func formatDate(t time.Time) string {
	return t.Format("1/2")
}

func parseDateString(dateStr string) (time.Time, error) {
	return time.Parse("1/2", dateStr)
}

func getPreviousDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	switch rangeStr {
	case "today":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, -1)
		return &from, &to

	case "3d":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -5)
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, -3)
		return &from, &to

	case "7d":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -13)
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, -7)
		return &from, &to

	case "all", "":
		return nil, nil

	default:
		return nil, nil
	}
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

func parseDateRange(rangeStr string, now time.Time) (*time.Time, *time.Time) {
	switch rangeStr {
	case "today":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
		return &from, &to

	case "3d":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -2)
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
		return &from, &to

	case "7d":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -6)
		to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
		return &from, &to

	case "all", "":
		return nil, nil

	default:
		return nil, nil
	}
}
