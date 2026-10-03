package trek_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/trek"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

const (
	createTripID = 3
	createItemID = 42

	createRoster = `{
	  "owner": {"id":3,"username":"owner-trek","email":"owner@example.com","role":"owner","is_guest":false},
	  "members": [{"id":8,"username":"bob","email":"bob@example.com","role":"member","is_guest":false}],
	  "current_user_id":7
	}`

	createCreated = `{"item":{"id":42,"name":"ramen","total_price":1200,"currency":"TWD",
	  "exchange_rate":1,"expense_date":"2026-05-01","note":"PAYMENT:cash","payers":[{"user_id":8,"amount":1200}]}}`

	createCreatedLinked = `{"item":{"id":42,"name":"ramen","total_price":1200,"currency":"TWD",
	  "exchange_rate":1,"expense_date":"2026-05-01","note":"PAYMENT:cash","payers":[{"user_id":8,"amount":1200}],
	  "receipts":[` + uploadStored + `]}}`

	uploadStored = `{"id":77,"filename":"3f9c1a.jpg","original_name":"receipt-4242.jpg",
	  "file_size":2048,"mime_type":"image/jpeg","url":"/api/trips/3/files/77/download"}`
)

type createCall struct {
	method string
	path   string
	query  string
	body   string
}

type createStub struct {
	server *httptest.Server

	mu          sync.Mutex
	calls       []createCall
	roster      string
	reply       int
	replyBody   string
	storedTotal *float64
	listing     string
	readStatus  int
	settlement  string

	updateStatus int
	updateBody   string
	deleteStatus int
	deleteBody   string
	row          *itemRow
	deleted      bool
	rows         map[int64]*itemRow
	written      bool

	fileStatus int
	fileBody   string
	fileOnCall map[int]stubAnswer
	uploads    []uploadSeen
}

func newCreateStub(t *testing.T) *createStub {
	t.Helper()

	stub := &createStub{
		roster:       createRoster,
		reply:        http.StatusCreated,
		replyBody:    createCreated,
		listing:      budgetListing(),
		readStatus:   http.StatusOK,
		updateStatus: http.StatusOK,
		updateBody:   `{"item":{"id":42,"total_price":1200}}`,
		fileStatus:   http.StatusCreated,
		fileBody:     `{"file":` + uploadStored + `}`,
		fileOnCall:   map[int]stubAnswer{},
		deleteStatus: http.StatusOK,
		deleteBody:   `{"success":true}`,
	}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *createStub) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && isFilesCollection(r.URL.Path) {
		s.handleUpload(w, r)
		return
	}

	payload, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, createCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(payload)})

	switch {
	case r.URL.Path == "/api/auth/login":
		writeJSON(w, http.StatusOK, `{"token":"token-1","user":{"id":7}}`)
	case r.URL.Path == "/api/trips":
		writeJSON(w, http.StatusOK, `{"trips":[{"id":3,"title":"2026 Tokyo","currency":"TWD"}]}`)
	case r.URL.Path == "/api/trips/3/members":
		writeJSON(w, http.StatusOK, s.roster)
	case r.URL.Path == "/api/trips/3/budget/settlement" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.settlement)
	case r.URL.Path == "/api/trips/3/budget" && r.Method == http.MethodPost:
		writeJSON(w, s.reply, s.createdLocked(payload))
	case r.URL.Path == "/api/trips/3/budget" && r.Method == http.MethodGet:
		writeJSON(w, s.readStatus, s.listingLocked())
	case strings.HasPrefix(r.URL.Path, "/api/trips/3/budget/"):
		s.handleItem(w, r, payload)
	default:
		writeJSON(w, http.StatusNotFound, `{"error":"Not found"}`)
	}
}

func (s *createStub) createdLocked(payload []byte) string {
	stored := s.storedTotal
	if stored == nil {
		var sent struct {
			TotalPrice *float64 `json:"total_price"`
		}
		if err := json.Unmarshal(payload, &sent); err != nil || sent.TotalPrice == nil {
			return s.replyBody
		}
		stored = sent.TotalPrice
	}

	var answer struct {
		Item map[string]any `json:"item"`
	}
	if err := json.Unmarshal([]byte(s.replyBody), &answer); err != nil || answer.Item == nil {
		return s.replyBody
	}
	answer.Item["total_price"] = *stored

	encoded, err := json.Marshal(answer)
	if err != nil {
		return s.replyBody
	}
	return string(encoded)
}

func (s *createStub) handleItem(w http.ResponseWriter, r *http.Request, payload []byte) {
	itemID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/trips/3/budget/"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, `{"error":"Budget item not found"}`)
		return
	}
	row, exists := s.rowsLocked()[itemID]

	switch {
	case r.Method == http.MethodPut && exists:
		var fields map[string]any
		if err := json.Unmarshal(payload, &fields); err != nil {
			writeJSON(w, http.StatusBadRequest, `{"error":"the stub could not read that body"}`)
			return
		}
		applyTREKUpdate(row, fields)
		s.row, s.written = row, true
		writeJSON(w, s.updateStatus, s.updateBody)
	case r.Method == http.MethodDelete && exists:
		delete(s.rowsLocked(), itemID)
		s.deleted, s.written = true, true
		writeJSON(w, s.deleteStatus, s.deleteBody)
	default:
		writeJSON(w, http.StatusNotFound, `{"error":"Budget item not found"}`)
	}
}

func (s *createStub) rowsLocked() map[int64]*itemRow {
	if s.rows != nil {
		return s.rows
	}

	var listing struct {
		Items []*itemRow `json:"items"`
	}
	if err := json.Unmarshal([]byte(s.listing), &listing); err != nil {
		listing.Items = nil
	}
	s.rows = make(map[int64]*itemRow, len(listing.Items))
	for _, row := range listing.Items {
		s.rows[row.ID] = row
	}
	return s.rows
}

func (s *createStub) listingLocked() string {
	rows := s.rowsLocked()
	if !s.written {
		return s.listing
	}

	items := make([]string, 0, len(rows))
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		items = append(items, string(encoded))
	}
	slices.Sort(items)
	return budgetListing(items...)
}

func (s *createStub) callsSeen() []createCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]createCall(nil), s.calls...)
}

func (s *createStub) creates() []createCall {
	var creates []createCall
	for _, call := range s.callsSeen() {
		if call.method == http.MethodPost && call.path == "/api/trips/3/budget" {
			creates = append(creates, call)
		}
	}
	return creates
}

func (s *createStub) budgetReads() []createCall {
	var reads []createCall
	for _, call := range s.callsSeen() {
		if call.method == http.MethodGet && call.path == "/api/trips/3/budget" {
			reads = append(reads, call)
		}
	}
	return reads
}

func (s *createStub) paths() []string {
	seen := s.callsSeen()
	paths := make([]string, 0, len(seen))
	for _, call := range seen {
		paths = append(paths, call.path)
	}
	return paths
}

func (s *createStub) updates() []createCall {
	return s.itemWrites(http.MethodPut)
}

func (s *createStub) deletes() []createCall {
	return s.itemWrites(http.MethodDelete)
}

func (s *createStub) itemWrites(method string) []createCall {
	var writes []createCall
	for _, call := range s.callsSeen() {
		if call.method == method && strings.HasPrefix(call.path, "/api/trips/3/budget/") {
			writes = append(writes, call)
		}
	}
	return writes
}

func (s *createStub) rowSeen(t *testing.T, id int64) itemRow {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.rowsLocked()[id]
	if !ok {
		t.Fatalf("item %d is not on the stub's budget: %v", id, s.paths())
	}
	return *row
}

func (s *createStub) wasDeleted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleted
}

func (s *createStub) startedClient(t *testing.T) *trek.Client {
	t.Helper()

	client := trek.NewClientWithBaseURL(createConfig(), s.server.URL)
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return client
}

func (s *createStub) repo(t *testing.T, base domain.Currency, rates rateQuoter) *tripRepoFixture {
	t.Helper()

	client := s.startedClient(t)
	return &tripRepoFixture{repo: trek.NewRepo(client, rates), trip: domain.Trip{ID: createTripID, Currency: base}}
}

func createConfig() trek.Config {
	return trek.Config{
		BaseURL:  "https://trek.example.com",
		Email:    "bot@example.com",
		Password: "s3cret",
	}
}

type rateQuoter interface {
	GetRate(ctx context.Context, source, target domain.Currency) (decimal.Decimal, error)
}

type quotedRate struct {
	rate        decimal.Decimal
	err         error
	asked       []domain.Currency
	askedTarget []domain.Currency
}

