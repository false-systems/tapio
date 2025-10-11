package base

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/trace"
)

// ExtractTraceContext extracts OTEL trace context from current span and adds to event
// This allows correlation of events across observer boundaries
func ExtractTraceContext(ctx context.Context, event *domain.ObserverEvent) {
	if event == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	sc := span.SpanContext()
	event.TraceID = sc.TraceID().String()
	event.SpanID = sc.SpanID().String()
	event.TraceFlags = byte(sc.TraceFlags())
}

// InjectTraceContext restores OTEL trace context from event into context
// This allows downstream observers to correlate with upstream trace
func InjectTraceContext(event *domain.ObserverEvent) context.Context {
	ctx := context.Background()

	if event == nil || event.TraceID == "" {
		return ctx
	}

	// Parse trace ID
	traceID, err := trace.TraceIDFromHex(event.TraceID)
	if err != nil {
		return ctx
	}

	// Parse span ID
	spanID, err := trace.SpanIDFromHex(event.SpanID)
	if err != nil {
		return ctx
	}

	// Create span context
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.TraceFlags(event.TraceFlags),
		Remote:     true, // Indicates this came from another process
	})

	// Inject into context
	return trace.ContextWithSpanContext(ctx, sc)
}

// HasTraceContext checks if event has valid trace context
func HasTraceContext(event *domain.ObserverEvent) bool {
	if event == nil {
		return false
	}

	return event.TraceID != "" && event.SpanID != ""
}
