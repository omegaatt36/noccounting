package user

import "github.com/omegaatt36/noccounting/domain"

type Service struct {
	userRepo domain.UserRepo
}

func NewService(userRepo domain.UserRepo) *Service {
	return &Service{
		userRepo: userRepo,
	}
}

func (s *Service) GetUser(request domain.GetUserRequest) (*domain.User, error) {
	return s.userRepo.GetUser(request)
}

func (s *Service) GetAllUsers() ([]domain.User, error) {
	return s.userRepo.GetUsers()
}

func (s *Service) IsAuthorized(telegramUserID int64) bool {
	user, err := s.userRepo.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		return false
	}

	return user != nil
}