func (q *quotedRate) GetRate(_ context.Context, source, target domain.Currency) (decimal.Decimal, error) {
	q.asked = append(q.asked, source)
	q.askedTarget = append(q.askedTarget, target)
	if q.err != nil {
		return decimal.Zero, q.err
	}
	return q.rate, nil
}

func jpyQuote() *quotedRate { return &quotedRate{rate: decimal.NewFromFloat(0.22)} }

func ramen() *domain.Expense {
	return &domain.Expense{
		Name:           "拉麵",
		Price:          1200,
		Currency:       domain.CurrencyTWD,
		Category:       domain.CategoryFood,
		Method:         domain.PaymentMethodCash,
		PaidByID:       "8",
		ShoppedAt:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ParticipantIDs: []string{"8"},
	}
}

func body(t *testing.T, call createCall) map[string]any {
	t.Helper()

	var fields map[string]any
	if err := json.Unmarshal([]byte(call.body), &fields); err != nil {
		t.Fatalf("request body is not a JSON object: %v (%s)", err, call.body)
	}
	return fields
}

func oneCreate(t *testing.T, stub *createStub) createCall {
	t.Helper()

	creates := stub.creates()
	if len(creates) != 1 {
		t.Fatalf("budget creates = %d, want exactly 1 (%v)", len(creates), stub.paths())
	}
	return creates[0]
}

func oneUpdate(t *testing.T, stub *createStub) createCall {
	t.Helper()

	updates := stub.updates()
	if len(updates) != 1 {
		t.Fatalf("budget updates = %d, want exactly 1 (%v)", len(updates), stub.paths())
	}
	return updates[0]
}

func oneDelete(t *testing.T, stub *createStub) createCall {
	t.Helper()

	deletes := stub.deletes()
	if len(deletes) != 1 {
		t.Fatalf("budget deletes = %d, want exactly 1 (%v)", len(deletes), stub.paths())
	}
	return deletes[0]
}

func wantBody(t *testing.T, got, want map[string]any) {
	t.Helper()

	want = asDecoded(t, want)
	if len(got) != len(want) {
		t.Errorf("request body carries %d fields %v, want %d %v", len(got), keysOf(got), len(want), keysOf(want))
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Errorf("request body has no %q field", key)
			continue
		}
		if !sameValue(gotValue, wantValue) {
			t.Errorf("request body %q = %#v, want %#v", key, gotValue, wantValue)
		}
	}
}

func asDecoded(t *testing.T, fields map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal(%v) error = %v", fields, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", raw, err)
	}
	return decoded
}

func keysOf(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	return keys
}

func sameValue(got, want any) bool {
	switch expected := want.(type) {
	case float64:
		number, isNumber := got.(float64)
		return isNumber && math.Abs(number-expected) < 1e-4
	case []any:
		list, isList := got.([]any)
		if !isList || len(list) != len(expected) {
			return false
		}
		for i := range expected {
			if !sameValue(list[i], expected[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		fields, isFields := got.(map[string]any)
		if !isFields || len(fields) != len(expected) {
			return false
		}
		for key, value := range expected {
			if !sameValue(fields[key], value) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}

func TestCreateExpense_WritesOneBudgetItem(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	call := oneCreate(t, stub)
	wantBody(t, body(t, call), map[string]any{
		"name":          "拉麵",
		"category":      "food",
		"total_price":   float64(1200),
		"currency":      "TWD",
		"exchange_rate": float64(1),
		"note":          "PAYMENT:cash",
		"expense_date":  "2026-05-01",
		"payers":        []any{map[string]any{"user_id": float64(8), "amount": float64(1200)}},
		"member_ids":    []any{float64(8)},
	})
	if expense.ID != strconv.Itoa(createItemID) {
		t.Errorf("expense.ID = %q, want %q — the created item's id is the only handle on the row", expense.ID, strconv.Itoa(createItemID))
	}
}

func TestCreateExpense_OmitsThePayersKeyWhenThereIsNoPayer(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()
	expense.PaidByID = ""

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	call := oneCreate(t, stub)
	fields := body(t, call)
	if _, present := fields["payers"]; present {
		t.Errorf("create body carries payers = %#v, want the key left out", fields["payers"])
	}
	if _, present := fields["paid_by_user_id"]; present {
		t.Error("create body carries paid_by_user_id, which TREK's create request does not have")
	}
	if want := `{"name":"拉麵","category":"food","currency":"TWD","total_price":1200,"exchange_rate":1,` +
		`"note":"PAYMENT:cash","expense_date":"2026-05-01","member_ids":[8]}`; call.body != want {
		t.Errorf("create body = %s\nwant %s", call.body, want)
	}
}

func TestCreateExpense_StoresTheAmountInMajorUnits(t *testing.T) {
	tests := []struct {
		name     string
		currency domain.Currency
		price    uint64
		want     float64
	}{
		{name: "TWD has no minor unit", currency: domain.CurrencyTWD, price: 1200, want: 1200},
		{name: "JPY has no minor unit", currency: domain.CurrencyJPY, price: 1000, want: 1000},
		{name: "a large amount is not rescaled", currency: domain.CurrencyTWD, price: 1234567, want: 1234567},
		{name: "a lower-case code is the same currency", currency: domain.Currency("twd"), price: 500, want: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, jpyQuote())
			expense := ramen()
			expense.Currency = tt.currency
			expense.Price = tt.price
			expense.PaidByID = ""

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v", err)
			}

			call := oneCreate(t, stub)
			if got := body(t, call)["total_price"]; got != tt.want {
				t.Errorf("total_price for %d %s = %v, want %v", tt.price, tt.currency, got, tt.want)
			}
		})
	}
}

func TestCreateExpense_RefusesACurrencyItCannotScale(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, jpyQuote())
	expense := ramen()
	expense.Currency = domain.Currency("USD")

	err := repo.CreateExpense(context.Background(), expense)
	if !errors.Is(err, trek.ErrInvalidExpense) {
		t.Fatalf("CreateExpense() error = %v, want ErrInvalidExpense", err)
	}
	if !strings.Contains(err.Error(), "USD") {
		t.Errorf("error %q does not name the currency it could not scale", err)
	}
	if creates := stub.creates(); len(creates) != 0 {
		t.Errorf("a refused expense was written anyway: %v", creates)
	}
}

func TestCreateExpense_RefusesAnAmountTREKCouldNotHoldExactly(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()
	expense.Price = uint64(1)<<53 + 1

	if err := repo.CreateExpense(context.Background(), expense); !errors.Is(err, trek.ErrInvalidExpense) {
		t.Errorf("CreateExpense() error = %v, want ErrInvalidExpense", err)
	}
	if creates := stub.creates(); len(creates) != 0 {
		t.Errorf("an unwriteable amount was written anyway: %v", creates)
	}
}

func TestCreateExpense_WritesAForeignExpenseWithItsPayer(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, jpyQuote())
	expense := ramen()
	expense.Currency = domain.CurrencyJPY
	expense.Price = 1000
	expense.ExchangeRate = decimal.NewFromFloat(0.22)

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	call := oneCreate(t, stub)
	wantBody(t, body(t, call), map[string]any{
		"name":          "拉麵",
		"category":      "food",
		"total_price":   float64(1000),
		"currency":      "JPY",
		"exchange_rate": 4.5455,
		"note":          "PAYMENT:cash",
		"expense_date":  "2026-05-01",
		"payers":        []any{map[string]any{"user_id": float64(8), "amount": float64(1000)}},
		"member_ids":    []any{float64(8)},
	})
}

func TestCreateExpense_QuotesTheProviderWhenNoRateWasSupplied(t *testing.T) {
	provider := jpyQuote()
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, provider)
	expense := ramen()
	expense.Currency = domain.CurrencyJPY
	expense.Price = 1000

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyJPY {
		t.Errorf("asked for %v, want exactly one quote for JPY", provider.asked)
	}
	if got := body(t, oneCreate(t, stub))["exchange_rate"]; !sameValue(got, 4.5455) {
		t.Errorf("exchange_rate = %v, want the 1/0.22 TREK divides by", got)
	}
}

func TestCreateExpense_QuotesNoRateForAnExpenseInTheTripCurrency(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()
	expense.ExchangeRate = decimal.NewFromFloat(4.5455)

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
	if got := body(t, oneCreate(t, stub))["exchange_rate"]; got != float64(1) {
		t.Errorf("exchange_rate = %v, want the identity 1 on a row in the trip currency", got)
	}
}

