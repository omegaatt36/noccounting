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

type FinMindClient struct {
	httpClient *http.Client
	baseURL    string
}

var _ expense.ExchangeRateFetcher = (*FinMindClient)(nil)

func NewFinMindClient() *FinMindClient {
	return &FinMindClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    finMindBaseURL,
	}
}

func NewFinMindClientWithBaseURL(baseURL string) *FinMindClient {
	return &FinMindClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
	}
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

// fetchTWDPerJPY is the board's latest cash selling rate: what a traveller
// pays in TWD for one JPY of cash.
func (c *FinMindClient) fetchTWDPerJPY(ctx context.Context) (decimal.Decimal, error) {
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

	latestRate := result.Data[len(result.Data)-1]
	if latestRate.CashSell <= 0 {
		return decimal.Zero, fmt.Errorf("no exchange rate data available")
	}
	return decimal.NewFromFloat(latestRate.CashSell), nil
}
