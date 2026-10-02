package domain

import "errors"

var ErrUserNotFound = errors.New("user not found")

// ErrNotOnTrip reports someone an expense names, as its payer or as one of the
// people it is shared with, who is not on the trip it is filed under. The
// backend would otherwise drop them without a word and keep an expense nobody
// owes anything for.
var ErrNotOnTrip = errors.New("not a member of the trip")

var ErrUnsupportedSplit = errors.New("custom split edits are not supported")
