package domain_test

import (
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

func day(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
}

func TestTrip_Contains(t *testing.T) {
	trip := domain.Trip{StartDate: day(time.October, 1), EndDate: day(time.October, 9)}

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"the first day", time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), true},
		{"the last day, late in it", time.Date(2026, time.October, 9, 23, 59, 0, 0, time.UTC), true},
		{"the day before", time.Date(2026, time.September, 30, 23, 59, 0, 0, time.UTC), false},
		{"the day after", time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trip.Contains(tt.at); got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}

	if (domain.Trip{}).Contains(day(time.October, 3)) {
		t.Error("an undated trip contains no day")
	}
}
