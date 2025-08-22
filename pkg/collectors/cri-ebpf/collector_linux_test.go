//go:build linux
// +build linux

package criebpf

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestLinuxCollectorStart(t *testing.T) {
	config := NewDefaultConfig("test-linux-cri-ebpf")
	config.BufferSize = 100
	config.EnableOOMKill = true
	config.EnableMemoryPressure = true
	config.EnableProcessExit = true
	config.EnableProcessFork = false // Reduce noise

	collector, err := NewCollector("test-linux-cri-ebpf", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to start - this will likely fail in CI/test environment
	// due to lack of eBPF capabilities, but we test the error path
	err = collector.Start(ctx)

	if err != nil {
		// Expected in most test environments
		t.Logf("Start failed as expected in test environment: %v", err)

		// Verify error is related to eBPF capabilities
		assert.Contains(t, err.Error(), "eBPF")

		// Ensure cleanup happens even on failure
		assert.False(t, collector.IsHealthy())
	} else {
		// If eBPF is available (unlikely in test), test full lifecycle
		t.Log("eBPF available, testing full lifecycle")

		assert.True(t, collector.IsHealthy())

		// Test events channel
		eventsChan := collector.Events()
		assert.NotNil(t, eventsChan)

		// Let it run briefly
		time.Sleep(100 * time.Millisecond)

		// Stop collector
		err = collector.Stop()
		assert.NoError(t, err)
		assert.False(t, collector.IsHealthy())
	}
}

func TestLinuxContainerMetadataWithK8sContext(t *testing.T) {
	collector, err := NewCollector("test", NewDefaultConfig("test"))
	require.NoError(t, err)

	// Add container metadata with K8s context
	containerID := "k8s-container-789"
	k8sMeta := &ContainerMetadata{
		ContainerID: containerID,
		PodUID:      "k8s-pod-uid-123",
		PodName:     "test-app-pod",
		Namespace:   "production",
		MemoryLimit: 2 * 1024 * 1024 * 1024, // 2GB
		CgroupID:    98765,
		CreatedAt:   time.Now(),
	}

	collector.UpdateContainerMetadata(containerID, k8sMeta)

	// Create BPF event for this container
	bpfEvent := &BPFContainerExitEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		PID:         5678,
		TGID:        5678,
		ExitCode:    0,
		CgroupID:    98765,
		MemoryUsage: 1536 * 1024 * 1024,     // 1.5GB
		MemoryLimit: 2 * 1024 * 1024 * 1024, // 2GB
		OOMKilled:   0,
	}

	copy(bpfEvent.ContainerID[:], GoStringToC(containerID, 64))
	copy(bpfEvent.Comm[:], GoStringToC("app-server", 16))

	// Convert to CollectorEvent
	event, err := collector.convertToCollectorEvent(bpfEvent)
	require.NoError(t, err)
	require.NotNil(t, event)

	// Verify K8s context is included
	require.NotNil(t, event.K8sContext)
	assert.Equal(t, "Pod", event.K8sContext.Kind)
	assert.Equal(t, "test-app-pod", event.K8sContext.Name)
	assert.Equal(t, "k8s-pod-uid-123", event.K8sContext.UID)
	assert.Equal(t, "production", event.K8sContext.Namespace)
	assert.Equal(t, containerID, event.K8sContext.ContainerID)

	// Verify correlation hints include pod UID
	assert.Equal(t, "k8s-pod-uid-123", event.CorrelationHints.PodUID)
}

func TestLinuxMemoryPressureDetection(t *testing.T) {
	collector, err := NewCollector("test", NewDefaultConfig("test"))
	require.NoError(t, err)

	// Test high memory utilization (95%)
	bpfEvent := &BPFContainerExitEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		PID:         9999,
		TGID:        9999,
		ExitCode:    0,
		CgroupID:    55555,
		MemoryUsage: 950 * 1024 * 1024,  // 950MB
		MemoryLimit: 1000 * 1024 * 1024, // 1000MB (95% utilization)
		OOMKilled:   0,
	}

	copy(bpfEvent.ContainerID[:], GoStringToC("high-memory-container", 64))
	copy(bpfEvent.Comm[:], GoStringToC("memory-app", 16))

	// Convert to CollectorEvent
	event, err := collector.convertToCollectorEvent(bpfEvent)
	require.NoError(t, err)

	// Verify metadata includes memory information
	assert.Equal(t, "950000000", event.Metadata.Labels["memory_usage"])
	assert.Equal(t, "1000000000", event.Metadata.Labels["memory_limit"])

	// This would trigger memory pressure metrics in a real scenario
	// We can't easily test the actual metrics in unit tests, but we verify
	// the conversion includes the necessary data
}

func TestLinuxEBPFProgramConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "OOM kill only",
			config: &Config{
				Name:                 "oom-only",
				BufferSize:           1000,
				EnableOOMKill:        true,
				EnableMemoryPressure: false,
				EnableProcessExit:    false,
				EnableProcessFork:    false,
				RingBufferSize:       256 * 1024,
				MetricsInterval:      30 * time.Second,
				MetadataCacheSize:    1000,
				MetadataCacheTTL:     5 * time.Minute,
			},
		},
		{
			name: "memory pressure only",
			config: &Config{
				Name:                 "memory-only",
				BufferSize:           1000,
				EnableOOMKill:        false,
				EnableMemoryPressure: true,
				EnableProcessExit:    false,
				EnableProcessFork:    false,
				RingBufferSize:       256 * 1024,
				MetricsInterval:      30 * time.Second,
				MetadataCacheSize:    1000,
				MetadataCacheTTL:     5 * time.Minute,
			},
		},
		{
			name: "all features enabled",
			config: &Config{
				Name:                 "full-featured",
				BufferSize:           1000,
				EnableOOMKill:        true,
				EnableMemoryPressure: true,
				EnableProcessExit:    true,
				EnableProcessFork:    true,
				RingBufferSize:       512 * 1024,
				MetricsInterval:      60 * time.Second,
				MetadataCacheSize:    5000,
				MetadataCacheTTL:     10 * time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config.Name, tt.config)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify config is stored correctly
			assert.Equal(t, tt.config.EnableOOMKill, collector.config.EnableOOMKill)
			assert.Equal(t, tt.config.EnableMemoryPressure, collector.config.EnableMemoryPressure)
			assert.Equal(t, tt.config.EnableProcessExit, collector.config.EnableProcessExit)
			assert.Equal(t, tt.config.EnableProcessFork, collector.config.EnableProcessFork)
		})
	}
}

func TestLinuxEventPriorityCalculation(t *testing.T) {
	collector, err := NewCollector("test", NewDefaultConfig("test"))
	require.NoError(t, err)

	tests := []struct {
		name             string
		oomKilled        uint8
		exitCode         int32
		expectedType     domain.CollectorEventType
		expectedPriority domain.EventPriority
	}{
		{
			name:             "normal exit",
			oomKilled:        0,
			exitCode:         0,
			expectedType:     domain.EventTypeContainerStop,
			expectedPriority: domain.PriorityNormal,
		},
		{
			name:             "error exit",
			oomKilled:        0,
			exitCode:         1,
			expectedType:     domain.EventTypeContainerExit,
			expectedPriority: domain.PriorityHigh,
		},
		{
			name:             "oom killed",
			oomKilled:        1,
			exitCode:         137,
			expectedType:     domain.EventTypeContainerOOM,
			expectedPriority: domain.PriorityCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bpfEvent := &BPFContainerExitEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				PID:         1111,
				TGID:        1111,
				ExitCode:    tt.exitCode,
				CgroupID:    11111,
				MemoryUsage: 100 * 1024 * 1024,
				MemoryLimit: 500 * 1024 * 1024,
				OOMKilled:   tt.oomKilled,
			}

			copy(bpfEvent.ContainerID[:], GoStringToC("priority-test-container", 64))
			copy(bpfEvent.Comm[:], GoStringToC("test-cmd", 16))

			event, err := collector.convertToCollectorEvent(bpfEvent)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedType, event.Type)
			assert.Equal(t, tt.expectedPriority, event.Metadata.Priority)
		})
	}
}

func TestLinuxConcurrentMetadataOperations(t *testing.T) {
	collector, err := NewCollector("test", NewDefaultConfig("test"))
	require.NoError(t, err)

	// Test concurrent metadata updates
	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	// Start multiple goroutines updating metadata
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				containerID := fmt.Sprintf("container-%d-%d", id, j)
				meta := &ContainerMetadata{
					ContainerID: containerID,
					PodUID:      fmt.Sprintf("pod-uid-%d", id),
					PodName:     fmt.Sprintf("pod-%d", id),
					Namespace:   "test",
					MemoryLimit: uint64(1024 * 1024 * 1024),
					CgroupID:    uint64(id*1000 + j),
					CreatedAt:   time.Now(),
				}

				collector.UpdateContainerMetadata(containerID, meta)

				// Verify we can read it back
				retrieved := collector.getContainerMetadata(containerID)
				assert.NotNil(t, retrieved)
				assert.Equal(t, containerID, retrieved.ContainerID)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// Success
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations to complete")
		}
	}

	// Verify final state
	totalExpected := numGoroutines * numOperations
	assert.Equal(t, totalExpected, len(collector.containerCache))
}
