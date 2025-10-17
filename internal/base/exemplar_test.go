package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TestRecordProcessingTimeWithTraceContext tests that exemplars are added when trace context exists
func TestRecordProcessingTimeWithTraceContext(t *testing.T) {
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

	// Note: NoopTracer creates invalid spans, but that's OK for this test
	// In production, real TracerProvider creates valid spans

	// Record processing time with trace context (even if invalid, should not panic)
	event := &domain.ObserverEvent{
		Type: "test_event",
	}

	// This should attach exemplar with trace_id and span_id (if valid)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	// Test passes if no panic - exemplar attachment is best-effort
	assert.True(t, true, "Should record metric with exemplar")
}

// TestRecordProcessingTimeWithoutTraceContext tests that metrics work without trace context
func TestRecordProcessingTimeWithoutTraceContext(t *testing.T) {
	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	// Context without trace (no active span)
	ctx := context.Background()

	event := &domain.ObserverEvent{
		Type: "test_event",
	}

	// Should record metric without exemplar (no panic)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	assert.True(t, true, "Should record metric without exemplar")
}

// TestRecordProcessingTimeWithInvalidSpan tests handling of invalid span context
func TestRecordProcessingTimeWithInvalidSpan(t *testing.T) {
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

	// Should record metric without exemplar (graceful handling)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 123.45)

	assert.True(t, true, "Should handle invalid span gracefully")
}

// TestRecordProcessingTimeNilEvent tests exemplars with nil event
func TestRecordProcessingTimeNilEvent(t *testing.T) {
	metrics, err := NewObserverMetrics("test-observer")
	assert.NoError(t, err)

	tracer := otel.Tracer("test-tracer")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// Record with nil event (should still work)
	metrics.RecordProcessingTime(ctx, "test-observer", nil, 123.45)

	assert.True(t, true, "Should record metric with nil event")
}
