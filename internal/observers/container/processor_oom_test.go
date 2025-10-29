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

// TestOOMProcessor_RejectsNonOOMEvents verifies processor ignores exit events
func TestOOMProcessor_RejectsNonOOMEvents(t *testing.T) {
	proc := NewOOMProcessor()
	ctx := context.Background()

	// Exit event should be ignored
	exitEvent := ContainerEventBPF{
		Type:        EventTypeExit,
		PID:         1234,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	result := proc.Process(ctx, exitEvent)
	assert.Nil(t, result, "Exit event should return nil")
}

// TestOOMProcessor_ProcessesOOMEvent verifies OOM event handling
func TestOOMProcessor_ProcessesOOMEvent(t *testing.T) {
	proc := NewOOMProcessor()
	ctx := context.Background()

	// Create OOM event with cgroup path
	cgroupPath := "/kubepods/burstable/pod-abc/cri-containerd-container123"
	var cgroupBytes [256]byte
	copy(cgroupBytes[:], []byte(cgroupPath))

	now := time.Now()
	oomEvent := ContainerEventBPF{
		Type:        EventTypeOOMKill,
		PID:         5678,
		TID:         5679,
		ExitCode:    137,
		Signal:      9,
		CgroupPath:  cgroupBytes,
		MemoryLimit: 1024 * 1024 * 512, // 512MB
		MemoryUsage: 1024 * 1024 * 510, // 510MB
		TimestampNs: uint64(now.UnixNano()),
	}

	result := proc.Process(ctx, oomEvent)
	require.NotNil(t, result, "OOM event should return domain event")

	// Verify domain event structure
	assert.Equal(t, string(domain.EventTypeContainer), result.Type)
	assert.Equal(t, "oom_kill", result.Subtype)

	// Verify container data
	require.NotNil(t, result.ContainerData)
	assert.Equal(t, "container123", result.ContainerData.ContainerID)
	assert.Equal(t, uint32(5678), result.ContainerData.PID)
	assert.Equal(t, int32(137), result.ContainerData.ExitCode)
	assert.Equal(t, int32(9), result.ContainerData.Signal)
	assert.Equal(t, "oom_kill", result.ContainerData.Category)
	assert.Equal(t, int64(1024*1024*512), result.ContainerData.MemoryLimit)
	assert.Equal(t, int64(1024*1024*510), result.ContainerData.MemoryUsage)
	assert.Equal(t, cgroupPath, result.ContainerData.CgroupPath)

	// Verify evidence
	assert.Contains(t, result.ContainerData.Evidence, "oom_kill event detected")

	// Verify timestamp (within 1ms tolerance)
	timeDiff := result.Timestamp.Sub(now)
	assert.Less(t, timeDiff, time.Millisecond)
}

// TestOOMProcessor_EmptyCgroupPath verifies handling of empty cgroup path
func TestOOMProcessor_EmptyCgroupPath(t *testing.T) {
	proc := NewOOMProcessor()
	ctx := context.Background()

	var emptyPath [256]byte
	oomEvent := ContainerEventBPF{
		Type:        EventTypeOOMKill,
		PID:         1111,
		CgroupPath:  emptyPath,
		TimestampNs: uint64(time.Now().UnixNano()),
	}

	result := proc.Process(ctx, oomEvent)
	require.NotNil(t, result, "Should still process event with empty path")

	// Should have empty container ID
	assert.Equal(t, "", result.ContainerData.ContainerID)
	assert.Equal(t, "", result.ContainerData.CgroupPath)
}
