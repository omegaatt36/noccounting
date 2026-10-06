package trek

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

const budgetPathSuffix = "/budget"

var ErrInvalidExpense = errors.New("the expense is not one TREK can store as given")

type tripRepo struct {
	client       *Client
	tripID       int64
	baseCurrency domain.Currency
	payers       *PayerResolver
	rates        rateProvider
	budgetCache  *budgetCache
}

func newTripRepo(client *Client, trip domain.Trip, payers *PayerResolver, rates rateProvider, cache ...*budgetCache) *tripRepo {
	var bCache *budgetCache
	if len(cache) > 0 {
		bCache = cache[0]
	}
	return &tripRepo{
		client:       client,
		tripID:       trip.ID,
		baseCurrency: trip.Currency,
		payers:       payers,
		rates:        rates,
		budgetCache:  bCache,
	}
}

type budgetCache struct {
	mu        sync.Mutex
	items     []budgetItem
	fetchedAt time.Time
	ttl       time.Duration
}

func newBudgetCache(ttl time.Duration) *budgetCache {
	return &budgetCache{ttl: ttl}
}

func (c *budgetCache) get(ctx context.Context, client *Client, tripID int64) ([]budgetItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.items != nil && time.Since(c.fetchedAt) < c.ttl {
		return c.items, nil
	}

	var resp budgetItemsResponse
	if err := client.do(ctx, http.MethodGet, tripPath(tripID, budgetPathSuffix), nil, &resp); err != nil {
		return nil, err
	}
	c.items, c.fetchedAt = resp.Items, time.Now()
	return resp.Items, nil
}

func (c *budgetCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = nil
	c.fetchedAt = time.Time{}
}

func (r *tripRepo) CreateExpense(ctx context.Context, e *domain.Expense) error {
	body, err := r.newBudgetItem(ctx, e)
	if err != nil {
		return err
	}
	body.Note = encodeNote(e.Method, "")

	var created struct {
		Item struct {
			ID         int64               `json:"id"`
			TotalPrice float64             `json:"total_price"`
			Receipts   []budgetItemReceipt `json:"receipts"`
		} `json:"item"`
	}
	if err := r.client.do(ctx, http.MethodPost, tripPath(r.tripID, budgetPathSuffix), body, &created); err != nil {
		return fmt.Errorf("creating the TREK budget item for %q: %w", e.Name, err)
	}

	r.budgetCache.invalidate()

	if created.Item.ID > 0 {
		e.ID = strconv.FormatInt(created.Item.ID, 10)
	}

	if math.Abs(created.Item.TotalPrice-body.TotalPrice) > storedAmountTolerance {
		slog.Warn("TREK stored an amount other than the one noccounting sent; the expense exists without the total it was recorded with",
			"trip_id", r.tripID, "item_id", created.Item.ID, "name", e.Name,
			"sent", body.TotalPrice, "stored", created.Item.TotalPrice)
	}

	if unlinked := unlinkedReceipts(created.Item.Receipts, body.ReceiptFileIDs); len(unlinked) > 0 {
		slog.Warn("the expense was created but TREK did not link its receipt; the receipt image is on the trip and attached to nothing",
			"trip_id", r.tripID, "item_id", created.Item.ID, "name", e.Name, "file_ids", unlinked)
	}

	slog.Debug("created a TREK budget item",
		"trip_id", r.tripID, "item_id", created.Item.ID, "name", e.Name, "currency", e.Currency)
	return nil
}

func unlinkedReceipts(stored []budgetItemReceipt, wanted []int64) []int64 {
	if len(wanted) == 0 {
		return nil
	}

	linked := make(map[int64]bool, len(stored))
	for _, receipt := range stored {
		linked[receipt.ID] = true
	}

	unlinked := make([]int64, 0, len(wanted))
	for _, id := range wanted {
		if !linked[id] {
			unlinked = append(unlinked, id)
		}
	}
	return unlinked
}

