package trek

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func TestLiveT1Contract(t *testing.T) {
	if os.Getenv("T1_LIVE") == "" {
		t.Skip("T1_LIVE=1 and T1_TRIP_ID with the TREK credential exported run this round trip against the live instance")
	}

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v (export the .env: set -a; . ./.env; set +a)", err)
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	tripID, err := strconv.ParseInt(os.Getenv("T1_TRIP_ID"), 10, 64)
	if err != nil {
		t.Fatalf("T1_TRIP_ID must name the designated test trip: %v", err)
	}
	trips, err := client.ListTrips(ctx)
	if err != nil {
		t.Fatalf("ListTrips() error = %v", err)
	}
	var trip domain.Trip
	for _, candidate := range trips {
		if candidate.ID == tripID {
			trip = candidate
		}
	}
	if trip.ID == 0 {
		t.Fatalf("the service account is not on trip %d, or the trip is not in a currency noccounting accounts in", tripID)
	}
	if trip.Currency != domain.CurrencyTWD {
		t.Fatalf("trip %d is denominated in %s, want TWD; set the trip's currency in TREK first", trip.ID, trip.Currency)
	}

	var rawTrips json.RawMessage
	if err := client.do(ctx, "GET", tripPathPrefix, nil, &rawTrips); err != nil {
		t.Fatalf("reading the trip list for the record: %v", err)
	}
	t.Logf("observed GET /api/trips: %s", rawTrips)

	roster, err := NewPayerResolver(client, trip.ID).rosterFor(ctx)
	if err != nil {
		t.Fatalf("reading the trip roster: %v", err)
	}
	t.Logf("trip roster: %s", roster)

	repo := newTripRepo(client, trip, NewPayerResolver(client, trip.ID), nil)
	name := "T1 live 驗收 " + time.Now().Format("0102-150405")

	exp := &domain.Expense{
		Name:         name,
		Price:        1000,
		Currency:     domain.CurrencyJPY,
		ExchangeRate: decimal.NewFromFloat(0.22), // noccounting's side: 0.22 TWD per JPY
		Category:     domain.CategoryFood,
		Method:       domain.PaymentMethodCash,
		ShoppedAt:    time.Now(),
	}
	if err := repo.CreateExpense(ctx, exp); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}
	if exp.ID == "" {
		t.Fatal("CreateExpense() left no id on the expense; nothing downstream could address it")
	}
	itemID := exp.ID
	t.Logf("created budget item %s", itemID)

	created := liveReadByID(t, repo, itemID)
	liveAssertExpense(t, created, liveExpectation{
		name:     name,
		price:    1000,
		currency: domain.CurrencyJPY,
		rate:     decimal.NewFromFloat(0.22),
		method:   domain.PaymentMethodCash,
	})
	if got := created.TotalInBase(trip.Currency).Round(0); !got.Equal(decimal.NewFromInt(220)) {
		t.Errorf("TotalInBase(trip.Currency) = %s, want 220 (1000 JPY at 0.22)", got)
	}

	raw := liveRawItem(t, client, trip.ID, itemID)
	t.Logf("observed GET /api/trips/%d/budget item %s: %s", trip.ID, itemID, raw)

	created.Name = name + " 改"
	created.Price = 2000
	created.Method = domain.PaymentMethodCreditCard
	if err := repo.UpdateExpense(ctx, &created); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}
	updated := liveReadByID(t, repo, itemID)
	liveAssertExpense(t, updated, liveExpectation{
		name:     name + " 改",
		price:    2000,
		currency: domain.CurrencyJPY,
		rate:     decimal.NewFromFloat(0.22),
		method:   domain.PaymentMethodCreditCard,
	})

	receiptPath := filepath.Join(t.TempDir(), "receipt.png")
	if err := os.WriteFile(receiptPath, liveTinyPNG(), 0o600); err != nil {
		t.Fatalf("writing the test receipt: %v", err)
	}
	fileID, err := repo.UploadFile(ctx, receiptPath)
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	t.Logf("uploaded receipt file %s", fileID)

	linked := liveReadByID(t, repo, itemID)
	linked.ReceiptURL = fileID // the id an upload returns is what a write carries
	if err := repo.UpdateExpense(ctx, &linked); err != nil {
		t.Fatalf("UpdateExpense() linking the receipt: %v", err)
	}
	withReceipt := liveReadByID(t, repo, itemID)
	if withReceipt.ReceiptURL == "" {
		t.Fatal("ReceiptURL is empty after linking the uploaded file, want TREK's download path")
	}
	t.Logf("receipt back on read as %q", withReceipt.ReceiptURL)

	if err := repo.UpdateExpense(ctx, &withReceipt); err != nil {
		t.Fatalf("UpdateExpense() re-saving the read-back expense: %v", err)
	}
	afterEdit := liveReadByID(t, repo, itemID)
	if afterEdit.ReceiptURL == "" {
		t.Error("the receipt did not survive an edit round trip; R5's preserve clause is broken against the live server")
	}

	if err := repo.DeleteExpense(ctx, itemID); err != nil {
		t.Fatalf("DeleteExpense() error = %v", err)
	}
	for _, read := range func() []domain.Expense {
		expenses, err := repo.QueryExpenses(ctx)
		if err != nil {
			t.Fatalf("post-delete QueryExpenses() error = %v", err)
		}
		return expenses
	}() {
		if read.ID == itemID {
			t.Errorf("budget item %s is still listed after DeleteExpense()", itemID)
		}
	}

	if err := client.do(ctx, "DELETE", tripPathPrefix+"/"+strconv.FormatInt(trip.ID, 10)+"/files/"+fileID, nil, nil); err != nil {
		t.Logf("soft-deleting receipt file %s failed (leave it in TREK's trash or remove it by hand): %v", fileID, err)
	} else if err := client.do(ctx, "DELETE", tripPathPrefix+"/"+strconv.FormatInt(trip.ID, 10)+"/files/"+fileID+"/permanent", nil, nil); err != nil {
		t.Logf("permanently deleting receipt file %s failed (file remains in the trash): %v", fileID, err)
	} else {
		t.Logf("receipt file %s trashed and removed", fileID)
	}
}

