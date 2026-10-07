package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func TestHandleStart(t *testing.T) {
	repo := &spyAccountingRepo{}
	handler := newTestHandler(repo, nil)

	t.Run("without webapp url", func(t *testing.T) {
		ctx := &spyContext{
			sender:  &tele.User{ID: testTelegramUserID},
			message: &tele.Message{},
		}
		if err := handler.handleStart(ctx); err != nil {
			t.Fatalf("handleStart error: %v", err)
		}
		if len(ctx.sentMsgs) != 1 {
			t.Fatalf("sent %d messages, want 1", len(ctx.sentMsgs))
		}
		msg := ctx.sentMsgs[0].(string)
		if !strings.Contains(msg, "旅行記帳 Bot") || !strings.Contains(msg, "2026 Tokyo") {
			t.Errorf("start message = %q, want greeting and current trip", msg)
		}
	})

	t.Run("with webapp url", func(t *testing.T) {
		userRepo := &fakeUserRepo{
			users: map[int64]*domain.User{
				testTelegramUserID: {ID: 1, TelegramID: testTelegramUserID, BackendUserID: testBackendUserID, Nickname: "test-user"},
			},
		}
		userSvc := user.NewService(userRepo)
		expenseSvc := expense.NewService(repo, &stubRateFetcher{}, nil)
		tripSvc := trip.NewService(singleTrip{})
		hWithURL := NewHandler(userSvc, expenseSvc, tripSvc, "https://example.com/app")

		ctx := &spyContext{
			sender:  &tele.User{ID: testTelegramUserID},
			message: &tele.Message{},
		}
		if err := hWithURL.handleStart(ctx); err != nil {
			t.Fatalf("handleStart error: %v", err)
		}
		if len(ctx.sentOpts) != 1 || len(ctx.sentOpts[0]) != 1 {
			t.Fatalf("options = %+v, want webapp keyboard", ctx.sentOpts)
		}
		markup, ok := ctx.sentOpts[0][0].(*tele.ReplyMarkup)
		if !ok || len(markup.InlineKeyboard) != 1 || markup.InlineKeyboard[0][0].WebApp == nil {
			t.Errorf("markup = %+v, want webapp button", markup)
		}
	})
}

func TestHandleRate(t *testing.T) {
	repo := &spyAccountingRepo{}
	handler := newTestHandler(repo, nil)
	ctx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{},
	}
	if err := handler.handleRate(ctx); err != nil {
		t.Fatalf("handleRate error: %v", err)
	}
	if len(ctx.sentMsgs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(ctx.sentMsgs))
	}
	msg := ctx.sentMsgs[0].(string)
	if !strings.Contains(msg, "匯率") && !strings.Contains(msg, "TWD") && !strings.Contains(msg, "JPY") {
		t.Errorf("rate message = %q, want exchange rate table", msg)
	}
}

func TestHandleCancel(t *testing.T) {
	repo := &spyAccountingRepo{}
	handler := newTestHandler(repo, nil)
	handler.convManager.StartEditFlow(testTelegramUserID, testTrip, &domain.Expense{ID: "exp-1"})

	ctx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{},
	}
	if err := handler.handleCancel(ctx); err != nil {
		t.Fatalf("handleCancel error: %v", err)
	}
	if len(ctx.sentMsgs) != 1 || !strings.Contains(ctx.sentMsgs[0].(string), "已取消操作") {
		t.Errorf("cancel message = %v, want cancellation notice", ctx.sentMsgs)
	}
	if state := handler.convManager.GetState(testTelegramUserID); state != nil {
		t.Error("conversation state was not cleared")
	}
}

func TestHandleEdit_EmptyExpenses(t *testing.T) {
	repo := &spyAccountingRepo{expenses: []domain.Expense{}}
	handler := newTestHandler(repo, nil)

	ctx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{},
	}
	if err := handler.handleEdit(ctx); err != nil {
		t.Fatalf("handleEdit error: %v", err)
	}
	if len(ctx.sentMsgs) != 1 || !strings.Contains(ctx.sentMsgs[0].(string), "目前沒有消費記錄可編輯") {
		t.Errorf("handleEdit sent %v, want empty message", ctx.sentMsgs)
	}
}

