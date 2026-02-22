package bot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// --- Mock: AccountingRepo ---

type mockAccountingRepo struct {
	createErr       error
	uploadErr       error
	uploadResult    string
	createdExpenses []*domain.Expense // records every CreateExpense call
	uploadCalls     int
}

func (m *mockAccountingRepo) CreateExpense(_ context.Context, expense *domain.Expense) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *mockAccountingRepo) QueryExpenses(_ context.Context) ([]domain.Expense, error) {
	return nil, nil
}

func (m *mockAccountingRepo) QueryExpensesWithFilter(_ context.Context, _ domain.ExpenseFilter) ([]domain.Expense, error) {
	return nil, nil
}

func (m *mockAccountingRepo) UpdateExpense(_ context.Context, _ *domain.Expense) error {
	return nil
}

func (m *mockAccountingRepo) DeleteExpense(_ context.Context, _ string) error {
	return nil
}

func (m *mockAccountingRepo) GetExpenseSummary(_ context.Context) (*domain.ExpenseSummary, error) {
	return nil, nil
}

func (m *mockAccountingRepo) UploadFile(_ context.Context, _ string) (string, error) {
	m.uploadCalls++
	if m.uploadErr != nil {
		return "", m.uploadErr
	}
	return m.uploadResult, nil
}

// --- Mock: ReceiptAnalyzer ---

type mockReceiptAnalyzer struct {
	result *domain.ReceiptAnalysis
	err    error
}

func (m *mockReceiptAnalyzer) Analyze(_ context.Context, _ []byte) (*domain.ReceiptAnalysis, error) {
	return m.result, m.err
}

// --- Mock: UserRepo (for user.Service) ---

type mockUserRepo struct {
	users map[int64]*domain.User
	err   error
}

