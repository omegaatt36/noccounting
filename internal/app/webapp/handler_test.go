package webapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type stubAccountingRepo struct {
	createErr  error
	trips      []domain.Trip
	expenses   []domain.Expense
	members    []domain.Member
	created    []*domain.Expense
	settlement *domain.Settlement
}

func (m *stubAccountingRepo) CreateExpense(_ context.Context, selected domain.Trip, expense *domain.Expense) error {
	if m.createErr == nil {
		m.trips = append(m.trips, selected)
		m.created = append(m.created, expense)
	}
	return m.createErr
}

func (m *stubAccountingRepo) QueryExpenses(_ context.Context, _ domain.Trip) ([]domain.Expense, error) {
	return m.expenses, nil
}

func (m *stubAccountingRepo) QueryExpensesWithFilter(_ context.Context, _ domain.Trip, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	var kept []domain.Expense
	for _, exp := range m.expenses {
		if filter.Method == nil || exp.Method == *filter.Method {
			kept = append(kept, exp)
		}
	}
	return kept, nil
}

func (m *stubAccountingRepo) UpdateExpense(_ context.Context, _ domain.Trip, _ *domain.Expense) error {
	return nil
}

func (m *stubAccountingRepo) DeleteExpense(_ context.Context, _ domain.Trip, _ string) error {
	return nil
}

func (m *stubAccountingRepo) Settlement(_ context.Context, _ domain.Trip) (*domain.Settlement, error) {
	if m.settlement != nil {
		return m.settlement, nil
	}
	return &domain.Settlement{Currency: domain.CurrencyTWD}, nil
}

func (m *stubAccountingRepo) Members(_ context.Context, _ domain.Trip) ([]domain.Member, error) {
	if m.members == nil {
		return []domain.Member{{ID: "8"}}, nil
	}
	return m.members, nil
}

func (m *stubAccountingRepo) UploadFile(_ context.Context, _ domain.Trip, _ string) (string, error) {
	return "file-id", nil
}

type fakeUserRepo struct {
	users map[int64]*domain.User
	err   error
}

func (m *fakeUserRepo) GetUser(req domain.GetUserRequest) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	if req.TelegramID != nil {
		if u, ok := m.users[*req.TelegramID]; ok {
			return u, nil
		}
		return nil, domain.ErrUserNotFound
	}
	return nil, domain.ErrUserNotFound
}

func (m *fakeUserRepo) GetUsers() ([]domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	var users []domain.User
	for _, u := range m.users {
		users = append(users, *u)
	}
	return users, nil
}

// testTrip is the one trip the Mini App tests file under.
var testTrip = domain.Trip{ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyTWD}

type singleTrip struct{}

func (singleTrip) ListTrips(context.Context) ([]domain.Trip, error) {
	return []domain.Trip{testTrip}, nil
}

func newTestTrips() *trip.Service { return trip.NewService(singleTrip{}) }

func TestHandleHealth(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "ok" {
		t.Errorf("expected body 'ok', got %q", body)
	}
}

func TestHandleIndex(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty HTML response")
	}

	if !strings.Contains(body, "<!") && !strings.Contains(body, "<html") && !strings.Contains(body, "<") {
		t.Errorf("expected HTML content, got: %s", body[:100])
	}
}

