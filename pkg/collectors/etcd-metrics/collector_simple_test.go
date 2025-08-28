package etcdmetrics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
)

// TestCollector_BasicFunctionality tests core collector functionality without etcd connection
func TestCollector_BasicFunctionality(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:    "test-collector",
		logger:  logger,
		config:  DefaultConfig(),
		events:  make(chan *domain.CollectorEvent, 100),
		healthy: true,
		tracer:  otel.Tracer("test"),
	}

	// Test Name
	assert.Equal(t, "test-collector", collector.Name())

	// Test IsHealthy
	assert.True(t, collector.IsHealthy())
	collector.healthy = false
	assert.False(t, collector.IsHealthy())
	collector.healthy = true

	// Test Events channel
	assert.NotNil(t, collector.Events())

	// Test generateEventID
	id1 := collector.generateEventID()
	id2 := collector.generateEventID()
	assert.Contains(t, id1, "test-collector-")
	assert.Contains(t, id2, "test-collector-")
	// IDs should be different due to timestamp
	if id1 == id2 {
		// If they're the same (rare), wait a nanosecond and try again
		time.Sleep(1 * time.Nanosecond)
		id2 = collector.generateEventID()
	}
	assert.NotEqual(t, id1, id2)
}

// TestCollector_EventHandling tests event sending and receiving
func TestCollector_EventHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		events: make(chan *domain.CollectorEvent, 10),
		ctx:    context.Background(),
	}

	// Send event
	event := &domain.CollectorEvent{
		EventID:   "test-event-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeETCD,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			ETCD: &domain.ETCDData{
				Operation: "test_op",
				Key:       "test_key",
				Value:     "test_value",
			},
		},
	}

	collector.sendEvent(event)

	// Receive event
	select {
	case received := <-collector.events:
		assert.Equal(t, event.EventID, received.EventID)
		assert.Equal(t, event.Type, received.Type)
		assert.Equal(t, event.Severity, received.Severity)
	case <-time.After(1 * time.Second):
		t.Error("Event not received")
	}
}

// TestCollector_ErrorHandling tests error handling
func TestCollector_ErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		events: make(chan *domain.CollectorEvent, 10),
		ctx:    context.Background(),
	}

	// Initially healthy
	collector.healthy = true

	// Handle error
	collector.handleError(assert.AnError, "test error context")

	// Should be unhealthy
	assert.False(t, collector.healthy)

	// Should have sent error event
	select {
	case event := <-collector.events:
		assert.Equal(t, domain.EventSeverityError, event.Severity)
		assert.Equal(t, domain.EventTypeETCD, event.Type)
		assert.Contains(t, event.EventData.ETCD.Value, "test error context")
	case <-time.After(1 * time.Second):
		t.Error("Error event not received")
	}
}

// TestCollector_StartWithoutClient tests collector start/stop without actual etcd client
func TestCollector_StartWithoutClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		config: Config{
			HealthCheckInterval: 1 * time.Hour, // Very long interval to avoid health checks
			Endpoints:           []string{"localhost:2379"},
		},
		events:  make(chan *domain.CollectorEvent, 10),
		healthy: true,
	}

	// Don't actually start the monitor loop
	collector.ctx, collector.cancel = context.WithCancel(context.Background())

	// Test that starting again fails
	err := collector.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollector_ConfigValidation tests configuration validation
func TestCollector_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Name:       "test",
				BufferSize: 100,
				Endpoints:  []string{"localhost:2379"},
			},
			wantErr: false,
		},
		{
			name: "no endpoints",
			config: Config{
				Name:       "test",
				BufferSize: 100,
				Endpoints:  []string{},
			},
			wantErr: true,
			errMsg:  "no etcd endpoints configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCollector(tt.config.Name, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				// Skip actual client creation in simple tests
				t.Skip("Skipping actual client creation in simple test")
			}
		})
	}
}

// TestCollector_MetricsRecording tests metric recording functionality
func TestCollector_MetricsRecording(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Initialize metrics (they can be nil in tests)
	collector := &Collector{
		name:            "test-collector",
		logger:          logger,
		events:          make(chan *domain.CollectorEvent, 10),
		ctx:             context.Background(),
		eventsProcessed: nil, // Metrics can be nil
		errorsTotal:     nil,
		responseTime:    nil,
	}

	// Send event - should handle nil metrics gracefully
	event := &domain.CollectorEvent{
		EventID: "test-1",
		Type:    domain.EventTypeETCD,
	}

	// Should not panic with nil metrics
	assert.NotPanics(t, func() {
		collector.sendEvent(event)
	})
}

// TestCollector_ConcurrentAccess tests thread safety
func TestCollector_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:    "test-collector",
		logger:  logger,
		events:  make(chan *domain.CollectorEvent, 100),
		ctx:     context.Background(),
		healthy: true,
	}

	// Concurrent writes to healthy status
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(val bool) {
			collector.mu.Lock()
			collector.healthy = val
			collector.mu.Unlock()
			done <- true
		}(i%2 == 0)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_ = collector.IsHealthy()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// No assertion needed - test passes if no race/panic
}

// TestCollector_CheckClusterStatusMock tests cluster status checking with nil client
func TestCollector_CheckClusterStatusMock(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		config: Config{
			Endpoints:       []string{"localhost:2379"},
			DbSizeThreshold: 1024,
		},
		events:       make(chan *domain.CollectorEvent, 10),
		ctx:          context.Background(),
		lastLeaderID: 12345,
		client:       nil, // Nil client should be handled gracefully
	}

	// Should handle nil client gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return early due to nil client
	collector.checkClusterStatus(ctx)

	// No events should be sent since client is nil
	select {
	case <-collector.events:
		t.Error("Should not send events with nil client")
	case <-time.After(100 * time.Millisecond):
		// Expected - no events
	}
}

// TestCollector_StopIdempotent tests that Stop can be called multiple times safely
func TestCollector_StopIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		events: make(chan *domain.CollectorEvent, 10),
	}

	// Stop without starting should be safe
	err := collector.Stop()
	assert.NoError(t, err)

	// Multiple stops should be safe
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollector_BufferManagement tests event buffer management
func TestCollector_BufferManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Small buffer to test overflow
	collector := &Collector{
		name:   "test-collector",
		logger: logger,
		events: make(chan *domain.CollectorEvent, 2),
		ctx:    context.Background(),
	}

	// Fill buffer
	event1 := &domain.CollectorEvent{EventID: "1", Type: domain.EventTypeETCD}
	event2 := &domain.CollectorEvent{EventID: "2", Type: domain.EventTypeETCD}
	event3 := &domain.CollectorEvent{EventID: "3", Type: domain.EventTypeETCD}

	collector.sendEvent(event1)
	collector.sendEvent(event2)
	collector.sendEvent(event3) // Should be dropped

	// Verify buffer contents
	received1 := <-collector.events
	assert.Equal(t, "1", received1.EventID)

	received2 := <-collector.events
	assert.Equal(t, "2", received2.EventID)

	// No more events
	select {
	case <-collector.events:
		t.Error("Should not have more events")
	default:
		// Expected
	}
}
