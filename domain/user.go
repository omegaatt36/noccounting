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

// UserRepo defines the interface for user management.
type UserRepo interface {
	GetUser(req GetUserRequest) (*User, error)
	GetUsers() ([]User, error)
}
