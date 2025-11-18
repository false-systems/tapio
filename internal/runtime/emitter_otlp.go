package runtime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OTLPEmitter exports observer events to OpenTelemetry Collector.
// This is the PRIMARY emitter for production deployments (OSS).
type OTLPEmitter struct {
	exporter *otlptrace.Exporter
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
}

// NewOTLPEmitter creates an OTLP emitter that exports to the given endpoint.
// endpoint: OTLP gRPC endpoint (e.g., "localhost:4317")
// insecure: If true, uses insecure connection (for dev/test)
func NewOTLPEmitter(endpoint string, insecure bool) (*OTLPEmitter, error) {
	// Create OTLP exporter
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create trace provider with batch span processor
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "tapio-observer"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	tracer := provider.Tracer("tapio-runtime")

	return &OTLPEmitter{
		exporter: exporter,
		tracer:   tracer,
		provider: provider,
	}, nil
}

// Emit sends an observer event to OTLP collector as a trace span.
func (e *OTLPEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	// Check context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Convert domain event to OTEL span
	var spanContext trace.SpanContext
	if event.TraceID != "" && event.SpanID != "" {
		// Parse existing trace context
		traceID, err := trace.TraceIDFromHex(event.TraceID)
		if err != nil {
			return fmt.Errorf("invalid TraceID: %w", err)
		}

		spanID, err := trace.SpanIDFromHex(event.SpanID)
		if err != nil {
			return fmt.Errorf("invalid SpanID: %w", err)
		}

		spanContext = trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.TraceFlags(event.TraceFlags),
		})

		ctx = trace.ContextWithSpanContext(ctx, spanContext)
	}

	// Create span for this event
	spanName := event.Type
	if event.Subtype != "" {
		spanName = fmt.Sprintf("%s.%s", event.Type, event.Subtype)
	}
	ctx, span := e.tracer.Start(ctx, spanName)
	defer span.End()

	// Add event attributes
	span.SetAttributes(
		attribute.String("event.id", event.ID),
		attribute.String("event.type", event.Type),
		attribute.String("event.subtype", event.Subtype),
		attribute.String("event.source", event.Source),
		attribute.Int64("event.timestamp", event.Timestamp.Unix()),
	)

	return nil
}

// Name returns the emitter name for logging and metrics.
func (e *OTLPEmitter) Name() string {
	return "otlp"
}

// IsCritical returns true - OTLP is the primary emitter and is critical.
func (e *OTLPEmitter) IsCritical() bool {
	return true
}

// Close shuts down the OTLP exporter and flushes any pending spans.
func (e *OTLPEmitter) Close() error {
	if e.provider != nil {
		ctx := context.Background()
		if err := e.provider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown trace provider: %w", err)
		}
	}
	return nil
}
