package trek

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

type stubRateProvider struct {
	rate decimal.Decimal
	err  error
}

func (s stubRateProvider) GetRate(_ context.Context, _, _ domain.Currency) (decimal.Decimal, error) {
	if s.err != nil {
		return decimal.Zero, s.err
	}
	return s.rate, nil
}

type spyRateProvider struct {
	rate   decimal.Decimal
	err    error
	asked  []domain.Currency
	target []domain.Currency
}

func (s *spyRateProvider) GetRate(_ context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	s.asked = append(s.asked, source)
	s.target = append(s.target, target)
	if s.err != nil {
		return decimal.Zero, s.err
	}
	return s.rate, nil
}

func jpyQuote() decimal.Decimal { return decimal.NewFromFloat(0.22) }

func assertRateClose(t *testing.T, got, want decimal.Decimal, tolerance float64) {
	t.Helper()
	if drift := got.Sub(want).Abs(); drift.GreaterThan(decimal.NewFromFloat(tolerance)) {
		t.Errorf("rate = %s, want %s (drift %s exceeds %v)", got, want, drift, tolerance)
	}
}

func TestRateForWrite_StoresTheRateTREKDivides(t *testing.T) {
	provider := &spyRateProvider{rate: jpyQuote()}

	stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD, jpyQuote(), provider)
	if err != nil {
		t.Fatalf("rateForWrite() error = %v", err)
	}
	assertRateClose(t, decimal.NewFromFloat(stored), decimal.NewFromFloat(4.5455), 0.0001)
	if len(provider.asked) != 0 {
		t.Errorf("a supplied rate must not be re-quoted, asked for %v", provider.asked)
	}
}

func TestRateForRead_RecoversTheRateTotalInBaseMultiplies(t *testing.T) {
	rate, err := rateForRead(context.Background(), 4.5455, domain.CurrencyJPY, domain.CurrencyTWD, nil)
	if err != nil {
		t.Fatalf("rateForRead() error = %v", err)
	}
	assertRateClose(t, rate, jpyQuote(), 0.00005)

	if total := decimal.NewFromInt(1000).Mul(rate).Round(2); !total.Equal(decimal.NewFromInt(220)) {
		t.Errorf("1000 JPY totals %s TWD, want 220", total)
	}
}

func TestRateConversion_RoundTripsAcrossTheBoundary(t *testing.T) {
	tests := []struct {
		name   string
		quoted float64
	}{
		{name: "the R6 JPY quote", quoted: 0.22},
		{name: "a rate that halves", quoted: 0.5},
		{name: "a rate below one hundredth", quoted: 0.075},
		{name: "a rate above one", quoted: 1.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD, decimal.NewFromFloat(tt.quoted), nil)
			if err != nil {
				t.Fatalf("rateForWrite() error = %v", err)
			}

			back, err := rateForRead(context.Background(), stored, domain.CurrencyJPY, domain.CurrencyTWD, nil)
			if err != nil {
				t.Fatalf("rateForRead() error = %v", err)
			}
			assertRateClose(t, back, decimal.NewFromFloat(tt.quoted), 0.000000001)
			assertRateClose(t, decimal.NewFromInt(1000).Mul(back), decimal.NewFromInt(1000).Mul(decimal.NewFromFloat(tt.quoted)), 0.001)
		})
	}
}

func TestToTrekRate_InvertsNoccountingsRate(t *testing.T) {
	tests := []struct {
		name string
		rate float64
		want float64
	}{
		{name: "0.22 TWD per JPY stores as 1/0.22", rate: 0.22, want: 4.5455},
		{name: "a halving rate", rate: 0.5, want: 2},
		{name: "a rate above one", rate: 4, want: 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := toTrekRate(decimal.NewFromFloat(tt.rate))
			if err != nil {
				t.Fatalf("toTrekRate(%v) error = %v", tt.rate, err)
			}
			assertRateClose(t, decimal.NewFromFloat(stored), decimal.NewFromFloat(tt.want), 0.0001)
		})
	}
}

func TestFromTrekRate_InvertsTREKsRate(t *testing.T) {
	tests := []struct {
		name   string
		stored float64
		want   float64
	}{
		{name: "the documented 4.5455", stored: 4.5455, want: 0.22},
		{name: "2 expense units per base unit", stored: 2, want: 0.5},
		{name: "a quarter of an expense unit per base unit", stored: 0.25, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := fromTrekRate(tt.stored)
			if err != nil {
				t.Fatalf("fromTrekRate(%v) error = %v", tt.stored, err)
			}
			assertRateClose(t, rate, decimal.NewFromFloat(tt.want), 0.0001)
		})
	}
}

