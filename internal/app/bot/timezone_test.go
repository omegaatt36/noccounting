package bot_test

import (
	"context"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/bot"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type mockTimezoneBotContext struct {
	tele.Context
	sender  *tele.User
	message *tele.Message
	cb      *tele.Callback
	sent    []string
}

func (m *mockTimezoneBotContext) Sender() *tele.User {
	return m.sender
}

func (m *mockTimezoneBotContext) Message() *tele.Message {
	return m.message
}

func (m *mockTimezoneBotContext) Callback() *tele.Callback {
	return m.cb
}

func (m *mockTimezoneBotContext) Send(what any, _ ...any) error {
	if s, ok := what.(string); ok {
		m.sent = append(m.sent, s)
	}
	return nil
}

func (m *mockTimezoneBotContext) Respond(_ ...*tele.CallbackResponse) error {
	return nil
}

type dummyTripRepo struct{}

func (d *dummyTripRepo) ListTrips(_ context.Context) ([]domain.Trip, error) {
	return []domain.Trip{{ID: 1, Title: "Trip 1", Currency: domain.CurrencyTWD}}, nil
}

func TestHandler_TimezoneCommandAndCallback(t *testing.T) {
	userRepo := &mockUserRepo{users: []domain.User{{ID: 1, TelegramID: 100, BackendUserID: "1", Nickname: "Alice"}}}
	userService := user.NewService(userRepo)
	tripService := trip.NewService(&dummyTripRepo{})
	expenseService := expense.NewService(nil, nil, nil)

	handler := bot.NewHandler(userService, expenseService, tripService, "")
	b, err := tele.NewBot(tele.Settings{Offline: true})
	if err != nil {
		t.Fatalf("failed to create bot: %v", err)
	}
	handler.RegisterHandlers(b)

	// 1. /tz with payload "Asia/Tokyo"
	ctx1 := &mockTimezoneBotContext{
		sender:  &tele.User{ID: 100},
		message: &tele.Message{Text: "/tz Asia/Tokyo", Payload: "Asia/Tokyo"},
	}
	if err := b.Trigger("/tz", ctx1); err != nil {
		t.Fatalf("/tz error: %v", err)
	}
	if len(ctx1.sent) == 0 || !strings.Contains(ctx1.sent[0], "Asia/Tokyo") {
		t.Errorf("expected confirmation of Asia/Tokyo, got %v", ctx1.sent)
	}

	// 2. /tz with payload "tw"
	ctx2 := &mockTimezoneBotContext{
		sender:  &tele.User{ID: 100},
		message: &tele.Message{Text: "/tz tw", Payload: "tw"},
	}
	if err := b.Trigger("/tz", ctx2); err != nil {
		t.Fatalf("/tz error: %v", err)
	}
	if len(ctx2.sent) == 0 || !strings.Contains(ctx2.sent[0], "Asia/Taipei") {
		t.Errorf("expected confirmation of Asia/Taipei, got %v", ctx2.sent)
	}

	// 3. /tz with invalid timezone
	ctx3 := &mockTimezoneBotContext{
		sender:  &tele.User{ID: 100},
		message: &tele.Message{Text: "/tz Mars/City", Payload: "Mars/City"},
	}
	if err := b.Trigger("/tz", ctx3); err != nil {
		t.Fatalf("/tz error: %v", err)
	}
	if len(ctx3.sent) == 0 || !strings.Contains(ctx3.sent[0], "無法辨識") {
		t.Errorf("expected error for invalid timezone, got %v", ctx3.sent)
	}

	// 4. Callback "tz|Asia/Tokyo"
	ctx4 := &mockTimezoneBotContext{
		sender: &tele.User{ID: 100},
		cb:     &tele.Callback{Data: "tz|Asia/Tokyo"},
	}
	if err := b.Trigger(tele.OnCallback, ctx4); err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if len(ctx4.sent) == 0 || !strings.Contains(ctx4.sent[0], "Asia/Tokyo") {
		t.Errorf("expected callback confirmation of Asia/Tokyo, got %v", ctx4.sent)
	}
}

type mockUserRepo struct {
	users []domain.User
}

func (m *mockUserRepo) GetUser(req domain.GetUserRequest) (*domain.User, error) {
	for _, u := range m.users {
		if req.TelegramID != nil && u.TelegramID == *req.TelegramID {
			return &u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (m *mockUserRepo) GetUsers() ([]domain.User, error) {
	return m.users, nil
}
