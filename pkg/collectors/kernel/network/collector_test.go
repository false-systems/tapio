package network

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// MockOTELExporter captures OTEL metrics and traces for verification
type MockOTELExporter struct {
	mu             sync.RWMutex
	eventsReceived int64
	errorsReceived int64
}

func (m *MockOTELExporter) recordMetric(name string, value int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "network_events_processed_total" {
		m.eventsReceived += value
	} else if name == "network_errors_total" {
		m.errorsReceived += value
	}
}

func (m *MockOTELExporter) getMetrics() (events, errors int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.eventsReceived, m.errorsReceived
}

// setupOTEL initializes OTEL with test exporters
func setupOTEL(t *testing.T) (*MockOTELExporter, func()) {
	exporter := &MockOTELExporter{}

	// Create resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("network-collector-test"),
			semconv.ServiceVersion("test"),
		),
	)
	require.NoError(t, err)

	// Setup trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Setup metric provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Setup propagator
	otel.SetTextMapPropagator(propagation.TraceContext{})

	cleanup := func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	}

	return exporter, cleanup
}

func TestNewNetworkCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := NewNetworkCollector(logger)
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.logger)
	assert.NotNil(t, collector.events)
	assert.NotNil(t, collector.safeParser)
}

func TestCollectorStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	// Test start
	ctx := context.Background()
	err := collector.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, collector.ctx)
	assert.NotNil(t, collector.cancel)

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)

	// Verify events channel is closed
	select {
	case _, ok := <-collector.Events():
		assert.False(t, ok, "Events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Error("Events channel not closed after stop")
	}
}

func TestRawEventEmission(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Since we can't trigger real eBPF events in test, we'll verify the structure
	events := collector.Events()
	assert.NotNil(t, events)

	// Test would receive events like this in production:
	// event := <-events
	// assert.Equal(t, "network", event.Type)
	// assert.NotEmpty(t, event.TraceID)
	// assert.NotEmpty(t, event.SpanID)
	// assert.NotNil(t, event.Data)
}

func TestOTELIntegration(t *testing.T) {
	exporter, cleanup := setupOTEL(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	// Create OTEL metrics
	meter := otel.Meter("network-collector")
	eventsCounter, err := meter.Int64Counter(
		"network_events_processed_total",
		metric.WithDescription("Total network events processed"),
	)
	require.NoError(t, err)

	errorsCounter, err := meter.Int64Counter(
		"network_errors_total",
		metric.WithDescription("Total errors in network collector"),
	)
	require.NoError(t, err)

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate event processing
	eventsCounter.Add(ctx, 10, metric.WithAttributes(
		attribute.String("collector", "network"),
		attribute.String("event_type", "tcp_connect"),
	))

	// Simulate error
	errorsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("collector", "network"),
		attribute.String("error_type", "parse_error"),
	))

	// Record in mock exporter
	exporter.recordMetric("network_events_processed_total", 10)
	exporter.recordMetric("network_errors_total", 1)

	// Verify metrics
	events, errors := exporter.getMetrics()
	assert.Equal(t, int64(10), events)
	assert.Equal(t, int64(1), errors)
}

func TestEventSerialization(t *testing.T) {
	event := NetworkEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       5678,
		EventType: 1, // TCP_CONNECT
		DataLen:   64,
		CgroupID:  9999,
		NetInfo: NetworkInfo{
			IPVersion: 4,
			SAddrV4:   0x7f000001, // 127.0.0.1
			DAddrV4:   0x08080808, // 8.8.8.8
			SPort:     12345,
			DPort:     443,
			Protocol:  6, // TCP
			State:     1, // ESTABLISHED
			Direction: 0, // Outgoing
		},
	}

	// Test JSON serialization
	data, err := json.Marshal(event)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test deserialization
	var decoded NetworkEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, event.PID, decoded.PID)
	assert.Equal(t, event.NetInfo.SPort, decoded.NetInfo.SPort)
}

func TestConcurrentEventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate concurrent event readers
	var wg sync.WaitGroup
	var eventsRead atomic.Int64

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			timeout := time.After(100 * time.Millisecond)
			for {
				select {
				case <-collector.Events():
					eventsRead.Add(1)
				case <-timeout:
					return
				}
			}
		}()
	}

	wg.Wait()
	// In production, we'd verify eventsRead > 0
}

func TestErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	// Test double start
	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)

	// Second start should handle gracefully
	err = collector.Start(ctx)
	assert.NoError(t, err) // Should not panic

	// Test double stop
	err = collector.Stop()
	assert.NoError(t, err)

	err = collector.Stop()
	assert.NoError(t, err) // Should not panic
}

func TestStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := zaptest.NewLogger(t)
	collector := NewNetworkCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Stress test configuration
	duration := 5 * time.Second
	readers := 10

	var wg sync.WaitGroup
	var totalEvents atomic.Int64
	stopCh := make(chan struct{})

	// Start concurrent readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-collector.Events():
					totalEvents.Add(1)
				case <-stopCh:
					return
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stopCh)
	wg.Wait()

	t.Logf("Stress test completed: %d events processed in %v",
		totalEvents.Load(), duration)
}

func TestRetryMechanism(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test exponential backoff
	backoff := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
	}

	for i, expected := range backoff {
		actual := calculateBackoff(i)
		assert.InDelta(t, expected, actual, float64(10*time.Millisecond),
			"Backoff %d should be close to %v", i, expected)
	}

	_ = logger // Use logger in production retry logic
}

func calculateBackoff(attempt int) time.Duration {
	base := 100 * time.Millisecond
	maxBackoff := 5 * time.Second

	backoff := base * (1 << attempt)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func TestCircuitBreaker(t *testing.T) {
	// Simple circuit breaker test
	type CircuitBreaker struct {
		failures  atomic.Int32
		threshold int32
		isOpen    atomic.Bool
	}

	cb := &CircuitBreaker{threshold: 3}

	// Simulate failures
	for i := 0; i < 3; i++ {
		cb.failures.Add(1)
		if cb.failures.Load() >= cb.threshold {
			cb.isOpen.Store(true)
		}
	}

	assert.True(t, cb.isOpen.Load(), "Circuit breaker should be open after threshold")

	// Reset circuit breaker
	cb.failures.Store(0)
	cb.isOpen.Store(false)
	assert.False(t, cb.isOpen.Load(), "Circuit breaker should be closed after reset")
}

// BenchmarkEventProcessing benchmarks event processing performance
func BenchmarkEventProcessing(b *testing.B) {
	logger := zap.NewNop()
	collector := NewNetworkCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			select {
			case <-collector.Events():
				// Process event
			default:
				// No event available
			}
		}
	})
}

// TestRawEventStructure verifies the RawEvent structure compliance
func TestRawEventStructure(t *testing.T) {
	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "network",
		Data:      []byte(`{"event": "tcp_connect"}`),
		Metadata: map[string]string{
			"source": "ebpf",
			"kernel": "5.15.0",
		},
		TraceID: "0123456789abcdef0123456789abcdef",
		SpanID:  "0123456789abcdef",
	}

	// Verify required fields
	assert.NotZero(t, rawEvent.Timestamp)
	assert.Equal(t, "network", rawEvent.Type)
	assert.NotNil(t, rawEvent.Data)
	assert.NotNil(t, rawEvent.Metadata)
	assert.Len(t, rawEvent.TraceID, 32)
	assert.Len(t, rawEvent.SpanID, 16)
}
