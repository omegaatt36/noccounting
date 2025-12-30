package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/urfave/cli/v3"
)

// App provides a wrapper around urfave/cli app setup, including
// signal handling for graceful shutdown, panic recovery, and flag registration hooks.
type App struct {
	// Flags holds common CLI flags for the application.
	Flags []cli.Flag
	// Main is the core function of the application, executed after setup.
	Main func(ctx context.Context, cmd *cli.Command) error
}

// before is executed by cli before the main Action. It initializes registered
// cliflag packages and includes basic panic recovery for the initialization phase.
func (a *App) before(ctx context.Context, cmd *cli.Command) (_ context.Context, err error) {
	// Panic handling.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Recovered from panic", "panic", r)
			debug.PrintStack()
			err = errors.New("init failed")
		}
	}()

	return ctx, Initialize(ctx, cmd)
}

// after is executed by cli after the main Action. It finalizes registered
// cliflag packages (usually for cleanup).
func (a *App) after(ctx context.Context, cmd *cli.Command) error {
	return Finalize(ctx, cmd)
}

// wrapMain wraps the execution of the application's Main function.
// It sets up a context cancellable by OS signals (SIGINT, SIGTERM)
// and includes panic recovery for the main application logic.
func (a *App) wrapMain(cCtx context.Context, cmd *cli.Command) error {
	appCtx, cancel := signal.NotifyContext(cCtx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-appCtx.Done()
		// Check the cause of context cancellation
		if errors.Is(appCtx.Err(), context.Canceled) {
			// This can happen if cancel() is called explicitly elsewhere,
			// or if the parent context (cCtx) was cancelled.
			slog.Info("Context canceled, initiating shutdown...")
		} else if errors.Is(appCtx.Err(), context.DeadlineExceeded) {
			slog.Info("Context deadline exceeded, initiating shutdown...")
		} else {
			// Likely due to os.Signal
			slog.Info("Received signal, initiating shutdown...")
		}
	}()

	// Panic handling.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Main recovered from panic", "panic", r)
			debug.PrintStack()
		}
	}()

	err := a.Main(appCtx, cmd) // Pass the cancellable context
	if err != nil {
		slog.Error("Main function returned error", "error", err)
	}

	slog.Info("terminated")

	return nil
}

// Run sets up the urfave/cli App with the configured flags, before/after hooks,
// and the main action wrapper, then executes it. It fatally logs any error
// during the cli app execution.
func (a *App) Run() {
	app := &cli.Command{
		Flags:  append(a.Flags, Globals()...),
		Before: a.before,
		After:  a.after,
		Action: a.wrapMain,
	}

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		slog.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}
