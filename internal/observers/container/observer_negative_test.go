//go:build linux
// +build linux

package container

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObserver_StartWithInvalidBPFPath verifies error handling for invalid BPF path
func TestObserver_StartWithInvalidBPFPath(t *testing.T) {
	observer := NewRuntimeObserver("test-observer")
	ctx := context.Background()

	// Try to start with non-existent BPF object
	err := observer.Start(ctx, "/nonexistent/path/to/bpf.o")
	require.Error(t, err, "Start should fail with invalid BPF path")
	assert.Contains(t, err.Error(), "failed to load BPF")
}

// TestObserver_StopWithoutStart verifies Stop is safe without Start
func TestObserver_StopWithoutStart(t *testing.T) {
	observer := NewRuntimeObserver("test-observer")

	// Stop without Start should not panic
	err := observer.Stop()
	assert.NoError(t, err, "Stop without Start should be safe")
}

// TestObserver_StopCleansUpResources verifies proper cleanup
func TestObserver_StopCleansUpResources(t *testing.T) {
	observer := NewRuntimeObserver("test-observer")

	// Manually set cleanup state (simulating started observer)
	observer.started = true

	// Stop should clean up
	err := observer.Stop()
	assert.NoError(t, err, "Stop should clean up successfully")
	assert.False(t, observer.started, "Observer should be marked as stopped")
}

// TestObserver_StartTwice verifies Start can't be called twice
func TestObserver_StartTwice(t *testing.T) {
	observer := NewRuntimeObserver("test-observer")
	observer.started = true // Simulate already started

	ctx := context.Background()
	err := observer.Start(ctx, "/some/path.o")
	require.Error(t, err, "Start should fail if already started")
	assert.Contains(t, err.Error(), "already started")
}

// TestObserver_ProcessBeforeStart verifies Process works before Start
func TestObserver_ProcessBeforeStart(t *testing.T) {
	observer := NewRuntimeObserver("test-observer")
	ctx := context.Background()

	// Process should work even before Start (processors are initialized in constructor)
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte("/test/path"))

	evt := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         12345,
		CgroupPath:  cgroupBytes,
		TimestampNs: 1234567890,
	}

	result := observer.Process(ctx, evt)
	assert.NotNil(t, result, "Process should work before Start")
}
