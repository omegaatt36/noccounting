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

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

var (
	tokyoTrip = domain.Trip{ID: 3, Title: "2026 Tokyo", Currency: domain.CurrencyTWD}
	osakaTrip = domain.Trip{ID: 9, Title: "Osaka", Currency: domain.CurrencyJPY}
)

type twoTrips struct{}

func (twoTrips) ListTrips(context.Context) ([]domain.Trip, error) {
	return []domain.Trip{tokyoTrip, osakaTrip}, nil
}

type stubRates struct{}

func (stubRates) GetRate(_ context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	if source == domain.CurrencyJPY {
		return decimal.RequireFromString("0.215"), nil
	}
	return decimal.RequireFromString("4.65"), nil
}

func devHandler(t *testing.T, repo *stubAccountingRepo, trips trip.Repository) (*Handler, *stubAccountingRepo) {
	t.Helper()

	users := &fakeUserRepo{users: map[int64]*domain.User{
		123456789: {ID: 1, TelegramID: 123456789, BackendUserID: "8", Nickname: "John"},
	}}

	handler, err := NewHandler(user.NewService(users), expense.NewService(repo, stubRates{}, nil), trip.NewService(trips), "test-token", true)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}
	return handler, repo
}

func get(t *testing.T, serve http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	serve(w, httptest.NewRequest("GET", target, nil))
	return w
}

func post(t *testing.T, serve http.HandlerFunc, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	serve(w, req)
	return w
}

func expenseForm(overrides map[string]string) url.Values {
	form := url.Values{
		"trip_id": {"3"}, "name": {"Lunch"}, "price": {"100"}, "currency": {"TWD"}, "category": {"food"}, "method": {"cash"},
	}
	for key, value := range overrides {
		form.Set(key, value)
	}
	return form
}

func TestHandleGetTrips_OffersEveryTripAndTheCurrentOne(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})

	w := get(t, handler.handleGetTrips, "/api/trips")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got TripsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(got.Trips) != 2 || got.Trips[1].Currency != "JPY" || got.Trips[1].Title != "Osaka" {
		t.Errorf("trips = %+v, want both with their currency", got.Trips)
	}
	if got.Current != tokyoTrip.ID {
		t.Errorf("current = %d, want the default, the first of two undated trips", got.Current)
	}
}

func TestHandleGetTrips_NoTripsIsAnEmptyListNotAnError(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, noTrips{})

	w := get(t, handler.handleGetTrips, "/api/trips")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got TripsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got.Current != 0 || len(got.Trips) != 0 {
		t.Errorf("response = %+v, want no trips and no current", got)
	}
}

type noTrips struct{}

func (noTrips) ListTrips(context.Context) ([]domain.Trip, error) { return nil, nil }

func TestHandleGetTrips_RequiresAuth(t *testing.T) {
	users := &fakeUserRepo{users: map[int64]*domain.User{}}
	handler, err := NewHandler(user.NewService(users), expense.NewService(&stubAccountingRepo{}, nil, nil), trip.NewService(twoTrips{}), "test-token", false)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	w := get(t, handler.handleGetTrips, "/api/trips")

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 without init_data", w.Code)
	}
}

// The Mini App and the bot share one choice per person, so switching in either
// moves the other.
func TestHandleSelectTrip_MovesWhereTheNextExpenseIsFiled(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, ledger := devHandler(t, repo, twoTrips{})

	if w := post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"9"}}); w.Code != http.StatusOK {
		t.Fatalf("select status = %d, want 200", w.Code)
	}
	if w := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{"trip_id": "9"})); w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200", w.Code)
	}

	if len(ledger.trips) != 1 || ledger.trips[0].ID != osakaTrip.ID {
		t.Errorf("books opened for %+v, want the trip just chosen", ledger.trips)
	}
}

func TestHandleSelectTrip_RefusesATripThatIsGone(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})

	if w := post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"404"}}); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if w := post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"x"}}); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a trip id that is not a number", w.Code)
	}
}

// A page left open keeps filing under the trip it was opened for, even after the
// person switched to another meanwhile.
func TestHandleCreateExpense_FilesUnderTheTripThePageWasOpenedFor(t *testing.T) {
	handler, ledger := devHandler(t, &stubAccountingRepo{}, twoTrips{})
	post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"9"}})

	if w := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{"trip_id": "3"})); w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200", w.Code)
	}

	if len(ledger.trips) != 1 || ledger.trips[0].ID != tokyoTrip.ID {
		t.Errorf("books opened for %+v, want the trip named in the form", ledger.trips)
	}
}