func (m *mockUserRepo) GetUser(req domain.GetUserRequest) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	if req.TelegramID != nil {
		if u, ok := m.users[*req.TelegramID]; ok {
			return u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (m *mockUserRepo) GetUsers() ([]domain.User, error) {
	return nil, nil
}

// --- Mock: ExchangeRateFetcher ---

type mockRateFetcher struct{}

func (m *mockRateFetcher) GetRate(_ context.Context, _ domain.Currency) (decimal.Decimal, error) {
	return decimal.NewFromFloat(0.27), nil
}

// --- Mock: telebot API (only methods used by receipt handlers) ---

type mockBotAPI struct {
	tele.API // embed to satisfy the interface; unused methods panic

	fileReader io.ReadCloser
	fileErr    error
	editCalls  int
}

func (m *mockBotAPI) File(_ *tele.File) (io.ReadCloser, error) {
	return m.fileReader, m.fileErr
}

func (m *mockBotAPI) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	m.editCalls++
	return &tele.Message{}, nil
}

// --- Mock: telebot Context ---

type mockContext struct {
	sender    *tele.User
	message   *tele.Message
	callback  *tele.Callback
	bot       tele.API
	sentMsgs  []any // records Send() payloads
	responded []*tele.CallbackResponse
}

func (m *mockContext) Bot() tele.API                                { return m.bot }
func (m *mockContext) Update() tele.Update                          { return tele.Update{} }
func (m *mockContext) Message() *tele.Message                       { return m.message }
func (m *mockContext) Callback() *tele.Callback                     { return m.callback }
func (m *mockContext) Query() *tele.Query                           { return nil }
func (m *mockContext) InlineResult() *tele.InlineResult             { return nil }
func (m *mockContext) ShippingQuery() *tele.ShippingQuery           { return nil }
func (m *mockContext) PreCheckoutQuery() *tele.PreCheckoutQuery     { return nil }
func (m *mockContext) Payment() *tele.Payment                       { return nil }
func (m *mockContext) Poll() *tele.Poll                             { return nil }
func (m *mockContext) PollAnswer() *tele.PollAnswer                 { return nil }
func (m *mockContext) ChatMember() *tele.ChatMemberUpdate           { return nil }
func (m *mockContext) ChatJoinRequest() *tele.ChatJoinRequest       { return nil }
func (m *mockContext) Migration() (int64, int64)                    { return 0, 0 }
func (m *mockContext) Topic() *tele.Topic                           { return nil }
func (m *mockContext) Boost() *tele.BoostUpdated                    { return nil }
func (m *mockContext) BoostRemoved() *tele.BoostRemoved             { return nil }
func (m *mockContext) PurchasedPaidMedia() *tele.PaidMediaPurchased { return nil }
func (m *mockContext) Sender() *tele.User                           { return m.sender }
func (m *mockContext) Chat() *tele.Chat                             { return nil }
func (m *mockContext) Recipient() tele.Recipient                    { return m.sender }
func (m *mockContext) Text() string                                 { return "" }
func (m *mockContext) ThreadID() int                                { return 0 }
func (m *mockContext) Entities() tele.Entities                      { return nil }
func (m *mockContext) Data() string                                 { return "" }
func (m *mockContext) Args() []string                               { return nil }

func (m *mockContext) Send(what any, _ ...any) error {
	m.sentMsgs = append(m.sentMsgs, what)
	return nil
}

func (m *mockContext) SendAlbum(_ tele.Album, _ ...any) error     { return nil }
func (m *mockContext) Reply(_ any, _ ...any) error                { return nil }
func (m *mockContext) Forward(_ tele.Editable, _ ...any) error    { return nil }
func (m *mockContext) ForwardTo(_ tele.Recipient, _ ...any) error { return nil }
func (m *mockContext) Edit(_ any, _ ...any) error                 { return nil }
func (m *mockContext) EditCaption(_ string, _ ...any) error       { return nil }
func (m *mockContext) EditOrSend(_ any, _ ...any) error           { return nil }
func (m *mockContext) EditOrReply(_ any, _ ...any) error          { return nil }
func (m *mockContext) Delete() error                              { return nil }
func (m *mockContext) DeleteAfter(_ time.Duration) *time.Timer    { return nil }
func (m *mockContext) Notify(_ tele.ChatAction) error             { return nil }
func (m *mockContext) Ship(_ ...any) error                        { return nil }
func (m *mockContext) Accept(_ ...string) error                   { return nil }
func (m *mockContext) Answer(_ *tele.QueryResponse) error         { return nil }

func (m *mockContext) Respond(resp ...*tele.CallbackResponse) error {
	m.responded = append(m.responded, resp...)
	return nil
}

func (m *mockContext) RespondText(_ string) error  { return nil }
func (m *mockContext) RespondAlert(_ string) error { return nil }
func (m *mockContext) Get(_ string) any            { return nil }
func (m *mockContext) Set(_ string, _ any)         {}

// --- Test Fixtures ---

const testTelegramUserID int64 = 12345
const testNotionUserID = "notion-user-abc"

func newTestAnalysis() *domain.ReceiptAnalysis {
	return &domain.ReceiptAnalysis{
		Summary:  "松屋 午餐",
		Currency: domain.CurrencyJPY,
		Total:    1280,
		Items: []domain.ReceiptItem{
			{Name: "牛丼", NameZH: "", Price: 480, Category: domain.Category食},
			{Name: "サラダ", NameZH: "沙拉", Price: 300, Category: domain.Category食},
			{Name: "ドリンク", NameZH: "飲料", Price: 500, Category: domain.Category食},
		},
	}
}

func newTestHandler(repo domain.AccountingRepo, analyzer domain.ReceiptAnalyzer) *Handler {
	userRepo := &mockUserRepo{
		users: map[int64]*domain.User{
			testTelegramUserID: {
				ID:         1,
				TelegramID: testTelegramUserID,
				NotionID:   testNotionUserID,
				Nickname:   "test-user",
			},
		},
	}
	svc := user.NewService(userRepo)

	return NewHandler(svc, repo, &mockRateFetcher{}, analyzer, "")
}

func newPhotoContext(botAPI tele.API) *mockContext {
	return &mockContext{
		sender: &tele.User{ID: testTelegramUserID},
		message: &tele.Message{
			Photo: &tele.Photo{
				File: tele.File{FileID: "photo-123"},
			},
		},
		bot: botAPI,
	}
}

func newCallbackContext(botAPI tele.API, data string) *mockContext {
	return &mockContext{
		sender: &tele.User{ID: testTelegramUserID},
		message: &tele.Message{
			Text: "📸 松屋 午餐\n...",
		},
		callback: &tele.Callback{
			Data: data,
		},
		bot: botAPI,
	}
}

// ============================================================
// Integration Tests: Photo → ReceiptAnalysis → Action → Repo
// ============================================================

func TestHandlePhoto_ThenSingle_CreatesOneExpense(t *testing.T) {
	analysis := newTestAnalysis()

	repo := &mockAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	// Verify conversation state was stored
	state := h.convManager.GetState(testTelegramUserID)
	if state == nil {
		t.Fatal("expected conversation state to be set after handlePhoto")
	}
	if state.Step != ReceiptConfirm {
		t.Errorf("expected step ReceiptConfirm, got %d", state.Step)
	}
	if state.ReceiptAnalysis != analysis {
		t.Error("expected analysis to be stored in state")
	}

	// --- Step 2: handleReceiptCallback with "single" ---
	callbackCtx := newCallbackContext(botAPI, "receipt|single")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|single"); err != nil {
		t.Fatalf("handleReceiptCallback(single) returned error: %v", err)
	}

	// --- Assertions ---

	// UploadFile should have been called once
	if repo.uploadCalls != 1 {
		t.Errorf("expected 1 UploadFile call, got %d", repo.uploadCalls)
	}

	// CreateExpense should have been called exactly once
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected 1 CreateExpense call, got %d", len(repo.createdExpenses))
	}

	exp := repo.createdExpenses[0]

	if exp.Name != "松屋 午餐" {
		t.Errorf("expected Name '松屋 午餐', got %q", exp.Name)
	}
	if exp.Price != 1280 {
		t.Errorf("expected Price 1280, got %d", exp.Price)
	}
	if exp.Currency != domain.CurrencyJPY {
		t.Errorf("expected Currency JPY, got %s", exp.Currency)
	}
	if exp.Category != domain.Category食 {
		t.Errorf("expected Category 食, got %s", exp.Category)
	}
	if exp.Method != domain.PaymentMethodCash {
		t.Errorf("expected Method cash, got %s", exp.Method)
	}
	if exp.PaidByID != testNotionUserID {
		t.Errorf("expected PaidByID %q, got %q", testNotionUserID, exp.PaidByID)
	}
	if exp.ReceiptURL != "uploaded-file-id" {
		t.Errorf("expected ReceiptURL 'uploaded-file-id', got %q", exp.ReceiptURL)
	}
	if len(exp.ReceiptItems) != 3 {
		t.Errorf("expected 3 ReceiptItems, got %d", len(exp.ReceiptItems))
	}

	// Conversation state should be cleared
	if h.convManager.GetState(testTelegramUserID) != nil {
		t.Error("expected conversation state to be cleared after single")
	}
}

