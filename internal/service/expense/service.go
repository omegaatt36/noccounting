package expense

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

type TodaySummary struct {
	Date       time.Time
	Currency   domain.Currency
	Items      []CategorySummary
	GrandTotal decimal.Decimal
	ItemCount  int
}

type CategorySummary struct {
	Category domain.Category
	Total    decimal.Decimal
}

type Service struct {
	repo            AccountingRepo
	rateFetcher     ExchangeRateFetcher // optional
	receiptAnalyzer ReceiptAnalyzer     // optional
}

func NewService(repo AccountingRepo, rateFetcher ExchangeRateFetcher, receiptAnalyzer ReceiptAnalyzer) *Service {
	return &Service{
		repo:            repo,
		rateFetcher:     rateFetcher,
		receiptAnalyzer: receiptAnalyzer,
	}
}

func (s *Service) CreateExpense(ctx context.Context, trip domain.Trip, expense *domain.Expense) error {
	if err := s.defaultParticipants(ctx, trip, expense); err != nil {
		return err
	}
	return s.repo.CreateExpense(ctx, trip, expense)
}

func (s *Service) QueryExpensesWithFilter(ctx context.Context, trip domain.Trip, filter ExpenseFilter) ([]domain.Expense, error) {
	return s.repo.QueryExpensesWithFilter(ctx, trip, filter)
}

func (s *Service) GetExpense(ctx context.Context, trip domain.Trip, id string) (*domain.Expense, error) {
	expenses, err := s.repo.QueryExpenses(ctx, trip)
	if err != nil {
		return nil, err
	}
	for i := range expenses {
		if expenses[i].ID == id {
			return &expenses[i], nil
		}
	}
	return nil, domain.ErrExpenseNotFound
}

func (s *Service) UpdateExpense(ctx context.Context, trip domain.Trip, expense *domain.Expense) error {
	if err := s.defaultParticipants(ctx, trip, expense); err != nil {
		return err
	}
	return s.repo.UpdateExpense(ctx, trip, expense)
}

func (s *Service) DeleteExpense(ctx context.Context, trip domain.Trip, id string) error {
	return s.repo.DeleteExpense(ctx, trip, id)
}

func (s *Service) Settlement(ctx context.Context, trip domain.Trip) (*domain.Settlement, error) {
	return s.repo.Settlement(ctx, trip)
}

func (s *Service) Members(ctx context.Context, trip domain.Trip) ([]domain.Member, error) {
	return s.repo.Members(ctx, trip)
}

func (s *Service) GetTodaySummary(ctx context.Context, trip domain.Trip, loc ...*time.Location) (*TodaySummary, error) {
	location := time.Local
	if len(loc) > 0 && loc[0] != nil {
		location = loc[0]
	}

	now := time.Now().In(location)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	endOfDay := startOfDay.Add(24 * time.Hour).Add(-time.Nanosecond)

	expenses, err := s.repo.QueryExpensesWithFilter(ctx, trip, ExpenseFilter{
		DateFrom: &startOfDay,
		DateTo:   &endOfDay,
	})
	if err != nil {
		return nil, err
	}

	totals := make(map[domain.Category]decimal.Decimal)
	var grandTotal decimal.Decimal
	for _, exp := range expenses {
		amount := exp.TotalInBase(trip.Currency)
		totals[exp.Category] = totals[exp.Category].Add(amount)
		grandTotal = grandTotal.Add(amount)
	}

	// Categories keep domain.CategoryValues order so output order is deterministic
	var items []CategorySummary
	for _, cat := range domain.CategoryValues() {
		total, exists := totals[cat]
		if exists && !total.IsZero() {
			items = append(items, CategorySummary{
				Category: cat,
				Total:    total,
			})
		}
	}

	return &TodaySummary{
		Date:       startOfDay,
		Currency:   trip.Currency,
		Items:      items,
		GrandTotal: grandTotal,
		ItemCount:  len(expenses),
	}, nil
}

