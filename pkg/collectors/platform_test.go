package collectors

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors/unified"
	"github.com/yairfalse/tapio/pkg/ebpf"
)

func TestPlatformDetection(t *testing.T) {
	platform := GetCurrentPlatform()

	// Verify platform detection
	assert.Equal(t, runtime.GOOS, platform.OS)
	assert.Equal(t, runtime.GOARCH, platform.Architecture)

	// Verify platform-specific features
	switch runtime.GOOS {
	case "linux":
		assert.True(t, platform.HasEBPF, "Linux should support eBPF")
		assert.True(t, platform.HasJournald, "Linux should support journald")
		assert.True(t, platform.HasSystemd, "Linux should support systemd")
	case "darwin", "windows":
		assert.False(t, platform.HasEBPF, "Non-Linux platforms should not support eBPF")
		assert.False(t, platform.HasJournald, "Non-Linux platforms should not support journald")
		assert.False(t, platform.HasSystemd, "Non-Linux platforms should not support systemd")
	}
}

func TestSupportedCollectors(t *testing.T) {
	supported := GetSupportedCollectors()

	// Basic collectors should always be supported
	assert.Contains(t, supported, "simple")
	assert.Contains(t, supported, "basic")
	assert.Contains(t, supported, "mock")
	assert.Contains(t, supported, "stub")

	// Platform-specific collectors
	if runtime.GOOS == "linux" {
		assert.Contains(t, supported, "ebpf")
		assert.Contains(t, supported, "journald")
		assert.Contains(t, supported, "memory")
		assert.Contains(t, supported, "network")
	} else {
		// On non-Linux, these should not be in the supported list
		// (they will have stub implementations)
		assert.NotContains(t, supported, "ebpf")
		assert.NotContains(t, supported, "journald")
	}
}

func TestCollectorSupport(t *testing.T) {
	testCases := []struct {
		collector string
		supported bool
	}{
		{"simple", true},
		{"basic", true},
		{"mock", true},
		{"stub", true},
	}

	// Add platform-specific test cases
	if runtime.GOOS == "linux" {
		testCases = append(testCases,
			struct {
				collector string
				supported bool
			}{"ebpf", true},
			struct {
				collector string
				supported bool
			}{"journald", true},
		)
	} else {
		testCases = append(testCases,
			struct {
				collector string
				supported bool
			}{"ebpf", false},
			struct {
				collector string
				supported bool
			}{"journald", false},
		)
	}

	for _, tc := range testCases {
		t.Run(tc.collector, func(t *testing.T) {
			supported := IsCollectorSupported(tc.collector)
			assert.Equal(t, tc.supported, supported,
				"Collector %s support should be %v on %s", tc.collector, tc.supported, runtime.GOOS)
		})
	}
}

func TestPlatformMessage(t *testing.T) {
	testCases := []string{"ebpf", "journald", "systemd", "basic"}

	for _, collector := range testCases {
		t.Run(collector, func(t *testing.T) {
			message := GetPlatformMessage(collector)
			assert.NotEmpty(t, message, "Platform message should not be empty")
			assert.Contains(t, message, runtime.GOOS, "Message should mention the current platform")
		})
	}
}

func TestEBPFAdapter(t *testing.T) {
	adapter, err := NewEBPFAdapter()
	require.NoError(t, err)
	assert.NotNil(t, adapter)

	// Test configuration
	config := types.CollectorConfig{
		Name:            "test-ebpf",
		Type:            "ebpf",
		Enabled:         true,
		EventBufferSize: 1000,
	}

	err = adapter.Configure(config)
	assert.NoError(t, err)

	// Test lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = adapter.Start(ctx)
	assert.NoError(t, err)

	// Test health check
	health := adapter.Health()
	assert.NotNil(t, health)
	assert.Equal(t, types.HealthStatusHealthy, health.Status)

	// Test statistics
	stats := adapter.GetStats()
	assert.NotNil(t, stats)
	assert.Contains(t, stats.Custom, "platform")
	assert.Contains(t, stats.Custom, "ebpf_available")

	// Test platform-specific behavior
	if runtime.GOOS == "linux" {
		assert.True(t, adapter.IsAvailable(), "eBPF should be available on Linux")
		assert.Contains(t, health.Message, "healthy")
		assert.Equal(t, true, stats.Custom["native_mode"])
	} else {
		assert.False(t, adapter.IsAvailable(), "eBPF should not be available on non-Linux")
		assert.Contains(t, health.Message, "stub mode")
		assert.Equal(t, false, stats.Custom["native_mode"])
	}

	// Test memory stats
	memStats, err := adapter.GetMemoryStats()
	assert.NoError(t, err)
	assert.NotNil(t, memStats)

	// Test memory predictions
	limits := map[uint32]uint64{1: 1024 * 1024 * 1024} // 1GB limit for PID 1
	predictions, err := adapter.GetMemoryPredictions(limits)
	assert.NoError(t, err)
	assert.NotNil(t, predictions)
	assert.Contains(t, predictions, uint32(1))

	// Test stopping
	err = adapter.Stop()
	assert.NoError(t, err)
}