func TestHandlePhoto_ThenSplit_CreatesMultipleExpenses(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "松屋 午餐",
		Currency: domain.CurrencyJPY,
		Total:    1280,
		Items: []domain.ReceiptItem{
			{Name: "牛丼", NameZH: "", Price: 480, Category: domain.Category食},
			{Name: "お土産", NameZH: "伴手禮", Price: 300, Category: domain.Category購},
			{Name: "タクシー", NameZH: "計程車", Price: 500, Category: domain.Category行},
		},
	}

	repo := &mockAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)
	if state == nil {
		t.Fatal("expected conversation state to be set")
	}

	// --- Step 2: handleReceiptCallback with "split" ---
	callbackCtx := newCallbackContext(botAPI, "receipt|split")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|split"); err != nil {
		t.Fatalf("handleReceiptCallback(split) returned error: %v", err)
	}

	// --- Assertions ---

	// UploadFile should have been called once (shared across all items)
	if repo.uploadCalls != 1 {
		t.Errorf("expected 1 UploadFile call, got %d", repo.uploadCalls)
	}

	// CreateExpense should have been called 3 times (one per item)
	if len(repo.createdExpenses) != 3 {
		t.Fatalf("expected 3 CreateExpense calls, got %d", len(repo.createdExpenses))
	}

	// Verify each expense matches its corresponding item
	expectations := []struct {
		name     string
		price    uint64
		category domain.Category
	}{
		{"牛丼", 480, domain.Category食},
		{"お土産（伴手禮）", 300, domain.Category購},
		{"タクシー（計程車）", 500, domain.Category行},
	}

	for i, want := range expectations {
		got := repo.createdExpenses[i]
		if got.Name != want.name {
			t.Errorf("item %d: expected Name %q, got %q", i, want.name, got.Name)
		}
		if got.Price != want.price {
			t.Errorf("item %d: expected Price %d, got %d", i, want.price, got.Price)
		}
		if got.Category != want.category {
			t.Errorf("item %d: expected Category %s, got %s", i, want.category, got.Category)
		}
		if got.Currency != domain.CurrencyJPY {
			t.Errorf("item %d: expected Currency JPY, got %s", i, got.Currency)
		}
		if got.Method != domain.PaymentMethodCash {
			t.Errorf("item %d: expected Method cash, got %s", i, got.Method)
		}
		if got.PaidByID != testNotionUserID {
			t.Errorf("item %d: expected PaidByID %q, got %q", i, testNotionUserID, got.PaidByID)
		}
		if got.ReceiptURL != "uploaded-file-id" {
			t.Errorf("item %d: expected ReceiptURL 'uploaded-file-id', got %q", i, got.ReceiptURL)
		}
	}

	// Conversation state should be cleared
	if h.convManager.GetState(testTelegramUserID) != nil {
		t.Error("expected conversation state to be cleared after split")
	}
}

