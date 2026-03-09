package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

// Analyzer implements domain.ReceiptAnalyzer using an OpenAI-compatible Vision API.
type Analyzer struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

var _ domain.ReceiptAnalyzer = (*Analyzer)(nil)

// NewAnalyzer creates a new LLM receipt analyzer.
func NewAnalyzer(baseURL, apiKey, model string) *Analyzer {
	return &Analyzer{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
	}
}

const receiptPrompt = `Analyze this receipt image. Extract all items with their prices, categories, and Traditional Chinese translations.

Respond ONLY with valid JSON in this exact format:
{
  "summary": "店名或簡短描述",
  "items": [
    {"name": "ラーメン", "name_zh": "拉麵", "price": 1200, "category": "食"}
  ],
  "currency": "JPY",
  "total": 1200
}

"summary" should be a short, readable name for the receipt (e.g. "松屋 午餐", "全家便利商店", "唐吉訶德 伴手禮"). Use the store name if visible, otherwise describe the main purchase.
"name" is the item name as it appears on the receipt (original language).
"name_zh" is the Traditional Chinese (正體中文) translation of the item name. If the item name is already in Chinese, set "name_zh" to "".
Available categories: 食 (food/drinks), 住 (accommodation), 行 (transport), 購 (shopping/souvenirs), 樂 (entertainment/experiences), 雜 (misc/fees).
Currency must be either "TWD" or "JPY".
Price must be an integer (no decimals).`

// Analyze sends a receipt image to the Vision API and parses the response.
func (a *Analyzer) Analyze(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error) {
	b64Image := base64.StdEncoding.EncodeToString(imageData)

	reqBody := chatRequest{
		Model: a.model,
		Messages: []message{
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
		ResponseFormat: &responseFormat{
			Type: "json_object",
		},
		MaxTokens: 8192,
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

	var analysis domain.ReceiptAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as receipt data: %w\nRaw Content: %s", err, content)
	}

	return &analysis, nil
}

// OpenAI-compatible request/response types (private to this package)

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
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
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}
