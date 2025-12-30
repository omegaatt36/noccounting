package webapp

import (
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

// mustDecimal is a helper to create a decimal.Decimal from string, panicking on error
func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// TestAggregateDashboard_Empty tests that empty expenses returns zero totals
func TestAggregateDashboard_Empty(t *testing.T) {
	expenses := []domain.Expense{}
	users := []domain.User{}

	result := aggregateDashboard(expenses, users)

	if !result.GrandTotalTWD.Equal(decimal.NewFromInt(0)) {
		t.Errorf("GrandTotalTWD: expected 0, got %v", result.GrandTotalTWD)
	}
	if result.ItemCount != 0 {
		t.Errorf("ItemCount: expected 0, got %d", result.ItemCount)
	}
	if len(result.ByCategory) != 0 {
		t.Errorf("ByCategory: expected empty, got %d items", len(result.ByCategory))
	}
	if len(result.ByDate) != 0 {
		t.Errorf("ByDate: expected empty, got %d items", len(result.ByDate))
	}
	if len(result.ByPayer) != 0 {
		t.Errorf("ByPayer: expected empty, got %d items", len(result.ByPayer))
	}
}

// TestAggregateDashboard_SingleExpense tests single JPY expense with exchange rate
func TestAggregateDashboard_SingleExpense(t *testing.T) {
	shopDate := time.Date(2026, 2, 21, 10, 30, 0, 0, time.UTC)

	expenses := []domain.Expense{
		{
			ID:           "exp-1",
			Name:         "Lunch",
			Price:        1000, // 1000 JPY (smallest unit)
			Currency:     domain.CurrencyJPY,
			ExchangeRate: mustDecimal("0.2"), // 1 JPY = 0.2 TWD
			Category:     domain.Category食,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "user-1",
			ShoppedAt:    shopDate,
		},
	}

	users := []domain.User{
		{
			ID:       1,
			NotionID: "user-1",
			Nickname: "Alice",
		},
	}

	result := aggregateDashboard(expenses, users)

	// 1000 * 0.2 = 200 TWD
	expectedTotal := mustDecimal("200")
	if !result.GrandTotalTWD.Equal(expectedTotal) {
		t.Errorf("GrandTotalTWD: expected %v, got %v", expectedTotal, result.GrandTotalTWD)
	}

	if result.ItemCount != 1 {
		t.Errorf("ItemCount: expected 1, got %d", result.ItemCount)
	}

	// Check ByCategory
	if len(result.ByCategory) != 1 {
		t.Fatalf("ByCategory: expected 1 category, got %d", len(result.ByCategory))
	}
	cat := result.ByCategory[0]
	if cat.Category != domain.Category食 {
		t.Errorf("Category: expected %s, got %s", domain.Category食, cat.Category)
	}
	if cat.Emoji != "🍜" {
		t.Errorf("Emoji: expected 🍜, got %s", cat.Emoji)
	}
	if !cat.AmountTWD.Equal(expectedTotal) {
		t.Errorf("AmountTWD: expected %v, got %v", expectedTotal, cat.AmountTWD)
	}
	if cat.Percentage != 100.0 {
		t.Errorf("Percentage: expected 100.0, got %f", cat.Percentage)
	}

	// Check ByDate
	if len(result.ByDate) != 1 {
		t.Fatalf("ByDate: expected 1 date, got %d", len(result.ByDate))
	}
	date := result.ByDate[0]
	if date.Date != "2/21" {
		t.Errorf("Date: expected 2/21, got %s", date.Date)
	}
	if !date.AmountTWD.Equal(expectedTotal) {
		t.Errorf("AmountTWD: expected %v, got %v", expectedTotal, date.AmountTWD)
	}

	// Check ByPayer
	if len(result.ByPayer) != 1 {
		t.Fatalf("ByPayer: expected 1 payer, got %d", len(result.ByPayer))
	}
	payer := result.ByPayer[0]
	if payer.Name != "Alice" {
		t.Errorf("Name: expected Alice, got %s", payer.Name)
	}
	if !payer.AmountTWD.Equal(expectedTotal) {
		t.Errorf("AmountTWD: expected %v, got %v", expectedTotal, payer.AmountTWD)
	}
}

// TestAggregateDashboard_MultipleExpenses tests multiple expenses with sorting
func TestAggregateDashboard_MultipleExpenses(t *testing.T) {
	date1 := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	date3 := time.Date(2026, 2, 22, 9, 0, 0, 0, time.UTC)

	expenses := []domain.Expense{
		// Category 食, 200 TWD
		{
			ID:           "exp-1",
			Name:         "Lunch",
			Price:        1000,
			Currency:     domain.CurrencyJPY,
			ExchangeRate: mustDecimal("0.2"),
			Category:     domain.Category食,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "user-1",
			ShoppedAt:    date1,
		},
		// Category 住, 500 TWD
		{
			ID:           "exp-2",
			Name:         "Hotel",
			Price:        500,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.Category住,
			Method:       domain.PaymentMethodCreditCard,
			PaidByID:     "user-2",
			ShoppedAt:    date2,
		},
		// Category 行, 100 TWD
		{
			ID:           "exp-3",
			Name:         "Train",
			Price:        100,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.Category行,
			Method:       domain.PaymentMethodIcCard,
			PaidByID:     "user-1",
			ShoppedAt:    date3,
		},
		// Category 食, 150 TWD on date2
		{
			ID:           "exp-4",
			Name:         "Dinner",
			Price:        750,
			Currency:     domain.CurrencyJPY,
			ExchangeRate: mustDecimal("0.2"),
			Category:     domain.Category食,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "user-2",
			ShoppedAt:    date2,
		},
		// Category 購, 300 TWD
		{
			ID:           "exp-5",
			Name:         "Shopping",
			Price:        300,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.Category購,
			Method:       domain.PaymentMethodEPay,
			PaidByID:     "user-3",
			ShoppedAt:    date1,
		},
	}

	users := []domain.User{
		{ID: 1, NotionID: "user-1", Nickname: "Alice"},
		{ID: 2, NotionID: "user-2", Nickname: "Bob"},
		{ID: 3, NotionID: "user-3", Nickname: "Charlie"},
	}

	result := aggregateDashboard(expenses, users)

	// Grand total: 200 + 500 + 100 + 150 + 300 = 1250 TWD
	expectedTotal := mustDecimal("1250")
	if !result.GrandTotalTWD.Equal(expectedTotal) {
		t.Errorf("GrandTotalTWD: expected %v, got %v", expectedTotal, result.GrandTotalTWD)
	}

	if result.ItemCount != 5 {
		t.Errorf("ItemCount: expected 5, got %d", result.ItemCount)
	}

	// Check ByCategory sorting (descending by amount)
	if len(result.ByCategory) != 4 {
		t.Fatalf("ByCategory: expected 4 categories, got %d", len(result.ByCategory))
	}

	expectedCategoryOrder := []struct {
		category domain.Category
		amount   string
		percent  float64
	}{
		{domain.Category食, "350", 28.0}, // 200 + 150
		{domain.Category住, "500", 40.0}, // 500
		{domain.Category購, "300", 24.0}, // 300
		{domain.Category行, "100", 8.0},  // 100
	}

	// Build a map of results for easier checking
	catMap := make(map[domain.Category]CategoryStat)
	for _, cat := range result.ByCategory {
		catMap[cat.Category] = cat
	}

	for _, expected := range expectedCategoryOrder {
		stat, ok := catMap[expected.category]
		if !ok {
			t.Errorf("Category %s not found", expected.category)
			continue
		}

		expectedAmount := mustDecimal(expected.amount)
		if !stat.AmountTWD.Equal(expectedAmount) {
			t.Errorf("Category %s: expected amount %v, got %v", expected.category, expectedAmount, stat.AmountTWD)
		}

		// Check percentage with small tolerance
		if stat.Percentage < expected.percent-0.1 || stat.Percentage > expected.percent+0.1 {
			t.Errorf("Category %s: expected percentage %.1f, got %.1f", expected.category, expected.percent, stat.Percentage)
		}
	}

	// Check ByDate sorting (chronological)
	if len(result.ByDate) != 3 {
		t.Fatalf("ByDate: expected 3 dates, got %d", len(result.ByDate))
	}

	expectedDates := []struct {
		date   string
		amount string
	}{
		{"2/20", "500"}, // 200 + 300
		{"2/21", "650"}, // 500 + 150
		{"2/22", "100"}, // 100
	}

	for i, expected := range expectedDates {
		actual := result.ByDate[i]
		if actual.Date != expected.date {
			t.Errorf("Date[%d]: expected %s, got %s", i, expected.date, actual.Date)
		}
		expectedAmount := mustDecimal(expected.amount)
		if !actual.AmountTWD.Equal(expectedAmount) {
			t.Errorf("Date[%d] amount: expected %v, got %v", i, expectedAmount, actual.AmountTWD)
		}
	}

	// Check ByPayer sorting (descending by amount)
	if len(result.ByPayer) != 3 {
		t.Fatalf("ByPayer: expected 3 payers, got %d", len(result.ByPayer))
	}

	expectedPayerOrder := []struct {
		name    string
		amount  string
		percent float64
	}{
		{"Bob", "650", 52.0},     // 500 + 150
		{"Alice", "300", 24.0},   // 200 + 100
		{"Charlie", "300", 24.0}, // 300
	}

	payerMap := make(map[string]PayerStat)
	for _, p := range result.ByPayer {
		payerMap[p.Name] = p
	}

	for _, expected := range expectedPayerOrder {
		stat, ok := payerMap[expected.name]
		if !ok {
			t.Errorf("Payer %s not found", expected.name)
			continue
		}

		expectedAmount := mustDecimal(expected.amount)
		if !stat.AmountTWD.Equal(expectedAmount) {
			t.Errorf("Payer %s: expected amount %v, got %v", expected.name, expectedAmount, stat.AmountTWD)
		}

		if stat.Percentage < expected.percent-0.1 || stat.Percentage > expected.percent+0.1 {
			t.Errorf("Payer %s: expected percentage %.1f, got %.1f", expected.name, expected.percent, stat.Percentage)
		}
	}

	// Verify ByPayer is sorted by amount descending
	for i := 1; i < len(result.ByPayer); i++ {
		if result.ByPayer[i].AmountTWD.GreaterThan(result.ByPayer[i-1].AmountTWD) {
			t.Errorf("ByPayer not sorted descending: %v > %v", result.ByPayer[i].AmountTWD, result.ByPayer[i-1].AmountTWD)
		}
	}
}

// TestParseDateRange tests date range parsing
func TestParseDateRange(t *testing.T) {
	now := time.Date(2026, 2, 22, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		rangeStr string
		wantFrom time.Time
		wantTo   time.Time
		wantNil  bool
	}{
		{
			name:     "today",
			rangeStr: "today",
			wantFrom: time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 22, 23, 59, 59, 999999999, time.UTC),
			wantNil:  false,
		},
		{
			name:     "3d (today, yesterday, day before)",
			rangeStr: "3d",
			wantFrom: time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 22, 23, 59, 59, 999999999, time.UTC),
			wantNil:  false,
		},
		{
			name:     "7d (today-6 to today)",
			rangeStr: "7d",
			wantFrom: time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 22, 23, 59, 59, 999999999, time.UTC),
			wantNil:  false,
		},
		{
			name:     "all",
			rangeStr: "all",
			wantNil:  true,
		},
		{
			name:     "empty string",
			rangeStr: "",
			wantNil:  true,
		},
		{
			name:     "invalid range",
			rangeStr: "invalid",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := parseDateRange(tt.rangeStr, now)

			if tt.wantNil {
				if from != nil || to != nil {
					t.Errorf("expected nil, got from=%v, to=%v", from, to)
				}
				return
			}

			if from == nil || to == nil {
				t.Fatalf("expected non-nil pointers, got from=%v, to=%v", from, to)
			}

			if !from.Equal(tt.wantFrom) {
				t.Errorf("from: expected %v, got %v", tt.wantFrom, *from)
			}
			if !to.Equal(tt.wantTo) {
				t.Errorf("to: expected %v, got %v", tt.wantTo, *to)
			}
		})
	}
}