func TestCreateExpense_RefusesToWriteARateItCannotKnow(t *testing.T) {
	tests := []struct {
		name  string
		base  domain.Currency
		rate  rateQuoter
		cause error
	}{
		{name: "no provider is configured", base: domain.CurrencyTWD, rate: nil, cause: trek.ErrNoExchangeRate},
		{name: "the provider fails", base: domain.CurrencyTWD, rate: &quotedRate{err: errors.New("no exchange rate data available")}, cause: trek.ErrNoExchangeRate},
		{name: "the trip currency is unknown", base: domain.Currency(""), rate: jpyQuote(), cause: trek.ErrUnknownTripCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, tt.base, tt.rate)
			expense := ramen()
			expense.Currency = domain.CurrencyJPY
			expense.Price = 1000

			err := repo.CreateExpense(context.Background(), expense)
			if !errors.Is(err, tt.cause) {
				t.Fatalf("CreateExpense() error = %v, want it to wrap %v", err, tt.cause)
			}
			if creates := stub.creates(); len(creates) != 0 {
				t.Errorf("an expense with no rate was written anyway: %v", creates)
			}
		})
	}
}

func TestCreateExpense_WritesTheCalendarDayItWasGiven(t *testing.T) {
	taipei := time.FixedZone("UTC+8", 8*60*60)
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "a date typed as a bare day", when: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), want: "2026-05-01"},
		{name: "late evening where the expense happened", when: time.Date(2026, 5, 1, 23, 30, 0, 0, taipei), want: "2026-05-01"},
		{name: "just after midnight in the same zone", when: time.Date(2026, 5, 2, 0, 30, 0, 0, taipei), want: "2026-05-02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := ramen()
			expense.ShoppedAt = tt.when

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v", err)
			}
			if got := body(t, oneCreate(t, stub))["expense_date"]; got != tt.want {
				t.Errorf("expense_date for %s = %v, want %q", tt.when, got, tt.want)
			}
		})
	}
}

func TestCreateExpense_RefusesAnExpenseItCannotStore(t *testing.T) {
	tests := []struct {
		name    string
		expense *domain.Expense
		about   string
	}{
		{name: "no expense at all", expense: nil, about: "no expense"},
		{name: "an empty name", expense: withExpense(func(e *domain.Expense) { e.Name = "" }), about: "name"},
		{name: "a name that is only whitespace", expense: withExpense(func(e *domain.Expense) { e.Name = "   " }), about: "name"},
		{name: "no amount", expense: withExpense(func(e *domain.Expense) { e.Price = 0 }), about: "amount"},
		{name: "no date", expense: withExpense(func(e *domain.Expense) { e.ShoppedAt = time.Time{} }), about: "date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			before := len(stub.callsSeen())

			err := repo.CreateExpense(context.Background(), tt.expense)
			if !errors.Is(err, trek.ErrInvalidExpense) {
				t.Fatalf("CreateExpense() error = %v, want ErrInvalidExpense", err)
			}
			if !strings.Contains(err.Error(), tt.about) {
				t.Errorf("error %q does not say what is wrong (%s)", err, tt.about)
			}
			if after := len(stub.callsSeen()); after != before {
				t.Errorf("a refused expense still made %d call(s): %v", after-before, stub.paths()[before:])
			}
		})
	}
}

func TestCreateExpense_RefusesAPayerWhoIsNotOnTheTrip(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()
	expense.PaidByID = "42"

	err := repo.CreateExpense(context.Background(), expense)
	if !errors.Is(err, trek.ErrPayerNotOnTrip) {
		t.Fatalf("CreateExpense() error = %v, want ErrPayerNotOnTrip", err)
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("error %q does not name the payer that is missing", err)
	}
	if creates := stub.creates(); len(creates) != 0 {
		t.Errorf("an off-trip payer was written anyway: %v", creates)
	}
}

