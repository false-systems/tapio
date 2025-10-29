//go:build linux
// +build linux

package container

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObserver_StartLoadsBPFAndCreatesReader verifies full integration
func TestObserver_StartLoadsBPFAndCreatesReader(t *testing.T) {
	observer := NewRuntimeObserver("integration-test")
	ctx := context.Background()

	// Try to start with test BPF object
	bpfPath := "testdata/container_monitor.o"
	err := observer.Start(ctx, bpfPath)

	if err != nil {
		// If BPF object doesn't exist, skip gracefully
		t.Skipf("BPF object not available: %v", err)
	}

	// Verify observer marked as started
	assert.True(t, observer.started, "Observer should be marked as started")

	// Verify ring reader was created
	assert.NotNil(t, observer.ringReader, "Ring reader should be initialized")

	// Cleanup
	err = observer.Stop()
	assert.NoError(t, err, "Stop should succeed")
}

// TestObserver_StopClosesRingAndCleansBPF verifies full cleanup
func TestObserver_StopClosesRingAndCleansBPF(t *testing.T) {
	observer := NewRuntimeObserver("cleanup-test")

	// Manually set started state (simulating successful Start)
	observer.started = true

	// Stop should clean up
	err := observer.Stop()
	assert.NoError(t, err, "Stop should succeed")
	assert.False(t, observer.started, "Observer should be marked as stopped")
	assert.Nil(t, observer.ringReader, "Ring reader should be nil after stop")
}

// TestObserver_IntegrationWithProcessing verifies end-to-end event flow
func TestObserver_IntegrationWithProcessing(t *testing.T) {
	observer := NewRuntimeObserver("e2e-test")
	ctx := context.Background()

	bpfPath := "testdata/container_monitor.o"
	err := observer.Start(ctx, bpfPath)

	if err != nil {
		t.Skipf("BPF object not available: %v", err)
	}

	// Process method should still work after Start
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte("/test/cgroup"))

	evt := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         99999,
		CgroupPath:  cgroupBytes,
		TimestampNs: 1234567890,
	}

	result := observer.Process(ctx, evt)
	require.NotNil(t, result, "Process should work after Start")
	assert.Equal(t, "container_exit", result.Subtype)

	// Cleanup
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserver_StartFailsIfAlreadyStarted verifies idempotency
func TestObserver_StartFailsIfAlreadyStarted(t *testing.T) {
	observer := NewRuntimeObserver("idempotency-test")
	observer.started = true

	ctx := context.Background()
	err := observer.Start(ctx, "/any/path.o")

	require.Error(t, err, "Start should fail if already started")
	assert.Contains(t, err.Error(), "already started")
}