func (r *tripRepo) newBudgetItem(ctx context.Context, e *domain.Expense) (budgetItemRequest, error) {
	if err := validateExpense(e); err != nil {
		return budgetItemRequest{}, err
	}

	total, err := majorUnits(e.Price, e.Currency)
	if err != nil {
		return budgetItemRequest{}, err
	}

	rate, err := rateForWrite(ctx, e.Currency, r.baseCurrency, e.ExchangeRate, r.rates)
	if err != nil {
		return budgetItemRequest{}, fmt.Errorf("the rate to store for the %s expense %q: %w", e.Currency, e.Name, err)
	}

	payer, err := r.payerFor(ctx, e)
	if err != nil {
		return budgetItemRequest{}, err
	}
	payers, err := PayerInput(payer, total)
	if err != nil {
		return budgetItemRequest{}, err
	}

	participants, err := r.participantsFor(ctx, e)
	if err != nil {
		return budgetItemRequest{}, err
	}

	ticket, err := r.ticketJSON(e, participants)
	if err != nil {
		return budgetItemRequest{}, err
	}

	return budgetItemRequest{
		Name:           strings.TrimSpace(e.Name),
		Category:       string(e.Category),
		Currency:       e.Currency,
		TotalPrice:     total,
		ExchangeRate:   rate,
		ExpenseDate:    e.ShoppedAt.Format(time.DateOnly),
		Payers:         payersKey(payers),
		MemberIDs:      &participants,
		ReceiptFileIDs: receiptFileIDs(e),
		TicketJSON:     ticket,
	}, nil
}

func receiptFileIDs(e *domain.Expense) []int64 {
	fileID, err := strconv.ParseInt(strings.TrimSpace(e.ReceiptURL), 10, 64)
	if err != nil || fileID <= 0 {
		slog.Debug("the expense names no receipt file id, so this write links no receipt",
			"name", e.Name, "receipt", e.ReceiptURL)
		return nil
	}
	return []int64{fileID}
}

func payersKey(payers []TrekPayer) *[]TrekPayer {
	if payers == nil {
		return nil
	}
	return &payers
}

func (r *tripRepo) payerFor(ctx context.Context, e *domain.Expense) (*Payer, error) {
	if strings.TrimSpace(e.PaidByID) == "" {
		return nil, nil
	}
	if r.payers == nil {
		return nil, fmt.Errorf("%w: %q names payer %q and no payer resolver is configured, so it would be stored as paid by nobody",
			ErrInvalidExpense, e.Name, e.PaidByID)
	}

	payer, err := r.payers.Resolve(ctx, PayerRef{ID: e.PaidByID})
	if err != nil {
		return nil, fmt.Errorf("the payer of %q: %w", e.Name, err)
	}
	return payer, nil
}

func (r *tripRepo) participantsFor(ctx context.Context, e *domain.Expense) ([]int64, error) {
	if r.payers == nil {
		return nil, fmt.Errorf("%w: %q is shared between the trip and no roster is configured to name who is on it",
			ErrInvalidExpense, e.Name)
	}

	participants, err := r.payers.Participants(ctx, e.ParticipantIDs)
	if err != nil {
		return nil, fmt.Errorf("who shares %q: %w", e.Name, err)
	}
	return participants, nil
}

// Omitted ticket_json preserves existing lines; omitted payers preserves attribution.
type budgetItemRequest struct {
	Name         string          `json:"name"`
	Category     string          `json:"category"`
	Currency     domain.Currency `json:"currency"`
	TotalPrice   float64         `json:"total_price"`
	ExchangeRate float64         `json:"exchange_rate"`
	Note         string          `json:"note"`
	ExpenseDate  string          `json:"expense_date"`
	Payers       *[]TrekPayer    `json:"payers,omitempty"`
	MemberIDs    *[]int64        `json:"member_ids,omitempty"`

	ReceiptFileIDs []int64 `json:"receipt_file_ids,omitempty"`
	TicketJSON     string  `json:"ticket_json,omitempty"`
}

