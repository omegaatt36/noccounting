package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/omegaatt36/noccounting/internal/app"
	"github.com/omegaatt36/noccounting/internal/app/webapp"
	"github.com/omegaatt36/noccounting/internal/persistence/notion"
	userrepo "github.com/omegaatt36/noccounting/internal/persistence/user"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type config struct {
	notionToken      string
	notionDatabaseID string
	userMapping      string
	port             string
	telegramBotToken string
	logLevel         string
	devMode          bool
}

var cfg = config{}

func initLog() {
	logLevel := slog.LevelDebug
	if lvlStr := cfg.logLevel; lvlStr != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(lvlStr)); err == nil {
			logLevel = level
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))
}

func wrapMain(ctx context.Context, _ *cli.Command) error {
	initLog()

	userRepo := userrepo.NewRepo(cfg.userMapping)

	userService := user.NewService(userRepo)
	accountingRepo := notion.NewClient(cfg.notionToken, cfg.notionDatabaseID)

	server, err := webapp.NewServer(userService, accountingRepo, cfg.port, cfg.telegramBotToken, cfg.devMode)
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
}

func main() {
	app := app.App{
		Main: wrapMain,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "notion-token",
				Usage:       "Notion API token",
				Sources:     cli.EnvVars("NOTION_TOKEN"),
				Destination: &cfg.notionToken,
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "notion-database-id",
				Usage:       "Notion database ID for accounting",
				Sources:     cli.EnvVars("NOTION_DATABASE_ID"),
				Destination: &cfg.notionDatabaseID,
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "user-mapping",
				Usage:       "User mapping in format: telegram_id1:notion_id1:nickname1,telegram_id2:notion_id2:nickname2",
				Sources:     cli.EnvVars("USER_MAPPING"),
				Destination: &cfg.userMapping,
			},
			&cli.StringFlag{
				Name:        "port",
				Usage:       "HTTP server port",
				Sources:     cli.EnvVars("PORT"),
				Destination: &cfg.port,
				Value:       "8080",
			},
			&cli.StringFlag{
				Name:        "telegram-bot-token",
				Usage:       "Telegram bot token for validating WebApp initData",
				Sources:     cli.EnvVars("TELEGRAM_BOT_TOKEN"),
				Destination: &cfg.telegramBotToken,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Usage:       "Log level",
				Sources:     cli.EnvVars("LOG_LEVEL"),
				Destination: &cfg.logLevel,
				Value:       "debug",
			},
			&cli.BoolFlag{
				Name:        "dev-mode",
				Usage:       "Enable dev mode (skip Telegram auth for local development)",
				Sources:     cli.EnvVars("DEV_MODE"),
				Destination: &cfg.devMode,
			},
		},
	}

	app.Run()
}
