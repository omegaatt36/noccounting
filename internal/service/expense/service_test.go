package expense

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

type spyRepo struct {
	expenses        []domain.Expense
	createdExpenses []*domain.Expense
	uploadFileFn    func(ctx context.Context, filePath string) (string, error)
}

func (m *spyRepo) CreateExpense(_ context.Context, expense *domain.Expense) error {
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *spyRepo) QueryExpenses(_ context.Context) ([]domain.Expense, error) {
	return m.expenses, nil
}

func (m *spyRepo) QueryExpensesWithFilter(_ context.Context, _ ExpenseFilter) ([]domain.Expense, error) {
	return m.expenses, nil
}

func (m *spyRepo) UpdateExpense(_ context.Context, _ *domain.Expense) error { return nil }
func (m *spyRepo) DeleteExpense(_ context.Context, _ string) error          { return nil }
func (m *spyRepo) GetExpenseSummary(_ context.Context) (*domain.ExpenseSummary, error) {
	return &domain.ExpenseSummary{}, nil
}

func (m *spyRepo) UploadFile(ctx context.Context, filePath string) (string, error) {
	if m.uploadFileFn != nil {
		return m.uploadFileFn(ctx, filePath)
	}
	return "https://example.com/receipt.jpg", nil
}

type spyRateFetcher struct {
	rate   decimal.Decimal
	err    error
	called bool
}

func (m *spyRateFetcher) GetRate(_ context.Context, _ domain.Currency) (decimal.Decimal, error) {
	m.called = true
	return m.rate, m.err
}

type stubReceiptAnalyzer struct {
	analysis *domain.ReceiptAnalysis
	err      error
}

func (m *stubReceiptAnalyzer) Analyze(_ context.Context, _ []byte) (*domain.ReceiptAnalysis, error) {
	return m.analysis, m.err
}

func TestGetTodaySummary_EmptyExpenses(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{}}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(summary.Items))
	}
	if summary.ItemCount != 0 {
		t.Errorf("expected ItemCount 0, got %d", summary.ItemCount)
	}
	if !summary.GrandTotalTWD.IsZero() {
		t.Errorf("expected zero grand total, got %s", summary.GrandTotalTWD)
	}
}

func TestGetTodaySummary_TWDOnly(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "lunch", Price: 100, Currency: domain.CurrencyTWD, Category: domain.Category食},
			{Name: "dinner", Price: 200, Currency: domain.CurrencyTWD, Category: domain.Category食},
			{Name: "taxi", Price: 150, Currency: domain.CurrencyTWD, Category: domain.Category行},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ItemCount != 3 {
		t.Errorf("expected ItemCount 3, got %d", summary.ItemCount)
	}

	// GrandTotal = 100 + 200 + 150 = 450 TWD
	expected := decimal.NewFromInt(450)
	if !summary.GrandTotalTWD.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotalTWD)
	}

	// 2 categories
	if len(summary.Items) != 2 {
		t.Errorf("expected 2 category items, got %d", len(summary.Items))
	}

	// Find 食 category
	var foodItem *CategorySummary
	for i := range summary.Items {
		if summary.Items[i].Category == domain.Category食 {
			foodItem = &summary.Items[i]
		}
	}
	if foodItem == nil {
		t.Fatal("expected 食 category in summary")
	}
	if foodItem.Total != 300 {
		t.Errorf("expected 食 total 300, got %d", foodItem.Total)
	}
}

func TestGetTodaySummary_JPYWithStoredRate(t *testing.T) {
	rate := decimal.NewFromFloat(0.22)
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: rate, Category: domain.Category食},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1000 JPY * 0.22 = 220 TWD
	expected := decimal.NewFromInt(1000).Mul(rate)
	if !summary.GrandTotalTWD.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotalTWD)
	}
}

