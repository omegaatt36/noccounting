package exchangerate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

const (
	finMindBaseURL  = "https://api.finmindtrade.com/api/v4/data"
	defaultCacheTTL = 1 * time.Hour
	// defaultMaxStale is how old a rate may be and still stand in when FinMind is down.
	defaultMaxStale = 7 * 24 * time.Hour
)

type FinMindClient struct {
	httpClient *http.Client
	baseURL    string
	cacheTTL   time.Duration
	maxStale   time.Duration

	mu         sync.RWMutex
	cachedRate decimal.Decimal
	cachedAt   time.Time
}

var _ expense.ExchangeRateFetcher = (*FinMindClient)(nil)

func NewFinMindClient() *FinMindClient {
	return NewFinMindClientWithBaseURL(finMindBaseURL)
}

func NewFinMindClientWithBaseURL(baseURL string) *FinMindClient {
	return &FinMindClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
		cacheTTL:   defaultCacheTTL,
		maxStale:   defaultMaxStale,
	}
}

func (c *FinMindClient) SetCacheTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheTTL = ttl
}

func (c *FinMindClient) SetMaxStale(age time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxStale = age
}

type finMindResponse struct {
	Status int              `json:"status"`
	Data   []finMindRateRow `json:"data"`
}
type finMindRateRow struct {
	Date     string  `json:"date"`
	Currency string  `json:"currency"`
	CashBuy  float64 `json:"cash_buy"`
	CashSell float64 `json:"cash_sell"`
	SpotBuy  float64 `json:"spot_buy"`
	SpotSell float64 `json:"spot_sell"`
}

// FinMind quotes TWD per JPY; the reverse pair uses its reciprocal.
func (c *FinMindClient) GetRate(ctx context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	if source == target {
		return decimal.NewFromInt(1), nil
	}

	switch {
	case source == domain.CurrencyJPY && target == domain.CurrencyTWD:
		return c.fetchTWDPerJPY(ctx)
	case source == domain.CurrencyTWD && target == domain.CurrencyJPY:
		twdPerJPY, err := c.fetchTWDPerJPY(ctx)
		if err != nil {
			return decimal.Zero, err
		}
		return decimal.NewFromInt(1).DivRound(twdPerJPY, 6), nil
	default:
		return decimal.Zero, fmt.Errorf("unsupported currency pair: %s to %s", source, target)
	}
}

// fetchTWDPerJPY is the board's latest cash selling rate; when it cannot be read, the last rate stands in for a while.
func (c *FinMindClient) fetchTWDPerJPY(ctx context.Context) (decimal.Decimal, error) {
	c.mu.RLock()
	cached, cachedAt := c.cachedRate, c.cachedAt
	ttl, maxStale := c.cacheTTL, c.maxStale
	c.mu.RUnlock()

	if !cached.IsZero() && time.Since(cachedAt) < ttl {
		return cached, nil
	}

	rate, err := c.requestTWDPerJPY(ctx)
	if err != nil {
		if !cached.IsZero() && time.Since(cachedAt) < maxStale {
			slog.Warn("could not fetch the exchange rate; using the last one", "age", time.Since(cachedAt).Round(time.Minute), "error", err)
			return cached, nil
		}
		return decimal.Zero, err
	}
	return rate, nil
}

func (c *FinMindClient) requestTWDPerJPY(ctx context.Context) (decimal.Decimal, error) {
	// Query past 7 days to tolerate weekends and market holidays.
	startDate := time.Now().AddDate(0, 0, -7).Format("2006-01-02")

	url := fmt.Sprintf("%s?dataset=TaiwanExchangeRate&data_id=JPY&start_date=%s", c.baseURL, startDate)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to fetch exchange rate: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to read response: %w", err)
	}

	var result finMindResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return decimal.Zero, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Status != 200 || len(result.Data) == 0 {
		return decimal.Zero, fmt.Errorf("no exchange rate data available")
	}

	latestRate := result.Data[len(result.Data)-1]
	if latestRate.CashSell <= 0 {
		return decimal.Zero, fmt.Errorf("no exchange rate data available")
	}

	rate := decimal.NewFromFloat(latestRate.CashSell)
	c.mu.Lock()
	c.cachedRate = rate
	c.cachedAt = time.Now()
	c.mu.Unlock()

	return rate, nil
}
