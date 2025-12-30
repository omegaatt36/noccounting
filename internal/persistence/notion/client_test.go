package notion_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/persistence/notion"
)

func TestClient_CreateExpense(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "page-123"}`))
	}))
	defer server.Close()

	client := notion.NewClientWithBaseURL("test-token", "db-123", server.URL)

	expense := &domain.Expense{
		Name:         "拉麵",
		Price:        1200,
		Currency:     domain.CurrencyJPY,
		ExchangeRate: decimal.NewFromFloat(0.22),
		Category:     domain.Category食,
		Method:       domain.PaymentMethodCash,
		PaidByID:     "user-abc",
		ShoppedAt:    time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC),
	}

	err := client.CreateExpense(context.Background(), expense)
	if err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
}

func TestClient_QueryExpenses(t *testing.T) {
	response := map[string]any{
		"results": []map[string]any{
			{
				"id": "page-1",
				"properties": map[string]any{
					"name": map[string]any{
						"title": []map[string]any{
							{"text": map[string]any{"content": "拉麵"}},
						},
					},
					"price":    map[string]any{"number": float64(1200)},
					"currency": map[string]any{"select": map[string]any{"name": "JPY"}},
					"category": map[string]any{"select": map[string]any{"name": "食"}},
					"method":   map[string]any{"select": map[string]any{"name": "cash"}},
					"ex_rate":  map[string]any{"number": float64(0.22)},
					"shopped_at": map[string]any{
						"date": map[string]any{"start": "2026-02-21"},
					},
					"paid_by": map[string]any{
						"people": []map[string]any{
							{"id": "user-abc"},
						},
					},
				},
			},
		},
		"has_more":    false,
		"next_cursor": nil,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := notion.NewClientWithBaseURL("test-token", "db-123", server.URL)

	expenses, err := client.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	if len(expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(expenses))
	}

	exp := expenses[0]
	if exp.Name != "拉麵" {
		t.Errorf("Name = %q, want %q", exp.Name, "拉麵")
	}
	if exp.Price != 1200 {
		t.Errorf("Price = %d, want %d", exp.Price, 1200)
	}
	if exp.Category != domain.Category食 {
		t.Errorf("Category = %q, want %q", exp.Category, domain.Category食)
	}
}

func TestClient_DeleteExpense(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/pages/page-123" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := notion.NewClientWithBaseURL("test-token", "db-123", server.URL)
	err := client.DeleteExpense(context.Background(), "page-123")
	if err != nil {
		t.Fatalf("DeleteExpense() error = %v", err)
	}
}

func TestClient_DeleteExpense_EmptyID(t *testing.T) {
	client := notion.NewClientWithBaseURL("test-token", "db-123", "http://unused")
	err := client.DeleteExpense(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}

func TestClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "unauthorized"}`))
	}))
	defer server.Close()

	client := notion.NewClientWithBaseURL("bad-token", "db-123", server.URL)
	_, err := client.QueryExpenses(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}