func TestCreateExpense_ReportsARejectedWrite(t *testing.T) {
	stub := newCreateStub(t)
	stub.reply = http.StatusUnprocessableEntity
	stub.replyBody = `{"error":"name should not be empty"}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	err := repo.CreateExpense(context.Background(), ramen())
	if err == nil {
		t.Fatal("CreateExpense() = nil, want TREK's rejection")
	}
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("CreateExpense() error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "name should not be empty") {
		t.Errorf("Message = %q, want TREK's own text", apiErr.Message)
	}
}

func TestCreateExpense_LinksTheUploadedReceipt(t *testing.T) {
	stub := newCreateStub(t)
	stub.replyBody = createCreatedLinked
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ReceiptURL = "77" })

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	call := oneCreate(t, stub)
	wantBody(t, body(t, call), map[string]any{
		"name":             "拉麵",
		"category":         "food",
		"total_price":      float64(1200),
		"currency":         "TWD",
		"exchange_rate":    float64(1),
		"note":             "PAYMENT:cash",
		"expense_date":     "2026-05-01",
		"payers":           []any{map[string]any{"user_id": float64(8), "amount": float64(1200)}},
		"member_ids":       []any{float64(8)},
		"receipt_file_ids": []any{float64(77)},
	})
	if want := `{"name":"拉麵","category":"food","currency":"TWD","total_price":1200,"exchange_rate":1,` +
		`"note":"PAYMENT:cash","expense_date":"2026-05-01","payers":[{"user_id":8,"amount":1200}],"member_ids":[8],"receipt_file_ids":[77]}`; call.body != want {
		t.Errorf("create body = %s\nwant %s", call.body, want)
	}

	if expense.ReceiptURL != "77" {
		t.Errorf("expense.ReceiptURL = %q, want the caller's id left alone", expense.ReceiptURL)
	}
	if uploads := stub.uploadsSeen(); len(uploads) != 0 {
		t.Errorf("the create uploaded %d file(s) of its own: %v", len(uploads), uploads)
	}
	if writes := stub.updates(); len(writes) != 0 {
		t.Errorf("the receipt link cost %d further write(s): %v", len(writes), writes)
	}
}

func TestCreateExpense_LeavesTheReceiptOutWhenThereIsNoReceipt(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen() // ReceiptURL empty, exactly as a failed upload leaves it

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v, want the expense written without a receipt", err)
	}
	if expense.ID != strconv.Itoa(createItemID) {
		t.Errorf("expense.ID = %q, want %q — the expense exists either way", expense.ID, strconv.Itoa(createItemID))
	}

	call := oneCreate(t, stub)
	if _, present := body(t, call)["receipt_file_ids"]; present {
		t.Errorf("create body carries receipt_file_ids = %#v, want the key left out", body(t, call)["receipt_file_ids"])
	}
	if want := `{"name":"拉麵","category":"food","currency":"TWD","total_price":1200,"exchange_rate":1,` +
		`"note":"PAYMENT:cash","expense_date":"2026-05-01","payers":[{"user_id":8,"amount":1200}],"member_ids":[8]}`; call.body != want {
		t.Errorf("create body = %s\nwant %s", call.body, want)
	}
}

func TestCreateExpense_AReceiptThatIsNotAFileIDLinksNothing(t *testing.T) {
	for _, receipt := range []string{
		"/api/trips/3/files/77/download",    // what a read leaves in the field
		"https://trek.example.com/files/77", // and what the Notion backend left
		"0", "-77", "77.5", "seventy7",      // nothing a trip_files row can be
	} {
		t.Run("receipt "+receipt, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := withExpense(func(e *domain.Expense) { e.ReceiptURL = receipt })

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v, want the expense written", err)
			}
			if fields := body(t, oneCreate(t, stub)); len(fields) != 9 {
				t.Errorf("create body carries %d fields %v, want the nine of an expense with no receipt", len(fields), keysOf(fields))
			}
		})
	}
}

func TestCreateExpense_ReportsAReceiptTREKDropsWithoutFailingTheCreate(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ReceiptURL = "77" })
	warnings := captureWarnings(t)

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v, want the expense reported as created anyway", err)
	}
	if expense.ID != strconv.Itoa(createItemID) {
		t.Errorf("expense.ID = %q, want %q — the row exists, only the link did not", expense.ID, strconv.Itoa(createItemID))
	}
	if !strings.Contains(warnings.String(), "receipt") || !strings.Contains(warnings.String(), "77") {
		t.Errorf("nothing warned about the receipt that was not linked: %s", warnings)
	}
}

func TestCreateExpense_SaysNothingWhenTREKStoredTheReceiptLink(t *testing.T) {
	stub := newCreateStub(t)
	stub.replyBody = createCreatedLinked
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	warnings := captureWarnings(t)

	if err := repo.CreateExpense(context.Background(), withExpense(func(e *domain.Expense) { e.ReceiptURL = "77" })); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
	if warnings.Len() != 0 {
		t.Errorf("a receipt TREK stored was reported as a problem: %s", warnings)
	}
}

func TestCreateExpense_ReportsAnAmountTREKDropsWithoutFailingTheCreate(t *testing.T) {
	stub := newCreateStub(t)
	stub.replyBody = `{"item":{"id":42,"name":"ramen","currency":"TWD",
	  "exchange_rate":1,"expense_date":"2026-05-01","note":"PAYMENT:cash","payers":[]}}`
	dropped := float64(0)
	stub.storedTotal = &dropped
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := ramen()
	warnings := captureWarnings(t)

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v, want the expense reported as created anyway", err)
	}
	if expense.ID != strconv.Itoa(createItemID) {
		t.Errorf("expense.ID = %q, want %q — the row exists, only the amount did not land", expense.ID, strconv.Itoa(createItemID))
	}
	if !strings.Contains(warnings.String(), "amount") || !strings.Contains(warnings.String(), "1200") {
		t.Errorf("nothing warned about the total that was not stored: %s", warnings)
	}
}

func TestCreateExpense_SaysNothingWhenTREKStoredTheAmount(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	warnings := captureWarnings(t)

	if err := repo.CreateExpense(context.Background(), ramen()); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
	if warnings.Len() != 0 {
		t.Errorf("an amount TREK stored as it was sent was reported as a problem: %s", warnings)
	}
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()

	warnings := &bytes.Buffer{}
	replaced := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(warnings, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(replaced) })
	return warnings
}

func withExpense(change func(*domain.Expense)) *domain.Expense {
	expense := ramen()
	change(expense)
	return expense
}

func TestUpdateExpense_NamesWhenItCollapsesASplit(t *testing.T) {
	warnings := captureWarnings(t)
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{
		"id": 42, "note": "PAYMENT:cash",
		"payers": []any{
			map[string]any{"user_id": 8, "amount": 800},
			map[string]any{"user_id": 9, "amount": 400},
		},
	})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	if err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.Price = 1200 })); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	for line := range strings.SplitSeq(warnings.String(), "\n") {
		if strings.Contains(line, "several users") {
			return
		}
	}
	t.Errorf("no warning named the collapsed split; the log says:\n%s", warnings.String())
}

func budgetListing(items ...string) string {
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

func budgetItemJSON(t *testing.T, fields map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal(%v) error = %v", fields, err)
	}
	return string(raw)
}

func ramenItem() map[string]any {
	return map[string]any{
		"id":              createItemID,
		"name":            "拉麵",
		"category":        "food",
		"total_price":     1200,
		"currency":        "TWD",
		"exchange_rate":   1,
		"note":            "PAYMENT:cash",
		"expense_date":    "2026-05-01",
		"created_at":      "2026-04-30 12:00:00",
		"paid_by_user_id": nil,
		"payers":          []any{map[string]any{"user_id": 8, "amount": 1200, "username": "bob"}},
	}
}

func listingOf(t *testing.T, items ...map[string]any) string {
	t.Helper()

	encoded := make([]string, 0, len(items))
	for _, item := range items {
		fields := ramenItem()
		maps.Copy(fields, item)
		encoded = append(encoded, budgetItemJSON(t, fields))
	}
	return budgetListing(encoded...)
}

func oneBudgetRead(t *testing.T, stub *createStub) createCall {
	t.Helper()

	reads := stub.budgetReads()
	if len(reads) != 1 {
		t.Fatalf("budget reads = %d, want exactly 1 (%v)", len(reads), stub.paths())
	}
	return reads[0]
}

func namesOf(expenses []domain.Expense) []string {
	names := make([]string, 0, len(expenses))
	for _, e := range expenses {
		names = append(names, e.Name)
	}
	return names
}

func oneExpense(t *testing.T, expenses []domain.Expense) domain.Expense {
	t.Helper()

	if len(expenses) != 1 {
		t.Fatalf("listing returned %d expenses %v, want exactly 1", len(expenses), namesOf(expenses))
	}
	return expenses[0]
}

func TestQueryExpenses_ReturnsEveryItemNewestFirst(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t,
		map[string]any{"id": 1, "name": "第一天", "expense_date": "2026-05-01"},
		map[string]any{"id": 2, "name": "第三天", "expense_date": "2026-05-03"},
		map[string]any{"id": 3, "name": "第二天", "expense_date": "2026-05-02"},
	)
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	if got, want := namesOf(expenses), []string{"第三天", "第二天", "第一天"}; !slices.Equal(got, want) {
		t.Errorf("QueryExpenses() = %v, want %v (newest expense date first)", got, want)
	}

	read := oneBudgetRead(t, stub)
	if read.query != "" {
		t.Errorf("the listing asked for %q, but TREK's budget route has no filter to ask for", read.query)
	}
	if read.path != "/api/trips/3/budget" {
		t.Errorf("the listing read %q, want the configured trip's own budget", read.path)
	}
}

func TestQueryExpenses_BreaksASameDayTieByWhenTheRowWasWritten(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t,
		map[string]any{"id": 1, "name": "早上", "expense_date": "2026-05-01", "created_at": "2026-05-01 09:00:00"},
		map[string]any{"id": 2, "name": "晚上", "expense_date": "2026-05-01", "created_at": "2026-05-01 21:00:00"},
		map[string]any{"id": 3, "name": "同秒的後一筆", "expense_date": "2026-05-01", "created_at": "2026-05-01 09:00:00"},
		map[string]any{"id": 4, "name": "沒有建立時間", "expense_date": "2026-05-01", "created_at": ""},
	)
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	want := []string{"晚上", "同秒的後一筆", "早上", "沒有建立時間"}
	if got := namesOf(expenses); !slices.Equal(got, want) {
		t.Errorf("QueryExpenses() = %v, want %v", got, want)
	}
}

func TestQueryExpensesWithFilter_TreatsBothDateBoundsAsInclusiveWholeDays(t *testing.T) {
	taipei := time.FixedZone("UTC+8", 8*60*60)
	startOfDay := time.Date(2026, 5, 2, 0, 0, 0, 0, taipei)
	endOfDay := startOfDay.Add(24 * time.Hour).Add(-time.Nanosecond)

	tests := []struct {
		name string
		from *time.Time
		to   *time.Time
		want []string
	}{
		{name: "one whole day", from: &startOfDay, to: &endOfDay, want: []string{"初二"}},
		{name: "from that day onwards", from: &startOfDay, to: nil, want: []string{"初五", "初四", "初三", "初二"}},
		{name: "up to and including that day", from: nil, to: &endOfDay, want: []string{"初二", "初一"}},
		{name: "a to bound at midnight still means that whole day", from: nil, to: &startOfDay, want: []string{"初二", "初一"}},
		{name: "a from bound at the last instant still means that whole day", from: &endOfDay, to: nil, want: []string{"初五", "初四", "初三", "初二"}},
		{name: "an empty filter keeps everything", from: nil, to: nil, want: []string{"初五", "初四", "初三", "初二", "初一"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t,
				map[string]any{"id": 1, "name": "初一", "expense_date": "2026-05-01"},
				map[string]any{"id": 2, "name": "初二", "expense_date": "2026-05-02"},
				map[string]any{"id": 3, "name": "初三", "expense_date": "2026-05-03"},
				map[string]any{"id": 4, "name": "初四", "expense_date": "2026-05-04"},
				map[string]any{"id": 5, "name": "初五", "expense_date": "2026-05-05"},
			)
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			expenses, err := repo.QueryExpensesWithFilter(context.Background(), expense.ExpenseFilter{DateFrom: tt.from, DateTo: tt.to})
			if err != nil {
				t.Fatalf("QueryExpensesWithFilter() error = %v", err)
			}

			if got := namesOf(expenses); !slices.Equal(got, tt.want) {
				t.Errorf("listing = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueryExpensesWithFilter_ReturnsOnlyThatPayersItems(t *testing.T) {
	tests := []struct {
		name    string
		listing string
		payer   string
		want    []string
	}{
		{
			name: "a payer noccounting wrote",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "拉的", "payers": []any{map[string]any{"user_id": 8, "amount": 1200}}},
				map[string]any{"id": 2, "name": "別人的", "payers": []any{map[string]any{"user_id": 9, "amount": 1200}}},
			),
			payer: "8",
			want:  []string{"拉的"},
		},
		{
			name: "a payer from a row TREK's own UI wrote",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "拉的", "paid_by_user_id": 8, "payers": []any{}},
				map[string]any{"id": 2, "name": "別人的", "paid_by_user_id": 9, "payers": []any{}},
			),
			payer: "8",
			want:  []string{"拉的"},
		},
		{
			name: "nobody paid for these",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "無付款人", "paid_by_user_id": nil, "payers": []any{}},
			),
			payer: "8",
			want:  nil,
		},
		{
			name: "a payer id 8 must not match user 80",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "拉的", "payers": []any{map[string]any{"user_id": 80, "amount": 1200}}},
				map[string]any{"id": 2, "name": "剛好是 8", "payers": []any{map[string]any{"user_id": 8, "amount": 1200}}},
			),
			payer: "8",
			want:  []string{"剛好是 8"},
		},
		{
			name: "one payer of a split",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "拆開的", "payers": []any{
					map[string]any{"user_id": 8, "amount": 800},
					map[string]any{"user_id": 9, "amount": 400},
				}},
			),
			payer: "8",
			want:  []string{"拆開的"},
		},
		{
			name: "the other payer of the same split",
			listing: listingOf(t,
				map[string]any{"id": 1, "name": "拆開的", "payers": []any{
					map[string]any{"user_id": 8, "amount": 800},
					map[string]any{"user_id": 9, "amount": 400},
				}},
			),
			payer: "9",
			want:  []string{"拆開的"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = tt.listing
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			payer := tt.payer

			expenses, err := repo.QueryExpensesWithFilter(context.Background(), expense.ExpenseFilter{PaidByID: &payer})
			if err != nil {
				t.Fatalf("QueryExpensesWithFilter() error = %v", err)
			}

			if got := namesOf(expenses); !slices.Equal(got, tt.want) {
				t.Errorf("listing for payer %s = %v, want %v", payer, got, tt.want)
			}
		})
	}
}

func TestQueryExpensesWithFilter_RefusesAPayerItCannotCompare(t *testing.T) {
	for _, payer := range []string{"bob", "8a", "0", "-8", ""} {
		t.Run("payer "+payer, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			_, err := repo.QueryExpensesWithFilter(context.Background(), expense.ExpenseFilter{PaidByID: &payer})
			if !errors.Is(err, trek.ErrInvalidPayerID) {
				t.Fatalf("QueryExpensesWithFilter() error = %v, want it to wrap ErrInvalidPayerID", err)
			}
			if !strings.Contains(err.Error(), payer) && payer != "" {
				t.Errorf("error %q does not name the payer it could not compare", err)
			}
		})
	}
}

func TestQueryExpensesWithFilter_ReturnsAtMostTheLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "the two newest of five", limit: 2, want: []string{"初五", "初四"}},
		{name: "everything on a trip of five", limit: 5, want: []string{"初五", "初四", "初三", "初二", "初一"}},
		{name: "more than the trip holds", limit: 50, want: []string{"初五", "初四", "初三", "初二", "初一"}},
		{name: "a limit of none at all", limit: 0, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t,
				map[string]any{"id": 1, "name": "初一", "expense_date": "2026-05-01"},
				map[string]any{"id": 2, "name": "初二", "expense_date": "2026-05-02"},
				map[string]any{"id": 3, "name": "初三", "expense_date": "2026-05-03"},
				map[string]any{"id": 4, "name": "初四", "expense_date": "2026-05-04"},
				map[string]any{"id": 5, "name": "初五", "expense_date": "2026-05-05"},
			)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			limit := tt.limit

			expenses, err := repo.QueryExpensesWithFilter(context.Background(), expense.ExpenseFilter{Limit: &limit})
			if err != nil {
				t.Fatalf("QueryExpensesWithFilter() error = %v", err)
			}

			if got := namesOf(expenses); !slices.Equal(got, tt.want) {
				t.Errorf("listing with limit %d = %v, want %v", limit, got, tt.want)
			}
		})
	}
}

func TestQueryExpenses_SkipsAnItemItCannotRead(t *testing.T) {
	tests := []struct {
		name   string
		broken map[string]any
	}{
		{name: "a date TREK cannot have stored", broken: map[string]any{"expense_date": "01/05/2026"}},
		{name: "no date at all", broken: map[string]any{"expense_date": ""}},
		{name: "a currency noccounting has no minor-unit table for", broken: map[string]any{"currency": "USD"}},
		{name: "an item with no amount", broken: map[string]any{"total_price": 0}},
		{name: "an amount larger than the domain can hold", broken: map[string]any{"total_price": 1e19}},
		{name: "a foreign amount with no frozen rate and nobody to ask", broken: map[string]any{"currency": "JPY", "exchange_rate": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broken := map[string]any{"id": 1, "name": "壞掉的"}
			maps.Copy(broken, tt.broken)
			stub := newCreateStub(t)
			stub.listing = listingOf(t, broken, map[string]any{"id": 2, "name": "好的"})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			expenses, err := repo.QueryExpenses(context.Background())
			if err != nil {
				t.Fatalf("QueryExpenses() error = %v, want the readable items rather than the whole listing failing", err)
			}

			got := oneExpense(t, expenses)
			if got.Name != "好的" {
				t.Errorf("listing kept %q, want the item that could be read", got.Name)
			}
			if got.ID != "2" || got.Price != 1200 {
				t.Errorf("the surviving item = %+v, want item 2 in full", got)
			}
		})
	}
}

func TestQueryExpenses_ReadsEveryFieldOfAnItem(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = budgetListing(`{
	  "id": 42, "trip_id": 3, "name": "拉麵", "category": "shopping",
	  "total_price": 1200, "currency": "TWD", "exchange_rate": 1,
	  "note": "PAYMENT:credit_card|加收小費", "expense_date": "2026-05-01",
	  "created_at": "2026-04-30 12:00:00", "paid_by_user_id": null,
	  "ticket_json": null, "persons": null, "days": null,
	  "members": [], "payers": [{"user_id": 8, "amount": 1200, "username": "bob"}], "receipts": []
	}`)
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	got := oneExpense(t, expenses)
	if got.ID != "42" {
		t.Errorf("ID = %q, want %q — the item id is the only handle on the row", got.ID, "42")
	}
	if got.Name != "拉麵" {
		t.Errorf("Name = %q, want %q", got.Name, "拉麵")
	}
	if got.Price != 1200 {
		t.Errorf("Price = %d, want 1200 — the domain counts the smallest unit", got.Price)
	}
	if got.Currency != domain.CurrencyTWD {
		t.Errorf("Currency = %q, want TWD", got.Currency)
	}
	if got.Category != domain.CategoryShopping {
		t.Errorf("Category = %q, want %q for the shopping slug", got.Category, domain.CategoryShopping)
	}
	if got.Method != domain.PaymentMethodCreditCard {
		t.Errorf("Method = %q, want the method the note was encoded with", got.Method)
	}
	if got.PaidByID != "8" {
		t.Errorf("PaidByID = %q, want the TREK user id as a string", got.PaidByID)
	}
	if want := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC); !got.ShoppedAt.Equal(want) {
		t.Errorf("ShoppedAt = %s, want %s — the stored date read back as that day", got.ShoppedAt, want)
	}
	if !got.ExchangeRate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("ExchangeRate = %s, want the identity on a row in the trip currency", got.ExchangeRate)
	}
	if len(got.ReceiptItems) != 0 {
		t.Errorf("ReceiptItems = %v, want none for a null ticket_json", got.ReceiptItems)
	}
}

func TestQueryExpenses_ReadsTheReceiptOffTheItem(t *testing.T) {
	tests := []struct {
		name     string
		receipts []any
		want     string
	}{
		{
			name:     "one receipt",
			receipts: []any{map[string]any{"id": 77, "filename": "3f9c1a.jpg", "original_name": "receipt-4242.jpg", "file_size": 2048, "mime_type": "image/jpeg", "url": "/api/trips/3/files/77/download"}},
			want:     "/api/trips/3/files/77/download",
		},
		{name: "several receipts, in TREK's own order", receipts: []any{
			map[string]any{"id": 77, "url": "/api/trips/3/files/77/download"},
			map[string]any{"id": 78, "url": "/api/trips/3/files/78/download"},
		}, want: "/api/trips/3/files/77/download"},
		{name: "no receipt at all", receipts: []any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"receipts": tt.receipts})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			expenses, err := repo.QueryExpenses(context.Background())
			if err != nil {
				t.Fatalf("QueryExpenses() error = %v", err)
			}

			if got := oneExpense(t, expenses).ReceiptURL; got != tt.want {
				t.Errorf("ReceiptURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryExpenses_ReadsAForeignAmountThroughTheRateTREKFroze(t *testing.T) {
	provider := jpyQuote()
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{
		"currency": "JPY", "total_price": 1000, "exchange_rate": 4.5455,
	})
	repo := stub.repo(t, domain.CurrencyTWD, provider)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	got := oneExpense(t, expenses)
	if got.Currency != domain.CurrencyJPY || got.Price != 1000 {
		t.Errorf("read %d %s, want 1000 JPY", got.Price, got.Currency)
	}
	if !got.ExchangeRate.Round(2).Equal(decimal.RequireFromString("0.22")) {
		t.Errorf("ExchangeRate = %s, want the 0.22 TWD per JPY the stored rate inverts to", got.ExchangeRate)
	}
	if total := got.TotalInBase(domain.CurrencyTWD); !total.Round(0).Equal(decimal.NewFromInt(220)) {
		t.Errorf("TotalInBase() = %s, want the 220 TWD R6's scenario reports", total)
	}
	if len(provider.asked) != 0 {
		t.Errorf("the provider was asked for %v, want no quote for a row whose rate was frozen", provider.asked)
	}
}

func TestQueryExpenses_QuotesAForeignRowWhoseRateWasNeverFrozen(t *testing.T) {
	provider := jpyQuote()
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"currency": "JPY", "total_price": 1000, "exchange_rate": 1})
	repo := stub.repo(t, domain.CurrencyTWD, provider)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	got := oneExpense(t, expenses)
	if len(provider.asked) != 1 || provider.asked[0] != domain.CurrencyJPY {
		t.Errorf("the provider was asked for %v, want exactly one quote for JPY", provider.asked)
	}
	if total := got.TotalInBase(domain.CurrencyTWD); !total.Round(0).Equal(decimal.NewFromInt(220)) {
		t.Errorf("TotalInBase() = %s, want the 220 TWD a quote of 0.22 gives", total)
	}
}

func TestQueryExpenses_ReadsAnAbsentCurrencyAsTheTripCurrency(t *testing.T) {
	provider := jpyQuote()
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"currency": nil, "total_price": 1200, "exchange_rate": 1})
	repo := stub.repo(t, domain.CurrencyTWD, provider)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	got := oneExpense(t, expenses)
	if got.Currency != domain.CurrencyTWD {
		t.Errorf("Currency = %q, want the trip currency TREK's NULL column stands for", got.Currency)
	}
	if total := got.TotalInBase(domain.CurrencyTWD); !total.Equal(decimal.NewFromInt(1200)) {
		t.Errorf("TotalInBase() = %s, want 1200 with no conversion at all", total)
	}
	if len(provider.asked) != 0 {
		t.Errorf("the provider was asked for %v, want no quote for a row in the trip currency", provider.asked)
	}
}

func TestQueryExpenses_RefusesToReadWithoutTheTripCurrency(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t)
	repo := &tripRepoFixture{repo: trek.NewRepo(stub.startedClient(t), nil), trip: domain.Trip{ID: createTripID}}

	if _, err := repo.QueryExpenses(context.Background()); !errors.Is(err, trek.ErrUnknownTripCurrency) {
		t.Fatalf("QueryExpenses() error = %v, want it to wrap ErrUnknownTripCurrency", err)
	}
	if reads := stub.budgetReads(); len(reads) != 0 {
		t.Errorf("the listing was read %d time(s) although nothing could be read from it", len(reads))
	}
}

func TestQueryExpenses_ReportsARejectedRead(t *testing.T) {
	stub := newCreateStub(t)
	stub.readStatus = http.StatusForbidden
	stub.listing = `{"error":"Budget is not available on this trip","code":"BUDGET_DISABLED"}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	_, err := repo.QueryExpenses(context.Background())
	if err == nil {
		t.Fatal("QueryExpenses() = nil, want TREK's rejection")
	}
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("QueryExpenses() error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "BUDGET_DISABLED" {
		t.Errorf("error = %v, want TREK's own 403 and code", apiErr)
	}
	if !strings.Contains(apiErr.Message, "Budget is not available") {
		t.Errorf("Message = %q, want TREK's own text", apiErr.Message)
	}
}

