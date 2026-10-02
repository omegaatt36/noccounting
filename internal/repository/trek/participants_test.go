package trek_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/trek"
)

func TestCreateExpense_EmptyParticipantsDoNotApplyServiceDefaults(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ParticipantIDs = nil })

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	got, _ := body(t, oneCreate(t, stub))["member_ids"].([]any)
	if want := []any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("member_ids = %#v, want the owner and the member, in id order", got)
	}
}

func TestCreateExpense_ASplitIsRefusedWhenAParticipantIsNotOnTheTrip(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ParticipantIDs = []string{"8", "42"} })

	err := repo.CreateExpense(context.Background(), expense)
	if !errors.Is(err, trek.ErrParticipantNotOnTrip) {
		t.Fatalf("CreateExpense() error = %v, want ErrParticipantNotOnTrip", err)
	}
	if creates := stub.creates(); len(creates) != 0 {
		t.Errorf("an expense shared with someone off the trip was written anyway: %v", creates)
	}
}

func TestCreateExpense_AParticipantNamedTwiceSharesOnce(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ParticipantIDs = []string{"8", "3", "8"} })

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	got, _ := body(t, oneCreate(t, stub))["member_ids"].([]any)
	if want := []any{float64(8), float64(3)}; !reflect.DeepEqual(got, want) {
		t.Errorf("member_ids = %#v, want each participant once, in the order named", got)
	}
}

func TestCreateExpense_ACategoryOutsideTREKsSetIsRefused(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.Category = domain.Category("souvenirs") })

	if err := repo.CreateExpense(context.Background(), expense); !errors.Is(err, trek.ErrInvalidExpense) {
		t.Fatalf("CreateExpense() error = %v, want ErrInvalidExpense", err)
	}
	if creates := stub.creates(); len(creates) != 0 {
		t.Errorf("an expense with an unrenderable category was written anyway: %v", creates)
	}
}

func TestUpdateExpense_KeepsTheCategoryTheListingRead(t *testing.T) {
	for _, category := range domain.CategoryValues() {
		t.Run(string(category), func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{
				"id": 42, "category": string(category), "members": []any{map[string]any{"user_id": 3}, map[string]any{"user_id": 8}},
			})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			read, err := repo.QueryExpenses(context.Background())
			if err != nil {
				t.Fatalf("QueryExpenses() error = %v", err)
			}
			expense := oneExpense(t, read)
			if expense.Category != category {
				t.Fatalf("Category = %q, want %q read as itself", expense.Category, category)
			}
			if want := []string{"3", "8"}; !reflect.DeepEqual(expense.ParticipantIDs, want) {
				t.Fatalf("ParticipantIDs = %v, want %v read off the item's members", expense.ParticipantIDs, want)
			}

			expense.Price = 1500
			if err := repo.UpdateExpense(context.Background(), &expense); err != nil {
				t.Fatalf("UpdateExpense() error = %v", err)
			}

			fields := body(t, oneUpdate(t, stub))
			if fields["category"] != string(category) {
				t.Errorf("update category = %v, want %q written back unchanged", fields["category"], category)
			}
			if got, _ := fields["member_ids"].([]any); !reflect.DeepEqual(got, []any{float64(3), float64(8)}) {
				t.Errorf("update member_ids = %#v, want the split the item was read with", fields["member_ids"])
			}
		})
	}
}

func TestUpdateExpense_RejectsCustomSplitBeforeWriting(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{
		"id": 42,
		"members": []any{
			map[string]any{"user_id": 3, "amount": 900},
			map[string]any{"user_id": 8, "amount": 300},
		},
	})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	read, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}
	expense := oneExpense(t, read)
	expense.Price = 1500

	err = repo.UpdateExpense(context.Background(), &expense)
	if !errors.Is(err, domain.ErrUnsupportedSplit) {
		t.Fatalf("UpdateExpense() error = %v, want ErrInvalidExpense for unsupported custom split", err)
	}
	if updates := stub.updates(); len(updates) != 0 {
		t.Errorf("custom split update wrote %d requests, want none", len(updates))
	}
}