func TestHandleEdit_ShowsExpenseListAndSelectionFlow(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	repo := &spyAccountingRepo{
		expenses: []domain.Expense{
			{
				ID:           "exp-1",
				Name:         "拉麵",
				Price:        1200,
				Currency:     domain.CurrencyJPY,
				ExchangeRate: decimal.NewFromFloat(0.22),
				Category:     domain.CategoryFood,
				Method:       domain.PaymentMethodCash,
				ShoppedAt:    now,
			},
		},
	}
	handler := newTestHandler(repo, nil)

	// 1. /edit lists records
	editCtx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{},
	}
	if err := handler.handleEdit(editCtx); err != nil {
		t.Fatalf("handleEdit error: %v", err)
	}
	if len(editCtx.sentMsgs) != 1 || !strings.Contains(editCtx.sentMsgs[0].(string), "請選擇要編輯的消費記錄") {
		t.Fatalf("sent = %v, want prompt", editCtx.sentMsgs)
	}
	markup, ok := editCtx.sentOpts[0][0].(*tele.ReplyMarkup)
	if !ok || len(markup.InlineKeyboard) < 2 {
		t.Fatalf("markup = %+v, want record button and cancel button", markup)
	}

	// 2. Select cancel callback
	cancelCbCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|cancel"},
	}
	if err := handler.handleCallback(cancelCbCtx); err != nil {
		t.Fatalf("cancel callback error: %v", err)
	}
	if len(cancelCbCtx.sentMsgs) != 1 || !strings.Contains(cancelCbCtx.sentMsgs[0].(string), "已取消編輯") {
		t.Errorf("cancel callback sent %v, want cancellation message", cancelCbCtx.sentMsgs)
	}

	// 3. Select exp-1 callback
	selectCbCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|exp-1"},
	}
	if err := handler.handleCallback(selectCbCtx); err != nil {
		t.Fatalf("select exp-1 error: %v", err)
	}
	if len(selectCbCtx.sentMsgs) != 1 || !strings.Contains(selectCbCtx.sentMsgs[0].(string), "請選擇要修改的欄位") {
		t.Fatalf("select exp-1 sent %v, want field selection", selectCbCtx.sentMsgs)
	}

	// 4. Select field "name"
	fieldNameCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_field|name"},
	}
	if err := handler.handleCallback(fieldNameCtx); err != nil {
		t.Fatalf("field name error: %v", err)
	}
	if len(fieldNameCtx.sentMsgs) != 1 || !strings.Contains(fieldNameCtx.sentMsgs[0].(string), "請輸入新的名稱") {
		t.Errorf("field name sent %v, want name input prompt", fieldNameCtx.sentMsgs)
	}

	// 5. Send new name text
	textNameCtx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{Text: "特製叉燒拉麵"},
	}
	if err := handler.handleText(textNameCtx); err != nil {
		t.Fatalf("send name error: %v", err)
	}
	if len(repo.updatedExpenses) != 1 || repo.updatedExpenses[0].Name != "特製叉燒拉麵" {
		t.Errorf("updatedExpenses = %+v, want name updated", repo.updatedExpenses)
	}
	if len(textNameCtx.sentMsgs) != 1 || !strings.Contains(textNameCtx.sentMsgs[0].(string), "已更新消費記錄") {
		t.Errorf("update response = %v, want confirmation card", textNameCtx.sentMsgs)
	}
}

