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

// telegram_id1:notion_id1:nickname1,telegram_id2:notion_id2:nickname2
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
			return nil, fmt.Errorf("invalid mapping format: %q, expected telegram_id:notion_id[:nickname]", pair)
		}

		telegramID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram ID %q: %w", parts[0], err)
		}

		notionID := strings.TrimSpace(parts[1])
		if notionID == "" {
			return nil, fmt.Errorf("empty notion ID for telegram ID %d", telegramID)
		}

		nickname := ""
		if len(parts) >= 3 {
			nickname = strings.TrimSpace(parts[2])
		}
		if nickname == "" {
			nickname = fmt.Sprintf("User-%d", telegramID%10000)
		}

		users = append(users, domain.User{
			ID:         sequence,
			TelegramID: telegramID,
			NotionID:   notionID,
			Nickname:   nickname,
		})

		sequence++
	}

	return users, nil
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
		if req.NotionID != nil && _user.NotionID == *req.NotionID {
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
