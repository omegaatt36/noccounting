package webapp

import (
	"sort"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

// DashboardData contains aggregated expense data for dashboard display
type DashboardData struct {
	GrandTotalTWD decimal.Decimal
	ItemCount     int
	ByCategory    []CategoryStat
	ByDate        []DailyStat
	ByPayer       []PayerStat
	DateRange     string
}

// CategoryStat represents aggregated data for a single category
type CategoryStat struct {
	Category   domain.Category
	Emoji      string
	AmountTWD  decimal.Decimal
	Percentage float64
}

// DailyStat represents aggregated data for a single date
type DailyStat struct {
	Date       string // format: "M/D" e.g. "2/21"
	AmountTWD  decimal.Decimal
	Percentage float64
}

// PayerStat represents aggregated data for a single payer
type PayerStat struct {
	Name       string
	AmountTWD  decimal.Decimal
	Percentage float64
}

// aggregateDashboard aggregates expenses into dashboard-ready data structures
func aggregateDashboard(expenses []domain.Expense, users []domain.User) DashboardData {
	result := DashboardData{
		GrandTotalTWD: decimal.NewFromInt(0),
		ItemCount:     len(expenses),
		ByCategory:    []CategoryStat{},
		ByDate:        []DailyStat{},
		ByPayer:       []PayerStat{},
	}

	if len(expenses) == 0 {
		return result
	}

	// Build notionID to nickname map
	nicknameMap := make(map[string]string)
	for i := range users {
		nicknameMap[users[i].NotionID] = users[i].Nickname
	}

	// Aggregate by category
	categoryMap := make(map[domain.Category]decimal.Decimal)
	// Aggregate by date
	dateMap := make(map[string]decimal.Decimal)
	// Aggregate by payer
	payerMap := make(map[string]decimal.Decimal)

	// Iterate through expenses and aggregate
	for _, expense := range expenses {
		amountTWD := expense.TotalInTWD()
		result.GrandTotalTWD = result.GrandTotalTWD.Add(amountTWD)

		// Category aggregation
		categoryMap[expense.Category] = categoryMap[expense.Category].Add(amountTWD)

		// Date aggregation (format: "M/D")
		dateStr := formatDate(expense.ShoppedAt)
		dateMap[dateStr] = dateMap[dateStr].Add(amountTWD)

		// Payer aggregation (use nickname if available, otherwise NotionID)
		nickname, ok := nicknameMap[expense.PaidByID]
		if !ok {
			nickname = expense.PaidByID
		}
		payerMap[nickname] = payerMap[nickname].Add(amountTWD)
	}

	// Build ByCategory, sorted by amount descending
	for category, amount := range categoryMap {
		percentage := 0.0
		if !result.GrandTotalTWD.IsZero() {
			pct := amount.Div(result.GrandTotalTWD).Mul(decimal.NewFromInt(100))
			pctFloat, _ := pct.Float64()
			percentage = pctFloat
		}

		result.ByCategory = append(result.ByCategory, CategoryStat{
			Category:   category,
			Emoji:      category.Emoji(),
			AmountTWD:  amount,
			Percentage: percentage,
		})
	}

	// Sort categories by amount descending
	sort.Slice(result.ByCategory, func(i, j int) bool {
		return result.ByCategory[i].AmountTWD.GreaterThan(result.ByCategory[j].AmountTWD)
	})

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

	// Sort by time chronologically
	sort.Slice(dateEntries, func(i, j int) bool {
		return dateEntries[i].time.Before(dateEntries[j].time)
	})

	for _, entry := range dateEntries {
		percentage := 0.0
		if !result.GrandTotalTWD.IsZero() {
			pct := entry.amount.Div(result.GrandTotalTWD).Mul(decimal.NewFromInt(100))
			pctFloat, _ := pct.Float64()
			percentage = pctFloat
		}

		result.ByDate = append(result.ByDate, DailyStat{
			Date:       entry.date,
			AmountTWD:  entry.amount,
			Percentage: percentage,
		})
	}

	// Build ByPayer, sorted by amount descending
	for payer, amount := range payerMap {
		percentage := 0.0
		if !result.GrandTotalTWD.IsZero() {
			pct := amount.Div(result.GrandTotalTWD).Mul(decimal.NewFromInt(100))
			pctFloat, _ := pct.Float64()
			percentage = pctFloat
		}

		result.ByPayer = append(result.ByPayer, PayerStat{
			Name:       payer,
			AmountTWD:  amount,
			Percentage: percentage,
		})
	}

	// Sort payers by amount descending
	sort.Slice(result.ByPayer, func(i, j int) bool {
		return result.ByPayer[i].AmountTWD.GreaterThan(result.ByPayer[j].AmountTWD)
	})

	return result
}

// formatDate formats time.Time to "M/D" string (e.g., "2/21")
func formatDate(t time.Time) string {
	return t.Format("1/2")
}

// parseDateString parses "M/D" string back to time.Time (assuming current year)
func parseDateString(dateStr string) (time.Time, error) {
	return time.Parse("1/2", dateStr)
}

// parseDateRange parses a date range string and returns from/to time pointers
// "today" -> today start to end of day
// "3d" -> today-2 to end of today
// "7d" -> today-6 to end of today
// "all" or "" -> nil, nil (no filter)
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
