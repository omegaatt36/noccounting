package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func decodeBootstrap(t *testing.T, w *httptest.ResponseRecorder) BootstrapResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var got BootstrapResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return got
}

func TestHandleBootstrap_OffersEverythingThePageDrawsFrom(t *testing.T) {
	repo := &stubAccountingRepo{members: []domain.Member{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "bob"}}}
	handler, _ := devHandler(t, repo, twoTrips{})

	got := decodeBootstrap(t, get(t, handler.handleBootstrap, "/api/bootstrap"))

	if len(got.Trips) != 2 || got.Trips[1].Currency != "JPY" || got.Trips[1].Title != "Osaka" {
		t.Errorf("trips = %+v, want both with their currency", got.Trips)
	}
	if got.Current != tokyoTrip.ID {
		t.Errorf("current = %d, want the default, the first of two undated trips", got.Current)
	}
	if len(got.Users) != 1 || got.Users[0].Nickname != "John" || got.Users[0].TelegramID != 123456789 {
		t.Errorf("users = %+v, want the user mapping", got.Users)
	}
	// User 8 is in the mapping as John; user 3 is not and keeps the name TREK has.
	want := []MemberInfo{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "John"}}
	if len(got.Members) != 2 || got.Members[0] != want[0] || got.Members[1] != want[1] {
		t.Errorf("members = %+v, want %+v", got.Members, want)
	}
}

func TestHandleBootstrap_NoTripsIsAnEmptyListNotAnError(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, noTrips{})

	got := decodeBootstrap(t, get(t, handler.handleBootstrap, "/api/bootstrap"))

	if got.Current != 0 || len(got.Trips) != 0 || len(got.Members) != 0 {
		t.Errorf("response = %+v, want no trips, no current and no members", got)
	}
	if got.Trips == nil || got.Members == nil {
		t.Errorf("response = %+v, want empty lists the page can iterate, not null", got)
	}
}

func TestHandleBootstrap_FollowsTheTripTheCallerSelected(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})
	post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"9"}})

	got := decodeBootstrap(t, get(t, handler.handleBootstrap, "/api/bootstrap"))

	if got.Current != osakaTrip.ID {
		t.Errorf("current = %d, want the trip just chosen", got.Current)
	}
}

func TestHandleBootstrap_ATripListThatCannotBeReadIsAServerError(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, scriptedTrips{err: http.ErrHandlerTimeout})

	if w := get(t, handler.handleBootstrap, "/api/bootstrap"); w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500, which the page offers a retry for", w.Code)
	}
}

func TestHandleBootstrap_OnlyLetsInWhoTelegramVouchesFor(t *testing.T) {
	const botToken = "test-token"
	member := &domain.User{ID: 1, TelegramID: 111, BackendUserID: "8", Nickname: "Ann"}
	handler, err := NewHandler(
		user.NewService(&fakeUserRepo{users: map[int64]*domain.User{member.TelegramID: member}}),
		expense.NewService(&stubAccountingRepo{}, nil, nil), newTestTrips(), botToken, false,
	)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}
	signed := func(id string) string {
		return buildValidTelegramInitData(botToken, map[string]string{"user": `{"id":` + id + `,"first_name":"x"}`})
	}
	forged := signed("111")
	forged = forged[:len(forged)-4] + "0000"

	tests := []struct {
		name     string
		initData string
		want     int
	}{
		{"nothing", "", http.StatusForbidden},
		{"a forged signature", forged, http.StatusForbidden},
		{"someone outside the mapping", signed("999"), http.StatusForbidden},
		{"a member", signed("111"), http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := get(t, handler.handleBootstrap, "/api/bootstrap?init_data="+url.QueryEscape(tt.initData))
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
