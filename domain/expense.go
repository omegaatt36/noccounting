//go:generate go-enum --marshal --names --values

package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// ENUM(food, transport, shopping, activities, accommodation, sightseeing, groceries, flights, fuel, parking, fees, health, tips, other)
type Category string

// ENUM(cash, credit_card, ic_card, e_pay)
type PaymentMethod string

// ENUM(TWD, JPY)
type Currency string

type Expense struct {
	ID           string
	Name         string
	Price        uint64 // Price in smallest currency unit (no decimals)
	Currency     Currency
	ExchangeRate decimal.Decimal // Base-currency units per one unit of Currency
	Category     Category
	Method       PaymentMethod
	PaidByID     string // Backend user id of the payer; empty means no payer
	// Empty participants default to everyone on the trip, shared equally.
	ParticipantIDs []string
	ShoppedAt      time.Time
	// ReceiptURL is a storage file ID on write and an authenticated download path on read.
	ReceiptURL   string
	ReceiptItems []ReceiptItem
}

func (e *Expense) PriceDecimal() decimal.Decimal {
	return decimal.NewFromUint64(e.Price)
}

// A missing exchange rate leaves the amount at face value.
func (e *Expense) TotalInBase(base Currency) decimal.Decimal {
	price := e.PriceDecimal()
	if e.Currency != base && !e.ExchangeRate.IsZero() {
		return price.Mul(e.ExchangeRate)
	}
	return price
}

// Settlement amounts use Currency.
type Settlement struct {
	Currency  Currency
	Balances  []Balance
	Transfers []Transfer
	// Expenses and payments omitted because their exchange rates were unavailable.
	Unconverted int
}

// A positive balance is owed to the person; a negative balance is their debt.
type Balance struct {
	UserID string
	Name   string
	Amount decimal.Decimal
}

type Transfer struct {
	FromID   string
	FromName string
	ToID     string
	ToName   string
	Amount   decimal.Decimal
}

// Rate is units of To per unit of From.
type RateQuote struct {
	From Currency
	To   Currency
	Rate decimal.Decimal
}