func TestGetTodaySummary_JPYFallbackRate(t *testing.T) {
	fallback := decimal.NewFromFloat(0.21)
	fetcher := &spyRateFetcher{rate: fallback}
	repo := &spyRepo{
		expenses: []domain.Expense{
			// ExchangeRate is zero → should use fallback
			{Name: "convenience store", Price: 500, Currency: domain.CurrencyJPY, ExchangeRate: decimal.Zero, Category: domain.Category食},
		},
	}
	svc := NewService(repo, fetcher, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fetcher.called {
		t.Error("expected rateFetcher.GetRate to be called")
	}

	// 500 JPY * 0.21 = 105 TWD
	expected := decimal.NewFromInt(500).Mul(fallback)
	if !summary.GrandTotalTWD.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotalTWD)
	}
}

func TestGetTodaySummary_JPYNoFallbackWhenRateFetcherNil(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "sushi", Price: 2000, Currency: domain.CurrencyJPY, ExchangeRate: decimal.Zero, Category: domain.Category食},
		},
	}
	svc := NewService(repo, nil, nil) // no rateFetcher

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TotalInTWD returns price as-is when currency is JPY and rate is zero
	// (no conversion possible)
	if summary.ItemCount != 1 {
		t.Errorf("expected 1 item, got %d", summary.ItemCount)
	}
}

func TestGetTodaySummary_MixedCurrencies(t *testing.T) {
	fallback := decimal.NewFromFloat(0.20)
	fetcher := &spyRateFetcher{rate: fallback}
	storedRate := decimal.NewFromFloat(0.22)

	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "twd food", Price: 100, Currency: domain.CurrencyTWD, Category: domain.Category食},
			{Name: "jpy with rate", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: storedRate, Category: domain.Category食},
			{Name: "jpy no rate", Price: 500, Currency: domain.CurrencyJPY, ExchangeRate: decimal.Zero, Category: domain.Category購},
		},
	}
	svc := NewService(repo, fetcher, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 食: 100 TWD + 1000*0.22 = 100 + 220 = 320
	// 購: 500*0.20 = 100
	// Grand: 420
	expected := decimal.NewFromFloat(420)
	if !summary.GrandTotalTWD.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotalTWD)
	}
	if len(summary.Items) != 2 {
		t.Errorf("expected 2 category items, got %d", len(summary.Items))
	}
}

func TestGetTodaySummary_FallbackRateFetchError(t *testing.T) {
	fetcher := &spyRateFetcher{err: errors.New("network error")}
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "snack", Price: 300, Currency: domain.CurrencyJPY, ExchangeRate: decimal.Zero, Category: domain.Category食},
		},
	}
	svc := NewService(repo, fetcher, nil)

	// Should not return error even if rate fetch fails; falls back to TotalInTWD behavior.
	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.ItemCount != 1 {
		t.Errorf("expected 1 item, got %d", summary.ItemCount)
	}
}

func TestGetTodaySummary_CategoryOrder(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "taxi", Price: 100, Currency: domain.CurrencyTWD, Category: domain.Category行},
			{Name: "lunch", Price: 200, Currency: domain.CurrencyTWD, Category: domain.Category食},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(summary.Items))
	}
	// domain.CategoryValues() order: 食, 住, 行, 購, 樂, 雜
	if summary.Items[0].Category != domain.Category食 {
		t.Errorf("expected first category to be 食, got %s", summary.Items[0].Category)
	}
	if summary.Items[1].Category != domain.Category行 {
		t.Errorf("expected second category to be 行, got %s", summary.Items[1].Category)
	}
}

func TestCreateFromReceipt_NoAnalyzer(t *testing.T) {
	svc := NewService(&spyRepo{}, nil, nil)
	err := svc.CreateFromReceipt(context.Background(), []byte("data"), "user1", false)
	if err == nil || err.Error() != "receipt analyzer not available" {
		t.Errorf("expected 'receipt analyzer not available', got %v", err)
	}
}

func TestCreateFromReceipt_AnalyzerError(t *testing.T) {
	analyzer := &stubReceiptAnalyzer{err: errors.New("analyze failed")}
	svc := NewService(&spyRepo{}, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), []byte("data"), "user1", false)
	if err == nil || err.Error() != "analyze failed" {
		t.Errorf("expected 'analyze failed', got %v", err)
	}
}

