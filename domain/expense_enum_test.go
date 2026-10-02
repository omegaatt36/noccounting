package domain_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestCategoryValues(t *testing.T) {
	expected := []domain.Category{
		domain.CategoryFood,
		domain.CategoryTransport,
		domain.CategoryShopping,
		domain.CategoryActivities,
		domain.CategoryAccommodation,
		domain.CategorySightseeing,
		domain.CategoryGroceries,
		domain.CategoryFlights,
		domain.CategoryFuel,
		domain.CategoryParking,
		domain.CategoryFees,
		domain.CategoryHealth,
		domain.CategoryTips,
		domain.CategoryOther,
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

func TestParseCategory(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.Category
		wantErr  bool
	}{
		{"food", domain.CategoryFood, false},
		{"flights", domain.CategoryFlights, false},
		{"other", domain.CategoryOther, false},
		{"食", domain.Category(""), true},
		{"Food", domain.Category(""), true},
		{"", domain.Category(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := domain.ParseCategory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCategory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("ParseCategory(%q) = %q, want %q", tt.input, got, tt.expected)
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
