package webapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type scriptedTrips struct {
	trips []domain.Trip
	err   error
}

func (s scriptedTrips) ListTrips(context.Context) ([]domain.Trip, error) { return s.trips, s.err }

func readyServer(t *testing.T, trips trip.Repository) http.Handler {
	t.Helper()
	users := user.NewService(&fakeUserRepo{users: map[int64]*domain.User{}})
	expenses := expense.NewService(&stubAccountingRepo{}, nil, nil)
	server, err := NewServer(users, expenses, trip.NewService(trips), "0", "test-token", false)
	if err != nil {
		t.Fatalf("creating the server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	return server.Handler()
}

func TestReady(t *testing.T) {
	t.Run("is ready while TREK answers", func(t *testing.T) {
		w := httptest.NewRecorder()
		readyServer(t, scriptedTrips{trips: []domain.Trip{testTrip}}).ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("a service account on no trip is still ready", func(t *testing.T) {
		w := httptest.NewRecorder()
		readyServer(t, scriptedTrips{}).ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: onboarding is not an outage", w.Code)
		}
	})

	t.Run("is unready, without saying why, when TREK fails", func(t *testing.T) {
		w := httptest.NewRecorder()
		failing := scriptedTrips{err: errors.New("dial tcp 10.0.0.7:443: connection refused")}
		readyServer(t, failing).ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
		if strings.Contains(w.Body.String(), "10.0.0.7") {
			t.Errorf("the response gives away where TREK is: %q", w.Body.String())
		}
	})

	t.Run("liveness does not depend on TREK", func(t *testing.T) {
		w := httptest.NewRecorder()
		readyServer(t, scriptedTrips{err: errors.New("down")}).ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: a TREK outage must not make a liveness probe restart the process", w.Code)
		}
	})
}

func TestProbesAreNotRateLimited(t *testing.T) {
	handler := rateLimit(1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for range 5 {
		req := httptest.NewRequest("GET", "/ready", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	}
}