func validateExpense(e *domain.Expense) error {
	if e == nil {
		return fmt.Errorf("%w: there is no expense to write", ErrInvalidExpense)
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("%w: %q has no name, and TREK's create schema requires one", ErrInvalidExpense, e.Name)
	}
	if !e.Category.IsValid() {
		return fmt.Errorf("%w: %q has the category %q, which is not one of TREK's cost categories", ErrInvalidExpense, e.Name, e.Category)
	}
	if e.Price == 0 {
		return fmt.Errorf("%w: the amount of %q is 0, which TREK stores as an expense with no amount", ErrInvalidExpense, e.Name)
	}
	if e.ShoppedAt.IsZero() {
		return fmt.Errorf("%w: the date of %q is not set, and the zero time would be stored as 0001-01-01", ErrInvalidExpense, e.Name)
	}
	return nil
}

var minorUnitsPerUnit = map[domain.Currency]int32{
	domain.CurrencyTWD: 1,
	domain.CurrencyJPY: 1,
}

const maxMinorAmount = uint64(1) << 53

func minorUnitsFor(currency domain.Currency) (int32, bool) {
	perUnit, known := minorUnitsPerUnit[domain.Currency(strings.ToUpper(string(currency)))]
	return perUnit, known
}

func majorUnits(amount uint64, currency domain.Currency) (float64, error) {
	perUnit, known := minorUnitsFor(currency)
	if !known {
		return 0, fmt.Errorf("%w: %d %s cannot be written in major units, because noccounting has no minor-unit table entry for %s",
			ErrInvalidExpense, amount, currency, currency)
	}
	if amount > maxMinorAmount {
		return 0, fmt.Errorf("%w: %d %s does not fit a float64 exactly, so TREK would store a different amount", ErrInvalidExpense, amount, currency)
	}

	return decimal.NewFromUint64(amount).Div(decimal.NewFromInt32(perUnit)).InexactFloat64(), nil
}

func minorUnits(amount float64, currency domain.Currency) (uint64, error) {
	perUnit, known := minorUnitsFor(currency)
	if !known {
		return 0, fmt.Errorf("a stored amount of %v %s cannot be read in minor units, because noccounting has no minor-unit table entry for %s",
			amount, currency, currency)
	}
	if !isPositiveNumber(amount) {
		return 0, fmt.Errorf("the stored amount %v %s is not a positive amount of money", amount, currency)
	}

	minor := decimal.NewFromFloat(amount).Mul(decimal.NewFromInt32(perUnit)).Round(0)
	whole := minor.BigInt()
	if !whole.IsUint64() || whole.BitLen() > 63 {
		return 0, fmt.Errorf("the stored amount %v %s is larger than noccounting can hold", amount, currency)
	}
	return whole.Uint64(), nil
}

func (r *tripRepo) QueryExpenses(ctx context.Context) ([]domain.Expense, error) {
	return r.QueryExpensesWithFilter(ctx, expense.ExpenseFilter{})
}

func (r *tripRepo) QueryExpensesWithFilter(ctx context.Context, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	listed, err := r.listExpenses(ctx)
	if err != nil {
		return nil, err
	}
	sortExpenses(listed)
	return selectExpenses(listed, filter)
}

func (r *tripRepo) UpdateExpense(ctx context.Context, e *domain.Expense) error {
	if e == nil {
		return fmt.Errorf("%w: there is no expense to update", ErrInvalidExpense)
	}
	itemID, err := budgetItemID(e.ID)
	if err != nil {
		return err
	}

	body, err := r.newBudgetItem(ctx, e)
	if err != nil {
		return err
	}

	// TREK keeps absent payers; an empty array clears them.
	if body.Payers == nil {
		body.Payers = &[]TrekPayer{}
	}

	row, err := r.storedRowOf(ctx, itemID)
	if err != nil {
		return err
	}
	body.Note = encodeNote(e.Method, row.noteText)

	if len(row.payerIDs) > 1 {
		slog.Warn("the TREK budget item was paid by several users, and this update rewrites them with the single payer noccounting records; the split is replaced by the expense's full total",
			"trip_id", r.tripID, "item_id", itemID, "name", e.Name,
			"paid_by", e.PaidByID, "split_payers", row.payerIDs)
	}

	if row.customSplit {
		return fmt.Errorf("%w: edit this expense in TREK", domain.ErrUnsupportedSplit)
	}

	var updated struct {
		Item struct {
			TotalPrice float64 `json:"total_price"`
		} `json:"item"`
	}
	if err := r.client.do(ctx, http.MethodPut, r.budgetItemPath(itemID), body, &updated); err != nil {
		return fmt.Errorf("updating the TREK budget item %d for %q: %w", itemID, e.Name, err)
	}

	r.budgetCache.invalidate()

	if math.Abs(updated.Item.TotalPrice-body.TotalPrice) > storedAmountTolerance {
		slog.Warn("TREK stored an amount other than the one noccounting sent; the expense no longer has the total it was edited to",
			"trip_id", r.tripID, "item_id", itemID, "name", e.Name,
			"sent", body.TotalPrice, "stored", updated.Item.TotalPrice)
	}
	slog.Debug("updated a TREK budget item",
		"trip_id", r.tripID, "item_id", itemID, "name", e.Name, "currency", e.Currency)
	return nil
}

