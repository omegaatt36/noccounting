package user_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
)

func TestRepo_GetUser_ByTelegramID(t *testing.T) {
	repo := userrepo.NewRepo("12345:8:Alice,67890:9:Bob")
	telegramID := int64(12345)
	user, err := repo.GetUser(domain.GetUserRequest{TelegramID: &telegramID})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.Nickname != "Alice" {
		t.Errorf("Nickname = %q, want %q", user.Nickname, "Alice")
	}
	if user.BackendUserID != "8" {
		t.Errorf("BackendUserID = %q, want %q", user.BackendUserID, "8")
	}
}

func TestRepo_GetUser_ByBackendUserID(t *testing.T) {
	repo := userrepo.NewRepo("12345:8:Alice,67890:9:Bob")
	backendUserID := "9"
	user, err := repo.GetUser(domain.GetUserRequest{BackendUserID: &backendUserID})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.Nickname != "Bob" {
		t.Errorf("Nickname = %q, want %q", user.Nickname, "Bob")
	}
}

func TestRepo_GetUser_NotFound(t *testing.T) {
	repo := userrepo.NewRepo("12345:8:Alice")
	telegramID := int64(99999)
	_, err := repo.GetUser(domain.GetUserRequest{TelegramID: &telegramID})
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestRepo_GetUsers(t *testing.T) {
	repo := userrepo.NewRepo("12345:8:Alice,67890:9:Bob")
	users, err := repo.GetUsers()
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

// TestNewRepo_AcceptsANumericBackendUserID pins that the mapping's format is
// unchanged: a decimal id still parses, and it is stored as written.
func TestNewRepo_AcceptsANumericBackendUserID(t *testing.T) {
	repo := userrepo.NewRepo("123456789:8:Alice")
	users, err := repo.GetUsers()
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].TelegramID != 123456789 {
		t.Errorf("TelegramID = %d, want %d", users[0].TelegramID, 123456789)
	}
	if users[0].BackendUserID != "8" {
		t.Errorf("BackendUserID = %q, want %q", users[0].BackendUserID, "8")
	}
	if users[0].Nickname != "Alice" {
		t.Errorf("Nickname = %q, want %q", users[0].Nickname, "Alice")
	}
}

// TestNewRepo_RejectsANonNumericBackendUserID is the loud half of the rename: a
// mapping left over from the retired backend refuses to start rather than being
// stored and refused once per expense afterwards.
func TestNewRepo_RejectsANonNumericBackendUserID(t *testing.T) {
	tests := []struct {
		name     string
		mapping  string
		wantInIt string
	}{
		{
			name:     "a leftover retired-backend uuid",
			mapping:  "12345:9f8e7d6c-5b4a-3210-fedc-ba9876543210:Alice",
			wantInIt: "not a number",
		},
		{
			name:     "an opaque non-numeric id",
			mapping:  "12345:abc:Alice",
			wantInIt: "not a number",
		},
		{
			name:     "zero",
			mapping:  "12345:0:Alice",
			wantInIt: "not a positive number",
		},
		{
			name:     "a negative id",
			mapping:  "12345:-7:Alice",
			wantInIt: "not a positive number",
		},
		{
			name:     "an empty id",
			mapping:  "12345::Alice",
			wantInIt: "empty backend user ID",
		},
		{
			name:     "one bad entry among good ones",
			mapping:  "12345:8:Alice,67890:nope:Bob",
			wantInIt: "67890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := panics(func() { userrepo.NewRepo(tt.mapping) })
			if err == nil {
				t.Fatalf("NewRepo(%q) started; want a refusal", tt.mapping)
			}
			if !strings.Contains(err.Error(), tt.wantInIt) {
				t.Errorf("NewRepo(%q) refused with %q, want it to mention %q", tt.mapping, err, tt.wantInIt)
			}
		})
	}
}

// panics returns what f panicked with as an error, or nil when it returned.
func panics(f func()) (err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if recoveredErr, ok := recovered.(error); ok {
			err = recoveredErr
			return
		}
		err = fmt.Errorf("%v", recovered)
	}()
	f()
	return nil
}