func TestEBPFMonitor(t *testing.T) {
	monitor := ebpf.NewMonitor(ebpf.DefaultConfig())
	assert.NotNil(t, monitor)

	// Test availability
	available := monitor.IsAvailable()
	if runtime.GOOS == "linux" {
		// On Linux, availability depends on actual eBPF support
		// We can't assert true/false without checking the system
		t.Logf("eBPF availability on Linux: %v", available)
	} else {
		assert.False(t, available, "eBPF should not be available on non-Linux")
	}

	// Test starting (should not fail even if not available)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := monitor.Start(ctx)
	if runtime.GOOS == "linux" {
		// On Linux, this might succeed or fail depending on system support
		t.Logf("eBPF monitor start result on Linux: %v", err)
	} else {
		assert.NoError(t, err, "Stub monitor should start without error")
	}

	// Test memory stats
	stats, err := monitor.GetMemoryStats()
	if runtime.GOOS == "linux" && available {
		// On Linux with eBPF support, this should work
		assert.NoError(t, err)
	} else {
		// On non-Linux or without eBPF, this should return mock data
		assert.NoError(t, err)
		assert.NotNil(t, stats)
	}

	// Test memory predictions
	limits := map[uint32]uint64{1: 1024 * 1024 * 1024}
	predictions, err := monitor.GetMemoryPredictions(limits)
	assert.NoError(t, err)
	assert.NotNil(t, predictions)

	// Test stopping
	err = monitor.Stop()
	assert.NoError(t, err)
}

func TestCrossPlatformBuild(t *testing.T) {
	// This test ensures that the code compiles on all platforms
	// by testing the basic interfaces

	// Test platform detection
	platform := GetCurrentPlatform()
	assert.NotEmpty(t, platform.OS)
	assert.NotEmpty(t, platform.Architecture)

	// Test eBPF factory
	factory := NewEBPFCollectorFactory()
	assert.NotNil(t, factory)

	// Test configuration validation
	config := types.CollectorConfig{
		Name:            "test",
		Type:            "ebpf",
		Enabled:         true,
		EventBufferSize: 1000,
		Extra:           map[string]interface{}{},
	}

	err := factory.ValidateConfig(config)
	assert.NoError(t, err)

	// Test collector creation
	collector, err := factory.CreateCollector(config)
	assert.NoError(t, err)
	assert.NotNil(t, collector)

	// Test requirements
	requirements := factory.GetRequirements()
	assert.NotNil(t, requirements)

	// Platform-specific requirements
	if runtime.GOOS == "linux" {
		assert.NotEmpty(t, requirements.Capabilities)
		assert.NotEmpty(t, requirements.KernelVersion)
		assert.NotEmpty(t, requirements.Features)
	} else {
		// Stub mode should have minimal requirements
		assert.Empty(t, requirements.Capabilities)
		assert.Empty(t, requirements.KernelVersion)
		assert.Empty(t, requirements.Features)
	}
}

func TestEventGeneration(t *testing.T) {
	// Test that adapters generate events appropriately on all platforms
	adapter, err := NewEBPFAdapter()
	require.NoError(t, err)

	config := types.CollectorConfig{
		Name:            "test-events",
		Type:            "ebpf",
		Enabled:         true,
		EventBufferSize: 100,
	}

	err = adapter.Configure(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = adapter.Start(ctx)
	require.NoError(t, err)

	// Wait for events
	eventChan := adapter.Events()

	select {
	case event := <-eventChan:
		assert.NotNil(t, event)
		assert.NotEmpty(t, event.ID)
		assert.NotEmpty(t, event.Source.Collector)
		assert.Contains(t, event.Attributes, "platform")
		assert.Equal(t, runtime.GOOS, event.Attributes["platform"])

		// On non-Linux, events should be marked as mock
		if runtime.GOOS != "linux" {
			assert.Equal(t, true, event.Attributes["mock"])
		}

	case <-time.After(35 * time.Second):
		// Events should be generated within 35 seconds
		t.Fatal("No events received within timeout")
	}

	err = adapter.Stop()
	assert.NoError(t, err)
}
