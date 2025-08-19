//go:build integration && !linux
// +build integration,!linux

package kernel

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestStubCollectorIntegration tests collector behavior on non-Linux platforms
func TestStubCollectorIntegration(t *testing.T) {
	t.Logf("Running stub integration tests on %s", runtime.GOOS)

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "stub-integration-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test initial state
	assert.Equal(t, config.Name, collector.Name())
	assert.True(t, collector.IsHealthy())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test lifecycle on non-Linux
	err = collector.Start(ctx)
	require.NoError(t, err, "Stub collector should start without error")

	// Events channel should exist but not generate events
	eventsCh := collector.Events()
	require.NotNil(t, eventsCh, "Events channel should exist")

	// Should not receive events on non-Linux platforms
	select {
	case event := <-eventsCh:
		t.Errorf("Unexpected event on non-Linux platform: %v", event)
	case <-time.After(2 * time.Second):
		// Expected - no events should be generated on non-Linux
		t.Log("Correctly received no events on non-Linux platform")
	}

	// Test clean shutdown
	err = collector.Stop()
	require.NoError(t, err, "Stub collector should stop without error")

	assert.False(t, collector.IsHealthy(), "Collector should be unhealthy after stop")
}

// TestStubEBPFFunctions tests eBPF stub functions
func TestStubEBPFFunctions(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "stub-ebpf-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test eBPF stubs don't crash
	err = collector.startEBPF()
	assert.NoError(t, err, "startEBPF stub should not return error")

	// stopEBPF should not crash
	collector.stopEBPF()

	// readEBPFEvents should handle context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	collector.ctx = ctx
	collector.cancel = cancel

	go collector.readEBPFEvents()

	// Cancel context to stop readEBPFEvents
	cancel()
	time.Sleep(100 * time.Millisecond)

	t.Log("eBPF stub functions work correctly on non-Linux platform")
}

// TestMultipleStubCollectors tests multiple collectors on non-Linux
func TestMultipleStubCollectors(t *testing.T) {
	logger := zap.NewNop()
	const numCollectors = 3

	collectors := make([]*Collector, numCollectors)

	// Create multiple collectors
	for i := 0; i < numCollectors; i++ {
		config := DefaultConfig()
		config.Name = "stub-multi-test-" + string(rune('A'+i))

		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err, "Should create collector %d", i)
		collectors[i] = collector
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start all collectors
	for i, collector := range collectors {
		err := collector.Start(ctx)
		require.NoError(t, err, "Should start collector %d", i)
		assert.True(t, collector.IsHealthy(), "Collector %d should be healthy", i)
	}

	// Let them run briefly
	time.Sleep(1 * time.Second)

	// Stop all collectors
	for i, collector := range collectors {
		err := collector.Stop()
		require.NoError(t, err, "Should stop collector %d", i)
		assert.False(t, collector.IsHealthy(), "Collector %d should be unhealthy after stop", i)
	}

	t.Logf("Successfully managed %d stub collectors on %s", numCollectors, runtime.GOOS)
}

// TestStubResourceCleanup tests resource cleanup on non-Linux platforms
func TestStubResourceCleanup(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "stub-cleanup-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start and stop multiple times
	for i := 0; i < 5; i++ {
		err = collector.Start(ctx)
		require.NoError(t, err, "Start iteration %d should succeed", i)

		time.Sleep(100 * time.Millisecond)

		err = collector.Stop()
		require.NoError(t, err, "Stop iteration %d should succeed", i)
	}

	t.Log("Resource cleanup works correctly on non-Linux platform")
}

// TestStubCompatibility tests CO-RE compatibility on non-Linux
func TestStubCompatibility(t *testing.T) {
	// CO-RE compatibility should work on any platform for version detection
	compatibility, err := NewCoreCompatibility()
	require.NoError(t, err)

	version := compatibility.GetKernelVersion()
	t.Logf("Kernel version on %s: %s", runtime.GOOS, version.String())

	// Features detection is based on kernel version, not platform
	features := compatibility.GetFeatures()
	t.Logf("Features on %s: BTF=%v, RingBuffer=%v, BPF_LSM=%v",
		runtime.GOOS, features.HasBTF, features.HasRingBuffer, features.HasBPFLSM)

	// Test compatibility checks - these are based on kernel version
	ringBufferCompat := compatibility.IsCompatible("ring_buffer")
	bpfLSMCompat := compatibility.IsCompatible("bpf_lsm")
	t.Logf("Compatibility on %s: RingBuffer=%v, BPF_LSM=%v",
		runtime.GOOS, ringBufferCompat, bpfLSMCompat)

	// BPF LSM should generally not be available on non-Linux
	if runtime.GOOS != "linux" {
		assert.False(t, bpfLSMCompat, "BPF LSM should not be compatible on non-Linux")
	}

	// Test fallback strategies
	if !ringBufferCompat {
		fallback := compatibility.GetFallbackStrategy("ring_buffer")
		assert.Equal(t, "use_perf_buffer", fallback)
	}

	if !bpfLSMCompat {
		fallback := compatibility.GetFallbackStrategy("bpf_lsm")
		assert.Equal(t, "use_syscall_tracing", fallback)
	}
}
