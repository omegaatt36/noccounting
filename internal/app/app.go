package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

type App struct {
	Main func(ctx context.Context) error
}

func (a App) Run() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Recovered from panic", "panic", r)
			debug.PrintStack()
			os.Exit(1)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		slog.Info("Received signal, initiating shutdown...")
	}()

	err := a.Main(ctx)
	if err != nil {
		slog.Error("Main function returned error", "error", err)
		os.Exit(1)
	}

	slog.Info("terminated")
}
