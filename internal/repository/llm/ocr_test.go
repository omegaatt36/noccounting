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
)

type spyTransport struct {
	request chatRequest
	content string
}

func (s *spyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := json.NewDecoder(r.Body).Decode(&s.request); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": s.content}}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
}

func TestOCRExtractsJSONAndPreservesSignedPrices(t *testing.T) {
	transport := &spyTransport{content: "```json\n" + `{"summary":"receipt","items":[{"name":"discount","price":-20,"category":"groceries"}],"currency":"JPY","total":80}` + "\n```"}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport
	result, err := analyzer.doAnalyze(context.Background(), "image")
	if err != nil {
		t.Fatalf("doAnalyze: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Price != -20 || string(result.Items[0].Category) != "groceries" {
		t.Fatalf("signed items: %+v", result.Items)
	}
}

func TestOCRParseErrorWrapsSyntaxError(t *testing.T) {
	transport := &spyTransport{content: "reasoning {broken json}"}
	analyzer := NewAnalyzer("https://example.test", "key", "model")
	analyzer.httpClient.Transport = transport
	_, err := analyzer.doAnalyze(context.Background(), "image")
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("expected wrapped syntax error, got %v", err)
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
	if config.Width != 512 || config.Height != 256 {
		t.Errorf("dimensions = %dx%d", config.Width, config.Height)
	}
}
