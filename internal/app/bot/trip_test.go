package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

var (
	tokyoTrip = domain.Trip{ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyTWD}
	osakaTrip = domain.Trip{ID: 9, Title: "Osaka", Currency: domain.CurrencyJPY}
)

type twoTrips struct{}

func (twoTrips) ListTrips(context.Context) ([]domain.Trip, error) {
	return []domain.Trip{tokyoTrip, osakaTrip}, nil
}

func newTwoTripHandler(repo *spyAccountingRepo) (*Handler, *spyAccountingRepo) {
	userRepo := &fakeUserRepo{users: map[int64]*domain.User{
		testTelegramUserID: {ID: 1, TelegramID: testTelegramUserID, BackendUserID: testBackendUserID, Nickname: "test-user"},
	}}

	h := NewHandler(user.NewService(userRepo), expense.NewService(repo, &stubRateFetcher{}, nil), trip.NewService(twoTrips{}), "")
	return h, repo
}

func newCommandContext(args ...string) *spyContext {
	return &spyContext{sender: &tele.User{ID: testTelegramUserID}, message: &tele.Message{}, bot: &spyBotAPI{}, args: args}
}

func lastMessage(t *testing.T, c *spyContext) string {
	t.Helper()
	if len(c.sentMsgs) == 0 {
		t.Fatal("expected a message to be sent")
	}
	msg, ok := c.sentMsgs[len(c.sentMsgs)-1].(string)
	if !ok {
		t.Fatalf("last message is %T, want a string", c.sentMsgs[len(c.sentMsgs)-1])
	}
	return msg
}

func TestHandleTrip_OffersEveryTripAndMarksTheCurrentOne(t *testing.T) {
	h, _ := newTwoTripHandler(&spyAccountingRepo{})
	c := newCommandContext()

	if err := h.handleTrip(c); err != nil {
		t.Fatalf("handleTrip() error = %v", err)
	}

	if msg := lastMessage(t, c); !strings.Contains(msg, "2026 Tokyo") {
		t.Errorf("message = %q, want the current trip named", msg)
	}

	markup, ok := c.sentOpts[0][0].(*tele.ReplyMarkup)
	if !ok || len(markup.InlineKeyboard) != 2 {
		t.Fatalf("options = %#v, want a keyboard with a button per trip", c.sentOpts[0])
	}
	if !strings.Contains(markup.InlineKeyboard[1][0].Text, "Osaka") {
		t.Errorf("second button = %q, want the other trip offered", markup.InlineKeyboard[1][0].Text)
	}
	// Neither trip is dated, so the list is by id and the first is the current.
	if !strings.HasPrefix(markup.InlineKeyboard[0][0].Text, "✅ ") {
		t.Errorf("first button = %q, want the current trip marked", markup.InlineKeyboard[0][0].Text)
	}
	if strings.HasPrefix(markup.InlineKeyboard[1][0].Text, "✅ ") {
		t.Errorf("second button = %q, want only the current trip marked", markup.InlineKeyboard[1][0].Text)
	}
}

func TestHandleTripCallback_SwitchesTheTripForThatPerson(t *testing.T) {
	repo := &spyAccountingRepo{}
	h, ledger := newTwoTripHandler(repo)

	switchCtx := newCallbackContext(&spyBotAPI{}, "trip|9")
	if err := h.handleCallback(switchCtx); err != nil {
		t.Fatalf("handleCallback() error = %v", err)
	}
	if msg := lastMessage(t, switchCtx); !strings.Contains(msg, "Osaka") {
		t.Errorf("message = %q, want the new trip named", msg)
	}

	todayCtx := newCommandContext()
	if err := h.handleToday(todayCtx); err != nil {
		t.Fatalf("handleToday() error = %v", err)
	}
	if len(ledger.trips) != 1 || ledger.trips[0].ID != osakaTrip.ID {
		t.Errorf("books opened for %+v, want the trip just chosen", ledger.trips)
	}
}

func TestHandleTripCallback_RefusesATripThatIsGone(t *testing.T) {
	h, _ := newTwoTripHandler(&spyAccountingRepo{})

	c := newCallbackContext(&spyBotAPI{}, "trip|404")
	if err := h.handleCallback(c); err != nil {
		t.Fatalf("handleCallback() error = %v", err)
	}
	if len(c.responded) != 1 || !strings.Contains(c.responded[0].Text, "不在清單") {
		t.Errorf("responses = %+v, want the trip reported as gone", c.responded)
	}
}

func TestHandleSummary_ReportsTheSettlementUnderTheNicknames(t *testing.T) {
	repo := &spyAccountingRepo{settlement: &domain.Settlement{
		Currency: domain.CurrencyTWD,
		Balances: []domain.Balance{{UserID: testBackendUserID, Name: "bob", Amount: decimalOf(-1500)}},
		Transfers: []domain.Transfer{
			{FromID: testBackendUserID, FromName: "bob", ToID: "3", ToName: "owner-trek", Amount: decimalOf(1500)},
		},
	}}
	h, _ := newTwoTripHandler(repo)
	c := newCommandContext()

	if err := h.handleSummary(c); err != nil {
		t.Fatalf("handleSummary() error = %v", err)
	}

	msg := lastMessage(t, c)
	if !strings.Contains(msg, "test-user → owner-trek  NT$1,500") {
		t.Errorf("message = %q, want the transfer under the mapped nickname", msg)
	}
}

func decimalOf(n int64) decimal.Decimal { return decimal.NewFromInt(n) }