const updateItemID = "42"

type itemRow struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	Currency     string  `json:"currency"`
	TotalPrice   float64 `json:"total_price"`
	ExchangeRate float64 `json:"exchange_rate"`
	Note         string  `json:"note"`
	ExpenseDate  string  `json:"expense_date"`
	TicketJSON   *string `json:"ticket_json"`
	Payers       []struct {
		UserID int64   `json:"user_id"`
		Amount float64 `json:"amount"`
	} `json:"payers"`
}

func applyTREKUpdate(row *itemRow, fields map[string]any) {
	if name, ok := fields["name"].(string); ok && name != "" {
		row.Name = name
	}
	if category, ok := fields["category"].(string); ok && category != "" {
		row.Category = category
	}
	if currency, ok := fields["currency"].(string); ok && currency != "" {
		row.Currency = currency
	}
	if rate, ok := fields["exchange_rate"].(float64); ok {
		row.ExchangeRate = rate
	}
	if date, ok := fields["expense_date"].(string); ok && date != "" {
		row.ExpenseDate = date
	}

	note, hasNote := fields["note"].(string)
	ticket, hasTicket := fields["ticket_json"].(string)
	if hasNote && strings.HasPrefix(note, "TICKETJSON:") {
		hasNote = false
		ticket, hasTicket = note[len("TICKETJSON:"):], true
	}
	if hasNote {
		row.Note = note
	}
	if hasTicket {
		row.TicketJSON = &ticket
	}
	if total, ok := fields["total_price"].(float64); ok {
		row.TotalPrice = total
	}

	payers, touchesPayers := fields["payers"].([]any)
	if !touchesPayers {
		return
	}
	row.Payers = nil
	total := float64(0)
	for _, payer := range payers {
		entry, isEntry := payer.(map[string]any)
		if !isEntry {
			continue
		}
		amount, _ := entry["amount"].(float64)
		if amount == 0 {
			continue
		}
		row.Payers = append(row.Payers, struct {
			UserID int64   `json:"user_id"`
			Amount float64 `json:"amount"`
		}{UserID: int64(entry["user_id"].(float64)), Amount: amount})
		total += amount
	}
	row.TotalPrice = total
	if len(payers) == 0 {
		if explicit, ok := fields["total_price"].(float64); ok {
			row.TotalPrice = explicit
		}
	}
}