func TestHandleCreateExpense_RefusesATripThatIsGone(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{"trip_id": "404"}))

	if !strings.Contains(w.Body.String(), "找不到這趟旅行") {
		t.Errorf("body = %q, want the missing trip reported", w.Body.String())
	}
	if len(repo.created) != 0 {
		t.Error("an expense was filed under a trip that does not exist")
	}
}

func TestHandleCreateExpense_SharesWithTheParticipantsTicked(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, _ := devHandler(t, repo, twoTrips{})

	form := expenseForm(nil)
	form["participants"] = []string{"8", " 3 ", ""}
	post(t, handler.handleCreateExpense, "/api/expense", form)

	if len(repo.created) != 1 {
		t.Fatalf("created %d expenses, want 1", len(repo.created))
	}
	got := repo.created[0].ParticipantIDs
	if len(got) != 2 || got[0] != "8" || got[1] != "3" {
		t.Errorf("ParticipantIDs = %v, want the ids ticked, trimmed and without blanks", got)
	}
}

func TestHandleCreateExpense_DefaultsToAllMembers(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, _ := devHandler(t, repo, twoTrips{})

	post(t, handler.handleCreateExpense, "/api/expense", expenseForm(nil))

	if len(repo.created) != 1 || len(repo.created[0].ParticipantIDs) != 1 || repo.created[0].ParticipantIDs[0] != "8" {
		t.Errorf("created = %+v, want all members as participants", repo.created)
	}
}

// Only an expense in another currency than the trip's carries a rate; the form
// posts the one it last showed even after the currency is switched back.
func TestHandleCreateExpense_OnlyAForeignExpenseCarriesTheRate(t *testing.T) {
	tests := []struct {
		name     string
		tripID   string
		currency string
		wantRate bool
	}{
		{"JPY on a TWD trip", "3", "JPY", true},
		{"TWD on a TWD trip", "3", "TWD", false},
		{"TWD on a JPY trip", "9", "TWD", true},
		{"JPY on a JPY trip", "9", "JPY", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubAccountingRepo{}
			handler, _ := devHandler(t, repo, twoTrips{})

			post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{
				"trip_id": tt.tripID, "currency": tt.currency, "exchange_rate": "4.65",
			}))

			if len(repo.created) != 1 {
				t.Fatalf("created %d expenses, want 1", len(repo.created))
			}
			if got := !repo.created[0].ExchangeRate.IsZero(); got != tt.wantRate {
				t.Errorf("carries a rate = %v, want %v", got, tt.wantRate)
			}
		})
	}
}

func TestHandleCreateExpense_SaysWhenSomeoneIsNotOnTheTrip(t *testing.T) {
	repo := &stubAccountingRepo{createErr: domain.ErrNotOnTrip}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(nil))

	if !strings.Contains(w.Body.String(), "不在這趟旅行裡") {
		t.Errorf("body = %q, want the person told who to fix", w.Body.String())
	}
}

func TestHandleCreateExpense_AcceptsDecimalPriceAndRoundsToWholeUnits(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := post(t, handler.handleCreateExpense, "/api/expense", expenseForm(map[string]string{
		"price": "120.6",
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d expenses, want 1", len(repo.created))
	}
	if repo.created[0].Price != 121 {
		t.Errorf("Price = %d, want 121 (rounded from 120.6)", repo.created[0].Price)
	}
}

func TestHandleGetMembers_UsesNicknamesForPeopleInTheMapping(t *testing.T) {
	repo := &stubAccountingRepo{members: []domain.Member{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "bob"}}}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := get(t, handler.handleGetMembers, "/api/members?trip_id=3")

	var got MembersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	// User 8 is in the mapping as John; user 3 is not and keeps the name TREK has.
	want := []MemberInfo{{ID: "3", Name: "owner-trek"}, {ID: "8", Name: "John"}}
	if len(got.Members) != 2 || got.Members[0] != want[0] || got.Members[1] != want[1] {
		t.Errorf("members = %+v, want %+v", got.Members, want)
	}
}

func TestHandleGetRates_QuotesBothDirections(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})

	w := get(t, handler.handleGetRates, "/api/rates")

	var got RatesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	seen := map[string]string{}
	for _, quote := range got.Quotes {
		seen[quote.From+">"+quote.To] = quote.Rate
	}
	if seen["JPY>TWD"] != "0.215" || seen["TWD>JPY"] != "4.65" {
		t.Errorf("quotes = %+v, want JPY to TWD and TWD to JPY", got.Quotes)
	}
}

