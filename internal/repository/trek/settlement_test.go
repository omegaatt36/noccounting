package trek_test

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/trek"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

func TestSettlement_ReadsBalancesAndTransfersFromTREK(t *testing.T) {
	stub := newCreateStub(t)
	stub.settlement = `{
	  "balances": [
	    {"user_id": 3, "username": "owner-trek", "avatar_url": null, "balance": 1500},
	    {"user_id": 8, "username": "bob", "avatar_url": null, "balance": -1500}
	  ],
	  "flows": [
	    {"from": {"user_id": 8, "username": "bob", "avatar_url": null},
	     "to": {"user_id": 3, "username": "owner-trek", "avatar_url": null}, "amount": 1500}
	  ],
	  "settlements": [],
	  "finalBudgets": [],
	  "currency": "TWD",
	  "unconverted": {"item_ids": [5], "settlement_ids": [], "currencies": ["JPY"]}
	}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	got, err := repo.Settlement(context.Background())
	if err != nil {
		t.Fatalf("Settlement() error = %v", err)
	}

	if got.Currency != domain.CurrencyTWD || got.Unconverted != 1 {
		t.Errorf("Settlement() = %+v, want TWD with the one unconverted expense counted", got)
	}

	if len(got.Balances) != 2 {
		t.Fatalf("Balances = %+v, want one per person", got.Balances)
	}
	for i, want := range []struct {
		id, name string
		amount   int64
	}{{"3", "owner-trek", 1500}, {"8", "bob", -1500}} {
		balance := got.Balances[i]
		if balance.UserID != want.id || balance.Name != want.name || !balance.Amount.Equal(decimal.NewFromInt(want.amount)) {
			t.Errorf("Balances[%d] = %+v, want %s %s %d", i, balance, want.id, want.name, want.amount)
		}
	}

	if len(got.Transfers) != 1 {
		t.Fatalf("Transfers = %+v, want the one flow TREK offered", got.Transfers)
	}
	transfer := got.Transfers[0]
	if transfer.FromID != "8" || transfer.FromName != "bob" || transfer.ToID != "3" || transfer.ToName != "owner-trek" ||
		!transfer.Amount.Equal(decimal.NewFromInt(1500)) {
		t.Errorf("Transfers[0] = %+v, want bob paying owner-trek 1500", transfer)
	}
}

func TestSettlement_RefusesAnAnswerInAnotherCurrency(t *testing.T) {
	stub := newCreateStub(t)
	stub.settlement = `{"balances":[],"flows":[],"currency":"EUR","unconverted":{"item_ids":[],"settlement_ids":[]}}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	if _, err := repo.Settlement(context.Background()); err == nil {
		t.Fatal("Settlement() = nil, want a refusal of an answer in a currency the trip is not in")
	}
}

func TestMembers_ListsTheTripRosterOwnerIncluded(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	got, err := repo.Members(context.Background())
	if err != nil {
		t.Fatalf("Members() error = %v", err)
	}

	want := []domain.Member{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "bob"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Members() = %+v, want %+v", got, want)
	}
}

func TestMembers_LeavesOutTheServiceAccount(t *testing.T) {
	stub := newCreateStub(t)
	stub.roster = `{
	  "owner": {"id":3,"username":"owner-trek","role":"owner"},
	  "members": [
	    {"id":7,"username":"noccounting-dev","role":"member"},
	    {"id":8,"username":"bob","role":"member"}
	  ]
	}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	got, err := repo.Members(context.Background())
	if err != nil {
		t.Fatalf("Members() error = %v", err)
	}

	want := []domain.Member{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "bob"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Members() = %+v, want %+v", got, want)
	}
}

func TestQueryExpensesWithFilter_KeepsOnlyTheExpensesPaidTheWayAsked(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t,
		map[string]any{"id": 1, "name": "現金的", "note": "PAYMENT:cash"},
		map[string]any{"id": 2, "name": "刷卡的", "note": "PAYMENT:credit_card|加收小費"},
		map[string]any{"id": 3, "name": "TREK 裡記的", "note": "just a note"},
	)
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	card := domain.PaymentMethodCreditCard
	got, err := repo.QueryExpensesWithFilter(context.Background(), expense.ExpenseFilter{Method: &card})
	if err != nil {
		t.Fatalf("QueryExpensesWithFilter() error = %v", err)
	}
	if names := namesOf(got); !reflect.DeepEqual(names, []string{"刷卡的"}) {
		t.Errorf("filtered by credit_card = %v, want only the card expense", names)
	}
}

func TestCreateExpense_QuotesAForeignExpenseAgainstAJPYTrip(t *testing.T) {
	stub := newCreateStub(t)
	provider := &quotedRate{rate: decimal.RequireFromString("4.5")}
	repo := stub.repo(t, domain.CurrencyJPY, provider)
	expense := ramen()
	expense.Currency = domain.CurrencyTWD
	expense.Price = 100

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyTWD || provider.askedTarget[0] != domain.CurrencyJPY {
		t.Errorf("quoted %v in %v, want one quote of TWD in JPY", provider.asked, provider.askedTarget)
	}
	rate, _ := body(t, oneCreate(t, stub))["exchange_rate"].(float64)
	if want := 1 / 4.5; math.Abs(rate-want) > 1e-9 {
		t.Errorf("exchange_rate = %v, want %v", rate, want)
	}
}

func TestRepo_ReusesRosterAcrossCalls(t *testing.T) {
	stub := newCreateStub(t)
	repo := trek.NewRepo(stub.startedClient(t), nil)
	trip := domain.Trip{ID: createTripID, Currency: domain.CurrencyTWD}
	for range 2 {
		if _, err := repo.Members(context.Background(), trip); err != nil {
			t.Fatal(err)
		}
	}
	reads := 0
	for _, call := range stub.callsSeen() {
		if strings.HasSuffix(call.path, "/members") {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("roster fetches = %d, want one across calls", reads)
	}
}
