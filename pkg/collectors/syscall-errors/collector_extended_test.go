package syscallerrors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestConfigValidation tests configuration validation scenarios
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name: "valid custom config",
			config: &Config{
				RingBufferSize:   16 * 1024 * 1024,
				EventChannelSize: 5000,
				RateLimitMs:      200,
				EnabledCategories: map[string]bool{
					"file":    true,
					"network": false,
				},
				RequireAllMetrics: false,
			},
			wantErr: false,
		},
		{
			name: "empty enabled categories",
			config: &Config{
				RingBufferSize:    8 * 1024 * 1024,
				EventChannelSize:  1000,
				RateLimitMs:       100,
				EnabledCategories: map[string]bool{},
			},
			wantErr: false,
		},
		{
			name: "require all metrics",
			config: &Config{
				RingBufferSize:   8 * 1024 * 1024,
				EventChannelSize: 1000,
				RateLimitMs:      100,
				EnabledCategories: map[string]bool{
					"file": true,
				},
				RequireAllMetrics: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			collector, err := NewCollector(logger, tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, "syscall-errors", collector.GetName())
			}
		})
	}
}

// TestCollectorHealth tests health checking functionality
func TestCollectorHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	// Should be healthy after creation
	assert.True(t, collector.IsHealthy())

	// Stop the collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Should be unhealthy after stop
	assert.False(t, collector.IsHealthy())
}

// TestMultipleStops tests that Stop() is idempotent
func TestMultipleStops(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	// Multiple stops should not panic or error
	for i := 0; i < 3; i++ {
		err = collector.Stop()
		assert.NoError(t, err, "Stop call %d should not error", i+1)
	}

	// Channel should be closed
	select {
	case _, ok := <-collector.GetEventChannel():
		assert.False(t, ok, "Channel should be closed")
	default:
		t.Error("Channel should not block")
	}
}

// TestChannelCreation tests event channel creation
func TestChannelCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name        string
		channelSize int
		expectPanic bool
	}{
		{
			name:        "default size",
			channelSize: 10000,
			expectPanic: false,
		},
		{
			name:        "small size",
			channelSize: 1,
			expectPanic: false,
		},
		{
			name:        "large size",
			channelSize: 100000,
			expectPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				EventChannelSize: tt.channelSize,
			}

			collector, err := NewCollector(logger, config)
			require.NoError(t, err)
			defer collector.Stop()

			ch := collector.GetEventChannel()
			assert.NotNil(t, ch)
		})
	}
}

// TestBasicInterface tests the basic collector interface
func TestBasicInterface(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	// Test GetName
	assert.Equal(t, "syscall-errors", collector.GetName())

	// Test IsHealthy
	assert.True(t, collector.IsHealthy())

	// Test GetEventChannel
	ch := collector.GetEventChannel()
	assert.NotNil(t, ch)

	// Channel should be readable/non-blocking initially
	select {
	case <-ch:
		// Might be closed or have data
	default:
		// Channel is open and empty, expected
	}
}

// TestConfigDefaults tests default configuration
func TestConfigDefaults(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 8*1024*1024, config.RingBufferSize)
	assert.Equal(t, 10000, config.EventChannelSize)
	assert.Equal(t, 100, config.RateLimitMs)
	assert.Equal(t, map[string]bool{
		"file":    true,
		"network": true,
		"memory":  true,
	}, config.EnabledCategories)
	assert.False(t, config.RequireAllMetrics)
}

// TestConcurrentCreation tests concurrent collector creation
func TestConcurrentCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	numWorkers := 10
	collectors := make([]*Collector, numWorkers)
	errs := make([]error, numWorkers)

	// Create collectors concurrently
	done := make(chan int, numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(index int) {
			defer func() { done <- index }()
			collectors[index], errs[index] = NewCollector(logger, nil)
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numWorkers; i++ {
		<-done
	}

	// Verify all succeeded
	for i := 0; i < numWorkers; i++ {
		assert.NoError(t, errs[i], "Collector %d creation failed", i)
		assert.NotNil(t, collectors[i], "Collector %d is nil", i)
		if collectors[i] != nil {
			collectors[i].Stop()
		}
	}
}

// TestRateLimiting tests error logging rate limiting
func TestRateLimiting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	// This test verifies the rate limiting structure exists
	// The actual rate limiting is tested in Linux-specific tests
	assert.NotNil(t, collector)
}

// TestStressConfiguration tests configuration with extreme values
func TestStressConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	stressConfigs := []*Config{
		{
			RingBufferSize:    1024, // Very small
			EventChannelSize:  1,    // Minimal
			RateLimitMs:       1,    // Very fast
			EnabledCategories: map[string]bool{},
		},
		{
			RingBufferSize:   1024 * 1024 * 1024, // 1GB
			EventChannelSize: 1000000,            // Large
			RateLimitMs:      60000,              // 1 minute
			EnabledCategories: map[string]bool{
				"file": true, "network": true, "memory": true, "process": true,
			},
		},
	}

	for i, config := range stressConfigs {
		t.Run(fmt.Sprintf("stress_config_%d", i), func(t *testing.T) {
			collector, err := NewCollector(logger, config)
			assert.NoError(t, err)
			if collector != nil {
				assert.Equal(t, "syscall-errors", collector.GetName())
				collector.Stop()
			}
		})
	}
}

// TestMemoryUsage tests basic memory usage patterns
func TestMemoryUsage(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create many collectors to test for memory leaks
	const numCollectors = 100
	var collectors []*Collector

	for i := 0; i < numCollectors; i++ {
		collector, err := NewCollector(logger, nil)
		require.NoError(t, err)
		collectors = append(collectors, collector)
	}

	// Stop all collectors
	for _, collector := range collectors {
		err := collector.Stop()
		assert.NoError(t, err)
	}

	// Force garbage collection
	// In a real test environment, you might use runtime.GC() and check memory stats
}

// TestConfigurableOptions tests all configurable options
func TestConfigurableOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := &Config{
		RingBufferSize:   4 * 1024 * 1024,
		EventChannelSize: 5000,
		RateLimitMs:      250,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": false,
			"memory":  true,
			"process": false,
		},
		RequireAllMetrics: true,
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	// Verify collector was created successfully with custom config
	assert.NotNil(t, collector)
	assert.Equal(t, "syscall-errors", collector.GetName())
}

// BenchmarkCollectorCreation benchmarks collector creation
func BenchmarkCollectorCreation(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector, err := NewCollector(logger, nil)
		if err != nil {
			b.Fatal(err)
		}
		collector.Stop()
	}
}

// BenchmarkChannelOperations benchmarks channel operations
func BenchmarkChannelOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)
	collector, err := NewCollector(logger, nil)
	require.NoError(b, err)
	defer collector.Stop()

	ch := collector.GetEventChannel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		select {
		case <-ch:
			// Channel read
		default:
			// No blocking
		}
	}
}