func TestHandleDashboard_ShowsTheSettlementAndSpendingByPaymentMethod(t *testing.T) {
	repo := &stubAccountingRepo{
		expenses: []domain.Expense{
			{ID: "1", Name: "拉麵", Price: 1200, Currency: domain.CurrencyTWD, Category: domain.CategoryFood, Method: domain.PaymentMethodCash, PaidByID: "8", ShoppedAt: time.Now()},
			{ID: "2", Name: "車票", Price: 3000, Currency: domain.CurrencyTWD, Category: domain.CategoryFlights, Method: domain.PaymentMethodCreditCard, PaidByID: "8", ShoppedAt: time.Now()},
		},
		settlement: &domain.Settlement{
			Currency: domain.CurrencyTWD,
			Balances: []domain.Balance{{UserID: "8", Name: "bob", Amount: decimal.NewFromInt(-1500)}},
			Transfers: []domain.Transfer{
				{FromID: "8", FromName: "bob", ToID: "3", ToName: "owner-trek", Amount: decimal.NewFromInt(1500)},
			},
			Unconverted: 1,
		},
	}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := get(t, handler.handleDashboardContent, "/partial/dashboard?range=all&trip_id=3")

	body := w.Body.String()
	for _, want := range []string{
		"2026 Tokyo",    // the trip it is about
		"NT$4,200",      // the total, in the trip's currency
		"機票",            // a category by its label, not its slug
		"付款方式明細", "信用卡", // spending by payment method
		"/partial/dashboard/method?name=credit_card", // drillable
		"💸 結算", "John → owner-trek", "NT$1,500", // the settlement, under the mapped nickname
		"有 1 筆因為缺少匯率沒有計入",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}
}

func TestHandleDashboard_ReportsInAJPYTripsCurrency(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{
		{ID: "1", Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, Category: domain.CategoryFood, Method: domain.PaymentMethodCash, ShoppedAt: time.Now()},
	}}
	handler, _ := devHandler(t, repo, twoTrips{})

	w := get(t, handler.handleDashboardContent, "/partial/dashboard?range=all&trip_id=9")

	if body := w.Body.String(); !strings.Contains(body, "¥1,000") {
		t.Errorf("dashboard should total in yen, got %q", body)
	}
}

func TestHandleMethodDetail_ListsOnlyWhatWasPaidThatWay(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{
		{ID: "1", Name: "現金拉麵", Price: 1200, Currency: domain.CurrencyTWD, Method: domain.PaymentMethodCash, ShoppedAt: time.Now()},
		{ID: "2", Name: "刷卡車票", Price: 3000, Currency: domain.CurrencyTWD, Method: domain.PaymentMethodCreditCard, ShoppedAt: time.Now()},
		{ID: "3", Name: "沒標的", Price: 50, Currency: domain.CurrencyTWD, ShoppedAt: time.Now()},
	}}
	handler, _ := devHandler(t, repo, twoTrips{})

	card := get(t, handler.handleMethodDetail, "/partial/dashboard/method?name=credit_card&range=all&trip_id=3").Body.String()
	if !strings.Contains(card, "刷卡車票") || strings.Contains(card, "現金拉麵") || strings.Contains(card, "沒標的") {
		t.Errorf("credit_card detail = %q, want only the card expense", card)
	}

	unlabeled := get(t, handler.handleMethodDetail, "/partial/dashboard/method?name=&range=all&trip_id=3").Body.String()
	if !strings.Contains(unlabeled, "沒標的") || strings.Contains(unlabeled, "刷卡車票") {
		t.Errorf("unlabeled detail = %q, want only the expense that records no method", unlabeled)
	}

	if w := get(t, handler.handleMethodDetail, "/partial/dashboard/method?name=bitcoin"); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a method that does not exist", w.Code)
	}
}

func TestHandleExportCSV_WritesLabelsAndTheTripsCurrency(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{
		{
			ID: "1", Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ExchangeRate: decimal.RequireFromString("0.22"),
			Category: domain.CategoryFlights, Method: domain.PaymentMethodCreditCard, PaidByID: "8", ShoppedAt: time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC),
		},
	}}
	handler, _ := devHandler(t, repo, twoTrips{})

	body := get(t, handler.handleExportCSV, "/api/export/csv?range=all&trip_id=3").Body.String()

	for _, want := range []string{"金額（TWD）", "機票", "信用卡", "John", "220"} {
		if !strings.Contains(body, want) {
			t.Errorf("CSV does not contain %q: %q", want, body)
		}
	}
}