func TestCreateFromReceipt_SingleMode(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "松屋 午餐",
		Total:    850,
		Currency: domain.CurrencyJPY,
		Items: []domain.ReceiptItem{
			{Name: "牛丼", Price: 500, Category: domain.Category食},
			{Name: "味噌湯", Price: 350, Category: domain.Category食},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{}
	svc := NewService(repo, nil, analyzer)

	err := svc.CreateFromReceipt(context.Background(), []byte("imagedata"), "userXYZ", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected 1 created expense, got %d", len(repo.createdExpenses))
	}

	exp := repo.createdExpenses[0]
	if exp.Name != "松屋 午餐" {
		t.Errorf("expected name '松屋 午餐', got '%s'", exp.Name)
	}
	if exp.Price != 850 {
		t.Errorf("expected price 850, got %d", exp.Price)
	}
	if exp.Category != domain.Category食 {
		t.Errorf("expected category 食, got %s", exp.Category)
	}
	if exp.Method != domain.PaymentMethodCash {
		t.Errorf("expected method cash, got %s", exp.Method)
	}
	if exp.PaidByID != "userXYZ" {
		t.Errorf("expected PaidByID 'userXYZ', got '%s'", exp.PaidByID)
	}
	if exp.ReceiptURL != "https://example.com/receipt.jpg" {
		t.Errorf("unexpected ReceiptURL: %s", exp.ReceiptURL)
	}
	if exp.ShoppedAt.IsZero() {
		t.Error("expected ShoppedAt to be set")
	}
}

func TestCreateFromReceipt_SplitMode(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "全家便利商店",
		Total:    350,
		Currency: domain.CurrencyTWD,
		Items: []domain.ReceiptItem{
			{Name: "茶葉蛋", Price: 10, Category: domain.Category食},
			{Name: "御飯糰", Price: 35, Category: domain.Category食},
			{Name: "洗衣精", Price: 99, Category: domain.Category雜},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{}
	svc := NewService(repo, nil, analyzer)

	err := svc.CreateFromReceipt(context.Background(), []byte("imagedata"), "userABC", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.createdExpenses) != 3 {
		t.Fatalf("expected 3 created expenses, got %d", len(repo.createdExpenses))
	}

	for i, exp := range repo.createdExpenses {
		if exp.PaidByID != "userABC" {
			t.Errorf("item %d: expected PaidByID 'userABC', got '%s'", i, exp.PaidByID)
		}
		if exp.ReceiptURL != "https://example.com/receipt.jpg" {
			t.Errorf("item %d: unexpected ReceiptURL: %s", i, exp.ReceiptURL)
		}
		if exp.ShoppedAt.IsZero() {
			t.Errorf("item %d: expected ShoppedAt to be set", i)
		}
	}

	// Verify names map to DisplayName()
	if repo.createdExpenses[0].Name != "茶葉蛋" {
		t.Errorf("expected '茶葉蛋', got '%s'", repo.createdExpenses[0].Name)
	}
	if repo.createdExpenses[2].Category != domain.Category雜 {
		t.Errorf("expected category 雜 for item 2, got %s", repo.createdExpenses[2].Category)
	}
}

// fakeFailOnSecondCallRepo wraps spyRepo and returns an error on the second CreateExpense call.
type fakeFailOnSecondCallRepo struct {
	spyRepo
	callCount int
}

func (r *fakeFailOnSecondCallRepo) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	r.callCount++
	if r.callCount == 2 {
		return errors.New("db error on second item")
	}
	return r.spyRepo.CreateExpense(ctx, expense)
}

func TestCreateFromReceipt_SplitMode_PartialFailure(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "test",
		Total:    100,
		Currency: domain.CurrencyTWD,
		Items: []domain.ReceiptItem{
			{Name: "item1", Price: 50, Category: domain.Category食},
			{Name: "item2", Price: 50, Category: domain.Category食},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &fakeFailOnSecondCallRepo{}

	svc := NewService(repo, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), []byte("data"), "u1", true)
	if err != nil {
		t.Fatalf("expected nil error on partial failure, got: %v", err)
	}

	// Second item failed, so only 1 expense should be successfully recorded.
	if len(repo.createdExpenses) != 1 {
		t.Errorf("expected 1 successfully created expense, got %d", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0].Name != "item1" {
		t.Errorf("expected first item 'item1', got '%s'", repo.createdExpenses[0].Name)
	}
}

func TestCreateFromReceipt_UploadError_GracefulDegradation(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{Summary: "test", Total: 100, Currency: domain.CurrencyTWD}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{
		uploadFileFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("upload failed")
		},
	}
	svc := NewService(repo, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), []byte("data"), "u1", false)
	if err != nil {
		t.Fatalf("expected no error on upload failure, got %v", err)
	}
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected expense to be created despite upload failure, got %d", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0].ReceiptURL != "" {
		t.Errorf("expected empty ReceiptURL, got %q", repo.createdExpenses[0].ReceiptURL)
	}
}

