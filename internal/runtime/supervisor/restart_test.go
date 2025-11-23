package supervisor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestart_AutoRestart tests that crashed observers auto-restart
func TestRestart_AutoRestart(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Observer that crashes once, then succeeds
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt := attempts.Add(1)

		if attempt == 1 {
			// First attempt - crash immediately
			return fmt.Errorf("first attempt failed")
		}

		// Second attempt - run successfully
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for both attempts
	require.Eventually(t, func() bool {
		return attempts.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond, "observer should restart after crash")

	cancel()
	err := <-errCh
	require.NoError(t, err)

	assert.Equal(t, int32(2), attempts.Load(), "should have attempted twice")
}

// TestRestart_ExponentialBackoff tests backoff timing
func TestRestart_ExponentialBackoff(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32
	var timestamps []time.Time
	var mu sync.Mutex

	// Observer that crashes multiple times
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()

		attempt := attempts.Add(1)

		if attempt <= 3 {
			// Crash first 3 attempts
			return fmt.Errorf("attempt %d failed", attempt)
		}

		// 4th attempt - succeed
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for 4 attempts
	require.Eventually(t, func() bool {
		return attempts.Load() >= 4
	}, 10*time.Second, 10*time.Millisecond, "should have 4 attempts")

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()

	// Verify exponential backoff
	// Attempt 1: immediate
	// Attempt 2: +1s backoff
	// Attempt 3: +2s backoff
	// Attempt 4: +4s backoff

	require.Len(t, timestamps, 4, "should have 4 timestamps")

	// Check backoff between attempts (with tolerance)
	checkBackoff := func(attempt int, expectedBackoff time.Duration) {
		actual := timestamps[attempt].Sub(timestamps[attempt-1])
		tolerance := 200 * time.Millisecond

		assert.InDelta(t, expectedBackoff.Seconds(), actual.Seconds(), tolerance.Seconds(),
			"backoff between attempt %d and %d should be ~%v (got %v)",
			attempt, attempt+1, expectedBackoff, actual)
	}

	checkBackoff(1, 1*time.Second) // 1st restart: 1s backoff
	checkBackoff(2, 2*time.Second) // 2nd restart: 2s backoff
	checkBackoff(3, 4*time.Second) // 3rd restart: 4s backoff
}

// TestRestart_CircuitBreaker tests that chronic failures disable observer
func TestRestart_CircuitBreaker(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Observer that always crashes
	sup.SuperviseFunc("failing-observer", func(ctx context.Context) error {
		attempts.Add(1)
		return fmt.Errorf("always fails")
	}, WithRestartPolicy(3, 1*time.Minute)) // Max 3 restarts

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait a bit for restarts
	time.Sleep(10 * time.Second)

	cancel()
	<-errCh

	// Should have tried initial + 3 restarts = 4 total attempts
	// After that, circuit breaker should disable it
	assert.LessOrEqual(t, int(attempts.Load()), 4, "should stop after max restarts")
	assert.GreaterOrEqual(t, int(attempts.Load()), 4, "should attempt at least 4 times")
}

// TestRestart_RestartWindow tests restart counting window
func TestRestart_RestartWindow(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Observer that crashes twice, then runs successfully
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt := attempts.Add(1)

		if attempt <= 2 {
			// First 2 attempts - crash
			return fmt.Errorf("attempt %d failed", attempt)
		}

		// 3rd attempt - run successfully
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 100*time.Millisecond)) // Short window for test

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for restart attempts
	require.Eventually(t, func() bool {
		return attempts.Load() >= 3
	}, 10*time.Second, 10*time.Millisecond, "should restart")

	cancel()
	<-errCh

	// Should have 3 attempts (initial + 2 restarts)
	assert.Equal(t, int32(3), attempts.Load())
}

// TestRestart_MultipleObserversIndependent tests that one observer crashing doesn't affect others
func TestRestart_MultipleObserversIndependent(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var failingAttempts atomic.Int32
	var healthyStarted atomic.Bool

	// Failing observer (crashes once)
	sup.SuperviseFunc("failing-observer", func(ctx context.Context) error {
		attempt := failingAttempts.Add(1)
		if attempt == 1 {
			return fmt.Errorf("first attempt failed")
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 1*time.Minute))

	// Healthy observer (never crashes)
	sup.SuperviseFunc("healthy-observer", func(ctx context.Context) error {
		healthyStarted.Store(true)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for both observers
	require.Eventually(t, func() bool {
		return failingAttempts.Load() >= 2 && healthyStarted.Load()
	}, 2*time.Second, 10*time.Millisecond, "both observers should be running")

	cancel()
	<-errCh

	// Failing observer should have restarted
	assert.Equal(t, int32(2), failingAttempts.Load(), "failing observer should restart")

	// Healthy observer should have started once
	assert.True(t, healthyStarted.Load(), "healthy observer should be running")
}
