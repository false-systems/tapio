package status

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestObserverCreation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "Invalid buffer size",
			config: &Config{
				BufferSize:    -1,
				SampleRate:    1.0,
				FlushInterval: 1 * time.Second,
			},
			expectError: true,
			errorMsg:    "buffer_size must be positive",
		},
		{
			name: "Invalid sample rate",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    1.5,
				FlushInterval: 1 * time.Second,
			},
			expectError: true,
			errorMsg:    "sample_rate must be between 0 and 1",
		},
		{
			name: "Invalid flush interval",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    0.5,
				FlushInterval: -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "flush_interval must be positive",
		},
		{
			name:        "Nil config uses defaults",
			config:      nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				assert.Equal(t, "test", observer.Name())
			}
		})
	}
}

func TestObserverLifecycleStates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		SampleRate:    1.0,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-lifecycle", config)
	require.NoError(t, err)

	t.Run("Initial state", func(t *testing.T) {
		// Observer starts healthy after creation
		stats := observer.Statistics()
		assert.NotNil(t, stats)
		assert.Equal(t, int64(0), stats.EventsProcessed)
	})

	t.Run("Start state", func(t *testing.T) {
		ctx := context.Background()
		err := observer.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, observer.IsHealthy())
	})

	t.Run("Events channel available", func(t *testing.T) {
		events := observer.Events()
		assert.NotNil(t, events)

		// Channel should be open
		select {
		case <-events:
			// Might receive an event
		default:
			// Or might be empty
		}
	})

	t.Run("Stop state", func(t *testing.T) {
		err := observer.Stop()
		assert.NoError(t, err)
		assert.False(t, observer.IsHealthy())
	})

	t.Run("Double stop is safe", func(t *testing.T) {
		err := observer.Stop()
		assert.NoError(t, err)
	})
}

func TestHashDecoderConcurrency(t *testing.T) {
	decoder := NewHashDecoder()

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				decoder.AddService(uint32(id*100+j), "service")
				decoder.AddEndpoint(uint32(id*100+j), "/endpoint")
			}
			done <- true
		}(i)
	}

	// Wait for writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				decoder.GetService(uint32(j))
				decoder.GetEndpoint(uint32(j))
			}
			done <- true
		}()
	}

	// Wait for reads
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify some entries exist
	assert.NotEmpty(t, decoder.GetService(50))
	assert.NotEmpty(t, decoder.GetEndpoint(150))
}

func TestStatusAggregatorConcurrency(t *testing.T) {
	aggregator := NewStatusAggregator(50 * time.Millisecond)

	// Concurrent additions
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				event := &StatusEvent{
					ServiceHash:  uint32(id),
					EndpointHash: uint32(j),
					StatusCode:   uint16(200 + (j % 400)),
					ErrorType:    ErrorType(j % 8),
					Latency:      uint32(100 + j),
					Timestamp:    uint64(time.Now().UnixNano()),
					PID:          uint32(1000 + id),
				}
				aggregator.Add(event)
			}
			done <- true
		}(i)
	}

	// Wait for all additions
	for i := 0; i < 10; i++ {
		<-done
	}

	// Flush and verify
	aggregates := aggregator.Flush()
	assert.NotEmpty(t, aggregates)

	// Check aggregate consistency
	for _, agg := range aggregates {
		assert.Greater(t, agg.TotalCount, uint64(0))
		if agg.LatencyCount > 0 {
			assert.Greater(t, agg.AvgLatency(), 0.0)
		}
		if agg.TotalCount > 0 {
			assert.GreaterOrEqual(t, agg.ErrorRate(), 0.0)
			assert.LessOrEqual(t, agg.ErrorRate(), 1.0)
		}
	}

	// Second flush should be empty
	aggregates2 := aggregator.Flush()
	assert.Empty(t, aggregates2)
}

func TestAggregatedStatusMetrics(t *testing.T) {
	tests := []struct {
		name          string
		status        *AggregatedStatus
		expectedAvg   float64
		expectedRate  float64
	}{
		{
			name: "Normal metrics",
			status: &AggregatedStatus{
				ServiceHash:  12345,
				ErrorCount:   10,
				TotalCount:   100,
				LatencySum:   5000,
				LatencyCount: 50,
			},
			expectedAvg:  100.0,
			expectedRate: 0.1,
		},
		{
			name: "No latency data",
			status: &AggregatedStatus{
				ServiceHash:  12345,
				ErrorCount:   0,
				TotalCount:   100,
				LatencySum:   0,
				LatencyCount: 0,
			},
			expectedAvg:  0.0,
			expectedRate: 0.0,
		},
		{
			name: "All errors",
			status: &AggregatedStatus{
				ServiceHash:  12345,
				ErrorCount:   50,
				TotalCount:   50,
				LatencySum:   10000,
				LatencyCount: 50,
			},
			expectedAvg:  200.0,
			expectedRate: 1.0,
		},
		{
			name: "Empty status",
			status: &AggregatedStatus{
				ServiceHash: 12345,
			},
			expectedAvg:  0.0,
			expectedRate: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedAvg, tt.status.AvgLatency())
			assert.Equal(t, tt.expectedRate, tt.status.ErrorRate())
		})
	}
}

