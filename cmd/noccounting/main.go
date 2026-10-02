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
	"github.com/omegaatt36/noccounting/internal/repository/trek"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
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

func newUserRepo(mapping string) (repo *userrepo.Repo, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		repo = nil
		if parseErr, ok := recovered.(error); ok {
			err = fmt.Errorf("invalid USER_MAPPING: %w", parseErr)
			return
		}
		err = fmt.Errorf("invalid USER_MAPPING: %v", recovered)
	}()

	return userrepo.NewRepo(mapping), nil
}

func main() {
	telegramToken, err := envRequired("TELEGRAM_BOT_TOKEN")
	if err != nil {
		slog.Error("failed to get TELEGRAM_BOT_TOKEN", "error", err)
		os.Exit(1)
	}

	userMapping := env("USER_MAPPING", "")
	port := env("PORT", "8080")
	webAppURL := env("WEBAPP_URL", "")
	logLevel := env("LOG_LEVEL", "debug")
	llmAPIKey := env("LLM_API_KEY", "")
	llmBaseURL := env("LLM_BASE_URL", "")
	llmModel := env("LLM_MODEL", "gpt-4o")
	devMode := os.Getenv("DEV_MODE") == "true"

	app.App{Main: func(ctx context.Context) error {
		lvl := slog.LevelDebug
		if err := lvl.UnmarshalText([]byte(logLevel)); err != nil {
			slog.Warn("invalid LOG_LEVEL, falling back to debug", "value", logLevel, "error", err)
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))

		trekConfig, err := trek.ConfigFromEnv()
		if err != nil {
			return fmt.Errorf("reading the TREK credential: %w", err)
		}

		userRepo, err := newUserRepo(userMapping)
		if err != nil {
			return err
		}
		userService := user.NewService(userRepo)

		trekClient := trek.NewClient(trekConfig)
		if err := trekClient.Start(ctx); err != nil {
			return fmt.Errorf("starting the TREK client: %w", err)
		}

		trips, err := trekClient.ListTrips(ctx)
		if err != nil {
			return fmt.Errorf("listing the TREK trips: %w", err)
		}
		if len(trips) == 0 {
			slog.Warn("the TREK service account is on no trip noccounting can use; add it to a trip in TREK and the bot will offer it")
		}
		for _, available := range trips {
			slog.Info("the TREK service account is on a trip", "trip_id", available.ID, "title", available.Title, "currency", available.Currency)
		}

		rateFetcher := exchangerate.NewFinMindClient()

		accountingRepo := trek.NewRepo(trekClient, rateFetcher)
		tripService := trip.NewService(trekClient)

		var analyzer expense.ReceiptAnalyzer
		if llmAPIKey != "" && llmBaseURL != "" {
			analyzer = llm.NewAnalyzer(llmBaseURL, llmAPIKey, llmModel)
		}

		expenseService := expense.NewService(accountingRepo, rateFetcher, analyzer)

		server, err := webapp.NewServer(userService, expenseService, tripService, port, telegramToken, devMode)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}
		if err := server.Start(); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}

		telegramBot, err := bot.New(telegramToken, webAppURL, userService, expenseService, tripService)
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