type liveExpectation struct {
	name     string
	price    uint64
	currency domain.Currency
	rate     decimal.Decimal
	method   domain.PaymentMethod
}

func liveAssertExpense(t *testing.T, exp domain.Expense, want liveExpectation) {
	t.Helper()
	if exp.Name != want.name {
		t.Errorf("Name = %q, want %q", exp.Name, want.name)
	}
	if exp.Price != want.price {
		t.Errorf("Price = %d, want %d", exp.Price, want.price)
	}
	if exp.Currency != want.currency {
		t.Errorf("Currency = %q, want %q", exp.Currency, want.currency)
	}
	if drift := exp.ExchangeRate.Sub(want.rate).Abs(); drift.GreaterThan(decimal.NewFromFloat(0.0001)) {
		t.Errorf("ExchangeRate = %s, want ≈%s (drift %s)", exp.ExchangeRate, want.rate, drift)
	}
	if exp.Method != want.method {
		t.Errorf("Method = %q, want %q", exp.Method, want.method)
	}
	if exp.ShoppedAt.IsZero() {
		t.Error("ShoppedAt is the zero time, want the day it was written")
	}
}

func liveReadByID(t *testing.T, repo *tripRepo, itemID string) domain.Expense {
	t.Helper()

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}
	for _, exp := range expenses {
		if exp.ID == itemID {
			return exp
		}
	}
	t.Fatalf("budget item %s is not in the trip's listing (read %d expenses)", itemID, len(expenses))
	return domain.Expense{}
}

func liveRawItem(t *testing.T, client *Client, tripID int64, itemID string) string {
	t.Helper()

	var resp struct {
		Items []json.RawMessage `json:"items"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := client.do(ctx, "GET", tripPath(tripID, budgetPathSuffix), nil, &resp); err != nil {
		t.Fatalf("reading the raw budget listing: %v", err)
	}
	for _, item := range resp.Items {
		var id struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(item, &id); err == nil && strconv.FormatInt(id.ID, 10) == itemID {
			return string(item)
		}
	}
	t.Fatalf("budget item %s not found in the raw listing", itemID)
	return ""
}

func liveTinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR length + type
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, // bit depth 8, RGBA, CRC start
		0x89, 0x00, 0x00, 0x00, 0x00, 0x49, 0x44, 0x41, // IDAT
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x49,
		0x44, 0x41, 0x54, 0x78, 0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82, // IEND
	}
}
