package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/shopspring/decimal"
	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type Handler struct {
	userService    *user.Service
	expenseService *expense.Service
	tripService    *trip.Service
	webAppURL      string
	convManager    *ConversationManager
	tzMu           sync.RWMutex
	timezones      map[int64]*time.Location
}

func NewHandler(
	userService *user.Service,
	expenseService *expense.Service,
	tripService *trip.Service,
	webAppURL string,
) *Handler {
	return &Handler{
		userService:    userService,
		expenseService: expenseService,
		tripService:    tripService,
		webAppURL:      webAppURL,
		convManager:    NewConversationManager(),
		timezones:      make(map[int64]*time.Location),
	}
}

func (h *Handler) RegisterHandlers(bot *tele.Bot) {
	bot.Use(h.requireAuthorized)

	bot.Handle("/start", h.handleStart)
	bot.Handle("/help", h.handleHelp)
	bot.Handle("/trip", h.handleTrip)
	bot.Handle("/rate", h.handleRate)
	bot.Handle("/summary", h.handleSummary)
	bot.Handle("/today", h.handleToday)
	bot.Handle("/tz", h.handleTimezone)
	bot.Handle("/timezone", h.handleTimezone)
	bot.Handle("/edit", h.handleEdit)
	bot.Handle("/cancel", h.handleCancel)

	bot.Handle(tele.OnText, h.handleText)
	bot.Handle(tele.OnPhoto, h.handlePhoto)
	bot.Handle(tele.OnCallback, h.handleCallback)
}

// requireAuthorized lets only the people in the user mapping through. Every
// command reads or changes the trip's real expenses, so none is open to someone
// who merely found the bot.
func (h *Handler) requireAuthorized(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		sender := c.Sender()
		if sender != nil && h.userService.IsAuthorized(sender.ID) {
			return next(c)
		}

		if sender != nil {
			slog.Warn("Ignoring an update from someone who is not in the user mapping", "telegram_id", sender.ID)
		}
		if c.Callback() != nil {
			return c.Respond(&tele.CallbackResponse{Text: "未授權的使用者"})
		}
		return c.Send("❌ 未授權的使用者")
	}
}

// currentTrip is the trip the sender is filing under. When there is none it has
// already told them why, so the caller only has to stop.
func (h *Handler) currentTrip(c tele.Context) (domain.Trip, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	current, err := h.tripService.Current(ctx, c.Sender().ID)
	if err == nil {
		return current, true
	}

	if errors.Is(err, trip.ErrNoTrip) {
		_ = c.Send("📬 目前沒有可用的旅行\n\n請先在 TREK 把記帳帳號加入旅行")
		return domain.Trip{}, false
	}
	slog.Error("Failed to read the trips", "error", err)
	_ = c.Send("❌ 讀取旅行失敗，請稍後再試")
	return domain.Trip{}, false
}

// writeFailure is what a person is told when an expense could not be written. A
// payer or participant who is not on the trip is a thing they can fix, so it is
// named; anything else is not theirs to act on.
func writeFailure(err error, generic string) string {
	if errors.Is(err, domain.ErrNotOnTrip) {
		return "❌ 付款人或分攤的人不在這趟旅行裡\n\n請先在 TREK 把對方加入旅行"
	}
	return generic
}

