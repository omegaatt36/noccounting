package expense

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

// TodaySummary holds aggregated expense data for today.
type TodaySummary struct {
	Date          time.Time
	Items         []CategorySummary
	GrandTotalTWD decimal.Decimal
	ItemCount     int
}

// CategorySummary holds aggregated expense data for a single category.
type CategorySummary struct {
	Category domain.Category
	Total    uint64
	Currency domain.Currency
	TotalTWD decimal.Decimal
}

// Service encapsulates expense business logic.
type Service struct {
	repo            AccountingRepo
	ledger          ActiveLedgerProvider
	rateFetcher     ExchangeRateFetcher // optional
	receiptAnalyzer ReceiptAnalyzer     // optional
}

// NewService creates a new Service. rateFetcher and receiptAnalyzer may be nil.
func NewService(repo AccountingRepo, ledger ActiveLedgerProvider, rateFetcher ExchangeRateFetcher, receiptAnalyzer ReceiptAnalyzer) *Service {
	return &Service{
		repo:            repo,
		ledger:          ledger,
		rateFetcher:     rateFetcher,
		receiptAnalyzer: receiptAnalyzer,
	}
}

// activeDBID resolves the Notion database ID of the active ledger.
func (s *Service) activeDBID(ctx context.Context) (string, error) {
	l, err := s.ledger.ActiveLedger(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve active ledger database: %w", err)
	}
	return l.NotionDatabaseID, nil
}

// withActiveDB resolves the active ledger's database ID and passes it to fn.
func (s *Service) withActiveDB(ctx context.Context, fn func(dbID string) error) error {
	dbID, err := s.activeDBID(ctx)
	if err != nil {
		return err
	}
	return fn(dbID)
}

// withActiveDBResult resolves the active ledger's database ID and passes it to fn,
// returning fn's result.
//
// withActiveDBResult is a function, not a method, because Go does not allow
// type parameters on methods.
func withActiveDBResult[T any](ctx context.Context, s *Service, fn func(dbID string) (T, error)) (T, error) {
	dbID, err := s.activeDBID(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(dbID)
}

// CreateExpense delegates to the repository.
func (s *Service) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	return s.withActiveDB(ctx, func(dbID string) error {
		return s.repo.CreateExpense(ctx, dbID, expense)
	})
}

// QueryExpenses delegates to the repository.
func (s *Service) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	return withActiveDBResult(ctx, s, func(dbID string) ([]domain.Expense, error) {
		return s.repo.QueryExpenses(ctx, dbID)
	})
}

// QueryExpensesWithFilter delegates to the repository.
func (s *Service) QueryExpensesWithFilter(ctx context.Context, filter ExpenseFilter) ([]domain.Expense, error) {
	return withActiveDBResult(ctx, s, func(dbID string) ([]domain.Expense, error) {
		return s.repo.QueryExpensesWithFilter(ctx, dbID, filter)
	})
}

// UpdateExpense delegates to the repository.
func (s *Service) UpdateExpense(ctx context.Context, expense *domain.Expense) error {
	return s.withActiveDB(ctx, func(dbID string) error {
		return s.repo.UpdateExpense(ctx, dbID, expense)
	})
}

// DeleteExpense delegates to the repository.
func (s *Service) DeleteExpense(ctx context.Context, id string) error {
	return s.withActiveDB(ctx, func(dbID string) error {
		return s.repo.DeleteExpense(ctx, dbID, id)
	})
}

// GetSummary delegates to the repository.
func (s *Service) GetSummary(ctx context.Context) (*domain.ExpenseSummary, error) {
	return withActiveDBResult(ctx, s, func(dbID string) (*domain.ExpenseSummary, error) {
		return s.repo.GetExpenseSummary(ctx, dbID)
	})
}

// GetTodaySummary queries today's expenses (local time) and aggregates them by category.
func (s *Service) GetTodaySummary(ctx context.Context) (*TodaySummary, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour).Add(-time.Nanosecond)

	filter := ExpenseFilter{
		DateFrom: &startOfDay,
		DateTo:   &endOfDay,
	}

	dbID, err := s.activeDBID(ctx)
	if err != nil {
		return nil, err
	}

	expenses, err := s.repo.QueryExpensesWithFilter(ctx, dbID, filter)
	if err != nil {
		return nil, err
	}

	// Fetch fallback JPY rate if needed.
	var fallbackJPYRate decimal.Decimal
	needsFallback := false
	for _, exp := range expenses {
		if exp.Currency == domain.CurrencyJPY && exp.ExchangeRate.IsZero() {
			needsFallback = true
			break
		}
	}
	if needsFallback && s.rateFetcher != nil {
		rate, err := s.rateFetcher.GetRate(ctx, domain.CurrencyJPY)
		if err != nil {
			slog.Warn("failed to fetch fallback JPY exchange rate", "error", err)
		} else {
			fallbackJPYRate = rate
		}
	}

	type categoryAcc struct {
		total    uint64
		totalTWD decimal.Decimal
		currency domain.Currency
	}

	accMap := make(map[domain.Category]*categoryAcc)
	var grandTotalTWD decimal.Decimal

	for _, exp := range expenses {
		acc, ok := accMap[exp.Category]
		if !ok {
			acc = &categoryAcc{}
			accMap[exp.Category] = acc
		}
		acc.total += exp.Price
		acc.currency = exp.Currency

		var twdAmount decimal.Decimal
		if exp.Currency == domain.CurrencyJPY && exp.ExchangeRate.IsZero() && !fallbackJPYRate.IsZero() {
			twdAmount = exp.PriceDecimal().Mul(fallbackJPYRate)
		} else {
			twdAmount = exp.TotalInTWD()
		}
		acc.totalTWD = acc.totalTWD.Add(twdAmount)
		grandTotalTWD = grandTotalTWD.Add(twdAmount)
	}

	// Build ordered category summaries, skip categories with zero spend.
	categories := domain.CategoryValues()
	items := make([]CategorySummary, 0, len(accMap))
	for _, cat := range categories {
		acc, ok := accMap[cat]
		if !ok || acc.total == 0 {
			continue
		}
		items = append(items, CategorySummary{
			Category: cat,
			Total:    acc.total,
			Currency: acc.currency,
			TotalTWD: acc.totalTWD,
		})
	}

	return &TodaySummary{
		Date:          startOfDay,
		Items:         items,
		GrandTotalTWD: grandTotalTWD,
		ItemCount:     len(expenses),
	}, nil
}

