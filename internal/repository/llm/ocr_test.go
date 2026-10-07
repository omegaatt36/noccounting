package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

	_, err := analyzer.Analyze(context.Background(), []byte("not-an-image"))
	if err == nil || !strings.Contains(err.Error(), "failed to parse LLM response as receipt data") {
		t.Fatalf("err = %v", err)
	}
}

func TestOCRRequestCompressesImageAndDisablesReasoning(t *testing.T) {
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 1600, 800)), nil); err != nil {
		t.Fatal(err)
	}
	transport := &spyTransport{content: `{"items":[],"total":0}`}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
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
	if config.Width != 1024 || config.Height != 512 {
		t.Errorf("dimensions = %dx%d, want 1024x512", config.Width, config.Height)
	}
}
