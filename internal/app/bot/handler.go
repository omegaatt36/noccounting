package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
	"github.com/shopspring/decimal"
)

// Handler handles Telegram bot commands.
type Handler struct {
	userService     *user.Service
	accountingRepo  domain.AccountingRepo
	rateFetcher     domain.ExchangeRateFetcher
	receiptAnalyzer domain.ReceiptAnalyzer
	webAppURL       string
	convManager     *ConversationManager
}

// NewHandler creates a new bot Handler.
func NewHandler(
	userService *user.Service,
	accountingRepo domain.AccountingRepo,
	rateFetcher domain.ExchangeRateFetcher,
	receiptAnalyzer domain.ReceiptAnalyzer,
	webAppURL string,
) *Handler {
	return &Handler{
		userService:     userService,
		accountingRepo:  accountingRepo,
		rateFetcher:     rateFetcher,
		receiptAnalyzer: receiptAnalyzer,
		webAppURL:       webAppURL,
		convManager:     NewConversationManager(),
	}
}

// RegisterHandlers registers all bot command handlers.
func (h *Handler) RegisterHandlers(bot *tele.Bot) {
	bot.Handle("/start", h.handleStart)
	bot.Handle("/help", h.handleHelp)
	bot.Handle("/add", h.handleAdd)
	bot.Handle("/list", h.handleList)
	bot.Handle("/summary", h.handleSummary)
	bot.Handle("/today", h.handleToday)
	bot.Handle("/quick", h.handleQuick)
	bot.Handle("/edit", h.handleEdit)
	bot.Handle("/cancel", h.handleCancel)

	// Handle text messages for conversation flows
	bot.Handle(tele.OnText, h.handleText)

	// Handle photo uploads for receipt scanning
	bot.Handle(tele.OnPhoto, h.handlePhoto)

	// Handle callback queries (inline keyboard buttons)
	bot.Handle(tele.OnCallback, h.handleCallback)
}

func (h *Handler) handleStart(c tele.Context) error {
	msg := `🧾 旅行記帳 Bot

使用 /help 查看所有指令`

	// If WebApp URL is configured, add a button to open Mini App
	if h.webAppURL != "" {
		webapp := &tele.WebApp{URL: h.webAppURL}
		btn := tele.InlineButton{
			Text:   "📝 新增消費",
			WebApp: webapp,
		}
		keyboard := &tele.ReplyMarkup{
			InlineKeyboard: [][]tele.InlineButton{{btn}},
		}
		return c.Send(msg, keyboard)
	}

	return c.Send(msg)
}

func (h *Handler) handleHelp(c tele.Context) error {
	catNames := strings.Join(domain.CategoryNames(), ", ")
	methodNames := strings.Join(domain.PaymentMethodNames(), ", ")

	help := fmt.Sprintf(`📖 指令說明

/add <名稱> <金額> <幣別> <分類> <付款方式>
  新增一筆消費記錄
  範例: /add 拉麵 1200 JPY 食 cash

/quick
  互動式新增消費（一步步引導）

/today
  查看今日消費統計

/list
  列出所有消費記錄

/edit
  編輯最近的消費記錄

/summary
  查看消費總覽

📌 分類選項: %s
💳 付款方式: %s
💱 幣別: TWD, JPY`, catNames, methodNames)

	return c.Send(help)
}

