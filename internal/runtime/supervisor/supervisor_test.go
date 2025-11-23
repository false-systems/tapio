package supervisor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSupervisor_BasicLifecycle tests basic start and stop
func TestSupervisor_BasicLifecycle(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	// Track observer lifecycle
	var started atomic.Bool
	var stopped atomic.Bool

	// Add mock observer
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		stopped.Store(true)
		return nil
	})

	// Start supervisor in background
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for observer to start
	require.Eventually(t, func() bool {
		return started.Load()
	}, 1*time.Second, 10*time.Millisecond, "observer should start")

	// Cancel and wait for shutdown
	cancel()
	err := <-errCh
	require.NoError(t, err, "supervisor should exit cleanly")

	// Verify observer stopped
	assert.True(t, stopped.Load(), "observer should have stopped")
}

// TestSupervisor_MultipleObservers tests supervising multiple observers
func TestSupervisor_MultipleObservers(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	// Track which observers started
	var obs1Started, obs2Started, obs3Started atomic.Bool

	sup.SuperviseFunc("observer-1", func(ctx context.Context) error {
		obs1Started.Store(true)
		<-ctx.Done()
		return nil
	})

	sup.SuperviseFunc("observer-2", func(ctx context.Context) error {
		obs2Started.Store(true)
		<-ctx.Done()
		return nil
	})

	sup.SuperviseFunc("observer-3", func(ctx context.Context) error {
		obs3Started.Store(true)
		<-ctx.Done()
		return nil
	})

	// Start supervisor
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for all observers to start
	require.Eventually(t, func() bool {
		return obs1Started.Load() && obs2Started.Load() && obs3Started.Load()
	}, 1*time.Second, 10*time.Millisecond, "all observers should start")

	// Wait for shutdown
	err := <-errCh
	require.NoError(t, err)
}

// TestSupervisor_EmptyObserverList tests error when no observers added
func TestSupervisor_EmptyObserverList(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	ctx := context.Background()
	err := sup.Run(ctx)

	require.Error(t, err, "should error with no observers")
	assert.Contains(t, err.Error(), "no observers", "error should mention no observers")
}

// TestSupervisor_ContextCancellation tests clean shutdown on context cancel
func TestSupervisor_ContextCancellation(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	var cleanShutdown atomic.Bool

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		cleanShutdown.Store(true)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Give observer time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for shutdown
	err := <-errCh
	require.NoError(t, err)
	assert.True(t, cleanShutdown.Load(), "observer should shut down cleanly")
}

// TestSupervisor_ObserverError tests observer that returns error
func TestSupervisor_ObserverError(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	// Observer that fails immediately
	sup.SuperviseFunc("failing-observer", func(ctx context.Context) error {
		return fmt.Errorf("intentional failure")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)

	// Supervisor should NOT fail if one observer fails (graceful degradation)
	// This will be tested more in auto-restart tests
	require.NoError(t, err, "supervisor should handle observer errors gracefully")
}

// TestSupervisor_ShutdownTimeout tests zombie detection
func TestSupervisor_ShutdownTimeout(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 100 * time.Millisecond, // Short timeout for test
	})

	// Observer that doesn't exit (zombie)
	sup.SuperviseFunc("zombie-observer", func(ctx context.Context) error {
		<-ctx.Done()
		// Stuck! Don't exit
		time.Sleep(5 * time.Second)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Give observer time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait for timeout
	cancel()
	err := <-errCh

	// Should get timeout error
	require.Error(t, err, "should timeout on zombie observer")
	assert.Contains(t, err.Error(), "timeout", "error should mention timeout")
}

// TestSupervisor_NamedObservers tests observer name tracking
func TestSupervisor_NamedObservers(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout: 2 * time.Second,
	})

	sup.SuperviseFunc("network", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	sup.SuperviseFunc("deployments", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	// Verify observers were registered
	names := sup.ObserverNames()
	require.Len(t, names, 2, "should have 2 observers")
	assert.Contains(t, names, "network", "should contain network observer")
	assert.Contains(t, names, "deployments", "should contain deployments observer")
}
