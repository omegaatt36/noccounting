package domain

import (
	"context"
	"time"
)

// ExpenseFilter defines filters for querying expenses.
type ExpenseFilter struct {
	DateFrom *time.Time // Filter expenses from this date (inclusive)
	DateTo   *time.Time // Filter expenses to this date (inclusive)
	PaidByID *string    // Filter by payer's ID
	Limit    *int       // Maximum number of results
}

// AccountingRepo defines the interface for expense persistence.
// This abstraction allows different storage implementations (Notion, PostgreSQL, etc.)
// to be used interchangeably without affecting the business logic.
type AccountingRepo interface {
	// CreateExpense creates a new expense record and returns the created expense with ID.
	CreateExpense(ctx context.Context, expense *Expense) error

	// QueryExpenses retrieves all expense records.
	QueryExpenses(ctx context.Context) ([]Expense, error)

	// QueryExpensesWithFilter retrieves expenses matching the given filter.
	QueryExpensesWithFilter(ctx context.Context, filter ExpenseFilter) ([]Expense, error)

	// UpdateExpense updates an existing expense record.
	UpdateExpense(ctx context.Context, expense *Expense) error

	// DeleteExpense deletes an expense record by ID (archives in Notion).
	DeleteExpense(ctx context.Context, id string) error

	// GetExpenseSummary calculates the expense summary for splitting.
	GetExpenseSummary(ctx context.Context) (*ExpenseSummary, error)
}