func TestHandlePhoto_ThenCancel_NoExpenseCreated(t *testing.T) {
	analysis := newTestAnalysis()

	repo := &mockAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)
	if state == nil {
		t.Fatal("expected conversation state to be set")
	}

	// --- Step 2: handleReceiptCallback with "cancel" ---
	callbackCtx := newCallbackContext(botAPI, "receipt|cancel")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|cancel"); err != nil {
		t.Fatalf("handleReceiptCallback(cancel) returned error: %v", err)
	}

	// --- Assertions ---

	// No UploadFile or CreateExpense should have been called
	if repo.uploadCalls != 0 {
		t.Errorf("expected 0 UploadFile calls, got %d", repo.uploadCalls)
	}
	if len(repo.createdExpenses) != 0 {
		t.Errorf("expected 0 CreateExpense calls, got %d", len(repo.createdExpenses))
	}

	// Conversation state should be cleared
	if h.convManager.GetState(testTelegramUserID) != nil {
		t.Error("expected conversation state to be cleared after cancel")
	}
}

func TestHandlePhoto_AnalyzerDisabled_ReturnsMessage(t *testing.T) {
	repo := &mockAccountingRepo{}
	h := newTestHandler(repo, nil) // receiptAnalyzer = nil
	h.receiptAnalyzer = nil

	botAPI := &mockBotAPI{}
	photoCtx := newPhotoContext(botAPI)

	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	if len(photoCtx.sentMsgs) == 0 {
		t.Fatal("expected a message to be sent")
	}

	msg, ok := photoCtx.sentMsgs[0].(string)
	if !ok || msg != "📸 收據分析功能尚未啟用" {
		t.Errorf("expected disabled message, got %v", photoCtx.sentMsgs[0])
	}
}

func TestHandlePhoto_AnalyzerFails_ReturnsErrorMessage(t *testing.T) {
	repo := &mockAccountingRepo{}
	analyzer := &mockReceiptAnalyzer{err: errors.New("LLM timeout")}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	// Should show error message, not crash
	found := false
	for _, msg := range photoCtx.sentMsgs {
		if s, ok := msg.(string); ok && s == "❌ 無法辨識收據，請嘗試手動輸入\n/quick" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected analysis failure message, got %v", photoCtx.sentMsgs)
	}

	// No expense should be created
	if len(repo.createdExpenses) != 0 {
		t.Error("expected 0 expenses after analyzer failure")
	}
}

