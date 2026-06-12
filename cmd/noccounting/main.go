package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/omegaatt36/noccounting/internal/app"
	"github.com/omegaatt36/noccounting/internal/app/bot"
	"github.com/omegaatt36/noccounting/internal/app/webapp"
	"github.com/omegaatt36/noccounting/internal/repository/exchangerate"
	"github.com/omegaatt36/noccounting/internal/repository/llm"
	"github.com/omegaatt36/noccounting/internal/repository/notion"
	"github.com/omegaatt36/noccounting/internal/repository/sqlite"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/ledger"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envRequired(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("required environment variable not set: %s", key)
	}
	return v, nil
}

func main() {
	telegramToken, err := envRequired("TELEGRAM_BOT_TOKEN")
	if err != nil {
		slog.Error("failed to get TELEGRAM_BOT_TOKEN", "error", err)
		os.Exit(1)
	}
	notionToken, err := envRequired("NOTION_TOKEN")
	if err != nil {
		slog.Error("failed to get NOTION_TOKEN", "error", err)
		os.Exit(1)
	}

	userMapping := env("USER_MAPPING", "")
	port := env("PORT", "8080")
	webAppURL := env("WEBAPP_URL", "")
	logLevel := env("LOG_LEVEL", "debug")
	llmAPIKey := env("LLM_API_KEY", "")
	llmBaseURL := env("LLM_BASE_URL", "")
	llmModel := env("LLM_MODEL", "gpt-4o")
	sqlitePath := env("SQLITE_PATH", "./noccounting.db")
	seedDatabaseID := env("NOTION_DATABASE_ID", "")
	devMode := os.Getenv("DEV_MODE") == "true"

	app.App{Main: func(ctx context.Context) error {
		lvl := slog.LevelDebug
		if err := lvl.UnmarshalText([]byte(logLevel)); err != nil {
			slog.Warn("invalid LOG_LEVEL, falling back to debug", "value", logLevel, "error", err)
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))

		userRepo := userrepo.NewRepo(userMapping)
		userService := user.NewService(userRepo)

		ledgerRepo, err := sqlite.NewRepo(sqlitePath)
		if err != nil {
			return fmt.Errorf("failed to open sqlite: %w", err)
		}
		defer ledgerRepo.Close()

		ledgerService := ledger.NewService(ledgerRepo)

		if seedDatabaseID != "" {
			seeded, err := ledgerService.SeedIfEmpty(ctx, "default", seedDatabaseID)
			if err != nil {
				return fmt.Errorf("failed to seed ledger: %w", err)
			}
			if seeded {
				slog.Info("seeded default ledger from NOTION_DATABASE_ID")
			}
		}

		notionRepo := notion.NewClient(notionToken)
		rateFetcher := exchangerate.NewFinMindClient()

		var analyzer expense.ReceiptAnalyzer
		if llmAPIKey != "" && llmBaseURL != "" {
			analyzer = llm.NewAnalyzer(llmBaseURL, llmAPIKey, llmModel)
		}

		expenseService := expense.NewService(notionRepo, ledgerService, rateFetcher, analyzer)

		server, err := webapp.NewServer(userService, expenseService, port, telegramToken, devMode)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}
		if err := server.Start(); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}

		telegramBot, err := bot.New(telegramToken, webAppURL, userService, expenseService, ledgerService)
		if err != nil {
			return fmt.Errorf("failed to create bot: %w", err)
		}
		telegramBot.Start()

		<-ctx.Done()

		telegramBot.Stop()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}}.Run()
}
