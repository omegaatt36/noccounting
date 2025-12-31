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

func buildProperties(expense *domain.Expense) map[string]interface{} {
	properties := map[string]interface{}{
		"name": TitleProperty{
			Title: []RichText{
				{Text: Text{Content: expense.Name}},
			},
		},
		"price": NumberProperty{
			Number: float64(expense.Price),
		},
		"currency": SelectProperty{
			Select: SelectOption{Name: string(expense.Currency)},
		},
		"category": SelectProperty{
			Select: SelectOption{Name: string(expense.Category)},
		},
		"method": SelectProperty{
			Select: SelectOption{Name: string(expense.Method)},
		},
		"shopped_at": DateProperty{
			Date: DateInfo{Start: expense.ShoppedAt.Format("2006-01-02")},
		},
	}

	// Add paid_by if provided
	if expense.PaidByID != "" {
		properties["paid_by"] = PeopleProperty{
			People: []Person{{ID: expense.PaidByID}},
		}
	}

	// Add exchange rate if provided
	if !expense.ExchangeRate.IsZero() {
		exRateFloat, _ := expense.ExchangeRate.Float64()
		properties["ex_rate"] = NumberProperty{
			Number: exRateFloat,
		}
	}

	return properties
}

// CreateExpense creates a new expense record in the database.
func (c *Client) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	reqBody := Page{
		Parent:     Parent{DatabaseID: c.databaseID},
		Properties: buildProperties(expense),
	}

	_, err := c.doRequest(ctx, http.MethodPost, "/pages", reqBody)
	if err != nil {
		return fmt.Errorf("failed to create expense: %w", err)
	}

	return nil
}

// QueryExpenses queries all expenses from the database.
func (c *Client) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	return c.QueryExpensesWithFilter(ctx, domain.ExpenseFilter{})
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

// QueryExpensesWithFilter queries expenses with the given filter.
func (c *Client) QueryExpensesWithFilter(ctx context.Context, filter domain.ExpenseFilter) ([]domain.Expense, error) {
	var allExpenses []domain.Expense
	var startCursor string

	// Build filter conditions for Notion API
	var filterConditions []Filter

	if filter.DateFrom != nil {
		filterConditions = append(filterConditions, Filter{
			Property: "shopped_at",
			Date: &DateFilter{
				OnOrAfter: filter.DateFrom.Format("2006-01-02"),
			},
		})
	}

	if filter.DateTo != nil {
		filterConditions = append(filterConditions, Filter{
			Property: "shopped_at",
			Date: &DateFilter{
				OnOrBefore: filter.DateTo.Format("2006-01-02"),
			},
		})
	}

	if filter.PaidByID != nil {
		filterConditions = append(filterConditions, Filter{
			Property: "paid_by",
			People: &PeopleFilter{
				Contains: *filter.PaidByID,
			},
		})
	}

	for {
		reqBody := QueryDatabaseRequest{
			PageSize: 100,
			Sorts: []Sort{
				{
					Property:  "shopped_at",
					Direction: "descending",
				},
			},
		}

		if startCursor != "" {
			reqBody.StartCursor = startCursor
		}

		// Apply filter if conditions exist
		if len(filterConditions) > 0 {
			if len(filterConditions) == 1 {
				reqBody.Filter = filterConditions[0]
			} else {
				reqBody.Filter = map[string]interface{}{
					"and": filterConditions,
				}
			}
		}

		respBody, err := c.doRequest(ctx, http.MethodPost, "/databases/"+c.databaseID+"/query", reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to query expenses: %w", err)
		}

		var result QueryResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		for _, page := range result.Results {
			expense, err := parseExpenseFromPage(page)
			if err != nil {
				continue
			}
			allExpenses = append(allExpenses, expense)

			// Check limit
			if filter.Limit != nil && len(allExpenses) >= *filter.Limit {
				return allExpenses, nil
			}
		}

		if !result.HasMore || result.NextCursor == nil {
			break
		}
		startCursor = *result.NextCursor
	}

	return allExpenses, nil
}

// UpdateExpense updates an existing expense record.
func (c *Client) UpdateExpense(ctx context.Context, expense *domain.Expense) error {
	if expense.ID == "" {
		return fmt.Errorf("expense ID is required for update")
	}

	reqBody := UpdatePageRequest{
		Properties: buildProperties(expense),
	}

	_, err := c.doRequest(ctx, http.MethodPatch, "/pages/"+expense.ID, reqBody)
	if err != nil {
		return fmt.Errorf("failed to update expense: %w", err)
	}

	return nil
}

// DeleteExpense archives an expense record (Notion doesn't truly delete).
func (c *Client) DeleteExpense(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("expense ID is required for deletion")
	}

	reqBody := UpdatePageRequest{
		Archived: true,
	}

	_, err := c.doRequest(ctx, http.MethodPatch, "/pages/"+id, reqBody)
	if err != nil {
		return fmt.Errorf("failed to delete expense: %w", err)
	}

	return nil
}

// parseExpenseFromPage parses a domain.Expense from a Notion page object.
func parseExpenseFromPage(page PageObject) (domain.Expense, error) {
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
