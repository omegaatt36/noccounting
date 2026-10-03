package trek

import (
	"errors"
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func bob() *Payer { return &Payer{UserID: 8, Nickname: "bob"} }

func bobAlone() []int64 { return []int64{bob().UserID} }

func lunch() *domain.Expense {
	return &domain.Expense{
		Name:     "午餐",
		Price:    1200,
		Currency: domain.CurrencyTWD,
		ReceiptItems: []domain.ReceiptItem{
			{Name: "拉麵", NameZH: "拉面", Price: 900, Category: domain.CategoryFood},
			{Name: "滷蛋", Price: 100, Category: domain.CategoryFood},
			{Name: "飲料", Price: 200, Category: domain.CategoryFood},
		},
	}
}

const lunchTicket = `{"items":[` +
	`{"name_zh":"拉面","name":"拉麵","price":"900","parts":[8]},` +
	`{"name":"滷蛋","price":"100","parts":[8]},` +
	`{"name":"飲料","price":"200","parts":[8]}]}`

func TestTicketJSON_WritesTheLinesInTheShapeTREKReads(t *testing.T) {
	repo := &tripRepo{client: NewClient(Config{}), tripID: 3}

	stored, err := repo.ticketJSON(lunch(), bobAlone())
	if err != nil {
		t.Fatalf("ticketJSON() error = %v", err)
	}
	if stored != lunchTicket {
		t.Errorf("ticketJSON() = %s\nwant %s", stored, lunchTicket)
	}
}

func TestTicketJSON_ScalesALineByTheSameTableAsTheTotal(t *testing.T) {
	tests := []struct {
		name     string
		currency domain.Currency
		price    int64
		want     string
	}{
		{name: "TWD has no minor unit", currency: domain.CurrencyTWD, price: 1200, want: `{"items":[{"name":"午餐","price":"1200","parts":[]}]}`},
		{name: "JPY has no minor unit", currency: domain.CurrencyJPY, price: 1200, want: `{"items":[{"name":"午餐","price":"1200","parts":[]}]}`},
		{name: "a code in another case is the same currency", currency: domain.Currency("twd"), price: 1200, want: `{"items":[{"name":"午餐","price":"1200","parts":[]}]}`},
		{name: "a single unit", currency: domain.CurrencyTWD, price: 1, want: `{"items":[{"name":"午餐","price":"1","parts":[]}]}`},
		{name: "a large amount is not rescaled", currency: domain.CurrencyTWD, price: 1234567, want: `{"items":[{"name":"午餐","price":"1234567","parts":[]}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tripRepo{client: NewClient(Config{}), tripID: 3}
			expense := &domain.Expense{
				Name:         "午餐",
				Price:        uint64(tt.price),
				Currency:     tt.currency,
				ReceiptItems: []domain.ReceiptItem{{Name: "午餐", Price: tt.price}},
			}

			stored, err := repo.ticketJSON(expense, nil)
			if err != nil {
				t.Fatalf("ticketJSON() error = %v", err)
			}
			if stored != tt.want {
				t.Errorf("ticketJSON() = %s, want %s", stored, tt.want)
			}
		})
	}
}

func TestTicketJSON_LeavesTheColumnAloneForAnExpenseWithNoLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []domain.ReceiptItem
	}{
		{name: "no lines at all", lines: nil},
		{name: "an empty slice of lines", lines: []domain.ReceiptItem{}},
		{
			name:  "no line worth storing",
			lines: []domain.ReceiptItem{{Name: "免費小菜", Price: 0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tripRepo{client: NewClient(Config{}), tripID: 3}
			expense := &domain.Expense{Name: "午餐", Currency: domain.CurrencyTWD, ReceiptItems: tt.lines}

			stored, err := repo.ticketJSON(expense, bobAlone())
			if err != nil {
				t.Fatalf("ticketJSON() error = %v", err)
			}
			if stored != "" {
				t.Errorf("ticketJSON() = %s, want no value at all so the key is left off the wire", stored)
			}
		})
	}
}

func TestTicketJSON_LeavesOutALineTheReadPathCouldNotGiveBack(t *testing.T) {
	tests := []struct {
		name string
		line domain.ReceiptItem
	}{
		{name: "a free item", line: domain.ReceiptItem{Name: "免費小菜", Price: 0}},
		{name: "a negative price", line: domain.ReceiptItem{Name: "折扣", Price: -50}},
		{name: "an amount the domain cannot hold", line: domain.ReceiptItem{Name: "鑽石", Price: 1<<53 + 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tripRepo{client: NewClient(Config{}), tripID: 3}
			expense := &domain.Expense{
				Name:         "午餐",
				Currency:     domain.CurrencyTWD,
				ReceiptItems: []domain.ReceiptItem{tt.line, {Name: "拉麵", Price: 1200}},
			}

			stored, err := repo.ticketJSON(expense, bobAlone())
			if err != nil {
				t.Fatalf("ticketJSON() error = %v", err)
			}
			if want := `{"items":[{"name":"拉麵","price":"1200","parts":[8]}]}`; stored != want {
				t.Errorf("ticketJSON() = %s, want %s — the unreadable line is left out and the rest is stored", stored, want)
			}
		})
	}
}

func TestTicketJSON_RefusesACurrencyItCannotScale(t *testing.T) {
	repo := &tripRepo{client: NewClient(Config{}), tripID: 3}
	expense := &domain.Expense{
		Name:         "午餐",
		Price:        1200,
		Currency:     domain.Currency("USD"),
		ReceiptItems: []domain.ReceiptItem{{Name: "拉麵", Price: 1200}},
	}

	_, err := repo.ticketJSON(expense, bobAlone())
	if !errors.Is(err, ErrInvalidExpense) {
		t.Fatalf("ticketJSON() error = %v, want ErrInvalidExpense", err)
	}
	if !strings.Contains(err.Error(), "USD") {
		t.Errorf("ticketJSON() error = %v, want it to name the currency it cannot scale", err)
	}
}

func TestDecodeTicketJSON_ReadsTheTranslationBackAsPartOfTheName(t *testing.T) {
	lines := decodeTicketJSON(`{"items":[{"name":"Coffee（咖啡）","price":"120","parts":[8]}]}`, domain.CurrencyTWD)

	if len(lines) != 1 {
		t.Fatalf("decodeTicketJSON() read %d lines, want 1", len(lines))
	}
	if got, want := lines[0].Name, "Coffee（咖啡）"; got != want {
		t.Errorf("DisplayName() = %q, want %q — the name survives the round trip as it is displayed", got, want)
	}
	if lines[0].NameZH != "" {
		t.Errorf("NameZH = %q, want it empty: the wire has one name field and the split cannot be undone", lines[0].NameZH)
	}
}

func TestDecodeTicketJSON_ScalesAStoredPriceBackIntoTheSmallestUnit(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		currency domain.Currency
		want     int64
	}{
		{name: "TWD", stored: `{"items":[{"name":"a","price":"1200","parts":[]}]}`, currency: domain.CurrencyTWD, want: 1200},
		{name: "JPY", stored: `{"items":[{"name":"a","price":"1000","parts":[]}]}`, currency: domain.CurrencyJPY, want: 1000},
		{name: "a code in another case", stored: `{"items":[{"name":"a","price":"7","parts":[]}]}`, currency: domain.Currency("twd"), want: 7},
		{name: "a price a line carries as a fraction TREK cannot have stored", stored: `{"items":[{"name":"a","price":"12.4","parts":[]}]}`, currency: domain.CurrencyTWD, want: 12},
		{name: "a price that is not a number", stored: `{"items":[{"name":"a","price":"twelve","parts":[]}]}`, currency: domain.CurrencyTWD, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := decodeTicketJSON(tt.stored, tt.currency)
			if tt.want == 0 {
				if len(lines) != 0 {
					t.Fatalf("decodeTicketJSON(%s) = %v, want no lines at all", tt.stored, lines)
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("decodeTicketJSON(%s) read %d lines, want 1", tt.stored, len(lines))
			}
			if lines[0].Price != tt.want {
				t.Errorf("Price = %d, want %d", lines[0].Price, tt.want)
			}
		})
	}
}

func TestDecodeTicketJSON_ReportsAnythingItCannotReadAsNoLines(t *testing.T) {
	tests := []struct {
		name   string
		stored string
	}{
		{name: "no column at all", stored: ""},
		{name: "whitespace", stored: "   "},
		{name: "malformed", stored: `{oops`},
		{name: "truncated", stored: `{"items":[{"name":"a"`},
		{name: "not an object", stored: `[{"name":"a","price":"1"}]`},
		{name: "an object that is not a receipt", stored: `{"note":"team dinner"}`},
		{name: "items that are not lines", stored: `{"items":{"a":1}}`},
		{name: "a receipt nobody itemized", stored: `{"items":[]}`},
		{name: "items that are null", stored: `{"items":null}`},
		{name: "a line with no price", stored: `{"items":[{"name":"a","parts":[8]}]}`},
		{name: "a price that is not a number", stored: `{"items":[{"name":"a","price":"twelve","parts":[8]}]}`},
		{name: "a free item", stored: `{"items":[{"name":"a","price":"0","parts":[8]}]}`},
		{name: "an amount the domain cannot hold", stored: `{"items":[{"name":"a","price":"1e19","parts":[8]}]}`},
		{name: "one unreadable line among readable ones", stored: `{"items":[{"name":"a","price":"10","parts":[8]},{"name":"b","price":"free","parts":[8]}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if lines := decodeTicketJSON(tt.stored, domain.CurrencyTWD); len(lines) != 0 {
				t.Errorf("decodeTicketJSON(%q) = %v, want no lines at all", tt.stored, lines)
			}
		})
	}
}

func TestDecodeTicketJSON_ReadsWhatTREKsOwnCostsPanelWrote(t *testing.T) {
	const stored = `{"items":[{"name":"Bread","price":"12","parts":[1,2]},{"name":"Wine","price":"7","parts":[2]}]}`

	lines := decodeTicketJSON(stored, domain.CurrencyTWD)
	if len(lines) != 2 {
		t.Fatalf("decodeTicketJSON() read %d lines, want 2", len(lines))
	}
	if lines[0].Name != "Bread" || lines[0].Price != 12 {
		t.Errorf("first line = %+v, want Bread at 12", lines[0])
	}
	if lines[1].Name != "Wine" || lines[1].Price != 7 {
		t.Errorf("second line = %+v, want Wine at 7", lines[1])
	}
}

func TestTicketJSON_RoundTripsThroughTheDomain(t *testing.T) {
	tests := []struct {
		name     string
		currency domain.Currency
		expense  *domain.Expense
	}{
		{name: "a receipt with a translation on one line", currency: domain.CurrencyTWD, expense: lunch()},
		{
			name:     "a single line",
			currency: domain.CurrencyTWD,
			expense: &domain.Expense{Currency: domain.CurrencyTWD, ReceiptItems: []domain.ReceiptItem{
				{Name: "Coffee", NameZH: "咖啡", Price: 120, Category: domain.CategoryShopping},
			}},
		},
		{
			name:     "a foreign expense",
			currency: domain.CurrencyJPY,
			expense: &domain.Expense{Currency: domain.CurrencyJPY, ReceiptItems: []domain.ReceiptItem{
				{Name: "ラーメン", Price: 980, Category: domain.CategoryFood},
				{Name: "ビール", Price: 500, Category: domain.CategoryOther},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tripRepo{client: NewClient(Config{}), tripID: 3}

			stored, err := repo.ticketJSON(tt.expense, bobAlone())
			if err != nil {
				t.Fatalf("ticketJSON() error = %v", err)
			}
			read := decodeTicketJSON(stored, tt.currency)

			if len(read) != len(tt.expense.ReceiptItems) {
				t.Fatalf("read back %d lines, want the %d that were written", len(read), len(tt.expense.ReceiptItems))
			}
			for i, line := range read {
				written := tt.expense.ReceiptItems[i]
				if line.NameZH != written.NameZH {
					t.Errorf("line %d translation = %q, want %q", i, line.NameZH, written.NameZH)
				}
				if line.Name != written.Name {
					t.Errorf("line %d name = %q, want %q", i, line.Name, written.Name)
				}
				if line.Price != written.Price {
					t.Errorf("line %d price = %d, want %d — the line is priced in the smallest unit on both sides",
						i, line.Price, written.Price)
				}
			}
			again, err := repo.ticketJSON(&domain.Expense{Currency: tt.currency, ReceiptItems: read}, bobAlone())
			if err != nil {
				t.Fatalf("ticketJSON() of the read expense error = %v", err)
			}
			if again != stored {
				t.Errorf("writing the lines back gives %s, want the document that was stored: %s", again, stored)
			}
		})
	}
}
