package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TraceExporter exports Tapio correlation results as OTEL traces
type TraceExporter struct {
	tracer trace.Tracer
	config *TraceConfig
}

// TraceConfig configures the trace exporter
type TraceConfig struct {
	ServiceName     string
	ServiceVersion  string
	TracerName      string
	
	// Performance settings
	MaxSpansPerTrace int
	ExportTimeout    time.Duration
	
	// Content settings
	IncludeFullEvents bool
	IncludeMetadata   bool
	SampleRate        float64
}

// NewTraceExporter creates a new OTEL trace exporter
func NewTraceExporter(config *TraceConfig) *TraceExporter {
	if config == nil {
		config = DefaultTraceConfig()
	}
	
	return &TraceExporter{
		tracer: otel.Tracer(config.TracerName),
		config: config,
	}
}

// DefaultTraceConfig returns sensible defaults for trace export
func DefaultTraceConfig() *TraceConfig {
	return &TraceConfig{
		ServiceName:       "tapio-correlation",
		ServiceVersion:    "1.0.0",
		TracerName:        "tapio-exports",
		MaxSpansPerTrace:  100,
		ExportTimeout:     5 * time.Second,
		IncludeFullEvents: true,
		IncludeMetadata:   true,
		SampleRate:        1.0,
	}
}

// ExportCorrelationResult creates an OTEL trace from a correlation result
func (te *TraceExporter) ExportCorrelationResult(ctx context.Context, result *correlation.Result) error {
	// Create root span for the correlation
	ctx, rootSpan := te.tracer.Start(ctx, "tapio.correlation.analysis",
		trace.WithAttributes(
			attribute.String("correlation.rule_id", result.RuleID),
			attribute.String("correlation.rule_name", result.RuleName),
			attribute.String("correlation.severity", string(result.Severity)),
			attribute.String("correlation.category", string(result.Category)),
			attribute.Float64("correlation.confidence", result.Confidence),
			attribute.String("correlation.title", result.Title),
			attribute.Int64("correlation.timestamp", result.Timestamp.UnixNano()),
		),
	)
	defer rootSpan.End()

	// Set span status based on severity
	switch result.Severity {
	case correlation.SeverityCritical:
		rootSpan.SetStatus(codes.Error, "Critical correlation found")
	case correlation.SeverityHigh:
		rootSpan.SetStatus(codes.Error, "High severity correlation found")
	case correlation.SeverityMedium:
		rootSpan.SetStatus(codes.Ok, "Medium severity correlation found")
	default:
		rootSpan.SetStatus(codes.Ok, "Low severity correlation found")
	}

	// Add description and impact if available
	if result.Description != "" {
		rootSpan.SetAttributes(attribute.String("correlation.description", result.Description))
	}
	if result.Impact != "" {
		rootSpan.SetAttributes(attribute.String("correlation.impact", result.Impact))
	}

	// Export evidence as child spans
	if err := te.exportEvidence(ctx, &result.Evidence); err != nil {
		rootSpan.RecordError(err)
		return fmt.Errorf("failed to export evidence: %w", err)
	}

	// Export recommendations as events
	te.exportRecommendations(rootSpan, result.Recommendations)

	// Export actions as events
	te.exportActions(rootSpan, result.Actions)

	return nil
}

// exportEvidence creates child spans for each piece of evidence
func (te *TraceExporter) exportEvidence(ctx context.Context, evidence *correlation.Evidence) error {
	// Create spans for related events
	for i, event := range evidence.Events {
		if i >= te.config.MaxSpansPerTrace {
			break // Prevent trace explosion
		}

		_, eventSpan := te.tracer.Start(ctx, "tapio.correlation.evidence.event",
			trace.WithAttributes(
				attribute.String("event.id", event.ID),
				attribute.String("event.source", string(event.Source)),
				attribute.String("event.type", event.Type),
				attribute.String("event.entity.type", event.Entity.Type),
				attribute.String("event.entity.name", event.Entity.Name),
				attribute.String("event.entity.namespace", event.Entity.Namespace),
				attribute.String("event.fingerprint", event.Fingerprint),
				attribute.Int64("event.timestamp", event.Timestamp.UnixNano()),
			),
		)

		// Add entity metadata if available
		if event.Entity.Node != "" {
			eventSpan.SetAttributes(attribute.String("event.entity.node", event.Entity.Node))
		}
		if event.Entity.Pod != "" {
			eventSpan.SetAttributes(attribute.String("event.entity.pod", event.Entity.Pod))
		}
		if event.Entity.Container != "" {
			eventSpan.SetAttributes(attribute.String("event.entity.container", event.Entity.Container))
		}

		// Add event attributes if configured
		if te.config.IncludeFullEvents {
			te.addEventAttributes(eventSpan, event.Attributes)
		}

		// Add labels
		te.addLabels(eventSpan, event.Labels)

		eventSpan.End()
	}

	// Create spans for timeline entries
	for i, entry := range evidence.Timeline {
		if i >= te.config.MaxSpansPerTrace {
			break
		}

		_, timelineSpan := te.tracer.Start(ctx, "tapio.correlation.evidence.timeline",
			trace.WithAttributes(
				attribute.String("timeline.description", entry.Description),
				attribute.String("timeline.source", entry.Source),
				attribute.Int64("timeline.timestamp", entry.Timestamp.UnixNano()),
			),
		)

		if entry.EventID != "" {
			timelineSpan.SetAttributes(attribute.String("timeline.event_id", entry.EventID))
		}

		timelineSpan.End()
	}

	// Add metric evidence as span events
	if len(evidence.Metrics) > 0 {
		_, metricSpan := te.tracer.Start(ctx, "tapio.correlation.evidence.metrics")
		
		for name, value := range evidence.Metrics {
			metricSpan.SetAttributes(attribute.Float64(fmt.Sprintf("metric.%s", name), value))
		}
		
		metricSpan.End()
	}

	return nil
}

