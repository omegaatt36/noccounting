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

	listCtx := newCommandContext()
	if err := h.handleList(listCtx); err != nil {
		t.Fatalf("handleList() error = %v", err)
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

// A conversation files under the trip it started with: switching halfway through
// must not move the expense to a trip the person never confirmed it for.
func TestQuickFlow_FilesUnderTheTripItStartedWith(t *testing.T) {
	repo := &spyAccountingRepo{}
	h, ledger := newTwoTripHandler(repo)

	if err := h.handleQuick(newCommandContext()); err != nil {
		t.Fatalf("handleQuick() error = %v", err)
	}
	state := h.convManager.GetState(testTelegramUserID)
	if state == nil || state.Trip.ID != tokyoTrip.ID {
		t.Fatalf("state = %+v, want the flow started under the default trip", state)
	}
	state.ExpenseDraft.Name = "拉麵"
	state.ExpenseDraft.Price = 1200
	state.ExpenseDraft.Currency = domain.CurrencyTWD
	state.ExpenseDraft.Category = domain.CategoryFood
	state.ExpenseDraft.Method = domain.PaymentMethodCash

	if err := h.handleCallback(newCallbackContext(&spyBotAPI{}, "trip|9")); err != nil {
		t.Fatalf("switching trip: %v", err)
	}
	if err := h.handleCallback(newCallbackContext(&spyBotAPI{}, "confirm|yes")); err != nil {
		t.Fatalf("confirming: %v", err)
	}

	if len(repo.createdExpenses) != 1 {
		t.Fatalf("created %d expenses, want 1", len(repo.createdExpenses))
	}
	if last := ledger.trips[len(ledger.trips)-1]; last.ID != tokyoTrip.ID {
		t.Errorf("the expense was filed under trip %d, want %d, where the flow started", last.ID, tokyoTrip.ID)
	}
}

func TestHandleList_FiltersByPaymentMethod(t *testing.T) {
	repo := &spyAccountingRepo{}
	h, _ := newTwoTripHandler(repo)

	if err := h.handleList(newCommandContext("信用卡")); err != nil {
		t.Fatalf("handleList() error = %v", err)
	}

	if repo.lastFilter.Method == nil || *repo.lastFilter.Method != domain.PaymentMethodCreditCard {
		t.Errorf("filter method = %v, want credit_card read off the label", repo.lastFilter.Method)
	}
}

func TestHandleList_RefusesAMethodItDoesNotKnow(t *testing.T) {
	repo := &spyAccountingRepo{}
	h, ledger := newTwoTripHandler(repo)
	c := newCommandContext("bitcoin")

	if err := h.handleList(c); err != nil {
		t.Fatalf("handleList() error = %v", err)
	}
	if !strings.Contains(lastMessage(t, c), "付款方式錯誤") {
		t.Errorf("message = %q, want the method refused", lastMessage(t, c))
	}
	if len(ledger.trips) != 0 {
		t.Error("the trip's books were opened for a query that was never going to run")
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
	// The user mapping names user 8 test-user; user 3 is not in it and keeps the
	// name TREK gave.
	if !strings.Contains(msg, "test-user → owner-trek  NT$1,500") {
		t.Errorf("message = %q, want the transfer under the mapped nickname", msg)
	}
}

func decimalOf(n int64) decimal.Decimal { return decimal.NewFromInt(n) }
