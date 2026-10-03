package expense

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

type spyRepo struct {
	membersErr      error
	opened          []domain.Trip
	expenses        []domain.Expense
	createdExpenses []*domain.Expense
	uploadFileFn    func(ctx context.Context, filePath string) (string, error)
}

func (m *spyRepo) CreateExpense(_ context.Context, trip domain.Trip, expense *domain.Expense) error {
	m.opened = append(m.opened, trip)
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *spyRepo) QueryExpenses(_ context.Context, trip domain.Trip) ([]domain.Expense, error) {
	m.opened = append(m.opened, trip)
	return m.expenses, nil
}

func (m *spyRepo) QueryExpensesWithFilter(_ context.Context, trip domain.Trip, _ ExpenseFilter) ([]domain.Expense, error) {
	m.opened = append(m.opened, trip)
	return m.expenses, nil
}

func (m *spyRepo) UpdateExpense(_ context.Context, trip domain.Trip, _ *domain.Expense) error {
	m.opened = append(m.opened, trip)
	return nil
}

func (m *spyRepo) DeleteExpense(_ context.Context, trip domain.Trip, _ string) error {
	m.opened = append(m.opened, trip)
	return nil
}

func (m *spyRepo) Settlement(_ context.Context, trip domain.Trip) (*domain.Settlement, error) {
	m.opened = append(m.opened, trip)
	return &domain.Settlement{Currency: domain.CurrencyTWD}, nil
}

func (m *spyRepo) Members(_ context.Context, trip domain.Trip) ([]domain.Member, error) {
	m.opened = append(m.opened, trip)
	return []domain.Member{{ID: "8", Name: "bob"}}, m.membersErr
}

func (m *spyRepo) UploadFile(ctx context.Context, trip domain.Trip, filePath string) (string, error) {
	m.opened = append(m.opened, trip)
	if m.uploadFileFn != nil {
		return m.uploadFileFn(ctx, filePath)
	}
	return "https://example.com/receipt.jpg", nil
}

type spyRateFetcher struct {
	rate   decimal.Decimal
	err    error
	called bool
	asked  [][2]domain.Currency
}

func (m *spyRateFetcher) GetRate(_ context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	m.called = true
	m.asked = append(m.asked, [2]domain.Currency{source, target})
	return m.rate, m.err
}

var testTrip = domain.Trip{ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyTWD}

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

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(summary.Items))
	}
	if summary.ItemCount != 0 {
		t.Errorf("expected ItemCount 0, got %d", summary.ItemCount)
	}
	if !summary.GrandTotal.IsZero() {
		t.Errorf("expected zero grand total, got %s", summary.GrandTotal)
	}
}

func TestGetTodaySummary_TWDOnly(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "lunch", Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood},
			{Name: "dinner", Price: 200, Currency: domain.CurrencyTWD, Category: domain.CategoryFood},
			{Name: "taxi", Price: 150, Currency: domain.CurrencyTWD, Category: domain.CategoryTransport},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ItemCount != 3 {
		t.Errorf("expected ItemCount 3, got %d", summary.ItemCount)
	}

	expected := decimal.NewFromInt(450)
	if !summary.GrandTotal.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotal)
	}

	if len(summary.Items) != 2 {
		t.Errorf("expected 2 category items, got %d", len(summary.Items))
	}

	var foodItem *CategorySummary
	for i := range summary.Items {
		if summary.Items[i].Category == domain.CategoryFood {
			foodItem = &summary.Items[i]
		}
	}
	if foodItem == nil {
		t.Fatal("expected food category in summary")
	}
	if !foodItem.Total.Equal(decimal.NewFromInt(300)) {
		t.Errorf("expected food total 300, got %s", foodItem.Total)
	}
}

func TestGetTodaySummary_JPYWithStoredRate(t *testing.T) {
	rate := decimal.NewFromFloat(0.22)
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: rate, Category: domain.CategoryFood},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := decimal.NewFromInt(1000).Mul(rate)
	if !summary.GrandTotal.Equal(expected) {
		t.Errorf("expected grand total %s, got %s", expected, summary.GrandTotal)
	}
}

func TestGetTodaySummary_CategoryOrder(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "taxi", Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryTransport},
			{Name: "lunch", Price: 200, Currency: domain.CurrencyTWD, Category: domain.CategoryFood},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(summary.Items))
	}
	if summary.Items[0].Category != domain.CategoryFood {
		t.Errorf("expected first category to be food, got %s", summary.Items[0].Category)
	}
	if summary.Items[1].Category != domain.CategoryTransport {
		t.Errorf("expected second category to be transport, got %s", summary.Items[1].Category)
	}
}

