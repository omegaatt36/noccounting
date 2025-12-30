package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

const (
	notionAPIVersion = "2022-06-28"
	notionBaseURL    = "https://api.notion.com/v1"
)

// Client is a Notion API client that implements domain.AccountingRepo.
type Client struct {
	httpClient *http.Client
	token      string
	databaseID string
}

// Ensure Client implements domain.AccountingRepo at compile time.
var _ domain.AccountingRepo = (*Client)(nil)

// NewClient creates a new Notion client.
func NewClient(token, databaseID string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		databaseID: databaseID,
	}
}

// doRequest performs an HTTP request to Notion API.
func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, notionBaseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("notion API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// CreateExpense creates a new expense record in the database.
func (c *Client) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	properties := map[string]any{
		"name": map[string]any{
			"title": []map[string]any{
				{
					"text": map[string]any{
						"content": expense.Name,
					},
				},
			},
		},
		"price": map[string]any{
			"number": expense.Price,
		},
		"currency": map[string]any{
			"select": map[string]any{
				"name": string(expense.Currency),
			},
		},
		"category": map[string]any{
			"select": map[string]any{
				"name": string(expense.Category),
			},
		},
		"method": map[string]any{
			"select": map[string]any{
				"name": string(expense.Method),
			},
		},
		"shopped_at": map[string]any{
			"date": map[string]any{
				"start": expense.ShoppedAt.Format("2006-01-02"),
			},
		},
	}

	// Add paid_by if provided
	if expense.PaidByID != "" {
		properties["paid_by"] = map[string]any{
			"people": []map[string]any{
				{
					"id": expense.PaidByID,
				},
			},
		}
	}

	// Add exchange rate if provided
	if !expense.ExchangeRate.IsZero() {
		exRateFloat, _ := expense.ExchangeRate.Float64()
		properties["ex_rate"] = map[string]any{
			"number": exRateFloat,
		}
	}

	reqBody := map[string]any{
		"parent": map[string]any{
			"database_id": c.databaseID,
		},
		"properties": properties,
	}

	_, err := c.doRequest(ctx, http.MethodPost, "/pages", reqBody)
	if err != nil {
		return fmt.Errorf("failed to create expense: %w", err)
	}

	return nil
}

// QueryExpenses queries all expenses from the database.
func (c *Client) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	var allExpenses []domain.Expense
	var startCursor *string

	for {
		reqBody := map[string]any{
			"page_size": 100,
		}
		if startCursor != nil {
			reqBody["start_cursor"] = *startCursor
		}

		respBody, err := c.doRequest(ctx, http.MethodPost, "/databases/"+c.databaseID+"/query", reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to query expenses: %w", err)
		}

		var result queryResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		for _, page := range result.Results {
			expense, err := parseExpenseFromPage(page)
			if err != nil {
				// Skip invalid entries but log them
				continue
			}
			allExpenses = append(allExpenses, expense)
		}

		if !result.HasMore {
			break
		}
		startCursor = result.NextCursor
	}

	return allExpenses, nil
}

// GetExpenseSummary calculates the expense summary for splitting.
func (c *Client) GetExpenseSummary(ctx context.Context) (*domain.ExpenseSummary, error) {
	expenses, err := c.QueryExpenses(ctx)
	if err != nil {
		return nil, err
	}

	summary := &domain.ExpenseSummary{
		TotalByPayer: make(map[string]decimal.Decimal),
		ItemCount:    len(expenses),
		GrandTotal:   decimal.Zero,
	}

	for _, exp := range expenses {
		amountInTWD := exp.TotalInTWD()

		if existing, ok := summary.TotalByPayer[exp.PaidByID]; ok {
			summary.TotalByPayer[exp.PaidByID] = existing.Add(amountInTWD)
		} else {
			summary.TotalByPayer[exp.PaidByID] = amountInTWD
		}
		summary.GrandTotal = summary.GrandTotal.Add(amountInTWD)
	}

	return summary, nil
}

// queryResponse represents the Notion database query response.
type queryResponse struct {
	Results    []pageObject `json:"results"`
	HasMore    bool         `json:"has_more"`
	NextCursor *string      `json:"next_cursor"`
}

// pageObject represents a Notion page object.
type pageObject struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
}

// parseExpenseFromPage parses a domain.Expense from a Notion page object.
func parseExpenseFromPage(page pageObject) (domain.Expense, error) {
	expense := domain.Expense{
		ID:           page.ID,
		ExchangeRate: decimal.Zero,
	}

	props := page.Properties

	// Parse name (title)
	if name, ok := props["name"].(map[string]interface{}); ok {
		if title, ok := name["title"].([]interface{}); ok && len(title) > 0 {
			if textObj, ok := title[0].(map[string]interface{}); ok {
				if text, ok := textObj["text"].(map[string]interface{}); ok {
					expense.Name, _ = text["content"].(string)
				}
			}
		}
	}

	// Parse price (as uint64)
	if price, ok := props["price"].(map[string]interface{}); ok {
		if num, ok := price["number"].(float64); ok {
			expense.Price = uint64(num)
		}
	}

	// Parse currency
	if currency, ok := props["currency"].(map[string]interface{}); ok {
		if sel, ok := currency["select"].(map[string]interface{}); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Currency = domain.Currency(name)
			}
		}
	}

	// Parse exchange rate
	if exRate, ok := props["ex_rate"].(map[string]interface{}); ok {
		if num, ok := exRate["number"].(float64); ok {
			expense.ExchangeRate = decimal.NewFromFloat(num)
		}
	}

	// Parse category
	if category, ok := props["category"].(map[string]interface{}); ok {
		if sel, ok := category["select"].(map[string]interface{}); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Category = domain.Category(name)
			}
		}
	}

	// Parse method
	if method, ok := props["method"].(map[string]interface{}); ok {
		if sel, ok := method["select"].(map[string]interface{}); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Method = domain.PaymentMethod(name)
			}
		}
	}

	// Parse paid_by
	if paidBy, ok := props["paid_by"].(map[string]interface{}); ok {
		if people, ok := paidBy["people"].([]interface{}); ok && len(people) > 0 {
			if person, ok := people[0].(map[string]interface{}); ok {
				expense.PaidByID, _ = person["id"].(string)
			}
		}
	}

	// Parse shopped_at
	if shoppedAt, ok := props["shopped_at"].(map[string]interface{}); ok {
		if date, ok := shoppedAt["date"].(map[string]interface{}); ok && date != nil {
			if start, ok := date["start"].(string); ok {
				expense.ShoppedAt, _ = time.Parse("2006-01-02", start)
			}
		}
	}

	return expense, nil
}
