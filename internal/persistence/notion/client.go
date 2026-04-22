package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

const (
	notionAPIVersion           = "2022-06-28"
	notionAPIVersionFileUpload = "2025-09-03"
	notionBaseURL              = "https://api.notion.com/v1"
)

// Client is a Notion API client that implements domain.AccountingRepo.
type Client struct {
	httpClient *http.Client
	token      string
	databaseID string
	baseURL    string
}

// Ensure Client implements domain.AccountingRepo at compile time.
var _ domain.AccountingRepo = (*Client)(nil)

// NewClient creates a new Notion client.
func NewClient(token, databaseID string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		databaseID: databaseID,
		baseURL:    notionBaseURL,
	}
}

// NewClientWithBaseURL creates a new Notion client with a custom base URL (for testing).
func NewClientWithBaseURL(token, databaseID, baseURL string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		databaseID: databaseID,
		baseURL:    baseURL,
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

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
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

func buildProperties(expense *domain.Expense) map[string]any {
	properties := map[string]any{
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

	// Add receipt photo if provided
	if expense.ReceiptURL != "" {
		properties["receipt"] = FilesProperty{
			Files: []FileReference{
				{
					Type:       "file_upload",
					Name:       "receipt.jpg",
					FileUpload: &FileUploadRef{ID: expense.ReceiptURL},
				},
			},
		}
	}

	return properties
}

// buildReceiptBlocks generates Notion page body blocks from receipt items.
// Returns nil if there are no items (omitted from JSON via omitempty).
func buildReceiptBlocks(expense *domain.Expense) []Block {
	if len(expense.ReceiptItems) == 0 {
		return nil
	}

	blocks := []Block{
		{
			Object: "block",
			Type:   "heading_3",
			Heading3: &RichTextBlock{
				RichText: []RichText{{Text: Text{Content: "收據明細"}}},
			},
		},
	}

	for _, item := range expense.ReceiptItems {
		line := fmt.Sprintf("%s %s %d %s",
			item.Category.Emoji(), item.DisplayName(), item.Price, expense.Currency)
		blocks = append(blocks, Block{
			Object: "block",
			Type:   "bulleted_list_item",
			BulletedListItem: &RichTextBlock{
				RichText: []RichText{{Text: Text{Content: line}}},
			},
		})
	}

	return blocks
}

// CreateExpense creates a new expense record in the database.
func (c *Client) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	properties := buildProperties(expense)
	reqBody := Page{
		Parent:     Parent{DatabaseID: c.databaseID},
		Properties: properties,
		Children:   buildReceiptBlocks(expense),
	}

	_, err := c.doRequest(ctx, http.MethodPost, "/pages", reqBody)
	if err != nil && expense.ReceiptURL != "" {
		// Retry without receipt property (database may not have it configured)
		slog.Warn("CreateExpense failed with receipt, retrying without", "error", err)
		delete(properties, "receipt")
		reqBody.Properties = properties
		_, retryErr := c.doRequest(ctx, http.MethodPost, "/pages", reqBody)
		if retryErr == nil {
			return nil
		}
		err = retryErr
	}
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
				reqBody.Filter = map[string]any{
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

// UploadFile uploads a file to Notion and returns the file upload ID.
// Uses Notion File Upload API (requires Notion-Version 2025-09-03).
func (c *Client) UploadFile(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Step 1: Create file upload object (POST /v1/file_uploads)
	createBody, err := json.Marshal(struct{}{})
	if err != nil {
		return "", fmt.Errorf("failed to marshal create request: %w", err)
	}

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/file_uploads", bytes.NewReader(createBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	createReq.Header.Set("Authorization", "Bearer "+c.token)
	createReq.Header.Set("Notion-Version", notionAPIVersionFileUpload)
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := c.httpClient.Do(createReq)
	if err != nil {
		return "", fmt.Errorf("failed to create file upload: %w", err)
	}
	defer createResp.Body.Close()

	createRespBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read create response: %w", err)
	}

	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to create file upload (status %d): %s", createResp.StatusCode, string(createRespBody))
	}

	var uploadObj FileUploadResponse
	if err := json.Unmarshal(createRespBody, &uploadObj); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	// Step 2: Send file content (POST /v1/file_uploads/{id}/send)
	// Detect content type from file header
	headerBuf := make([]byte, 512)
	n, _ := f.Read(headerBuf)
	contentType := http.DetectContentType(headerBuf[:n])
	if _, err := f.Seek(0, 0); err != nil {
		return "", fmt.Errorf("failed to seek file: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(filePath))},
		"Content-Type":        {contentType},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("failed to write file content: %w", err)
	}
	writer.Close()

	sendReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/file_uploads/"+uploadObj.ID+"/send", &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create send request: %w", err)
	}
	sendReq.Header.Set("Authorization", "Bearer "+c.token)
	sendReq.Header.Set("Notion-Version", notionAPIVersionFileUpload)
	sendReq.Header.Set("Content-Type", writer.FormDataContentType())

	sendResp, err := c.httpClient.Do(sendReq)
	if err != nil {
		return "", fmt.Errorf("failed to send file: %w", err)
	}
	defer sendResp.Body.Close()

	if sendResp.StatusCode < 200 || sendResp.StatusCode >= 300 {
		body, _ := io.ReadAll(sendResp.Body)
		return "", fmt.Errorf("file send failed (status %d): %s", sendResp.StatusCode, string(body))
	}

	return uploadObj.ID, nil
}

