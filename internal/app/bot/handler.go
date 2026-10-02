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
	"time"

	"github.com/shopspring/decimal"
	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// listLimit is how many expenses /list shows: a message is capped at 4096
// characters, and a long trip would otherwise fail to send at all.
const listLimit = 30

type Handler struct {
	userService    *user.Service
	expenseService *expense.Service
	tripService    *trip.Service
	webAppURL      string
	convManager    *ConversationManager
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
	}
}

func (h *Handler) RegisterHandlers(bot *tele.Bot) {
	bot.Use(h.requireAuthorized)

	bot.Handle("/start", h.handleStart)
	bot.Handle("/help", h.handleHelp)
	bot.Handle("/trip", h.handleTrip)
	bot.Handle("/rate", h.handleRate)
	bot.Handle("/add", h.handleAdd)
	bot.Handle("/list", h.handleList)
	bot.Handle("/summary", h.handleSummary)
	bot.Handle("/today", h.handleToday)
	bot.Handle("/quick", h.handleQuick)
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
		_ = c.Send("📭 目前沒有可用的旅行\n\n請先在 TREK 把記帳帳號加入旅行")
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

/add <名稱> <金額> <幣別> <分類> <付款方式> [日期]
  新增一筆消費記錄，日期選填，格式 2006-01-02
  範例: /add 拉麵 1200 JPY 餐飲 現金
        /add 拉麵 1200 JPY food cash 2026-05-01

/quick
  互動式新增消費（一步步引導）

/today
  查看今日消費統計

/list [付款方式]
  列出最近的消費記錄，可只看某種付款方式

/edit
  編輯最近的消費記錄

/summary
  查看結算：誰該付給誰

/rate
  查看目前匯率

📌 分類: %s
💳 付款方式: %s
💱 幣別: %s`, strings.Join(labels, ", "), strings.Join(methods, ", "), strings.Join(domain.CurrencyNames(), ", "))

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
		return c.Send("📭 目前沒有可用的旅行\n\n請先在 TREK 把記帳帳號加入旅行")
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

	// A conversation under way keeps the trip it started with.
	_ = c.Respond(&tele.CallbackResponse{Text: "已切換"})
	return c.Send("✅ 已切換到\n" + tripLine(selected))
}

func (h *Handler) handleRate(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return c.Send(rateMessage(h.expenseService.ExchangeRates(ctx)))
}

func (h *Handler) handleAdd(c tele.Context) error {
	args := c.Args()
	if len(args) < 5 {
		return c.Send(`❌ 格式錯誤

用法: /add <名稱> <金額> <幣別> <分類> <付款方式> [日期]
範例: /add 拉麵 1200 JPY 餐飲 現金
      /add 拉麵 1200 JPY food cash 2026-05-01`)
	}

	name := args[0]

	price, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return c.Send("❌ 金額格式錯誤，請輸入正整數")
	}

	currency := domain.Currency(strings.ToUpper(args[2]))
	if !currency.IsValid() {
		return c.Send(fmt.Sprintf("❌ 幣別錯誤，請使用: %s", strings.Join(domain.CurrencyNames(), ", ")))
	}

	category, err := format.ParseCategoryInput(args[3])
	if err != nil {
		return c.Send("❌ 分類錯誤，使用 /help 查看可用的分類")
	}

	method, err := format.ParsePaymentMethodInput(args[4])
	if err != nil {
		return c.Send("❌ 付款方式錯誤，使用 /help 查看可用的付款方式")
	}

	shoppedAt := time.Now()
	if len(args) >= 6 {
		t, err := time.Parse("2006-01-02", args[5])
		if err != nil {
			return c.Send("❌ 日期格式錯誤，請使用 2006-01-02（例如 2026-05-03）")
		}
		shoppedAt = t
	}

	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		return c.Send("❌ 無法取得用戶資訊，請確認您已註冊")
	}

	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	exp := &domain.Expense{
		Name:      name,
		Price:     price,
		Currency:  currency,
		Category:  category,
		Method:    method,
		PaidByID:  u.BackendUserID,
		ShoppedAt: shoppedAt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.expenseService.CreateExpense(ctx, current, exp); err != nil {
		slog.Error("Failed to create expense", "error", err)
		return c.Send(writeFailure(err, "❌ 新增失敗，連線資料庫錯誤，請稍後再試"))
	}

	return c.Send(expenseCard("✅ 已新增消費記錄", current, exp))
}

// handleList shows the latest expenses, optionally only those paid one way: the
// method rides in the expense's note, so this is the only place to ask for it.
func (h *Handler) handleList(c tele.Context) error {
	filter := expense.ExpenseFilter{}
	var heading string
	if args := c.Args(); len(args) > 0 {
		method, err := format.ParsePaymentMethodInput(args[0])
		if err != nil {
			return c.Send("❌ 付款方式錯誤，使用 /help 查看可用的付款方式")
		}
		filter.Method = &method
		heading = " · " + format.PaymentEmoji(method) + " " + format.PaymentLabel(method)
	}

	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expenses, err := h.expenseService.QueryExpensesWithFilter(ctx, current, filter)
	if err != nil {
		slog.Error("Failed to query expenses", "error", err)
		return c.Send("❌ 查詢失敗，連線資料庫錯誤，請稍後再試")
	}

	if len(expenses) == 0 {
		return c.Send("📭 目前沒有消費記錄")
	}

	var total decimal.Decimal
	for _, exp := range expenses {
		total = total.Add(exp.TotalInBase(current.Currency))
	}

	shown := expenses
	if len(shown) > listLimit {
		shown = shown[:listLimit]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 消費記錄%s\n%s\n\n", heading, tripLine(current))
	for _, exp := range shown {
		fmt.Fprintf(&sb, "• %s %s %s %s %s\n",
			exp.ShoppedAt.Format("01/02"), exp.Name, format.Money(exp.Currency, exp.PriceDecimal()),
			format.CategoryEmoji(exp.Category), format.PaymentEmoji(exp.Method))
	}
	if len(expenses) > len(shown) {
		fmt.Fprintf(&sb, "\n僅顯示最近 %d 筆，共 %d 筆\n", len(shown), len(expenses))
	}
	fmt.Fprintf(&sb, "\n💰 合計: %s（%d 筆）", format.Money(current.Currency, total), len(expenses))

	return c.Send(sb.String())
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

func (h *Handler) handleQuick(c tele.Context) error {
	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		return c.Send("❌ 無法取得用戶資訊，請確認您已註冊")
	}

	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	h.convManager.StartQuickFlow(telegramUserID, u.BackendUserID, current)

	return c.Send(fmt.Sprintf("📝 開始新增消費\n%s\n\n請輸入消費名稱：\n\n(輸入 /cancel 取消)", tripLine(current)))
}

func (h *Handler) handlePhoto(c tele.Context) error {
	if !h.expenseService.HasReceiptAnalyzer() {
		return c.Send("📸 收據分析功能尚未啟用")
	}

	photo := c.Message().Photo
	if photo == nil {
		return c.Send("❌ 無法取得照片")
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	analysis, err := h.expenseService.AnalyzeReceipt(ctx, imageData)
	if err != nil {
		slog.Error("Receipt analysis failed", "error", err)
		return c.Send("❌ 無法辨識收據，請嘗試手動輸入\n/quick")
	}

	h.convManager.SetState(c.Sender().ID, &ConversationState{
		Step:            ReceiptConfirm,
		Trip:            current,
		ReceiptAnalysis: analysis,
		ReceiptImage:    imageData,
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "📸 %s\n%s\n\n", analysis.Summary, tripLine(current))
	for i, item := range analysis.Items {
		fmt.Fprintf(&sb, "%d. %s %s %s\n",
			i+1, format.CategoryEmoji(item.Category), format.ReceiptName(item), format.Money(analysis.Currency, decimal.NewFromInt(item.Price)))
	}
	fmt.Fprintf(&sb, "\n合計: %s\n", format.Money(analysis.Currency, decimal.NewFromUint64(analysis.Total)))

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

	// Handle trip selection (format: "trip|{trip_id}")
	if strings.HasPrefix(data, "trip|") {
		return h.handleTripCallback(c, data)
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

	// A foreign expense is converted into the trip's currency at the rate of the
	// day; one in the trip's own currency needs none.
	if currency != state.Trip.Currency {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rate, err := h.expenseService.FetchExchangeRate(ctx, currency, state.Trip.Currency)
		if err != nil {
			slog.Warn("Failed to fetch exchange rate", "error", err)
		} else if !rate.IsZero() {
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

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + format.CategoryLabel(category)})

	return c.Send("💳 請選擇付款方式：", makePaymentMethodKeyboard("method"))
}

func (h *Handler) handleQuickMethod(c tele.Context, state *ConversationState, value string) error {
	method := domain.PaymentMethod(value)
	if !method.IsValid() {
		return c.Respond(&tele.CallbackResponse{Text: "無效的付款方式"})
	}

	state.ExpenseDraft.Method = method
	state.Step = StepQuickConfirm

	_ = c.Respond(&tele.CallbackResponse{Text: "已選擇 " + format.PaymentLabel(method)})

	exp := state.ExpenseDraft

	// Build confirmation message with optional exchange rate info
	var rateInfo string
	if exp.Currency != state.Trip.Currency && !exp.ExchangeRate.IsZero() {
		rateInfo = fmt.Sprintf("\n💱 匯率: %s", exp.ExchangeRate.StringFixed(4))
	}

	confirmMsg := fmt.Sprintf(`📋 確認消費資訊
%s

📝 名稱: %s
💰 金額: %s%s%s
📂 分類: %s
💳 付款: %s

確定要新增嗎？`, tripLine(state.Trip), exp.Name, format.Money(exp.Currency, exp.PriceDecimal()), convertedSuffix(state.Trip, exp), rateInfo,
		format.Category(exp.Category), format.PaymentLabel(exp.Method))

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
	if err := h.expenseService.CreateExpense(ctx, state.Trip, exp); err != nil {
		slog.Error("Failed to create expense", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send(writeFailure(err, "❌ 新增失敗，連線資料庫錯誤，請稍後再試"))
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	return c.Send(expenseCard("✅ 已新增消費記錄", state.Trip, exp))
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
		return c.Send("📭 目前沒有消費記錄可編輯")
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
		price, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return c.Send("❌ 金額格式錯誤，請輸入正整數：")
		}
		exp.Price = price
	default:
		return c.Send("❌ 未知的欄位")
	}

	return h.saveEdit(c, state, "更新失敗")
}

// saveEdit writes the expense an edit conversation has been changing and
// reports it, so the four ways to change one end the same way.
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

	telegramUserID := c.Sender().ID
	u, err := h.userService.GetUser(domain.GetUserRequest{
		TelegramID: &telegramUserID,
	})
	if err != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "取得用戶失敗"})
		return c.Send("❌ 無法取得用戶資訊")
	}

	if err := h.expenseService.CreateFromAnalysis(ctx, state.Trip, receiptCommand(state.ReceiptAnalysis), state.ReceiptImage, u.BackendUserID, false); err != nil {
		slog.Error("Failed to create expense from receipt", "error", err)
		_ = c.Respond(&tele.CallbackResponse{Text: "新增失敗"})
		return c.Send(writeFailure(err, "❌ 新增失敗，請稍後再試"))
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "新增成功！"})

	analysis := state.ReceiptAnalysis
	return c.Send(expenseCard("✅ 已新增消費記錄", state.Trip, &domain.Expense{
		Name:     analysis.Summary,
		Price:    analysis.Total,
		Currency: analysis.Currency,
		Category: domain.CategoryFood,
		Method:   domain.PaymentMethodCash,
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
	return c.Send(sb.String())
}

func (h *Handler) handleToday(c tele.Context) error {
	current, ok := h.currentTrip(c)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := h.expenseService.GetTodaySummary(ctx, current)
	if err != nil {
		slog.Error("Failed to get today's summary", "error", err)
		return c.Send("❌ 查詢失敗，請稍後再試")
	}

	if len(summary.Items) == 0 {
		return c.Send(fmt.Sprintf("📅 %s 消費統計\n%s\n\n📭 今日尚無消費記錄", summary.Date.Format("2006/01/02"), tripLine(current)))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📅 %s 消費統計\n%s\n\n", summary.Date.Format("2006/01/02"), tripLine(current))

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
			format.PaymentEmoji(method)+" "+format.PaymentLabel(method),
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

func receiptCommand(analysis *domain.ReceiptAnalysis) *domain.ReceiptAnalysis {
	command := *analysis
	command.Items = slices.Clone(analysis.Items)
	for i, item := range analysis.Items {
		command.Items[i].Name = format.ReceiptName(item)
		command.Items[i].NameZH = ""
	}
	return &command
}
