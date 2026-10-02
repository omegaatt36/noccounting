package trek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	payerTripID    = 3
	payerOwnerID   = 3
	payerMemberID  = 8
	payerGuestID   = 9
	payerOffTripID = 42
)

const payerRosterJSON = `{
  "owner": {"id":3,"username":"owner-trek","email":"owner@example.com","role":"owner","is_guest":false},
  "members": [
    {"id":8,"username":"member-trek","email":"member@example.com","role":"member","is_guest":false},
    {"id":9,"username":"guest-trek","email":"","role":"member","is_guest":true}
  ],
  "current_user_id":7
}`

const payerRosterJSONWithoutOwner = `{"owner":{"id":0,"username":"","email":"","role":"","is_guest":false},"members":[],"current_user_id":7}`

type payerRosterStub struct {
	server *httptest.Server

	body     string
	status   int
	statuses []int

	mu     sync.Mutex
	calls  []string
	bodies []string
}

func newPayerRosterStub(t *testing.T, body string) *payerRosterStub {
	t.Helper()

	stub := &payerRosterStub{body: body, status: http.StatusOK}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *payerRosterStub) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, r.URL.Path)
	answer, code := s.body, s.status
	if call := len(s.calls) - 1; call < len(s.bodies) {
		answer = s.bodies[call]
	}
	if call := len(s.calls) - 1; call < len(s.statuses) {
		code = s.statuses[call]
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	io.WriteString(w, answer)
}

func (s *payerRosterStub) client() *Client {
	return NewClientWithBaseURL(Config{
		BaseURL:  "https://trek.example.com",
		Email:    "bot@example.com",
		Password: "s3cret",
	}, s.server.URL)
}

func (s *payerRosterStub) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestPayerResolver_ResolvesAMappedPayerToATrekUserID(t *testing.T) {
	tests := []struct {
		name string
		ref  PayerRef
		want int64
	}{
		{name: "a member", ref: PayerRef{ID: "8", Nickname: "Bob"}, want: payerMemberID},
		{name: "a guest", ref: PayerRef{ID: "9", Nickname: "Cleo"}, want: payerGuestID},
		{name: "an id padded with whitespace", ref: PayerRef{ID: " 8\t", Nickname: "Bob"}, want: payerMemberID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newPayerRosterStub(t, payerRosterJSON)

			payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(), tt.ref)
			if err != nil {
				t.Fatalf("Resolve(%+v) error = %v", tt.ref, err)
			}
			if payer == nil {
				t.Fatalf("Resolve(%+v) = no payer, want TREK user %d", tt.ref, tt.want)
			}
			if payer.UserID != tt.want {
				t.Errorf("Resolve(%+v) = TREK user %d, want %d", tt.ref, payer.UserID, tt.want)
			}
			if payer.Nickname != tt.ref.Nickname {
				t.Errorf("Nickname = %q, want %q", payer.Nickname, tt.ref.Nickname)
			}

			wantPath := "/api/trips/3/members"
			if paths := stub.paths(); len(paths) != 1 || paths[0] != wantPath {
				t.Errorf("requests = %v, want exactly one %s", paths, wantPath)
			}
		})
	}
}

func TestPayerResolver_CountsTheTripOwnerOnTheRoster(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)
	resolver := NewPayerResolver(stub.client(), payerTripID)

	payer, err := resolver.Resolve(context.Background(), PayerRef{ID: "3", Nickname: "Olive"})
	if err != nil {
		t.Fatalf("Resolve(owner) error = %v", err)
	}
	if payer == nil || payer.UserID != payerOwnerID {
		t.Fatalf("Resolve(owner) = %+v, want TREK user %d", payer, payerOwnerID)
	}

	roster, err := resolver.rosterFor(context.Background())
	if err != nil {
		t.Fatalf("rosterFor() error = %v", err)
	}
	if !roster.Contains(payerOwnerID) {
		t.Error("roster.Contains(owner) = false, so the owner could never be a payer")
	}
	owner, ok := roster.Member(payerOwnerID)
	if !ok {
		t.Fatalf("roster.Member(%d) found nothing", payerOwnerID)
	}
	if owner.Role != "owner" || owner.Username != "owner-trek" {
		t.Errorf("roster.Member(%d) = %+v, want the owner row the route returned separately", payerOwnerID, owner)
	}
	var ids []int64
	for _, member := range roster.Members() {
		ids = append(ids, member.UserID)
	}
	if want := []int64{payerOwnerID, payerMemberID, payerGuestID}; !slicesEqual(ids, want) {
		t.Errorf("roster.Members() ids = %v, want %v", ids, want)
	}
}

func TestPayerResolver_RejectsAPayerWhoIsNotOnTheTrip(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)

	payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(),
		PayerRef{ID: "42", Nickname: "Alice"})
	if payer != nil {
		t.Errorf("Resolve() = %+v, want no payer alongside an error", payer)
	}
	if !errors.Is(err, ErrPayerNotOnTrip) {
		t.Fatalf("error = %v, want ErrPayerNotOnTrip", err)
	}

	for _, want := range []string{
		"Alice",            // who is off the trip, by the name the operator configured
		"42",               // and the id they have to correct
		"is not on trip 3", // which trip refused them
		"owner-trek",       // who is on it, so the mapping can be checked against reality
		"member-trek",
		"USER_MAPPING", // and where to correct it
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if paths := stub.paths(); len(paths) != 2 {
		t.Errorf("roster requests = %v, want a cached read plus one refresh", paths)
	}
}

