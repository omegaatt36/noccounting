package domain

import "time"

type Trip struct {
	ID    int64
	Title string
	// Currency is the accounting base.
	Currency Currency
	// StartDate and EndDate are the trip's days, zero for an undated trip.
	StartDate time.Time
	EndDate   time.Time
}

func (t Trip) HasDates() bool {
	return !t.StartDate.IsZero() && !t.EndDate.IsZero()
}

func (t Trip) Contains(at time.Time) bool {
	if !t.HasDates() {
		return false
	}
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	return !day.Before(t.StartDate) && !day.After(t.EndDate)
}

type Member struct {
	ID   string
	Name string
}