func edited(change func(*domain.Expense)) *domain.Expense {
	expense := ramen()
	if change != nil {
		change(expense)
	}
	expense.ID = updateItemID
	return expense
}

func TestUpdateExpense_ReplacesEveryMutableField(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "name": "拉麵", "note": "PAYMENT:cash"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := edited(func(e *domain.Expense) {
		e.Name = "牛肉麵"
		e.Price = 1500
		e.Category = domain.CategoryShopping
		e.Method = domain.PaymentMethodCreditCard
	})

	if err := repo.UpdateExpense(context.Background(), expense); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	update := oneUpdate(t, stub)
	if update.path != "/api/trips/3/budget/42" {
		t.Errorf("the update went to %q, want the one budget item of the configured trip", update.path)
	}
	wantBody(t, body(t, update), map[string]any{
		"name":          "牛肉麵",
		"category":      "shopping",
		"currency":      "TWD",
		"total_price":   float64(1500),
		"exchange_rate": float64(1),
		"note":          "PAYMENT:credit_card",
		"expense_date":  "2026-05-01",
		"payers":        []any{map[string]any{"user_id": float64(8), "amount": float64(1500)}},
		"member_ids":    []any{float64(8)},
	})
	if want := `{"name":"牛肉麵","category":"shopping","currency":"TWD","total_price":1500,` +
		`"exchange_rate":1,"note":"PAYMENT:credit_card","expense_date":"2026-05-01",` +
		`"payers":[{"user_id":8,"amount":1500}],"member_ids":[8]}`; update.body != want {
		t.Errorf("update body = %s\nwant %s", update.body, want)
	}

	row := stub.rowSeen(t, 42)
	if row.Name != "牛肉麵" || row.Category != "shopping" {
		t.Errorf("the row is %q in %q, want the new name and category", row.Name, row.Category)
	}
	if row.TotalPrice != 1500 {
		t.Errorf("row total_price = %v, want 1500 — the payer carries the whole amount", row.TotalPrice)
	}
	if len(row.Payers) != 1 || row.Payers[0].UserID != 8 {
		t.Errorf("row payers = %v, want user 8", row.Payers)
	}
	if row.Note != "PAYMENT:credit_card" {
		t.Errorf("row note = %q, want the method re-encoded", row.Note)
	}
}

