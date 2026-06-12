package ledger_test

import (
	"context"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/ledger"
)

// fakeRepo is an in-memory LedgerRepo for testing.
type fakeRepo struct {
	ledgers []domain.Ledger
}

func (f *fakeRepo) List(ctx context.Context) ([]domain.Ledger, error) {
	return f.ledgers, nil
}

func (f *fakeRepo) Add(ctx context.Context, l *domain.Ledger) error {
	for _, e := range f.ledgers {
		if e.Name == l.Name || e.NotionDatabaseID == l.NotionDatabaseID {
			return domain.ErrLedgerExists
		}
	}
	l.IsActive = len(f.ledgers) == 0
	f.ledgers = append(f.ledgers, *l)
	return nil
}

func (f *fakeRepo) GetActive(ctx context.Context) (*domain.Ledger, error) {
	for i := range f.ledgers {
		if f.ledgers[i].IsActive {
			return &f.ledgers[i], nil
		}
	}
	return nil, domain.ErrNoActiveLedger
}

func (f *fakeRepo) SetActiveByName(ctx context.Context, name string) error {
	found := false
	for i := range f.ledgers {
		if f.ledgers[i].Name == name {
			found = true
		}
	}
	if !found {
		return domain.ErrLedgerNotFound
	}
	for i := range f.ledgers {
		f.ledgers[i].IsActive = f.ledgers[i].Name == name
	}
	return nil
}

func (f *fakeRepo) GetByName(ctx context.Context, name string) (*domain.Ledger, error) {
	for i := range f.ledgers {
		if f.ledgers[i].Name == name {
			return &f.ledgers[i], nil
		}
	}
	return nil, domain.ErrLedgerNotFound
}

func (f *fakeRepo) SeedIfEmpty(ctx context.Context, name, notionDatabaseID string) (bool, error) {
	if len(f.ledgers) > 0 {
		return false, nil
	}
	f.ledgers = append(f.ledgers, domain.Ledger{Name: name, NotionDatabaseID: notionDatabaseID, IsActive: true})
	return true, nil
}

func TestService_AddFirstIsActive(t *testing.T) {
	svc := ledger.NewService(&fakeRepo{})
	ctx := context.Background()

	if err := svc.Add(ctx, "tokyo", "db-1"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	active, err := svc.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.Name != "tokyo" {
		t.Errorf("first ledger should be active, got %+v", active)
	}
}

func TestService_AddSecondNotActive(t *testing.T) {
	svc := ledger.NewService(&fakeRepo{})
	ctx := context.Background()
	_ = svc.Add(ctx, "a", "db-1")
	_ = svc.Add(ctx, "b", "db-2")

	active, _ := svc.GetActive(ctx)
	if active.Name != "a" {
		t.Errorf("active should remain a, got %s", active.Name)
	}
}

func TestService_AddInvalid(t *testing.T) {
	svc := ledger.NewService(&fakeRepo{})
	if err := svc.Add(context.Background(), "", "db-1"); err == nil {
		t.Errorf("expected validation error for empty name")
	}
}

func TestService_SetActive(t *testing.T) {
	svc := ledger.NewService(&fakeRepo{})
	ctx := context.Background()
	_ = svc.Add(ctx, "a", "db-1")
	_ = svc.Add(ctx, "b", "db-2")

	if err := svc.SetActive(ctx, "b"); err != nil {
		t.Fatalf("SetActive() error = %v", err)
	}
	active, _ := svc.GetActive(ctx)
	if active.Name != "b" {
		t.Errorf("active = %s, want b", active.Name)
	}
}

func TestService_ActiveLedgerProvider(t *testing.T) {
	svc := ledger.NewService(&fakeRepo{})
	ctx := context.Background()
	_ = svc.Add(ctx, "a", "db-1")

	l, err := svc.ActiveLedger(ctx)
	if err != nil {
		t.Fatalf("ActiveLedger() error = %v", err)
	}
	if l.NotionDatabaseID != "db-1" {
		t.Errorf("got %+v", l)
	}
}