func TestHandleEdit_FieldPrice(t *testing.T) {
	repo := &spyAccountingRepo{
		expenses: []domain.Expense{
			{
				ID:        "exp-1",
				Name:      "午餐",
				Price:     100,
				Currency:  domain.CurrencyTWD,
				Category:  domain.CategoryFood,
				Method:    domain.PaymentMethodCash,
				ShoppedAt: time.Now(),
			},
		},
	}
	handler := newTestHandler(repo, nil)

	// Start edit flow for exp-1
	_ = handler.handleCallback(&spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|exp-1"},
	})
	_ = handler.handleCallback(&spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_field|price"},
	})

	// Invalid price text
	badPriceCtx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{Text: "-50"},
	}
	if err := handler.handleText(badPriceCtx); err != nil {
		t.Fatalf("bad price error: %v", err)
	}
	if len(badPriceCtx.sentMsgs) != 1 || !strings.Contains(badPriceCtx.sentMsgs[0].(string), "金額格式錯誤") {
		t.Errorf("bad price sent %v, want format error", badPriceCtx.sentMsgs)
	}

	// Valid price text
	goodPriceCtx := &spyContext{
		sender:  &tele.User{ID: testTelegramUserID},
		message: &tele.Message{Text: "250"},
	}
	if err := handler.handleText(goodPriceCtx); err != nil {
		t.Fatalf("good price error: %v", err)
	}
	if len(repo.updatedExpenses) != 1 || repo.updatedExpenses[0].Price != 250 {
		t.Errorf("updatedExpenses = %+v, want price 250", repo.updatedExpenses)
	}
}

func TestHandleEdit_FieldCategoryAndMethod(t *testing.T) {
	repo := &spyAccountingRepo{
		expenses: []domain.Expense{
			{
				ID:        "exp-1",
				Name:      "車票",
				Price:     100,
				Currency:  domain.CurrencyTWD,
				Category:  domain.CategoryFood,
				Method:    domain.PaymentMethodCash,
				ShoppedAt: time.Now(),
			},
		},
	}
	handler := newTestHandler(repo, nil)

	// 1. Change category
	_ = handler.handleCallback(&spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|exp-1"},
	})
	catCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_cat|transport"},
	}
	if err := handler.handleCallback(catCtx); err != nil {
		t.Fatalf("change category error: %v", err)
	}
	if len(repo.updatedExpenses) != 1 || repo.updatedExpenses[0].Category != domain.CategoryTransport {
		t.Errorf("updatedExpenses = %+v, want category transport", repo.updatedExpenses)
	}

	// 2. Change method
	_ = handler.handleCallback(&spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|exp-1"},
	})
	methodCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_method|credit_card"},
	}
	if err := handler.handleCallback(methodCtx); err != nil {
		t.Fatalf("change method error: %v", err)
	}
	if len(repo.updatedExpenses) != 2 || repo.updatedExpenses[1].Method != domain.PaymentMethodCreditCard {
		t.Errorf("updatedExpenses = %+v, want method credit_card", repo.updatedExpenses)
	}
}

func TestHandleEdit_Delete(t *testing.T) {
	repo := &spyAccountingRepo{
		expenses: []domain.Expense{
			{
				ID:        "exp-delete-me",
				Name:      "待刪除項目",
				Price:     99,
				Currency:  domain.CurrencyTWD,
				Category:  domain.CategoryOther,
				Method:    domain.PaymentMethodCash,
				ShoppedAt: time.Now(),
			},
		},
	}
	handler := newTestHandler(repo, nil)

	_ = handler.handleCallback(&spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_select|exp-delete-me"},
	})
	delCtx := &spyContext{
		sender:   &tele.User{ID: testTelegramUserID},
		callback: &tele.Callback{Data: "edit_field|delete"},
	}
	if err := handler.handleCallback(delCtx); err != nil {
		t.Fatalf("delete callback error: %v", err)
	}
	if len(repo.deletedExpenseIDs) != 1 || repo.deletedExpenseIDs[0] != "exp-delete-me" {
		t.Errorf("deletedExpenseIDs = %v, want ['exp-delete-me']", repo.deletedExpenseIDs)
	}
	if len(delCtx.sentMsgs) != 1 || !strings.Contains(delCtx.sentMsgs[0].(string), "已刪除消費記錄") {
		t.Errorf("delete response = %v, want deletion confirmation", delCtx.sentMsgs)
	}
}

func TestKeyboards(t *testing.T) {
	catKb := makeCategoryKeyboard("test_cat")
	if len(catKb.InlineKeyboard) == 0 {
		t.Fatal("expected non-empty category keyboard")
	}

	methodKb := makePaymentMethodKeyboard("test_method")
	if len(methodKb.InlineKeyboard) == 0 {
		t.Fatal("expected non-empty method keyboard")
	}
}
