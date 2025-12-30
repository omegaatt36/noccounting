package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRecoverWrapNoPanic tests that recoverWrap passes through normal handlers without modification.
func TestRecoverWrapNoPanic(t *testing.T) {
	handler := recoverWrap()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body != `{"status":"ok"}` {
		t.Errorf("expected body '{\"status\":\"ok\"}', got %s", body)
	}
}

// TestRecoverWrapWithPanic tests that recoverWrap catches panics and returns 500.
func TestRecoverWrapWithPanic(t *testing.T) {
	handler := recoverWrap()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp map[string]any
	err := json.NewDecoder(w.Body).Decode(&resp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if code, ok := resp["code"].(float64); !ok || code != -1 {
		t.Errorf("expected code -1, got %v", resp["code"])
	}

	if msg, ok := resp["message"].(string); !ok || msg != "Internal Server Error" {
		t.Errorf("expected message 'Internal Server Error', got %v", resp["message"])
	}
}

// TestRateLimiterAllowWithinLimit tests that requests within the rate limit are allowed.
func TestRateLimiterAllowWithinLimit(t *testing.T) {
	limiter := newRateLimiter(3, 100*time.Millisecond)

	ip := "192.168.1.1"

	// First request should be allowed
	if !limiter.allow(ip) {
		t.Errorf("first request should be allowed")
	}

	// Second request should be allowed
	if !limiter.allow(ip) {
		t.Errorf("second request should be allowed")
	}

	// Third request should be allowed
	if !limiter.allow(ip) {
		t.Errorf("third request should be allowed")
	}
}

// TestRateLimiterAllowOverLimit tests that requests exceeding the rate limit are blocked.
func TestRateLimiterAllowOverLimit(t *testing.T) {
	limiter := newRateLimiter(2, 100*time.Millisecond)

	ip := "192.168.1.1"

	// First two requests should be allowed
	if !limiter.allow(ip) {
		t.Errorf("first request should be allowed")
	}
	if !limiter.allow(ip) {
		t.Errorf("second request should be allowed")
	}

	// Third request should be blocked
	if limiter.allow(ip) {
		t.Errorf("third request should be blocked")
	}
}

// TestRateLimiterResetsAfterWindow tests that the rate limit resets after the time window passes.
func TestRateLimiterResetsAfterWindow(t *testing.T) {
	window := 50 * time.Millisecond
	limiter := newRateLimiter(1, window)

	ip := "192.168.1.1"

	// First request should be allowed
	if !limiter.allow(ip) {
		t.Errorf("first request should be allowed")
	}

	// Second request should be blocked (within window)
	if limiter.allow(ip) {
		t.Errorf("second request within window should be blocked")
	}

	// Wait for window to pass
	time.Sleep(window + 10*time.Millisecond)

	// Third request should be allowed (after window reset)
	if !limiter.allow(ip) {
		t.Errorf("request after window should be allowed")
	}
}

// TestRateLimiterPerIP tests that different IPs have separate rate limits.
func TestRateLimiterPerIP(t *testing.T) {
	limiter := newRateLimiter(1, 100*time.Millisecond)

	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	// First IP: first request allowed
	if !limiter.allow(ip1) {
		t.Errorf("first request for ip1 should be allowed")
	}

	// First IP: second request blocked
	if limiter.allow(ip1) {
		t.Errorf("second request for ip1 should be blocked")
	}

	// Second IP: first request should be allowed (separate limit)
	if !limiter.allow(ip2) {
		t.Errorf("first request for ip2 should be allowed")
	}

	// Second IP: second request blocked
	if limiter.allow(ip2) {
		t.Errorf("second request for ip2 should be blocked")
	}
}

// TestRateLimitMiddlewareSkipsHealthCheck tests that the /health endpoint bypasses rate limiting.
func TestRateLimitMiddlewareSkipsHealthCheck(t *testing.T) {
	handler := rateLimit(1, 100*time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	ip := "192.168.1.1"

	// Make a health check request
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health check should return 200, got %d", w.Code)
	}

	// Make another health check request with same IP
	// It should succeed even though we've "used up" the rate limit
	req = httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = ip
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("second health check should return 200, got %d", w.Code)
	}
}

// TestRateLimitMiddlewareBlocksOverLimit tests that non-health requests are rate limited.
func TestRateLimitMiddlewareBlocksOverLimit(t *testing.T) {
	handler := rateLimit(1, 100*time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	ip := "192.168.1.1"

	// First request should succeed
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request should return 200, got %d", w.Code)
	}

	// Second request should be rate limited
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = ip
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second request should return 429, got %d", w.Code)
	}

	// Verify response format
	var resp map[string]string
	err := json.NewDecoder(w.Body).Decode(&resp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if msg, ok := resp["error"]; !ok || msg != "too many requests" {
		t.Errorf("expected error message 'too many requests', got %v", resp["error"])
	}

	// Verify Retry-After header
	if retryAfter := w.Header().Get("Retry-After"); retryAfter != "60" {
		t.Errorf("expected Retry-After header '60', got %s", retryAfter)
	}
}

// TestRateLimitMiddlewareUsesXForwardedFor tests that X-Forwarded-For header is used for IP extraction.
func TestRateLimitMiddlewareUsesXForwardedFor(t *testing.T) {
	handler := rateLimit(1, 100*time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Forwarded-For header
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "127.0.0.1:9000"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request should return 200, got %d", w.Code)
	}

	// Second request with same X-Forwarded-For header should be rate limited
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "127.0.0.1:9001" // Different RemoteAddr but same X-Forwarded-For
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second request with same X-Forwarded-For should return 429, got %d", w.Code)
	}
}