func TestFetchExchangeRate_NoFetcher(t *testing.T) {
	svc := NewService(&spyRepo{}, nil, nil)
	rate, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rate.IsZero() {
		t.Errorf("expected zero rate, got %s", rate)
	}
}

func TestFetchExchangeRate_WithFetcher(t *testing.T) {
	expected := decimal.NewFromFloat(0.215)
	fetcher := &spyRateFetcher{rate: expected}
	svc := NewService(&spyRepo{}, fetcher, nil)

	rate, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rate.Equal(expected) {
		t.Errorf("expected rate %s, got %s", expected, rate)
	}
	if !fetcher.called {
		t.Error("expected rateFetcher to be called")
	}
}

func TestFetchExchangeRate_FetcherError(t *testing.T) {
	fetcher := &spyRateFetcher{err: errors.New("timeout")}
	svc := NewService(&spyRepo{}, fetcher, nil)

	_, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY)
	if err == nil || err.Error() != "timeout" {
		t.Errorf("expected 'timeout', got %v", err)
	}
}

func TestDelegation_CreateExpense(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	exp := &domain.Expense{Name: "test", Price: 100, Currency: domain.CurrencyTWD}
	if err := svc.CreateExpense(context.Background(), exp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.createdExpenses) != 1 {
		t.Errorf("expected 1 created expense, got %d", len(repo.createdExpenses))
	}
}

func TestDelegation_GetSummary(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	summary, err := svc.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary == nil {
		t.Error("expected non-nil summary")
	}
}

func TestGetTodaySummary_DateFieldSet(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{}}
	svc := NewService(repo, nil, nil)

	now := time.Now()
	before := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	summary, err := svc.GetTodaySummary(context.Background())
	after := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Date.Before(before) || summary.Date.After(after) {
		t.Errorf("summary.Date %v not in expected range [%v, %v]", summary.Date, before, after)
	}
}

func TestCreateFromAnalysis_SingleMode(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "松屋 午餐",
		Total:    850,
		Currency: domain.CurrencyJPY,
		Items: []domain.ReceiptItem{
			{Name: "牛丼", Price: 500, Category: domain.Category食},
			{Name: "味噌湯", Price: 350, Category: domain.Category食},
		},
	}
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), analysis, []byte("imagedata"), "userXYZ", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected 1 created expense, got %d", len(repo.createdExpenses))
	}

	exp := repo.createdExpenses[0]
	if exp.Name != "松屋 午餐" {
		t.Errorf("expected name '松屋 午餐', got '%s'", exp.Name)
	}
	if exp.Price != 850 {
		t.Errorf("expected price 850, got %d", exp.Price)
	}
	if exp.Category != domain.Category食 {
		t.Errorf("expected category 食, got %s", exp.Category)
	}
	if exp.Method != domain.PaymentMethodCash {
		t.Errorf("expected method cash, got %s", exp.Method)
	}
	if exp.PaidByID != "userXYZ" {
		t.Errorf("expected PaidByID 'userXYZ', got '%s'", exp.PaidByID)
	}
	if exp.ReceiptURL != "https://example.com/receipt.jpg" {
		t.Errorf("unexpected ReceiptURL: %s", exp.ReceiptURL)
	}
	if exp.ShoppedAt.IsZero() {
		t.Error("expected ShoppedAt to be set")
	}
}

