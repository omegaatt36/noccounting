package trek

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestTripPath_AddressesOneTrip(t *testing.T) {
	cases := map[string]string{
		"":           "/api/trips/3",
		"/budget":    "/api/trips/3/budget",
		"/budget/12": "/api/trips/3/budget/12",
		"/files":     "/api/trips/3/files",
		"/members":   "/api/trips/3/members",
	}
	for suffix, want := range cases {
		if got := tripPath(3, suffix); got != want {
			t.Errorf("tripPath(3, %q) = %q, want %q", suffix, got, want)
		}
	}
}

func TestNewClient_TrimsTheBaseURL(t *testing.T) {
	client := NewClient(Config{BaseURL: "https://trek.example.com/"})
	if client.baseURL != "https://trek.example.com" {
		t.Errorf("baseURL = %q, want the trailing slash trimmed", client.baseURL)
	}
}

func TestNewAPIError_ParsesTheErrorBody(t *testing.T) {
	err := newAPIError(401, []byte(`{"error":"Access token required","code":"AUTH_REQUIRED"}`))
	if err.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", err.StatusCode)
	}
	if err.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q, want AUTH_REQUIRED", err.Code)
	}
	if err.Message != "Access token required" {
		t.Errorf("Message = %q, want %q", err.Message, "Access token required")
	}
}

func TestNewAPIError_FallsBackToTheRawBody(t *testing.T) {
	err := newAPIError(500, []byte("upstream exploded"))
	if err.Message != "upstream exploded" {
		t.Errorf("Message = %q, want the raw body", err.Message)
	}
	if got := err.Error(); got != "trek API error (status 500): upstream exploded" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRepo_KeepsRosterCachesSeparateByTrip(t *testing.T) {
	repo := NewRepo(nil, nil)
	first := repo.forTrip(domain.Trip{ID: 3, Currency: domain.CurrencyTWD})
	same := repo.forTrip(domain.Trip{ID: 3, Currency: domain.CurrencyJPY})
	other := repo.forTrip(domain.Trip{ID: 9, Currency: domain.CurrencyTWD})
	if first.payers != same.payers {
		t.Fatal("same trip must reuse the roster cache after a currency change")
	}
	if first.payers == other.payers {
		t.Fatal("different trips must not share a roster cache")
	}
	if same.baseCurrency != domain.CurrencyJPY {
		t.Fatal("trip currency change did not reach repository call")
	}
}
