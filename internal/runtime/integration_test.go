package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Integration Test 1: Runtime uses Sampler to filter events
func TestIntegration_Runtime_Sampler(t *testing.T) {
	proc := &mockProcessor{name: "test"}

	// Create runtime with custom sampling config
	runtime, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = true
		r.config.Sampling.DefaultRate = 0.0 // Drop all by default
		r.config.Sampling.Rules = []SamplingRule{
			{EventType: "network", Subtype: "critical", KeepAll: true}, // Keep critical
		}
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Process events - processor returns them
	proc.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		if string(rawEvent) == "event1" {
			return &domain.ObserverEvent{Type: "network", Subtype: "normal"}, nil
		}
		return &domain.ObserverEvent{Type: "network", Subtype: "critical"}, nil
	}

	if err := runtime.ProcessEvent(ctx, []byte("event1")); err != nil {
		t.Logf("ProcessEvent error (expected): %v", err)
	}
	if err := runtime.ProcessEvent(ctx, []byte("event2")); err != nil {
		t.Logf("ProcessEvent error (expected): %v", err)
	}

	// Note: Currently ProcessEvent doesn't use Sampler for filtering
	// This test documents that integration gap for future implementation
}

// Integration Test 2: Config validation prevents bad runtime
func TestIntegration_Config_Validation(t *testing.T) {
	proc := &mockProcessor{name: "test"}

	// Try to create runtime with invalid config
	_, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Backpressure.QueueSize = -1 // Invalid!
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
	assert.Contains(t, err.Error(), "QueueSize must be > 0")
}

// Integration Test 3: Runtime with multiple emitters (critical vs non-critical)
func TestIntegration_Runtime_MultipleEmitters(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	criticalEmitter := &mockEmitter{name: "critical", critical: true}
	nonCriticalEmitter := &mockEmitter{name: "non-critical", critical: false}

	runtime, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false // Disable sampling for this test
	}, WithEmitters(criticalEmitter, nonCriticalEmitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Process event - should emit to both
	proc.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		return &domain.ObserverEvent{Type: "test", Subtype: "event"}, nil
	}
	err = runtime.ProcessEvent(ctx, []byte("data"))
	require.NoError(t, err)

	// Wait for async drainQueue to emit
	time.Sleep(50 * time.Millisecond)

	// Verify both emitters received event
	assert.Equal(t, 1, criticalEmitter.EmittedCount(), "Critical emitter should receive event")
	assert.Equal(t, 1, nonCriticalEmitter.EmittedCount(), "Non-critical emitter should receive event")
}

// Integration Test 4: Critical emitter failure triggers retry
func TestIntegration_Runtime_CriticalEmitterFailure(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	criticalEmitter := &mockEmitter{
		name:       "critical",
		critical:   true,
		alwaysFail: true, // Always fail to test persistent failure
	}

	runtime, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false // Disable sampling for this test
	}, WithEmitters(criticalEmitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start runtime
	go func() {
		_ = runtime.Run(ctx) // Ignore: Test side effects only, not return value
	}()

	time.Sleep(50 * time.Millisecond)

	// Process event - ProcessEvent succeeds (just enqueues)
	proc.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		return &domain.ObserverEvent{Type: "test"}, nil
	}
	err = runtime.ProcessEvent(ctx, []byte("data"))
	require.NoError(t, err, "ProcessEvent should succeed (async queue)")

	// Wait for drainQueue attempts (will keep failing and retrying)
	time.Sleep(100 * time.Millisecond)

	// Verify event was never successfully emitted (emitter keeps failing)
	assert.Equal(t, 0, criticalEmitter.EmittedCount(), "Event should not be emitted when critical emitter always fails")
}

// Integration Test 5: Runtime health tracking
func TestIntegration_Runtime_HealthTracking(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	runtime, err := NewObserverRuntime(proc)
	require.NoError(t, err)

	// Initially not healthy (not running)
	assert.False(t, runtime.IsHealthy())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Should be healthy when running
	assert.True(t, runtime.IsHealthy())

	// Cancel and verify unhealthy
	cancel()
	time.Sleep(100 * time.Millisecond)
	assert.False(t, runtime.IsHealthy())
}

// Integration Test 6: Processor Setup/Teardown lifecycle
func TestIntegration_Runtime_ProcessorLifecycle(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	runtime, err := NewObserverRuntime(proc)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run should call Setup and Teardown
	err = runtime.Run(ctx)
	require.NoError(t, err)

	// Verify lifecycle methods were called
	assert.True(t, proc.setupCalled)
	assert.True(t, proc.teardownCalled)
}