func TestHandleCreateExpense_RequiresExplicitTrip(t *testing.T) {
	repo := &stubAccountingRepo{}
	handler, _ := devHandler(t, repo, twoTrips{})
	post(t, handler.handleSelectTrip, "/api/trip", url.Values{"trip_id": {"9"}})
	form := expenseForm(nil)
	form.Del("trip_id")
	w := post(t, handler.handleCreateExpense, "/api/expense", form)
	if !strings.Contains(w.Body.String(), "找不到這趟旅行") {
		t.Errorf("missing trip was not rejected: %s", w.Body.String())
	}
	if len(repo.created) != 0 {
		t.Fatal("expense was written without an explicit trip")
	}
}

func TestTripScopedReads_RequireExplicitTrip(t *testing.T) {
	handler, _ := devHandler(t, &stubAccountingRepo{}, twoTrips{})
	tests := []struct {
		path  string
		serve http.HandlerFunc
	}{
		{"/api/members", handler.handleGetMembers},
		{"/partial/dashboard", handler.handleDashboardContent},
		{"/partial/dashboard/category?name=food", handler.handleCategoryDetail},
		{"/partial/dashboard/method?name=cash", handler.handleMethodDetail},
		{"/api/export/csv", handler.handleExportCSV},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if w := get(t, tt.serve, tt.path); w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want missing-trip rejection", w.Code)
			}
		})
	}
}

func TestHandleDeleteExpense(t *testing.T) {
	repo := &stubAccountingRepo{expenses: []domain.Expense{
		{ID: "exp-123", Name: "ramen", Price: 1000, Currency: domain.CurrencyJPY, ShoppedAt: time.Now()},
	}}
	handler, _ := devHandler(t, repo, twoTrips{})

	req := httptest.NewRequest(http.MethodDelete, "/api/expense?id=exp-123&trip_id=3", nil)
	w := httptest.NewRecorder()
	handler.handleDeleteExpense(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "exp-123" {
		t.Errorf("deleted = %v, want exp-123", repo.deleted)
	}
	if w.Header().Get("HX-Trigger") != "dashboard-refresh" {
		t.Errorf("HX-Trigger = %q, want dashboard-refresh", w.Header().Get("HX-Trigger"))
	}
}

type stubAnalyzer struct {
	result *domain.ReceiptAnalysis
	err    error
}

func (s stubAnalyzer) Analyze(_ context.Context, _ []byte) (*domain.ReceiptAnalysis, error) {
	return s.result, s.err
}

func TestHandleAnalyzeReceipt(t *testing.T) {
	users := &fakeUserRepo{users: map[int64]*domain.User{
		123456789: {ID: 1, TelegramID: 123456789, BackendUserID: "8", Nickname: "John"},
	}}
	analysis := &domain.ReceiptAnalysis{
		Summary:       "Matsuya",
		Total:         650,
		Currency:      domain.CurrencyJPY,
		Category:      domain.CategoryFood,
		PaymentMethod: domain.PaymentMethodCash,
	}
	expSvc := expense.NewService(&stubAccountingRepo{}, stubRates{}, stubAnalyzer{result: analysis})
	handler, err := NewHandler(user.NewService(users), expSvc, trip.NewService(twoTrips{}), "test-token", true)
	if err != nil {
		t.Fatal(err)
	}

	body := new(strings.Builder)
	body.WriteString("--boundary\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"receipt\"; filename=\"receipt.jpg\"\r\n")
	body.WriteString("Content-Type: image/jpeg\r\n\r\n")
	body.WriteString("fake-image-bytes\r\n")
	body.WriteString("--boundary--\r\n")

	payload := strings.ReplaceAll(body.String(), "\\r\\n", "\r\n")
	req := httptest.NewRequest(http.MethodPost, "/api/receipt/analyze", strings.NewReader(payload))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	w := httptest.NewRecorder()
	handler.handleAnalyzeReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp analyzeReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Summary != "Matsuya" || resp.Total != 650 || resp.Currency != domain.CurrencyJPY {
		t.Errorf("unexpected response: %+v", resp)
	}
}