// exportRecommendations adds recommendations as span events
func (te *TraceExporter) exportRecommendations(span trace.Span, recommendations []string) {
	for i, rec := range recommendations {
		span.AddEvent("recommendation", trace.WithAttributes(
			attribute.Int("recommendation.index", i),
			attribute.String("recommendation.text", rec),
		))
	}
}

// exportActions adds actions as span events
func (te *TraceExporter) exportActions(span trace.Span, actions []correlation.Action) {
	for i, action := range actions {
		attrs := []attribute.KeyValue{
			attribute.Int("action.index", i),
			attribute.String("action.type", action.Type),
			attribute.String("action.target", action.Target),
		}

		if action.Priority != "" {
			attrs = append(attrs, attribute.String("action.priority", action.Priority))
		}
		if action.Condition != "" {
			attrs = append(attrs, attribute.String("action.condition", action.Condition))
		}
		if action.Delay > 0 {
			attrs = append(attrs, attribute.Int64("action.delay_ms", action.Delay.Milliseconds()))
		}

		// Add action parameters
		for key, value := range action.Parameters {
			attrs = append(attrs, attribute.String(fmt.Sprintf("action.param.%s", key), value))
		}

		span.AddEvent("action", trace.WithAttributes(attrs...))
	}
}

// addEventAttributes adds event attributes to a span with proper type handling
func (te *TraceExporter) addEventAttributes(span trace.Span, attributes map[string]interface{}) {
	for key, value := range attributes {
		attrKey := fmt.Sprintf("event.attr.%s", key)
		
		switch v := value.(type) {
		case string:
			span.SetAttributes(attribute.String(attrKey, v))
		case int:
			span.SetAttributes(attribute.Int(attrKey, v))
		case int64:
			span.SetAttributes(attribute.Int64(attrKey, v))
		case float64:
			span.SetAttributes(attribute.Float64(attrKey, v))
		case bool:
			span.SetAttributes(attribute.Bool(attrKey, v))
		default:
			// Convert to string for complex types
			span.SetAttributes(attribute.String(attrKey, fmt.Sprintf("%v", v)))
		}
	}
}

// addLabels adds labels to a span as attributes
func (te *TraceExporter) addLabels(span trace.Span, labels map[string]string) {
	for key, value := range labels {
		span.SetAttributes(attribute.String(fmt.Sprintf("label.%s", key), value))
	}
}

// ExportBatch exports multiple correlation results as a single trace
func (te *TraceExporter) ExportBatch(ctx context.Context, results []*correlation.Result) error {
	if len(results) == 0 {
		return nil
	}

	// Create root span for the batch
	ctx, batchSpan := te.tracer.Start(ctx, "tapio.correlation.batch",
		trace.WithAttributes(
			attribute.Int("batch.size", len(results)),
			attribute.Int64("batch.timestamp", time.Now().UnixNano()),
		),
	)
	defer batchSpan.End()

	// Export each result as a child span
	for i, result := range results {
		if i >= te.config.MaxSpansPerTrace {
			batchSpan.AddEvent("batch.truncated", trace.WithAttributes(
				attribute.Int("batch.total_results", len(results)),
				attribute.Int("batch.exported_results", i),
			))
			break
		}

		if err := te.ExportCorrelationResult(ctx, result); err != nil {
			batchSpan.RecordError(err)
			// Continue with other results
		}
	}

	return nil
}

// ShouldSample determines if a correlation result should be sampled for export
func (te *TraceExporter) ShouldSample(result *correlation.Result) bool {
	// Always sample critical and high severity
	if result.Severity == correlation.SeverityCritical || result.Severity == correlation.SeverityHigh {
		return true
	}

	// Apply sampling rate for lower severity
	return te.config.SampleRate >= 1.0 || 
		   (te.config.SampleRate > 0 && float64(time.Now().UnixNano()%1000)/1000.0 < te.config.SampleRate)
}

// GetTracerProvider returns the configured tracer provider for external use
func (te *TraceExporter) GetTracerProvider() trace.TracerProvider {
	return otel.GetTracerProvider()
}

// SetTracerProvider allows external configuration of the tracer provider
func (te *TraceExporter) SetTracerProvider(provider trace.TracerProvider) {
	otel.SetTracerProvider(provider)
	te.tracer = provider.Tracer(te.config.TracerName)
}