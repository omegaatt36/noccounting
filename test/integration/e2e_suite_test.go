package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/webapp"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type inMemoryAccountingRepo struct {
	mu       sync.Mutex
	expenses []domain.Expense
	nextID   int
	queryErr error
}

func newInMemoryAccountingRepo() *inMemoryAccountingRepo {
	return &inMemoryAccountingRepo{
		nextID: 1,
	}
}

func (r *inMemoryAccountingRepo) CreateExpense(_ context.Context, _ domain.Trip, exp *domain.Expense) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp.ID = fmt.Sprintf("exp-%d", r.nextID)
	r.nextID++
	r.expenses = append(r.expenses, *exp)
	return nil
}

func (r *inMemoryAccountingRepo) QueryExpenses(_ context.Context, _ domain.Trip) ([]domain.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	copied := make([]domain.Expense, len(r.expenses))
	copy(copied, r.expenses)
	return copied, nil
}

func (r *inMemoryAccountingRepo) QueryExpensesWithFilter(_ context.Context, _ domain.Trip, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	var res []domain.Expense
	for _, e := range r.expenses {
		if filter.Method != nil && e.Method != *filter.Method {
			continue
		}
		if filter.PaidByID != nil && e.PaidByID != *filter.PaidByID {
			continue
		}
		if filter.DateFrom != nil {
			from := time.Date(filter.DateFrom.Year(), filter.DateFrom.Month(), filter.DateFrom.Day(), 0, 0, 0, 0, time.UTC)
			day := time.Date(e.ShoppedAt.Year(), e.ShoppedAt.Month(), e.ShoppedAt.Day(), 0, 0, 0, 0, time.UTC)
			if day.Before(from) {
				continue
			}
		}
		if filter.DateTo != nil {
			to := time.Date(filter.DateTo.Year(), filter.DateTo.Month(), filter.DateTo.Day(), 0, 0, 0, 0, time.UTC)
			day := time.Date(e.ShoppedAt.Year(), e.ShoppedAt.Month(), e.ShoppedAt.Day(), 0, 0, 0, 0, time.UTC)
			if day.After(to) {
				continue
			}
		}
		res = append(res, e)
	}
	if filter.Limit != nil && len(res) > *filter.Limit {
		res = res[:*filter.Limit]
	}
	return res, nil
}

func (r *inMemoryAccountingRepo) UpdateExpense(_ context.Context, _ domain.Trip, exp *domain.Expense) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.expenses {
		if e.ID == exp.ID {
			r.expenses[i] = *exp
			return nil
		}
	}
	return fmt.Errorf("expense %s not found", exp.ID)
}

func (r *inMemoryAccountingRepo) DeleteExpense(_ context.Context, _ domain.Trip, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.expenses {
		if e.ID == id {
			r.expenses = append(r.expenses[:i], r.expenses[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("expense %s not found", id)
}

func (r *inMemoryAccountingRepo) Settlement(_ context.Context, t domain.Trip) (*domain.Settlement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &domain.Settlement{
		Currency: t.Currency,
		Balances: []domain.Balance{
			{UserID: "8", Name: "Alice", Amount: decimal.NewFromInt(100)},
			{UserID: "9", Name: "Bob", Amount: decimal.NewFromInt(-100)},
		},
	}, nil
}

func (r *inMemoryAccountingRepo) Members(_ context.Context, _ domain.Trip) ([]domain.Member, error) {
	return []domain.Member{
		{ID: "8", Name: "Alice"},
		{ID: "9", Name: "Bob"},
	}, nil
}

func (r *inMemoryAccountingRepo) UploadFile(_ context.Context, _ domain.Trip, _ string) (string, error) {
	return "https://example.com/receipt.jpg", nil
}

type inMemoryTripRepo struct {
	mu    sync.Mutex
	trips []domain.Trip
	err   error
}

func (r *inMemoryTripRepo) ListTrips(_ context.Context) ([]domain.Trip, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return r.trips, nil
}

func (r *inMemoryTripRepo) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

type staticRateFetcher struct{}

func (staticRateFetcher) GetRate(_ context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	if source == domain.CurrencyJPY && target == domain.CurrencyTWD {
		return decimal.NewFromFloat(0.22), nil
	}
	if source == domain.CurrencyTWD && target == domain.CurrencyJPY {
		return decimal.NewFromFloat(4.55), nil
	}
	return decimal.NewFromInt(1), nil
}

type WebAppE2ESuite struct {
	suite.Suite

	server    *httptest.Server
	repo      *inMemoryAccountingRepo
	userSvc   *user.Service
	expSvc    *expense.Service
	tripSvc   *trip.Service
	testTrip  domain.Trip
	botToken  string
	createdID string
}

func (s *WebAppE2ESuite) SetupSuite() {
	s.botToken = "integration-bot-token"
	s.testTrip = domain.Trip{ID: 10, Title: "2026 Tokyo Adventure", Currency: domain.CurrencyTWD}

	uRepo := userrepo.NewRepo("12345:8:Alice,67890:9:Bob")
	s.userSvc = user.NewService(uRepo)

	tRepo := &inMemoryTripRepo{trips: []domain.Trip{s.testTrip}}
	s.tripSvc = trip.NewService(tRepo)

	s.repo = newInMemoryAccountingRepo()
	s.expSvc = expense.NewService(s.repo, staticRateFetcher{}, nil)

	handler, err := webapp.NewHandler(s.userSvc, s.expSvc, s.tripSvc, s.botToken, true)
	s.Require().NoError(err)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	s.server = httptest.NewServer(mux)
}

func (s *WebAppE2ESuite) TearDownSuite() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *WebAppE2ESuite) Test01_HealthCheck() {
	resp, err := http.Get(s.server.URL + "/health")
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	s.Assert().Equal("ok", string(body))
}

func (s *WebAppE2ESuite) Test02_Bootstrap() {
	resp, err := http.Get(s.server.URL + "/api/bootstrap")
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusOK, resp.StatusCode)
	var got struct {
		Users []struct {
			Nickname   string `json:"nickname"`
			TelegramID int64  `json:"telegram_id"`
		} `json:"users"`
		Trips []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Currency string `json:"currency"`
		} `json:"trips"`
		Current int64 `json:"current"`
		Members []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"members"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&got))
	s.Assert().Len(got.Trips, 1)
	s.Assert().Equal(int64(10), got.Trips[0].ID)
	s.Assert().Equal(int64(10), got.Current)
	s.Assert().Len(got.Users, 2)
	s.Assert().Len(got.Members, 2)
}

