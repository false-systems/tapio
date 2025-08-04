package telemetry

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NATSCarrier implements the TextMapCarrier interface for NATS headers
type NATSCarrier struct {
	headers nats.Header
}

// NewNATSCarrier creates a new NATS carrier
func NewNATSCarrier(headers nats.Header) *NATSCarrier {
	if headers == nil {
		headers = make(nats.Header)
	}
	return &NATSCarrier{headers: headers}
}

// Get returns the value for a key
func (c *NATSCarrier) Get(key string) string {
	return c.headers.Get(key)
}

// Set sets the value for a key
func (c *NATSCarrier) Set(key, value string) {
	c.headers.Set(key, value)
}

// Keys returns all keys
func (c *NATSCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}

// InjectTraceContext injects trace context into NATS message headers
func InjectTraceContext(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}
	carrier := NewNATSCarrier(msg.Header)
	// Use the global propagator to inject context
	propagator := otel.GetTextMapPropagator()
	if propagator != nil {
		propagator.Inject(ctx, carrier)
	}
}

// ExtractTraceContext extracts trace context from NATS message headers
func ExtractTraceContext(ctx context.Context, msg *nats.Msg) context.Context {
	if msg.Header == nil {
		return ctx
	}
	carrier := NewNATSCarrier(msg.Header)
	propagator := otel.GetTextMapPropagator()
	if propagator != nil {
		return propagator.Extract(ctx, carrier)
	}
	return ctx
}

// EventMetadata carries trace context and other metadata in events
type EventMetadata struct {
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
	TraceFlags string            `json:"trace_flags,omitempty"`
	TraceState string            `json:"trace_state,omitempty"`
	Baggage    map[string]string `json:"baggage,omitempty"`
}

// InjectIntoEvent injects trace context into an event's metadata
func InjectIntoEvent(ctx context.Context) EventMetadata {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return EventMetadata{}
	}

	spanCtx := span.SpanContext()
	metadata := EventMetadata{
		TraceID:    spanCtx.TraceID().String(),
		SpanID:     spanCtx.SpanID().String(),
		TraceFlags: spanCtx.TraceFlags().String(),
		TraceState: spanCtx.TraceState().String(),
		Baggage:    make(map[string]string),
	}

	// Extract baggage
	// This is simplified - in production you'd properly extract baggage members
	// _ = propagation.Baggage{}.Extract(ctx, propagation.HeaderCarrier{})

	return metadata
}

// ExtractFromEvent extracts trace context from event metadata
func ExtractFromEvent(metadata EventMetadata) trace.SpanContext {
	if metadata.TraceID == "" || metadata.SpanID == "" {
		return trace.SpanContext{}
	}

	// Parse trace and span IDs
	traceID, err := trace.TraceIDFromHex(metadata.TraceID)
	if err != nil {
		return trace.SpanContext{}
	}

	spanID, err := trace.SpanIDFromHex(metadata.SpanID)
	if err != nil {
		return trace.SpanContext{}
	}

	// Parse trace flags
	var traceFlags trace.TraceFlags
	if metadata.TraceFlags != "" {
		// Simple parsing - in production would be more robust
		traceFlags = trace.FlagsSampled
	}

	config := trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: traceFlags,
		Remote:     true,
	}

	return trace.NewSpanContext(config)
}

// LinkSpans creates a span link between different traces
func LinkSpans(ctx context.Context, linkedTraceID, linkedSpanID string, attributes ...attribute.KeyValue) trace.Link {
	traceID, _ := trace.TraceIDFromHex(linkedTraceID)
	spanID, _ := trace.SpanIDFromHex(linkedSpanID)

	linkedCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
		Remote:  true,
	})

	return trace.Link{
		SpanContext: linkedCtx,
		Attributes:  attributes,
	}
}

// CorrelationBaggage helps carry correlation hints through the pipeline
type CorrelationBaggage struct {
	CorrelationID   string  `json:"correlation_id,omitempty"`
	CorrelationType string  `json:"correlation_type,omitempty"`
	RootCauseHint   string  `json:"root_cause_hint,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
}

// AddCorrelationBaggage adds correlation information to context baggage
func AddCorrelationBaggage(ctx context.Context, baggage CorrelationBaggage) context.Context {
	// In production, use proper OTEL baggage API
	// For now, we'll store it in context
	return context.WithValue(ctx, "correlation_baggage", baggage)
}

// GetCorrelationBaggage retrieves correlation information from context
func GetCorrelationBaggage(ctx context.Context) (CorrelationBaggage, bool) {
	baggage, ok := ctx.Value("correlation_baggage").(CorrelationBaggage)
	return baggage, ok
}

// EnrichSpanWithCorrelation adds correlation details to current span
func EnrichSpanWithCorrelation(ctx context.Context, correlationType string, relatedEvents []string, confidence float64) {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	span.SetAttributes(
		attribute.String("correlation.type", correlationType),
		attribute.Float64("correlation.confidence", confidence),
		attribute.Int("correlation.event_count", len(relatedEvents)),
	)

	// Add related events as span links if they have trace IDs
	for _, eventID := range relatedEvents {
		// In production, you'd look up the trace ID for each event
		span.AddEvent("related_event", trace.WithAttributes(
			attribute.String("event.id", eventID),
		))
	}
}

// MarshalTraceContext serializes trace context for storage
func MarshalTraceContext(ctx context.Context) ([]byte, error) {
	metadata := InjectIntoEvent(ctx)
	return json.Marshal(metadata)
}

// UnmarshalTraceContext deserializes trace context from storage
func UnmarshalTraceContext(data []byte) (trace.SpanContext, error) {
	var metadata EventMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return trace.SpanContext{}, err
	}
	return ExtractFromEvent(metadata), nil
}
