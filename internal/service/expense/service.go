package expense

import (
	"context"
	"errors"
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
	rateFetcher     ExchangeRateFetcher // optional
	receiptAnalyzer ReceiptAnalyzer     // optional
}

// NewService creates a new Service. rateFetcher and receiptAnalyzer may be nil.
func NewService(repo AccountingRepo, rateFetcher ExchangeRateFetcher, receiptAnalyzer ReceiptAnalyzer) *Service {
	return &Service{
		repo:            repo,
		rateFetcher:     rateFetcher,
		receiptAnalyzer: receiptAnalyzer,
	}
}

// CreateExpense delegates to the repository.
func (s *Service) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	return s.repo.CreateExpense(ctx, expense)
}

// QueryExpenses delegates to the repository.
func (s *Service) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	return s.repo.QueryExpenses(ctx)
}

// QueryExpensesWithFilter delegates to the repository.
func (s *Service) QueryExpensesWithFilter(ctx context.Context, filter ExpenseFilter) ([]domain.Expense, error) {
	return s.repo.QueryExpensesWithFilter(ctx, filter)
}

// UpdateExpense delegates to the repository.
func (s *Service) UpdateExpense(ctx context.Context, expense *domain.Expense) error {
	return s.repo.UpdateExpense(ctx, expense)
}

// DeleteExpense delegates to the repository.
func (s *Service) DeleteExpense(ctx context.Context, id string) error {
	return s.repo.DeleteExpense(ctx, id)
}

// GetSummary delegates to the repository.
func (s *Service) GetSummary(ctx context.Context) (*domain.ExpenseSummary, error) {
	return s.repo.GetExpenseSummary(ctx)
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

	expenses, err := s.repo.QueryExpensesWithFilter(ctx, filter)
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
		return s.repo.CreateExpense(ctx, expense)
	}

	// Split mode: one expense per item; partial failures are logged but do not abort.
	for _, item := range analysis.Items {
		expense := &domain.Expense{
			Name:       item.DisplayName(),
			Price:      item.Price,
			Currency:   analysis.Currency,
			Category:   item.Category,
			Method:     domain.PaymentMethodCash,
			PaidByID:   notionUserID,
			ShoppedAt:  now,
			ReceiptURL: receiptURL,
		}
		if err := s.repo.CreateExpense(ctx, expense); err != nil {
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