func TestUpdateExpense_ReEncodesTheMethodIntoTheNoteItReadsOffTheRow(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		method domain.PaymentMethod
		want   string
	}{
		{name: "the text survives a new method", stored: "PAYMENT:cash|加收小費", method: domain.PaymentMethodCreditCard, want: "PAYMENT:credit_card|加收小費"},
		{name: "a note with no text stays a bare method", stored: "PAYMENT:cash", method: domain.PaymentMethodCreditCard, want: "PAYMENT:credit_card"},
		{name: "an unwritten note is not text", stored: "", method: domain.PaymentMethodCreditCard, want: "PAYMENT:credit_card"},
		{name: "text someone typed by hand is kept", stored: "加了小費", method: domain.PaymentMethodCash, want: "PAYMENT:cash|加了小費"},
		{name: "clearing the method leaves the text alone", stored: "PAYMENT:cash|加收小費", method: "", want: "加收小費"},
		{name: "a separator in the text is part of the text", stored: "PAYMENT:cash|一 | 二", method: domain.PaymentMethodCash, want: "PAYMENT:cash|一 | 二"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": tt.stored})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			if err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.Method = tt.method })); err != nil {
				t.Fatalf("UpdateExpense() error = %v", err)
			}

			if got := body(t, oneUpdate(t, stub))["note"]; got != tt.want {
				t.Errorf("update body note = %#v, want %#v", got, tt.want)
			}
			if got := stub.rowSeen(t, 42).Note; got != tt.want {
				t.Errorf("the stored note = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdateExpense_ReadsTheNoteOffTheRowRatherThanOffTheCaller(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash|加收小費"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	if err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.Method = domain.PaymentMethodCreditCard })); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	if reads := stub.budgetReads(); len(reads) != 1 {
		t.Fatalf("budget reads = %d, want exactly 1 — the note is read off the row, and there is no route that reads one item", len(reads))
	}
}

func TestUpdateExpense_ClearingThePayerKeepsTheAmount(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{
		"id": 42, "note": "PAYMENT:cash",
		"payers": []any{map[string]any{"user_id": 8, "amount": 1200}},
	})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	if err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.PaidByID = "" })); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	update := oneUpdate(t, stub)
	if want := `{"name":"拉麵","category":"food","currency":"TWD","total_price":1200,` +
		`"exchange_rate":1,"note":"PAYMENT:cash","expense_date":"2026-05-01","payers":[],"member_ids":[8]}`; update.body != want {
		t.Errorf("update body = %s\nwant %s", update.body, want)
	}

	row := stub.rowSeen(t, 42)
	if len(row.Payers) != 0 {
		t.Errorf("row payers = %v, want none — the payer was cleared", row.Payers)
	}
	if row.TotalPrice != 1200 {
		t.Errorf("row total_price = %v, want 1200 — an empty payers array rewrites the total to 0 unless the body carries it", row.TotalPrice)
	}
}

func TestUpdateExpense_SendingThePayerAndTheTotalTogetherIsWhatKeepsTheAmount(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]any
		want   float64
	}{
		{
			name:   "the amount alone leaves the payer behind",
			fields: map[string]any{"total_price": float64(1200)},
			want:   1200,
		},
		{
			name:   "an empty payer list alone zeroes the amount",
			fields: map[string]any{"payers": []any{}},
			want:   0,
		},
		{
			name:   "both together clear the payer and keep the amount",
			fields: map[string]any{"total_price": float64(1200), "payers": []any{}},
			want:   1200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := itemRow{ID: 42, Name: "拉麵", TotalPrice: 900, Payers: []struct {
				UserID int64   `json:"user_id"`
				Amount float64 `json:"amount"`
			}{{UserID: 8, Amount: 900}}}

			applyTREKUpdate(&row, tt.fields)

			if row.TotalPrice != tt.want {
				t.Errorf("total_price = %v, want %v", row.TotalPrice, tt.want)
			}
		})
	}
}

func TestUpdateExpense_AnEmptyNameWouldBeSilentlyIgnoredSoItIsRefused(t *testing.T) {
	row := itemRow{ID: 42, Name: "拉麵"}
	applyTREKUpdate(&row, map[string]any{"name": "", "total_price": float64(1200)})
	if row.Name != "拉麵" {
		t.Errorf("the stored name is %q, want the old one — an empty name is dropped, which is what the refusal below is for", row.Name)
	}

	for _, name := range []string{"", "   "} {
		t.Run("name "+name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.Name = name }))
			if !errors.Is(err, trek.ErrInvalidExpense) {
				t.Fatalf("UpdateExpense() error = %v, want ErrInvalidExpense", err)
			}
			if !strings.Contains(err.Error(), "name") {
				t.Errorf("error %q does not say what is wrong", err)
			}
			if writes := len(stub.updates()) + len(stub.deletes()); writes != 0 {
				t.Errorf("a refused update still wrote %d time(s): %v", writes, stub.paths())
			}
		})
	}
}

func TestUpdateExpense_RefusesAnUpdateItCannotAddress(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "no id at all", id: ""},
		{name: "an id of only whitespace", id: "   "},
		{name: "an id that is not a number", id: "bob"},
		{name: "an id that is not a whole number", id: "42.5"},
		{name: "an id that is not a row id", id: "0"},
		{name: "a negative id", id: "-42"},
		{name: "a path dressed as an id", id: "42/members"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := edited(nil)
			expense.ID = tt.id
			before := len(stub.callsSeen())

			err := repo.UpdateExpense(context.Background(), expense)
			if !errors.Is(err, trek.ErrInvalidExpense) {
				t.Fatalf("UpdateExpense() error = %v, want ErrInvalidExpense", err)
			}
			if after := len(stub.callsSeen()); after != before {
				t.Errorf("a refused update still made %d call(s): %v", after-before, stub.paths()[before:])
			}
		})
	}
}

func TestUpdateExpense_RefusesNoExpenseAtAll(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	before := len(stub.callsSeen())

	if err := repo.UpdateExpense(context.Background(), nil); !errors.Is(err, trek.ErrInvalidExpense) {
		t.Fatalf("UpdateExpense() error = %v, want ErrInvalidExpense", err)
	}
	if after := len(stub.callsSeen()); after != before {
		t.Errorf("a refused update still made %d call(s): %v", after-before, stub.paths()[before:])
	}
}

func TestUpdateExpense_RefusesAnExpenseItCannotStore(t *testing.T) {
	tests := []struct {
		name    string
		expense *domain.Expense
		about   string
	}{
		{name: "an empty name", expense: edited(func(e *domain.Expense) { e.Name = "" }), about: "name"},
		{name: "no amount", expense: edited(func(e *domain.Expense) { e.Price = 0 }), about: "amount"},
		{name: "no date", expense: edited(func(e *domain.Expense) { e.ShoppedAt = time.Time{} }), about: "date"},
		{name: "a currency noccounting cannot scale", expense: edited(func(e *domain.Expense) { e.Currency = domain.Currency("USD") }), about: "USD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			repo := stub.repo(t, domain.CurrencyTWD, jpyQuote())
			before := len(stub.callsSeen())

			err := repo.UpdateExpense(context.Background(), tt.expense)
			if !errors.Is(err, trek.ErrInvalidExpense) {
				t.Fatalf("UpdateExpense() error = %v, want ErrInvalidExpense", err)
			}
			if !strings.Contains(err.Error(), tt.about) {
				t.Errorf("error %q does not say what is wrong (%s)", err, tt.about)
			}
			if writes := len(stub.updates()) + len(stub.deletes()); writes != 0 {
				t.Errorf("a refused update still wrote %d time(s): %v", writes, stub.paths()[before:])
			}
		})
	}
}

func TestUpdateExpense_RefusesAPayerWhoIsNotOnTheTrip(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := edited(func(e *domain.Expense) { e.PaidByID = "42" })
	readsBefore := len(stub.budgetReads())

	err := repo.UpdateExpense(context.Background(), expense)
	if !errors.Is(err, trek.ErrPayerNotOnTrip) {
		t.Fatalf("UpdateExpense() error = %v, want ErrPayerNotOnTrip", err)
	}
	if writes := len(stub.updates()) + len(stub.deletes()); writes != 0 {
		t.Errorf("an off-trip payer was written anyway: %v", stub.paths())
	}
	if reads := len(stub.budgetReads()); reads != readsBefore {
		t.Errorf("the note was read %d time(s) before the write was refused", reads-readsBefore)
	}
}

func TestUpdateExpense_LeavesTheReceiptsAndTheItemizedLinesAlone(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash|加收小費"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	if err := repo.UpdateExpense(context.Background(), edited(func(e *domain.Expense) { e.Price = 1500 })); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	fields := body(t, oneUpdate(t, stub))
	for _, key := range []string{"receipt_file_ids", "ticket_json", "members", "persons", "days", "sort_order", "paid_by_user_id"} {
		if value, present := fields[key]; present {
			t.Errorf("the update carries %q = %#v, want the key left out so the stored column is kept", key, value)
		}
	}
}

