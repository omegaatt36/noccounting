package webapp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// The Mini App sends init_data in query strings, and while it is valid it speaks
// for the person who opened the app. Whatever the server logs about a request,
// that must not be in it, whether the request succeeds or is refused.
func TestRequestsNeverLogInitData(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	const botToken = "test-token"
	member := &domain.User{ID: 1, TelegramID: 111, BackendUserID: "8", Nickname: "Ann"}
	users := user.NewService(&fakeUserRepo{users: map[int64]*domain.User{member.TelegramID: member}})
	expenses := expense.NewService(&stubAccountingRepo{}, nil, nil)
	server, err := NewServer(users, expenses, newTestTrips(), "0", botToken, false)
	if err != nil {
		t.Fatalf("creating the server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	handler := server.Handler()

	signed := func(telegramID, name string) string {
		return buildValidTelegramInitData(botToken, map[string]string{
			"query_id": "AAHdF6IQAAAAAAAA",
			"user":     `{"id":` + telegramID + `,"first_name":"` + name + `"}`,
		})
	}

	valid := signed("111", "LeakCheckMember")
	stranger := signed("999", "LeakCheckStranger")
	forged := valid[:len(valid)-4] + "0000"

	paths := []string{
		"/api/auth",
		"/api/users",
		"/api/trips",
		"/api/members?trip_id=3",
		"/api/rates",
		"/partial/dashboard?range=all&trip_id=3",
		"/partial/dashboard/category?name=food&range=all&trip_id=3",
		"/api/export/csv?range=all&trip_id=3",
	}

	var secrets []string
	for _, initData := range []string{valid, stranger, forged} {
		secrets = append(secrets, initData, url.QueryEscape(initData))
		for _, path := range paths {
			separator := "?"
			if strings.Contains(path, "?") {
				separator = "&"
			}
			req := httptest.NewRequest(http.MethodGet, path+separator+"init_data="+url.QueryEscape(initData), nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}
	}
	// The signature and the profile inside are as sensitive as the whole string.
	secrets = append(secrets, "LeakCheckMember", "LeakCheckStranger", "AAHdF6IQAAAAAAAA", valid[strings.Index(valid, "hash=")+5:])

	if logs.Len() == 0 {
		t.Fatal("nothing was logged, so this test proves nothing")
	}
	for _, secret := range secrets {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the log contains %q:\n%s", secret, logs.String())
		}
	}
}