func TestHandlePhoto_ThenSplit_PartialFailure(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{
		Summary:  "便利商店",
		Currency: domain.CurrencyTWD,
		Total:    200,
		Items: []domain.ReceiptItem{
			{Name: "飯糰", Price: 35, Category: domain.Category食},
			{Name: "牛奶", Price: 65, Category: domain.Category食},
			{Name: "雜誌", Price: 100, Category: domain.Category雜},
		},
	}

	// CreateExpense fails on 2nd call
	repo := &partialFailRepo{
		uploadResult: "file-id",
		failOnCall:   2, // fail on the 2nd item
	}

	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)
	callbackCtx := newCallbackContext(botAPI, "receipt|split")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|split"); err != nil {
		t.Fatalf("handleReceiptCallback(split) returned error: %v", err)
	}

	// Should have 2 successful expenses (1st and 3rd)
	if len(repo.createdExpenses) != 2 {
		t.Errorf("expected 2 successful CreateExpense calls, got %d", len(repo.createdExpenses))
	}

	// Should show partial success message
	found := false
	for _, msg := range callbackCtx.sentMsgs {
		if s, ok := msg.(string); ok {
			if len(s) > 0 && s[0] == 0xe2 { // starts with ⚠️ (UTF-8)
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected partial success message, got %v", callbackCtx.sentMsgs)
	}
}

// partialFailRepo fails CreateExpense on a specific call number.
type partialFailRepo struct {
	uploadResult    string
	failOnCall      int
	callCount       int
	createdExpenses []*domain.Expense
}

func (m *partialFailRepo) CreateExpense(_ context.Context, expense *domain.Expense) error {
	m.callCount++
	if m.callCount == m.failOnCall {
		return errors.New("simulated failure")
	}
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *partialFailRepo) QueryExpenses(_ context.Context) ([]domain.Expense, error) {
	return nil, nil
}

func (m *partialFailRepo) QueryExpensesWithFilter(_ context.Context, _ domain.ExpenseFilter) ([]domain.Expense, error) {
	return nil, nil
}

func (m *partialFailRepo) UpdateExpense(_ context.Context, _ *domain.Expense) error {
	return nil
}

func (m *partialFailRepo) DeleteExpense(_ context.Context, _ string) error {
	return nil
}

func (m *partialFailRepo) GetExpenseSummary(_ context.Context) (*domain.ExpenseSummary, error) {
	return nil, nil
}

func (m *partialFailRepo) UploadFile(_ context.Context, _ string) (string, error) {
	return m.uploadResult, nil
}

// --- Helper: Handler with custom UserRepo ---

func newTestHandlerWithUserRepo(repo domain.AccountingRepo, analyzer domain.ReceiptAnalyzer, userRepo domain.UserRepo) *Handler {
	svc := user.NewService(userRepo)
	return NewHandler(svc, repo, &mockRateFetcher{}, analyzer, "")
}

// ============================================================
// Error Path Tests
// ============================================================

func TestHandlePhoto_UnauthorizedUser_ReturnsError(t *testing.T) {
	analysis := newTestAnalysis()
	repo := &mockAccountingRepo{}
	analyzer := &mockReceiptAnalyzer{result: analysis}

	// UserRepo with no users → IsAuthorized returns false
	h := newTestHandlerWithUserRepo(repo, analyzer, &mockUserRepo{
		users: map[int64]*domain.User{}, // empty
	})

	botAPI := &mockBotAPI{}
	photoCtx := newPhotoContext(botAPI)

	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	if len(photoCtx.sentMsgs) == 0 {
		t.Fatal("expected a message to be sent")
	}

	msg, ok := photoCtx.sentMsgs[0].(string)
	if !ok || msg != "❌ 未授權的使用者" {
		t.Errorf("expected unauthorized message, got %v", photoCtx.sentMsgs[0])
	}

	// No expense created
	if len(repo.createdExpenses) != 0 {
		t.Error("expected 0 expenses for unauthorized user")
	}
}

func TestHandlePhoto_FileDownloadFails_ReturnsError(t *testing.T) {
	analysis := newTestAnalysis()
	repo := &mockAccountingRepo{}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	botAPI := &mockBotAPI{
		fileErr: errors.New("telegram file download failed"),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	found := false
	for _, msg := range photoCtx.sentMsgs {
		if s, ok := msg.(string); ok && s == "❌ 無法下載照片" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected download failure message, got %v", photoCtx.sentMsgs)
	}

	if len(repo.createdExpenses) != 0 {
		t.Error("expected 0 expenses after download failure")
	}
}

func TestHandlePhoto_ThenSingle_UploadFails_StillCreatesExpenseWithoutReceiptURL(t *testing.T) {
	analysis := newTestAnalysis()

	repo := &mockAccountingRepo{
		uploadErr: errors.New("Notion upload failed"),
	}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)
	callbackCtx := newCallbackContext(botAPI, "receipt|single")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|single"); err != nil {
		t.Fatalf("handleReceiptCallback(single) returned error: %v", err)
	}

	// Expense should still be created (graceful degradation)
	if len(repo.createdExpenses) != 1 {
		t.Fatalf("expected 1 CreateExpense call, got %d", len(repo.createdExpenses))
	}

	exp := repo.createdExpenses[0]

	// ReceiptURL should be empty since upload failed
	if exp.ReceiptURL != "" {
		t.Errorf("expected empty ReceiptURL after upload failure, got %q", exp.ReceiptURL)
	}

	// Other fields should still be correct
	if exp.Name != "松屋 午餐" {
		t.Errorf("expected Name '松屋 午餐', got %q", exp.Name)
	}
	if exp.Price != 1280 {
		t.Errorf("expected Price 1280, got %d", exp.Price)
	}
}