func TestUpdateExpense_NamesAReceiptOnlyWhenTheExpenseCarriesItsID(t *testing.T) {
	tests := []struct {
		name    string
		receipt string
		want    []any
	}{
		{name: "the id an upload returned", receipt: "77", want: []any{float64(77)}},
		{name: "an id with whitespace around it", receipt: " 77\t", want: []any{float64(77)}},
		{name: "the path a read left", receipt: "/api/trips/3/files/77/download"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			expense := edited(func(e *domain.Expense) { e.ReceiptURL = tt.receipt })
			if err := repo.UpdateExpense(context.Background(), expense); err != nil {
				t.Fatalf("UpdateExpense() error = %v", err)
			}

			got, present := body(t, oneUpdate(t, stub))["receipt_file_ids"]
			if tt.want == nil {
				if present {
					t.Errorf("the update carries receipt_file_ids = %#v, want the key left out", got)
				}
				return
			}
			if !present {
				t.Fatalf("the update carries no receipt_file_ids, want %v", tt.want)
			}
			if !sameValue(got, tt.want) {
				t.Errorf("receipt_file_ids = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestUpdateExpense_ReportsARejectedUpdate(t *testing.T) {
	tests := []struct {
		name   string
		status int
		reply  string
	}{
		{name: "the row is not there", status: http.StatusNotFound, reply: `{"error":"Budget item not found"}`},
		{name: "the credential may not edit the budget", status: http.StatusForbidden, reply: `{"error":"Missing budget_edit permission","code":"FORBIDDEN"}`},
		{name: "the body was refused", status: http.StatusUnprocessableEntity, reply: `{"error":"name should not be empty"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			stub.updateStatus, stub.updateBody = tt.status, tt.reply
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			err := repo.UpdateExpense(context.Background(), edited(nil))
			if err == nil {
				t.Fatal("UpdateExpense() = nil, want TREK's rejection")
			}
			var apiErr *trek.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("UpdateExpense() error = %v, want an *trek.APIError", err)
			}
			if apiErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
			}
			if !strings.Contains(apiErr.Message, "Budget item not found") &&
				!strings.Contains(apiErr.Message, "budget_edit") &&
				!strings.Contains(apiErr.Message, "name should not be empty") {
				t.Errorf("Message = %q, want TREK's own text", apiErr.Message)
			}
			if !strings.Contains(err.Error(), "42") {
				t.Errorf("error %q does not name the expense it could not update", err)
			}
		})
	}
}

func TestUpdateExpense_TellsARowThatIsGoneApartFromOneThatIsForbidden(t *testing.T) {
	statuses := map[int]bool{}
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		stub := newCreateStub(t)
		stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
		stub.updateStatus = status
		stub.updateBody = `{"error":"Budget item not found"}`
		repo := stub.repo(t, domain.CurrencyTWD, nil)

		err := repo.UpdateExpense(context.Background(), edited(nil))
		var apiErr *trek.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: error = %v, want an *trek.APIError", status, err)
		}
		if apiErr.StatusCode != status {
			t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, status)
		}
		statuses[status] = true
	}
	if len(statuses) != 2 {
		t.Errorf("the two rejections were not distinguishable: %v", statuses)
	}
}

func TestUpdateExpense_LetsTREKDecideWhetherTheRowStillExists(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 7, "name": "別人的"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	err := repo.UpdateExpense(context.Background(), edited(nil))
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("UpdateExpense() error = %v, want TREK's own 404", err)
	}
	if writes := len(stub.updates()); writes != 1 {
		t.Errorf("budget updates = %d, want 1 — existence is TREK's to decide, not the listing's", writes)
	}
	if got := stub.rowSeen(t, 7).Name; got != "別人的" {
		t.Errorf("the other row is %q, want it untouched", got)
	}
}

func TestUpdateExpense_ReportsAReadItCouldNotDoTheWriteWithout(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
	stub.readStatus = http.StatusInternalServerError
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	err := repo.UpdateExpense(context.Background(), edited(nil))
	if err == nil {
		t.Fatal("UpdateExpense() = nil, want the failed read behind the note")
	}
	if writes := len(stub.updates()) + len(stub.deletes()); writes != 0 {
		t.Errorf("the update went out %d time(s) although its note could not be read", writes)
	}
}

func TestDeleteExpense_RemovesTheRowPermanently(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	before := len(stub.callsSeen())

	if err := repo.DeleteExpense(context.Background(), updateItemID); err != nil {
		t.Fatalf("DeleteExpense() error = %v", err)
	}

	del := oneDelete(t, stub)
	if del.path != "/api/trips/3/budget/42" {
		t.Errorf("the delete went to %q, want the one budget item of the configured trip", del.path)
	}
	if del.body != "" {
		t.Errorf("the delete carried a body %q, want none", del.body)
	}
	if !stub.wasDeleted() {
		t.Error("the row is still on the budget, want it removed")
	}
	if after := stub.paths()[before:]; len(after) != 1 {
		t.Errorf("the delete made %d call(s) %v, want the one DELETE", len(after), after)
	}
	if reads := stub.budgetReads(); len(reads) != 0 {
		t.Errorf("a delete read the budget %d time(s), want none", len(reads))
	}
}

func TestDeleteExpense_RefusesAnEmptyOrUnusableID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "no id at all", id: ""},
		{name: "an id of only whitespace", id: " \t "},
		{name: "an id that is not a number", id: "ramen"},
		{name: "an id that is not a whole number", id: "42.0"},
		{name: "an id that is not a row id", id: "0"},
		{name: "a path dressed as an id", id: "42/members"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			before := len(stub.callsSeen())

			err := repo.DeleteExpense(context.Background(), tt.id)
			if !errors.Is(err, trek.ErrInvalidExpense) {
				t.Fatalf("DeleteExpense() error = %v, want ErrInvalidExpense", err)
			}
			if after := len(stub.callsSeen()); after != before {
				t.Errorf("a refused delete still made %d call(s): %v", after-before, stub.paths()[before:])
			}
		})
	}
}

func TestDeleteExpense_ReportsARowTREKDoesNotHave(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 7, "name": "別人的"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	err := repo.DeleteExpense(context.Background(), updateItemID)
	if err == nil {
		t.Fatal("DeleteExpense() = nil, want TREK's 404")
	}
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("DeleteExpense() error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "Budget item not found") {
		t.Errorf("Message = %q, want TREK's own text", apiErr.Message)
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("error %q does not name the expense it could not delete", err)
	}
	if stub.wasDeleted() {
		t.Error("a row was removed although TREK refused")
	}
}

func TestDeleteExpense_ReportsAnAnswerThatDoesNotSayItDeleted(t *testing.T) {
	for _, reply := range []string{`{"success":false}`, `{}`} {
		t.Run(reply, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
			stub.deleteBody = reply
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			err := repo.DeleteExpense(context.Background(), updateItemID)
			if err == nil {
				t.Fatalf("DeleteExpense() = nil for %s, want a refusal rather than a reported removal", reply)
			}
			if !strings.Contains(err.Error(), "42") {
				t.Errorf("error %q does not name the expense it could not confirm removing", err)
			}
		})
	}
}

type tripRepoFixture struct {
	repo *trek.Repo
	trip domain.Trip
}

func (r *tripRepoFixture) CreateExpense(ctx context.Context, e *domain.Expense) error {
	return r.repo.CreateExpense(ctx, r.trip, e)
}

func (r *tripRepoFixture) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	return r.repo.QueryExpenses(ctx, r.trip)
}

func (r *tripRepoFixture) QueryExpensesWithFilter(ctx context.Context, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	return r.repo.QueryExpensesWithFilter(ctx, r.trip, filter)
}

func (r *tripRepoFixture) UpdateExpense(ctx context.Context, e *domain.Expense) error {
	return r.repo.UpdateExpense(ctx, r.trip, e)
}

func (r *tripRepoFixture) DeleteExpense(ctx context.Context, id string) error {
	return r.repo.DeleteExpense(ctx, r.trip, id)
}

func (r *tripRepoFixture) Settlement(ctx context.Context) (*domain.Settlement, error) {
	return r.repo.Settlement(ctx, r.trip)
}

func (r *tripRepoFixture) Members(ctx context.Context) ([]domain.Member, error) {
	return r.repo.Members(ctx, r.trip)
}

func (r *tripRepoFixture) UploadFile(ctx context.Context, filePath string) (string, error) {
	return r.repo.UploadFile(ctx, r.trip, filePath)
}
