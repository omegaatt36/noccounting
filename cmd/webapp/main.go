package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/omegaatt36/noccounting/internal/app"
	"github.com/omegaatt36/noccounting/internal/app/webapp"
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
	notionToken := envRequired("NOTION_TOKEN")
	notionDatabaseID := envRequired("NOTION_DATABASE_ID")

	userMapping := env("USER_MAPPING", "")
	port := env("PORT", "8080")
	telegramBotToken := env("TELEGRAM_BOT_TOKEN", "")
	logLevel := env("LOG_LEVEL", "debug")
	devMode := os.Getenv("DEV_MODE") == "true"

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
		expenseService := expense.NewService(accountingRepo, nil, nil)

		server, err := webapp.NewServer(userService, expenseService, port, telegramBotToken, devMode)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}

		if err := server.Start(); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}

		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		return server.Shutdown(shutdownCtx)
	}}.Run()
}
