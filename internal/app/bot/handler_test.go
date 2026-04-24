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
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// --- Spy: AccountingRepo ---

type spyAccountingRepo struct {
	createErr       error
	uploadErr       error
	uploadResult    string
	createdExpenses []*domain.Expense // records every CreateExpense call
	uploadCalls     int
}

func (m *spyAccountingRepo) CreateExpense(_ context.Context, expense *domain.Expense) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *spyAccountingRepo) QueryExpenses(_ context.Context) ([]domain.Expense, error) {
	return nil, nil
}

func (m *spyAccountingRepo) QueryExpensesWithFilter(_ context.Context, _ expense.ExpenseFilter) ([]domain.Expense, error) {
	return nil, nil
}

func (m *spyAccountingRepo) UpdateExpense(_ context.Context, _ *domain.Expense) error {
	return nil
}

func (m *spyAccountingRepo) DeleteExpense(_ context.Context, _ string) error {
	return nil
}

func (m *spyAccountingRepo) GetExpenseSummary(_ context.Context) (*domain.ExpenseSummary, error) {
	return nil, nil
}

func (m *spyAccountingRepo) UploadFile(_ context.Context, _ string) (string, error) {
	m.uploadCalls++
	if m.uploadErr != nil {
		return "", m.uploadErr
	}
	return m.uploadResult, nil
}

// --- Stub: ReceiptAnalyzer ---

type stubReceiptAnalyzer struct {
	result *domain.ReceiptAnalysis
	err    error
}

func (m *stubReceiptAnalyzer) Analyze(_ context.Context, _ []byte) (*domain.ReceiptAnalysis, error) {
	return m.result, m.err
}

// --- Fake: UserRepo (for user.Service) ---

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
	}
	return nil, domain.ErrUserNotFound
}

func (m *fakeUserRepo) GetUsers() ([]domain.User, error) {
	return nil, nil
}

// --- Stub: ExchangeRateFetcher ---

type stubRateFetcher struct{}

func (m *stubRateFetcher) GetRate(_ context.Context, _ domain.Currency) (decimal.Decimal, error) {
	return decimal.NewFromFloat(0.27), nil
}

// --- Spy: telebot API (only methods used by receipt handlers) ---

type spyBotAPI struct {
	tele.API // embed to satisfy the interface; unused methods panic

	fileReader io.ReadCloser
	fileErr    error
	editCalls  int
}

func (m *spyBotAPI) File(_ *tele.File) (io.ReadCloser, error) {
	return m.fileReader, m.fileErr
}

func (m *spyBotAPI) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	m.editCalls++
	return &tele.Message{}, nil
}

// --- Spy: telebot Context ---

type spyContext struct {
	sender    *tele.User
	message   *tele.Message
	callback  *tele.Callback
	bot       tele.API
	sentMsgs  []any // records Send() payloads
	responded []*tele.CallbackResponse
}

func (m *spyContext) Bot() tele.API                                { return m.bot }
func (m *spyContext) Update() tele.Update                          { return tele.Update{} }
func (m *spyContext) Message() *tele.Message                       { return m.message }
func (m *spyContext) Callback() *tele.Callback                     { return m.callback }
func (m *spyContext) Query() *tele.Query                           { return nil }
func (m *spyContext) InlineResult() *tele.InlineResult             { return nil }
func (m *spyContext) ShippingQuery() *tele.ShippingQuery           { return nil }
func (m *spyContext) PreCheckoutQuery() *tele.PreCheckoutQuery     { return nil }
func (m *spyContext) Payment() *tele.Payment                       { return nil }
func (m *spyContext) Poll() *tele.Poll                             { return nil }
func (m *spyContext) PollAnswer() *tele.PollAnswer                 { return nil }
func (m *spyContext) ChatMember() *tele.ChatMemberUpdate           { return nil }
func (m *spyContext) ChatJoinRequest() *tele.ChatJoinRequest       { return nil }
func (m *spyContext) Migration() (int64, int64)                    { return 0, 0 }
func (m *spyContext) Topic() *tele.Topic                           { return nil }
func (m *spyContext) Boost() *tele.BoostUpdated                    { return nil }
func (m *spyContext) BoostRemoved() *tele.BoostRemoved             { return nil }
func (m *spyContext) PurchasedPaidMedia() *tele.PaidMediaPurchased { return nil }
func (m *spyContext) Sender() *tele.User                           { return m.sender }
func (m *spyContext) Chat() *tele.Chat                             { return nil }
func (m *spyContext) Recipient() tele.Recipient                    { return m.sender }
func (m *spyContext) Text() string                                 { return "" }
func (m *spyContext) ThreadID() int                                { return 0 }
func (m *spyContext) Entities() tele.Entities                      { return nil }
func (m *spyContext) Data() string                                 { return "" }
func (m *spyContext) Args() []string                               { return nil }

