package user

import (
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/omegaatt36/noccounting/domain"
)

type Service struct {
	userRepo  UserRepo
	tzMu      sync.RWMutex
	timezones map[int64]*time.Location
}

func NewService(userRepo UserRepo) *Service {
	return &Service{
		userRepo:  userRepo,
		timezones: make(map[int64]*time.Location),
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

func (s *Service) Timezone(telegramUserID int64) *time.Location {
	s.tzMu.RLock()
	loc := s.timezones[telegramUserID]
	s.tzMu.RUnlock()
	if loc != nil {
		return loc
	}
	return DefaultTimezone()
}

// Today is the user's calendar day as a UTC date, so it compares with the dates expenses are filed under.
func (s *Service) Today(telegramUserID int64) time.Time {
	local := time.Now().In(s.Timezone(telegramUserID))
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) SetTimezone(telegramUserID int64, loc *time.Location) {
	if loc == nil {
		return
	}
	s.tzMu.Lock()
	s.timezones[telegramUserID] = loc
	s.tzMu.Unlock()
}

func DefaultTimezone() *time.Location {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}