func (h *Handler) handleAdd(c tele.Context) error {
	args := c.Args()
	if len(args) < 5 {
		return c.Send(`❌ 格式錯誤

用法: /add <名稱> <金額> <幣別> <分類> <付款方式>
範例: /add 拉麵 1200 JPY 食 cash`)
	}

	name := args[0]

	price, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return c.Send("❌ 金額格式錯誤，請輸入正整數")
	}

	currency := domain.Currency(strings.ToUpper(args[2]))
	if !currency.IsValid() {
		return c.Send("❌ 幣別錯誤，請使用 TWD 或 JPY")
	}

	category := domain.Category(args[3])
	if !category.IsValid() {
		return c.Send(fmt.Sprintf("❌ 分類錯誤，請使用: %s", strings.Join(domain.CategoryNames(), ", ")))
	}

	method := domain.PaymentMethod(strings.ToLower(args[4]))
	if !method.IsValid() {
		return c.Send(fmt.Sprintf("❌ 付款方式錯誤，請使用: %s", strings.Join(domain.PaymentMethodNames(), ", ")))
	}

	// Get Notion user ID from mapping
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		return c.Send("❌ 無法取得用戶資訊，請確認您已註冊")
	}

	expense := &domain.Expense{
		Name:      name,
		Price:     price,
		Currency:  currency,
		Category:  category,
		Method:    method,
		PaidByID:  u.NotionID,
		ShoppedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.accountingRepo.CreateExpense(ctx, expense); err != nil {
		slog.Error("Failed to create expense", "error", err)
		return c.Send("❌ 新增失敗，連線資料庫錯誤，請稍後再試")
	}

	return c.Send(fmt.Sprintf(`✅ 已新增消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		name, price, currency, category.Emoji(), category, method.DisplayName()))
}

func (h *Handler) handleList(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expenses, err := h.accountingRepo.QueryExpenses(ctx)
	if err != nil {
		slog.Error("Failed to query expenses", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send("📭 目前沒有消費記錄")
	}

	var sb strings.Builder
	sb.WriteString("📋 消費記錄\n\n")

	for _, exp := range expenses {
		sb.WriteString(fmt.Sprintf("• %s: %d %s (%s %s)\n",
			exp.Name, exp.Price, exp.Currency, exp.Category.Emoji(), exp.Category))
	}

	return c.Send(sb.String())
}

func (h *Handler) handleSummary(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := h.accountingRepo.GetExpenseSummary(ctx)
	if err != nil {
		slog.Error("Failed to get summary", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	var sb strings.Builder
	sb.WriteString("📊 消費總覽\n\n")
	sb.WriteString(fmt.Sprintf("📝 總筆數: %d\n", summary.ItemCount))
	sb.WriteString(fmt.Sprintf("💰 總金額: %s TWD\n\n", summary.GrandTotal.StringFixed(0)))

	return c.Send(sb.String())
}

func (h *Handler) handleCancel(c tele.Context) error {
	h.convManager.ClearState(c.Sender().ID)
	return c.Send("❌ 已取消操作")
}

func (h *Handler) handleQuick(c tele.Context) error {
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		return c.Send("❌ 無法取得用戶資訊，請確認您已註冊")
	}

	h.convManager.StartQuickFlow(telegramUserID, u.NotionID)

	return c.Send("📝 開始新增消費\n\n請輸入消費名稱：\n\n(輸入 /cancel 取消)")
}

func (h *Handler) handlePhoto(c tele.Context) error {
	if h.receiptAnalyzer == nil {
		return c.Send("📸 收據分析功能尚未啟用")
	}

	if !h.userService.IsAuthorized(c.Sender().ID) {
		return c.Send("❌ 未授權的使用者")
	}

	photo := c.Message().Photo
	if photo == nil {
		return c.Send("❌ 無法取得照片")
	}

	c.Send("🔍 正在分析收據...")

	// Download photo from Telegram
	reader, err := c.Bot().File(&photo.File)
	if err != nil {
		slog.Error("Failed to download photo", "error", err)
		return c.Send("❌ 無法下載照片")
	}

	imageData, err := io.ReadAll(reader)
	if err != nil {
		return c.Send("❌ 無法讀取照片")
	}

	// Analyze receipt
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	analysis, err := h.receiptAnalyzer.Analyze(ctx, imageData)
	if err != nil {
		slog.Error("Receipt analysis failed", "error", err)
		return c.Send("❌ 無法辨識收據，請嘗試手動輸入\n/quick")
	}

	// Store analysis and image in conversation state
	h.convManager.SetState(c.Sender().ID, &ConversationState{
		Step:            ReceiptConfirm,
		ReceiptAnalysis: analysis,
		ReceiptImage:    imageData,
	})

	// Format analysis result
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📸 %s\n\n", analysis.Summary))
	for i, item := range analysis.Items {
		sb.WriteString(fmt.Sprintf("%d. %s %s ¥%d → %s\n",
			i+1, item.Category.Emoji(), item.DisplayName(), item.Price, item.Category))
	}
	sb.WriteString(fmt.Sprintf("\n合計: %d (%s)\n", analysis.Total, analysis.Currency))

	// Inline buttons
	keyboard := &tele.ReplyMarkup{}
	btnSingle := keyboard.Data("📦 整筆記", "receipt", "single")
	btnSplit := keyboard.Data("📋 拆開記", "receipt", "split")
	btnCancel := keyboard.Data("❌ 取消", "receipt", "cancel")
	keyboard.Inline(
		keyboard.Row(btnSingle, btnSplit),
		keyboard.Row(btnCancel),
	)

	return c.Send(sb.String(), keyboard)
}

func (h *Handler) handleText(c tele.Context) error {
	userID := c.Sender().ID
	state := h.convManager.GetState(userID)

	if state == nil {
		return nil // No active conversation, ignore
	}

	text := strings.TrimSpace(c.Text())

	switch state.Step {
	case StepQuickName:
		state.ExpenseDraft.Name = text
		state.Step = StepQuickPrice
		return c.Send("💰 請輸入金額（正整數）：")

	case StepQuickPrice:
		price, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return c.Send("❌ 金額格式錯誤，請輸入正整數：")
		}
		state.ExpenseDraft.Price = price
		state.Step = StepQuickCurrency

		// Show currency keyboard
		keyboard := &tele.ReplyMarkup{}
		keyboard.Inline(
			keyboard.Row(
				keyboard.Data("🇯🇵 JPY", "currency", "JPY"),
				keyboard.Data("🇹🇼 TWD", "currency", "TWD"),
			),
		)
		return c.Send("💱 請選擇幣別：", keyboard)

	case StepEditValue:
		return h.handleEditValue(c, state, text)
	}

	return nil
}

func (h *Handler) handleCallback(c tele.Context) error {
	userID := c.Sender().ID
	state := h.convManager.GetState(userID)
	data := c.Callback().Data

	// telebot v4 prefixes callback data with \f (form feed character)
	// We need to strip it for proper parsing
	data = strings.TrimPrefix(data, "\f")

	// Handle edit expense selection (format: "edit_select|{expense_id}")
	if strings.HasPrefix(data, "edit_select|") {
		return h.handleEditSelectCallback(c, data)
	}

	// Handle edit field selection (format: "edit_field|{field}")
	if strings.HasPrefix(data, "edit_field|") {
		return h.handleEditFieldCallback(c, state, data)
	}

	// Handle receipt callback (format: "receipt|{action}")
	if strings.HasPrefix(data, "receipt|") {
		return h.handleReceiptCallback(c, state, data)
	}

	if state == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期，請重新開始"})
	}

	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	action, value := parts[0], parts[1]

	switch action {
	case "currency":
		return h.handleQuickCurrency(c, state, value)
	case "category":
		return h.handleQuickCategory(c, state, value)
	case "method":
		return h.handleQuickMethod(c, state, value)
	case "confirm":
		return h.handleQuickConfirm(c, state, value)
	case "edit_cat":
		return h.handleEditCategory(c, state, value)
	case "edit_method":
		return h.handleEditMethod(c, state, value)
	}

	return c.Respond(&tele.CallbackResponse{Text: "未知操作"})
}

func (h *Handler) handleQuickCurrency(c tele.Context, state *ConversationState, value string) error {
	currency := domain.Currency(value)
	if !currency.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的幣別"})
	}

	state.ExpenseDraft.Currency = currency

	// Fetch exchange rate for JPY
	if currency == domain.CurrencyJPY && h.rateFetcher != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		rate, err := h.rateFetcher.GetRate(ctx, currency)
		if err != nil {
			slog.Warn("Failed to fetch exchange rate", "error", err)
			// Continue without rate, user can still proceed
		} else {
			state.ExpenseDraft.ExchangeRate = rate
			slog.Info("Fetched exchange rate", "currency", currency, "rate", rate.String())
		}
	}

	state.Step = StepQuickCategory

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + value})

	return c.Send("📂 請選擇分類：", makeCategoryKeyboard("category"))
}

func (h *Handler) handleQuickCategory(c tele.Context, state *ConversationState, value string) error {
	category := domain.Category(value)
	if !category.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的分類"})
	}

	state.ExpenseDraft.Category = category
	state.Step = StepQuickMethod

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + value})

	return c.Send("💳 請選擇付款方式：", makePaymentMethodKeyboard("method"))
}

func (h *Handler) handleQuickMethod(c tele.Context, state *ConversationState, value string) error {
	method := domain.PaymentMethod(value)
	if !method.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的付款方式"})
	}

	state.ExpenseDraft.Method = method
	state.Step = StepQuickConfirm

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + method.DisplayName()})

	// Show confirmation
	exp := state.ExpenseDraft

	// Build confirmation message with optional exchange rate info
	var rateInfo string

	if exp.Currency == domain.CurrencyJPY && !exp.ExchangeRate.IsZero() {
		twdAmount := exp.TotalInTWD()
		rateInfo = fmt.Sprintf("\n💱 匯率: %s (≈NT$%s)", exp.ExchangeRate.StringFixed(4), twdAmount.StringFixed(0))
	}

	confirmMsg := fmt.Sprintf(`📋 確認消費資訊

📝 名稱: %s
💰 金額: %d %s%s
📂 分類: %s %s
💳 付款: %s

確定要新增嗎？`, exp.Name, exp.Price, exp.Currency, rateInfo, exp.Category.Emoji(), exp.Category, exp.Method.DisplayName())

	keyboard := &tele.ReplyMarkup{}
	keyboard.Inline(
		keyboard.Row(
			keyboard.Data("✅ 確認", "confirm", "yes"),
			keyboard.Data("❌ 取消", "confirm", "no"),
		),
	)
	return c.Send(confirmMsg, keyboard)
}

func (h *Handler) handleQuickConfirm(c tele.Context, state *ConversationState, value string) error {
	defer h.convManager.ClearState(c.Sender().ID)

	if value != "yes" {
		_ = c.Respond(&tele.CallbackResponse{Text: "已取消"})
		return c.Send("❌ 已取消新增")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exp := state.ExpenseDraft
	if err := h.accountingRepo.CreateExpense(ctx, exp); err != nil {
		slog.Error("Failed to create expense", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send("❌ 新增失敗，連線資料庫錯誤，請稍後再試")
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	return c.Send(fmt.Sprintf(`✅ 已新增消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category.Emoji(), exp.Category, exp.Method.DisplayName()))
}

