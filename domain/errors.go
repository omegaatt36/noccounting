package domain

import "errors"

var ErrUserNotFound = errors.New("user not found")

// ErrNotOnTrip: the backend would silently drop a payer or participant who is not on the trip.
var ErrNotOnTrip = errors.New("not a member of the trip")

var ErrUnsupportedSplit = errors.New("custom split edits are not supported")

var ErrExpenseNotFound = errors.New("expense not found")

var ErrUnsupportedCurrency = errors.New("unsupported currency")
