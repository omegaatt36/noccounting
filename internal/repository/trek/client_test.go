package trek_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/trek"
)

func testConfig() trek.Config {
	return trek.Config{
		BaseURL:  "https://trek.example.com",
		Email:    "bot@example.com",
		Password: "s3cret",
	}
}

type stubTrek struct {
	server *httptest.Server

	loginStatus int
	loginBody   string
	loginOnCall map[int]stubAnswer

	tripStatus int
	tripBody   string
	tripOnCall map[int]stubAnswer

	mu           sync.Mutex
	loginCalls   int
	tripCalls    int
	authHeaders  []string
	loginPayload []byte
}

type stubAnswer struct {
	status int
	body   string
}

func newStubTrek(t *testing.T) *stubTrek {
	t.Helper()

	s := &stubTrek{
		loginStatus: http.StatusOK,
		loginBody:   `{"token":"token-1","user":{"id":7,"email":"bot@example.com"}}`,
		loginOnCall: map[int]stubAnswer{},
		tripStatus:  http.StatusOK,
		tripBody:    `{"trips":[{"id":3,"title":"2026 Tokyo","currency":"TWD"}]}`,
		tripOnCall:  map[int]stubAnswer{},
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stubTrek) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/auth/login":
		s.handleLogin(w, r)
	case "/api/trips":
		s.handleTrips(w, r)
	default:
		writeJSON(w, http.StatusNotFound, `{"error":"Not found"}`)
	}
}

func (s *stubTrek) handleLogin(w http.ResponseWriter, r *http.Request) {
	payload, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.loginCalls++
	s.loginPayload = payload
	call := s.loginCalls
	s.mu.Unlock()

	if override, ok := s.loginOnCall[call]; ok {
		writeJSON(w, override.status, override.body)
		return
	}
	writeJSON(w, s.loginStatus, s.loginBody)
}

func (s *stubTrek) handleTrips(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.tripCalls++
	call := s.tripCalls
	s.authHeaders = append(s.authHeaders, r.Header.Get("Authorization"))
	s.mu.Unlock()

	if override, ok := s.tripOnCall[call]; ok {
		writeJSON(w, override.status, override.body)
		return
	}
	writeJSON(w, s.tripStatus, s.tripBody)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The client may cancel the request before the stub finishes writing.
	_, _ = io.WriteString(w, body)
}

func (s *stubTrek) snapshot() (logins, trips int, auth []string, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loginCalls, s.tripCalls, append([]string(nil), s.authHeaders...), append([]byte(nil), s.loginPayload...)
}

func (s *stubTrek) client() *trek.Client {
	return trek.NewClientWithBaseURL(testConfig(), s.server.URL)
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("reads every variable", func(t *testing.T) {
		t.Setenv("TREK_URL", "https://trek.example.com/")
		t.Setenv("TREK_EMAIL", "bot@example.com")
		t.Setenv("TREK_PASSWORD", "s3cret")

		cfg, err := trek.ConfigFromEnv()
		if err != nil {
			t.Fatalf("ConfigFromEnv() error = %v", err)
		}
		if cfg.BaseURL != "https://trek.example.com" {
			t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://trek.example.com")
		}
		if cfg.Email != "bot@example.com" {
			t.Errorf("Email = %q, want %q", cfg.Email, "bot@example.com")
		}
		if cfg.Password != "s3cret" {
			t.Errorf("Password = %q, want %q", cfg.Password, "s3cret")
		}
	})

	t.Run("names every missing variable", func(t *testing.T) {
		for _, key := range []string{"TREK_URL", "TREK_EMAIL", "TREK_PASSWORD"} {
			t.Setenv(key, "")
		}

		_, err := trek.ConfigFromEnv()
		if err == nil {
			t.Fatal("expected an error when nothing is configured")
		}
		for _, key := range []string{"TREK_URL", "TREK_EMAIL", "TREK_PASSWORD"} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name %s", err, key)
			}
		}
	})

	t.Run("needs no Notion or SQLite variable", func(t *testing.T) {
		for _, key := range []string{
			"NOTION_TOKEN", "NOTION_DATABASE_ID", "SQLITE_PATH", "SQLITE_DB_PATH", "NOTION_API_KEY",
		} {
			t.Setenv(key, "")
		}
		t.Setenv("TREK_URL", "https://trek.example.com")
		t.Setenv("TREK_EMAIL", "bot@example.com")
		t.Setenv("TREK_PASSWORD", "s3cret")

		cfg, err := trek.ConfigFromEnv()
		if err != nil {
			t.Fatalf("ConfigFromEnv() error = %v, want the three TREK variables to be the whole credential", err)
		}
		if cfg.Email != "bot@example.com" {
			t.Errorf("Email = %q, want the credential read", cfg.Email)
		}
	})

	t.Run("requires https beyond localhost", func(t *testing.T) {
		afterTrim := t
		_ = afterTrim
		tests := []struct {
			name    string
			url     string
			wantErr string
		}{
			{name: "plain http against a public host", url: "http://trek.example.com", wantErr: "plain text"},
			{name: "an address with no scheme", url: "trek.example.com", wantErr: "scheme"},
			{name: "a bare host with no host at all", url: "https://", wantErr: "names no host"},
			{name: "a URL carrying its own path", url: "https://trek.example.com/api/", wantErr: "instance root"},
			{name: "a URL carrying its own query", url: "https://trek.example.com?x=1", wantErr: "query or fragment"},
			{name: "a URL carrying its own fragment", url: "https://trek.example.com#api", wantErr: "query or fragment"},
			{name: "http against a non-loopback local name", url: "http://trek.lan:3001", wantErr: "plain text"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv("TREK_URL", tt.url)
				t.Setenv("TREK_EMAIL", "bot@example.com")
				t.Setenv("TREK_PASSWORD", "s3cret")

				_, err := trek.ConfigFromEnv()
				if err == nil {
					t.Fatalf("ConfigFromEnv() accepted %q", tt.url)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not mention %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "TREK_URL") {
					t.Errorf("error %q does not name TREK_URL", err)
				}
			})
		}
	})

	t.Run("accepts https anywhere and http on this machine", func(t *testing.T) {
		tests := []struct {
			name string
			url  string
		}{
			{name: "the deployed https instance", url: "https://trek.example.com/"},
			{name: "localhost by name", url: "http://localhost:3001"},
			{name: "localhost by the whole literal", url: "http://127.0.0.1:3001"},
			{name: "localhost by the ipv6 literal", url: "http://[::1]:3001"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv("TREK_URL", tt.url)
				t.Setenv("TREK_EMAIL", "bot@example.com")
				t.Setenv("TREK_PASSWORD", "s3cret")

				cfg, err := trek.ConfigFromEnv()
				if err != nil {
					t.Fatalf("ConfigFromEnv() refused %q: %v", tt.url, err)
				}
				if !strings.HasSuffix(cfg.BaseURL, ":3001") && tt.url != "https://trek.example.com/" {
					t.Errorf("BaseURL = %q, want the port to have survived the parse", cfg.BaseURL)
				}
			})
		}
	})
}