func (m *spyContext) Send(what any, _ ...any) error {
	m.sentMsgs = append(m.sentMsgs, what)
	return nil
}

func (m *spyContext) SendAlbum(_ tele.Album, _ ...any) error     { return nil }
func (m *spyContext) Reply(_ any, _ ...any) error                { return nil }
func (m *spyContext) Forward(_ tele.Editable, _ ...any) error    { return nil }
func (m *spyContext) ForwardTo(_ tele.Recipient, _ ...any) error { return nil }
func (m *spyContext) Edit(_ any, _ ...any) error                 { return nil }
func (m *spyContext) EditCaption(_ string, _ ...any) error       { return nil }
func (m *spyContext) EditOrSend(_ any, _ ...any) error           { return nil }
func (m *spyContext) EditOrReply(_ any, _ ...any) error          { return nil }
func (m *spyContext) Delete() error                              { return nil }
func (m *spyContext) DeleteAfter(_ time.Duration) *time.Timer    { return nil }
func (m *spyContext) Notify(_ tele.ChatAction) error             { return nil }
func (m *spyContext) Ship(_ ...any) error                        { return nil }
func (m *spyContext) Accept(_ ...string) error                   { return nil }
func (m *spyContext) Answer(_ *tele.QueryResponse) error         { return nil }

func (m *spyContext) Respond(resp ...*tele.CallbackResponse) error {
	m.responded = append(m.responded, resp...)
	return nil
}

func (m *spyContext) RespondText(_ string) error  { return nil }
func (m *spyContext) RespondAlert(_ string) error { return nil }
func (m *spyContext) Get(_ string) any            { return nil }
func (m *spyContext) Set(_ string, _ any)         {}

// --- Test Fixtures ---

const (
	testTelegramUserID int64 = 12345
	testNotionUserID         = "notion-user-abc"
)

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

func newTestHandler(repo expense.AccountingRepo, analyzer expense.ReceiptAnalyzer) *Handler {
	userRepo := &fakeUserRepo{
		users: map[int64]*domain.User{
			testTelegramUserID: {
				ID:         1,
				TelegramID: testTelegramUserID,
				NotionID:   testNotionUserID,
				Nickname:   "test-user",
			},
		},
	}
	userSvc := user.NewService(userRepo)
	expenseSvc := expense.NewService(repo, &stubRateFetcher{}, analyzer)

	return NewHandler(userSvc, expenseSvc, "")
}

func newPhotoContext(botAPI tele.API) *spyContext {
	return &spyContext{
		sender: &tele.User{ID: testTelegramUserID},
		message: &tele.Message{
			Photo: &tele.Photo{
				File: tele.File{FileID: "photo-123"},
			},
		},
		bot: botAPI,
	}
}

