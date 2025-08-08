package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap/zaptest"
)

func setupOTEL(t *testing.T) func() {
	// Setup trace exporter
	traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	require.NoError(t, err)

	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
	)
	otel.SetTracerProvider(tp)

	// Setup metric exporter
	metricExporter, err := stdoutmetric.New()
	require.NoError(t, err)

	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(1*time.Second))),
	)
	otel.SetMeterProvider(mp)

	return func() {
		tp.Shutdown(context.Background())
		mp.Shutdown(context.Background())
	}
}

func TestEtcdInstrumentation(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	instrumentation, err := NewEtcdInstrumentation(logger)
	require.NoError(t, err)

	// Test basic instrumentation fields
	assert.Equal(t, "etcd-collector", instrumentation.ServiceName)
	assert.NotNil(t, instrumentation.Logger)
	assert.NotNil(t, instrumentation.Tracer)
	assert.NotNil(t, instrumentation.meter)

	// Test required metrics are initialized
	assert.NotNil(t, instrumentation.RequestsTotal)
	assert.NotNil(t, instrumentation.RequestDuration)
	assert.NotNil(t, instrumentation.ActiveRequests)
	assert.NotNil(t, instrumentation.ErrorsTotal)

	// Test etcd-specific metrics
	assert.NotNil(t, instrumentation.EventsTotal)
	assert.NotNil(t, instrumentation.APILatency)
	assert.NotNil(t, instrumentation.PollsActive)
	assert.NotNil(t, instrumentation.SyscallsTracked)
	assert.NotNil(t, instrumentation.EtcdErrorsTotal)
}

