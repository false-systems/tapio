//go:build linux
// +build linux

package container

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewObserver_CreatesWithProcessors verifies observer initialization
func TestNewObserver_CreatesWithProcessors(t *testing.T) {
	observer := NewObserver("test-observer")
	require.NotNil(t, observer, "Observer should be created")
	assert.Equal(t, "test-observer", observer.name)
	assert.NotNil(t, observer.oomProcessor, "OOMProcessor should be initialized")
	assert.NotNil(t, observer.exitProcessor, "ExitProcessor should be initialized")
}

// TestObserver_ProcessOOMEvent verifies OOM event dispatching
func TestObserver_ProcessOOMEvent(t *testing.T) {
	observer := NewObserver("test-observer")
	ctx := context.Background()

	// Create OOM event
	cgroupPath := "/kubepods/burstable/pod-abc/cri-containerd-oomtest"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	oomEvent := ContainerEventBPF{
		Type:        EventTypeOOMKill,
		PID:         12345,
		ExitCode:    137,
		Signal:      9,
		CgroupPath:  cgroupBytes,
		MemoryLimit: 1024 * 1024 * 512,
		MemoryUsage: 1024 * 1024 * 510,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	// Process through observer
	result := observer.Process(ctx, oomEvent)
	require.NotNil(t, result, "OOM event should be processed")

	// Verify it went through OOMProcessor
	assert.Equal(t, "oom_kill", result.Subtype)
	assert.Equal(t, "oomtest", result.ContainerData.ContainerID)
	assert.Equal(t, "oom_kill", result.ContainerData.Category)
}

// TestObserver_ProcessExitEvent verifies exit event dispatching
func TestObserver_ProcessExitEvent(t *testing.T) {
	observer := NewObserver("test-observer")
	ctx := context.Background()

	// Create normal exit event
	cgroupPath := "/kubepods/guaranteed/pod-xyz/docker-normaltest"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	exitEvent := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         54321,
		ExitCode:    0,
		Signal:      0,
		CgroupPath:  cgroupBytes,
		MemoryLimit: 1024 * 1024 * 256,
		MemoryUsage: 1024 * 1024 * 100,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	// Process through observer
	result := observer.Process(ctx, exitEvent)
	require.NotNil(t, result, "Exit event should be processed")

	// Verify it went through ExitProcessor
	assert.Equal(t, "container_exit", result.Subtype)
	assert.Equal(t, "normaltest", result.ContainerData.ContainerID)
	assert.Equal(t, "normal", result.ContainerData.Category)
}

// TestObserver_ProcessUnknownEvent verifies handling of unknown event types
func TestObserver_ProcessUnknownEvent(t *testing.T) {
	observer := NewObserver("test-observer")
	ctx := context.Background()

	// Create event with unknown type
	unknownEvent := ContainerEventBPF{
		Type:        999, // Invalid event type
		PID:         99999,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	// Process through observer
	result := observer.Process(ctx, unknownEvent)
	assert.Nil(t, result, "Unknown event should return nil")
}

// TestObserver_ProcessorChainOrder verifies OOMProcessor runs before ExitProcessor
func TestObserver_ProcessorChainOrder(t *testing.T) {
	observer := NewObserver("test-observer")
	ctx := context.Background()

	// If somehow an event has Type=Exit but looks like OOM,
	// ExitProcessor should handle it (not OOMProcessor)
	cgroupPath := "/kubepods/burstable/pod-test/cri-containerd-chaintest"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	exitEvent := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         77777,
		ExitCode:    137,
		Signal:      9,
		CgroupPath:  cgroupBytes,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	result := observer.Process(ctx, exitEvent)
	require.NotNil(t, result)

	// Should be processed as exit (not OOM) because Type=Exit
	assert.Equal(t, "container_exit", result.Subtype)
	// But classification should recognize it as error exit (exitCode=137, signal=9)
	assert.Equal(t, "error", result.ContainerData.Category)
}