func TestClient_Start_HoldsTheSessionToken(t *testing.T) {
	stub := newStubTrek(t)
	client := stub.client()

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	logins, trips, _, payload := stub.snapshot()
	if logins != 1 {
		t.Errorf("login calls = %d, want 1", logins)
	}
	if trips != 0 {
		t.Errorf("trip list calls = %d, want 0 — Start only logs in", trips)
	}

	var sent struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.Unmarshal(payload, &sent); err != nil {
		t.Fatalf("login payload is not JSON: %v", err)
	}
	if sent.Email != testConfig().Email || sent.Password != testConfig().Password {
		t.Errorf("login payload = %+v, want the configured service account", sent)
	}
	if !sent.RememberMe {
		t.Error("remember_me = false, want true so the long-lived session token is issued")
	}

	if _, err := client.ListTrips(context.Background()); err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}
	if _, _, auth, _ := stub.snapshot(); len(auth) != 1 || auth[0] != "Bearer token-1" {
		t.Errorf("trip list authorization = %v, want the session token Start held", auth)
	}
}

func TestClient_Start_RefusesASecondFactor(t *testing.T) {
	stub := newStubTrek(t)
	stub.loginBody = `{"mfa_required":true,"mfa_token":"mfa-123"}`
	client := stub.client()

	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error when a second factor is demanded")
	}
	if !errors.Is(err, trek.ErrSecondFactorRequired) {
		t.Errorf("error = %v, want ErrSecondFactorRequired", err)
	}
	if !strings.Contains(err.Error(), "without 2FA") {
		t.Errorf("error %q does not say a service account without 2FA is required", err)
	}
}

func TestClient_Start_ReportsABadCredential(t *testing.T) {
	stub := newStubTrek(t)
	stub.loginStatus = http.StatusUnauthorized
	stub.loginBody = `{"error":"Invalid email or password"}`
	client := stub.client()

	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error for a rejected login")
	}
	if !strings.Contains(err.Error(), "Invalid email or password") {
		t.Errorf("error %q does not carry the server's message", err)
	}
	if strings.Contains(err.Error(), testConfig().Email) {
		t.Errorf("error %q leaks the account email; name the credential without its address", err)
	}

	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestClient_ListTrips_ReadsEveryTripTheAccountIsOn(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripBody = `{"trips":[
		{"id":3,"title":"2026 Tokyo","currency":"JPY","start_date":"2026-10-01","end_date":"2026-10-09"},
		{"id":9,"title":"Osaka 2026","currency":"TWD","start_date":null,"end_date":null}
	]}`
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	trips, err := client.ListTrips(context.Background())
	if err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}

	want := []domain.Trip{
		{
			ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyJPY,
			StartDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
		},
		{ID: 9, Title: "Osaka 2026", Currency: domain.CurrencyTWD},
	}
	if !reflect.DeepEqual(trips, want) {
		t.Errorf("ListTrips() = %+v\nwant %+v", trips, want)
	}
}

