package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	if err := healthcheck(t.Context(), server.URL); err != nil {
		t.Errorf("a ready instance failed the check: %v", err)
	}

	status = http.StatusServiceUnavailable
	if err := healthcheck(t.Context(), server.URL); err == nil {
		t.Error("an unready instance passed the check")
	}

	server.Close()
	if err := healthcheck(t.Context(), server.URL); err == nil {
		t.Error("an instance that is not running passed the check")
	}
}
