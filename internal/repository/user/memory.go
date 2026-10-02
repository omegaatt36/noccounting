package user

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/omegaatt36/noccounting/domain"
)

type Repo struct {
	users []domain.User
}

func NewRepo(mappedUserString string) *Repo {
	users, err := parseUsers(mappedUserString)
	if err != nil {
		panic(err)
	}
	return &Repo{
		users: users,
	}
}

// parseUsers parses USER_MAPPING as
// telegram_id:backend_user_id[:nickname],… — comma-separated. The backend id
// is validated here, at the one point the mapping is read: a mapping that
// cannot name a backend user is a startup mistake, not an error to repeat per
// expense.
func parseUsers(s string) ([]domain.User, error) {
	var users []domain.User

	if s == "" {
		return users, nil
	}

	var sequence uint = 1

	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, ":", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid mapping format: %q, expected telegram_id:backend_user_id[:nickname]", pair)
		}

		telegramID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram ID %q: %w", parts[0], err)
		}

		backendUserID := strings.TrimSpace(parts[1])
		if backendUserID == "" {
			return nil, fmt.Errorf("empty backend user ID for telegram ID %d", telegramID)
		}
		if err := validateBackendUserID(backendUserID); err != nil {
			return nil, fmt.Errorf("invalid backend user ID for telegram ID %d: %w", telegramID, err)
		}

		nickname := ""
		if len(parts) >= 3 {
			nickname = strings.TrimSpace(parts[2])
		}
		if nickname == "" {
			nickname = fmt.Sprintf("User-%d", telegramID%10000)
		}

		users = append(users, domain.User{
			ID:            sequence,
			TelegramID:    telegramID,
			BackendUserID: backendUserID,
			Nickname:      nickname,
		})

		sequence++
	}

	return users, nil
}

// validateBackendUserID refuses a mapped id that is not a positive decimal
// number — the only shape a TREK user id has.
func validateBackendUserID(id string) error {
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a number", id)
	}
	if parsed <= 0 {
		return fmt.Errorf("%q is not a positive number", id)
	}
	return nil
}

func (r *Repo) GetUser(req domain.GetUserRequest) (*domain.User, error) {
	var user *domain.User
	for _, _user := range r.users {
		if req.ID != nil && _user.ID == *req.ID {
			user = &_user
			break
		}
		if req.TelegramID != nil && _user.TelegramID == *req.TelegramID {
			user = &_user
			break
		}
		if req.BackendUserID != nil && _user.BackendUserID == *req.BackendUserID {
			user = &_user
			break
		}
	}

	if user == nil {
		return nil, domain.ErrUserNotFound
	}

	return user, nil
}

func (r *Repo) GetUsers() ([]domain.User, error) {
	users := make([]domain.User, len(r.users))
	copy(users, r.users)

	return users, nil
}