const storedAmountTolerance = 1e-6

type storedRow struct {
	noteText    string
	payerIDs    []int64
	customSplit bool
}

func (r *tripRepo) storedRowOf(ctx context.Context, itemID int64) (storedRow, error) {
	var items []budgetItem
	if r.budgetCache != nil {
		cached, err := r.budgetCache.get(ctx, r.client, r.tripID)
		if err != nil {
			return storedRow{}, fmt.Errorf("reading the TREK budget of trip %d to keep the note of item %d: %w", r.tripID, itemID, err)
		}
		items = cached
	} else {
		var resp budgetItemsResponse
		if err := r.client.do(ctx, http.MethodGet, tripPath(r.tripID, budgetPathSuffix), nil, &resp); err != nil {
			return storedRow{}, fmt.Errorf("reading the TREK budget of trip %d to keep the note of item %d: %w", r.tripID, itemID, err)
		}
		items = resp.Items
	}

	for _, item := range items {
		if item.ID == itemID {
			_, text := decodeNote(item.Note)
			if strings.HasPrefix(text, legacyTicketPrefix) {
				slog.Debug("the stored note carries TREK's own receipt payload, which is not text and is not written back as a note",
					"trip_id", r.tripID, "item_id", itemID)
				text = ""
			}
			return storedRow{noteText: text, payerIDs: item.payerIDs(), customSplit: item.hasCustomSplit()}, nil
		}
	}

	slog.Warn("the budget item an update names is not in the listing, so its note can only be written from the method",
		"trip_id", r.tripID, "item_id", itemID)
	return storedRow{}, nil
}

func (r *tripRepo) DeleteExpense(ctx context.Context, id string) error {
	itemID, err := budgetItemID(id)
	if err != nil {
		return err
	}

	var removed struct {
		Success bool `json:"success"`
	}
	if err := r.client.do(ctx, http.MethodDelete, r.budgetItemPath(itemID), nil, &removed); err != nil {
		return fmt.Errorf("deleting the TREK budget item %d: %w", itemID, err)
	}
	if !removed.Success {
		return fmt.Errorf("TREK answered the delete of its budget item %d without saying it removed it", itemID)
	}

	r.budgetCache.invalidate()

	slog.Info("deleted a TREK budget item, permanently", "trip_id", r.tripID, "item_id", itemID)
	return nil
}

func budgetItemID(id string) (int64, error) {
	raw := strings.TrimSpace(id)
	if raw == "" {
		return 0, fmt.Errorf("%w: there is no expense id, so there is no row to address", ErrInvalidExpense)
	}

	itemID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || itemID <= 0 {
		return 0, fmt.Errorf("%w: %q is not a TREK budget item id, which is a positive whole number", ErrInvalidExpense, id)
	}
	return itemID, nil
}

func (r *tripRepo) budgetItemPath(itemID int64) string {
	return tripPath(r.tripID, budgetPathSuffix+"/"+strconv.FormatInt(itemID, 10))
}