func TestGetTodaySummary_GroupsInNoccountingsCategoryOrder(t *testing.T) {
	repo := &spyRepo{
		expenses: []domain.Expense{
			{Name: "stationery", Price: 60, Currency: domain.CurrencyTWD, Category: domain.CategoryOther},
			{Name: "tickets", Price: 70, Currency: domain.CurrencyTWD, Category: domain.CategoryActivities},
			{Name: "souvenirs", Price: 80, Currency: domain.CurrencyTWD, Category: domain.CategoryShopping},
			{Name: "taxi", Price: 90, Currency: domain.CurrencyTWD, Category: domain.CategoryTransport},
			{Name: "hotel", Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryAccommodation},
			{Name: "ramen", Price: 110, Currency: domain.CurrencyTWD, Category: domain.CategoryFood},
		},
	}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []domain.Category{
		domain.CategoryFood,
		domain.CategoryTransport,
		domain.CategoryShopping,
		domain.CategoryActivities,
		domain.CategoryAccommodation,
		domain.CategoryOther,
	}
	if len(summary.Items) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(summary.Items))
	}
	for i, category := range want {
		if summary.Items[i].Category != category {
			t.Errorf("item %d is %s, want %s — the groups must follow domain.CategoryValues()", i, summary.Items[i].Category, category)
		}
	}
}

func TestCreateFromReceipt_NoAnalyzer(t *testing.T) {
	svc := NewService(&spyRepo{}, nil, nil)
	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("data"), "user1", false)
	if err == nil || err.Error() != "receipt analyzer not available" {
		t.Errorf("expected 'receipt analyzer not available', got %v", err)
	}
}

func TestCreateFromReceipt_AnalyzerError(t *testing.T) {
	analyzer := &stubReceiptAnalyzer{err: errors.New("analyze failed")}
	svc := NewService(&spyRepo{}, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("data"), "user1", false)
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
			{Name: "牛丼", Price: 500, Category: domain.CategoryFood},
			{Name: "味噌湯", Price: 350, Category: domain.CategoryFood},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{}
	svc := NewService(repo, nil, analyzer)

	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("imagedata"), "userXYZ", false)
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
	if exp.Category != domain.CategoryFood {
		t.Errorf("expected category food, got %s", exp.Category)
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
			{Name: "茶葉蛋", Price: 10, Category: domain.CategoryFood},
			{Name: "御飯糰", Price: 35, Category: domain.CategoryFood},
			{Name: "洗衣精", Price: 99, Category: domain.CategoryOther},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{}
	svc := NewService(repo, nil, analyzer)

	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("imagedata"), "userABC", true)
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
	if repo.createdExpenses[2].Category != domain.CategoryOther {
		t.Errorf("expected category other for item 2, got %s", repo.createdExpenses[2].Category)
	}
}

type fakeFailOnSecondCallRepo struct {
	spyRepo
	callCount int
}

func (r *fakeFailOnSecondCallRepo) CreateExpense(ctx context.Context, trip domain.Trip, expense *domain.Expense) error {
	r.callCount++
	if r.callCount == 2 {
		return errors.New("db error on second item")
	}
	return r.spyRepo.CreateExpense(ctx, trip, expense)
}

func TestCreateFromReceipt_SplitMode_PartialFailure(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "test",
		Total:    100,
		Currency: domain.CurrencyTWD,
		Items: []domain.ReceiptItem{
			{Name: "item1", Price: 50, Category: domain.CategoryFood},
			{Name: "item2", Price: 50, Category: domain.CategoryFood},
		},
	}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &fakeFailOnSecondCallRepo{}

	svc := NewService(repo, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("data"), "u1", true)
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

func TestCreateFromReceipt_UploadError_GracefulDegradation(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{Summary: "test", Total: 100, Currency: domain.CurrencyTWD}
	analyzer := &stubReceiptAnalyzer{analysis: analysis}
	repo := &spyRepo{
		uploadFileFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("upload failed")
		},
	}
	svc := NewService(repo, nil, analyzer)
	err := svc.CreateFromReceipt(context.Background(), testTrip, []byte("data"), "u1", false)
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
	rate, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
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

	rate, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
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

	_, err := svc.FetchExchangeRate(context.Background(), domain.CurrencyJPY, domain.CurrencyTWD)
	if err == nil || err.Error() != "timeout" {
		t.Errorf("expected 'timeout', got %v", err)
	}
}

func TestDelegation_CreateExpense(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	exp := &domain.Expense{Name: "test", Price: 100, Currency: domain.CurrencyTWD}
	if err := svc.CreateExpense(context.Background(), testTrip, exp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.createdExpenses) != 1 {
		t.Errorf("expected 1 created expense, got %d", len(repo.createdExpenses))
	}
}

