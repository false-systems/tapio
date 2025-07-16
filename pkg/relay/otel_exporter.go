package relay

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/api"
	"github.com/yairfalse/tapio/pkg/output"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// OTELExporter exports relay events as OTEL traces
type OTELExporter struct {
	tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	endpoint       string
}

// NewOTELExporter creates a new OTEL exporter
func NewOTELExporter(endpoint string) (*OTELExporter, error) {
	// Create resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("tapio-relay"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("tapio.component", "relay"),
			attribute.String("tapio.intelligence", "enabled"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter
	exporter, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL exporter: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)

	return &OTELExporter{
		tracer:         tp.Tracer("tapio-relay"),
		tracerProvider: tp,
		endpoint:       endpoint,
	}, nil
}

// ConvertEventsToSpans converts relay events to OTEL spans
func (e *OTELExporter) ConvertEventsToSpans(ctx context.Context, events []*api.Event) []trace.Span {
	// Group events by correlation
	groupedEvents := e.groupEventsByCorrelation(events)
	
	spans := make([]trace.Span, 0, len(groupedEvents))
	
	for groupID, group := range groupedEvents {
		// Create parent span for correlation group
		ctx, span := e.tracer.Start(ctx, fmt.Sprintf("relay.correlation.%s", groupID),
			trace.WithAttributes(
				attribute.String("correlation.id", groupID),
				attribute.Int("correlation.event_count", len(group)),
				attribute.String("correlation.type", e.detectPatternType(group)),
			),
		)
		
		// Add child spans for individual events
		for _, event := range group {
			e.createEventSpan(ctx, event)
		}
		
		span.End()
		spans = append(spans, span)
	}
	
	return spans
}

// Export sends spans to OTEL collector
func (e *OTELExporter) Export(ctx context.Context, spans []trace.Span) error {
	// Force flush to ensure spans are sent
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	if err := e.tracerProvider.ForceFlush(ctx); err != nil {
		return fmt.Errorf("failed to flush traces: %w", err)
	}
	
	return nil
}

// Close shuts down the exporter
func (e *OTELExporter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := e.tracerProvider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}
	
	return nil
}

// createEventSpan creates a span for a single event
func (e *OTELExporter) createEventSpan(ctx context.Context, event *api.Event) {
	_, span := e.tracer.Start(ctx, fmt.Sprintf("event.%s", event.Type),
		trace.WithAttributes(
			// Core event attributes
			attribute.String("event.id", event.Id),
			attribute.String("event.type", event.Type),
			attribute.String("event.source", event.Source),
			attribute.Int64("event.timestamp", event.Timestamp),
			
			// Kubernetes context
			attribute.String("k8s.namespace", event.Namespace),
			attribute.String("k8s.pod", event.PodName),
			attribute.String("k8s.container", event.ContainerName),
			attribute.String("k8s.node", event.NodeName),
			
			// Event details
			attribute.String("event.message", event.Message),
			attribute.String("event.level", event.Level),
			
			// Tapio intelligence
			attribute.Bool("tapio.relay_processed", true),
			attribute.String("tapio.pattern", e.detectEventPattern(event)),
		),
		trace.WithTimestamp(time.Unix(0, event.Timestamp)),
	)
	defer span.End()
	
	// Set span status based on event level
	switch event.Level {
	case "ERROR", "CRITICAL":
		span.SetStatus(codes.Error, event.Message)
	case "WARNING":
		span.SetStatus(codes.Error, event.Message)
	default:
		span.SetStatus(codes.Ok, "")
	}
	
	// Add metadata as events
	if event.Metadata != nil {
		for k, v := range event.Metadata {
			span.AddEvent("metadata",
				trace.WithAttributes(
					attribute.String("key", k),
					attribute.String("value", v),
				),
			)
		}
	}
}

// groupEventsByCorrelation groups events that are correlated
func (e *OTELExporter) groupEventsByCorrelation(events []*api.Event) map[string][]*api.Event {
	groups := make(map[string][]*api.Event)
	
	for _, event := range events {
		// Simple correlation by pod/namespace for now
		// Real implementation would use correlation engine
		groupID := fmt.Sprintf("%s/%s", event.Namespace, event.PodName)
		if groupID == "/" {
			groupID = "uncorrelated"
		}
		groups[groupID] = append(groups[groupID], event)
	}
	
	return groups
}

// detectPatternType detects the pattern type for a group of events
func (e *OTELExporter) detectPatternType(events []*api.Event) string {
	// Simple pattern detection
	// Real implementation would use correlation engine
	
	errorCount := 0
	for _, event := range events {
		if event.Level == "ERROR" || event.Level == "CRITICAL" {
			errorCount++
		}
	}
	
	if errorCount > len(events)/2 {
		return "error_storm"
	}
	
	// Check for restart patterns
	restartCount := 0
	for _, event := range events {
		if event.Type == "pod_restart" || event.Type == "container_restart" {
			restartCount++
		}
	}
	
	if restartCount > 3 {
		return "restart_loop"
	}
	
	return "normal_activity"
}

// detectEventPattern detects patterns in individual events
func (e *OTELExporter) detectEventPattern(event *api.Event) string {
	// Simple pattern detection for demo
	// Real implementation would use ML/pattern matching
	
	switch event.Type {
	case "pod_oom_killed":
		return "memory_pressure"
	case "pod_evicted":
		return "resource_exhaustion"
	case "pod_restart":
		return "stability_issue"
	case "network_error":
		return "connectivity_issue"
	default:
		return "normal"
	}
}