func TestClient_ListTrips_FoldsTheCurrencyCode(t *testing.T) {
	tests := map[string]domain.Currency{
		"jpy":   domain.CurrencyJPY,
		" TWD ": domain.CurrencyTWD,
	}

	for stored, want := range tests {
		t.Run(stored, func(t *testing.T) {
			stub := newStubTrek(t)
			stub.tripBody = `{"trips":[{"id":3,"title":"t","currency":"` + stored + `"}]}`
			client := stub.client()
			if err := client.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}

			trips, err := client.ListTrips(context.Background())
			if err != nil {
				t.Fatalf("ListTrips() error = %v", err)
			}
			if len(trips) != 1 || trips[0].Currency != want {
				t.Errorf("ListTrips() = %+v, want one trip in %s", trips, want)
			}
		})
	}
}

func TestClient_ListTrips_LeavesOutATripInAnUnsupportedCurrency(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripBody = `{"trips":[
		{"id":3,"title":"2026 Tokyo","currency":"TWD"},
		{"id":4,"title":"Seoul","currency":"KRW"}
	]}`
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	trips, err := client.ListTrips(context.Background())
	if err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}
	if len(trips) != 1 || trips[0].ID != 3 {
		t.Errorf("ListTrips() = %+v, want only trip 3", trips)
	}
}

func TestClient_ListTrips_KeepsATripWithAnUnreadableDate(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripBody = `{"trips":[{"id":3,"title":"t","currency":"TWD","start_date":"soon","end_date":"later"}]}`
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	trips, err := client.ListTrips(context.Background())
	if err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}
	if len(trips) != 1 || trips[0].HasDates() {
		t.Errorf("ListTrips() = %+v, want the trip kept, undated", trips)
	}
}

func TestClient_ListTrips_ReportsAFailedRead(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripStatus = http.StatusInternalServerError
	stub.tripBody = `{"error":"database is unavailable"}`
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := client.ListTrips(context.Background()); err == nil {
		t.Fatal("expected a failed read to be an error, not an empty list")
	}
}

func TestClient_ReauthenticatesOnceOnUnauthorized(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripOnCall[1] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	stub.loginOnCall[2] = stubAnswer{
		status: http.StatusOK,
		body:   `{"token":"token-2","user":{"id":7,"email":"bot@example.com"}}`,
	}
	client := stub.client()

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := client.ListTrips(context.Background()); err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}

	logins, trips, auth, _ := stub.snapshot()
	if logins != 2 {
		t.Errorf("login calls = %d, want 2 (initial + one re-authentication)", logins)
	}
	if trips != 2 {
		t.Errorf("trip list calls = %d, want 2 (a rejected read and its replay)", trips)
	}
	if len(auth) != 2 {
		t.Fatalf("trip list authorization = %v, want two calls", auth)
	}
	if auth[0] != "Bearer token-1" {
		t.Errorf("first call authorization = %q, want %q", auth[0], "Bearer token-1")
	}
	if auth[1] != "Bearer token-2" {
		t.Errorf("replay authorization = %q, want %q", auth[1], "Bearer token-2")
	}
}

func TestClient_DoesNotRetryASecondUnauthorized(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripStatus = http.StatusUnauthorized
	stub.tripBody = `{"error":"Access token required","code":"AUTH_REQUIRED"}`
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := client.ListTrips(context.Background())
	if err == nil {
		t.Fatal("expected the second 401 to reach the caller")
	}
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}

	logins, trips, _, _ := stub.snapshot()
	if logins != 2 {
		t.Errorf("login calls = %d, want exactly 2 — the retry must not re-authenticate again", logins)
	}
	if trips != 2 {
		t.Errorf("trip list calls = %d, want exactly 2", trips)
	}
}

func TestClient_ReportsAFailedReauthentication(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripOnCall[1] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	stub.loginOnCall[2] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Invalid email or password"}`,
	}
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := client.ListTrips(context.Background())
	if err == nil {
		t.Fatal("expected the failed re-authentication to reach the caller")
	}
	if !strings.Contains(err.Error(), "Invalid email or password") {
		t.Errorf("error %q does not carry the server's message", err)
	}

	if _, trips, _, _ := stub.snapshot(); trips != 1 {
		t.Errorf("trip list calls = %d, want 1 — an unauthenticated retry is pointless", trips)
	}
}

func TestClient_ListTripsIsSafeUnderConcurrency(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripOnCall[1] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := range callers {
		wg.Go(func() {
			<-start
			_, errs[i] = client.ListTrips(context.Background())
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: ListTrips() error = %v", i, err)
		}
	}
}

func TestClient_ConcurrentUnauthorizedOnlyReauthenticatesOnce(t *testing.T) {
	stub := newStubTrek(t)
	stub.tripOnCall[1] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	stub.tripOnCall[2] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	stub.loginOnCall[2] = stubAnswer{
		status: http.StatusOK,
		body:   `{"token":"token-2","user":{"id":7,"email":"bot@example.com"}}`,
	}
	client := stub.client()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.ListTrips(context.Background())
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("ListTrips() error = %v", err)
		}
	}

	logins, _, _, _ := stub.snapshot()
	if logins != 2 {
		t.Errorf("login calls = %d, want 2 (initial + exactly one re-auth across concurrent calls)", logins)
	}
}
