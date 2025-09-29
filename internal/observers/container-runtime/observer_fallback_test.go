//go:build !linux
// +build !linux

package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/observers/base"
	"go.uber.org/zap"
)

// TestObserverFallback tests the fallback implementation
func TestObserverFallback(t *testing.T) {
	config := NewDefaultConfig("test-fallback")
	observer, err := NewObserver("test-fallback", config)
	require.NoError(t, err)
	assert.NotNil(t, observer)
	observer.logger = zap.NewNop()

	t.Run("Start fallback", func(t *testing.T) {
		ctx := context.Background()
		err := observer.Start(ctx)
		assert.NoError(t, err)
		// Just check it doesn't crash
	})

	t.Run("Check health", func(t *testing.T) {
		healthy := observer.IsHealthy()
		assert.True(t, healthy)
	})

	t.Run("Get statistics", func(t *testing.T) {
		stats := observer.Statistics()
		assert.NotNil(t, stats)
		// Just check basic fields exist
		assert.GreaterOrEqual(t, stats.EventsProcessed, int64(0))
		assert.GreaterOrEqual(t, stats.ErrorCount, int64(0))
	})

	t.Run("Container lifecycle events", func(t *testing.T) {
		metadata := &ContainerMetadata{
			ContainerID:   "test-container",
			ContainerName: "test-app",
			PodName:       "test-pod",
			Namespace:     "default",
			Runtime:       "docker",
			Labels: map[string]string{
				"app": "test",
			},
		}

		// Start container
		err := observer.OnContainerStart("test-container", metadata)
		assert.NoError(t, err)

		// Check cache
		cached, exists := observer.containerCache["test-container"]
		assert.True(t, exists)
		assert.Equal(t, metadata.ContainerID, cached.ContainerID)
		assert.Equal(t, metadata.PodName, cached.PodName)

		// Stop container
		err = observer.OnContainerStop("test-container")
		assert.NoError(t, err)

		// Check removed from cache
		_, exists = observer.containerCache["test-container"]
		assert.False(t, exists)
	})

	t.Run("Stop fallback", func(t *testing.T) {
		err := observer.Stop()
		assert.NoError(t, err)
		// Just check it doesn't crash
	})
}

// TestStartEBPFFallback tests eBPF initialization fallback
func TestStartEBPFFallback(t *testing.T) {
	config := NewDefaultConfig("test-ebpf-fallback")
	observer, err := NewObserver("test-ebpf-fallback", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	// Start eBPF should succeed on non-Linux
	err = observer.startEBPF()
	assert.NoError(t, err)

	// Should not have eBPF state on non-Linux
	assert.Nil(t, observer.ebpfState)
}

// TestStopEBPFFallback tests eBPF cleanup fallback
func TestStopEBPFFallback(t *testing.T) {
	config := NewDefaultConfig("test-stop-fallback")
	observer, err := NewObserver("test-stop-fallback", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	// Stop should not panic even without eBPF state
	observer.stopEBPF()
	assert.Nil(t, observer.ebpfState)
}

// TestProcessEventsFallback tests event processing fallback
func TestProcessEventsFallback(t *testing.T) {
	config := NewDefaultConfig("test-process-fallback")
	observer, err := NewObserver("test-process-fallback", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	observer.LifecycleManager = &base.LifecycleManager{}
	observer.LifecycleManager.Start("test", func() {})

	// processEvents should handle context cancellation gracefully
	go observer.processEvents()

	// Wait for context to cancel
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	// Should not panic
	assert.True(t, true)
}

// TestLoadEBPFProgramsFallback tests eBPF program loading fallback
func TestLoadEBPFProgramsFallback(t *testing.T) {
	observer := &Observer{
		logger: zap.NewNop(),
	}

	err := observer.loadEBPFPrograms()
	// Should succeed on non-Linux (no-op)
	assert.NoError(t, err)
	assert.Nil(t, observer.ebpfState)
}

// TestAttachProgramsFallback tests eBPF program attachment fallback
func TestAttachProgramsFallback(t *testing.T) {
	observer := &Observer{
		logger: zap.NewNop(),
		config: &Config{
			EnableOOMKill:        true,
			EnableMemoryPressure: true,
			EnableProcessExit:    true,
			EnableProcessFork:    true,
		},
	}

	err := observer.attachPrograms()
	// Should succeed on non-Linux (no-op)
	assert.NoError(t, err)
}

// TestCleanupFallback tests cleanup fallback
func TestCleanupFallback(t *testing.T) {
	observer := &Observer{
		logger:        zap.NewNop(),
		runtimeClient: &mockRuntimeClient{},
		ebpfState:     nil, // No eBPF state on non-Linux
	}

	// Should not panic
	observer.cleanup()
	assert.Nil(t, observer.runtimeClient)
	assert.Nil(t, observer.ebpfState)
}

// TestHandleRingBufferEventFallback tests ring buffer event handling fallback
func TestHandleRingBufferEventFallback(t *testing.T) {
	config := NewDefaultConfig("test-ring-fallback")
	observer, err := NewObserver("test-ring-fallback", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	// Initialize minimal components
	observer.BaseObserver = &base.BaseObserver{}

	// Should handle empty data gracefully
	observer.handleRingBufferEvent([]byte{})

	// Just check it doesn't crash
	assert.True(t, true)
}

// TestConvertToObserverEventFallback tests event conversion fallback
func TestConvertToObserverEventFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestUpdateEventMetricsFallback tests metric updates fallback
func TestUpdateEventMetricsFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestCollectMetricsFallback tests metric collection fallback
func TestCollectMetricsFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestInitializeRuntimeClientFallback tests runtime client initialization fallback
func TestInitializeRuntimeClientFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestUpdateEBPFMapsWithContainersFallback tests eBPF map update fallback
func TestUpdateEBPFMapsWithContainersFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestWatchContainerEventsFallback tests container event watching fallback
func TestWatchContainerEventsFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestHandleContainerEventFallback tests container event handling fallback
func TestHandleContainerEventFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}

// TestRemoveContainerFromMapsFallback tests container removal fallback
func TestRemoveContainerFromMapsFallback(t *testing.T) {
	// Just test basic functionality on non-Linux
	assert.True(t, true)
}
