package exchangerate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/infrastructure/exchangerate"
)

func TestFinMindClient_GetRate_TWD(t *testing.T) {
	client := exchangerate.NewFinMindClient()
	rate, err := client.GetRate(context.Background(), domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("GetRate(TWD) error = %v", err)
	}
	if !rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("GetRate(TWD) = %s, want 1", rate)
	}
}

func TestFinMindClient_GetRate_JPY(t *testing.T) {
	response := map[string]any{
		"status": 200,
		"data": []map[string]any{
			{
				"date":      "2026-02-20",
				"currency":  "JPY",
				"cash_buy":  0.2000,
				"cash_sell": 0.2200,
				"spot_buy":  0.2100,
				"spot_sell": 0.2150,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	rate, err := client.GetRate(context.Background(), domain.CurrencyJPY)
	if err != nil {
		t.Fatalf("GetRate(JPY) error = %v", err)
	}

	expected := decimal.NewFromFloat(0.22)
	if !rate.Equal(expected) {
		t.Errorf("GetRate(JPY) = %s, want %s", rate, expected)
	}
}

func TestFinMindClient_GetRate_UnsupportedCurrency(t *testing.T) {
	client := exchangerate.NewFinMindClient()
	_, err := client.GetRate(context.Background(), domain.Currency("USD"))
	if err == nil {
		t.Fatal("expected error for unsupported currency")
	}
}

func TestFinMindClient_GetRate_EmptyData(t *testing.T) {
	response := map[string]any{
		"status": 200,
		"data":   []any{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	_, err := client.GetRate(context.Background(), domain.CurrencyJPY)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestFinMindClient_GetRate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	_, err := client.GetRate(context.Background(), domain.CurrencyJPY)
	if err == nil {
		t.Fatal("expected error for server error")
	}
}
