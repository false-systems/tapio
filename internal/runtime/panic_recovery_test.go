package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test panic recovery wrapper
func TestWithPanicRecovery_Success(t *testing.T) {
	var called bool

	fn := func() error {
		called = true
		return nil
	}

	err := WithPanicRecovery("test", fn)
	require.NoError(t, err)
	assert.True(t, called)
}

// RED: Test panic recovery catches panic
func TestWithPanicRecovery_CatchesPanic(t *testing.T) {
	fn := func() error {
		panic("something went wrong")
	}

	err := WithPanicRecovery("test-observer", fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")
	assert.Contains(t, err.Error(), "test-observer")
	assert.Contains(t, err.Error(), "something went wrong")
}

// RED: Test panic recovery with nil panic
func TestWithPanicRecovery_NilPanic(t *testing.T) {
	fn := func() error {
		panic(nil)
	}

	err := WithPanicRecovery("test", fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")
}

// RED: Test panic recovery with error panic
func TestWithPanicRecovery_ErrorPanic(t *testing.T) {
	fn := func() error {
		panic(assert.AnError)
	}

	err := WithPanicRecovery("test", fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}

// RED: Test panic recovery preserves original error
func TestWithPanicRecovery_PreservesError(t *testing.T) {
	expectedErr := assert.AnError

	fn := func() error {
		return expectedErr
	}

	err := WithPanicRecovery("test", fn)
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

// RED: Test panic recovery is thread-safe
func TestWithPanicRecovery_ThreadSafe(t *testing.T) {
	const goroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			fn := func() error {
				if id%2 == 0 {
					panic("even panic")
				}
				return nil
			}

			errors <- WithPanicRecovery("concurrent", fn)
		}(i)
	}

	wg.Wait()
	close(errors)

	// Count panics
	var panicCount int
	for err := range errors {
		if err != nil {
			panicCount++
			assert.Contains(t, err.Error(), "panic recovered")
		}
	}

	assert.Equal(t, 5, panicCount) // Half should panic
}

// RED: Test panic logging callback
func TestWithPanicRecoveryAndLog_LogsCalled(t *testing.T) {
	var loggedName string
	var loggedErr error

	logFn := func(observerName string, panicErr error) {
		loggedName = observerName
		loggedErr = panicErr
	}

	fn := func() error {
		panic("test panic")
	}

	err := WithPanicRecoveryAndLog("my-observer", fn, logFn)
	require.Error(t, err)

	// Verify log callback was called
	assert.Equal(t, "my-observer", loggedName)
	assert.NotNil(t, loggedErr)
	assert.Contains(t, loggedErr.Error(), "panic recovered")
}

// RED: Test panic recovery with timeout
func TestWithPanicRecoveryTimeout_Success(t *testing.T) {
	fn := func(ctx context.Context) error {
		select {
		case <-time.After(50 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	err := WithPanicRecoveryTimeout("test", fn, 200*time.Millisecond)
	require.NoError(t, err)
}

// RED: Test panic recovery timeout triggers
func TestWithPanicRecoveryTimeout_Triggers(t *testing.T) {
	fn := func(ctx context.Context) error {
		select {
		case <-time.After(500 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	err := WithPanicRecoveryTimeout("test", fn, 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}
