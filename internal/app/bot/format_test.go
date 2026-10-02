package bot

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func TestTruncate_CountsCharactersNotBytes(t *testing.T) {
	// Cutting a Chinese label at a byte offset leaves half a character, which
	// Telegram refuses.
	got := truncate("牛肉麵加蛋加叉燒加青菜", 6)
	if got != "牛肉麵加蛋…" {
		t.Errorf("truncate() = %q, want the first five characters and an ellipsis", got)
	}
	if short := truncate("拉麵", 6); short != "拉麵" {
		t.Errorf("truncate() = %q, want a short label left alone", short)
	}
}

func TestExpenseCard_NamesTheTripAndConvertsAForeignExpense(t *testing.T) {
	trip := domain.Trip{ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyTWD}
	exp := &domain.Expense{
		Name:         "拉麵",
		Price:        1200,
		Currency:     domain.CurrencyJPY,
		ExchangeRate: decimal.RequireFromString("0.22"),
		Category:     domain.CategoryFood,
		Method:       domain.PaymentMethodCreditCard,
	}

	got := expenseCard("✅ 已新增消費記錄", trip, exp)

	for _, want := range []string{"✅ 已新增消費記錄", "🧳 2026 Tokyo", "📝 拉麵", "💰 ¥1,200（≈NT$264）", "📂 🍜 餐飲", "💳 信用卡"} {
		if !strings.Contains(got, want) {
			t.Errorf("expenseCard() = %q, want it to contain %q", got, want)
		}
	}
}

func TestExpenseCard_LeavesAnExpenseInTheTripsCurrencyUnconverted(t *testing.T) {
	trip := domain.Trip{Title: "Tokyo", Currency: domain.CurrencyJPY}
	exp := &domain.Expense{
		Name: "拉麵", Price: 1200, Currency: domain.CurrencyJPY,
		ExchangeRate: decimal.RequireFromString("0.22"), // stray: it is the trip's own currency
		Category:     domain.CategoryFood, Method: domain.PaymentMethodCash,
	}

	if got := expenseCard("x", trip, exp); strings.Contains(got, "≈") {
		t.Errorf("expenseCard() = %q, want no conversion for the trip's own currency", got)
	}
}

func TestSettlementMessage(t *testing.T) {
	trip := domain.Trip{Title: "2026 Tokyo", Currency: domain.CurrencyTWD}
	settlement := &domain.Settlement{
		Currency: domain.CurrencyTWD,
		Balances: []domain.Balance{
			{UserID: "3", Name: "owner-trek", Amount: decimal.NewFromInt(1500)},
			{UserID: "8", Name: "bob", Amount: decimal.NewFromInt(-1500)},
		},
		Transfers: []domain.Transfer{
			{FromID: "8", FromName: "bob", ToID: "3", ToName: "owner-trek", Amount: decimal.NewFromInt(1500)},
		},
		Unconverted: 2,
	}
	// A person in the user mapping goes by their nickname here; anyone else by
	// the name TREK gave them.
	names := map[string]string{"3": "Alice"}

	got := settlementMessage(trip, settlement, names)

	for _, want := range []string{
		"🧳 2026 Tokyo（TWD）",
		"• Alice  +NT$1,500",
		"• bob  -NT$1,500",
		"• bob → Alice  NT$1,500",
		"有 2 筆因為缺少匯率沒有計入",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("settlementMessage() = %q, want it to contain %q", got, want)
		}
	}
}

func TestSettlementMessage_NobodyOwesAnything(t *testing.T) {
	got := settlementMessage(domain.Trip{Title: "t", Currency: domain.CurrencyTWD}, &domain.Settlement{Currency: domain.CurrencyTWD}, nil)

	if !strings.Contains(got, "目前沒有任何人欠款") || strings.Contains(got, "建議轉帳") {
		t.Errorf("settlementMessage() = %q, want the all-square message and no transfers", got)
	}
}

func TestRateMessage_ListsEveryDirection(t *testing.T) {
	got := rateMessage([]domain.RateQuote{
		{From: domain.CurrencyTWD, To: domain.CurrencyJPY, Rate: decimal.RequireFromString("4.651163")},
		{From: domain.CurrencyJPY, To: domain.CurrencyTWD, Rate: decimal.RequireFromString("0.215")},
	})

	for _, want := range []string{"1 TWD = ¥4.6512", "1 JPY = NT$0.2150"} {
		if !strings.Contains(got, want) {
			t.Errorf("rateMessage() = %q, want it to contain %q", got, want)
		}
	}
}

func TestRateMessage_NoQuotes(t *testing.T) {
	if got := rateMessage(nil); !strings.Contains(got, "無法取得匯率") {
		t.Errorf("rateMessage() = %q, want it to say no rate could be had", got)
	}
}
