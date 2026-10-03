package bot

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

const (
	PollerLongPolling    = "long"
	PollerWebhook        = "webhook"
	defaultWebhookListen = ":8081"
)

// Config holds the Telegram bot's settings.
type Config struct {
	Token              string
	WebAppURL          string
	Poller             string
	WebhookPublicURL   string
	WebhookListen      string
	WebhookSecretToken string
}

// commands is the menu Telegram clients show. Keep it in step with the routes in
// Handler.RegisterHandlers.
var commands = []tele.Command{
	{Text: "start", Description: "開始使用"},
	{Text: "help", Description: "指令說明"},
	{Text: "trip", Description: "查看並切換旅行"},
	{Text: "add", Description: "新增一筆消費"},
	{Text: "quick", Description: "互動式新增消費"},
	{Text: "today", Description: "今日消費統計"},
	{Text: "list", Description: "最近的消費記錄"},
	{Text: "edit", Description: "編輯最近的消費"},
	{Text: "summary", Description: "結算：誰該付給誰"},
	{Text: "rate", Description: "目前匯率"},
	{Text: "cancel", Description: "取消目前的操作"},
}

// Bot wraps the Telegram bot and its lifecycle management.
type Bot struct {
	cfg     Config
	bot     *tele.Bot
	webhook *tele.Webhook // nil unless Poller is PollerWebhook
	handler *Handler
}

// New creates and initializes a new Telegram bot.
func New(
	cfg Config,
	userService *user.Service,
	expenseService *expense.Service,
	tripService *trip.Service,
) (*Bot, error) {
	pref := tele.Settings{
		Token:   cfg.Token,
		OnError: onError,
	}

	var webhook *tele.Webhook
	switch cfg.Poller {
	case "", PollerLongPolling:
		pref.Poller = &tele.LongPoller{Timeout: 10 * time.Second}
	case PollerWebhook:
		if cfg.WebhookPublicURL == "" {
			return nil, errors.New("webhook public URL is required when poller=webhook (set WEBHOOK_PUBLIC_URL)")
		}
		if cfg.WebhookSecretToken == "" {
			return nil, errors.New("webhook secret token is required when poller=webhook (set WEBHOOK_SECRET_TOKEN)")
		}
		webhook = &tele.Webhook{
			Listen:           cfg.WebhookListen,
			SecretToken:      cfg.WebhookSecretToken,
			Endpoint:         &tele.WebhookEndpoint{PublicURL: cfg.WebhookPublicURL},
			AllowedUpdates:   tele.AllowedUpdates,
			IgnoreSetWebhook: true,
		}
		if webhook.Listen == "" {
			webhook.Listen = defaultWebhookListen
		}
		pref.Poller = webhook
	default:
		return nil, fmt.Errorf("unknown poller %q: want %q or %q", cfg.Poller, PollerLongPolling, PollerWebhook)
	}

	teleBot, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	handler := NewHandler(userService, expenseService, tripService, cfg.WebAppURL)
	handler.RegisterHandlers(teleBot)

	return &Bot{
		cfg:     cfg,
		bot:     teleBot,
		webhook: webhook,
		handler: handler,
	}, nil
}

// onError sends telebot's handler errors to the structured log, with the update
// they came from, instead of telebot's plain std logger.
func onError(err error, c tele.Context) {
	if c == nil {
		slog.Error("Telegram handler error", "error", err)
		return
	}
	slog.Error("Telegram handler error", "update_id", c.Update().ID, "error", err)
}

// Start tells Telegram how to reach the bot and then serves updates in a
// goroutine.
//
// A bot has one delivery method at a time: setWebhook turns off getUpdates and
// the reverse. Start picks the one the config asks for on every run, so switching
// mode or rolling back never leaves a stale webhook that makes getUpdates fail
// with 409.
func (b *Bot) Start() error {
	if b.webhook != nil {
		if err := b.bot.SetWebhook(b.webhook); err != nil {
			return fmt.Errorf("failed to register the webhook with Telegram: %w", err)
		}
		slog.Info("Webhook registered with Telegram", "public_url", b.cfg.WebhookPublicURL, "listen", b.webhook.Listen)
	} else {
		if err := b.bot.RemoveWebhook(); err != nil {
			return fmt.Errorf("failed to remove a stale webhook: %w", err)
		}
		slog.Info("No webhook is set, using long polling")
	}

	if err := b.bot.SetCommands(commands); err != nil {
		slog.Warn("Failed to register the command menu, clients may show a stale one", "error", err)
	}

	go func() {
		slog.Info("Bot started...", "username", b.bot.Me.Username)
		b.bot.Start()
	}()
	return nil
}

// Stop gracefully stops the bot.
func (b *Bot) Stop() {
	slog.Info("Shutting down bot...")
	b.bot.Stop()
}
