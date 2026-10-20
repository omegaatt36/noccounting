package user

import "github.com/omegaatt36/noccounting/domain"

type UserRepo interface {
	GetUser(req domain.GetUserRequest) (*domain.User, error)
	GetUsers() ([]domain.User, error)
}
