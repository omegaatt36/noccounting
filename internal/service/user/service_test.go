package user_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	userrepo "github.com/omegaatt36/noccounting/internal/persistence/user"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func TestService_IsAuthorized(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice")
	svc := user.NewService(repo)
	if !svc.IsAuthorized(12345) {
		t.Error("Alice should be authorized")
	}
	if svc.IsAuthorized(99999) {
		t.Error("unknown user should not be authorized")
	}
}

func TestService_GetUser(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice")
	svc := user.NewService(repo)
	telegramID := int64(12345)
	u, err := svc.GetUser(domain.GetUserRequest{TelegramID: &telegramID})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if u.Nickname != "Alice" {
		t.Errorf("Nickname = %q, want %q", u.Nickname, "Alice")
	}
}

func TestService_GetAllUsers(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice,67890:notion-def:Bob")
	svc := user.NewService(repo)
	users, err := svc.GetAllUsers()
	if err != nil {
		t.Fatalf("GetAllUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}
