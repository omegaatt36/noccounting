package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/util/imageutil"
)

type Analyzer struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

var _ expense.ReceiptAnalyzer = (*Analyzer)(nil)

func NewAnalyzer(baseURL, apiKey, model string) *Analyzer {
	return &Analyzer{
		httpClient: &http.Client{Timeout: 180 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
	}
}

const systemPrompt = `You are a receipt OCR parser.
Your ONLY job is to read the receipt image and output a single JSON object.
Do NOT think step by step. Do NOT explain. Do NOT output markdown code blocks.
Output raw, valid JSON and nothing else.`

const receiptPromptTemplate = `Analyze this receipt image. Extract all items with their prices, categories, payment method, and Traditional Chinese translations.

Respond ONLY with valid JSON in this exact format:
{
  "summary": "店名或簡短描述",
  "category": "food",
  "payment_method": "cash",
  "items": [
    {"name": "ラーメン", "name_zh": "拉麵", "price": 1200, "category": "food"}
  ],
  "currency": "JPY",
  "total": 1200
}

Rules:
- "summary" should be a short, readable name for the receipt (e.g. "松屋 午餐", "全家便利商店", "唐吉訶德 伴手禮"). Use the store name if visible, otherwise describe the main purchase.
- "category" is the overall primary category for this expense, must be exactly one of: %s. Use "groceries" for food bought to take away from a supermarket or convenience store, "food" for meals and drinks consumed out, "shopping" for goods and souvenirs, and "other" when nothing fits.
- "payment_method" must be exactly one of: cash, credit_card, ic_card, e_pay. If the receipt indicates credit card (クレジット, VISA, Master, etc.), use "credit_card". If IC card (Suica, PASMO, ICOCA, etc.), use "ic_card". If QR/barcode (PayPay, LINE Pay, etc.), use "e_pay". If cash (現金) or unspecified, default to "cash".
- "name" is the item name as it appears on the receipt (original language).
- "name_zh" is the Traditional Chinese (正體中文) translation of the item name. If the item name is already in Chinese, set "name_zh" to "".
- Each item's "category" must be exactly one of: %s.
- Currency must be either "TWD" or "JPY".
- Price must be a positive integer (>= 0, no decimals).
- Do NOT include discount items, tax adjustments, service fees, or set-deal breakdowns (e.g. セット値引き, discount, tax, etc.). Only list the actual goods or services purchased.
- When a receipt shows a set meal with sub-items and discounts, list the set as a single item with its final set price, or list only the main items with their final prices after discount. Do NOT include negative prices.`

var receiptPrompt = fmt.Sprintf(receiptPromptTemplate, strings.Join(domain.CategoryNames(), ", "), strings.Join(domain.CategoryNames(), ", "))

func (a *Analyzer) Analyze(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error) {
	resized, err := imageutil.ResizeAndCompress(imageData, 1024, 85)
	if err != nil {
		slog.Warn("Failed to resize image, using original", "error", err)
		resized = imageData
	}
	b64Image := base64.StdEncoding.EncodeToString(resized)
	slog.Debug("Prepared image for LLM", "original_bytes", len(imageData), "resized_bytes", len(resized))

	const maxRetries = 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			sleepDuration := time.Duration(1<<attempt) * time.Second
			slog.Info("Retrying LLM request", "attempt", attempt, "sleep", sleepDuration)
			select {
			case <-time.After(sleepDuration):
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			}
		}

		analysis, err := a.doAnalyze(ctx, b64Image)
		if err == nil {
			return analysis, nil
		}
		lastErr = err

		// Don't retry on client errors (4xx) unless it's 429 Too Many Requests
		if strings.Contains(err.Error(), "LLM API error (status 4") && !strings.Contains(err.Error(), "429") {
			break
		}
	}

	return nil, fmt.Errorf("failed to call LLM API after %d retries: %w", maxRetries, lastErr)
}

var jsonBlockRE = regexp.MustCompile(`(?s)\{.*\}`)

func (a *Analyzer) doAnalyze(ctx context.Context, b64Image string) (*domain.ReceiptAnalysis, error) {
	reqID := uuid.New().String()
	reqBody := chatRequest{
		Model: a.model,
		Messages: []message{
			{Role: "system", Content: []contentPart{{Type: "text", Text: systemPrompt}}},
			{
				Role: "user",
				Content: []contentPart{
					{Type: "text", Text: receiptPrompt},
					{
						Type: "image_url",
						ImageURL: &imageURL{
							URL: "data:image/jpeg;base64," + b64Image,
						},
					},
				},
			},
		},
		ResponseFormat:  &responseFormat{Type: "json_object"},
		MaxTokens:       16384,
		Temperature:     new(0.0),
		ReasoningEffort: "low",
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-Request-ID", reqID)

	slog.Debug("Calling LLM API", "request_id", reqID, "url", a.baseURL+"/chat/completions")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	content := chatResp.Choices[0].Message.Content
	var analysis receiptResponse
	parseErr := json.Unmarshal([]byte(content), &analysis)
	if parseErr != nil {
		if block := jsonBlockRE.FindString(content); block != "" {
			analysis = receiptResponse{}
			parseErr = json.Unmarshal([]byte(block), &analysis)
		}
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse LLM response as receipt data: %w\nRaw Content: %s", parseErr, content)
		}
	}

	items := make([]domain.ReceiptItem, 0, len(analysis.Items))
	for _, item := range analysis.Items {
		items = append(items, domain.ReceiptItem{
			Name: item.Name, NameZH: item.NameZH, Price: item.Price, Category: item.Category,
		})
	}

	category := analysis.Category
	if !category.IsValid() {
		if len(items) > 0 && items[0].Category.IsValid() {
			category = items[0].Category
		} else {
			category = domain.CategoryFood
		}
	}

	paymentMethod := analysis.PaymentMethod
	if !paymentMethod.IsValid() {
		paymentMethod = domain.PaymentMethodCash
	}

	return &domain.ReceiptAnalysis{
		Summary:       analysis.Summary,
		Items:         items,
		Currency:      analysis.Currency,
		Total:         analysis.Total,
		Category:      category,
		PaymentMethod: paymentMethod,
	}, nil
}

type receiptResponse struct {
	Summary       string                `json:"summary"`
	Category      domain.Category       `json:"category"`
	PaymentMethod domain.PaymentMethod  `json:"payment_method"`
	Items         []receiptItemResponse `json:"items"`
	Currency      domain.Currency       `json:"currency"`
	Total         uint64                `json:"total"`
}

type receiptItemResponse struct {
	Name     string          `json:"name"`
	NameZH   string          `json:"name_zh"`
	Price    int64           `json:"price"`
	Category domain.Category `json:"category"`
}

type chatRequest struct {
	Model           string          `json:"model"`
	Messages        []message       `json:"messages"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
}

type choice struct {
	Message choiceMessage `json:"message"`
}

type choiceMessage struct {
	Content string `json:"content"`
}