func TestCreateFromAnalysis_SplitMode(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "全家便利商店",
		Total:    350,
		Currency: domain.CurrencyTWD,
		Items: []domain.ReceiptItem{
			{Name: "茶葉蛋", Price: 10, Category: domain.Category食},
			{Name: "御飯糰", Price: 35, Category: domain.Category食},
			{Name: "洗衣精", Price: 99, Category: domain.Category雜},
		},
	}
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), analysis, []byte("imagedata"), "userABC", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.createdExpenses) != 3 {
		t.Fatalf("expected 3 created expenses, got %d", len(repo.createdExpenses))
	}

	for i, exp := range repo.createdExpenses {
		if exp.PaidByID != "userABC" {
			t.Errorf("item %d: expected PaidByID 'userABC', got '%s'", i, exp.PaidByID)
		}
		if exp.ReceiptURL != "https://example.com/receipt.jpg" {
			t.Errorf("item %d: unexpected ReceiptURL: %s", i, exp.ReceiptURL)
		}
		if exp.ShoppedAt.IsZero() {
			t.Errorf("item %d: expected ShoppedAt to be set", i)
		}
	}

	if repo.createdExpenses[0].Name != "茶葉蛋" {
		t.Errorf("expected '茶葉蛋', got '%s'", repo.createdExpenses[0].Name)
	}
	if repo.createdExpenses[2].Category != domain.Category雜 {
		t.Errorf("expected category 雜 for item 2, got %s", repo.createdExpenses[2].Category)
	}
}

func TestCreateFromAnalysis_NilImageData_SkipsUpload(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{Summary: "test", Total: 100, Currency: domain.CurrencyTWD}
	uploadCalled := false
	repo := &spyRepo{
		uploadFileFn: func(_ context.Context, _ string) (string, error) {
			uploadCalled = true
			return "https://example.com/receipt.jpg", nil
		},
	}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), analysis, nil, "u1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uploadCalled {
		t.Error("expected upload to be skipped when imageData is nil")
	}
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0].ReceiptURL != "" {
		t.Errorf("expected empty ReceiptURL, got %q", repo.createdExpenses[0].ReceiptURL)
	}
}

func TestCreateFromAnalysis_UploadError_GracefulDegradation(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{Summary: "test", Total: 100, Currency: domain.CurrencyTWD}
	repo := &spyRepo{
		uploadFileFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("upload failed")
		},
	}
	svc := NewService(repo, nil, nil)
	err := svc.CreateFromAnalysis(context.Background(), analysis, []byte("data"), "u1", false)
	if err != nil {
		t.Fatalf("expected no error on upload failure, got %v", err)
	}
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected expense to be created despite upload failure, got %d", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0].ReceiptURL != "" {
		t.Errorf("expected empty ReceiptURL, got %q", repo.createdExpenses[0].ReceiptURL)
	}
}

func TestCreateFromAnalysis_SplitMode_PartialFailure(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "test",
		Total:    100,
		Currency: domain.CurrencyTWD,
		Items: []domain.ReceiptItem{
			{Name: "item1", Price: 50, Category: domain.Category食},
			{Name: "item2", Price: 50, Category: domain.Category食},
		},
	}
	repo := &fakeFailOnSecondCallRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), analysis, []byte("data"), "u1", true)
	if err != nil {
		t.Fatalf("expected nil error on partial failure, got: %v", err)
	}

	if len(repo.createdExpenses) != 1 {
		t.Errorf("expected 1 successfully created expense, got %d", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0].Name != "item1" {
		t.Errorf("expected first item 'item1', got '%s'", repo.createdExpenses[0].Name)
	}
}