func (r *tripRepo) listExpenses(ctx context.Context) ([]readExpense, error) {
	if r.baseCurrency == "" {
		return nil, fmt.Errorf("reading the TREK budget of trip %d: %w", r.tripID, ErrUnknownTripCurrency)
	}

	var resp budgetItemsResponse
	if err := r.client.do(ctx, http.MethodGet, tripPath(r.tripID, budgetPathSuffix), nil, &resp); err != nil {
		return nil, fmt.Errorf("reading the TREK budget of trip %d: %w", r.tripID, err)
	}

	read := make([]readExpense, 0, len(resp.Items))
	for _, item := range resp.Items {
		decoded, err := r.expenseFrom(ctx, item)
		if err != nil {
			slog.Warn("skipping a TREK budget item that could not be read; it is missing from every total",
				"trip_id", r.tripID, "item_id", item.ID, "name", item.Name,
				"expense_date", item.ExpenseDate, "currency", item.Currency, "error", err)
			continue
		}
		read = append(read, decoded)
	}

	slog.Debug("read the TREK budget", "trip_id", r.tripID,
		"items", len(resp.Items), "readable", len(read))
	return read, nil
}

type readExpense struct {
	expense   domain.Expense
	createdAt time.Time
	itemID    int64
	payerID   int64
	payerRows []budgetItemPayer
}

func (r *tripRepo) expenseFrom(ctx context.Context, item budgetItem) (readExpense, error) {
	currency := item.Currency
	if strings.TrimSpace(currency) == "" {
		currency = string(r.baseCurrency)
	}

	price, err := minorUnits(item.TotalPrice, domain.Currency(currency))
	if err != nil {
		return readExpense{}, err
	}

	rate, err := rateForRead(ctx, item.ExchangeRate, domain.Currency(currency), r.baseCurrency, r.rates)
	if err != nil {
		return readExpense{}, err
	}

	shoppedAt, err := time.Parse(time.DateOnly, item.ExpenseDate)
	if err != nil {
		return readExpense{}, fmt.Errorf("reading the expense date %q of item %d: %w", item.ExpenseDate, item.ID, err)
	}

	method, text := decodeNote(item.Note)
	if text != "" {
		slog.Debug("a budget item carries free-form note text noccounting's domain has no field for",
			"trip_id", r.tripID, "item_id", item.ID, "text", text)
	}

	payer := item.payerID()
	lookup := item.Payers

	lines := decodeTicketJSON(item.TicketJSON, domain.Currency(currency))
	if len(lines) == 0 && strings.TrimSpace(item.TicketJSON) != "" {
		slog.Debug("a budget item carries a receipt noccounting could not read, so its expense is reported with no lines",
			"trip_id", r.tripID, "item_id", item.ID, "name", item.Name)
	}

	read := readExpense{
		expense: domain.Expense{
			ID:             strconv.FormatInt(item.ID, 10),
			Name:           item.Name,
			Price:          price,
			Currency:       domain.Currency(currency),
			ExchangeRate:   rate,
			Category:       fromTrekCategory(item.Category),
			Method:         method,
			PaidByID:       trekUserID(payer),
			ShoppedAt:      shoppedAt,
			ParticipantIDs: item.memberIDs(),
			ReceiptURL:     item.receiptURL(),
			ReceiptItems:   lines,
		},
		createdAt: item.createdAt(),
		itemID:    item.ID,
		payerID:   payer,
		payerRows: lookup,
	}
	return read, nil
}

func trekUserID(userID int64) string {
	if userID <= 0 {
		return ""
	}
	return strconv.FormatInt(userID, 10)
}

type budgetItemsResponse struct {
	Items []budgetItem `json:"items"`
}

type budgetItem struct {
	ID           int64               `json:"id"`
	Name         string              `json:"name"`
	Category     string              `json:"category"`
	TotalPrice   float64             `json:"total_price"`
	Currency     string              `json:"currency"`
	ExchangeRate float64             `json:"exchange_rate"`
	Note         string              `json:"note"`
	ExpenseDate  string              `json:"expense_date"`
	CreatedAt    string              `json:"created_at"`
	PaidByUserID *int64              `json:"paid_by_user_id"`
	TicketJSON   string              `json:"ticket_json"`
	Payers       []budgetItemPayer   `json:"payers"`
	Members      []budgetItemMember  `json:"members"`
	Receipts     []budgetItemReceipt `json:"receipts"`
}

type budgetItemMember struct {
	UserID int64    `json:"user_id"`
	Amount *float64 `json:"amount"`
}

