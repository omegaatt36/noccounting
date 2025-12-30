package main

import (
	"context"
	"fmt"
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
}

var cfg = config{}

func wrapMain(ctx context.Context, _ *cli.Command) error {
	userRepo := userrepo.NewRepo(cfg.userMapping)

	userService := user.NewService(userRepo)
	accountingRepo := notion.NewClient(cfg.notionToken, cfg.notionDatabaseID)

	server, err := webapp.NewServer(userService, accountingRepo, cfg.port, cfg.telegramBotToken)
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
				Required:    true,
			},
		},
	}

	app.Run()
}