func TestGetTodaySummary_DateFieldSet(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{}}
	svc := NewService(repo, nil, nil)

	now := time.Now()
	before := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
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
			{Name: "牛丼", Price: 500, Category: domain.CategoryFood},
			{Name: "味噌湯", Price: 350, Category: domain.CategoryFood},
		},
	}
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, []byte("imagedata"), "userXYZ", false)
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
	if exp.Category != domain.CategoryFood {
		t.Errorf("expected category food, got %s", exp.Category)
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
			{Name: "茶葉蛋", Price: 10, Category: domain.CategoryFood},
			{Name: "御飯糰", Price: 35, Category: domain.CategoryFood},
			{Name: "洗衣精", Price: 99, Category: domain.CategoryOther},
		},
	}
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, []byte("imagedata"), "userABC", true)
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
	if repo.createdExpenses[2].Category != domain.CategoryOther {
		t.Errorf("expected category other for item 2, got %s", repo.createdExpenses[2].Category)
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

	err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, nil, "u1", false)
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
	err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, []byte("data"), "u1", false)
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
			{Name: "item1", Price: 50, Category: domain.CategoryFood},
			{Name: "item2", Price: 50, Category: domain.CategoryFood},
		},
	}
	repo := &fakeFailOnSecondCallRepo{}
	svc := NewService(repo, nil, nil)

	err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, []byte("data"), "u1", true)
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

func TestService_CreateExpenseReachesTheRepo(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)

	expense := &domain.Expense{Name: "x"}
	if err := svc.CreateExpense(context.Background(), testTrip, expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("repo got %d creates, want 1", len(repo.createdExpenses))
	}
	if repo.createdExpenses[0] != expense {
		t.Error("repo got a different expense than the one passed in")
	}
}

func TestService_QueryExpensesReachesTheRepo(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{{Name: "拉麵"}}}
	svc := NewService(repo, nil, nil)

	expenses, err := svc.QueryExpenses(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}
	if len(expenses) != 1 || expenses[0].Name != "拉麵" {
		t.Errorf("QueryExpenses() = %+v, want the repo's expenses", expenses)
	}
}

func TestService_PassesTheTripToEachRepositoryCall(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	osaka := domain.Trip{ID: 9, Title: "Osaka", Currency: domain.CurrencyJPY}
	ctx := context.Background()

	_ = svc.CreateExpense(ctx, osaka, &domain.Expense{Name: "x"})
	_, _ = svc.QueryExpenses(ctx, osaka)
	_, _ = svc.Settlement(ctx, osaka)
	_, _ = svc.Members(ctx, osaka)

	if len(repo.opened) != 5 {
		t.Fatalf("books opened %d times, want once per call", len(repo.opened))
	}
	for _, opened := range repo.opened {
		if opened != osaka {
			t.Errorf("books opened for %+v, want the trip passed in", opened)
		}
	}
}

func TestService_SettlementAndMembersReachTheRepo(t *testing.T) {
	svc := NewService(&spyRepo{}, nil, nil)

	settlement, err := svc.Settlement(context.Background(), testTrip)
	if err != nil || settlement == nil {
		t.Fatalf("Settlement() = %v, %v, want the repo's", settlement, err)
	}
	members, err := svc.Members(context.Background(), testTrip)
	if err != nil || len(members) != 1 {
		t.Fatalf("Members() = %v, %v, want the repo's", members, err)
	}
}

func TestGetTodaySummary_ReportsInTheTripsCurrency(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{
		{Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, Category: domain.CategoryFood},
		{Name: "gift", Price: 100, Currency: domain.CurrencyTWD, ExchangeRate: decimal.NewFromFloat(4.65), Category: domain.CategoryShopping},
	}}
	svc := NewService(repo, nil, nil)
	osaka := domain.Trip{ID: 9, Title: "Osaka", Currency: domain.CurrencyJPY}

	summary, err := svc.GetTodaySummary(context.Background(), osaka)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.Currency != domain.CurrencyJPY {
		t.Errorf("Currency = %s, want the trip's", summary.Currency)
	}
	if want := decimal.NewFromInt(1465); !summary.GrandTotal.Equal(want) {
		t.Errorf("GrandTotal = %s, want %s", summary.GrandTotal, want)
	}
}

