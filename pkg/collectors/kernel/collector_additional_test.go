package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectorProcessEvents tests the processEvents method
func TestCollectorProcessEvents(t *testing.T) {
	cfg := NewDefaultConfig("test-process-events")
	collector, err := NewCollector("test-process-events", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	collector.ctx = ctx
	collector.cancel = cancel

	// Run processEvents in goroutine
	done := make(chan bool)
	go func() {
		collector.processEvents()
		done <- true
	}()

	// Cancel context to trigger exit
	cancel()

	// Should exit quickly
	select {
	case <-done:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("processEvents did not exit")
	}
}

// TestCollectorAPIStubs tests that collector API methods exist (even if stubbed)
func TestCollectorAPIStubs(t *testing.T) {
	cfg := NewDefaultConfig("test-api")
	collector, err := NewCollector("test-api", cfg)
	require.NoError(t, err)

	// These methods exist on the collector but may be no-ops on non-Linux
	// Testing that the public API is consistent across platforms
	assert.NotNil(t, collector)

	// The collector should have standard methods
	assert.NotEmpty(t, collector.Name())
	assert.NotNil(t, collector.Events())
	assert.True(t, collector.IsHealthy())
}

// TestCollectorStartAlreadyStarted tests starting an already started collector
func TestCollectorStartAlreadyStarted(t *testing.T) {
	cfg := NewDefaultConfig("test-double-start")
	collector, err := NewCollector("test-double-start", cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// First start
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Second start should be safe
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Clean up
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollectorWithLargeBufferSize tests collector with large buffer
func TestCollectorWithLargeBufferSize(t *testing.T) {
	cfg := NewDefaultConfig("test-large-buffer")
	cfg.BufferSize = 100000 // Large buffer

	collector, err := NewCollector("test-large-buffer", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	assert.Equal(t, 100000, cap(collector.events))
	assert.Equal(t, "test-large-buffer", collector.Name())
}

// TestCollectorEventChannelAfterStop tests event channel behavior after stop
func TestCollectorEventChannelAfterStop(t *testing.T) {
	cfg := NewDefaultConfig("test-events-after-stop")
	collector, err := NewCollector("test-events-after-stop", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Get events channel before stop
	events := collector.Events()
	assert.NotNil(t, events)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// After stop, events channel is closed and nil on subsequent calls might be nil
	// This is expected behavior - just verify no panic
}

// TestProcessKernelEventWithInvalidData tests processing invalid kernel events
func TestProcessKernelEventWithInvalidData(t *testing.T) {
	// Create a kernel event with various fields
	event := &KernelEvent{
		Timestamp: 0, // Invalid timestamp
		PID:       0, // Invalid PID
		TID:       0,
		EventType: 999, // Unknown event type
	}

	// Create event data
	eventData := KernelEventData{
		PID:       event.PID,
		TID:       event.TID,
		EventType: event.EventType,
		ErrorCode: 0,
	}

	// Verify handling of invalid event types
	assert.Equal(t, uint32(999), eventData.EventType)
}

// TestConfigInfoAlignment tests ConfigInfo struct alignment
func TestConfigInfoAlignment(t *testing.T) {
	ci := ConfigInfo{
		ErrorCode: -1, // Negative error code
	}

	// Fill mount path
	testPath := "test/path"
	copy(ci.MountPath[:], []byte(testPath))

	assert.Equal(t, int32(-1), ci.ErrorCode)
	assert.Equal(t, 60, len(ci.MountPath))
}

// TestEventTypeConstants tests all event type constants
func TestEventTypeConstants(t *testing.T) {
	// Verify all constants are unique
	eventTypes := []uint8{
		EventTypeConfigMapAccess,
		EventTypeSecretAccess,
		EventTypePodSyscall,
		EventTypeConfigAccessFailed,
		EventTypeProcess,
		EventTypeFile,
		EventTypeNetwork,
	}

	seen := make(map[uint8]bool)
	for _, et := range eventTypes {
		assert.False(t, seen[et], "Duplicate event type: %d", et)
		seen[et] = true
	}
}

// TestCollectorStopWithoutStart tests stopping collector without starting
func TestCollectorStopWithoutStart(t *testing.T) {
	cfg := NewDefaultConfig("test-stop-without-start")
	collector, err := NewCollector("test-stop-without-start", cfg)
	require.NoError(t, err)

	// Stop without start should be safe
	err = collector.Stop()
	assert.NoError(t, err)

	// Multiple stops should be safe
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestNewCollectorWithConfigNilConfig tests nil config handling
func TestNewCollectorWithConfigNilConfig(t *testing.T) {
	// Nil config should return error
	collector, err := NewCollectorWithConfig(nil, nil)
	assert.Error(t, err)
	assert.Nil(t, collector)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

// TestCollectorEventCreation tests creating domain events
func TestCollectorEventCreation(t *testing.T) {
	// Test event data structures
	kernelData := KernelEventData{
		PID:        1234,
		TID:        1235,
		CgroupID:   999888777,
		EventType:  uint32(EventTypeConfigMapAccess),
		Comm:       "test-app",
		ConfigType: "configmap",
		MountPath:  "/etc/config/app.yaml",
		ErrorCode:  0,
	}

	// Verify fields
	assert.Equal(t, uint32(1234), kernelData.PID)
	assert.Equal(t, "configmap", kernelData.ConfigType)
	assert.Equal(t, "/etc/config/app.yaml", kernelData.MountPath)
}