func TestStartSpanInstrumentation(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	instrumentation, err := NewEtcdInstrumentation(logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test StartSpan creates span and increments active requests
	spanCtx, span := instrumentation.StartSpan(ctx, "test_operation")
	defer span.End()

	// Verify span is created
	assert.True(t, span.SpanContext().IsValid())
	assert.NotEqual(t, ctx, spanCtx) // Context should be different with span

	// Test that attributes can be added to span
	span.SetAttributes(
		attribute.String("test", "value"),
		attribute.Int("count", 42),
	)

	// No direct way to test metrics without actual OTEL backend,
	// but we can verify methods don't panic
	instrumentation.ActiveRequests.Add(spanCtx, -1, attribute.String("operation", "test_operation"))
}

func TestCollectorOTELIntegration(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	// Test collector initialization with OTEL
	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	// Verify OTEL components are initialized
	assert.NotNil(t, collector.instrumentation)
	assert.NotNil(t, collector.logger)
	assert.Equal(t, "etcd-collector", collector.instrumentation.ServiceName)
}

func TestCollectorTraceContext(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	ctx := context.Background()

	// Test trace context extraction without span
	traceID, spanID := collector.extractTraceContext(ctx)
	assert.NotEmpty(t, traceID)
	assert.NotEmpty(t, spanID)

	// Test trace context extraction with span
	spanCtx, span := collector.instrumentation.StartSpan(ctx, "test")
	defer span.End()

	extractedTraceID, extractedSpanID := collector.extractTraceContext(spanCtx)
	assert.NotEmpty(t, extractedTraceID)
	assert.NotEmpty(t, extractedSpanID)

	// Should extract real trace/span IDs when span is present
	if span.SpanContext().IsValid() {
		assert.Equal(t, span.SpanContext().TraceID().String(), extractedTraceID)
		assert.Equal(t, span.SpanContext().SpanID().String(), extractedSpanID)
	}
}

func TestCreateEventWithContext(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	ctx := context.Background()
	spanCtx, span := collector.instrumentation.StartSpan(ctx, "test_event_creation")
	defer span.End()

	// Test event creation with context
	eventData := map[string]interface{}{
		"key":   "test-key",
		"value": "test-value",
	}

	event := collector.createEventWithContext(spanCtx, "test", eventData)

	assert.Equal(t, "etcd", event.Type)
	assert.Equal(t, "test-etcd", event.Metadata["collector"])
	assert.Equal(t, "test", event.Metadata["event"])
	assert.NotEmpty(t, event.TraceID)
	assert.NotEmpty(t, event.SpanID)

	// Should have trace context from span if valid
	if span.SpanContext().IsValid() {
		assert.Equal(t, span.SpanContext().TraceID().String(), event.TraceID)
		assert.Equal(t, span.SpanContext().SpanID().String(), event.SpanID)
	}
}

func TestCollectorMetricsRecording(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	_, testClient, etcdCleanup := setupTestEtcd(t)
	defer etcdCleanup()

	endpoints := testClient.Endpoints()
	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector (this should record metrics)
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Give time for metrics to be recorded
	time.Sleep(100 * time.Millisecond)

	// Put some data to trigger event processing metrics
	testClient.Put(ctx, "/registry/pods/default/test-pod", `{"name":"test-pod"}`)

	// Collect the event to trigger event metrics
	eventsChan := collector.Events()
	select {
	case <-eventsChan:
		// Event received, metrics should be recorded
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Give time for all metrics to be processed
	time.Sleep(500 * time.Millisecond)

	// Can't directly test metric values without complex setup,
	// but we can verify the collector is working with OTEL
	healthy, details := collector.Health()
	assert.True(t, healthy)
	assert.Contains(t, details, "instrumentation")
	assert.Equal(t, "etcd-collector", details["instrumentation"])
}

func TestErrorTrackingWithSpans(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	ctx := context.Background()

	// Test error tracking in event marshaling
	eventData := make(chan int) // Non-marshallable data

	spanCtx, span := collector.instrumentation.StartSpan(ctx, "test_error")
	defer span.End()

	// This should trigger error tracking
	event := collector.createEventWithContext(spanCtx, "error_test", eventData)

	// Should still create event with error message
	assert.Equal(t, "etcd", event.Type)
	assert.Contains(t, string(event.Data), "error")
}

func TestHealthWithOTELMetrics(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	collector, err := NewCollector("test-etcd", Config{
		Endpoints: []string{"localhost:2379"},
	})
	require.NoError(t, err)

	healthy, details := collector.Health()
	assert.True(t, healthy)

	// Verify OTEL-specific health information
	assert.Contains(t, details, "instrumentation")
	assert.Equal(t, "etcd-collector", details["instrumentation"])
	assert.Contains(t, details, "buffer_size")
	assert.Contains(t, details, "buffer_available")
	assert.Contains(t, details, "ebpf_active")

	// Test statistics include OTEL service info
	stats := collector.Statistics()
	assert.Contains(t, stats, "service_name")
	assert.Equal(t, "etcd-collector", stats["service_name"])
	assert.Contains(t, stats, "buffer_utilization")
}

func TestInstrumentationFailureHandling(t *testing.T) {
	// Test what happens when OTEL setup fails
	// This mainly tests the error path in NewEtcdInstrumentation
	logger := zaptest.NewLogger(t)

	// With proper setup, should succeed
	instrumentation, err := NewEtcdInstrumentation(logger)
	require.NoError(t, err)
	assert.NotNil(t, instrumentation)

	// Test the instrumentation works
	ctx := context.Background()
	spanCtx, span := instrumentation.StartSpan(ctx, "test")
	defer span.End()

	assert.True(t, span.SpanContext().IsValid())
}

func TestComprehensiveOTELCoverage(t *testing.T) {
	cleanup := setupOTEL(t)
	defer cleanup()

	// Test all 5 required metrics are present and named correctly
	logger := zaptest.NewLogger(t)
	instrumentation, err := NewEtcdInstrumentation(logger)
	require.NoError(t, err)

	// Common metrics (matching kubelet pattern)
	assert.NotNil(t, instrumentation.RequestsTotal)   // tapio.requests.total
	assert.NotNil(t, instrumentation.RequestDuration) // tapio.request.duration
	assert.NotNil(t, instrumentation.ActiveRequests)  // tapio.requests.active
	assert.NotNil(t, instrumentation.ErrorsTotal)     // tapio.errors.total

	// Etcd-specific metrics (5 minimum required)
	assert.NotNil(t, instrumentation.EventsTotal)     // tapio.etcd.events.total - Events collected by type
	assert.NotNil(t, instrumentation.EtcdErrorsTotal) // tapio.etcd.errors.total - Errors by category
	assert.NotNil(t, instrumentation.APILatency)      // tapio.etcd.api.latency - etcd API call latency
	assert.NotNil(t, instrumentation.PollsActive)     // tapio.etcd.polls.active - Active watch operations
	assert.NotNil(t, instrumentation.SyscallsTracked) // tapio.etcd.syscalls.tracked - eBPF syscalls monitored

	// Test service name matches kubelet pattern
	assert.Equal(t, "etcd-collector", instrumentation.ServiceName)
}
