package format_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func TestCategoryDisplay_CoversEveryCategory(t *testing.T) {
	for _, cat := range domain.CategoryValues() {
		if format.CategoryEmoji(cat) == "❓" {
			t.Errorf("%q has no emoji", cat)
		}
		if format.CategoryLabel(cat) == string(cat) {
			t.Errorf("%q has no label", cat)
		}
	}
}

func TestPaymentMethodDisplayName(t *testing.T) {
	tests := []struct {
		method domain.PaymentMethod
		name   string
	}{
		{domain.PaymentMethodCash, "現金"},
		{domain.PaymentMethodCreditCard, "信用卡"},
		{domain.PaymentMethodIcCard, "IC卡"},
		{domain.PaymentMethodEPay, "電子支付"},
	}

	for _, tt := range tests {
		t.Run(string(tt.method), func(t *testing.T) {
			if got := format.PaymentLabel(tt.method); got != tt.name {
				t.Errorf("PaymentMethod(%q).DisplayName() = %q, want %q", tt.method, got, tt.name)
			}
		})
	}
}

func TestPaymentMethodEmoji_CoversEveryMethod(t *testing.T) {
	for _, method := range domain.PaymentMethodValues() {
		if format.PaymentEmoji(method) == "❔" {
			t.Errorf("%q has no emoji", method)
		}
	}
}

func TestCurrency_Format(t *testing.T) {
	tests := []struct {
		currency domain.Currency
		amount   string
		want     string
	}{
		{domain.CurrencyTWD, "0", "NT$0"},
		{domain.CurrencyTWD, "1234", "NT$1,234"},
		{domain.CurrencyJPY, "1234567", "¥1,234,567"},
		{domain.CurrencyJPY, "999.6", "¥1,000"},
		{domain.CurrencyTWD, "-1500", "-NT$1,500"},
		{domain.CurrencyTWD, "100", "NT$100"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			amount, _ := decimal.NewFromString(tt.amount)
			if got := format.Money(tt.currency, amount); got != tt.want {
				t.Errorf("%s.Format(%s) = %q, want %q", tt.currency, tt.amount, got, tt.want)
			}
		})
	}
}

func TestTrip_Label(t *testing.T) {
	dated := domain.Trip{Title: "2026 Tokyo", StartDate: day(time.October, 1), EndDate: day(time.October, 9)}
	if got := format.Trip(dated); got != "2026 Tokyo 10/01–10/09" {
		t.Errorf("Label() = %q", got)
	}
	if got := format.Trip(domain.Trip{Title: "Someday"}); got != "Someday" {
		t.Errorf("Label() = %q, want an undated trip by its name alone", got)
	}
}
