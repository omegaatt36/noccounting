package domain_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestCategoryValues(t *testing.T) {
	expected := []domain.Category{
		domain.Category食,
		domain.Category住,
		domain.Category行,
		domain.Category購,
		domain.Category樂,
		domain.Category雜,
	}

	got := domain.CategoryValues()
	if len(got) != len(expected) {
		t.Fatalf("expected %d categories, got %d", len(expected), len(got))
	}

	for i, cat := range expected {
		if got[i] != cat {
			t.Errorf("category[%d]: expected %q, got %q", i, cat, got[i])
		}
	}
}

func TestCategoryNames(t *testing.T) {
	names := domain.CategoryNames()
	expected := []string{"食", "住", "行", "購", "樂", "雜"}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}

	for i, name := range expected {
		if names[i] != name {
			t.Errorf("name[%d]: expected %q, got %q", i, name, names[i])
		}
	}
}

func TestParseCategory(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.Category
		wantErr  bool
	}{
		{"食", domain.Category食, false},
		{"住", domain.Category住, false},
		{"行", domain.Category行, false},
		{"購", domain.Category購, false},
		{"樂", domain.Category樂, false},
		{"雜", domain.Category雜, false},
		{"衣", domain.Category(""), true},
		{"無", domain.Category(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := domain.ParseCategory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCategory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("ParseCategory(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCategoryEmoji(t *testing.T) {
	tests := []struct {
		cat   domain.Category
		emoji string
	}{
		{domain.Category食, "🍜"},
		{domain.Category住, "🏠"},
		{domain.Category行, "🚃"},
		{domain.Category購, "🛍️"},
		{domain.Category樂, "🎯"},
		{domain.Category雜, "📎"},
	}

	for _, tt := range tests {
		t.Run(string(tt.cat), func(t *testing.T) {
			if got := tt.cat.Emoji(); got != tt.emoji {
				t.Errorf("Category(%q).Emoji() = %q, want %q", tt.cat, got, tt.emoji)
			}
		})
	}
}

func TestPaymentMethodValues(t *testing.T) {
	expected := []domain.PaymentMethod{
		domain.PaymentMethodCash,
		domain.PaymentMethodCreditCard,
		domain.PaymentMethodIcCard,
		domain.PaymentMethodEPay,
	}

	got := domain.PaymentMethodValues()
	if len(got) != len(expected) {
		t.Fatalf("expected %d methods, got %d", len(expected), len(got))
	}

	for i, method := range expected {
		if got[i] != method {
			t.Errorf("method[%d]: expected %q, got %q", i, method, got[i])
		}
	}
}

func TestParsePaymentMethod(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.PaymentMethod
		wantErr  bool
	}{
		{"cash", domain.PaymentMethodCash, false},
		{"credit_card", domain.PaymentMethodCreditCard, false},
		{"ic_card", domain.PaymentMethodIcCard, false},
		{"e_pay", domain.PaymentMethodEPay, false},
		{"paypay", domain.PaymentMethod(""), true},
		{"bitcoin", domain.PaymentMethod(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := domain.ParsePaymentMethod(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePaymentMethod(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("ParsePaymentMethod(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
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
			if got := tt.method.DisplayName(); got != tt.name {
				t.Errorf("PaymentMethod(%q).DisplayName() = %q, want %q", tt.method, got, tt.name)
			}
		})
	}
}
