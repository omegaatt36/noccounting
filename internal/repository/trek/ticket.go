package trek

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

const legacyTicketPrefix = "TICKETJSON:"

// TREK ticket prices use decimal strings; name_zh preserves an optional translation.
type ticketLine struct {
	NameZH string  `json:"name_zh,omitempty"`
	Name   string  `json:"name"`
	Price  string  `json:"price"`
	Parts  []int64 `json:"parts"`
}

type ticketReceipt struct {
	Items []ticketLine `json:"items"`
}

func (r *tripRepo) ticketJSON(e *domain.Expense, participants []int64) (string, error) {
	if e == nil || len(e.ReceiptItems) == 0 {
		return "", nil
	}

	perUnit, known := minorUnitsFor(e.Currency)
	if !known {
		return "", fmt.Errorf("%w: a receipt line of %s cannot be written in major units, because noccounting has no minor-unit table entry for %s",
			ErrInvalidExpense, e.Currency, e.Currency)
	}

	parts := slices.Clone(participants)
	if parts == nil {
		parts = []int64{}
	}

	items := make([]ticketLine, 0, len(e.ReceiptItems))
	var refused []string
	for _, line := range e.ReceiptItems {
		if line.Price <= 0 || line.Price > int64(maxMinorAmount) {
			refused = append(refused, line.Name)
			continue
		}
		items = append(items, ticketLine{
			Name:   line.Name,
			NameZH: line.NameZH,
			Price:  decimal.NewFromInt(line.Price).Div(decimal.NewFromInt32(perUnit)).String(),
			Parts:  parts,
		})
	}
	if len(refused) > 0 {
		slog.Warn("receipt lines noccounting cannot store were left off the item's receipt",
			"trip_id", r.tripID, "name", e.Name, "currency", e.Currency, "lines", refused)
	}

	if len(items) == 0 {
		return "", nil
	}

	stored, err := json.Marshal(ticketReceipt{Items: items})
	if err != nil {
		return "", fmt.Errorf("writing the receipt lines of %q: %w", e.Name, err)
	}
	return string(stored), nil
}

func decodeTicketJSON(stored string, currency domain.Currency) []domain.ReceiptItem {
	if strings.TrimSpace(stored) == "" {
		return nil
	}

	var receipt ticketReceipt
	if err := json.Unmarshal([]byte(stored), &receipt); err != nil {
		return nil
	}
	if len(receipt.Items) == 0 {
		return nil
	}

	lines := make([]domain.ReceiptItem, 0, len(receipt.Items))
	for _, item := range receipt.Items {
		amount, err := strconv.ParseFloat(item.Price, 64)
		if err != nil {
			return nil
		}
		price, err := minorUnits(amount, currency)
		if err != nil {
			return nil
		}
		lines = append(lines, domain.ReceiptItem{Name: item.Name, NameZH: item.NameZH, Price: int64(price)})
	}
	return lines
}
