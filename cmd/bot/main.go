package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/omegaatt36/noccounting/internal/app"
	"github.com/omegaatt36/noccounting/internal/app/bot"
	"github.com/omegaatt36/noccounting/internal/repository/exchangerate"
	"github.com/omegaatt36/noccounting/internal/repository/llm"
	"github.com/omegaatt36/noccounting/internal/repository/notion"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envRequired(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func main() {
	telegramToken := envRequired("TELEGRAM_BOT_TOKEN")
	notionToken := envRequired("NOTION_TOKEN")
	notionDatabaseID := envRequired("NOTION_DATABASE_ID")

	userMapping := env("USER_MAPPING", "")
	webAppURL := env("WEBAPP_URL", "")
	logLevel := env("LOG_LEVEL", "debug")
	llmAPIKey := env("LLM_API_KEY", "")
	llmBaseURL := env("LLM_BASE_URL", "")
	llmModel := env("LLM_MODEL", "gpt-4o")

	app.App{Main: func(ctx context.Context) error {
		// Initialize logger
		lvl := slog.LevelDebug
		if logLevel != "" {
			if err := lvl.UnmarshalText([]byte(logLevel)); err == nil {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
			}
		}

		userRepo := userrepo.NewRepo(userMapping)
		userService := user.NewService(userRepo)
		accountingRepo := notion.NewClient(notionToken, notionDatabaseID)
		rateFetcher := exchangerate.NewFinMindClient()

		var analyzer expense.ReceiptAnalyzer
		if llmAPIKey != "" && llmBaseURL != "" {
			analyzer = llm.NewAnalyzer(llmBaseURL, llmAPIKey, llmModel)
		}

		expenseService := expense.NewService(accountingRepo, rateFetcher, analyzer)

		telegramBot, err := bot.New(
			telegramToken,
			webAppURL,
			userService,
			expenseService,
		)
		if err != nil {
			return fmt.Errorf("failed to create bot: %w", err)
		}

		telegramBot.Run(ctx)
		return nil
	}}.Run()
}
