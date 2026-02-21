package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/infrastructure/llm"
)

func TestAnalyzer_Analyze(t *testing.T) {
	mockResponse := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": `{"summary":"松屋 午餐","items":[{"name":"ラーメン","name_zh":"拉麵","price":1200,"category":"食"},{"name":"クッキーギフト","name_zh":"餅乾禮盒","price":800,"category":"購"}],"currency":"JPY","total":2000}`,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(server.URL, "test-key", "test-model")

	result, err := analyzer.Analyze(context.Background(), []byte("fake-image-data"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.Items[0].Name != "ラーメン" {
		t.Errorf("item[0].Name = %q, want %q", result.Items[0].Name, "ラーメン")
	}
	if result.Items[0].NameZH != "拉麵" {
		t.Errorf("item[0].NameZH = %q, want %q", result.Items[0].NameZH, "拉麵")
	}
	if result.Items[0].Price != 1200 {
		t.Errorf("item[0].Price = %d, want %d", result.Items[0].Price, 1200)
	}
	if result.Items[0].Category != domain.Category食 {
		t.Errorf("item[0].Category = %q, want %q", result.Items[0].Category, domain.Category食)
	}
	if result.Items[1].Name != "クッキーギフト" {
		t.Errorf("item[1].Name = %q, want %q", result.Items[1].Name, "クッキーギフト")
	}
	if result.Items[1].NameZH != "餅乾禮盒" {
		t.Errorf("item[1].NameZH = %q, want %q", result.Items[1].NameZH, "餅乾禮盒")
	}
	if result.Items[1].Category != domain.Category購 {
		t.Errorf("item[1].Category = %q, want %q", result.Items[1].Category, domain.Category購)
	}
	if result.Total != 2000 {
		t.Errorf("Total = %d, want %d", result.Total, 2000)
	}
	if result.Currency != domain.CurrencyJPY {
		t.Errorf("Currency = %q, want %q", result.Currency, domain.CurrencyJPY)
	}
}

func TestAnalyzer_Analyze_InvalidJSON(t *testing.T) {
	mockResponse := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": "I can't read this receipt clearly",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(server.URL, "test-key", "test-model")
	_, err := analyzer.Analyze(context.Background(), []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestAnalyzer_Analyze_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(server.URL, "test-key", "test-model")
	_, err := analyzer.Analyze(context.Background(), []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for API error")
	}
}

func TestAnalyzer_Analyze_EmptyChoices(t *testing.T) {
	mockResponse := map[string]any{
		"choices": []map[string]any{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(server.URL, "test-key", "test-model")
	_, err := analyzer.Analyze(context.Background(), []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
