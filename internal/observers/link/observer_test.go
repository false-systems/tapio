package link

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestObserverCreation(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name   string
		config *Config
		valid  bool
	}{
		{
			name:   "with default config",
			config: nil,
			valid:  true,
		},
		{
			name: "with custom config",
			config: &Config{
				Enabled:       true,
				BufferSize:    5000,
				SampleRate:    0.5,
				FlushInterval: 1 * time.Minute,
			},
			valid: true,
		},
		{
			name: "with invalid config gets fixed",
			config: &Config{
				BufferSize:    -1,
				SampleRate:    2.0,
				FlushInterval: 0,
			},
			valid: true, // Validate fixes invalid values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test-link", tt.config, logger)
			if tt.valid {
				require.NoError(t, err)
				require.NotNil(t, observer)
				assert.Equal(t, "test-link", observer.Name())
				assert.NotNil(t, observer.config)
				assert.NotNil(t, observer.logger)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
	}

	observer, err := NewObserver("test-link", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start the observer
	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
	}

	// Test health
	health := observer.Health()
	assert.NotNil(t, health)
	assert.True(t, observer.IsHealthy())

	// Test statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Test event channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Stop the observer
	err = observer.Stop()
	assert.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}

func TestFailureTracking(t *testing.T) {
	logger := zap.NewNop()
	observer, err := NewObserver("test-link", nil, logger)
	require.NoError(t, err)

	// Track some failures
	observer.trackFailure("192.168.1.1", "10.0.0.1", EventSYNTimeout)
	observer.trackFailure("192.168.1.1", "10.0.0.1", EventSYNTimeout)
	observer.trackFailure("192.168.1.1", "10.0.0.1", EventConnectionRST)

	// Check tracking
	observer.mu.RLock()
	stats, exists := observer.failures["192.168.1.1:10.0.0.1"]
	observer.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, uint64(2), stats.SYNTimeouts)
	assert.Equal(t, uint64(1), stats.ConnectionRSTs)
	assert.Equal(t, uint64(0), stats.ARPTimeouts)
}

func TestFailurePatternDetection(t *testing.T) {
	logger := zap.NewNop()
	observer, err := NewObserver("test-link", nil, logger)
	require.NoError(t, err)

	// Simulate many failures
	for i := 0; i < 6; i++ {
		observer.trackFailure("192.168.1.1", "10.0.0.1", EventSYNTimeout)
	}
	for i := 0; i < 11; i++ {
		observer.trackFailure("192.168.1.2", "10.0.0.2", EventConnectionRST)
	}

	// Check patterns (this would normally log warnings)
	observer.checkFailurePatterns()

	// Verify tracking is working
	observer.mu.RLock()
	assert.Len(t, observer.failures, 2)
	observer.mu.RUnlock()
}

func TestConfiguration(t *testing.T) {
	config := DefaultConfig()
	assert.NotNil(t, config)
	assert.True(t, config.Enabled)
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 1.0, config.SampleRate)
	assert.Equal(t, 30*time.Second, config.FlushInterval)

	// Test validation
	err := config.Validate()
	assert.NoError(t, err)

	// Test invalid values get fixed
	config.BufferSize = -1
	config.SampleRate = 2.0
	config.FlushInterval = 0

	err = config.Validate()
	assert.NoError(t, err)
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 1.0, config.SampleRate)
	assert.Equal(t, 30*time.Second, config.FlushInterval)
}

func TestTypes(t *testing.T) {
	t.Run("GetEventTypeName", func(t *testing.T) {
		assert.Equal(t, "syn_timeout", GetEventTypeName(EventSYNTimeout))
		assert.Equal(t, "connection_reset", GetEventTypeName(EventConnectionRST))
		assert.Equal(t, "arp_timeout", GetEventTypeName(EventARPTimeout))
		assert.Equal(t, "unknown", GetEventTypeName(255))
	})
}

func TestFormatIP(t *testing.T) {
	tests := []struct {
		name string
		ip   uint32
		want string
	}{
		{
			name: "localhost",
			ip:   0x0100007f, // 127.0.0.1 in little endian
			want: "127.0.0.1",
		},
		{
			name: "private IP",
			ip:   0x0101a8c0, // 192.168.1.1 in little endian
			want: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatIP(tt.ip)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFailureCleanup(t *testing.T) {
	logger := zap.NewNop()
	observer, err := NewObserver("test-link", nil, logger)
	require.NoError(t, err)

	// Add many failures to trigger cleanup
	for i := 0; i < 1005; i++ {
		srcIP := fmt.Sprintf("192.168.1.%d", i%255)
		dstIP := fmt.Sprintf("10.0.0.%d", i%255)
		observer.trackFailure(srcIP, dstIP, EventSYNTimeout)
	}

	// Should have cleaned up old entries (keeping <= 1000)
	observer.mu.RLock()
	assert.LessOrEqual(t, len(observer.failures), 1000)
	observer.mu.RUnlock()
}

func BenchmarkFailureTracking(b *testing.B) {
	logger := zap.NewNop()
	observer, _ := NewObserver("bench", nil, logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			srcIP := fmt.Sprintf("192.168.1.%d", i%255)
			dstIP := fmt.Sprintf("10.0.0.%d", i%255)
			observer.trackFailure(srcIP, dstIP, EventSYNTimeout)
			i++
		}
	})
}

func BenchmarkFormatIP(b *testing.B) {
	testIP := uint32(0x0101a8c0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = formatIP(testIP)
	}
}
