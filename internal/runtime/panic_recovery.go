package runtime

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// WithPanicRecovery wraps a function with panic recovery.
// Returns the original error if no panic occurred.
// Returns a panic error if a panic was caught.
func WithPanicRecovery(observerName string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Capture stack trace
			stack := debug.Stack()
			err = fmt.Errorf("panic recovered in observer %s: %v\nStack:\n%s", observerName, r, stack)
		}
	}()

	return fn()
}

// WithPanicRecoveryAndLog wraps a function with panic recovery and logging.
// Calls the log function when a panic is caught.
func WithPanicRecoveryAndLog(observerName string, fn func() error, logFn func(observerName string, panicErr error)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			panicErr := fmt.Errorf("panic recovered in observer %s: %v\nStack:\n%s", observerName, r, stack)

			// Call log callback
			if logFn != nil {
				logFn(observerName, panicErr)
			}

			err = panicErr
		}
	}()

	return fn()
}

// WithPanicRecoveryTimeout wraps a function with panic recovery and timeout.
// The provided function must accept a context and should respect its cancellation.
// Returns a timeout error if the function doesn't complete in time.
func WithPanicRecoveryTimeout(observerName string, fn func(ctx context.Context) error, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		errCh <- WithPanicRecovery(observerName, func() error { return fn(ctx) })
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timeout after %v in observer %s", timeout, observerName)
	}
}
