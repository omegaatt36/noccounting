package user_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
)

func TestRepo_GetUser_ByTelegramID(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice,67890:notion-def:Bob")
	telegramID := int64(12345)
	user, err := repo.GetUser(domain.GetUserRequest{TelegramID: &telegramID})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.Nickname != "Alice" {
		t.Errorf("Nickname = %q, want %q", user.Nickname, "Alice")
	}
	if user.NotionID != "notion-abc" {
		t.Errorf("NotionID = %q, want %q", user.NotionID, "notion-abc")
	}
}

func TestRepo_GetUser_NotFound(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice")
	telegramID := int64(99999)
	_, err := repo.GetUser(domain.GetUserRequest{TelegramID: &telegramID})
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestRepo_GetUsers(t *testing.T) {
	repo := userrepo.NewRepo("12345:notion-abc:Alice,67890:notion-def:Bob")
	users, err := repo.GetUsers()
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}
