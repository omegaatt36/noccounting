package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// App provides a minimal wrapper around application setup and execution,
// including signal handling for graceful shutdown and panic recovery.
type App struct {
	// Main is the core function of the application, executed after setup.
	Main func(ctx context.Context) error
}

// Run sets up signal handling, panic recovery, and executes the Main function.
// It fatally logs any error during execution and returns an exit code.
func (a App) Run() {
	// Panic handling.
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
