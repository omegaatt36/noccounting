// Package expensetest provides shared test doubles for the expense service.
package expensetest

import (
	"context"

	"github.com/omegaatt36/noccounting/domain"
)

// FakeLedgerProvider implements expense.ActiveLedgerProvider for testing.
// The zero value returns a ledger with NotionDatabaseID "db-test".
type FakeLedgerProvider struct {
	Ledger domain.Ledger
	Err    error
}

// ActiveLedger returns the configured ledger or error.
func (f FakeLedgerProvider) ActiveLedger(_ context.Context) (domain.Ledger, error) {
	if f.Err != nil {
		return domain.Ledger{}, f.Err
	}
	if f.Ledger.NotionDatabaseID == "" {
		return domain.Ledger{NotionDatabaseID: "db-test"}, nil
	}
	return f.Ledger, nil
}
