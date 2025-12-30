package bot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// Bot wraps the Telegram bot and its lifecycle management.
type Bot struct {
	bot     *tele.Bot
	handler *Handler
}

// New creates and initializes a new Telegram bot.
func New(
	token string,
	webAppURL string,
	userService *user.Service,
	accountingRepo domain.AccountingRepo,
	rateFetcher domain.ExchangeRateFetcher,
	receiptAnalyzer domain.ReceiptAnalyzer,
) (*Bot, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	teleBot, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	handler := NewHandler(userService, accountingRepo, rateFetcher, receiptAnalyzer, webAppURL)
	handler.RegisterHandlers(teleBot)

	return &Bot{
		bot:     teleBot,
		handler: handler,
	}, nil
}

// Start starts the bot in a goroutine.
func (b *Bot) Start() {
	go func() {
		slog.Info("Bot started...")
		b.bot.Start()
	}()
}

// Stop gracefully stops the bot.
func (b *Bot) Stop() {
	slog.Info("Shutting down bot...")
	b.bot.Stop()
}

// Run starts the bot and waits for context cancellation.
func (b *Bot) Run(ctx context.Context) {
	b.Start()
	<-ctx.Done()
	b.Stop()
}