func TestHandleAuthMissingInitData(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/auth", nil)
	w := httptest.NewRecorder()

	handler.handleAuth(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var authResp AuthResponse
	err = json.NewDecoder(w.Body).Decode(&authResp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if authResp.Authorized {
		t.Errorf("expected Authorized to be false")
	}

	if authResp.Error != "missing init_data" {
		t.Errorf("expected error 'missing init_data', got %q", authResp.Error)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}
}

func TestHandleAuthInvalidInitData(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/auth?init_data=invalid_data", nil)
	w := httptest.NewRecorder()

	handler.handleAuth(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var authResp AuthResponse
	err = json.NewDecoder(w.Body).Decode(&authResp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if authResp.Authorized {
		t.Errorf("expected Authorized to be false")
	}

	if authResp.Error != "invalid authentication" {
		t.Errorf("expected error 'invalid authentication', got %q", authResp.Error)
	}
}

func TestHandleAuthUnauthorizedUser(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	botToken := "test-token"
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	req := httptest.NewRequest("GET", "/api/auth?init_data="+url.QueryEscape(initData), nil)
	w := httptest.NewRecorder()

	handler.handleAuth(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var authResp AuthResponse
	err = json.NewDecoder(w.Body).Decode(&authResp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if authResp.Authorized {
		t.Errorf("expected Authorized to be false")
	}

	if authResp.Error != "unauthorized user" {
		t.Errorf("expected error 'unauthorized user', got %q", authResp.Error)
	}
}

func TestHandleAuthSuccess(t *testing.T) {
	botToken := "test-token"
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	req := httptest.NewRequest("GET", "/api/auth?init_data="+url.QueryEscape(initData), nil)
	w := httptest.NewRecorder()

	handler.handleAuth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var authResp AuthResponse
	err = json.NewDecoder(w.Body).Decode(&authResp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !authResp.Authorized {
		t.Errorf("expected Authorized to be true")
	}

	if authResp.Nickname != "John Doe" {
		t.Errorf("expected nickname 'John Doe', got %q", authResp.Nickname)
	}

	if authResp.Error != "" {
		t.Errorf("expected no error, got %q", authResp.Error)
	}
}

func TestHandleCreateExpenseMissingInitData(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// POST without init_data
	data := url.Values{"trip_id": {"3"}}
	data.Set("name", "Lunch")
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body")
	}
}

func TestHandleCreateExpenseInvalidInitData(t *testing.T) {
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// POST with invalid init_data
	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", "invalid_data")
	data.Set("name", "Lunch")
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

func TestHandleCreateExpenseUnauthorizedUser(t *testing.T) {
	botToken := "test-token"
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data for unauthorized user
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":999999999,"is_bot":false,"first_name":"Unknown"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	data.Set("name", "Lunch")
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

func TestHandleCreateExpenseMissingName(t *testing.T) {
	botToken := "test-token"
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	// Missing name
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

func TestHandleCreateExpenseMissingPrice(t *testing.T) {
	botToken := "test-token"
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	data.Set("name", "Lunch")
	// Missing price
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

func TestHandleCreateExpenseInvalidPrice(t *testing.T) {
	botToken := "test-token"
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	data.Set("name", "Lunch")
	data.Set("price", "invalid")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML response, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

func TestHandleCreateExpenseWithPaidBy(t *testing.T) {
	botToken := "test-token"
	telegramID1 := int64(123456789)
	telegramID2 := int64(987654321)

	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID1: {
				ID:            1,
				TelegramID:    telegramID1,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
			telegramID2: {
				ID:            2,
				TelegramID:    telegramID2,
				BackendUserID: "456",
				Nickname:      "Jane Smith",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data for user 1
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	data.Set("name", "Lunch")
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")
	data.Set("paid_by", "987654321") // User 2 paid

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body")
	}
}

func TestHandleCreateExpenseRepositoryError(t *testing.T) {
	botToken := "test-token"
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{createErr: context.DeadlineExceeded}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), botToken, false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create valid init_data
	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}
	initData := buildValidTelegramInitData(botToken, params)

	data := url.Values{"trip_id": {"3"}}
	data.Set("init_data", initData)
	data.Set("name", "Lunch")
	data.Set("price", "100")
	data.Set("currency", "TWD")
	data.Set("category", "food")
	data.Set("method", "cash")

	req := httptest.NewRequest("POST", "/api/expense", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.handleCreateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Errorf("expected non-empty response body with error")
	}
}

// TestRegisterRoutes verifies that all routes are registered correctly.
func TestRegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mockRepo := &stubAccountingRepo{}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	handler.RegisterRoutes(mux)

	routes := []string{
		"GET /",
		"GET /api/auth",
		"GET /api/users",
		"POST /api/expense",
		"GET /health",
		"GET /partial/form",
		"GET /partial/dashboard",
		"GET /api/export/csv",
	}

	for _, route := range routes {
		parts := strings.Split(route, " ")
		method, path := parts[0], parts[1]

		// Verify route exists by making a test request
		req := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()

		// The router will call the handler if the route exists
		mux.ServeHTTP(w, req)

		// We just verify the handler doesn't return 404 (method not allowed)
		// Some handlers may return 400 or other codes due to validation
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s should be registered", method, path)
		}
	}
}

func TestHandleExportCSVDevMode(t *testing.T) {
	telegramID := int64(123456789)
	mockRepo := &stubAccountingRepo{
		expenses: []domain.Expense{
			{
				ID:        "expense-1",
				Name:      "Lunch",
				Price:     100,
				Currency:  domain.CurrencyTWD,
				Category:  domain.CategoryFood,
				Method:    domain.PaymentMethodCash,
				PaidByID:  "123",
				ShoppedAt: time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	fakeUserRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			telegramID: {
				ID:            1,
				TelegramID:    telegramID,
				BackendUserID: "123",
				Nickname:      "John Doe",
			},
		},
	}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", true)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/export/csv?range=all&trip_id=3", nil)
	w := httptest.NewRecorder()

	handler.handleExportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/csv; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/csv; charset=utf-8', got %q", contentType)
	}

	disposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") {
		t.Errorf("expected Content-Disposition to contain 'attachment', got %q", disposition)
	}

	body := w.Body.String()
	// Check for BOM
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Errorf("expected CSV to start with BOM")
	}

	// Check for header row
	if !strings.Contains(body, "日期") {
		t.Errorf("expected CSV header to contain '日期'")
	}

	// Check for data
	if !strings.Contains(body, "Lunch") {
		t.Errorf("expected CSV data to contain 'Lunch'")
	}
}

func TestHandleExportCSVEmpty(t *testing.T) {
	mockRepo := &stubAccountingRepo{
		expenses: []domain.Expense{},
	}
	fakeUserRepo := &fakeUserRepo{users: make(map[int64]*domain.User)}
	userService := user.NewService(fakeUserRepo)
	expenseService := expense.NewService(mockRepo, nil, nil)
	handler, err := NewHandler(userService, expenseService, newTestTrips(), "test-token", true)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/export/csv?range=all&trip_id=3", nil)
	w := httptest.NewRecorder()

	handler.handleExportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/csv; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/csv; charset=utf-8', got %q", contentType)
	}

	body := w.Body.String()
	// Check for BOM
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Errorf("expected CSV to start with BOM")
	}

	// Check for header row even with no data
	if !strings.Contains(body, "日期") {
		t.Errorf("expected CSV to contain header row even when empty")
	}
}
