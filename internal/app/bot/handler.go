package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// Handler handles Telegram bot commands.
type Handler struct {
	userService    *user.Service
	accountingRepo domain.AccountingRepo
	webAppURL      string
	convManager    *ConversationManager
}

// NewHandler creates a new bot Handler.
func NewHandler(userService *user.Service, accountingRepo domain.AccountingRepo, webAppURL string) *Handler {
	return &Handler{
		userService:    userService,
		accountingRepo: accountingRepo,
		webAppURL:      webAppURL,
		convManager:    NewConversationManager(),
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
	help := `📖 指令說明

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

📌 分類選項: 食, 衣, 住, 行, 樂
💳 付款方式: cash, credit_card, ic_card, paypay
💱 幣別: TWD, JPY`

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
		return c.Send("❌ 分類錯誤，請使用: 食, 衣, 住, 行, 樂")
	}

	method := domain.PaymentMethod(strings.ToLower(args[4]))
	if !method.IsValid() {
		return c.Send("❌ 付款方式錯誤，請使用: cash, credit_card, ic_card, paypay")
	}

	// Get Notion user ID from mapping
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})

	if err != nil {
		return c.Send("❌ 無法取得用戶資訊")
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
		return c.Send("❌ 新增失敗，請稍後再試")
	}

	return c.Send(fmt.Sprintf(`✅ 已新增消費記錄

📝 %s
💰 %d %s
📂 %s
💳 %s`,
		name, price, currency, category, method.DisplayName()))
}

func (h *Handler) handleList(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expenses, err := h.accountingRepo.QueryExpenses(ctx)
	if err != nil {
		slog.Error("Failed to query expenses", "error", err)
		return c.Send("❌ 查詢失敗，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send("📭 目前沒有消費記錄")
	}

	var sb strings.Builder
	sb.WriteString("📋 消費記錄\n\n")

	for _, exp := range expenses {
		sb.WriteString(fmt.Sprintf("• %s: %d %s (%s)\n",
			exp.Name, exp.Price, exp.Currency, exp.Category))
	}

	return c.Send(sb.String())
}

func (h *Handler) handleSummary(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := h.accountingRepo.GetExpenseSummary(ctx)
	if err != nil {
		slog.Error("Failed to get summary", "error", err)
		return c.Send("❌ 查詢失敗，請稍後再試")
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
		return c.Send("❌ 無法取得用戶資訊")
	}

	h.convManager.StartQuickFlow(telegramUserID, u.NotionID)

	return c.Send("📝 開始新增消費\n\n請輸入消費名稱：\n\n(輸入 /cancel 取消)")
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
	state.Step = StepQuickCategory

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + value})

	// Show category keyboard
	keyboard := &tele.ReplyMarkup{}
	keyboard.Inline(
		keyboard.Row(
			keyboard.Data("🍜 食", "category", "食"),
			keyboard.Data("👔 衣", "category", "衣"),
			keyboard.Data("🏠 住", "category", "住"),
		),
		keyboard.Row(
			keyboard.Data("🚃 行", "category", "行"),
			keyboard.Data("🎮 樂", "category", "樂"),
		),
	)
	return c.Send("📂 請選擇分類：", keyboard)
}

func (h *Handler) handleQuickCategory(c tele.Context, state *ConversationState, value string) error {
	category := domain.Category(value)
	if !category.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的分類"})
	}

	state.ExpenseDraft.Category = category
	state.Step = StepQuickMethod

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + value})

	// Show payment method keyboard
	keyboard := &tele.ReplyMarkup{}
	keyboard.Inline(
		keyboard.Row(
			keyboard.Data("💵 現金", "method", "cash"),
			keyboard.Data("💳 信用卡", "method", "credit_card"),
		),
		keyboard.Row(
			keyboard.Data("🎫 IC卡", "method", "ic_card"),
			keyboard.Data("📱 PayPay", "method", "paypay"),
		),
	)
	return c.Send("💳 請選擇付款方式：", keyboard)
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
	confirmMsg := fmt.Sprintf(`📋 確認消費資訊

📝 名稱: %s
💰 金額: %d %s
📂 分類: %s
💳 付款: %s

確定要新增嗎？`, exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName())

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
		return c.Send("❌ 新增失敗，請稍後再試")
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	return c.Send(fmt.Sprintf(`✅ 已新增消費記錄

📝 %s
💰 %d %s
📂 %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName()))
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
		return c.Send("❌ 查詢失敗，請稍後再試")
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
		keyboard := &tele.ReplyMarkup{}
		keyboard.Inline(
			keyboard.Row(
				keyboard.Data("🍜 食", "edit_cat", "食"),
				keyboard.Data("👔 衣", "edit_cat", "衣"),
				keyboard.Data("🏠 住", "edit_cat", "住"),
			),
			keyboard.Row(
				keyboard.Data("🚃 行", "edit_cat", "行"),
				keyboard.Data("🎮 樂", "edit_cat", "樂"),
			),
		)
		return c.Send("請選擇新的分類：", keyboard)
	case "method":
		keyboard := &tele.ReplyMarkup{}
		keyboard.Inline(
			keyboard.Row(
				keyboard.Data("💵 現金", "edit_method", "cash"),
				keyboard.Data("💳 信用卡", "edit_method", "credit_card"),
			),
			keyboard.Row(
				keyboard.Data("🎫 IC卡", "edit_method", "ic_card"),
				keyboard.Data("📱 PayPay", "edit_method", "paypay"),
			),
		)
		return c.Send("請選擇新的付款方式：", keyboard)
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
📂 %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName()))
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
📂 %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName()))
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
📂 %s
💳 %s`,
		exp.Name, exp.Price, exp.Currency, exp.Category, exp.Method.DisplayName()))
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

	// Group by category and calculate totals
	categoryEmoji := map[domain.Category]string{
		domain.Category食: "🍜",
		domain.Category衣: "👔",
		domain.Category住: "🏠",
		domain.Category行: "🚃",
		domain.Category樂: "🎮",
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

		twdAmount, _ := exp.TotalInTWD().Float64()
		categoryTotals[exp.Category].totalTWD += twdAmount
		grandTotalTWD += twdAmount
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 %s 消費統計\n\n", now.Format("2006/01/02")))

	// Sort categories for consistent output
	categories := []domain.Category{domain.Category食, domain.Category衣, domain.Category住, domain.Category行, domain.Category樂}
	for _, cat := range categories {
		if sum, ok := categoryTotals[cat]; ok {
			emoji := categoryEmoji[cat]
			sb.WriteString(fmt.Sprintf("%s %s: %d %s (≈NT$%.0f)\n",
				emoji, cat, sum.total, sum.currency, sum.totalTWD))
		}
	}

	sb.WriteString("─────────────\n")
	sb.WriteString(fmt.Sprintf("💰 今日合計: NT$%.0f (%d 筆)\n", grandTotalTWD, len(expenses)))

	return c.Send(sb.String())
}