func (h *Handler) handleEdit(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get recent 5 expenses
	limit := 5
	filter := domain.ExpenseFilter{
		Limit: &limit,
	}

	expenses, err := h.accountingRepo.QueryExpensesWithFilter(ctx, filter)
	if err != nil {
		slog.Error("Failed to query expenses for edit", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send("📭 目前沒有消費記錄可編輯")
	}

	// Build inline keyboard with expense options
	keyboard := &tele.ReplyMarkup{}
	var rows []tele.Row

	for _, exp := range expenses {
		label := fmt.Sprintf("%s %d%s (%s)", exp.Name, exp.Price, exp.Currency, exp.Category)
		// Truncate if too long
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		btn := keyboard.Data(label, "edit_select", exp.ID)
		rows = append(rows, keyboard.Row(btn))
	}

	// Add cancel button
	rows = append(rows, keyboard.Row(keyboard.Data("❌ 取消", "edit_select", "cancel")))
	keyboard.Inline(rows...)

	return c.Send("📝 請選擇要編輯的消費記錄：", keyboard)
}

func (h *Handler) handleEditSelectCallback(c tele.Context, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	expenseID := parts[1]

	if expenseID == "cancel" {
		h.convManager.ClearState(c.Sender().ID)
		_ = c.Respond(&tele.CallbackResponse{Text: "已取消"})
		return c.Send("❌ 已取消編輯")
	}

	// Fetch the expense to edit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Query all and find by ID (Notion doesn't have get by ID in our current impl)
	limit := 20
	expenses, err := h.accountingRepo.QueryExpensesWithFilter(ctx, domain.ExpenseFilter{Limit: &limit})
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "查詢失敗"})
	}

	var targetExpense *domain.Expense
	for i := range expenses {
		if expenses[i].ID == expenseID {
			targetExpense = &expenses[i]
			break
		}
	}

	if targetExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "找不到該消費記錄"})
	}

	// Start edit flow
	h.convManager.StartEditFlow(c.Sender().ID, targetExpense)

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇"})

	// Show field selection keyboard
	keyboard := &tele.ReplyMarkup{}
	keyboard.Inline(
		keyboard.Row(
			keyboard.Data("📝 名稱", "edit_field", "name"),
			keyboard.Data("💰 金額", "edit_field", "price"),
		),
		keyboard.Row(
			keyboard.Data("📂 分類", "edit_field", "category"),
			keyboard.Data("💳 付款方式", "edit_field", "method"),
		),
		keyboard.Row(
			keyboard.Data("🗑️ 刪除此筆", "edit_field", "delete"),
			keyboard.Data("❌ 取消", "edit_field", "cancel"),
		),
	)

	exp := targetExpense
	msg := fmt.Sprintf(`📋 編輯消費記錄

📝 名稱: %s
💰 金額: %d %s
📂 分類: %s
💳 付款: %s
📅 日期: %s

請選擇要修改的欄位：`, exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName(), exp.ShoppedAt.Format("2006/01/02"))

	return c.Send(msg, keyboard)
}

