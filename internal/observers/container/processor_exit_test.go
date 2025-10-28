//go:build linux
// +build linux

package container

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestExitProcessor_RejectsOOMEvents verifies processor ignores OOM events
func TestExitProcessor_RejectsOOMEvents(t *testing.T) {
	proc := NewExitProcessor()
	ctx := context.Background()

	// OOM event should be ignored (handled by OOMProcessor)
	oomEvent := ContainerEventBPF{
		Type:        EventTypeOOMKill,
		PID:         1234,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	result := proc.Process(ctx, oomEvent)
	assert.Nil(t, result, "OOM event should return nil")
}

// TestExitProcessor_ProcessesNormalExit verifies normal exit handling
func TestExitProcessor_ProcessesNormalExit(t *testing.T) {
	proc := NewExitProcessor()
	ctx := context.Background()

	// Create normal exit event (exitCode=0)
	cgroupPath := "/kubepods/burstable/pod-xyz/cri-containerd-container456"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	now := time.Now()
	exitEvent := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         9999,
		TID:         10000,
		ExitCode:    0,
		Signal:      0,
		CgroupPath:  cgroupBytes,
		MemoryLimit: 0,
		MemoryUsage: 0,
		TimestampNs: uint64(now.UnixNano()),
	}

	result := proc.Process(ctx, exitEvent)
	require.NotNil(t, result, "Exit event should return domain event")

	// Verify domain event structure
	assert.Equal(t, string(domain.EventTypeContainer), result.Type)
	assert.Equal(t, "container_exit", result.Subtype)

	// Verify container data
	require.NotNil(t, result.ContainerData)
	assert.Equal(t, "container456", result.ContainerData.ContainerID)
	assert.Equal(t, uint32(9999), result.ContainerData.PID)
	assert.Equal(t, int32(0), result.ContainerData.ExitCode)
	assert.Equal(t, "normal", result.ContainerData.Category)
	assert.Contains(t, result.ContainerData.Evidence, "exit_code=0")

	// Verify timestamp (within 1ms tolerance)
	timeDiff := result.Timestamp.Sub(now)
	assert.Less(t, timeDiff, time.Millisecond)
}

// TestExitProcessor_ProcessesErrorExit verifies error exit handling
func TestExitProcessor_ProcessesErrorExit(t *testing.T) {
	proc := NewExitProcessor()
	ctx := context.Background()

	// Create error exit event (exitCode=1, signal=0)
	cgroupPath := "/kubepods/guaranteed/pod-def/docker-errorcontainer"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	now := time.Now()
	exitEvent := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         7777,
		TID:         7778,
		ExitCode:    1,
		Signal:      0,
		CgroupPath:  cgroupBytes,
		MemoryLimit: 1024 * 1024 * 256,
		MemoryUsage: 1024 * 1024 * 100,
		TimestampNs: uint64(now.UnixNano()),
	}

	result := proc.Process(ctx, exitEvent)
	require.NotNil(t, result, "Error exit should return domain event")

	// Verify domain event structure
	assert.Equal(t, string(domain.EventTypeContainer), result.Type)
	assert.Equal(t, "container_exit", result.Subtype)

	// Verify container data
	require.NotNil(t, result.ContainerData)
	assert.Equal(t, "errorcontainer", result.ContainerData.ContainerID)
	assert.Equal(t, uint32(7777), result.ContainerData.PID)
	assert.Equal(t, int32(1), result.ContainerData.ExitCode)
	assert.Equal(t, int32(0), result.ContainerData.Signal)
	assert.Equal(t, "error", result.ContainerData.Category)
	assert.Contains(t, result.ContainerData.Evidence, "exit_code=1")
	assert.Equal(t, int64(1024*1024*256), result.ContainerData.MemoryLimit)
	assert.Equal(t, int64(1024*1024*100), result.ContainerData.MemoryUsage)
	assert.Equal(t, cgroupPath, result.ContainerData.CgroupPath)
}
