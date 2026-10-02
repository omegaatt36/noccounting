package trek_test

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func lunchLines() []domain.ReceiptItem {
	return []domain.ReceiptItem{
		{Name: "拉麵", NameZH: "拉面", Price: 900, Category: domain.CategoryFood},
		{Name: "滷蛋", Price: 100, Category: domain.CategoryFood},
		{Name: "飲料", Price: 200, Category: domain.CategoryFood},
	}
}

const lunchTicket = `{"items":[{"name_zh":"拉面","name":"拉麵","price":"900","parts":[8]},{"name":"滷蛋","price":"100","parts":[8]},{"name":"飲料","price":"200","parts":[8]}]}`

func wantLines(t *testing.T, got, want []domain.ReceiptItem) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("read %d receipt lines %v, want the %d that were written", len(got), got, len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Errorf("line %d name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].Price != want[i].Price {
			t.Errorf("line %d price = %d, want %d — a line is priced in the smallest unit on both sides",
				i, got[i].Price, want[i].Price)
		}
	}
}

func ticketTotal(t *testing.T, stored any) float64 {
	t.Helper()

	raw, isString := stored.(string)
	if !isString {
		t.Fatalf("ticket_json is %T, want a string", stored)
	}
	var receipt struct {
		Items []struct {
			Price string `json:"price"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatalf("ticket_json is not a receipt: %v (%s)", err, raw)
	}

	total := 0.0
	for _, line := range receipt.Items {
		price, err := strconv.ParseFloat(line.Price, 64)
		if err != nil {
			t.Fatalf("line price %q is not a number: %v", line.Price, err)
		}
		total += price
	}
	return total
}

func TestCreateExpense_StoresTheReceiptLinesInTicketJSON(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) { e.ReceiptItems = lunchLines() })

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v", err)
	}

	call := oneCreate(t, stub)
	if got := body(t, call)["ticket_json"]; got != lunchTicket {
		t.Errorf("create body ticket_json = %#v, want %s", got, lunchTicket)
	}
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
		"ticket_json":   lunchTicket,
	})
}

func TestCreateExpense_LeavesTicketJSONOffForAnExpenseWithNoLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []domain.ReceiptItem
	}{
		{name: "no receipt was ever analyzed", lines: nil},
		{name: "the analysis found no line", lines: []domain.ReceiptItem{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := withExpense(func(e *domain.Expense) { e.ReceiptItems = tt.lines })

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v, want the expense written without a receipt", err)
			}

			fields := body(t, oneCreate(t, stub))
			if _, present := fields["ticket_json"]; present {
				t.Errorf("create body carries ticket_json = %#v, want the key left out", fields["ticket_json"])
			}
			wantBody(t, fields, map[string]any{
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
		})
	}
}

func TestCreateExpense_GivesEveryLineTheExpensesParticipants(t *testing.T) {
	tests := []struct {
		name         string
		participants []string
		wantParts    string
		wantMembers  []any
	}{
		{name: "the people named are in on every line", participants: []string{"8"}, wantParts: "[8]", wantMembers: []any{float64(8)}},
		{name: "empty participants remain empty", participants: nil, wantParts: "[]", wantMembers: []any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := withExpense(func(e *domain.Expense) {
				e.ReceiptItems = []domain.ReceiptItem{{Name: "滷蛋", Price: 100}}
				e.ParticipantIDs = tt.participants
			})

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v", err)
			}

			fields := body(t, oneCreate(t, stub))
			want := `{"items":[{"name":"滷蛋","price":"100","parts":` + tt.wantParts + `}]}`
			if got := fields["ticket_json"]; got != want {
				t.Errorf("create body ticket_json = %#v, want %s", got, want)
			}
			if got, _ := fields["member_ids"].([]any); !reflect.DeepEqual(got, tt.wantMembers) {
				t.Errorf("create body member_ids = %#v, want %#v, the ids the lines carry", fields["member_ids"], tt.wantMembers)
			}
		})
	}
}

func TestCreateExpense_PricesEveryLineTheSameWayAsTheTotal(t *testing.T) {
	tests := []struct {
		name     string
		currency domain.Currency
		price    uint64
		lines    []domain.ReceiptItem
	}{
		{name: "TWD", currency: domain.CurrencyTWD, price: 1200, lines: lunchLines()},
		{
			name:     "JPY",
			currency: domain.CurrencyJPY,
			price:    1480,
			lines:    []domain.ReceiptItem{{Name: "拉麺", Price: 980}, {Name: "ビール", Price: 500}},
		},
		{
			name:     "one line that is the whole expense",
			currency: domain.CurrencyTWD,
			price:    1200,
			lines:    []domain.ReceiptItem{{Name: "午飯", Price: 1200}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, jpyQuote())
			expense := withExpense(func(e *domain.Expense) {
				e.Currency = tt.currency
				e.Price = tt.price
				e.ReceiptItems = tt.lines
			})

			if err := repo.CreateExpense(context.Background(), expense); err != nil {
				t.Fatalf("CreateExpense() error = %v", err)
			}

			fields := body(t, oneCreate(t, stub))
			if got, want := ticketTotal(t, fields["ticket_json"]), fields["total_price"]; got != want {
				t.Errorf("the lines add up to %v but the expense was written at %v — a line priced in anything but the total's own units is a receipt nobody can add up", got, want)
			}
		})
	}
}

func TestCreateExpense_ReportsALineItCouldNotStore(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := withExpense(func(e *domain.Expense) {
		e.ReceiptItems = []domain.ReceiptItem{{Name: "免費小菜", Price: 0}, {Name: "拉麵", Price: 1200}}
	})
	warnings := captureWarnings(t)

	if err := repo.CreateExpense(context.Background(), expense); err != nil {
		t.Fatalf("CreateExpense() error = %v, want the expense written with the one line left out", err)
	}

	if got, want := body(t, oneCreate(t, stub))["ticket_json"], `{"items":[{"name":"拉麵","price":"1200","parts":[8]}]}`; got != want {
		t.Errorf("create body ticket_json = %#v, want %s", got, want)
	}
	if !strings.Contains(warnings.String(), "免費小菜") {
		t.Errorf("a receipt line was dropped without a word in the log: %s", warnings)
	}
}

func TestUpdateExpense_WritesTheLinesAndTheRowKeepsThem(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := edited(func(e *domain.Expense) { e.ReceiptItems = lunchLines() })

	if err := repo.UpdateExpense(context.Background(), expense); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	if got := body(t, oneUpdate(t, stub))["ticket_json"]; got != lunchTicket {
		t.Errorf("update body ticket_json = %#v, want %s", got, lunchTicket)
	}
	row := stub.rowSeen(t, 42)
	if row.TicketJSON == nil {
		t.Fatal("the row holds no receipt after an update that carried one")
	}
	if *row.TicketJSON != lunchTicket {
		t.Errorf("the stored receipt = %s, want %s", *row.TicketJSON, lunchTicket)
	}
}

func TestUpdateExpense_LeavesTheStoredLinesAloneWhenTheExpenseCarriesNone(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash", "ticket_json": lunchTicket})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := edited(func(e *domain.Expense) { e.Name = "牛肉麵" })

	if err := repo.UpdateExpense(context.Background(), expense); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	if _, present := body(t, oneUpdate(t, stub))["ticket_json"]; present {
		t.Error("the update carried a ticket_json, so TREK would have replaced the stored receipt with it")
	}
	row := stub.rowSeen(t, 42)
	if row.TicketJSON == nil || *row.TicketJSON != lunchTicket {
		t.Errorf("the stored receipt = %v, want the %s the row held before the edit", row.TicketJSON, lunchTicket)
	}
}

func TestUpdateExpense_ReplacesTheStoredLinesWithTheOnesItCarries(t *testing.T) {
	const stored = `{"items":[{"name":"昨天的","price":"999","parts":[8]}]}`
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash", "ticket_json": stored})
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	expense := edited(func(e *domain.Expense) { e.ReceiptItems = lunchLines() })

	if err := repo.UpdateExpense(context.Background(), expense); err != nil {
		t.Fatalf("UpdateExpense() error = %v", err)
	}

	row := stub.rowSeen(t, 42)
	if row.TicketJSON == nil || *row.TicketJSON != lunchTicket {
		t.Errorf("the stored receipt = %v, want %s — an update writes the lines the expense carries", row.TicketJSON, lunchTicket)
	}
}

func TestUpdateExpense_NeverSendsANoteTREKWouldReadAsItsOwnReceipt(t *testing.T) {
	const legacy = `TICKETJSON:{"items":[{"name":"舊的","price":"1200","parts":[8]}]}`

	tests := []struct {
		name   string
		method domain.PaymentMethod
		want   string
	}{
		{name: "a method this encoding writes", method: domain.PaymentMethodCreditCard, want: "PAYMENT:credit_card"},
		{name: "no method to write", method: "", want: ""},
		{name: "a method from a newer version", method: "crypto", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.listing = listingOf(t, map[string]any{"id": 42, "note": legacy})
			repo := stub.repo(t, domain.CurrencyTWD, nil)
			expense := edited(func(e *domain.Expense) {
				e.Method = tt.method
				e.ReceiptItems = lunchLines()
			})

			if err := repo.UpdateExpense(context.Background(), expense); err != nil {
				t.Fatalf("UpdateExpense() error = %v", err)
			}

			sent, _ := body(t, oneUpdate(t, stub))["note"].(string)
			if strings.HasPrefix(sent, "TICKETJSON:") {
				t.Fatalf("update body note = %q, which TREK would read as its own receipt payload and move into ticket_json", sent)
			}
			if sent != tt.want {
				t.Errorf("update body note = %q, want %q", sent, tt.want)
			}
			row := stub.rowSeen(t, 42)
			if row.TicketJSON == nil || *row.TicketJSON != lunchTicket {
				t.Errorf("the stored receipt = %v, want the %s this write sent", row.TicketJSON, lunchTicket)
			}
		})
	}
}

func TestQueryExpenses_ReadsTheReceiptLinesOffTheColumn(t *testing.T) {
	stub := newCreateStub(t)
	stub.listing = listingOf(t, map[string]any{"id": 42, "ticket_json": lunchTicket})
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	expenses, err := repo.QueryExpenses(context.Background())
	if err != nil {
		t.Fatalf("QueryExpenses() error = %v", err)
	}

	got := oneExpense(t, expenses)
	wantLines(t, got.ReceiptItems, lunchLines())
	if got.ReceiptItems[0].Category != "" {
		t.Errorf("line category = %q, want none — a TREK receipt line has no category field to read one from",
			got.ReceiptItems[0].Category)
	}
}

func TestQueryExpenses_TreatsAnUnreadableReceiptAsAnExpenseWithNoLines(t *testing.T) {
	tests := []struct {
		name   string
		ticket map[string]any
	}{
		{name: "the column is not there at all", ticket: nil},
		{name: "the column is null", ticket: map[string]any{"ticket_json": nil}},
		{name: "the column is empty", ticket: map[string]any{"ticket_json": ""}},
		{name: "the column is whitespace", ticket: map[string]any{"ticket_json": " "}},
		{name: "the column is malformed", ticket: map[string]any{"ticket_json": "{oops"}},
		{name: "the column is not a receipt", ticket: map[string]any{"ticket_json": "[1,2,3]"}},
		{name: "the receipt has no line", ticket: map[string]any{"ticket_json": `{"items":[]}`}},
		{name: "items is not a list of lines", ticket: map[string]any{"ticket_json": `{"items":{"a":1}}`}},
		{name: "a line has a price nothing can read", ticket: map[string]any{"ticket_json": `{"items":[{"name":"a","price":"free","parts":[8]}]}`}},
		{name: "a line is a free item", ticket: map[string]any{"ticket_json": `{"items":[{"name":"a","price":"0","parts":[8]}]}`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			item := map[string]any{"id": 42}
			maps.Copy(item, tt.ticket)
			stub.listing = listingOf(t, item)
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			expenses, err := repo.QueryExpenses(context.Background())
			if err != nil {
				t.Fatalf("QueryExpenses() error = %v, want the expense rather than the whole listing failing", err)
			}

			got := oneExpense(t, expenses)
			if len(got.ReceiptItems) != 0 {
				t.Errorf("ReceiptItems = %v, want none", got.ReceiptItems)
			}
			if got.Name != "拉麵" || got.Price != 1200 || got.PaidByID != "8" {
				t.Errorf("the expense = %+v, want it read in full — a receipt nothing can read is not a reason to lose the expense", got)
			}
		})
	}
}

func TestTicketJSON_RoundTripsThroughAWriteAndARead(t *testing.T) {
	t.Run("the document a create stored reads back", func(t *testing.T) {
		stub := newCreateStub(t)
		repo := stub.repo(t, domain.CurrencyTWD, nil)
		expense := withExpense(func(e *domain.Expense) { e.ReceiptItems = lunchLines() })

		if err := repo.CreateExpense(context.Background(), expense); err != nil {
			t.Fatalf("CreateExpense() error = %v", err)
		}
		written := body(t, oneCreate(t, stub))["ticket_json"]

		stub.listing = listingOf(t, map[string]any{"id": 42, "ticket_json": written})
		read := stub.repo(t, domain.CurrencyTWD, nil)

		expenses, err := read.QueryExpenses(context.Background())
		if err != nil {
			t.Fatalf("QueryExpenses() error = %v", err)
		}
		wantLines(t, oneExpense(t, expenses).ReceiptItems, lunchLines())
	})

	t.Run("the row an update stored reads back", func(t *testing.T) {
		stub := newCreateStub(t)
		stub.listing = listingOf(t, map[string]any{"id": 42, "note": "PAYMENT:cash"})
		repo := stub.repo(t, domain.CurrencyTWD, nil)
		expense := edited(func(e *domain.Expense) { e.ReceiptItems = lunchLines() })

		if err := repo.UpdateExpense(context.Background(), expense); err != nil {
			t.Fatalf("UpdateExpense() error = %v", err)
		}

		read := stub.repo(t, domain.CurrencyTWD, nil)

		expenses, err := read.QueryExpenses(context.Background())
		if err != nil {
			t.Fatalf("QueryExpenses() error = %v", err)
		}
		wantLines(t, oneExpense(t, expenses).ReceiptItems, lunchLines())
	})
}
