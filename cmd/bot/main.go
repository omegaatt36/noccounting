package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/omegaatt36/noccounting/internal/app"
	"github.com/omegaatt36/noccounting/internal/app/bot"
	"github.com/omegaatt36/noccounting/internal/persistence/notion"
	userrepo "github.com/omegaatt36/noccounting/internal/persistence/user"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type config struct {
	telegramToken    string
	notionToken      string
	notionDatabaseID string
	userMapping      string
	webAppURL        string
	logLevel         string
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

	telegramBot, err := bot.New(
		cfg.telegramToken,
		cfg.webAppURL,
		userService,
		accountingRepo,
	)
	if err != nil {
		return fmt.Errorf("failed to create bot: %w", err)
	}

	telegramBot.Run(ctx)

	return nil
}

func main() {
	app := app.App{
		Main: wrapMain,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "telegram-token",
				Usage:       "Telegram bot token",
				Sources:     cli.EnvVars("TELEGRAM_BOT_TOKEN"),
				Destination: &cfg.telegramToken,
				Required:    true,
			},
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
				Name:        "webapp-url",
				Usage:       "URL of the Mini App (for bot menu button)",
				Sources:     cli.EnvVars("WEBAPP_URL"),
				Destination: &cfg.webAppURL,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Usage:       "Log level",
				Sources:     cli.EnvVars("LOG_LEVEL"),
				Destination: &cfg.logLevel,
				Value:       "debug",
			},
		},
	}

	app.Run()
}
