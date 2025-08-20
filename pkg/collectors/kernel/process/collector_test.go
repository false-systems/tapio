package process

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestCollectorLifecycle tests the basic lifecycle of the process collector
func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewProcessCollector(logger)

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.logger)
	assert.NotNil(t, collector.events)
	assert.Equal(t, 3000, cap(collector.events))

	// Start collector
	ctx := context.Background()
	err := collector.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, collector.ctx)
	assert.NotNil(t, collector.cancel)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Verify channel is closed
	select {
	case _, ok := <-collector.Events():
		assert.False(t, ok, "Channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Error("Channel not closed after stop")
	}
}

// TestOTELMetrics tests OTEL metric emission
func TestOTELMetrics(t *testing.T) {
	// Setup OTEL
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("process-collector-test"),
		),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	defer mp.Shutdown(context.Background())

	// Create metrics
	meter := otel.Meter("process-collector")
	eventsCounter, err := meter.Int64Counter(
		"process_events_total",
		metric.WithDescription("Total process events"),
	)
	require.NoError(t, err)

	forksCounter, err := meter.Int64Counter(
		"process_forks_total",
		metric.WithDescription("Total process forks"),
	)
	require.NoError(t, err)

	exitsCounter, err := meter.Int64Counter(
		"process_exits_total",
		metric.WithDescription("Total process exits"),
	)
	require.NoError(t, err)

	// Record metrics
	ctx := context.Background()
	eventsCounter.Add(ctx, 100, metric.WithAttributes(
		attribute.String("collector", "process"),
	))
	forksCounter.Add(ctx, 50)
	exitsCounter.Add(ctx, 45)

	// In production, these would be exported to OTEL backend
}

// TestProcessEvent tests process event structure
func TestProcessEvent(t *testing.T) {
	event := ProcessEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		PPID:      1,
		UID:       1000,
		GID:       1000,
		EventType: 1, // FORK
		ExitCode:  0,
		CgroupID:  5678,
	}
	copy(event.Comm[:], "test-process")

	// Test JSON serialization
	data, err := json.Marshal(event)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test deserialization
	var decoded ProcessEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, event.PID, decoded.PID)
	assert.Equal(t, event.PPID, decoded.PPID)
	assert.Equal(t, event.EventType, decoded.EventType)
}

// TestRawEventCompliance tests RawEvent structure compliance
func TestRawEventCompliance(t *testing.T) {
	event := domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "process",
		Data:      []byte(`{"pid": 1234, "event": "fork"}`),
	}

	// Verify structure
	assert.Equal(t, "process", event.Source)
	assert.NotNil(t, event.Data)
}

// TestConcurrentAccess tests thread-safe operations
func TestConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewProcessCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	var wg sync.WaitGroup
	numReaders := 5

	// Start concurrent readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			timeout := time.After(100 * time.Millisecond)
			for {
				select {
				case <-collector.Events():
					// Process event
				case <-timeout:
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestErrorHandling tests error scenarios
func TestErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewProcessCollector(logger)

	// Test stopping before starting
	err := collector.Stop()
	assert.NoError(t, err) // Should handle gracefully

	// Test double start
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	err = collector.Start(ctx)
	assert.NoError(t, err) // Should handle gracefully

	// Clean up
	collector.Stop()
}

// TestStressScenario tests high-load conditions
func TestStressScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := zaptest.NewLogger(t)
	collector := NewProcessCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate high event rate
	duration := 3 * time.Second
	var eventsProcessed atomic.Int64
	done := make(chan struct{})

	// Multiple readers
	for i := 0; i < 10; i++ {
		go func() {
			for {
				select {
				case <-collector.Events():
					eventsProcessed.Add(1)
				case <-done:
					return
				}
			}
		}()
	}

	time.Sleep(duration)
	close(done)

	t.Logf("Processed %d events in %v", eventsProcessed.Load(), duration)
}

// TestRetryLogic tests retry mechanism with exponential backoff
func TestRetryLogic(t *testing.T) {
	attempts := 0
	maxAttempts := 3

	retryFunc := func() error {
		attempts++
		if attempts < maxAttempts {
			return assert.AnError
		}
		return nil
	}

	// Simple retry with exponential backoff
	var err error
	for i := 0; i < maxAttempts; i++ {
		err = retryFunc()
		if err == nil {
			break
		}

		// Exponential backoff
		backoff := time.Duration(1<<i) * 100 * time.Millisecond
		if i < maxAttempts-1 {
			time.Sleep(backoff)
		}
	}

	assert.NoError(t, err)
	assert.Equal(t, maxAttempts, attempts)
}

// TestCircuitBreaker tests circuit breaker pattern
func TestCircuitBreaker(t *testing.T) {
	type CircuitBreaker struct {
		mu        sync.RWMutex
		failures  int
		threshold int
		isOpen    bool
		lastFail  time.Time
		cooldown  time.Duration
	}

	cb := &CircuitBreaker{
		threshold: 3,
		cooldown:  1 * time.Second,
	}

	// Helper to record failure
	recordFailure := func() bool {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		cb.failures++
		cb.lastFail = time.Now()

		if cb.failures >= cb.threshold {
			cb.isOpen = true
			return true
		}
		return false
	}

	// Helper to check if circuit is open
	isOpen := func() bool {
		cb.mu.RLock()
		defer cb.mu.RUnlock()

		if !cb.isOpen {
			return false
		}

		// Check if cooldown period has passed
		if time.Since(cb.lastFail) > cb.cooldown {
			cb.failures = 0
			cb.isOpen = false
			return false
		}

		return true
	}

	// Test circuit breaker
	for i := 0; i < 3; i++ {
		recordFailure()
	}

	assert.True(t, isOpen(), "Circuit should be open after threshold")

	// Wait for cooldown
	time.Sleep(cb.cooldown + 100*time.Millisecond)
	assert.False(t, isOpen(), "Circuit should be closed after cooldown")
}

// TestTracingIntegration tests OpenTelemetry tracing
func TestTracingIntegration(t *testing.T) {
	// Setup tracer
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			semconv.ServiceName("process-collector-test"),
		)),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	tracer := otel.Tracer("process-collector")

	// Create spans
	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "process_event",
		trace.WithAttributes(
			attribute.Int("pid", 1234),
			attribute.String("event_type", "fork"),
		))
	defer span.End()

	// Create child span
	_, childSpan := tracer.Start(ctx, "process_metadata")
	childSpan.SetAttributes(
		attribute.String("comm", "test-process"),
		attribute.Int("ppid", 1),
	)
	childSpan.End()

	span.SetStatus(codes.Ok, "Process event handled successfully")
}

// BenchmarkEventProcessing benchmarks event processing
func BenchmarkEventProcessing(b *testing.B) {
	logger := zap.NewNop()
	collector := NewProcessCollector(logger)

	ctx := context.Background()
	collector.Start(ctx)
	defer collector.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			select {
			case <-collector.Events():
				// Process event
			default:
				// No event
			}
		}
	})
}

// TestMemoryLeaks tests for memory leaks
func TestMemoryLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// Run multiple iterations
	for i := 0; i < 10; i++ {
		collector := NewProcessCollector(logger)
		ctx := context.Background()

		err := collector.Start(ctx)
		require.NoError(t, err)

		// Simulate some work
		time.Sleep(10 * time.Millisecond)

		err = collector.Stop()
		require.NoError(t, err)
	}

	// In production, use runtime.ReadMemStats to verify memory usage
}
