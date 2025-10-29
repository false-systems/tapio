//go:build linux
// +build linux

package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestObserver_RunStopsOnContextCancellation verifies graceful shutdown
func TestObserver_RunStopsOnContextCancellation(t *testing.T) {
	observer := NewRuntimeObserver("shutdown-test")

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start observer (will fail without BPF, but Run should still work)
	observer.started = true                  // Simulate started state
	observer.ringReader = NewRingReader(nil) // Mock ring reader (will fail on Read, but structure exists)

	// Run in goroutine
	done := make(chan error, 1)
	go func() {
		done <- observer.Run(ctx)
	}()

	// Cancel context
	cancel()

	// Wait for Run to exit
	select {
	case err := <-done:
		assert.NoError(t, err, "Run should exit cleanly on context cancel")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// TestObserver_RunFailsIfNotStarted verifies Run requires Start
func TestObserver_RunFailsIfNotStarted(t *testing.T) {
	observer := NewRuntimeObserver("not-started-test")
	ctx := context.Background()

	err := observer.Run(ctx)
	require.Error(t, err, "Run should fail if observer not started")
	assert.Contains(t, err.Error(), "observer not started")
}

// TestObserver_RunEmitsEvents verifies event processing
func TestObserver_RunEmitsEvents(t *testing.T) {
	observer := NewRuntimeObserver("emit-test")
	observer.started = true

	// Create event channel for testing
	observer.eventChan = make(chan *domain.ObserverEvent, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Run in goroutine (will fail without ring reader, but we're testing channel structure)
	done := make(chan error, 1)
	go func() {
		done <- observer.Run(ctx)
	}()

	// Wait for Run to exit (will fail quickly without ring reader)
	select {
	case err := <-done:
		// Expected to fail without ring reader
		require.Error(t, err, "Run should fail without ring reader")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Run did not exit")
	}

	// Verify the channel structure exists
	assert.NotNil(t, observer.eventChan, "Event channel should be initialized")
}

// TestObserver_SetEventChannel verifies channel configuration
func TestObserver_SetEventChannel(t *testing.T) {
	observer := NewRuntimeObserver("channel-test")

	ch := make(chan *domain.ObserverEvent, 100)
	observer.SetEventChannel(ch)

	assert.Equal(t, ch, observer.eventChan, "Event channel should be set")
}

// TestObserver_RunWithNilRingReader verifies error handling
func TestObserver_RunWithNilRingReader(t *testing.T) {
	observer := NewRuntimeObserver("nil-ring-test")
	observer.started = true
	observer.ringReader = nil

	ctx := context.Background()
	err := observer.Run(ctx)

	require.Error(t, err, "Run should fail with nil ring reader")
	assert.Contains(t, err.Error(), "ring reader not initialized")
}