func newCallbackContext(botAPI tele.API, data string) *spyContext {
	return &spyContext{
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

	repo := &spyAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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

	repo := &spyAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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

	repo := &spyAccountingRepo{uploadResult: "uploaded-file-id"}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	// --- Step 1: handlePhoto ---
	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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
	repo := &spyAccountingRepo{}
	h := newTestHandler(repo, nil) // receiptAnalyzer = nil → HasReceiptAnalyzer() returns false

	botAPI := &spyBotAPI{}
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
	repo := &spyAccountingRepo{}
	analyzer := &stubReceiptAnalyzer{err: errors.New("LLM timeout")}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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
	repo := &spyPartialFailRepo{
		uploadResult: "file-id",
		failOnCall:   2, // fail on the 2nd item
	}

	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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

// spyPartialFailRepo fails CreateExpense on a specific call number and records successful creates.
type spyPartialFailRepo struct {
	uploadResult    string
	failOnCall      int
	callCount       int
	createdExpenses []*domain.Expense
}

func (m *spyPartialFailRepo) CreateExpense(_ context.Context, expense *domain.Expense) error {
	m.callCount++
	if m.callCount == m.failOnCall {
		return errors.New("simulated failure")
	}
	m.createdExpenses = append(m.createdExpenses, expense)
	return nil
}

func (m *spyPartialFailRepo) QueryExpenses(_ context.Context) ([]domain.Expense, error) {
	return nil, nil
}

func (m *spyPartialFailRepo) QueryExpensesWithFilter(_ context.Context, _ expense.ExpenseFilter) ([]domain.Expense, error) {
	return nil, nil
}

func (m *spyPartialFailRepo) UpdateExpense(_ context.Context, _ *domain.Expense) error {
	return nil
}

func (m *spyPartialFailRepo) DeleteExpense(_ context.Context, _ string) error {
	return nil
}

func (m *spyPartialFailRepo) GetExpenseSummary(_ context.Context) (*domain.ExpenseSummary, error) {
	return nil, nil
}

func (m *spyPartialFailRepo) UploadFile(_ context.Context, _ string) (string, error) {
	return m.uploadResult, nil
}

// --- Helper: Handler with custom UserRepo ---

func newTestHandlerWithUserRepo(repo expense.AccountingRepo, analyzer expense.ReceiptAnalyzer, userRepo user.UserRepo) *Handler {
	userSvc := user.NewService(userRepo)
	expenseSvc := expense.NewService(repo, &stubRateFetcher{}, analyzer)
	return NewHandler(userSvc, expenseSvc, "")
}

// ============================================================
// Error Path Tests
// ============================================================

func TestHandlePhoto_UnauthorizedUser_ReturnsError(t *testing.T) {
	analysis := newTestAnalysis()
	repo := &spyAccountingRepo{}
	analyzer := &stubReceiptAnalyzer{result: analysis}

	// UserRepo with no users → IsAuthorized returns false
	h := newTestHandlerWithUserRepo(repo, analyzer, &fakeUserRepo{
		users: map[int64]*domain.User{}, // empty
	})

	botAPI := &spyBotAPI{}
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
	repo := &spyAccountingRepo{}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	botAPI := &spyBotAPI{
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

	repo := &spyAccountingRepo{
		uploadErr: errors.New("Notion upload failed"),
	}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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

	repo := &spyAccountingRepo{
		uploadResult: "file-id",
		createErr:    errors.New("Notion API error"),
	}
	analyzer := &stubReceiptAnalyzer{result: analysis}
	h := newTestHandler(repo, analyzer)

	imageData := []byte("fake-jpeg-image-data")
	botAPI := &spyBotAPI{
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

	repo := &spyAccountingRepo{uploadResult: "file-id"}
	analyzer := &stubReceiptAnalyzer{result: analysis}

	// User exists for IsAuthorized (handlePhoto) but GetUser fails during callback
	// We simulate this by using a user repo that succeeds for the photo step
	// then swapping to one that fails. Instead, we use a repo where the user
	// is authorized but GetUser returns an error for the Notion ID lookup.
	failUserRepo := &fakeUserRepo{
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
	botAPI := &spyBotAPI{
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
