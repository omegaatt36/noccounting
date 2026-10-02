package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func TestExpense_PriceDecimal(t *testing.T) {
	expense := &domain.Expense{Price: 1200}
	got := expense.PriceDecimal()
	expected := decimal.NewFromInt(1200)
	if !got.Equal(expected) {
		t.Errorf("PriceDecimal() = %s, want %s", got, expected)
	}
}

func TestExpense_TotalInBase(t *testing.T) {
	tests := []struct {
		name     string
		base     domain.Currency
		expense  domain.Expense
		expected decimal.Decimal
	}{
		{
			name: "TWD stays as is",
			base: domain.CurrencyTWD,
			expense: domain.Expense{
				Price:    500,
				Currency: domain.CurrencyTWD,
			},
			expected: decimal.NewFromInt(500),
		},
		{
			name: "the base currency ignores a stray rate",
			base: domain.CurrencyTWD,
			expense: domain.Expense{
				Price:        500,
				Currency:     domain.CurrencyTWD,
				ExchangeRate: decimal.NewFromFloat(0.22),
			},
			expected: decimal.NewFromInt(500),
		},
		{
			name: "JPY converted with exchange rate",
			base: domain.CurrencyTWD,
			expense: domain.Expense{
				Price:        1000,
				Currency:     domain.CurrencyJPY,
				ExchangeRate: decimal.NewFromFloat(0.22),
			},
			expected: decimal.NewFromFloat(220),
		},
		{
			name: "TWD converted into a JPY trip",
			base: domain.CurrencyJPY,
			expense: domain.Expense{
				Price:        100,
				Currency:     domain.CurrencyTWD,
				ExchangeRate: decimal.NewFromFloat(4.65),
			},
			expected: decimal.NewFromInt(465),
		},
		{
			name: "JPY with zero exchange rate returns price as is",
			base: domain.CurrencyTWD,
			expense: domain.Expense{
				Price:        1000,
				Currency:     domain.CurrencyJPY,
				ExchangeRate: decimal.Zero,
			},
			expected: decimal.NewFromInt(1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expense.TotalInBase(tt.base)
			if !got.Equal(tt.expected) {
				t.Errorf("TotalInBase(%s) = %s, want %s", tt.base, got, tt.expected)
			}
		})
	}
}
