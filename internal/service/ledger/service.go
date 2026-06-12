package ledger

import (
	"context"
	"fmt"

	"github.com/omegaatt36/noccounting/domain"
)

// Service encapsulates ledger management business logic.
type Service struct {
	repo LedgerRepo
}

// NewService creates a new ledger Service.
func NewService(repo LedgerRepo) *Service {
	return &Service{repo: repo}
}

// List returns all ledgers.
func (s *Service) List(ctx context.Context) ([]domain.Ledger, error) {
	return s.repo.List(ctx)
}

// Add registers a new ledger. The first ledger registered becomes active.
func (s *Service) Add(ctx context.Context, name, notionDatabaseID string) error {
	l := &domain.Ledger{Name: name, NotionDatabaseID: notionDatabaseID}
	if err := l.Validate(); err != nil {
		return err
	}

	return s.repo.Add(ctx, l)
}

// SetActive switches the global active ledger by name.
func (s *Service) SetActive(ctx context.Context, name string) error {
	return s.repo.SetActiveByName(ctx, name)
}

// GetActive returns the active ledger.
func (s *Service) GetActive(ctx context.Context) (*domain.Ledger, error) {
	return s.repo.GetActive(ctx)
}

// SeedIfEmpty inserts a first active ledger only when the table is empty.
// Returns true when a row was seeded.
func (s *Service) SeedIfEmpty(ctx context.Context, name, notionDatabaseID string) (bool, error) {
	return s.repo.SeedIfEmpty(ctx, name, notionDatabaseID)
}

// ActiveLedger returns the active ledger by value, allowing this service to
// be used as an active-ledger provider by other services.
func (s *Service) ActiveLedger(ctx context.Context) (domain.Ledger, error) {
	l, err := s.repo.GetActive(ctx)
	if err != nil {
		return domain.Ledger{}, fmt.Errorf("get active ledger: %w", err)
	}
	return *l, nil
}
