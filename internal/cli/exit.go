package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
)

type interruptionError struct{ signal os.Signal }

func (e *interruptionError) Error() string { return fmt.Sprintf("command interrupted: %s", e.signal) }
func (e *interruptionError) Unwrap() error { return context.Canceled }

// ExitCode maps command errors to the public CLI exit contract.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var interrupted *interruptionError
	if errors.As(err, &interrupted) {
		return interruptionCode(interrupted.signal)
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 1
}

func interruptionContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	incoming := make(chan os.Signal, 1)
	// Keep the signal as the cancellation cause to distinguish SIGINT/SIGTERM.
	signal.Notify(incoming, interruptionSignals()...)
	go func() {
		select {
		case sig := <-incoming:
			cancel(&interruptionError{signal: sig})
		case <-ctx.Done():
		}
	}()
	return ctx, func() { signal.Stop(incoming); cancel(nil) }
}