func (h *Handler) handleEditFieldCallback(c tele.Context, state *ConversationState, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	field := parts[1]

	if field == "cancel" {
		h.convManager.ClearState(c.Sender().ID)
		_ = c.Respond(&tele.CallbackResponse{Text: "已取消"})
		return c.Send("❌ 已取消編輯")
	}

	if field == "delete" {
		return h.handleEditDelete(c, state)
	}

	if state == nil || state.EditingExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期，請重新開始"})
	}

	state.EditField = field
	state.Step = StepEditValue

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇"})

	switch field {
	case "name":
		return c.Send("請輸入新的名稱：")
	case "price":
		return c.Send("請輸入新的金額（正整數）：")
	case "category":
		return c.Send("請選擇新的分類：", makeCategoryKeyboard("edit_cat"))
	case "method":
		return c.Send("請選擇新的付款方式：", makePaymentMethodKeyboard("edit_method"))
	}

	return nil
}

func (h *Handler) handleEditValue(c tele.Context, state *ConversationState, text string) error {
	if state == nil || state.EditingExpense == nil {
		return c.Send("❌ 會話已過期，請使用 /edit 重新開始")
	}

	exp := state.EditingExpense

	switch state.EditField {
	case "name":
		exp.Name = text
	case "price":
		price, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return c.Send("❌ 金額格式錯誤，請輸入正整數：")
		}
		exp.Price = price
	default:
		return c.Send("❌ 未知的欄位")
	}

	// Update in Notion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.accountingRepo.UpdateExpense(ctx, exp); err != nil {
		slog.Error("Failed to update expense", "error", err)
		return c.Send("❌ 更新失敗，請稍後再試")
	}

	h.convManager.ClearState(c.Sender().ID)

	return c.Send(fmt.Sprintf(`✅ 已更新消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category.Emoji(), exp.Category, exp.Method.DisplayName()))
}

func (h *Handler) handleEditDelete(c tele.Context, state *ConversationState) error {
	if state == nil || state.EditingExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exp := state.EditingExpense
	if err := h.accountingRepo.DeleteExpense(ctx, exp.ID); err != nil {
		slog.Error("Failed to delete expense", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "刪除失敗"})
		return c.Send("❌ 刪除失敗，請稍後再試")
	}

	h.convManager.ClearState(c.Sender().ID)
	_ = c.Respond(&tele.CallbackResponse{Text: "已刪除"})

	return c.Send(fmt.Sprintf("✅ 已刪除消費記錄：%s %d %s", exp.Name, exp.Price, exp.Currency))
}

func (h *Handler) handleEditCategory(c tele.Context, state *ConversationState, value string) error {
	if state == nil || state.EditingExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	category := domain.Category(value)
	if !category.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的分類"})
	}

	state.EditingExpense.Category = category

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.accountingRepo.UpdateExpense(ctx, state.EditingExpense); err != nil {
		slog.Error("Failed to update expense category", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "更新失敗"})
		return c.Send("❌ 更新失敗，請稍後再試")
	}

	h.convManager.ClearState(c.Sender().ID)
	_ = c.Respond(&tele.CallbackResponse{Text: "已更新"})

	exp := state.EditingExpense
	return c.Send(fmt.Sprintf(`✅ 已更新消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category.Emoji(), exp.Category, exp.Method.DisplayName()))
}

