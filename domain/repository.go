package domain

import "context"

// AccountingRepo defines the interface for expense persistence.
// This abstraction allows different storage implementations (Notion, PostgreSQL, etc.)
// to be used interchangeably without affecting the business logic.
type AccountingRepo interface {
	// CreateExpense creates a new expense record.
	CreateExpense(ctx context.Context, expense *Expense) error

	// QueryExpenses retrieves all expense records.
	QueryExpenses(ctx context.Context) ([]Expense, error)

	// GetExpenseSummary calculates the expense summary for splitting.
	GetExpenseSummary(ctx context.Context) (*ExpenseSummary, error)
}
