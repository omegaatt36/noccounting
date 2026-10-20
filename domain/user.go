package domain

type User struct {
	ID         uint
	TelegramID int64
	// BackendUserID is text because the user mapping is text; the mapping parser validates its shape.
	BackendUserID string
	Nickname      string
}

type GetUserRequest struct {
	ID            *uint
	TelegramID    *int64
	BackendUserID *string
}
