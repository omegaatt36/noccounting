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
}

// NewHandler creates a new bot Handler.
func NewHandler(userService *user.Service, accountingRepo domain.AccountingRepo, webAppURL string) *Handler {
	return &Handler{
		userService:    userService,
		accountingRepo: accountingRepo,
		webAppURL:      webAppURL,
	}
}

// RegisterHandlers registers all bot command handlers.
func (h *Handler) RegisterHandlers(bot *tele.Bot) {
	bot.Handle("/start", h.handleStart)
	bot.Handle("/help", h.handleHelp)
	bot.Handle("/add", h.handleAdd)
	bot.Handle("/list", h.handleList)
	bot.Handle("/summary", h.handleSummary)
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

/list
  列出所有消費記錄

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

	sb.WriteString("👥 各人支出:\n")
	for userID, total := range summary.TotalByPayer {
		// Show abbreviated user ID for privacy
		shortID := userID
		if len(userID) > 8 {
			shortID = userID[:8] + "..."
		}
		sb.WriteString(fmt.Sprintf("  • %s: %s TWD\n", shortID, total.StringFixed(0)))
	}

	return c.Send(sb.String())
}
