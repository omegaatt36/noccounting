package domain

// User represents a user in the system.
type User struct {
	ID         uint
	TelegramID int64
	NotionID   string
	Nickname   string
}

type GetUserRequest struct {
	ID         *uint
	TelegramID *int64
	NotionID   *string
}