func TestGetTodaySummary_SumsMixedCurrenciesInTheTripsCurrency(t *testing.T) {
	repo := &spyRepo{expenses: []domain.Expense{
		{Name: "twd food", Price: 100, Currency: domain.CurrencyTWD, Category: domain.CategoryFood},
		{Name: "jpy food", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: decimal.NewFromFloat(0.22), Category: domain.CategoryFood},
	}}
	svc := NewService(repo, nil, nil)

	summary, err := svc.GetTodaySummary(context.Background(), testTrip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summary.Items) != 1 || !summary.Items[0].Total.Equal(decimal.NewFromInt(320)) {
		t.Errorf("Items = %+v, want one food bucket of 320 TWD", summary.Items)
	}
}

func TestExchangeRates_QuotesEveryPairBothWays(t *testing.T) {
	fetcher := &spyRateFetcher{rate: decimal.NewFromFloat(0.215)}
	svc := NewService(&spyRepo{}, fetcher, nil)

	quotes := svc.ExchangeRates(context.Background())

	if len(quotes) != 2 {
		t.Fatalf("quotes = %+v, want JPY to TWD and TWD to JPY", quotes)
	}
	seen := map[[2]domain.Currency]bool{}
	for _, quote := range quotes {
		seen[[2]domain.Currency{quote.From, quote.To}] = true
	}
	for _, pair := range [][2]domain.Currency{{domain.CurrencyJPY, domain.CurrencyTWD}, {domain.CurrencyTWD, domain.CurrencyJPY}} {
		if !seen[pair] {
			t.Errorf("no quote for %s to %s in %+v", pair[0], pair[1], quotes)
		}
	}
}

func TestExchangeRates_LeavesOutAPairThatCannotBeQuoted(t *testing.T) {
	svc := NewService(&spyRepo{}, &spyRateFetcher{err: errors.New("down")}, nil)

	if quotes := svc.ExchangeRates(context.Background()); len(quotes) != 0 {
		t.Errorf("quotes = %+v, want none when the provider is down", quotes)
	}
}

func TestExchangeRates_NoFetcher(t *testing.T) {
	svc := NewService(&spyRepo{}, nil, nil)

	if quotes := svc.ExchangeRates(context.Background()); len(quotes) != 0 {
		t.Errorf("quotes = %+v, want none without a rate fetcher", quotes)
	}
}

func TestCreateExpense_DefaultsParticipantsToAllTripMembers(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	e := &domain.Expense{Name: "dinner"}
	if err := svc.CreateExpense(context.Background(), testTrip, e); err != nil {
		t.Fatal(err)
	}
	if len(e.ParticipantIDs) != 1 || e.ParticipantIDs[0] != "8" {
		t.Fatalf("participants = %v, want all trip members", e.ParticipantIDs)
	}
}

func TestUpdateExpense_DefaultsParticipantsToAllTripMembers(t *testing.T) {
	repo := &spyRepo{}
	svc := NewService(repo, nil, nil)
	e := &domain.Expense{Name: "dinner"}
	if err := svc.UpdateExpense(context.Background(), testTrip, e); err != nil {
		t.Fatal(err)
	}
	if len(e.ParticipantIDs) != 1 || e.ParticipantIDs[0] != "8" {
		t.Fatalf("participants = %v, want all trip members", e.ParticipantIDs)
	}
}

func TestCreateFromAnalysis_DefaultsParticipantsForEveryReceiptItem(t *testing.T) {
	for _, split := range []bool{false, true} {
		repo := &spyRepo{}
		svc := NewService(repo, nil, nil)
		analysis := &domain.ReceiptAnalysis{Summary: "dinner", Total: 100, Currency: domain.CurrencyTWD, Items: []domain.ReceiptItem{{Name: "soup", Price: 40}, {Name: "rice", Price: 60}}}
		if err := svc.CreateFromAnalysis(context.Background(), testTrip, analysis, nil, "8", split); err != nil {
			t.Fatal(err)
		}
		for _, e := range repo.createdExpenses {
			if len(e.ParticipantIDs) != 1 || e.ParticipantIDs[0] != "8" {
				t.Fatalf("split=%v, participants=%v, want all members", split, e.ParticipantIDs)
			}
		}
	}
}

func TestCreateFromAnalysis_StopsWhenRosterCannotBeRead(t *testing.T) {
	for _, split := range []bool{false, true} {
		want := errors.New("roster unavailable")
		repo := &spyRepo{membersErr: want}
		svc := NewService(repo, nil, nil)
		err := svc.CreateFromAnalysis(context.Background(), testTrip, &domain.ReceiptAnalysis{}, nil, "8", split)
		if !errors.Is(err, want) {
			t.Fatalf("split=%v, error=%v, want roster error", split, err)
		}
		if len(repo.createdExpenses) != 0 {
			t.Fatal("receipt created expenses without a roster")
		}
	}
}
