package network

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewCollector verifies collector creation with various configurations
func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name        string
		config      *NetworkCollectorConfig
		expectError bool
		validate    func(t *testing.T, c *Collector)
	}{
		{
			name:   "default_config",
			config: nil,
			validate: func(t *testing.T, c *Collector) {
				assert.NotNil(t, c)
				assert.NotNil(t, c.events)
				assert.Equal(t, "test", c.name)
			},
		},
		{
			name: "custom_config",
			config: &NetworkCollectorConfig{
				BufferSize:         2000,
				EnableIPv4:         true,
				EnableTCP:          true,
				EnableHTTP:         true,
				HTTPPorts:          []int{80, 8080},
				MaxEventsPerSecond: 10000,
				SamplingRate:       0.5,
			},
			validate: func(t *testing.T, c *Collector) {
				assert.NotNil(t, c)
				assert.NotNil(t, c.events)
				// Channel buffer size should match config
				assert.Equal(t, 2000, cap(c.events))
			},
		},
		{
			name: "minimal_config",
			config: &NetworkCollectorConfig{
				BufferSize: 10,
				EnableIPv4: true,
			},
			validate: func(t *testing.T, c *Collector) {
				assert.NotNil(t, c)
				assert.Equal(t, 10, cap(c.events))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test", tt.config, logger)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				tt.validate(t, collector)
			}
		})
	}
}

// TestCollectorLifecycle tests Start/Stop behavior
func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("normal_lifecycle", func(t *testing.T) {
		collector, err := NewCollector("test", nil, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start collector
		err = collector.Start(ctx)
		require.NoError(t, err)
		assert.True(t, collector.IsHealthy())

		// Verify context is set
		assert.NotNil(t, collector.ctx)

		// Stop collector
		err = collector.Stop()
		require.NoError(t, err)

		// Verify channel is closed
		select {
		case _, ok := <-collector.events:
			assert.False(t, ok, "channel should be closed")
		default:
			// Channel might be empty but closed
		}
	})

	t.Run("multiple_starts", func(t *testing.T) {
		collector, err := NewCollector("test", nil, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// First start should succeed
		err = collector.Start(ctx)
		require.NoError(t, err)

		// Second start should also succeed (idempotent)
		err = collector.Start(ctx)
		require.NoError(t, err)

		err = collector.Stop()
		require.NoError(t, err)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		collector, err := NewCollector("test", nil, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		err = collector.Start(ctx)
		require.NoError(t, err)

		// Cancel context
		cancel()

		// Collector should still be stoppable
		err = collector.Stop()
		require.NoError(t, err)
	})
}

// TestEventChannel tests event channel operations
func TestEventChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("event_publishing", func(t *testing.T) {
		config := &NetworkCollectorConfig{
			BufferSize: 100,
		}
		collector, err := NewCollector("test", config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)

		// Create test event
		testEvent := &domain.CollectorEvent{
			EventID:   "test-123",
			Timestamp: time.Now(),
			Type:      domain.EventTypeTCP,
			Source:    "test-collector",
			Severity:  domain.EventSeverityInfo,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					Protocol:   "tcp",
					SourceIP:   "10.0.0.1",
					DestIP:     "10.0.0.2",
					SourcePort: 12345,
					DestPort:   80,
				},
			},
			Metadata: domain.EventMetadata{
				PodName: "test-collector",
			},
		}

		// Send event
		select {
		case collector.events <- testEvent:
			// Success
		case <-time.After(time.Second):
			t.Fatal("Failed to send event")
		}

		// Receive event
		select {
		case received := <-collector.Events():
			assert.Equal(t, testEvent.EventID, received.EventID)
			assert.Equal(t, testEvent.Type, received.Type)
		case <-time.After(time.Second):
			t.Fatal("Failed to receive event")
		}

		err = collector.Stop()
		require.NoError(t, err)
	})

	t.Run("channel_buffer_size", func(t *testing.T) {
		config := &NetworkCollectorConfig{
			BufferSize: 10,
		}
		collector, err := NewCollector("test", config, logger)
		require.NoError(t, err)

		// Verify buffer size
		assert.Equal(t, 10, cap(collector.events))
	})
}

// TestCollectorHealth tests health check functionality
func TestCollectorHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector, err := NewCollector("test", nil, logger)
	require.NoError(t, err)

	// Should be healthy by default
	assert.True(t, collector.IsHealthy())

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should remain healthy after start
	assert.True(t, collector.IsHealthy())

	err = collector.Stop()
	require.NoError(t, err)

	// Should still be healthy after stop (graceful shutdown)
	assert.True(t, collector.IsHealthy())
}

