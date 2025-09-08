package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// MockNATSConn implements a mock NATS connection for testing
type MockNATSConn struct {
	mu            sync.RWMutex
	published     []PublishedMessage
	closed        bool
	failPublish   bool
	blockPublish  bool
	publishDelay  time.Duration
	flushCallback func() error
}

type PublishedMessage struct {
	Subject string
	Data    []byte
	Time    time.Time
}

func (m *MockNATSConn) Publish(subject string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nats.ErrConnectionClosed
	}
	if m.failPublish {
		return errors.New("mock publish error")
	}
	if m.blockPublish {
		time.Sleep(m.publishDelay)
	}

	m.published = append(m.published, PublishedMessage{
		Subject: subject,
		Data:    data,
		Time:    time.Now(),
	})
	return nil
}

func (m *MockNATSConn) Flush() error {
	if m.flushCallback != nil {
		return m.flushCallback()
	}
	return nil
}

func (m *MockNATSConn) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func (m *MockNATSConn) GetPublished() []PublishedMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]PublishedMessage{}, m.published...)
}

func TestEnhancedNATSPublisher(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("Initialization", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			MaxReconnects:     3,
			JetStreamEnabled:  true,
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		assert.NoError(t, err)
		assert.NotNil(t, publisher)
		assert.Equal(t, config, publisher.config)
		assert.NotNil(t, publisher.eventQueue)
		assert.Equal(t, DefaultQueueSize, cap(publisher.eventQueue))
	})

	t.Run("EventQueuing", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Create test event
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		// Queue event
		err = publisher.PublishCollectorEvent(context.Background(), event)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(publisher.eventQueue))

		// Verify metrics
		assert.Equal(t, int64(1), publisher.metrics.EventsQueued)
		assert.Equal(t, int64(0), publisher.metrics.EventsPublished)
	})

	t.Run("BackpressureHandling", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		// Create publisher with small queue
		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Override queue size for testing
		publisher.eventQueue = make(chan *domain.CollectorEvent, 2)

		// Fill the queue
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		// Queue should accept first 2 events
		err = publisher.PublishCollectorEvent(context.Background(), event)
		assert.NoError(t, err)
		err = publisher.PublishCollectorEvent(context.Background(), event)
		assert.NoError(t, err)

		// Third event should be dropped due to backpressure
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		err = publisher.PublishCollectorEvent(ctx, event)
		assert.Error(t, err) // Should timeout or drop

		// Verify metrics
		assert.True(t, publisher.metrics.EventsDropped > 0)
	})

	t.Run("BatchProcessing", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Queue multiple events
		for i := 0; i < 5; i++ {
			event := &domain.CollectorEvent{
				Timestamp: time.Now(),
				Source:    "test",
				Type:      domain.EventTypeKernelNetwork,
				Severity:  domain.EventSeverityInfo,
			}
			err = publisher.PublishCollectorEvent(context.Background(), event)
			assert.NoError(t, err)
		}

		// Verify batch size
		assert.Equal(t, 5, len(publisher.eventQueue))
	})

	t.Run("ConnectionRecovery", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			MaxReconnects:     3,
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Simulate connection state changes
		publisher.updateConnectionState(ConnectionStateConnecting)
		assert.Equal(t, ConnectionStateConnecting, publisher.connState)

		publisher.updateConnectionState(ConnectionStateConnected)
		assert.Equal(t, ConnectionStateConnected, publisher.connState)
		assert.Equal(t, int64(1), publisher.metrics.ConnectionsEstablished)

		publisher.updateConnectionState(ConnectionStateReconnecting)
		assert.Equal(t, ConnectionStateReconnecting, publisher.connState)
		assert.Equal(t, int64(1), publisher.metrics.ConnectionsLost)

		publisher.updateConnectionState(ConnectionStateConnected)
		assert.Equal(t, ConnectionStateConnected, publisher.connState)
		assert.Equal(t, int64(1), publisher.metrics.ReconnectionSuccesses)
	})

	t.Run("MetricsTracking", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Queue events
		for i := 0; i < 3; i++ {
			event := &domain.CollectorEvent{
				Timestamp: time.Now(),
				Source:    "test",
				Type:      domain.EventTypeKernelNetwork,
				Severity:  domain.EventSeverityInfo,
			}
			publisher.PublishCollectorEvent(context.Background(), event)
		}

		// Get metrics
		metrics := publisher.GetMetrics()
		assert.Equal(t, int64(3), metrics.EventsQueued)
		assert.Equal(t, float64(3), metrics.CurrentQueueSize)
		assert.True(t, metrics.QueueUtilization > 0)
	})

	t.Run("GracefulShutdown", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Start publisher
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			publisher.Start(ctx)
		}()

		// Queue some events
		for i := 0; i < 3; i++ {
			event := &domain.CollectorEvent{
				Timestamp: time.Now(),
				Source:    "test",
				Type:      domain.EventTypeKernelNetwork,
				Severity:  domain.EventSeverityInfo,
			}
			publisher.PublishCollectorEvent(context.Background(), event)
		}

		// Wait a bit for processing
		time.Sleep(10 * time.Millisecond)

		// Stop publisher
		err = publisher.Stop()
		assert.NoError(t, err)

		// Cancel context to stop goroutine
		cancel()
		wg.Wait()
	})
}

func TestPublisherErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("InvalidConfig", func(t *testing.T) {
		config := &config.NATSConfig{
			URL: "", // Invalid URL
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		assert.Error(t, err)
		assert.Nil(t, publisher)
	})

	t.Run("PublishWithClosedConnection", func(t *testing.T) {
		config := &config.NATSConfig{
			URL:               "nats://localhost:4222",
			RawEventsSubjects: []string{"events.raw"},
		}

		publisher, err := NewEnhancedNATSPublisher(logger, config)
		require.NoError(t, err)

		// Simulate closed connection
		publisher.connState = ConnectionStateClosed

		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		// Should still queue event (will be retried when reconnected)
		err = publisher.PublishCollectorEvent(context.Background(), event)
		assert.NoError(t, err)
	})
}

func BenchmarkNATSPublisher(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := &config.NATSConfig{
		URL:               "nats://localhost:4222",
		RawEventsSubjects: []string{"events.raw"},
	}

	publisher, err := NewEnhancedNATSPublisher(logger, config)
	require.NoError(b, err)

	event := &domain.CollectorEvent{
		Timestamp: time.Now(),
		Source:    "benchmark",
		Type:      domain.EventTypeKernelNetwork,
		Severity:  domain.EventSeverityInfo,
	}

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = publisher.PublishCollectorEvent(ctx, event)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
}
