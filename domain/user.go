package domain

// User is someone the Telegram bot recognizes, mapped onto a backend user id.
type User struct {
	ID         uint
	TelegramID int64
	// BackendUserID is the id the system of record knows this user by, spelled
	// the way the user mapping spells it. It is a string because the mapping is
	// text; what a given backend requires of that text is the mapping
	// parser's business to enforce, not this type's.
	BackendUserID string
	Nickname      string
}

type GetUserRequest struct {
	ID            *uint
	TelegramID    *int64
	BackendUserID *string
}