// TestConcurrentOperations tests thread safety
func TestConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("concurrent_event_publishing", func(t *testing.T) {
		config := &NetworkCollectorConfig{
			BufferSize: 1000,
		}
		collector, err := NewCollector("test", config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)

		var wg sync.WaitGroup
		numGoroutines := 10
		eventsPerGoroutine := 100

		// Track sent events
		var sentCount int32

		// Start publishers
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < eventsPerGoroutine; j++ {
					event := &domain.CollectorEvent{
						EventID:   "test-event",
						Timestamp: time.Now(),
						Type:      domain.EventTypeTCP,
						Source:    "test",
						Severity:  domain.EventSeverityInfo,
					}
					select {
					case collector.events <- event:
						atomic.AddInt32(&sentCount, 1)
					case <-time.After(100 * time.Millisecond):
						// Timeout, skip
					}
				}
			}(i)
		}

		// Start consumer
		var receivedCount int32
		done := make(chan bool)
		go func() {
			for {
				select {
				case <-collector.Events():
					atomic.AddInt32(&receivedCount, 1)
				case <-done:
					return
				}
			}
		}()

		// Wait for publishers
		wg.Wait()

		// Give consumer time to process
		time.Sleep(100 * time.Millisecond)
		close(done)

		// Verify counts match (allowing for some buffer)
		sent := atomic.LoadInt32(&sentCount)
		received := atomic.LoadInt32(&receivedCount)
		assert.Greater(t, sent, int32(0), "Should have sent events")
		assert.LessOrEqual(t, received, sent, "Received should not exceed sent")

		err = collector.Stop()
		require.NoError(t, err)
	})

	t.Run("concurrent_health_checks", func(t *testing.T) {
		collector, err := NewCollector("test", nil, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)

		var wg sync.WaitGroup
		numGoroutines := 100

		// Concurrent health checks should not panic
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = collector.IsHealthy()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		wg.Wait()

		err = collector.Stop()
		require.NoError(t, err)
	})
}

// TestNewIntelligenceCollector tests intelligence collector creation
func TestNewIntelligenceCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name        string
		config      *IntelligenceCollectorConfig
		expectError bool
	}{
		{
			name:   "nil_config_uses_defaults",
			config: nil,
		},
		{
			name:   "default_config",
			config: DefaultIntelligenceConfig(),
		},
		{
			name: "custom_config",
			config: &IntelligenceCollectorConfig{
				NetworkCollectorConfig: &NetworkCollectorConfig{
					BufferSize: 2000,
					EnableIPv4: true,
					EnableTCP:  true,
				},
				EnableIntelligenceMode: true,
				SlowRequestThresholdMs: 500,
				ErrorStatusThreshold:   400,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewIntelligenceCollector("test-intel", tt.config, logger)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, collector)
				assert.NotNil(t, collector.Collector)
				assert.Equal(t, "test-intel", collector.name)
			}
		})
	}
}

// TestIntelligenceCollectorLifecycle tests intelligence collector lifecycle
func TestIntelligenceCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector, err := NewIntelligenceCollector("test-intel", nil, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should log warning on non-Linux platforms but still start
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
}

// TestIntelligenceStats tests statistics collection
func TestIntelligenceStats(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector, err := NewIntelligenceCollector("test-intel", nil, logger)
	require.NoError(t, err)

	stats := collector.GetIntelligenceStats()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.DependenciesFound)
	assert.Equal(t, int64(0), stats.ErrorPatternsFound)
	assert.Equal(t, float64(0), stats.FilteringEfficiency)
}

// TestServiceDependencies tests service dependency tracking
func TestServiceDependencies(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector, err := NewIntelligenceCollector("test-intel", nil, logger)
	require.NoError(t, err)

	deps := collector.GetServiceDependencies()
	assert.NotNil(t, deps)
	assert.Empty(t, deps)
}

// BenchmarkEventPublishing benchmarks event publishing performance
func BenchmarkEventPublishing(b *testing.B) {
	logger := zap.NewNop()
	config := &NetworkCollectorConfig{
		BufferSize: 10000,
	}

	collector, err := NewCollector("bench", config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		b.Fatal(err)
	}
	defer collector.Stop()

	// Create test event
	event := &domain.CollectorEvent{
		EventID:   "bench-event",
		Timestamp: time.Now(),
		Type:      domain.EventTypeTCP,
		Source:    "bench",
		Severity:  domain.EventSeverityInfo,
	}

	// Start consumer
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-collector.Events():
				// Consume
			case <-done:
				return
			}
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			select {
			case collector.events <- event:
				// Success
			default:
				// Channel full, skip
			}
		}
	})

	close(done)
}

// BenchmarkConcurrentOperations benchmarks concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	logger := zap.NewNop()
	config := &NetworkCollectorConfig{
		BufferSize: 10000,
	}

	collector, err := NewCollector("bench", config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		b.Fatal(err)
	}
	defer collector.Stop()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of operations
			_ = collector.IsHealthy()
			_ = collector.Name()

			event := &domain.CollectorEvent{
				EventID: "bench",
			}
			select {
			case collector.events <- event:
			default:
			}
		}
	})
}
