//go:generate go-enum --marshal --names --values

package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Category represents expense categories.
// ENUM(食, 衣, 住, 行, 樂)
type Category string

// PaymentMethod represents payment methods.
// ENUM(cash, credit_card, ic_card, paypay)
type PaymentMethod string

// DisplayName returns a human-readable name for the payment method.
func (p PaymentMethod) DisplayName() string {
	switch p {
	case PaymentMethodCash:
		return "現金"
	case PaymentMethodCreditCard:
		return "信用卡"
	case PaymentMethodIcCard:
		return "IC卡"
	case PaymentMethodPaypay:
		return "PayPay"
	default:
		return string(p)
	}
}

// Currency represents supported currencies.
// ENUM(TWD, JPY)
type Currency string

// Expense represents a single expense record.
type Expense struct {
	ID           string
	Name         string
	Price        uint64 // Price in smallest currency unit (no decimals)
	Currency     Currency
	ExchangeRate decimal.Decimal // Exchange rate to TWD
	Category     Category
	Method       PaymentMethod
	PaidByID     string // User ID in the storage system
	ShoppedAt    time.Time
}

// PriceDecimal returns the price as a decimal.
func (e *Expense) PriceDecimal() decimal.Decimal {
	return decimal.NewFromUint64(e.Price)
}

// TotalInTWD calculates the total amount in TWD.
func (e *Expense) TotalInTWD() decimal.Decimal {
	price := e.PriceDecimal()
	if e.Currency == CurrencyJPY && !e.ExchangeRate.IsZero() {
		return price.Mul(e.ExchangeRate)
	}
	return price
}

// ExpenseSummary represents a summary of expenses for splitting.
type ExpenseSummary struct {
	TotalByPayer map[string]decimal.Decimal // User ID -> total amount (in TWD)
	GrandTotal   decimal.Decimal
	ItemCount    int
}
