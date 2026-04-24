package exchangerate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

const (
	finMindBaseURL = "https://api.finmindtrade.com/api/v4/data"
)

// FinMindClient fetches exchange rates from FinMind API.
type FinMindClient struct {
	httpClient *http.Client
	baseURL    string
}

// Ensure FinMindClient implements expense.ExchangeRateFetcher at compile time.
var _ expense.ExchangeRateFetcher = (*FinMindClient)(nil)

// NewFinMindClient creates a new FinMind client.
func NewFinMindClient() *FinMindClient {
	return &FinMindClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    finMindBaseURL,
	}
}

// NewFinMindClientWithBaseURL creates a new FinMind client with custom base URL.
func NewFinMindClientWithBaseURL(baseURL string) *FinMindClient {
	return &FinMindClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
	}
}

// finMindResponse represents the FinMind API response structure.
type finMindResponse struct {
	Status int              `json:"status"`
	Data   []finMindRateRow `json:"data"`
}

// finMindRateRow represents a single exchange rate record.
type finMindRateRow struct {
	Date     string  `json:"date"`
	Currency string  `json:"currency"`
	CashBuy  float64 `json:"cash_buy"`
	CashSell float64 `json:"cash_sell"`
	SpotBuy  float64 `json:"spot_buy"`
	SpotSell float64 `json:"spot_sell"`
}

// GetRate fetches the exchange rate for converting from source currency to TWD.
func (c *FinMindClient) GetRate(ctx context.Context, sourceCurrency domain.Currency) (decimal.Decimal, error) {
	if sourceCurrency == domain.CurrencyTWD {
		return decimal.NewFromInt(1), nil
	}

	if sourceCurrency != domain.CurrencyJPY {
		return decimal.Zero, fmt.Errorf("unsupported currency: %s", sourceCurrency)
	}

	// Query yesterday's data to ensure availability
	yesterday := time.Now().AddDate(0, 0, -1)
	startDate := yesterday.Format("2006-01-02")

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

	// Use the latest available rate (cash_sell rate for buying JPY with TWD)
	latestRate := result.Data[len(result.Data)-1]
	return decimal.NewFromFloat(latestRate.CashSell), nil
}
