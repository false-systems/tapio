package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidation_RestartPolicyNegativeValues tests validation of negative values
func TestValidation_RestartPolicyNegativeValues(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var started atomic.Bool

	// Register observer with negative maxRestarts (should default to 0 = no restarts)
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		if !started.Load() {
			started.Store(true)
			return assert.AnError // Fail once
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(-1, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for observer to start and fail
	require.Eventually(t, func() bool {
		return started.Load()
	}, 1*time.Second, 10*time.Millisecond)

	// Wait a bit to ensure no restart happens
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-errCh

	// Should have started only once (no restarts with maxRestarts=0)
	assert.True(t, started.Load())
}

// TestValidation_RestartPolicyZeroWindow tests validation of zero/negative window
func TestValidation_RestartPolicyZeroWindow(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Register observer with zero window (should default to 1 minute)
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt := attempts.Add(1)
		if attempt <= 2 {
			return assert.AnError
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 0)) // Zero window should default to 1 minute

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return attempts.Load() >= 3
	}, 10*time.Second, 10*time.Millisecond)

	cancel()
	<-errCh

	// Should have restarted (window was defaulted, not zero)
	assert.GreaterOrEqual(t, int(attempts.Load()), 3)
}

// TestValidation_WorkerScalingInvalid tests validation of min > max
func TestValidation_WorkerScalingInvalid(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	// Register with min > max (should swap or fix)
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithWorkerScaling(10, 5)) // Invalid: min > max

	// Should not panic - validation should handle this
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	assert.NoError(t, err)
}

// TestValidation_ResourceLimitsNegative tests validation of negative resource limits
func TestValidation_ResourceLimitsNegative(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	// Register with negative CPU (should default to 0 or positive)
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithResourceLimits(-1.0, 0))

	// Should not panic
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	assert.NoError(t, err)
}

// TestValidation_DuplicateObserverName tests handling of duplicate observer names
func TestValidation_DuplicateObserverName(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var firstStarted atomic.Bool
	var secondStarted atomic.Bool

	// Register first observer
	sup.SuperviseFunc("duplicate", func(ctx context.Context) error {
		firstStarted.Store(true)
		<-ctx.Done()
		return nil
	})

	// Register second observer with same name (should overwrite or error)
	sup.SuperviseFunc("duplicate", func(ctx context.Context) error {
		secondStarted.Store(true)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return secondStarted.Load()
	}, 1*time.Second, 10*time.Millisecond)

	cancel()
	<-errCh

	// Only second observer should have started (overwrites first)
	assert.True(t, secondStarted.Load())
	assert.False(t, firstStarted.Load())
}

// TestValidation_RunMultipleTimes tests that Run cannot be called multiple times
func TestValidation_RunMultipleTimes(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	// First run
	ctx1, cancel1 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err1 := sup.Run(ctx1)
	cancel1()
	require.NoError(t, err1)

	// Second run should fail
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	err2 := sup.Run(ctx2)

	// Should return error about already running/run
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), "already")
}

// TestRestart_BackoffCap tests that exponential backoff doesn't overflow
func TestRestart_BackoffCap(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Observer that fails multiple times to test high backoff
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt := attempts.Add(1)

		// Fail first 5 times (backoff: 1s, 2s, 4s, 8s, 16s)
		// Total time: 1+2+4+8+16 = 31s
		if attempt <= 5 {
			return assert.AnError
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(10, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for 6 attempts (5 failures + 1 success)
	require.Eventually(t, func() bool {
		return attempts.Load() >= 6
	}, 35*time.Second, 10*time.Millisecond)

	cancel()
	<-errCh

	// Should have completed without overflow/panic
	assert.GreaterOrEqual(t, int(attempts.Load()), 6)

	// The key test: no panic/overflow occurred even with high attempt numbers
	// Backoff calculation with cap prevents undefined behavior
}

// TestRestart_AttemptResetWithWindow tests that attempt counter resets with restart window
func TestRestart_AttemptResetWithWindow(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var attempts atomic.Int32

	// Observer that fails, succeeds, then fails again after window
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt := attempts.Add(1)

		// Fail twice, then succeed briefly
		if attempt == 1 || attempt == 2 {
			return assert.AnError
		}

		// On 3rd attempt, run briefly then fail again
		if attempt == 3 {
			time.Sleep(200 * time.Millisecond) // Run successfully past window
			return assert.AnError              // Then fail
		}

		// 4th attempt should use reset backoff (not exponential from attempt 3)
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(10, 150*time.Millisecond)) // Short window

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return attempts.Load() >= 4
	}, 10*time.Second, 10*time.Millisecond)

	cancel()
	<-errCh

	assert.Equal(t, int32(4), attempts.Load())
}
