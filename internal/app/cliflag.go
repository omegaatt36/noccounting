package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/urfave/cli/v3"
)

// CliFlager is interface to describe a struct
type CliFlager interface {
	CliFlags() []cli.Flag
}

// Beforer is interface for some package may needs an before hook to init
type Beforer interface {
	Before(context.Context, *cli.Command) error
}

// Afterer is interface for some package may needs an after hook to destroy
type Afterer interface {
	After(context.Context, *cli.Command) error
}

var cliFlagers []CliFlager

// Register adds a CliFlager instance to the global registry. This ensures its
// flags are collected and its Before/After hooks (if implemented) are called.
func Register(f CliFlager) {
	cliFlagers = append(cliFlagers, f)
}

// IsBeforer checks interface conversion.
func IsBeforer(Beforer) {}

// IsAfterer checks interface conversion.
func IsAfterer(Afterer) {}

// Globals collects and returns a combined slice of all cli.Flag definitions
// from all registered CliFlager instances.
func Globals() []cli.Flag {
	var res []cli.Flag
	for _, f := range cliFlagers {
		res = append(res, f.CliFlags()...)
	}

	return res
}

// Initialize iterates through all registered packages and calls the Before method
// for those that implement the Beforer interface.
func Initialize(ctx context.Context, cmd *cli.Command) error {
	for _, f := range cliFlagers {
		b, ok := f.(Beforer)
		if ok {
			slog.Info("running Before", "type", fmt.Sprintf("%T", b))
			err := b.Before(ctx, cmd)
			if err != nil {
				return fmt.Errorf("before hook failed for %T: %w", b, err)
			}
		}
	}

	return nil
}

// Finalize iterates through all registered packages and calls the After method
// for those that implement the Afterer interface.
func Finalize(ctx context.Context, cmd *cli.Command) error {
	//revive:disable:defer
	var finalizationErrors []error
	// Iterate in reverse to mimic defer LIFO order somewhat for cleanup
	for i := len(cliFlagers) - 1; i >= 0; i-- {
		f := cliFlagers[i]
		a, ok := f.(Afterer)
		if ok {
			slog.Info("running After", "type", fmt.Sprintf("%T", a))
			err := a.After(ctx, cmd)
			if err != nil {
				slog.Error("error during finalize", "type", fmt.Sprintf("%T", a), "error", err)
				// Collect errors, decide later how to handle them (e.g., return first, aggregate)
				finalizationErrors = append(finalizationErrors, fmt.Errorf("after hook failed for %T: %w", a, err))
			}
		}
	}
	//revive:enable:defer

	if len(finalizationErrors) > 0 {
		return finalizationErrors[0]
	}

	return nil
}
