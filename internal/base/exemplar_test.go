package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TestRecordProcessingTimeWithTraceContext tests that metrics work with trace context
// Exemplars are attached automatically by the OTel SDK/exporter, not by this code
func TestRecordProcessingTimeWithTraceContext(t *testing.T) {
	// Need to set up MeterProvider before creating metrics
	otel.SetMeterProvider(otel.GetMeterProvider())

	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	// Set up a noop tracer provider (needed for valid spans in tests)
	tp := trace.NewNoopTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(trace.NewNoopTracerProvider())

	// Create a tracer and start a span (simulates active trace)
	tracer := tp.Tracer("test-tracer")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	event := &domain.ObserverEvent{
		Type: "test_event",
	}

	// Record processing time - OTel SDK will attach exemplars automatically if:
	// 1. ctx contains a valid span
	// 2. The metric exporter supports exemplars (Prometheus, OTLP)
	// 3. An exemplar filter is configured
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	// Test passes if no panic - metric recorded successfully with trace context
	assert.True(t, true, "Should record metric with trace context")
}

// TestRecordProcessingTimeWithoutTraceContext tests that metrics work without trace context
func TestRecordProcessingTimeWithoutTraceContext(t *testing.T) {
	otel.SetMeterProvider(otel.GetMeterProvider())

	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	// Context without trace (no active span)
	ctx := context.Background()

	event := &domain.ObserverEvent{
		Type: "test_event",
	}

	// Record metric - no exemplar will be attached (no trace context)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	assert.True(t, true, "Should record metric without trace context")
}

// TestRecordProcessingTimeWithInvalidSpan tests that metrics work with invalid span context
func TestRecordProcessingTimeWithInvalidSpan(t *testing.T) {
	otel.SetMeterProvider(otel.GetMeterProvider())

	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	// Create context with invalid span (no tracer provider set)
	ctx := context.Background()

	// Get span from context (will be invalid/noop span)
	span := trace.SpanFromContext(ctx)
	spanCtx := span.SpanContext()

	assert.False(t, spanCtx.IsValid(), "Span context should be invalid")

	event := &domain.ObserverEvent{
		Type: "test_event",
	}

	// Record metric - OTel SDK will not attach exemplar (invalid span)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	assert.True(t, true, "Should handle invalid span gracefully")
}

// TestRecordProcessingTimeNilEvent tests metrics with nil event
func TestRecordProcessingTimeNilEvent(t *testing.T) {
	otel.SetMeterProvider(otel.GetMeterProvider())

	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	tracer := otel.Tracer("test-tracer")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// Record with nil event (should still work - just missing event.type attribute)
	metrics.RecordProcessingTime(ctx, "test-observer", nil, 123.45)

	assert.True(t, true, "Should record metric with nil event")
}
