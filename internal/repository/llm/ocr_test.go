package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

type spyTransport struct {
	request chatRequest
	content string
}

func (s *spyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := json.NewDecoder(req.Body).Decode(&s.request); err != nil {
		return nil, err
	}
	resp := chatResponse{
		Choices: []choice{{Message: choiceMessage{Content: s.content}}},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(raw)),
		Header:     make(http.Header),
	}, nil
}

func TestOCRExtractsJSONAndPreservesSignedPrices(t *testing.T) {
	transport := &spyTransport{content: "```json\n{\"summary\":\"FamilyMart\",\"currency\":\"JPY\",\"total\":420,\"items\":[{\"name\":\"Onigiri\",\"price\":150,\"category\":\"food\"},{\"name\":\"Tea\",\"price\":-50,\"category\":\"food\"},{\"name\":\"Total\",\"price\":420,\"category\":\"food\"}]}\n```"}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport

	analysis, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Summary != "FamilyMart" || analysis.Currency != domain.CurrencyJPY || analysis.Total != 420 {
		t.Fatalf("analysis = %+v", analysis)
	}
	if len(analysis.Items) != 3 || analysis.Items[1].Price != -50 {
		t.Fatalf("items = %+v", analysis.Items)
	}
}

func TestOCRParseErrorWrapsSyntaxError(t *testing.T) {
	transport := &spyTransport{content: "not json at all"}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport
	analyzer.retryBase = time.Millisecond

	_, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
	if err == nil || !strings.Contains(err.Error(), "failed to parse LLM response as receipt data") {
		t.Fatalf("err = %v", err)
	}
}

func TestOCRRequestCompressesImageAndDisablesReasoning(t *testing.T) {
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 3000, 1500)), nil); err != nil {
		t.Fatal(err)
	}
	transport := &spyTransport{content: `{"items":[],"currency":"JPY","total":0}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model").WithReasoningEffort("low")
	analyzer.httpClient.Transport = transport
	if _, err := analyzer.Analyze(context.Background(), input.Bytes()); err != nil {
		t.Fatal(err)
	}
	if analyzer.httpClient.Timeout != 180*time.Second {
		t.Errorf("timeout = %v", analyzer.httpClient.Timeout)
	}
	raw, err := json.Marshal(transport.request)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if request["temperature"] != float64(0) || request["reasoning_effort"] != "low" || request["max_tokens"] != float64(16384) {
		t.Errorf("OCR request parameters: temperature=%v reasoning_effort=%v max_tokens=%v", request["temperature"], request["reasoning_effort"], request["max_tokens"])
	}
	messages := transport.request.Messages
	if len(messages) != 2 || messages[0].Role != "system" {
		t.Fatalf("messages: %+v", messages)
	}
	if !strings.Contains(messages[1].Content[0].Text, "groceries") {
		t.Error("missing current category prompt")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(messages[1].Content[1].ImageURL.URL, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 2*receiptShortEdge || config.Height != receiptShortEdge {
		t.Errorf("dimensions = %dx%d, want the shorter side at %d", config.Width, config.Height, receiptShortEdge)
	}
}

func TestOCRRequestOmitsReasoningEffortUnlessAsked(t *testing.T) {
	transport := &spyTransport{content: `{"items":[],"currency":"JPY","total":0}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport
	if _, err := analyzer.Analyze(context.Background(), []byte("not-an-image")); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(transport.request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "reasoning_effort") {
		t.Errorf("request %s names a reasoning effort nobody configured", raw)
	}
}

func TestOCRToleratesWordsOutsideTheLists(t *testing.T) {
	transport := &spyTransport{content: `{"summary":"x","category":"Drinks","payment_method":"Credit Card","currency":"jpy","total":1280.0,
		"items":[{"name":"a","price":300.0,"category":"drinks"},{"name":"b","price":200,"category":""},{"name":"c","price":500,"category":"Food"}]}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport

	analysis, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Currency != domain.CurrencyJPY || analysis.Total != 1280 {
		t.Errorf("currency=%s total=%d, want JPY and 1280 from \"jpy\" and 1280.0", analysis.Currency, analysis.Total)
	}
	if analysis.PaymentMethod != domain.PaymentMethodCreditCard {
		t.Errorf("payment method = %q, want \"Credit Card\" read as credit_card", analysis.PaymentMethod)
	}
	if analysis.Category != domain.CategoryFood {
		t.Errorf("category = %q, want the first known item category", analysis.Category)
	}
	if analysis.Items[0].Category != "" || analysis.Items[1].Category != "" || analysis.Items[2].Category != domain.CategoryFood {
		t.Errorf("item categories = %q %q %q, want unknown ones left empty and \"Food\" read as food",
			analysis.Items[0].Category, analysis.Items[1].Category, analysis.Items[2].Category)
	}
	if analysis.Items[0].Price != 300 {
		t.Errorf("price = %d, want 300 from 300.0", analysis.Items[0].Price)
	}
}

func TestOCRFallsBackWhenPaymentMethodIsUnknown(t *testing.T) {
	transport := &spyTransport{content: `{"summary":"x","payment_method":"barter","currency":"TWD","total":10,"items":[]}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport

	analysis, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.PaymentMethod != domain.PaymentMethodCash || analysis.Category != domain.CategoryFood {
		t.Errorf("method=%q category=%q, want cash and food as the fallbacks", analysis.PaymentMethod, analysis.Category)
	}
}

type countingTransport struct {
	calls   int
	status  int
	content string
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	raw, err := json.Marshal(chatResponse{Choices: []choice{{Message: choiceMessage{Content: c.content}}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
}

func TestOCRDoesNotAskAgainAboutWhatAskingCannotFix(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		content string
		want    error
	}{
		"currency noccounting does not account in": {http.StatusOK, `{"currency":"USD","total":5,"items":[]}`, domain.ErrUnsupportedCurrency},
		"no currency":                  {http.StatusOK, `{"total":5,"items":[]}`, domain.ErrUnsupportedCurrency},
		"request the endpoint refuses": {http.StatusBadRequest, `{}`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			transport := &countingTransport{status: tc.status, content: tc.content}
			analyzer := NewAnalyzer("https://example.test", "key", "model")
			analyzer.httpClient.Transport = transport
			analyzer.retryBase = time.Millisecond

			_, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want it to be %v", err, tc.want)
			}
			if transport.calls != 1 {
				t.Errorf("the model was asked %d times, want once", transport.calls)
			}
		})
	}
}

func TestOCRAsksAgainAfterAServerError(t *testing.T) {
	transport := &countingTransport{status: http.StatusBadGateway, content: `{}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport
	analyzer.retryBase = time.Millisecond

	if _, err := analyzer.Analyze(context.Background(), []byte("not-an-image")); err == nil {
		t.Fatal("want an error")
	}
	if transport.calls != 3 {
		t.Errorf("the model was asked %d times, want the first try and 2 retries", transport.calls)
	}
}