// UploadReceiptImage returns the file id to link an expense to; no image is no file and no error.
func (s *Service) UploadReceiptImage(ctx context.Context, trip domain.Trip, imageData []byte) (string, error) {
	if len(imageData) == 0 {
		return "", nil
	}
	tmpFile, err := os.CreateTemp("", "receipt-*.jpg")
	if err != nil {
		return "", fmt.Errorf("creating a temp file for the receipt image: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(imageData); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing the receipt image to a temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("closing the receipt image temp file: %w", err)
	}

	return s.repo.UploadFile(ctx, trip, tmpPath)
}

// RemainderName is the line a split receipt gets for what its items do not add up to (tax, service fees), so the lines sum to the total.
const RemainderName = "稅金／服務費等"

type ReceiptRequest struct {
	Analysis   *domain.ReceiptAnalysis
	Image      []byte
	PaidByID   string
	ShoppedAt  time.Time
	SplitItems bool
}

type ReceiptResult struct {
	Filed  []domain.ReceiptItem
	Failed []domain.ReceiptItem
	// ReceiptMissing is set when there was an image that could not be attached.
	ReceiptMissing bool
}

// CreateFromAnalysis treats a split that files some lines and loses others as no error: the result names both. Filing nothing is one.
func (s *Service) CreateFromAnalysis(ctx context.Context, trip domain.Trip, req ReceiptRequest) (ReceiptResult, error) {
	var result ReceiptResult
	analysis := req.Analysis

	defaultSplit := &domain.Expense{}
	if err := s.defaultParticipants(ctx, trip, defaultSplit); err != nil {
		return result, err
	}

	receiptURL, err := s.UploadReceiptImage(ctx, trip, req.Image)
	if err != nil {
		slog.Warn("failed to upload the receipt image; filing without it", "error", err)
		result.ReceiptMissing = true
	}

	category := analysis.Category
	if !category.IsValid() {
		if len(analysis.Items) > 0 && analysis.Items[0].Category.IsValid() {
			category = analysis.Items[0].Category
		} else {
			category = domain.CategoryFood
		}
	}
	method := analysis.PaymentMethod
	if !method.IsValid() {
		method = domain.PaymentMethodCash
	}

	if !req.SplitItems {
		expense := &domain.Expense{
			Name:           analysis.Summary,
			Price:          analysis.Total,
			Currency:       analysis.Currency,
			Category:       category,
			Method:         method,
			PaidByID:       req.PaidByID,
			ShoppedAt:      req.ShoppedAt,
			ReceiptURL:     receiptURL,
			ParticipantIDs: defaultSplit.ParticipantIDs,
			ReceiptItems:   analysis.Items,
		}
		if err := s.CreateExpense(ctx, trip, expense); err != nil {
			return result, err
		}
		result.Filed = []domain.ReceiptItem{{Name: analysis.Summary, Price: int64(analysis.Total), Category: category}}
		return result, nil
	}

	var firstErr error
	for _, item := range splitLines(analysis, category) {
		itemCat := item.Category
		if !itemCat.IsValid() {
			itemCat = category
		}
		expense := &domain.Expense{
			Name:           item.Name,
			Price:          uint64(item.Price),
			Currency:       analysis.Currency,
			Category:       itemCat,
			Method:         method,
			PaidByID:       req.PaidByID,
			ShoppedAt:      req.ShoppedAt,
			ReceiptURL:     receiptURL,
			ParticipantIDs: defaultSplit.ParticipantIDs,
		}
		if err := s.CreateExpense(ctx, trip, expense); err != nil {
			slog.Warn("failed to create expense for receipt item", "item", item.Name, "error", err)
			result.Failed = append(result.Failed, item)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Filed = append(result.Filed, item)
	}

	if len(result.Filed) == 0 && firstErr != nil {
		return result, firstErr
	}
	return result, nil
}

// splitLines are the items with a price, plus a last line for the difference to the receipt's total when they fall short.
func splitLines(analysis *domain.ReceiptAnalysis, category domain.Category) []domain.ReceiptItem {
	lines := make([]domain.ReceiptItem, 0, len(analysis.Items)+1)
	var sum int64
	for _, item := range analysis.Items {
		if item.Price <= 0 {
			slog.Debug("skipping non-positive receipt item", "name", item.Name, "price", item.Price)
			continue
		}
		lines = append(lines, item)
		sum += item.Price
	}
	if len(lines) > 0 && analysis.Total > uint64(sum) {
		lines = append(lines, domain.ReceiptItem{
			Name:     RemainderName,
			Price:    int64(analysis.Total) - sum,
			Category: category,
		})
	}
	return lines
}

func (s *Service) FetchExchangeRate(ctx context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	if s.rateFetcher == nil {
		return decimal.Zero, nil
	}
	return s.rateFetcher.GetRate(ctx, source, target)
}

func (s *Service) ExchangeRates(ctx context.Context) []domain.RateQuote {
	var quotes []domain.RateQuote
	for _, source := range domain.CurrencyValues() {
		for _, target := range domain.CurrencyValues() {
			if source == target {
				continue
			}
			rate, err := s.FetchExchangeRate(ctx, source, target)
			if err != nil || rate.IsZero() {
				slog.Warn("could not quote an exchange rate", "from", source, "to", target, "error", err)
				continue
			}
			quotes = append(quotes, domain.RateQuote{From: source, To: target, Rate: rate})
		}
	}
	return quotes
}

func (s *Service) HasReceiptAnalyzer() bool {
	return s.receiptAnalyzer != nil
}

func (s *Service) AnalyzeReceipt(ctx context.Context, imageData []byte) (*domain.ReceiptAnalysis, error) {
	if s.receiptAnalyzer == nil {
		return nil, errors.New("receipt analyzer not available")
	}
	return s.receiptAnalyzer.Analyze(ctx, imageData)
}

func (s *Service) defaultParticipants(ctx context.Context, trip domain.Trip, e *domain.Expense) error {
	if e == nil || len(e.ParticipantIDs) != 0 {
		return nil
	}
	members, err := s.repo.Members(ctx, trip)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return domain.ErrNotOnTrip
	}
	e.ParticipantIDs = make([]string, 0, len(members))
	for _, member := range members {
		e.ParticipantIDs = append(e.ParticipantIDs, member.ID)
	}
	return nil
}
