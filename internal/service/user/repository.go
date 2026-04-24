package user

import "github.com/omegaatt36/noccounting/domain"

// UserRepo defines the interface for user data access.
// Defined here as the consumer (service/user) rather than in domain.
type UserRepo interface {
	GetUser(req domain.GetUserRequest) (*domain.User, error)
	GetUsers() ([]domain.User, error)
}
