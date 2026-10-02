package expense

import (
	"context"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

type ExpenseFilter struct {
	DateFrom *time.Time
	DateTo   *time.Time
	PaidByID *string
	Method   *domain.PaymentMethod
	Limit    *int
}

type AccountingRepo interface {
	CreateExpense(ctx context.Context, trip domain.Trip, expense *domain.Expense) error
	QueryExpenses(ctx context.Context, trip domain.Trip) ([]domain.Expense, error)
	QueryExpensesWithFilter(ctx context.Context, trip domain.Trip, filter ExpenseFilter) ([]domain.Expense, error)
	UpdateExpense(ctx context.Context, trip domain.Trip, expense *domain.Expense) error
	DeleteExpense(ctx context.Context, trip domain.Trip, id string) error
	Settlement(ctx context.Context, trip domain.Trip) (*domain.Settlement, error)
	Members(ctx context.Context, trip domain.Trip) ([]domain.Member, error)
	UploadFile(ctx context.Context, trip domain.Trip, filePath string) (string, error)
}

type ExchangeRateFetcher interface {
	GetRate(ctx context.Context, source, target domain.Currency) (decimal.Decimal, error)
}

type ReceiptAnalyzer interface {
	Analyze(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error)
}
