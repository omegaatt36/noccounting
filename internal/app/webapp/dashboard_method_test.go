package webapp

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func TestAggregateDashboard_GroupsSpendingByPaymentMethod(t *testing.T) {
	now := time.Now()
	expenses := []domain.Expense{
		{Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, Method: domain.PaymentMethodCreditCard, ShoppedAt: now},
		{Price: 300, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, Method: domain.PaymentMethodCash, ShoppedAt: now},
		{Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, Method: domain.PaymentMethodCash, ShoppedAt: now},
		{Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, ShoppedAt: now}, // no method recorded
	}

	got := aggregateDashboard(expenses, nil, domain.CurrencyTWD)

	// The domain's order, cash before credit_card, then what no method was
	// recorded for.
	want := []struct {
		method domain.PaymentMethod
		amount int64
	}{
		{domain.PaymentMethodCash, 400},
		{domain.PaymentMethodCreditCard, 100},
		{"", 100},
	}
	if len(got.ByMethod) != len(want) {
		t.Fatalf("ByMethod = %+v, want %d groups", got.ByMethod, len(want))
	}
	for i, w := range want {
		if got.ByMethod[i].Method != w.method || !got.ByMethod[i].Amount.Equal(decimal.NewFromInt(w.amount)) {
			t.Errorf("ByMethod[%d] = %+v, want %q %d", i, got.ByMethod[i], w.method, w.amount)
		}
	}
	if got.ByMethod[2].Label != "未標示" {
		t.Errorf("the group with no method is labeled %q, want 未標示", got.ByMethod[2].Label)
	}
}

func TestAggregateDashboard_ConvertsIntoTheTripsCurrency(t *testing.T) {
	expenses := []domain.Expense{
		{Price: 1000, Currency: domain.CurrencyJPY, Category: domain.CategoryFood, ShoppedAt: time.Now()},
		{Price: 100, Currency: domain.CurrencyTWD, ExchangeRate: decimal.RequireFromString("4.65"), Category: domain.CategoryFood, ShoppedAt: time.Now()},
	}

	got := aggregateDashboard(expenses, nil, domain.CurrencyJPY)

	// 1000 JPY as it stands, and 100 TWD at 4.65 JPY each.
	if want := decimal.NewFromInt(1465); !got.GrandTotal.Equal(want) {
		t.Errorf("GrandTotal = %s, want %s", got.GrandTotal, want)
	}
	if got.Currency != domain.CurrencyJPY {
		t.Errorf("Currency = %s, want the trip's", got.Currency)
	}
}
