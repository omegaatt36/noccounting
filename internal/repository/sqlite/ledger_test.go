package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/sqlite"
)

func newTestRepo(t *testing.T) *sqlite.Repo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repo, err := sqlite.NewRepo(dbPath)
	if err != nil {
		t.Fatalf("NewRepo() error = %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestRepo_AddAndList(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	l := &domain.Ledger{Name: "tokyo", NotionDatabaseID: "db-1", IsActive: true}
	if err := repo.Add(ctx, l); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "tokyo" {
		t.Fatalf("List() = %+v", list)
	}
	if list[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be populated")
	}
}

func TestRepo_DuplicateReturnsErrLedgerExists(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if err := repo.Add(ctx, &domain.Ledger{Name: "a", NotionDatabaseID: "db-1"}); err != nil {
		t.Fatalf("Add() setup error = %v", err)
	}
	err := repo.Add(ctx, &domain.Ledger{Name: "a", NotionDatabaseID: "db-2"})
	if !errors.Is(err, domain.ErrLedgerExists) {
		t.Fatalf("expected ErrLedgerExists, got %v", err)
	}
}

func TestRepo_GetActiveNoneReturnsErrNoActiveLedger(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.GetActive(ctx)
	if !errors.Is(err, domain.ErrNoActiveLedger) {
		t.Fatalf("expected ErrNoActiveLedger, got %v", err)
	}
}

func TestRepo_AddFirstLedgerIsActiveRegardlessOfFlag(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	l := &domain.Ledger{Name: "a", NotionDatabaseID: "db-1", IsActive: false}
	if err := repo.Add(ctx, l); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !l.IsActive {
		t.Errorf("first ledger should become active regardless of requested flag")
	}

	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.Name != "a" {
		t.Errorf("active = %s, want a", active.Name)
	}
}

func TestRepo_SetActiveByNameSwitchesAtMostOne(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Add(ctx, &domain.Ledger{Name: "a", NotionDatabaseID: "db-1", IsActive: true}); err != nil {
		t.Fatalf("Add() setup error = %v", err)
	}
	if err := repo.Add(ctx, &domain.Ledger{Name: "b", NotionDatabaseID: "db-2", IsActive: false}); err != nil {
		t.Fatalf("Add() setup error = %v", err)
	}

	if err := repo.SetActiveByName(ctx, "b"); err != nil {
		t.Fatalf("SetActiveByName() error = %v", err)
	}

	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.Name != "b" {
		t.Errorf("active = %s, want b", active.Name)
	}

	list, _ := repo.List(ctx)
	activeCount := 0
	for _, l := range list {
		if l.IsActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("activeCount = %d, want 1", activeCount)
	}
}

func TestRepo_SetActiveByNameMissing(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	err := repo.SetActiveByName(ctx, "nope")
	if !errors.Is(err, domain.ErrLedgerNotFound) {
		t.Fatalf("expected ErrLedgerNotFound, got %v", err)
	}
}

func TestRepo_GetByName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Add(ctx, &domain.Ledger{Name: "a", NotionDatabaseID: "db-1"}); err != nil {
		t.Fatalf("Add() setup error = %v", err)
	}

	got, err := repo.GetByName(ctx, "a")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if got.NotionDatabaseID != "db-1" {
		t.Errorf("got %+v", got)
	}

	if _, err := repo.GetByName(ctx, "missing"); !errors.Is(err, domain.ErrLedgerNotFound) {
		t.Errorf("expected ErrLedgerNotFound, got %v", err)
	}
}

func TestRepo_SeedIfEmpty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	seeded, err := repo.SeedIfEmpty(ctx, "default", "db-seed")
	if err != nil {
		t.Fatalf("SeedIfEmpty() error = %v", err)
	}
	if !seeded {
		t.Errorf("expected seeded=true on empty table")
	}

	active, err := repo.GetActive(ctx)
	if err != nil || active.Name != "default" {
		t.Fatalf("active = %+v, err = %v", active, err)
	}

	// Second call must be a no-op.
	seeded, err = repo.SeedIfEmpty(ctx, "other", "db-other")
	if err != nil {
		t.Fatalf("SeedIfEmpty() second error = %v", err)
	}
	if seeded {
		t.Errorf("expected seeded=false when table non-empty")
	}
}