func TestToTrekRate_RefusesRatesItCannotInvert(t *testing.T) {
	tests := []struct {
		name string
		rate decimal.Decimal
	}{
		{name: "no rate supplied", rate: decimal.Zero},
		{name: "a negative rate is not a quote", rate: decimal.NewFromInt(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := toTrekRate(tt.rate)
			if err == nil {
				t.Fatalf("toTrekRate(%s) = %v, want an error", tt.rate, stored)
			}
			if !errors.Is(err, ErrNoExchangeRate) {
				t.Errorf("toTrekRate(%s) error = %v, want it to wrap ErrNoExchangeRate", tt.rate, err)
			}
			if stored != 0 {
				t.Errorf("toTrekRate(%s) = %v alongside its error, want 0", tt.rate, stored)
			}
		})
	}
}

func TestFromTrekRate_RefusesRatesTREKCouldNotHaveFrozen(t *testing.T) {
	tests := []struct {
		name   string
		stored float64
	}{
		{name: "an absent rate", stored: 0},
		{name: "a negative rate", stored: -1},
		{name: "not a number", stored: math.NaN()},
		{name: "infinite", stored: math.Inf(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := fromTrekRate(tt.stored)
			if err == nil {
				t.Fatalf("fromTrekRate(%v) = %s, want an error", tt.stored, rate)
			}
			if !errors.Is(err, ErrNoExchangeRate) {
				t.Errorf("fromTrekRate(%v) error = %v, want it to wrap ErrNoExchangeRate", tt.stored, err)
			}
			if !rate.IsZero() {
				t.Errorf("fromTrekRate(%v) = %s alongside its error, want zero", tt.stored, rate)
			}
		})
	}
}

func TestHasFrozenRate_TreatsOneAsNotFrozen(t *testing.T) {
	tests := []struct {
		name   string
		stored float64
		frozen bool
	}{
		{name: "one is the not-frozen sentinel", stored: 1, frozen: false},
		{name: "an absent rate", stored: 0, frozen: false},
		{name: "a negative rate", stored: -1, frozen: false},
		{name: "the R6 JPY rate", stored: 4.5455, frozen: true},
		{name: "a rate below one", stored: 0.25, frozen: true},
		{name: "a rate far above one", stored: 33.3333, frozen: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasFrozenRate(tt.stored); got != tt.frozen {
				t.Errorf("hasFrozenRate(%v) = %t, want %t", tt.stored, got, tt.frozen)
			}
		})
	}
}

func TestRateForWrite_TreatsTheTripCurrencyAsTheIdentity(t *testing.T) {
	tests := []struct {
		name string
		rate decimal.Decimal
	}{
		{name: "the rate a base-currency expense carries", rate: decimal.NewFromInt(1)},
		{name: "a rate left over from another currency", rate: decimal.NewFromFloat(4.5455)},
		{name: "no rate at all", rate: decimal.Zero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := rateForWrite(context.Background(), domain.CurrencyTWD, domain.CurrencyTWD, tt.rate, nil)
			if err != nil {
				t.Fatalf("rateForWrite() error = %v", err)
			}
			if stored != 1 {
				t.Errorf("rateForWrite(TWD, rate %s) = %v, want the identity 1", tt.rate, stored)
			}
		})
	}
}

func TestRateForRead_TreatsTheTripCurrencyAsTheIdentity(t *testing.T) {
	tests := []struct {
		name     string
		stored   float64
		currency domain.Currency
	}{
		{name: "a stored 1 on a base-currency row", stored: 1, currency: domain.CurrencyTWD},
		{name: "no rate stored at all", stored: 0, currency: domain.CurrencyTWD},
		{name: "a rate left over from another currency", stored: 4.5455, currency: domain.CurrencyTWD},
		{name: "a lower-case code, which TREK uppercases on read", stored: 1, currency: domain.Currency("twd")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := rateForRead(context.Background(), tt.stored, tt.currency, domain.CurrencyTWD, nil)
			if err != nil {
				t.Fatalf("rateForRead() error = %v", err)
			}
			if !rate.Equal(decimal.NewFromInt(1)) {
				t.Errorf("rateForRead(%v, %s) = %s, want the identity 1", tt.stored, tt.currency, rate)
			}
		})
	}
}

