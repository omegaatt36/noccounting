package expense

import (
	"context"
	"errors"
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

func (s *Service) QueryExpenses(ctx context.Context, trip domain.Trip) ([]domain.Expense, error) {
	return s.repo.QueryExpenses(ctx, trip)
}

func (s *Service) QueryExpensesWithFilter(ctx context.Context, trip domain.Trip, filter ExpenseFilter) ([]domain.Expense, error) {
	return s.repo.QueryExpensesWithFilter(ctx, trip, filter)
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

	items := make([]CategorySummary, 0, len(totals))
	for _, cat := range domain.CategoryValues() {
		if total, ok := totals[cat]; ok && !total.IsZero() {
			items = append(items, CategorySummary{Category: cat, Total: total})
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

func (s *Service) CreateFromAnalysis(ctx context.Context, trip domain.Trip, analysis *domain.ReceiptAnalysis, imageData []byte, backendUserID string, splitItems bool) error {
	repo := s.repo
	defaultSplit := &domain.Expense{}
	if err := s.defaultParticipants(ctx, trip, defaultSplit); err != nil {
		return err
	}
	receiptURL := ""
	if len(imageData) > 0 {
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

		url, err := repo.UploadFile(ctx, trip, tmpPath)
		if err != nil {
			slog.Warn("failed to upload receipt image; continuing without receipt URL", "error", err)
		} else {
			receiptURL = url
		}
	}

	now := time.Now()

	if !splitItems {
		expense := &domain.Expense{
			Name:           analysis.Summary,
			Price:          analysis.Total,
			Currency:       analysis.Currency,
			Category:       domain.CategoryFood,
			Method:         domain.PaymentMethodCash,
			PaidByID:       backendUserID,
			ShoppedAt:      now,
			ReceiptURL:     receiptURL,
			ParticipantIDs: defaultSplit.ParticipantIDs,
			ReceiptItems:   analysis.Items,
		}
		return s.CreateExpense(ctx, trip, expense)
	}

	for _, item := range analysis.Items {
		if item.Price <= 0 {
			slog.Debug("skipping non-positive receipt item", "name", item.Name, "price", item.Price)
			continue
		}
		expense := &domain.Expense{
			Name:           item.Name,
			Price:          uint64(item.Price),
			Currency:       analysis.Currency,
			Category:       item.Category,
			Method:         domain.PaymentMethodCash,
			PaidByID:       backendUserID,
			ShoppedAt:      now,
			ReceiptURL:     receiptURL,
			ParticipantIDs: defaultSplit.ParticipantIDs,
		}
		if err := s.CreateExpense(ctx, trip, expense); err != nil {
			slog.Warn("failed to create expense for receipt item",
				"item", item.Name,
				"error", err,
			)
		}
	}

	return nil
}

func (s *Service) CreateFromReceipt(ctx context.Context, trip domain.Trip, imageData []byte, backendUserID string, splitItems bool) error {
	analysis, err := s.AnalyzeReceipt(ctx, imageData)
	if err != nil {
		return err
	}
	return s.CreateFromAnalysis(ctx, trip, analysis, imageData, backendUserID, splitItems)
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