func TestPayerResolver_NamesAnOffTripPayerWithoutANickname(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)

	_, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(), PayerRef{ID: "42"})
	if !errors.Is(err, ErrPayerNotOnTrip) {
		t.Fatalf("error = %v, want ErrPayerNotOnTrip", err)
	}
	if !strings.Contains(err.Error(), "TREK user 42") {
		t.Errorf("error %q does not identify the payer by id", err)
	}
	if !strings.Contains(err.Error(), "member-trek") {
		t.Errorf("error %q does not report who is on the trip", err)
	}
}

func TestPayerResolver_AcceptsAPayerTheRefreshedRosterHas(t *testing.T) {
	beforeAddition := `{
      "owner": {"id":3,"username":"owner-trek","email":"owner@example.com","role":"owner","is_guest":false},
      "members": [],
      "current_user_id":7
    }`
	afterAddition := `{
      "owner": {"id":3,"username":"owner-trek","email":"owner@example.com","role":"owner","is_guest":false},
      "members": [{"id":42,"username":"alice-trek","email":"alice@example.com","role":"member","is_guest":false}],
      "current_user_id":7
    }`

	stub := newPayerRosterStub(t, beforeAddition)
	stub.bodies = []string{beforeAddition, afterAddition}
	resolver := NewPayerResolver(stub.client(), payerTripID)

	payer, err := resolver.Resolve(context.Background(),
		PayerRef{ID: "42", Nickname: "Alice"})
	if err != nil {
		t.Fatalf("Resolve() error = %v, want the refreshed roster to be accepted", err)
	}
	if payer == nil || payer.UserID != payerOffTripID {
		t.Fatalf("Resolve() = %+v, want TREK user %d", payer, payerOffTripID)
	}
	if paths := stub.paths(); len(paths) != 2 {
		t.Errorf("roster requests = %v, want the cached read and one refresh", paths)
	}

	if _, err := resolver.Resolve(context.Background(), PayerRef{ID: "42"}); err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if paths := stub.paths(); len(paths) != 2 {
		t.Errorf("roster requests = %v, want the refreshed roster to have been cached", paths)
	}
}

func TestPayerResolver_StoresAnExpenseWithNoPayer(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		t.Run("id "+strconv.Quote(id), func(t *testing.T) {
			stub := newPayerRosterStub(t, payerRosterJSON)

			payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(),
				PayerRef{ID: id, Nickname: "Bob"})
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v, want an expense with no payer to be accepted", id, err)
			}
			if payer != nil {
				t.Errorf("Resolve(%q) = %+v, want no payer", id, payer)
			}
			if paths := stub.paths(); len(paths) != 0 {
				t.Errorf("roster requests = %v, want none: there is nobody to check", paths)
			}
		})
	}
}

func TestPayerResolver_RefusesAMappedIdThatIsNotATrekUserID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "a leftover uuid from the retired backend", id: "9f8e7d6c-5b4a-3210-fedc-ba9876543210"},
		{name: "a word", id: "alice"},
		{name: "a decimal", id: "8.5"},
		{name: "scientific notation", id: "8e3"},
		{name: "a hexadecimal literal", id: "0x8"},
		{name: "digits with a letter", id: "8a"},
		{name: "zero", id: "0"},
		{name: "a negative id", id: "-8"},
		{name: "beyond int64", id: "9223372036854775808"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newPayerRosterStub(t, payerRosterJSON)

			payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(),
				PayerRef{ID: tt.id, Nickname: "Bob"})
			if payer != nil {
				t.Errorf("Resolve(%q) = %+v, want no payer", tt.id, payer)
			}
			if !errors.Is(err, ErrInvalidPayerID) {
				t.Fatalf("Resolve(%q) error = %v, want ErrInvalidPayerID", tt.id, err)
			}
			if errors.Is(err, ErrPayerNotOnTrip) {
				t.Errorf("Resolve(%q) error = %v, want an unusable id not reported as an off-trip payer", tt.id, err)
			}
			if !strings.Contains(err.Error(), strconv.Quote(tt.id)) {
				t.Errorf("error %q does not quote the value that has to be corrected", err)
			}
			if paths := stub.paths(); len(paths) != 0 {
				t.Errorf("roster requests = %v, want none: an unusable id is not a roster question", paths)
			}
		})
	}
}