func TestRateForRead_QuotesAForeignRowWhoseStoredRateIsTheSentinel(t *testing.T) {
	for _, stored := range []float64{1, 0, -1} {
		t.Run("stored rate "+decimal.NewFromFloat(stored).String(), func(t *testing.T) {
			provider := &spyRateProvider{rate: jpyQuote()}

			rate, err := rateForRead(context.Background(), stored, domain.CurrencyJPY, domain.CurrencyTWD, provider)
			if err != nil {
				t.Fatalf("rateForRead() error = %v", err)
			}
			if !rate.Equal(jpyQuote()) {
				t.Errorf("rateForRead(stored %v) = %s, want the provider's quote %s", stored, rate, jpyQuote())
			}
			if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyJPY {
				t.Errorf("asked for %v, want exactly one quote for JPY", provider.asked)
			}
		})
	}
}

func TestRateForRead_RefusesAnUnconvertibleForeignRow(t *testing.T) {
	providerFailure := errors.New("no exchange rate data available")
	tests := []struct {
		name     string
		provider rateProvider
		cause    error
	}{
		{name: "the provider fails", provider: &spyRateProvider{err: providerFailure}, cause: providerFailure},
		{name: "no provider is configured", provider: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := rateForRead(context.Background(), 1, domain.CurrencyJPY, domain.CurrencyTWD, tt.provider)
			if err == nil {
				t.Fatalf("rateForRead() = %s, want an error rather than an unconverted amount", rate)
			}
			if !errors.Is(err, ErrNoExchangeRate) {
				t.Errorf("rateForRead() error = %v, want it to wrap ErrNoExchangeRate", err)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Errorf("rateForRead() error = %v, want the provider's own failure in the chain", err)
			}
			if !rate.IsZero() {
				t.Errorf("rateForRead() = %s alongside its error, want zero", rate)
			}
		})
	}
}

func TestRateForWrite_QuotesTheProviderWhenNoRateWasSupplied(t *testing.T) {
	provider := &spyRateProvider{rate: jpyQuote()}

	stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD, decimal.Zero, provider)
	if err != nil {
		t.Fatalf("rateForWrite() error = %v", err)
	}
	assertRateClose(t, decimal.NewFromFloat(stored), decimal.NewFromFloat(4.5455), 0.0001)
	if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyJPY {
		t.Errorf("asked for %v, want exactly one quote for JPY", provider.asked)
	}
}

func TestRateForWrite_ReplacesABareUnitRateOnAForeignExpense(t *testing.T) {
	provider := &spyRateProvider{rate: jpyQuote()}

	stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD, decimal.NewFromInt(1), provider)
	if err != nil {
		t.Fatalf("rateForWrite() error = %v", err)
	}
	assertRateClose(t, decimal.NewFromFloat(stored), decimal.NewFromFloat(4.5455), 0.0001)
	if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyJPY {
		t.Errorf("asked for %v, want exactly one quote for JPY", provider.asked)
	}
}

func TestRateForWrite_RefusesToWriteAnUnquotableForeignExpense(t *testing.T) {
	tests := []struct {
		name     string
		provider rateProvider
	}{
		{name: "the provider fails", provider: &spyRateProvider{err: errors.New("no exchange rate data available")}},
		{name: "the provider quotes zero", provider: stubRateProvider{rate: decimal.Zero}},
		{name: "no provider is configured", provider: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD, decimal.Zero, tt.provider)
			if err == nil {
				t.Fatalf("rateForWrite() = %v, want an error rather than an unquotable row", stored)
			}
			if !errors.Is(err, ErrNoExchangeRate) {
				t.Errorf("rateForWrite() error = %v, want it to wrap ErrNoExchangeRate", err)
			}
		})
	}
}

func TestRateConversion_RefusesAnUnknownTripCurrency(t *testing.T) {
	t.Run("write", func(t *testing.T) {
		stored, err := rateForWrite(context.Background(), domain.CurrencyJPY, domain.Currency(""), jpyQuote(), nil)
		if !errors.Is(err, ErrUnknownTripCurrency) {
			t.Errorf("rateForWrite() error = %v, want ErrUnknownTripCurrency", err)
		}
		if stored != 0 {
			t.Errorf("rateForWrite() = %v alongside its error, want 0", stored)
		}
	})

	t.Run("read", func(t *testing.T) {
		rate, err := rateForRead(context.Background(), 4.5455, domain.CurrencyJPY, domain.Currency(""), nil)
		if !errors.Is(err, ErrUnknownTripCurrency) {
			t.Errorf("rateForRead() error = %v, want ErrUnknownTripCurrency", err)
		}
		if !rate.IsZero() {
			t.Errorf("rateForRead() = %s alongside its error, want zero", rate)
		}
	})
}