// CreateFromAnalysis creates expense(s) from a pre-computed receipt analysis.
// imageData is used only for uploading the receipt image; pass nil to skip uploading.
// If splitItems is false, one expense is created using the receipt summary total.
// If splitItems is true, one expense per receipt item is created; partial failures are non-fatal.
func (s *Service) CreateFromAnalysis(ctx context.Context, analysis *domain.ReceiptAnalysis, imageData []byte, notionUserID string, splitItems bool) error {
	dbID, err := s.activeDBID(ctx)
	if err != nil {
		return err
	}

	receiptURL := ""
	if len(imageData) > 0 {
		// Write image to a temp file so the repo can upload it.
		tmpFile, err := os.CreateTemp("", "receipt-*.jpg")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.Write(imageData); err != nil {
			tmpFile.Close()
			return err
		}
		tmpFile.Close()

		url, err := s.repo.UploadFile(ctx, tmpPath)
		if err != nil {
			slog.Warn("failed to upload receipt image; continuing without receipt URL", "error", err)
		} else {
			receiptURL = url
		}
	}

	now := time.Now()

	if !splitItems {
		expense := &domain.Expense{
			Name:         analysis.Summary,
			Price:        analysis.Total,
			Currency:     analysis.Currency,
			Category:     domain.Category食,
			Method:       domain.PaymentMethodCash,
			PaidByID:     notionUserID,
			ShoppedAt:    now,
			ReceiptURL:   receiptURL,
			ReceiptItems: analysis.Items,
		}
		return s.repo.CreateExpense(ctx, dbID, expense)
	}

	// Split mode: one expense per item; partial failures are logged but do not abort.
	// Skip items with non-positive prices (e.g. discounts or adjustments).
	for _, item := range analysis.Items {
		if item.Price <= 0 {
			slog.Debug("skipping non-positive receipt item", "name", item.DisplayName(), "price", item.Price)
			continue
		}
		expense := &domain.Expense{
			Name:       item.DisplayName(),
			Price:      uint64(item.Price),
			Currency:   analysis.Currency,
			Category:   item.Category,
			Method:     domain.PaymentMethodCash,
			PaidByID:   notionUserID,
			ShoppedAt:  now,
			ReceiptURL: receiptURL,
		}
		if err := s.repo.CreateExpense(ctx, dbID, expense); err != nil {
			slog.Warn("failed to create expense for receipt item",
				"item", item.DisplayName(),
				"error", err,
			)
		}
	}

	return nil
}

// CreateFromReceipt analyzes a receipt image and creates expense record(s).
// This is a convenience wrapper around AnalyzeReceipt + CreateFromAnalysis.
// If splitItems is false, one expense is created using the receipt summary total.
// If splitItems is true, one expense per receipt item is created; partial failures are non-fatal.
func (s *Service) CreateFromReceipt(ctx context.Context, imageData []byte, notionUserID string, splitItems bool) error {
	analysis, err := s.AnalyzeReceipt(ctx, imageData)
	if err != nil {
		return err
	}
	return s.CreateFromAnalysis(ctx, analysis, imageData, notionUserID, splitItems)
}

// FetchExchangeRate returns the exchange rate for the given currency.
// Returns zero + nil error when no rateFetcher is configured.
func (s *Service) FetchExchangeRate(ctx context.Context, currency domain.Currency) (decimal.Decimal, error) {
	if s.rateFetcher == nil {
		return decimal.Zero, nil
	}
	return s.rateFetcher.GetRate(ctx, currency)
}

// HasReceiptAnalyzer reports whether a receipt analyzer is configured.
func (s *Service) HasReceiptAnalyzer() bool {
	return s.receiptAnalyzer != nil
}

// AnalyzeReceipt delegates to the receipt analyzer.
// Returns an error if no analyzer is configured.
func (s *Service) AnalyzeReceipt(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error) {
	if s.receiptAnalyzer == nil {
		return nil, errors.New("receipt analyzer not available")
	}
	return s.receiptAnalyzer.Analyze(ctx, imageData)
}
