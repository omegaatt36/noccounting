package domain

import (
	"context"

	"github.com/shopspring/decimal"
)

// ExchangeRateFetcher defines the interface for fetching exchange rates.
// This abstraction allows different data sources (FinMind, TWSE, etc.)
// to be used interchangeably.
type ExchangeRateFetcher interface {
	// GetRate fetches the exchange rate for converting from source currency to TWD.
	// Returns the rate where: amount_in_TWD = amount_in_source * rate
	GetRate(ctx context.Context, sourceCurrency Currency) (decimal.Decimal, error)
}