// parseExpenseFromPage parses a domain.Expense from a Notion page object.
func parseExpenseFromPage(page PageObject) (domain.Expense, error) {
	expense := domain.Expense{
		ID:           page.ID,
		ExchangeRate: decimal.Zero,
	}

	props := page.Properties

	// Parse name (title)
	if name, ok := props["name"].(map[string]any); ok {
		if title, ok := name["title"].([]any); ok && len(title) > 0 {
			if textObj, ok := title[0].(map[string]any); ok {
				if text, ok := textObj["text"].(map[string]any); ok {
					expense.Name, _ = text["content"].(string)
				}
			}
		}
	}

	// Parse price (as uint64)
	if price, ok := props["price"].(map[string]any); ok {
		if num, ok := price["number"].(float64); ok {
			expense.Price = uint64(num)
		}
	}

	// Parse currency
	if currency, ok := props["currency"].(map[string]any); ok {
		if sel, ok := currency["select"].(map[string]any); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Currency = domain.Currency(name)
			}
		}
	}

	// Parse exchange rate
	if exRate, ok := props["ex_rate"].(map[string]any); ok {
		if num, ok := exRate["number"].(float64); ok {
			expense.ExchangeRate = decimal.NewFromFloat(num)
		}
	}

	// Parse category
	if category, ok := props["category"].(map[string]any); ok {
		if sel, ok := category["select"].(map[string]any); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Category = domain.Category(name)
			}
		}
	}

	// Parse method
	if method, ok := props["method"].(map[string]any); ok {
		if sel, ok := method["select"].(map[string]any); ok && sel != nil {
			if name, ok := sel["name"].(string); ok {
				expense.Method = domain.PaymentMethod(name)
			}
		}
	}

	// Parse paid_by
	if paidBy, ok := props["paid_by"].(map[string]any); ok {
		if people, ok := paidBy["people"].([]any); ok && len(people) > 0 {
			if person, ok := people[0].(map[string]any); ok {
				expense.PaidByID, _ = person["id"].(string)
			}
		}
	}

	// Parse shopped_at
	if shoppedAt, ok := props["shopped_at"].(map[string]any); ok {
		if date, ok := shoppedAt["date"].(map[string]any); ok && date != nil {
			if start, ok := date["start"].(string); ok {
				expense.ShoppedAt, _ = time.Parse("2006-01-02", start)
			}
		}
	}

	return expense, nil
}