func (h *Handler) handleStart(c tele.Context) error {
	msg := `🧾 旅行記帳 Bot

使用 /help 查看所有指令`

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if current, err := h.tripService.Current(ctx, c.Sender().ID); err == nil {
		msg += "\n\n" + tripLine(current) + "\n使用 /trip 切換旅行"
	}

	if h.webAppURL != "" {
		webapp := &tele.WebApp{URL: h.webAppURL}
		btn := tele.InlineButton{
			Text:   "📝 開啟記帳 App",
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
	labels := make([]string, 0, len(domain.CategoryValues()))
	for _, category := range domain.CategoryValues() {
		labels = append(labels, format.CategoryLabel(category))
	}
	methods := make([]string, 0, len(domain.PaymentMethodValues()))
	for _, method := range domain.PaymentMethodValues() {
		methods = append(methods, format.PaymentLabel(method))
	}

	help := fmt.Sprintf(`📖 指令說明

/trip
  查看並切換目前記帳的旅行

/today
  查看今日消費統計

/edit
  編輯最近的消費記錄

/summary
  查看結算：誰該付給誰

/rate
  查看目前匯率

/tz [時區]
  查看並切換統計時區（台灣 Asia/Taipei 或日本 Asia/Tokyo）

/cancel
  取消目前的編輯操作

📸 傳送收據照片
  直接發送收據照片自動辨識記帳

💡 新增消費與完整圖表請使用 Mini App 操作。

📌 分類: %s
💳 付款方式: %s
💰 幣別: %s`, strings.Join(labels, ", "), strings.Join(methods, ", "), strings.Join(domain.CurrencyNames(), ", "))

	return c.Send(help)
}

// handleTrip shows the trip the sender files under and offers the others.
func (h *Handler) handleTrip(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	trips, err := h.tripService.List(ctx)
	if err != nil {
		slog.Error("Failed to list the trips", "error", err)
		return c.Send("❌ 讀取旅行失敗，請稍後再試")
	}
	if len(trips) == 0 {
		return c.Send("📬 目前沒有可用的旅行\n\n請先在 TREK 把記帳帳號加入旅行")
	}

	current, err := h.tripService.Current(ctx, c.Sender().ID)
	if err != nil {
		slog.Error("Failed to read the current trip", "error", err)
		return c.Send("❌ 讀取旅行失敗，請稍後再試")
	}

	keyboard := &tele.ReplyMarkup{}
	rows := make([]tele.Row, 0, len(trips))
	for _, candidate := range trips {
		label := truncate(format.Trip(candidate), 40)
		if candidate.ID == current.ID {
			label = "✅ " + label
		}
		rows = append(rows, keyboard.Row(keyboard.Data(label, "trip", strconv.FormatInt(candidate.ID, 10))))
	}
	keyboard.Inline(rows...)

	return c.Send(fmt.Sprintf("目前記帳的旅行：\n%s\n\n切換到：", tripLine(current)), keyboard)
}

func (h *Handler) handleTripCallback(c tele.Context, data string) error {
	_, value, _ := strings.Cut(data, "|")
	tripID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	selected, err := h.tripService.Select(ctx, c.Sender().ID, tripID)
	if err != nil {
		if errors.Is(err, trip.ErrTripNotFound) {
			return c.Respond(&tele.CallbackResponse{Text: "這趟旅行已經不在清單裡"})
		}
		slog.Error("Failed to select a trip", "error", err)
		return c.Respond(&tele.CallbackResponse{Text: "切換失敗"})
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "已切換"})
	return c.Send("✅ 已切換到\n" + tripLine(selected))
}

func (h *Handler) handleRate(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return c.Send(rateMessage(h.expenseService.ExchangeRates(ctx)))
}

// handleSummary reports where the trip's money stands, as TREK works it out.
func (h *Handler) handleSummary(c tele.Context) error {
	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	settlement, err := h.expenseService.Settlement(ctx, current)
	if err != nil {
		slog.Error("Failed to get the settlement", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	names := map[string]string{}
	if users, err := h.userService.GetAllUsers(); err == nil {
		for _, u := range users {
			names[u.BackendUserID] = u.Nickname
		}
	}

	return c.Send(settlementMessage(current, settlement, names))
}

func (h *Handler) handleCancel(c tele.Context) error {
	h.convManager.ClearState(c.Sender().ID)
	return c.Send("❌ 已取消操作")
}

func (h *Handler) renderReceiptMessage(current domain.Trip, analysis *domain.ReceiptAnalysis) (string, *tele.ReplyMarkup) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "📸 %s\n%s\n\n", analysis.Summary, tripLine(current))
	for i, item := range analysis.Items {
		fmt.Fprintf(&sb, "%d. %s %s %s\n",
			i+1, format.CategoryEmoji(item.Category), format.ReceiptName(item), format.Money(analysis.Currency, decimal.NewFromInt(item.Price)))
	}
	fmt.Fprintf(&sb, "\n💰 合計: %s\n", format.Money(analysis.Currency, decimal.NewFromUint64(analysis.Total)))
	fmt.Fprintf(&sb, "📂 分類: %s\n", format.Category(analysis.Category))
	fmt.Fprintf(&sb, "💳 付款方式: %s\n", format.PaymentLabel(analysis.PaymentMethod))

	keyboard := &tele.ReplyMarkup{}
	btnSingle := keyboard.Data("📦 整筆記", "receipt", "single")
	btnSplit := keyboard.Data("📋 拆開記", "receipt", "split")
	btnToggleMethod := keyboard.Data("💳 切換付款方式 ("+format.PaymentLabel(analysis.PaymentMethod)+")", "receipt", "toggle_method")
	btnCancel := keyboard.Data("❌ 取消", "receipt", "cancel")
	keyboard.Inline(
		keyboard.Row(btnSingle, btnSplit),
		keyboard.Row(btnToggleMethod),
		keyboard.Row(btnCancel),
	)
	return sb.String(), keyboard
}

func (h *Handler) handlePhoto(c tele.Context) error {
	if !h.expenseService.HasReceiptAnalyzer() {
		return c.Send("📸 收據分析功能尚未啟用")
	}

	photo := c.Message().Photo
	if photo == nil {
		return c.Send("❌ 無法取得照片")
	}

	if state := h.convManager.GetState(c.Sender().ID); state != nil && state.Step == ReceiptConfirm {
		return c.Send("⚠️ 您還有一筆收據待確認，請先完成或取消後再傳新照片\n/cancel")
	}

	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	if err := c.Send("🔍 正在分析收據..."); err != nil {
		slog.Warn("Failed to send analysis message", "error", err)
	}

	reader, err := c.Bot().File(&photo.File)
	if err != nil {
		slog.Error("Failed to download photo", "error", err)
		return c.Send("❌ 無法下載照片")
	}

	imageData, err := io.ReadAll(reader)
	if err != nil {
		slog.Error("Failed to read photo data", "error", err)
		return c.Send("❌ 無法讀取照片")
	}
	if len(imageData) == 0 {
		return c.Send("❌ 無法讀取照片")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	slog.Debug("Downloaded photo from Telegram", "bytes", len(imageData))

	analysis, err := h.expenseService.AnalyzeReceipt(ctx, imageData)
	if err != nil {
		slog.Error("Receipt analysis failed", "error", err)
		return c.Send("❌ 無法辨識收據，請使用 Mini App 手動新增")
	}

	h.convManager.SetState(c.Sender().ID, &ConversationState{
		Step:            ReceiptConfirm,
		Trip:            current,
		ReceiptAnalysis: analysis,
		ReceiptImage:    imageData,
		ReceiptCategory: analysis.Category,
		ReceiptMethod:   analysis.PaymentMethod,
	})

	msg, keyboard := h.renderReceiptMessage(current, analysis)
	return c.Send(msg, keyboard)
}

func (h *Handler) handleText(c tele.Context) error {
	userID := c.Sender().ID
	state := h.convManager.GetState(userID)

	if state == nil {
		return nil // No active conversation, ignore
	}

	text := strings.TrimSpace(c.Text())

	switch state.Step {
	case StepEditValue:
		return h.handleEditValue(c, state, text)

	case StepNone, StepEditSelect, StepEditField, ReceiptConfirm:
		// These steps accept callbacks rather than text input.
		return nil
	}

	return nil
}

func (h *Handler) handleCallback(c tele.Context) error {
	userID := c.Sender().ID
	state := h.convManager.GetState(userID)
	data := c.Callback().Data

	// telebot v4 prefixes callback data with \f (form feed character)
	data = strings.TrimPrefix(data, "\f")

	// Handle trip selection (format: "trip|{trip_id}")
	if strings.HasPrefix(data, "trip|") {
		return h.handleTripCallback(c, data)
	}

	// Handle timezone selection (format: "tz|{timezone}")
	if strings.HasPrefix(data, "tz|") {
		return h.handleTimezoneCallback(c, data)
	}

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
	case "edit_cat":
		return h.handleEditCategory(c, state, value)
	case "edit_method":
		return h.handleEditMethod(c, state, value)
	}

	return c.Respond(&tele.CallbackResponse{Text: "未知操作"})
}

func (h *Handler) handleEdit(c tele.Context) error {
	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	limit := 5
	expenses, err := h.expenseService.QueryExpensesWithFilter(ctx, current, expense.ExpenseFilter{Limit: &limit})
	if err != nil {
		slog.Error("Failed to query expenses for edit", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send("📬 目前沒有消費記錄可編輯")
	}

	keyboard := &tele.ReplyMarkup{}
	var rows []tele.Row

	for _, exp := range expenses {
		label := truncate(fmt.Sprintf("%s %s (%s)", exp.Name, format.Money(exp.Currency, exp.PriceDecimal()), format.CategoryLabel(exp.Category)), 36)
		btn := keyboard.Data(label, "edit_select", exp.ID)
		rows = append(rows, keyboard.Row(btn))
	}

	rows = append(rows, keyboard.Row(keyboard.Data("❌ 取消", "edit_select", "cancel")))
	keyboard.Inline(rows...)

	return c.Send(fmt.Sprintf("📝 請選擇要編輯的消費記錄：\n%s", tripLine(current)), keyboard)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	current, err := h.tripService.Current(ctx, c.Sender().ID)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "讀取旅行失敗"})
	}

	limit := 20
	expenses, err := h.expenseService.QueryExpensesWithFilter(ctx, current, expense.ExpenseFilter{Limit: &limit})
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

	h.convManager.StartEditFlow(c.Sender().ID, current, targetExpense)

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇"})

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
%s