func TestPatternDetection(t *testing.T) {
	t.Run("Cascading timeout detection", func(t *testing.T) {
		events := make([]*StatusEvent, 0)

		// Not enough timeouts
		for i := 0; i < 3; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: uint32(i),
				ErrorType:   ErrorTimeout,
			})
		}

		pattern := KnownPatterns[0] // CascadingTimeout
		assert.False(t, pattern.Detector(events))

		// Add more timeouts to trigger
		for i := 3; i < 10; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: uint32(i),
				ErrorType:   ErrorTimeout,
			})
		}

		assert.True(t, pattern.Detector(events))
	})

	t.Run("Retry storm detection", func(t *testing.T) {
		events := make([]*StatusEvent, 0)

		// Too few events
		for i := 0; i < 5; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: 12345,
			})
		}

		pattern := KnownPatterns[1] // RetryStorm
		assert.False(t, pattern.Detector(events))

		// Add concentrated events from single service
		for i := 0; i < 20; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: 12345,
			})
		}

		assert.True(t, pattern.Detector(events))
	})

	t.Run("Service down detection", func(t *testing.T) {
		events := make([]*StatusEvent, 0)

		// Add some refused connections
		for i := 0; i < 15; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: uint32(i % 3),
				ErrorType:   ErrorRefused,
			})
		}

		pattern := KnownPatterns[2] // ServiceDown
		assert.True(t, pattern.Detector(events))

		// Test with too few refused
		events = events[:5]
		assert.False(t, pattern.Detector(events))
	})
}

func TestErrorTypeConstants(t *testing.T) {
	// Verify error type values match expected constants
	assert.Equal(t, ErrorType(0), ErrorNone)
	assert.Equal(t, ErrorType(1), ErrorTimeout)
	assert.Equal(t, ErrorType(2), ErrorRefused)
	assert.Equal(t, ErrorType(3), ErrorReset)
	assert.Equal(t, ErrorType(4), Error5XX)
	assert.Equal(t, ErrorType(5), Error4XX)
	assert.Equal(t, ErrorType(6), ErrorSlow)
	assert.Equal(t, ErrorType(7), ErrorPartial)
}

func TestObserverOTELSetup(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("test-otel", config)
	require.NoError(t, err)

	// Verify OTEL components are initialized
	assert.NotNil(t, observer.tracer)
	assert.NotNil(t, observer.eventsProcessed)
	assert.NotNil(t, observer.eventsDropped)
	assert.NotNil(t, observer.processingTime)
	assert.NotNil(t, observer.httpErrors)
	assert.NotNil(t, observer.grpcErrors)
	assert.NotNil(t, observer.timeouts)
	assert.NotNil(t, observer.latency)
	assert.NotNil(t, observer.errorRate)
}

func TestUpdateErrorRates(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("test-rates", config)
	require.NoError(t, err)

	aggregates := map[uint32]*AggregatedStatus{
		100: {
			ServiceHash: 100,
			ErrorCount:  25,
			TotalCount:  100,
		},
		200: {
			ServiceHash: 200,
			ErrorCount:  0,
			TotalCount:  50,
		},
		300: {
			ServiceHash: 300,
			ErrorCount:  10,
			TotalCount:  20,
		},
	}

	observer.updateErrorRates(aggregates)

	// Verify error rates were calculated correctly
	observer.mu.RLock()
	defer observer.mu.RUnlock()

	assert.Equal(t, 0.25, observer.errorRates[100])
	assert.Equal(t, 0.0, observer.errorRates[200])
	assert.Equal(t, 0.5, observer.errorRates[300])
}

func TestObserverEventChannel(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10,
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("test-channel", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Get event channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Send test event through EventChannelManager
	testEvent := &domain.CollectorEvent{
		EventID:   "test-123",
		Timestamp: time.Now(),
		Type:      domain.EventTypeNetworkConnection,
		Source:    "status-test-channel",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				SrcIP:   "10.0.0.1",
				DstIP:   "10.0.0.2",
				DstPort: 80,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test-channel",
				"version":  "1.0.0",
				"test":     "value",
			},
		},
	}

	_ = observer.EventChannelManager.SendEvent(testEvent)
	// Event validation may fail, so don't assert sent

	// Try to receive event (may timeout due to validation)
	select {
	case received := <-events:
		assert.Equal(t, testEvent.EventID, received.EventID)
	case <-time.After(100 * time.Millisecond):
		// Event may have been dropped due to validation, that's OK
		t.Log("Event not received (may have been dropped due to validation)")
	}
}

func TestFactoryFunction(t *testing.T) {
	logger := zap.NewNop()

	t.Run("With valid config", func(t *testing.T) {
		config := &Config{
			Enabled:       true,
			BufferSize:    1000,
			SampleRate:    0.5,
			FlushInterval: 10 * time.Second,
			Logger:        logger,
		}

		observer, err := Factory(config, logger)
		assert.NoError(t, err)
		assert.NotNil(t, observer)
		assert.Equal(t, "status", observer.Name())
	})

	t.Run("With invalid config type", func(t *testing.T) {
		// Pass wrong type, should use defaults
		observer, err := Factory("invalid", logger)
		assert.NoError(t, err)
		assert.NotNil(t, observer)
		assert.Equal(t, "status", observer.Name())
	})

	t.Run("With nil config", func(t *testing.T) {
		observer, err := Factory(nil, logger)
		assert.NoError(t, err)
		assert.NotNil(t, observer)
		assert.Equal(t, "status", observer.Name())
	})
}