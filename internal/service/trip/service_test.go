package trip

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

type stubRepo struct {
	trips []domain.Trip
	err   error
	calls int
}

func (r *stubRepo) ListTrips(context.Context) ([]domain.Trip, error) {
	r.calls++
	return r.trips, r.err
}

func day(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
}

func dated(id int64, from, to time.Time) domain.Trip {
	return domain.Trip{ID: id, Title: "trip", Currency: domain.CurrencyTWD, StartDate: from, EndDate: to}
}

func serviceAt(repo Repository, now time.Time) *Service {
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	return svc
}

func ids(trips []domain.Trip) []int64 {
	got := make([]int64, 0, len(trips))
	for _, trip := range trips {
		got = append(got, trip.ID)
	}
	return got
}

func TestList_PutsTheTripUnderWayFirst(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{
		{ID: 5, Title: "someday"},                              // undated
		dated(4, day(time.December, 1), day(time.December, 9)), // 2 months out
		dated(3, day(time.August, 1), day(time.August, 9)),     // finished
		dated(2, day(time.October, 1), day(time.October, 9)),   // under way
		dated(1, day(time.October, 20), day(time.October, 25)), // two weeks out
	}}
	svc := serviceAt(repo, time.Date(2026, time.October, 3, 15, 0, 0, 0, time.Local))

	trips, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	want := []int64{2, 1, 3, 4, 5}
	if got := ids(trips); !equal(got, want) {
		t.Errorf("List() order = %v, want %v: under way, then by distance from today, undated last", got, want)
	}
}

func TestCurrent_DefaultsToTheTripUnderWay(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{
		dated(1, day(time.October, 20), day(time.October, 25)),
		dated(2, day(time.October, 1), day(time.October, 9)),
	}}
	svc := serviceAt(repo, day(time.October, 3))

	trip, err := svc.Current(context.Background(), 100)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if trip.ID != 2 {
		t.Errorf("Current() = trip %d, want 2, the one under way", trip.ID)
	}
}

func TestCurrent_FollowsASelectionPerPerson(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{
		dated(1, day(time.October, 1), day(time.October, 9)),
		dated(2, day(time.October, 20), day(time.October, 25)),
	}}
	svc := serviceAt(repo, day(time.October, 3))
	ctx := context.Background()

	if _, err := svc.Select(ctx, 100, 2); err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	chosen, _ := svc.Current(ctx, 100)
	other, _ := svc.Current(ctx, 200)
	if chosen.ID != 2 {
		t.Errorf("the person who chose trip 2 is on trip %d", chosen.ID)
	}
	if other.ID != 1 {
		t.Errorf("a person who chose nothing is on trip %d, want the default 1", other.ID)
	}
}

func TestCurrent_FallsBackWhenTheChosenTripIsGone(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{
		dated(1, day(time.October, 1), day(time.October, 9)),
		dated(2, day(time.October, 20), day(time.October, 25)),
	}}
	now := day(time.October, 3)
	svc := serviceAt(repo, now)
	ctx := context.Background()

	if _, err := svc.Select(ctx, 100, 2); err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	// The operator takes the account off trip 2, and the cached list lapses.
	repo.trips = repo.trips[:1]
	svc.now = func() time.Time { return now.Add(2 * listTTL) }

	trip, err := svc.Current(ctx, 100)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if trip.ID != 1 {
		t.Errorf("Current() = trip %d, want the default 1 once trip 2 is gone", trip.ID)
	}
}

func TestSelect_RefusesATripTheAccountIsNotOn(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{dated(1, day(time.October, 1), day(time.October, 9))}}
	svc := serviceAt(repo, day(time.October, 3))

	if _, err := svc.Select(context.Background(), 100, 99); !errors.Is(err, ErrTripNotFound) {
		t.Errorf("Select() error = %v, want ErrTripNotFound", err)
	}
}

func TestCurrent_NoTrips(t *testing.T) {
	svc := serviceAt(&stubRepo{}, day(time.October, 3))

	if _, err := svc.Current(context.Background(), 100); !errors.Is(err, ErrNoTrip) {
		t.Errorf("Current() error = %v, want ErrNoTrip", err)
	}
}

func TestList_ReadsTheBackendOncePerTTL(t *testing.T) {
	repo := &stubRepo{trips: []domain.Trip{dated(1, day(time.October, 1), day(time.October, 9))}}
	now := day(time.October, 3)
	svc := serviceAt(repo, now)
	ctx := context.Background()

	for range 3 {
		if _, err := svc.List(ctx); err != nil {
			t.Fatalf("List() error = %v", err)
		}
	}
	if repo.calls != 1 {
		t.Errorf("the backend was read %d times inside the TTL, want 1", repo.calls)
	}

	svc.now = func() time.Time { return now.Add(2 * listTTL) }
	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if repo.calls != 2 {
		t.Errorf("the backend was read %d times after the TTL lapsed, want 2", repo.calls)
	}
}

func TestList_ReportsABackendFailure(t *testing.T) {
	boom := errors.New("boom")
	svc := serviceAt(&stubRepo{err: boom}, day(time.October, 3))

	if _, err := svc.List(context.Background()); !errors.Is(err, boom) {
		t.Errorf("List() error = %v, want the backend's", err)
	}
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGet_DoesNotChangeSelection(t *testing.T) {
	svc := serviceAt(&stubRepo{trips: []domain.Trip{
		dated(1, day(time.October, 1), day(time.October, 9)),
		dated(2, day(time.October, 20), day(time.October, 25)),
	}}, day(time.October, 3))
	ctx := context.Background()
	if _, err := svc.Select(ctx, 100, 1); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, 2)
	if err != nil || got.ID != 2 {
		t.Fatalf("Get() = %+v, %v; want trip 2", got, err)
	}
	current, err := svc.Current(ctx, 100)
	if err != nil || current.ID != 1 {
		t.Fatalf("Current() = %+v, %v; want selection unchanged", current, err)
	}
}

func TestGet_RejectsUnavailableTrip(t *testing.T) {
	svc := serviceAt(&stubRepo{trips: []domain.Trip{{ID: 1}}}, day(time.October, 3))
	for _, id := range []int64{0, -1, 99} {
		if _, err := svc.Get(context.Background(), id); !errors.Is(err, ErrTripNotFound) {
			t.Errorf("Get(%d) error = %v, want ErrTripNotFound", id, err)
		}
	}
}

func TestGet_ReportsBackendFailure(t *testing.T) {
	boom := errors.New("backend unavailable")
	svc := NewService(&stubRepo{err: boom})
	if _, err := svc.Get(context.Background(), 1); !errors.Is(err, boom) {
		t.Errorf("Get() error = %v, want backend error", err)
	}
}
