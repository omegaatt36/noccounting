package trip

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

var ErrNoTrip = errors.New("the service account is on no trip noccounting can use")

var ErrTripNotFound = errors.New("the trip is not one the service account is on")

const listTTL = time.Minute

// Trip selections live in memory and reset on restart.
type Service struct {
	repo Repository
	now  func() time.Time

	mu       sync.Mutex
	trips    []domain.Trip
	fetched  time.Time
	selected map[int64]int64 // person (Telegram id) -> trip id
}

func NewService(repo Repository) *Service {
	return &Service{
		repo:     repo,
		now:      time.Now,
		selected: make(map[int64]int64),
	}
}

func (s *Service) List(ctx context.Context) ([]domain.Trip, error) {
	trips, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	ordered := slices.Clone(trips)
	slices.SortStableFunc(ordered, func(a, b domain.Trip) int {
		return cmp.Or(
			cmp.Compare(s.distance(a), s.distance(b)),
			a.StartDate.Compare(b.StartDate),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return ordered, nil
}

func (s *Service) Current(ctx context.Context, person int64) (domain.Trip, error) {
	trips, err := s.List(ctx)
	if err != nil {
		return domain.Trip{}, err
	}
	if len(trips) == 0 {
		return domain.Trip{}, ErrNoTrip
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if chosen, ok := s.selected[person]; ok {
		for _, trip := range trips {
			if trip.ID == chosen {
				return trip, nil
			}
		}
		// The trip ended and the account was taken off it.
		delete(s.selected, person)
	}
	return trips[0], nil
}

func (s *Service) Get(ctx context.Context, tripID int64) (domain.Trip, error) {
	trips, err := s.list(ctx)
	if err != nil {
		return domain.Trip{}, err
	}

	for _, trip := range trips {
		if trip.ID == tripID {
			return trip, nil
		}
	}
	return domain.Trip{}, fmt.Errorf("%w: %d", ErrTripNotFound, tripID)
}

func (s *Service) Select(ctx context.Context, person, tripID int64) (domain.Trip, error) {
	selected, err := s.Get(ctx, tripID)
	if err != nil {
		return domain.Trip{}, err
	}
	s.mu.Lock()
	s.selected[person] = selected.ID
	s.mu.Unlock()
	return selected, nil
}

func (s *Service) list(ctx context.Context) ([]domain.Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.trips != nil && s.now().Sub(s.fetched) < listTTL {
		return s.trips, nil
	}

	trips, err := s.repo.ListTrips(ctx)
	if err != nil {
		return nil, err
	}
	if trips == nil {
		trips = []domain.Trip{}
	}
	s.trips, s.fetched = trips, s.now()
	return trips, nil
}

// Ongoing trips rank first, undated trips last, others by distance from today.
func (s *Service) distance(trip domain.Trip) time.Duration {
	if !trip.HasDates() {
		return time.Duration(1<<62 - 1)
	}

	now := s.now()
	if trip.Contains(now) {
		return 0
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if trip.StartDate.After(today) {
		return trip.StartDate.Sub(today)
	}
	return today.Sub(trip.EndDate)
}
