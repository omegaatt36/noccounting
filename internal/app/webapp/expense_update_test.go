package webapp

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func storedExpense() domain.Expense {
	return domain.Expense{
		ID: "7", Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: decimal.RequireFromString("0.215"),
		Category: domain.CategoryFood, Method: domain.PaymentMethodCash, PaidByID: "3", ParticipantIDs: []string{"3", "8"},
		ShoppedAt: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), ReceiptURL: "41",
		ReceiptItems: []domain.ReceiptItem{{Name: "noodles", Price: 1000}},
	}
}

func TestHandleUpdateExpense_RewritesWhatTheFormSays(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}}
	handler, _ := devHandler(t, repo, twoTrips{})

	form := expenseForm(map[string]string{
		"expense_id": "7", "name": " tsukemen ", "price": "1300", "currency": "TWD", "category": "groceries", "method": "credit_card",
		"paid_by": "123456789", "shopped_at": "2026-10-14",
	})
	form["participants"] = []string{"8"}
	w := put(t, handler.handleUpdateExpense, "/api/expense", form)

	if !strings.Contains(w.Body.String(), `data-success="true"`) || !strings.Contains(w.Body.String(), "tsukemen") {
		t.Fatalf("the update was not confirmed: %s", w.Body.String())
	}
	if len(repo.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(repo.updated))
	}
	got := repo.updated[0]
	if got.ID != "7" || got.Name != "tsukemen" || got.Price != 1300 || got.Currency != domain.CurrencyTWD ||
		got.Category != domain.CategoryGroceries || got.Method != domain.PaymentMethodCreditCard ||
		got.PaidByID != "8" || got.ShoppedAt.Format(time.DateOnly) != "2026-10-14" ||
		len(got.ParticipantIDs) != 1 || got.ParticipantIDs[0] != "8" {
		t.Errorf("updated = %+v, want the form's values", got)
	}
	if !got.ExchangeRate.IsZero() {
		t.Errorf("rate = %s, want none for an expense in the trip's own currency", got.ExchangeRate)
	}
}

func TestHandleUpdateExpense_KeepsWhatTheFormHasNoFieldFor(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}}
	handler, _ := devHandler(t, repo, twoTrips{})

	put(t, handler.handleUpdateExpense, "/api/expense", expenseForm(map[string]string{"expense_id": "7", "currency": "JPY", "exchange_rate": "0.22"}))

	if len(repo.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(repo.updated))
	}
	got := repo.updated[0]
	if got.PaidByID != "3" {
		t.Errorf("payer = %q, want the stored one", got.PaidByID)
	}
	if got.ShoppedAt.Format(time.DateOnly) != "2026-10-13" {
		t.Errorf("day = %s, want the stored one", got.ShoppedAt)
	}
	if got.ReceiptURL != "41" || len(got.ReceiptItems) != 1 {
		t.Errorf("receipt = %q with %d lines, want it kept", got.ReceiptURL, len(got.ReceiptItems))
	}
	if got.ExchangeRate.String() != "0.22" {
		t.Errorf("rate = %s, want the one the form carries", got.ExchangeRate)
	}
}

func TestHandleUpdateExpense_SaysWhyItFailed(t *testing.T) {
	tests := []struct {
		name  string
		repo  *stubAccountingRepo
		form  url.Values
		wants string
	}{
		{"an expense deleted meanwhile", &stubAccountingRepo{}, expenseForm(map[string]string{"expense_id": "7"}), "找不到這筆消費"},
		{"no expense named", &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}}, expenseForm(nil), "找不到這筆消費"},
		{"a custom split", &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}, updateErr: domain.ErrUnsupportedSplit}, expenseForm(map[string]string{"expense_id": "7"}), "請到 TREK 修改"},
		{"someone off the trip", &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}, updateErr: domain.ErrNotOnTrip}, expenseForm(map[string]string{"expense_id": "7"}), "不在這趟旅行"},
		{"a bad amount", &stubAccountingRepo{expenses: []domain.Expense{storedExpense()}}, expenseForm(map[string]string{"expense_id": "7", "price": "-3"}), "請輸入有效金額"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := devHandler(t, tt.repo, twoTrips{})

			body := put(t, handler.handleUpdateExpense, "/api/expense", tt.form).Body.String()

			if !strings.Contains(body, `data-success="false"`) || !strings.Contains(body, "儲存失敗") || !strings.Contains(body, tt.wants) {
				t.Errorf("response = %s, want a failure saying %q", body, tt.wants)
			}
			if len(tt.repo.updated) != 0 {
				t.Error("something was written despite the failure")
			}
		})
	}
}

func TestHandleCreateExpense_FailureNamesWhatFailed(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})

	body := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{"name": " "})).Body.String()

	if !strings.Contains(body, "新增失敗") || !strings.Contains(body, "請輸入消費名稱") {
		t.Errorf("response = %s, want the failure and its reason", body)
	}
}