func (b budgetItem) hasCustomSplit() bool {
	for _, member := range b.Members {
		if member.Amount != nil {
			return true
		}
	}
	return false
}

type budgetItemPayer struct {
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
}

type budgetItemReceipt struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

func (b budgetItem) receiptURL() string {
	if len(b.Receipts) == 0 {
		return ""
	}
	return b.Receipts[0].URL
}

func (b budgetItem) payerID() int64 {
	if payerIDs := b.payerIDs(); len(payerIDs) > 0 {
		return payerIDs[0]
	}
	return 0
}

func (b budgetItem) payerIDs() []int64 {
	ids := make([]int64, 0, len(b.Payers))
	for _, payer := range b.Payers {
		if payer.UserID > 0 {
			ids = append(ids, payer.UserID)
		}
	}
	if len(ids) == 0 && b.PaidByUserID != nil && *b.PaidByUserID > 0 {
		ids = append(ids, *b.PaidByUserID)
	}
	return ids
}

func (b budgetItem) memberIDs() []string {
	ids := make([]string, 0, len(b.Members))
	for _, member := range b.Members {
		if member.UserID > 0 {
			ids = append(ids, trekUserID(member.UserID))
		}
	}
	return ids
}

func (b budgetItem) createdAt() time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if at, err := time.ParseInLocation(layout, b.CreatedAt, time.UTC); err == nil {
			return at
		}
	}
	return time.Time{}
}

func selectExpenses(listed []readExpense, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	payer, err := filteredPayer(filter)
	if err != nil {
		return nil, err
	}
	days := newDayRange(filter)

	selected := make([]domain.Expense, 0, len(listed))
	for _, read := range listed {
		if !days.contains(read.expense.ShoppedAt) || !payer.matches(read) {
			continue
		}
		if filter.Method != nil && read.expense.Method != *filter.Method {
			continue
		}
		if filter.Limit != nil && len(selected) >= *filter.Limit {
			break
		}
		selected = append(selected, read.expense)
	}
	return selected, nil
}

type dayRange struct {
	from, to       time.Time
	hasFrom, hasTo bool
}

// TREK stores calendar dates; comparing instants would apply a timezone offset.
func newDayRange(filter expense.ExpenseFilter) dayRange {
	days := dayRange{}
	if filter.DateFrom != nil {
		days.from, days.hasFrom = calendarDay(*filter.DateFrom), true
	}
	if filter.DateTo != nil {
		days.to, days.hasTo = calendarDay(*filter.DateTo), true
	}
	return days
}

func (d dayRange) contains(day time.Time) bool {
	if d.hasFrom && day.Before(d.from) {
		return false
	}
	if d.hasTo && day.After(d.to) {
		return false
	}
	return true
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

type payerFilter struct {
	userID int64
	set    bool
}

func filteredPayer(filter expense.ExpenseFilter) (payerFilter, error) {
	if filter.PaidByID == nil {
		return payerFilter{}, nil
	}

	ref, err := parseTrekUserID(PayerRef{ID: *filter.PaidByID})
	if err != nil {
		return payerFilter{}, fmt.Errorf("filtering by payer %q: %w", *filter.PaidByID, err)
	}
	if ref == 0 {
		return payerFilter{}, fmt.Errorf("%w: filtering by payer %q, which names no user", ErrInvalidPayerID, *filter.PaidByID)
	}
	return payerFilter{userID: ref, set: true}, nil
}

func (f payerFilter) matches(read readExpense) bool {
	if !f.set {
		return true
	}
	if len(read.payerRows) == 0 {
		return read.payerID == f.userID
	}
	for _, payer := range read.payerRows {
		if payer.UserID == f.userID {
			return true
		}
	}
	return false
}

func sortExpenses(listed []readExpense) {
	slices.SortFunc(listed, func(a, b readExpense) int {
		if byDate := b.expense.ShoppedAt.Compare(a.expense.ShoppedAt); byDate != 0 {
			return byDate
		}
		if byCreated := b.createdAt.Compare(a.createdAt); byCreated != 0 {
			return byCreated
		}
		return cmp.Compare(b.itemID, a.itemID)
	})
}