func (h *Handler) handleEditMethod(c tele.Context, state *ConversationState, value string) error {
	if state == nil || state.EditingExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	method := domain.PaymentMethod(value)
	if !method.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的付款方式"})
	}

	state.EditingExpense.Method = method

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.accountingRepo.UpdateExpense(ctx, state.EditingExpense); err != nil {
		slog.Error("Failed to update expense method", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "更新失敗"})
		return c.Send("❌ 更新失敗，請稍後再試")
	}

	h.convManager.ClearState(c.Sender().ID)
	_ = c.Respond(&tele.CallbackResponse{Text: "已更新"})

	exp := state.EditingExpense
	return c.Send(fmt.Sprintf(`✅ 已更新消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category.Emoji(), exp.Category, exp.Method.DisplayName()))
}

func (h *Handler) handleReceiptCallback(c tele.Context, state *ConversationState, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	action := parts[1]

	// Remove inline buttons from the original message
	if msg := c.Message(); msg != nil {
		_, _ = c.Bot().Edit(msg, msg.Text)
	}

	if action == "cancel" {
		h.convManager.ClearState(c.Sender().ID)
		_ = c.Respond(&tele.CallbackResponse{Text: "已取消"})
		return c.Send("❌ 已取消操作")
	}

	if state == nil || state.ReceiptAnalysis == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	if action == "single" {
		return h.handleReceiptSingle(c, state)
	}

	if action == "split" {
		return h.handleReceiptSplit(c, state)
	}

	return c.Respond(&tele.CallbackResponse{Text: "未知操作"})
}

func (h *Handler) handleReceiptSingle(c tele.Context, state *ConversationState) error {
	defer h.convManager.ClearState(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get user's Notion ID
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "取得用戶失敗"})
		return c.Send("❌ 無法取得用戶資訊")
	}

	analysis := state.ReceiptAnalysis
	imageData := state.ReceiptImage

	// Upload photo to Notion
	receiptURL := ""
	if len(imageData) > 0 {
		tmpFile, err := os.CreateTemp("", "receipt-*.jpg")
		if err != nil {
			slog.Error("Failed to create temp file", "error", err)
		} else {
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write(imageData); err != nil {
				slog.Error("Failed to write image to temp file", "error", err)
			} else {
				tmpFile.Close()
				if uploadID, err := h.accountingRepo.UploadFile(ctx, tmpFile.Name()); err != nil {
					slog.Error("Failed to upload receipt", "error", err)
				} else {
					receiptURL = uploadID
				}
			}
		}
	}

	// Create ONE expense with total amount
	expense := &domain.Expense{
		Name:         analysis.Summary,
		Price:        analysis.Total,
		Currency:     analysis.Currency,
		Category:     domain.Category食, // Default to food
		Method:       domain.PaymentMethodCash,
		PaidByID:     u.NotionID,
		ShoppedAt:    time.Now(),
		ReceiptURL:   receiptURL,
		ReceiptItems: analysis.Items,
	}

	if err := h.accountingRepo.CreateExpense(ctx, expense); err != nil {
		slog.Error("Failed to create expense", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send("❌ 新增失敗，請稍後再試")
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	return c.Send(fmt.Sprintf(`✅ 已新增消費記錄

📝 %s
💰 %d %s
📂 %s %s
💳 %s`,
		expense.Name, expense.Price, expense.Currency, expense.Category.Emoji(), expense.Category, expense.Method.DisplayName()))
}

func (h *Handler) handleReceiptSplit(c tele.Context, state *ConversationState) error {
	defer h.convManager.ClearState(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get user's Notion ID
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "取得用戶失敗"})
		return c.Send("❌ 無法取得用戶資訊")
	}

	analysis := state.ReceiptAnalysis
	imageData := state.ReceiptImage

	// Upload photo to Notion
	receiptURL := ""
	if len(imageData) > 0 {
		tmpFile, err := os.CreateTemp("", "receipt-*.jpg")
		if err != nil {
			slog.Error("Failed to create temp file", "error", err)
		} else {
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write(imageData); err != nil {
				slog.Error("Failed to write image to temp file", "error", err)
			} else {
				tmpFile.Close()
				if uploadID, err := h.accountingRepo.UploadFile(ctx, tmpFile.Name()); err != nil {
					slog.Error("Failed to upload receipt", "error", err)
				} else {
					receiptURL = uploadID
				}
			}
		}
	}

	// Create MULTIPLE expenses (one per item)
	successCount := 0
	for _, item := range analysis.Items {
		expense := &domain.Expense{
			Name:       item.DisplayName(),
			Price:      item.Price,
			Currency:   analysis.Currency,
			Category:   item.Category,
			Method:     domain.PaymentMethodCash,
			PaidByID:   u.NotionID,
			ShoppedAt:  time.Now(),
			ReceiptURL: receiptURL,
		}

		if err := h.accountingRepo.CreateExpense(ctx, expense); err != nil {
			slog.Error("Failed to create expense", "error", err, "item", item.Name)
			continue
		}
		successCount++
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	var msgText string
	if successCount == len(analysis.Items) {
		msgText = fmt.Sprintf("✅ 已拆開新增 %d 項消費\n\n", successCount)
		for i, item := range analysis.Items {
			msgText += fmt.Sprintf("%d. %s %s %d %s\n", i+1, item.Category.Emoji(), item.DisplayName(), item.Price, analysis.Currency)
		}
	} else {
		msgText = fmt.Sprintf("⚠️ 成功新增 %d/%d 項\n\n部分項目新增失敗，請稍後重試", successCount, len(analysis.Items))
	}

	return c.Send(msgText)
}

func (h *Handler) handleToday(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get today's date range (local timezone)
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())

	filter := domain.ExpenseFilter{
		DateFrom: &startOfDay,
		DateTo:   &endOfDay,
	}

	expenses, err := h.accountingRepo.QueryExpensesWithFilter(ctx, filter)
	if err != nil {
		slog.Error("Failed to query today's expenses", "error", err)
		return c.Send("❌ 查詢失敗，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send(fmt.Sprintf("📅 %s 消費統計\n\n📭 今日尚無消費記錄", now.Format("2006/01/02")))
	}

	// Fetch fallback exchange rate for JPY expenses without rate
	var fallbackJPYRate decimal.Decimal
	needsFallbackRate := false
	for _, exp := range expenses {
		if exp.Currency == domain.CurrencyJPY && exp.ExchangeRate.IsZero() {
			needsFallbackRate = true
			break
		}
	}

	if needsFallbackRate && h.rateFetcher != nil {
		rate, err := h.rateFetcher.GetRate(ctx, domain.CurrencyJPY)
		if err != nil {
			slog.Warn("Failed to fetch fallback exchange rate", "error", err)
		} else {
			fallbackJPYRate = rate
		}
	}

	type categorySum struct {
		total    uint64
		totalTWD float64
		currency domain.Currency
	}

	// Aggregate by category
	categoryTotals := make(map[domain.Category]*categorySum)
	var grandTotalTWD float64

	for _, exp := range expenses {
		if _, ok := categoryTotals[exp.Category]; !ok {
			categoryTotals[exp.Category] = &categorySum{}
		}
		categoryTotals[exp.Category].total += exp.Price
		categoryTotals[exp.Category].currency = exp.Currency

		// Calculate TWD amount, using fallback rate if needed
		var twdAmount float64
		if exp.Currency == domain.CurrencyJPY && exp.ExchangeRate.IsZero() && !fallbackJPYRate.IsZero() {
			// Use fallback rate for expenses without stored rate
			twdAmount, _ = exp.PriceDecimal().Mul(fallbackJPYRate).Float64()
		} else {
			twdAmount, _ = exp.TotalInTWD().Float64()
		}
		categoryTotals[exp.Category].totalTWD += twdAmount
		grandTotalTWD += twdAmount
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 %s 消費統計\n\n", now.Format("2006/01/02")))

	// Sort categories for consistent output
	categories := domain.CategoryValues()
	for _, cat := range categories {
		if sum, ok := categoryTotals[cat]; ok {
			sb.WriteString(fmt.Sprintf("%s %s: %d %s (≈NT$%.0f)\n",
				cat.Emoji(), cat, sum.total, sum.currency, sum.totalTWD))
		}
	}

	sb.WriteString("─────────────\n")
	sb.WriteString(fmt.Sprintf("💰 今日合計: NT$%.0f (%d 筆)\n", grandTotalTWD, len(expenses)))

	return c.Send(sb.String())
}

func makeCategoryKeyboard(actionPrefix string) *tele.ReplyMarkup {
	keyboard := &tele.ReplyMarkup{}
	categories := domain.CategoryValues()
	var rows []tele.Row
	var currentRow []tele.Btn

	for i, cat := range categories {
		btn := keyboard.Data(
			fmt.Sprintf("%s %s", cat.Emoji(), cat),
			actionPrefix,
			string(cat),
		)
		currentRow = append(currentRow, btn)

		// 3 buttons per row, or last row
		if len(currentRow) == 3 || i == len(categories)-1 {
			rows = append(rows, keyboard.Row(currentRow...))
			currentRow = []tele.Btn{}
		}
	}
	keyboard.Inline(rows...)
	return keyboard
}

func makePaymentMethodKeyboard(actionPrefix string) *tele.ReplyMarkup {
	keyboard := &tele.ReplyMarkup{}
	methods := domain.PaymentMethodValues()
	var rows []tele.Row
	var currentRow []tele.Btn

	for i, method := range methods {
		btn := keyboard.Data(
			method.DisplayName(),
			actionPrefix,
			string(method),
		)
		currentRow = append(currentRow, btn)

		// 2 buttons per row for payment methods
		if len(currentRow) == 2 || i == len(methods)-1 {
			rows = append(rows, keyboard.Row(currentRow...))
			currentRow = []tele.Btn{}
		}
	}
	keyboard.Inline(rows...)
	return keyboard
}