func TestPayerResolver_ReadsTheRosterOnceWhileItIsFresh(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)
	resolver := NewPayerResolver(stub.client(), payerTripID)

	for range 3 {
		if _, err := resolver.Resolve(context.Background(), PayerRef{ID: "8"}); err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
	}
	if paths := stub.paths(); len(paths) != 1 {
		t.Errorf("roster requests = %v, want 1 for three writes", paths)
	}

	resolver.rosterTTL = 0
	if _, err := resolver.Resolve(context.Background(), PayerRef{ID: "8"}); err != nil {
		t.Fatalf("Resolve() after expiry error = %v", err)
	}
	if paths := stub.paths(); len(paths) != 2 {
		t.Errorf("roster requests = %v, want a re-read once the cached one expired", paths)
	}
}

func TestPayerResolver_ReportsAFailedRosterRead(t *testing.T) {
	stub := newPayerRosterStub(t, `{"error":"No permission","code":"FORBIDDEN"}`)
	stub.status = http.StatusForbidden

	payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(), PayerRef{ID: "8"})
	if payer != nil {
		t.Errorf("Resolve() = %+v, want no payer", payer)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want the server's *APIError in the chain", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", apiErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "roster") {
		t.Errorf("error %q does not say which read failed", err)
	}

	cached := newPayerRosterStub(t, payerRosterJSON)
	cached.bodies = []string{payerRosterJSON, `{"error":"Service unavailable"}`}
	cached.statuses = []int{http.StatusOK, http.StatusServiceUnavailable}
	resolver := NewPayerResolver(cached.client(), payerTripID)
	if _, err := resolver.Resolve(context.Background(), PayerRef{ID: "8"}); err != nil {
		t.Fatalf("Resolve() with a healthy roster error = %v", err)
	}

	payer, err = resolver.Resolve(context.Background(), PayerRef{ID: "42"})
	if payer != nil {
		t.Errorf("Resolve() = %+v, want no payer when the roster cannot be refreshed", payer)
	}
	if !strings.Contains(err.Error(), "roster") {
		t.Errorf("error = %v, want the failed refresh reported", err)
	}
}

func TestPayerResolver_RefusesARosterThatNamesNoOwner(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSONWithoutOwner)

	payer, err := NewPayerResolver(stub.client(), payerTripID).Resolve(context.Background(), PayerRef{ID: "8"})
	if payer != nil {
		t.Errorf("Resolve() = %+v, want no payer", payer)
	}
	if err == nil {
		t.Fatal("expected an error when the roster names no owner")
	}
	for _, want := range []string{"owner", "3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestPayerResolver_IsSafeUnderConcurrency(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)
	resolver := NewPayerResolver(stub.client(), payerTripID)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := range callers {
		wg.Go(func() {
			<-start
			_, errs[i] = resolver.Resolve(context.Background(), PayerRef{ID: "8", Nickname: "Bob"})
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: Resolve() error = %v", i, err)
		}
	}
	if paths := stub.paths(); len(paths) == 0 {
		t.Error("no roster was read at all")
	}
}

func TestRoster_String_NamesTheWholeTrip(t *testing.T) {
	stub := newPayerRosterStub(t, payerRosterJSON)

	roster, err := NewPayerResolver(stub.client(), payerTripID).rosterFor(context.Background())
	if err != nil {
		t.Fatalf("rosterFor() error = %v", err)
	}

	want := "3 owner-trek (owner), 8 member-trek (member), 9 guest-trek (member)"
	if got := roster.String(); got != want {
		t.Errorf("roster.String() = %q, want %q", got, want)
	}
	if got := (&Roster{}).String(); got != "nobody" {
		t.Errorf("(&Roster{}).String() = %q, want %q", got, "nobody")
	}
}

type payerBudgetItemBody struct {
	TotalPrice float64     `json:"total_price"`
	Payers     []TrekPayer `json:"payers,omitempty"`
}

func TestPayerInput_RecordsThePayerInThePayersArray(t *testing.T) {
	payers, err := PayerInput(&Payer{UserID: payerMemberID, Nickname: "Bob"}, 220)
	if err != nil {
		t.Fatalf("PayerInput() error = %v", err)
	}

	body, err := json.Marshal(payerBudgetItemBody{TotalPrice: 220, Payers: payers})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if want := `{"total_price":220,"payers":[{"user_id":8,"amount":220}]}`; string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPayerInput_LeavesThePayersKeyOutWhenThereIsNoPayer(t *testing.T) {
	payers, err := PayerInput(nil, 220)
	if err != nil {
		t.Fatalf("PayerInput() error = %v", err)
	}
	if payers != nil {
		t.Errorf("PayerInput() = %v, want nil so the key is left out", payers)
	}

	body, err := json.Marshal(payerBudgetItemBody{TotalPrice: 220, Payers: payers})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if want := `{"total_price":220}`; string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPayerInput_RefusesAnAmountTREKWouldDrop(t *testing.T) {
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		t.Run(strconv.FormatFloat(amount, 'g', -1, 64), func(t *testing.T) {
			payers, err := PayerInput(&Payer{UserID: payerMemberID}, amount)
			if payers != nil {
				t.Errorf("PayerInput(%v) = %v, want no payers", amount, payers)
			}
			if !errors.Is(err, ErrPayerAmountUnset) {
				t.Errorf("PayerInput(%v) error = %v, want ErrPayerAmountUnset", amount, err)
			}
		})
	}
}

func slicesEqual(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