📝 名稱: %s
💰 金額: %s%s
📂 分類: %s
💳 付款: %s
📅 日期: %s

請選擇要修改的欄位：`, tripLine(current), exp.Name, format.Money(exp.Currency, exp.PriceDecimal()), convertedSuffix(current, exp),
		format.Category(exp.Category), format.PaymentLabel(exp.Method), exp.ShoppedAt.Format("2006/01/02"))

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
		priceDec, err := decimal.NewFromString(text)
		if err != nil || !priceDec.IsPositive() {
			return c.Send("❌ 金額格式錯誤，請輸入大於 0 的金額：")
		}
		rounded := priceDec.Round(0)
		if !rounded.IsPositive() || !rounded.BigInt().IsUint64() {
			return c.Send("❌ 金額格式錯誤，請輸入大於 0 的金額：")
		}
		exp.Price = rounded.BigInt().Uint64()
	default:
		return c.Send("❌ 未知的欄位")
	}

	return h.saveEdit(c, state, "更新失敗")
}

func (h *Handler) saveEdit(c tele.Context, state *ConversationState, failure string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exp := state.EditingExpense
	if err := h.expenseService.UpdateExpense(ctx, state.Trip, exp); err != nil {
		if errors.Is(err, domain.ErrUnsupportedSplit) {
			return c.Send("這筆消費使用自訂分攤，請到 TREK 修改")
		}
		slog.Error("Failed to update expense", "error", err)
		if c.Callback() != nil {
			_ = c.Respond(&tele.CallbackResponse{Text: failure})
		}
		return c.Send(writeFailure(err, "❌ 更新失敗，請稍後再試"))
	}

	h.convManager.ClearState(c.Sender().ID)
	if c.Callback() != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "已更新"})
	}

	return c.Send(expenseCard("✅ 已更新消費記錄", state.Trip, exp))
}

func (h *Handler) handleEditDelete(c tele.Context, state *ConversationState) error {
	if state == nil || state.EditingExpense == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exp := state.EditingExpense
	if err := h.expenseService.DeleteExpense(ctx, state.Trip, exp.ID); err != nil {
		slog.Error("Failed to delete expense", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "刪除失敗"})
		return c.Send("❌ 刪除失敗，請稍後再試")
	}

	h.convManager.ClearState(c.Sender().ID)
	_ = c.Respond(&tele.CallbackResponse{Text: "已刪除"})

	return c.Send(fmt.Sprintf("✅ 已刪除消費記錄：%s %s", exp.Name, format.Money(exp.Currency, exp.PriceDecimal())))
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
	return h.saveEdit(c, state, "更新失敗")
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
	return h.saveEdit(c, state, "更新失敗")
}

func (h *Handler) handleReceiptCallback(c tele.Context, state *ConversationState, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}

	action := parts[1]

	if action == "cancel" {
		h.convManager.ClearState(c.Sender().ID)
		if msg := c.Message(); msg != nil {
			_, _ = c.Bot().Edit(msg, msg.Text)
		}
		_ = c.Respond(&tele.CallbackResponse{Text: "已取消"})
		return c.Send("❌ 已取消操作")
	}

	if state == nil || state.ReceiptAnalysis == nil {
		return c.Respond(&tele.CallbackResponse{Text: "會話已過期"})
	}

	if action == "toggle_method" {
		var nextMethod domain.PaymentMethod
		switch state.ReceiptAnalysis.PaymentMethod {
		case domain.PaymentMethodCash:
			nextMethod = domain.PaymentMethodCreditCard
		case domain.PaymentMethodCreditCard:
			nextMethod = domain.PaymentMethodIcCard
		case domain.PaymentMethodIcCard:
			nextMethod = domain.PaymentMethodEPay
		case domain.PaymentMethodEPay:
			nextMethod = domain.PaymentMethodCash
		default:
			nextMethod = domain.PaymentMethodCash
		}
		state.ReceiptAnalysis.PaymentMethod = nextMethod
		_ = c.Respond(&tele.CallbackResponse{Text: "付款方式切換為: " + format.PaymentLabel(nextMethod)})
		msg, keyboard := h.renderReceiptMessage(state.Trip, state.ReceiptAnalysis)
		if originalMsg := c.Message(); originalMsg != nil {
			_, err := c.Bot().Edit(originalMsg, msg, keyboard)
			return err
		}
		return c.Send(msg, keyboard)
	}

	// Remove inline buttons from the original message for final actions
	if msg := c.Message(); msg != nil {
		_, _ = c.Bot().Edit(msg, msg.Text)
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

	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "取得用戶失敗"})
		return c.Send("❌ 無法取得用戶資訊")
	}

	analysis := state.ReceiptAnalysis
	if err := h.expenseService.CreateFromAnalysis(ctx, state.Trip, receiptCommand(analysis), state.ReceiptImage, u.BackendUserID, false); err != nil {
		slog.Error("Failed to create expense from receipt", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send(writeFailure(err, "❌ 新增失敗，請稍後再試"))
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	return c.Send(expenseCard("✅ 已新增消費記錄", state.Trip, &domain.Expense{
		Name:     analysis.Summary,
		Price:    analysis.Total,
		Currency: analysis.Currency,
		Category: analysis.Category,
		Method:   analysis.PaymentMethod,
	}))
}

func (h *Handler) handleReceiptSplit(c tele.Context, state *ConversationState) error {
	defer h.convManager.ClearState(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "取得用戶失敗"})
		return c.Send("❌ 無法取得用戶資訊")
	}

	analysis := state.ReceiptAnalysis
	if err := h.expenseService.CreateFromAnalysis(ctx, state.Trip, receiptCommand(analysis), state.ReceiptImage, u.BackendUserID, true); err != nil {
		slog.Error("Failed to create expenses from receipt", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send(writeFailure(err, "❌ 新增失敗，請稍後再試"))
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ 已拆開新增 %d 項消費\n%s\n\n", len(analysis.Items), tripLine(state.Trip))
	for i, item := range analysis.Items {
		fmt.Fprintf(&sb, "%d. %s %s %s\n",
			i+1, format.CategoryEmoji(item.Category), format.ReceiptName(item), format.Money(analysis.Currency, decimal.NewFromInt(item.Price)))
	}
	fmt.Fprintf(&sb, "\n💳 付款方式: %s", format.PaymentLabel(analysis.PaymentMethod))
	return c.Send(sb.String())
}

func (h *Handler) handleToday(c tele.Context) error {
	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loc := h.getTimezone(c.Sender().ID)
	summary, err := h.expenseService.GetTodaySummary(ctx, current, loc)
	if err != nil {
		slog.Error("Failed to get today's summary", "error", err)
		return c.Send("❌ 查詢失敗，請稍後再試")
	}

	tzNotice := fmt.Sprintf("（%s）", loc.String())
	if len(summary.Items) == 0 {
		return c.Send(fmt.Sprintf("📅 %s %s消費統計\n%s\n\n📬 今日尚無消費記錄", summary.Date.Format("2006/01/02"), tzNotice, tripLine(current)))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📅 %s %s消費統計\n%s\n\n", summary.Date.Format("2006/01/02"), tzNotice, tripLine(current))

	for _, item := range summary.Items {
		fmt.Fprintf(&sb, "%s: %s\n", format.Category(item.Category), format.Money(summary.Currency, item.Total))
	}

	sb.WriteString("─────────────\n")
	fmt.Fprintf(&sb, "💰 今日合計: %s (%d 筆)\n", format.Money(summary.Currency, summary.GrandTotal), summary.ItemCount)

	return c.Send(sb.String())
}

func makeCategoryKeyboard(actionPrefix string) *tele.ReplyMarkup {
	keyboard := &tele.ReplyMarkup{}
	categories := domain.CategoryValues()
	var rows []tele.Row
	var currentRow []tele.Btn

	for i, cat := range categories {
		btn := keyboard.Data(
			format.Category(cat),
			actionPrefix,
			string(cat),
		)
		currentRow = append(currentRow, btn)

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
			format.PaymentEmoji(method)+" "+format.PaymentLabel(method),
			actionPrefix,
			string(method),
		)
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 || i == len(methods)-1 {
			rows = append(rows, keyboard.Row(currentRow...))
			currentRow = []tele.Btn{}
		}
	}
	keyboard.Inline(rows...)
	return keyboard
}

func receiptCommand(analysis *domain.ReceiptAnalysis) *domain.ReceiptAnalysis {
	command := *analysis
	command.Items = slices.Clone(analysis.Items)
	for i, item := range analysis.Items {
		command.Items[i].Name = format.ReceiptName(item)
		command.Items[i].NameZH = ""
	}
	return &command
}

func (h *Handler) getTimezone(userID int64) *time.Location {
	h.tzMu.RLock()
	defer h.tzMu.RUnlock()
	if loc, ok := h.timezones[userID]; ok && loc != nil {
		return loc
	}
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func (h *Handler) setTimezone(userID int64, loc *time.Location) {
	h.tzMu.Lock()
	defer h.tzMu.Unlock()
	h.timezones[userID] = loc
}

func (h *Handler) handleTimezone(c tele.Context) error {
	loc := h.getTimezone(c.Sender().ID)
	name := loc.String()
	offset := tzOffsetString(loc)

	payload := strings.TrimSpace(c.Message().Payload)
	if payload != "" {
		newLoc, err := parseTimezone(payload)
		if err != nil {
			return c.Send("❌ 無法辨識的時區名稱。支援例如：Asia/Taipei, Asia/Tokyo 或直接使用 /tz 點擊按鈕選擇")
		}
		h.setTimezone(c.Sender().ID, newLoc)
		return c.Send(fmt.Sprintf("✅ 已將時區設定為：%s (%s)", newLoc.String(), tzOffsetString(newLoc)))
	}

	keyboard := &tele.ReplyMarkup{}
	keyboard.Inline(
		keyboard.Row(
			keyboard.Data("🇹🇼 台灣 (UTC+8)", "tz", "Asia/Taipei"),
			keyboard.Data("🇯🇵 日本 (UTC+9)", "tz", "Asia/Tokyo"),
		),
	)

	return c.Send(fmt.Sprintf("🕒 目前統計時區：%s (%s)\n\n請選擇統計時區：", name, offset), keyboard)
}

func (h *Handler) handleTimezoneCallback(c tele.Context, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) < 2 {
		return c.Respond(&tele.CallbackResponse{Text: "無效的選擇"})
	}
	tzName := parts[1]
	newLoc, err := parseTimezone(tzName)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "無法辨識的時區"})
	}
	h.setTimezone(c.Sender().ID, newLoc)
	if err := c.Respond(&tele.CallbackResponse{Text: "已更新時區為 " + newLoc.String()}); err != nil {
		slog.Warn("Failed to respond callback", "error", err)
	}
	return c.Send(fmt.Sprintf("✅ 已將時區更新為：%s (%s)", newLoc.String(), tzOffsetString(newLoc)))
}

func parseTimezone(input string) (*time.Location, error) {
	cleaned := strings.TrimSpace(input)
	switch strings.ToLower(cleaned) {
	case "taipei", "taiwan", "asia/taipei", "utc+8", "+8", "gmt+8":
		return time.LoadLocation("Asia/Taipei")
	case "tokyo", "japan", "asia/tokyo", "utc+9", "+9", "gmt+9":
		return time.LoadLocation("Asia/Tokyo")
	default:
		return time.LoadLocation(cleaned)
	}
}

func tzOffsetString(loc *time.Location) string {
	_, offset := time.Now().In(loc).Zone()
	hours := offset / 3600
	mins := (offset % 3600) / 60
	if mins != 0 {
		return fmt.Sprintf("UTC%+d:%02d", hours, mins)
	}
	return fmt.Sprintf("UTC%+d", hours)
}