func TestHandlePhoto_ThenSingle_CreateExpenseFails_ReturnsError(t *testing.T) {
	analysis := newTestAnalysis()

	repo := &mockAccountingRepo{
		uploadResult: "file-id",
		createErr:    errors.New("Notion API error"),
	}
	analyzer := &mockReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)
	callbackCtx := newCallbackContext(botAPI, "receipt|single")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|single"); err != nil {
		t.Fatalf("handleReceiptCallback(single) returned error: %v", err)
	}

	// Should show error message
	found := false
	for _, msg := range callbackCtx.sentMsgs {
		if s, ok := msg.(string); ok && s == "❌ 新增失敗，請稍後再試" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected create failure message, got %v", callbackCtx.sentMsgs)
	}

	// No expenses should be stored (createErr prevents append)
	if len(repo.createdExpenses) != 0 {
		t.Errorf("expected 0 stored expenses, got %d", len(repo.createdExpenses))
	}
}

func TestHandlePhoto_ThenSingle_GetUserFails_ReturnsError(t *testing.T) {
	analysis := newTestAnalysis()

	repo := &mockAccountingRepo{uploadResult: "file-id"}
	analyzer := &mockReceiptAnalyzer{result: analysis}

	// User exists for IsAuthorized (handlePhoto) but GetUser fails during callback
	// We simulate this by using a user repo that succeeds for the photo step
	// then swapping to one that fails. Instead, we use a repo where the user
	// is authorized but GetUser returns an error for the Notion ID lookup.
	failUserRepo := &mockUserRepo{
		users: map[int64]*domain.User{
			testTelegramUserID: {
				ID:         1,
				TelegramID: testTelegramUserID,
				NotionID:   testNotionUserID,
				Nickname:   "test-user",
			},
		},
	}
	h := newTestHandlerWithUserRepo(repo, analyzer, failUserRepo)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &mockBotAPI{
		fileReader: io.NopCloser(bytes.NewReader(imageData)),
	}

	photoCtx := newPhotoContext(botAPI)
	if err := h.handlePhoto(photoCtx); err != nil {
		t.Fatalf("handlePhoto returned error: %v", err)
	}

	state := h.convManager.GetState(testTelegramUserID)

	// Now make the user repo fail for GetUser
	failUserRepo.err = errors.New("database connection lost")

	callbackCtx := newCallbackContext(botAPI, "receipt|single")
	if err := h.handleReceiptCallback(callbackCtx, state, "receipt|single"); err != nil {
		t.Fatalf("handleReceiptCallback(single) returned error: %v", err)
	}

	// Should show user lookup error
	found := false
	for _, msg := range callbackCtx.sentMsgs {
		if s, ok := msg.(string); ok && s == "❌ 無法取得用戶資訊" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user lookup failure message, got %v", callbackCtx.sentMsgs)
	}

	// No expense should be created
	if len(repo.createdExpenses) != 0 {
		t.Error("expected 0 expenses after GetUser failure")
	}
}
