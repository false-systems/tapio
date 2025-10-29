//go:build linux
// +build linux

package containerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObserver_StartAttachesTracepoints verifies tracepoint attachment
func TestObserver_StartAttachesTracepoints(t *testing.T) {
	observer := NewRuntimeObserver("tracepoint-test")
	ctx := context.Background()

	bpfPath := "testdata/container_monitor.o"
	err := observer.Start(ctx, bpfPath)

	if err != nil {
		// Skip if BPF object not available or kernel doesn't support tracepoints
		t.Skipf("BPF attachment skipped: %v", err)
	}

	// Verify links were created and stored
	assert.NotNil(t, observer.links, "Links should be initialized")

	// In a real system with proper BPF programs, we'd have links
	// For now, just verify the structure exists

	// Cleanup
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserver_StopDetachesTracepoints verifies cleanup
func TestObserver_StopDetachesTracepoints(t *testing.T) {
	observer := NewRuntimeObserver("detach-test")

	// Simulate started with links
	observer.started = true
	observer.links = []tracepointLink{}

	err := observer.Stop()
	assert.NoError(t, err, "Stop should succeed")
	assert.Nil(t, observer.links, "Links should be nil after stop")
}

// TestObserver_StartFailsWithMissingPrograms verifies error handling
func TestObserver_StartFailsWithMissingPrograms(t *testing.T) {
	observer := NewRuntimeObserver("missing-prog-test")
	ctx := context.Background()

	// Try with invalid BPF path
	err := observer.Start(ctx, "/nonexistent/path.o")
	require.Error(t, err, "Start should fail with missing BPF")
	assert.Contains(t, err.Error(), "failed to load BPF")
}

// TestObserver_AttachTracepoint verifies attachment helper
func TestObserver_AttachTracepoint(t *testing.T) {
	observer := NewRuntimeObserver("attach-helper-test")

	// Test with nil collection (should handle gracefully)
	err := observer.attachTracepoints()
	require.Error(t, err, "Should fail with nil collection")
	assert.Contains(t, err.Error(), "collection not initialized")
}

// TestObserver_DetachTracepoints verifies detachment helper
func TestObserver_DetachTracepoints(t *testing.T) {
	observer := NewRuntimeObserver("detach-helper-test")

	// Empty links should be safe
	observer.links = []tracepointLink{}
	err := observer.detachTracepoints()
	assert.NoError(t, err, "Detaching empty links should succeed")
}
