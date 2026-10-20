package exchangerate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/exchangerate"
)

func TestFinMindClient_GetRate_SameCurrencyIsOne(t *testing.T) {
	client := exchangerate.NewFinMindClient()
	rate, err := client.GetRate(context.Background(), domain.CurrencyTWD, domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("GetRate(TWD, TWD) error = %v", err)
	}
	if !rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("GetRate(TWD, TWD) = %s, want 1", rate)
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	rate, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("GetRate(JPY, TWD) error = %v", err)
	}

	expected := decimal.NewFromFloat(0.22)
	if !rate.Equal(expected) {
		t.Errorf("GetRate(JPY, TWD) = %s, want %s", rate, expected)
	}
}

func TestFinMindClient_GetRate_TWDInJPY(t *testing.T) {
	response := map[string]any{
		"status": 200,
		"data":   []map[string]any{{"date": "2026-02-20", "currency": "JPY", "cash_sell": 0.2200}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	rate, err := client.GetRate(context.Background(), domain.CurrencyTWD, domain.CurrencyJPY)
	if err != nil {
		t.Fatalf("GetRate(TWD, JPY) error = %v", err)
	}

	if expected := decimal.RequireFromString("4.545455"); !rate.Equal(expected) {
		t.Errorf("GetRate(TWD, JPY) = %s, want %s", rate, expected)
	}
}

func TestFinMindClient_GetRate_UnsupportedCurrency(t *testing.T) {
	client := exchangerate.NewFinMindClient()
	_, err := client.GetRate(context.Background(), domain.Currency("USD"), domain.CurrencyTWD)
	if err == nil {
		t.Fatal("expected error for unsupported currency")
	}
}

func TestFinMindClient_GetRate_EmptyData(t *testing.T) {
	response := map[string]any{
		"status": 200,
		"data":   []any{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	_, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestFinMindClient_GetRate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	_, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestFinMindClient_GetRate_UsesCachedRate(t *testing.T) {
	requests := 0
	response := map[string]any{
		"status": 200,
		"data": []map[string]any{
			{"date": "2026-02-20", "currency": "JPY", "cash_sell": 0.2200},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	rate1, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("first GetRate() error = %v", err)
	}

	rate2, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("second GetRate() error = %v", err)
	}

	if !rate1.Equal(rate2) {
		t.Errorf("rate1 (%s) != rate2 (%s)", rate1, rate2)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (second call should use cache)", requests)
	}
}

func TestFinMindClient_GetRate_UsesTheLastRateWhenFinMindIsDown(t *testing.T) {
	down := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status": 200,
			"data":   []map[string]any{{"date": "2026-02-20", "currency": "JPY", "cash_sell": 0.2200}},
		}); err != nil {
			t.Errorf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	client.SetCacheTTL(time.Nanosecond)
	fresh, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err != nil {
		t.Fatal(err)
	}

	down = true
	stale, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err != nil {
		t.Fatalf("GetRate() with FinMind down error = %v, want the last rate", err)
	}
	if !stale.Equal(fresh) {
		t.Errorf("rate = %s, want the last one, %s", stale, fresh)
	}

	inverse, err := client.GetRate(context.Background(), domain.CurrencyTWD, domain.CurrencyJPY)
	if err != nil || inverse.IsZero() {
		t.Errorf("TWD to JPY with FinMind down = %s, %v, want the inverse of the last rate", inverse, err)
	}
}

func TestFinMindClient_GetRate_DoesNotUseARateThatIsTooOld(t *testing.T) {
	down := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status": 200,
			"data":   []map[string]any{{"date": "2026-02-20", "currency": "JPY", "cash_sell": 0.2200}},
		}); err != nil {
			t.Errorf("Encode error = %v", err)
		}
	}))
	defer server.Close()

	client := exchangerate.NewFinMindClientWithBaseURL(server.URL)
	client.SetCacheTTL(time.Nanosecond)
	client.SetMaxStale(time.Nanosecond)
	if _, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD); err != nil {
		t.Fatal(err)
	}

	down = true
	time.Sleep(time.Millisecond)
	if _, err := client.GetRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD); err == nil {
		t.Error("GetRate() = nil error, want a rate older than the limit refused")
	}
}
