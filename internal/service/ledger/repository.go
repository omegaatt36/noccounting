package ledger

import (
	"context"

	"github.com/omegaatt36/noccounting/domain"
)

// LedgerRepo defines ledger metadata persistence consumed by this service.
type LedgerRepo interface {
	List(ctx context.Context) ([]domain.Ledger, error)
	Add(ctx context.Context, l *domain.Ledger) error
	GetActive(ctx context.Context) (*domain.Ledger, error)
	SetActiveByName(ctx context.Context, name string) error
	GetByName(ctx context.Context, name string) (*domain.Ledger, error)
	SeedIfEmpty(ctx context.Context, name, notionDatabaseID string) (bool, error)
}