func (s *WebAppE2ESuite) Test03_CreateExpenseE2E() {
	formData := url.Values{
		"trip_id":      {"10"},
		"name":         {"東京拉麵午餐"},
		"price":        {"1200"},
		"currency":     {"JPY"},
		"category":     {"food"},
		"method":       {"cash"},
		"paid_by":      {"12345"},
		"participants": {"8", "9"},
	}

	resp, err := http.PostForm(s.server.URL+"/api/expense", formData)
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	s.Assert().Contains(string(body), "東京拉麵午餐")

	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	s.Require().Len(s.repo.expenses, 1)
	exp := s.repo.expenses[0]
	s.Assert().Equal("東京拉麵午餐", exp.Name)
	s.Assert().Equal(uint64(1200), exp.Price)
	s.Assert().Equal(domain.CurrencyJPY, exp.Currency)
	s.Assert().Equal(domain.PaymentMethodCash, exp.Method)
	s.Assert().Equal("8", exp.PaidByID)

	s.createdID = exp.ID
}

func (s *WebAppE2ESuite) Test04_UpdateExpenseE2E() {
	s.Require().NotEmpty(s.createdID)

	formData := url.Values{
		"trip_id":       {"10"},
		"expense_id":    {s.createdID},
		"name":          {"東京沾麵午餐"},
		"price":         {"1500"},
		"currency":      {"JPY"},
		"exchange_rate": {"0.22"},
		"category":      {"food"},
		"method":        {"credit_card"},
		"paid_by":       {"67890"},
		"participants":  {"9"},
	}
	req, err := http.NewRequest(http.MethodPut, s.server.URL+"/api/expense", strings.NewReader(formData.Encode()))
	s.Require().NoError(err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	s.Assert().Contains(string(body), "已更新")

	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	s.Require().Len(s.repo.expenses, 1, "an update changes the expense and does not add one")
	exp := s.repo.expenses[0]
	s.Assert().Equal(s.createdID, exp.ID)
	s.Assert().Equal("東京沾麵午餐", exp.Name)
	s.Assert().Equal(uint64(1500), exp.Price)
	s.Assert().Equal(domain.PaymentMethodCreditCard, exp.Method)
	s.Assert().Equal("9", exp.PaidByID)
	s.Assert().Equal([]string{"9"}, exp.ParticipantIDs)
}

func (s *WebAppE2ESuite) Test05_DashboardSettlementAndList() {
	get := func(path string) string {
		resp, err := http.Get(s.server.URL + path)
		s.Require().NoError(err)
		defer resp.Body.Close()
		s.Require().Equal(http.StatusOK, resp.StatusCode, path)
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	s.Assert().Contains(get("/partial/dashboard?trip_id=10&range=all"), "餐飲")
	s.Assert().Contains(get("/partial/settlement?trip_id=10"), "Alice")

	list := get("/partial/expenses?trip_id=10&range=all&method=credit_card")
	s.Assert().Contains(list, "東京沾麵午餐")
	s.Assert().Contains(get("/partial/expenses?trip_id=10&range=all&method=cash"), "沒有符合條件的消費")
}

func (s *WebAppE2ESuite) Test06_ExportCSVE2E() {
	resp, err := http.Get(s.server.URL + "/api/export/csv?trip_id=10&range=all")
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusOK, resp.StatusCode)
	s.Assert().Equal("text/csv; charset=utf-8", resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	csvStr := string(body)
	s.Assert().True(strings.HasPrefix(csvStr, "\xef\xbb\xbf"), "should have UTF-8 BOM")
	s.Assert().Contains(csvStr, "日期")
	s.Assert().Contains(csvStr, "品名")
	s.Assert().Contains(csvStr, "東京沾麵午餐")
	s.Assert().Contains(csvStr, "信用卡")
	s.Assert().Contains(csvStr, "Bob")
}

func (s *WebAppE2ESuite) Test07_DeleteExpenseE2E() {
	s.Require().NotEmpty(s.createdID)

	delURL := fmt.Sprintf("%s/api/expense?trip_id=10&id=%s", s.server.URL, s.createdID)
	req, err := http.NewRequest(http.MethodDelete, delURL, nil)
	s.Require().NoError(err)

	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()

	s.Assert().Equal(http.StatusNoContent, resp.StatusCode)

	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	s.Assert().Len(s.repo.expenses, 0)
}

func (s *WebAppE2ESuite) Test08_ServerLifecycle() {
	srv, err := webapp.NewServer(s.userSvc, s.expSvc, s.tripSvc, "0", s.botToken, true)
	s.Require().NoError(err)

	err = srv.Start()
	s.Require().NoError(err)

	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = srv.Shutdown(ctx)
	s.Assert().NoError(err)
}

func TestWebAppE2ESuite(t *testing.T) {
	suite.Run(t, new(WebAppE2ESuite))
}
