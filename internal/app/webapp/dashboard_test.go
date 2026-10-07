package webapp

import (
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestAggregateDashboard_Empty(t *testing.T) {
	expenses := []domain.Expense{}
	users := []domain.User{}

	result := aggregateDashboard(expenses, users, domain.CurrencyTWD)

	if !result.GrandTotal.Equal(decimal.NewFromInt(0)) {
		t.Errorf("GrandTotal: expected 0, got %v", result.GrandTotal)
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

func TestAggregateDashboard_SingleExpense(t *testing.T) {
	shopDate := time.Date(2026, 2, 21, 10, 30, 0, 0, time.UTC)

	expenses := []domain.Expense{
		{
			ID:           "exp-1",
			Name:         "Lunch",
			Price:        1000, // 1000 JPY (smallest unit)
			Currency:     domain.CurrencyJPY,
			ExchangeRate: mustDecimal("0.2"), // 1 JPY = 0.2 TWD
			Category:     domain.CategoryFood,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "1",
			ShoppedAt:    shopDate,
		},
	}

	users := []domain.User{
		{
			ID:            1,
			BackendUserID: "1",
			Nickname:      "Alice",
		},
	}

	result := aggregateDashboard(expenses, users, domain.CurrencyTWD)

	// 1000 * 0.2 = 200 TWD
	expectedTotal := mustDecimal("200")
	if !result.GrandTotal.Equal(expectedTotal) {
		t.Errorf("GrandTotal: expected %v, got %v", expectedTotal, result.GrandTotal)
	}

	if result.ItemCount != 1 {
		t.Errorf("ItemCount: expected 1, got %d", result.ItemCount)
	}

	// Check ByCategory
	if len(result.ByCategory) != 1 {
		t.Fatalf("ByCategory: expected 1 category, got %d", len(result.ByCategory))
	}
	cat := result.ByCategory[0]
	if cat.Category != domain.CategoryFood {
		t.Errorf("Category: expected %s, got %s", domain.CategoryFood, cat.Category)
	}
	if cat.Emoji != "🍜" {
		t.Errorf("Emoji: expected 🍜, got %s", cat.Emoji)
	}
	if !cat.Amount.Equal(expectedTotal) {
		t.Errorf("Amount: expected %v, got %v", expectedTotal, cat.Amount)
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
	if !date.Amount.Equal(expectedTotal) {
		t.Errorf("Amount: expected %v, got %v", expectedTotal, date.Amount)
	}

	// Check ByPayer
	if len(result.ByPayer) != 1 {
		t.Fatalf("ByPayer: expected 1 payer, got %d", len(result.ByPayer))
	}
	payer := result.ByPayer[0]
	if payer.Name != "Alice" {
		t.Errorf("Name: expected Alice, got %s", payer.Name)
	}
	if !payer.Amount.Equal(expectedTotal) {
		t.Errorf("Amount: expected %v, got %v", expectedTotal, payer.Amount)
	}
}

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
			Category:     domain.CategoryFood,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "1",
			ShoppedAt:    date1,
		},
		// Category 住, 500 TWD
		{
			ID:           "exp-2",
			Name:         "Hotel",
			Price:        500,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.CategoryAccommodation,
			Method:       domain.PaymentMethodCreditCard,
			PaidByID:     "2",
			ShoppedAt:    date2,
		},
		// Category 行, 100 TWD
		{
			ID:           "exp-3",
			Name:         "Train",
			Price:        100,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.CategoryTransport,
			Method:       domain.PaymentMethodIcCard,
			PaidByID:     "1",
			ShoppedAt:    date3,
		},
		// Category 食, 150 TWD on date2
		{
			ID:           "exp-4",
			Name:         "Dinner",
			Price:        750,
			Currency:     domain.CurrencyJPY,
			ExchangeRate: mustDecimal("0.2"),
			Category:     domain.CategoryFood,
			Method:       domain.PaymentMethodCash,
			PaidByID:     "2",
			ShoppedAt:    date2,
		},
		// Category 購, 300 TWD
		{
			ID:           "exp-5",
			Name:         "Shopping",
			Price:        300,
			Currency:     domain.CurrencyTWD,
			ExchangeRate: decimal.NewFromInt(1),
			Category:     domain.CategoryShopping,
			Method:       domain.PaymentMethodEPay,
			PaidByID:     "3",
			ShoppedAt:    date1,
		},
	}

	users := []domain.User{
		{ID: 1, BackendUserID: "1", Nickname: "Alice"},
		{ID: 2, BackendUserID: "2", Nickname: "Bob"},
		{ID: 3, BackendUserID: "3", Nickname: "Charlie"},
	}

	result := aggregateDashboard(expenses, users, domain.CurrencyTWD)

	// Grand total: 200 + 500 + 100 + 150 + 300 = 1250 TWD
	expectedTotal := mustDecimal("1250")
	if !result.GrandTotal.Equal(expectedTotal) {
		t.Errorf("GrandTotal: expected %v, got %v", expectedTotal, result.GrandTotal)
	}

	if result.ItemCount != 5 {
		t.Errorf("ItemCount: expected 5, got %d", result.ItemCount)
	}

	// Check ByCategory: noccounting's own category order (R7), down to the
	// slice itself — a map would check the members and never the order both
	// the panel and the donut render.
	if len(result.ByCategory) != 4 {
		t.Fatalf("ByCategory: expected 4 categories, got %d", len(result.ByCategory))
	}

	expectedCategoryOrder := []struct {
		category domain.Category
		amount   string
		percent  float64
	}{
		{domain.CategoryFood, "350", 28.0},          // 200 + 150
		{domain.CategoryTransport, "100", 8.0},      // 100
		{domain.CategoryShopping, "300", 24.0},      // 300
		{domain.CategoryAccommodation, "500", 40.0}, // 500
	}

	// food, transport, shopping, accommodation is the domain's order, and it
	// differs from an amount ranking here, so this pins that the order wins.
	for i, expected := range expectedCategoryOrder {
		stat := result.ByCategory[i]
		if stat.Category != expected.category {
			t.Errorf("ByCategory[%d] = %s, want %s (noccounting's own category order)", i, stat.Category, expected.category)
		}

		expectedAmount := mustDecimal(expected.amount)
		if !stat.Amount.Equal(expectedAmount) {
			t.Errorf("Category %s: expected amount %v, got %v", expected.category, expectedAmount, stat.Amount)
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
		if !actual.Amount.Equal(expectedAmount) {
			t.Errorf("Date[%d] amount: expected %v, got %v", i, expectedAmount, actual.Amount)
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
		if !stat.Amount.Equal(expectedAmount) {
			t.Errorf("Payer %s: expected amount %v, got %v", expected.name, expectedAmount, stat.Amount)
		}

		if stat.Percentage < expected.percent-0.1 || stat.Percentage > expected.percent+0.1 {
			t.Errorf("Payer %s: expected percentage %.1f, got %.1f", expected.name, expected.percent, stat.Percentage)
		}
	}

	// Verify ByPayer is sorted by amount descending
	for i := 1; i < len(result.ByPayer); i++ {
		if result.ByPayer[i].Amount.GreaterThan(result.ByPayer[i-1].Amount) {
			t.Errorf("ByPayer not sorted descending: %v > %v", result.ByPayer[i].Amount, result.ByPayer[i-1].Amount)
		}
	}
}

// TestAggregateDashboard_CategoryBeatsAmount is R7's order clause on a case
// where following the amounts would give a different answer than following
// the domain's category order: the biggest group is 購 last in the domain's
// order, and it still renders there rather than first.
func TestAggregateDashboard_CategoryBeatsAmount(t *testing.T) {
	when := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	expenses := []domain.Expense{
		{ID: "shop", Name: "購物", Price: 9000, Currency: domain.CurrencyTWD, Category: domain.CategoryShopping, ShoppedAt: when},
		{ID: "food", Name: "拉麵", Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, ShoppedAt: when},
		{ID: "misc", Name: "雜費", Price: 10, Currency: domain.CurrencyTWD, Category: domain.CategoryOther, ShoppedAt: when},
	}

	result := aggregateDashboard(expenses, nil, domain.CurrencyTWD)

	got := make([]domain.Category, 0, len(result.ByCategory))
	for _, stat := range result.ByCategory {
		got = append(got, stat.Category)
	}
	want := []domain.Category{domain.CategoryFood, domain.CategoryShopping, domain.CategoryOther}
	if !slices.Equal(got, want) {
		t.Errorf("ByCategory order = %v, want %v — the domain's order, not an amount ranking", got, want)
	}
}

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

func TestGetPreviousDateRange(t *testing.T) {
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
			wantFrom: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 21, 23, 59, 59, 999999999, time.UTC),
			wantNil:  false,
		},
		{
			name:     "3d (previous 3-day block)",
			rangeStr: "3d",
			wantFrom: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 19, 23, 59, 59, 999999999, time.UTC),
			wantNil:  false,
		},
		{
			name:     "7d (previous 7-day block)",
			rangeStr: "7d",
			wantFrom: time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 2, 15, 23, 59, 59, 999999999, time.UTC),
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
			rangeStr: "other",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := getPreviousDateRange(tt.rangeStr, now)
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

func TestIsWithinRange(t *testing.T) {
	from := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		from *time.Time
		to   *time.Time
		want bool
	}{
		{"within bounded range", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), &from, &to, true},
		{"before bounded range", time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC), &from, &to, false},
		{"after bounded range", time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC), &from, &to, false},
		{"nil bounds", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), nil, nil, true},
		{"nil from only within", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), nil, &to, true},
		{"nil from only after", time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC), nil, &to, false},
		{"nil to only within", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), &from, nil, true},
		{"nil to only before", time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC), &from, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWithinRange(tt.t, tt.from, tt.to); got != tt.want {
				t.Errorf("isWithinRange() = %v, want %v", got, tt.want)
			}
		})
	}
}
