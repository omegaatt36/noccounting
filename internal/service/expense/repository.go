package expense

import (
	"context"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

// ExpenseFilter defines filters for querying expenses.
type ExpenseFilter struct {
	DateFrom *time.Time
	DateTo   *time.Time
	PaidByID *string
	Limit    *int
}

// AccountingRepo defines expense persistence operations consumed by this service.
type AccountingRepo interface {
	CreateExpense(ctx context.Context, expense *domain.Expense) error
	QueryExpenses(ctx context.Context) ([]domain.Expense, error)
	QueryExpensesWithFilter(ctx context.Context, filter ExpenseFilter) ([]domain.Expense, error)
	UpdateExpense(ctx context.Context, expense *domain.Expense) error
	DeleteExpense(ctx context.Context, id string) error
	GetExpenseSummary(ctx context.Context) (*domain.ExpenseSummary, error)
	UploadFile(ctx context.Context, filePath string) (string, error)
}

// ExchangeRateFetcher defines exchange rate retrieval consumed by this service.
type ExchangeRateFetcher interface {
	GetRate(ctx context.Context, sourceCurrency domain.Currency) (decimal.Decimal, error)
}

// ReceiptAnalyzer defines receipt AI analysis consumed by this service.
type ReceiptAnalyzer interface {
	Analyze(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error)
}
