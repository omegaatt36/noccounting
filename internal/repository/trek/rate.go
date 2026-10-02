package trek

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

// TREK stores expense units per base unit; domain stores base units per expense unit.
// TREK uses 1 as the unfrozen-rate sentinel for foreign-currency rows.
const unityRate = 1

var ErrUnknownTripCurrency = errors.New("trek trip currency is unknown")

var ErrNoExchangeRate = errors.New("no exchange rate available")

type rateProvider interface {
	GetRate(ctx context.Context, source, target domain.Currency) (decimal.Decimal, error)
}

func toTrekRate(rate decimal.Decimal) (float64, error) {
	if !rate.IsPositive() {
		return 0, fmt.Errorf("%w: a rate of %s cannot be inverted", ErrNoExchangeRate, rate)
	}
	wire, _ := decimal.NewFromInt(1).Div(rate).Float64()
	return wire, nil
}

func fromTrekRate(storedRate float64) (decimal.Decimal, error) {
	if !isPositiveNumber(storedRate) {
		return decimal.Zero, fmt.Errorf("%w: the stored rate %v is not a positive number", ErrNoExchangeRate, storedRate)
	}

	return decimal.NewFromInt(1).Div(decimal.NewFromFloat(storedRate)), nil
}

func rateForWrite(ctx context.Context, expenseCurrency, baseCurrency domain.Currency, rate decimal.Decimal, provider rateProvider) (float64, error) {
	if baseCurrency == "" {
		return 0, ErrUnknownTripCurrency
	}

	if sameCurrency(expenseCurrency, baseCurrency) {
		return unityRate, nil
	}

	if !hasRate(rate) {
		quoted, err := quoteRate(ctx, provider, expenseCurrency, baseCurrency)
		if err != nil {
			return 0, err
		}
		rate = quoted
	}

	return toTrekRate(rate)
}

func rateForRead(ctx context.Context, storedRate float64, expenseCurrency, baseCurrency domain.Currency, provider rateProvider) (decimal.Decimal, error) {
	if baseCurrency == "" {
		return decimal.Zero, ErrUnknownTripCurrency
	}

	if sameCurrency(expenseCurrency, baseCurrency) {
		if hasFrozenRate(storedRate) {
			slog.Debug("a row in the trip currency carries a foreign rate; reporting the amount as it stands",
				"currency", expenseCurrency, "base_currency", baseCurrency, "stored_rate", storedRate)
		}
		return decimal.NewFromInt(unityRate), nil
	}

	if !hasFrozenRate(storedRate) {
		slog.Warn("a foreign row has no frozen rate; quoting it now rather than reporting it as base currency",
			"currency", expenseCurrency, "base_currency", baseCurrency, "stored_rate", storedRate)
		return quoteRate(ctx, provider, expenseCurrency, baseCurrency)
	}

	rate, err := fromTrekRate(storedRate)
	if err != nil {
		return decimal.Zero, fmt.Errorf("reading the %v rate stored for a %s expense: %w", storedRate, expenseCurrency, err)
	}
	return rate, nil
}

func hasFrozenRate(storedRate float64) bool {
	return isPositiveNumber(storedRate) && storedRate != unityRate
}

func hasRate(rate decimal.Decimal) bool {
	return rate.IsPositive() && !rate.Equal(decimal.NewFromInt(unityRate))
}

func quoteRate(ctx context.Context, provider rateProvider, currency, base domain.Currency) (decimal.Decimal, error) {
	if provider == nil {
		return decimal.Zero, fmt.Errorf("%w: no rate provider is configured to quote %s", ErrNoExchangeRate, currency)
	}

	rate, err := provider.GetRate(ctx, currency, base)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: quoting %s in %s from the rate provider: %w", ErrNoExchangeRate, currency, base, err)
	}
	if !rate.IsPositive() {
		return decimal.Zero, fmt.Errorf("%w: the rate provider quoted %s for %s in %s", ErrNoExchangeRate, rate, currency, base)
	}
	return rate, nil
}

func sameCurrency(a, b domain.Currency) bool {
	return strings.EqualFold(string(a), string(b))
}

func isPositiveNumber(value float64) bool {
	return value > 0 && !math.IsInf(value, 1)
}
