package webapp

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func day(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

func listedExpenses() []domain.Expense {
	return []domain.Expense{
		{ID: "1", Name: "現金拉麵", Price: 1200, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, Method: domain.PaymentMethodCash, PaidByID: "8", ShoppedAt: day(12)},
		{ID: "2", Name: "刷卡車票", Price: 3000, Currency: domain.CurrencyTWD, Category: domain.CategoryTransport, Method: domain.PaymentMethodCreditCard, PaidByID: "8", ShoppedAt: day(13)},
		{ID: "3", Name: "沒標的", Price: 50, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, ShoppedAt: day(13)},
	}
}

func TestHandleExpenseList_GroupsByDayNewestFirst(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{expenses: listedExpenses()}, twoTrips{})

	body := get(t, handler.handleExpenseList, "/partial/expenses?range=all&trip_id=3").Body.String()

	newer, older := strings.Index(body, "10/13（週二）"), strings.Index(body, "10/12（週一）")
	if newer < 0 || older < 0 || newer > older {
		t.Fatalf("days are not newest first (13th at %d, 12th at %d):\n%s", newer, older, body)
	}
	for _, want := range []string{
		"NT$3,050",
		"John · 信用卡",
		"共 3 筆",
		`aria-label="編輯 刷卡車票"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleExpenseList_FiltersBySearchCategoryAndMethod(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{expenses: listedExpenses()}, twoTrips{})

	tests := []struct {
		name  string
		query string
		want  []string
		not   []string
	}{
		{"search ignores case and surrounding space", "q=%20%E6%8B%89%E9%BA%B5", []string{"現金拉麵"}, []string{"刷卡車票", "沒標的"}},
		{"category", "category=food", []string{"現金拉麵", "沒標的"}, []string{"刷卡車票"}},
		{"payment method", "method=credit_card", []string{"刷卡車票"}, []string{"現金拉麵", "沒標的"}},
		{"payer", "paid_by=8", []string{"現金拉麵", "刷卡車票"}, []string{"沒標的"}},
		{"single date", "date=2026-10-12", []string{"現金拉麵"}, []string{"刷卡車票", "沒標的"}},
		{"all of them together", "category=food&method=cash&paid_by=8&date=2026-10-12&q=%E6%8B%89", []string{"現金拉麵"}, []string{"刷卡車票", "沒標的"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := get(t, handler.handleExpenseList, "/partial/expenses?range=all&trip_id=3&"+tt.query).Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("list does not contain %q:\n%s", want, body)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(body, not) {
					t.Errorf("list contains %q, which the filter excludes", not)
				}
			}
		})
	}
}

func TestHandleExpenseList_RefusesAFilterThatNamesNothing(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{expenses: listedExpenses()}, twoTrips{})

	for _, query := range []string{"method=bitcoin", "category=rent", "date=not-a-date"} {
		if w := get(t, handler.handleExpenseList, "/partial/expenses?trip_id=3&"+query); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, w.Code)
		}
	}
}

func TestHandleExpenseList_TellsAnEmptyTripFromAnEmptiedList(t *testing.T) {
	empty, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})
	if body := get(t, empty.handleExpenseList, "/partial/expenses?trip_id=3").Body.String(); !strings.Contains(body, "還沒有消費記錄") || !strings.Contains(body, "show-form") {
		t.Errorf("a trip with nothing in it should say so and offer to add one: %s", body)
	}

	handler, _ := devHandler(t, &stubAccountingRepo{expenses: listedExpenses()}, twoTrips{})
	body := get(t, handler.handleExpenseList, "/partial/expenses?trip_id=3&q=nothing-like-this").Body.String()
	if !strings.Contains(body, "沒有符合條件的消費") || strings.Contains(body, "show-form") {
		t.Errorf("a filtered-out list should say the filters hid everything: %s", body)
	}
}

func TestHandleExpenseList_RowsCarryWhatTheEditorFillsFormFrom(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{{
		ID: "7", Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: decimal.RequireFromString("0.215"),
		Category: domain.CategoryFood, Method: domain.PaymentMethodIcCard, PaidByID: "8", ParticipantIDs: []string{"3", "8"}, ShoppedAt: day(13),
	}}}
	handler, _ := devHandler(t, repo, twoTrips{})

	body := get(t, handler.handleExpenseList, "/partial/expenses?trip_id=3").Body.String()

	// The row is an HTML attribute, so its JSON arrives quoted as entities.
	body = strings.NewReplacer("&#34;", `"`, "&quot;", `"`).Replace(body)
	for _, want := range []string{
		`"id":"7"`, `"price":1000`, `"currency":"JPY"`, `"exchange_rate":"0.215"`,
		`"category":"food"`, `"method":"ic_card"`,
		`"paid_by":"123456789"`,
		`"participants":["3","8"]`, `"shopped_at":"2026-10-13"`,
		"¥1,000 (NT$215)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("row does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleExpenseList_ARateIsOnlyOfferedForAForeignExpense(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{{
		ID: "7", Name: "lunch", Price: 100, Currency: domain.CurrencyTWD, ExchangeRate: decimal.NewFromInt(1),
		Category: domain.CategoryFood, ShoppedAt: day(13),
	}}}
	handler, _ := devHandler(t, repo, twoTrips{})

	body := strings.NewReplacer("&#34;", `"`, "&quot;", `"`).Replace(get(t, handler.handleExpenseList, "/partial/expenses?trip_id=3").Body.String())

	if !strings.Contains(body, `"exchange_rate":""`) {
		t.Errorf("an expense in the trip's own currency has no rate to edit: %s", body)
	}
}
