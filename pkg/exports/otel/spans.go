package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SpanGenerator creates structured OTEL spans from Tapio events and correlations
type SpanGenerator struct {
	tracer trace.Tracer
	config *SpanConfig
}

// SpanConfig configures span generation behavior
type SpanConfig struct {
	// Naming
	SpanPrefix       string
	IncludeRuleID    bool
	IncludeTimestamp bool

	// Performance
	MaxAttributesPerSpan int
	MaxEventsPerSpan     int
	AttributeValueLimit  int

	// Content filtering
	ExcludeAttributes []string
	IncludeOnlyKeys   []string
	SanitizeValues    bool
}

// NewSpanGenerator creates a new span generator
func NewSpanGenerator(tracer trace.Tracer, config *SpanConfig) *SpanGenerator {
	if config == nil {
		config = DefaultSpanConfig()
	}

	return &SpanGenerator{
		tracer: tracer,
		config: config,
	}
}

// DefaultSpanConfig returns default span configuration
func DefaultSpanConfig() *SpanConfig {
	return &SpanConfig{
		SpanPrefix:           "tapio",
		IncludeRuleID:        true,
		IncludeTimestamp:     true,
		MaxAttributesPerSpan: 100,
		MaxEventsPerSpan:     50,
		AttributeValueLimit:  1000,
		SanitizeValues:       true,
	}
}

// GenerateCorrelationSpan creates a comprehensive span for a correlation result
func (sg *SpanGenerator) GenerateCorrelationSpan(ctx context.Context, result *correlation.Result) (context.Context, trace.Span) {
	spanName := sg.buildSpanName("correlation", result.RuleName)

	// Start span with core attributes
	ctx, span := sg.tracer.Start(ctx, spanName, trace.WithAttributes(
		sg.buildCoreAttributes(result)...,
	))

	// Set span status based on correlation result
	sg.setSpanStatus(span, result)

	// Add detailed attributes
	sg.addDetailedAttributes(span, result)

	// Add evidence as events
	sg.addEvidenceEvents(span, &result.Evidence)

	return ctx, span
}

// GenerateEventSpan creates a span for an individual event within a correlation
func (sg *SpanGenerator) GenerateEventSpan(ctx context.Context, event *correlation.Event, parentResult *correlation.Result) (context.Context, trace.Span) {
	spanName := sg.buildSpanName("event", event.Type)

	ctx, span := sg.tracer.Start(ctx, spanName, trace.WithAttributes(
		attribute.String("event.id", event.ID),
		attribute.String("event.source", string(event.Source)),
		attribute.String("event.type", event.Type),
		attribute.String("event.fingerprint", event.Fingerprint),
		attribute.Int64("event.timestamp_ns", event.Timestamp.UnixNano()),
	))

	// Add entity information
	sg.addEntityAttributes(span, &event.Entity)

	// Add event-specific attributes
	sg.addEventAttributes(span, event)

	// Link to parent correlation if available
	if parentResult != nil {
		span.SetAttributes(
			attribute.String("correlation.parent_rule_id", parentResult.RuleID),
			attribute.String("correlation.parent_title", parentResult.Title),
		)
	}

	return ctx, span
}

// GenerateTimelineSpan creates spans for timeline entries
func (sg *SpanGenerator) GenerateTimelineSpan(ctx context.Context, entry *correlation.TimelineEntry, correlationID string) (context.Context, trace.Span) {
	spanName := sg.buildSpanName("timeline", "entry")

	ctx, span := sg.tracer.Start(ctx, spanName, trace.WithAttributes(
		attribute.String("timeline.description", sg.sanitizeValue(entry.Description)),
		attribute.String("timeline.source", entry.Source),
		attribute.Int64("timeline.timestamp_ns", entry.Timestamp.UnixNano()),
		attribute.String("timeline.correlation_id", correlationID),
	))

	if entry.EventID != "" {
		span.SetAttributes(attribute.String("timeline.event_id", entry.EventID))
	}

	return ctx, span
}

// buildSpanName constructs consistent span names
func (sg *SpanGenerator) buildSpanName(category, name string) string {
	spanName := fmt.Sprintf("%s.%s", sg.config.SpanPrefix, category)

	if name != "" {
		spanName = fmt.Sprintf("%s.%s", spanName, sg.sanitizeSpanName(name))
	}

	return spanName
}

// buildCoreAttributes creates the core attributes for a correlation span
func (sg *SpanGenerator) buildCoreAttributes(result *correlation.Result) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("correlation.severity", string(result.Severity)),
		attribute.String("correlation.category", string(result.Category)),
		attribute.Float64("correlation.confidence", result.Confidence),
		attribute.String("correlation.title", sg.sanitizeValue(result.Title)),
	}

	if sg.config.IncludeRuleID {
		attrs = append(attrs,
			attribute.String("correlation.rule_id", result.RuleID),
			attribute.String("correlation.rule_name", result.RuleName),
		)
	}

	if sg.config.IncludeTimestamp {
		attrs = append(attrs, attribute.Int64("correlation.timestamp_ns", result.Timestamp.UnixNano()))
	}

	return attrs
}

// setSpanStatus sets appropriate span status based on correlation severity
func (sg *SpanGenerator) setSpanStatus(span trace.Span, result *correlation.Result) {
	switch result.Severity {
	case correlation.SeverityCritical:
		span.SetStatus(codes.Error, fmt.Sprintf("Critical: %s", result.Title))
	case correlation.SeverityHigh:
		span.SetStatus(codes.Error, fmt.Sprintf("High: %s", result.Title))
	case correlation.SeverityMedium:
		span.SetStatus(codes.Ok, fmt.Sprintf("Medium: %s", result.Title))
	default:
		span.SetStatus(codes.Ok, fmt.Sprintf("Low: %s", result.Title))
	}
}

// addDetailedAttributes adds comprehensive attributes to a span
func (sg *SpanGenerator) addDetailedAttributes(span trace.Span, result *correlation.Result) {
	attrCount := 0

	// Add description and impact
	if result.Description != "" && attrCount < sg.config.MaxAttributesPerSpan {
		span.SetAttributes(attribute.String("correlation.description", sg.sanitizeValue(result.Description)))
		attrCount++
	}

	if result.Impact != "" && attrCount < sg.config.MaxAttributesPerSpan {
		span.SetAttributes(attribute.String("correlation.impact", sg.sanitizeValue(result.Impact)))
		attrCount++
	}

	// Add TTL if specified
	if result.TTL > 0 && attrCount < sg.config.MaxAttributesPerSpan {
		span.SetAttributes(attribute.Int64("correlation.ttl_seconds", int64(result.TTL.Seconds())))
		attrCount++
	}

	// Add evidence metrics
	if len(result.Evidence.Metrics) > 0 {
		for key, value := range result.Evidence.Metrics {
			if attrCount >= sg.config.MaxAttributesPerSpan {
				break
			}
			if sg.shouldIncludeAttribute(key) {
				span.SetAttributes(attribute.Float64(fmt.Sprintf("evidence.metric.%s", key), value))
				attrCount++
			}
		}
	}

	// Add evidence patterns
	if len(result.Evidence.Patterns) > 0 {
		for i, pattern := range result.Evidence.Patterns {
			if attrCount >= sg.config.MaxAttributesPerSpan {
				break
			}
			span.SetAttributes(attribute.String(fmt.Sprintf("evidence.pattern.%d", i), sg.sanitizeValue(pattern)))
			attrCount++
		}
	}

	// Add metadata
	if len(result.Metadata) > 0 {
		for key, value := range result.Metadata {
			if attrCount >= sg.config.MaxAttributesPerSpan {
				break
			}
			if sg.shouldIncludeAttribute(key) {
				span.SetAttributes(attribute.String(fmt.Sprintf("metadata.%s", key), sg.sanitizeValue(value)))
				attrCount++
			}
		}
	}
}

// addEntityAttributes adds entity information to a span
func (sg *SpanGenerator) addEntityAttributes(span trace.Span, entity *correlation.Entity) {
	span.SetAttributes(
		attribute.String("entity.type", entity.Type),
		attribute.String("entity.name", entity.Name),
		attribute.String("entity.uid", entity.UID),
	)

	if entity.Namespace != "" {
		span.SetAttributes(attribute.String("entity.namespace", entity.Namespace))
	}
	if entity.Node != "" {
		span.SetAttributes(attribute.String("entity.node", entity.Node))
	}
	if entity.Pod != "" {
		span.SetAttributes(attribute.String("entity.pod", entity.Pod))
	}
	if entity.Container != "" {
		span.SetAttributes(attribute.String("entity.container", entity.Container))
	}
	if entity.Process != "" {
		span.SetAttributes(attribute.String("entity.process", entity.Process))
	}

	// Add entity metadata
	attrCount := 8 // Count of attributes already added
	for key, value := range entity.Metadata {
		if attrCount >= sg.config.MaxAttributesPerSpan {
			break
		}
		if sg.shouldIncludeAttribute(key) {
			span.SetAttributes(attribute.String(fmt.Sprintf("entity.meta.%s", key), sg.sanitizeValue(value)))
			attrCount++
		}
	}
}

// addEventAttributes adds event-specific attributes
func (sg *SpanGenerator) addEventAttributes(span trace.Span, event *correlation.Event) {
	attrCount := 0

	// Add event attributes
	for key, value := range event.Attributes {
		if attrCount >= sg.config.MaxAttributesPerSpan {
			break
		}
		if sg.shouldIncludeAttribute(key) {
			sg.addTypedAttribute(span, fmt.Sprintf("event.attr.%s", key), value)
			attrCount++
		}
	}

	// Add labels
	for key, value := range event.Labels {
		if attrCount >= sg.config.MaxAttributesPerSpan {
			break
		}
		if sg.shouldIncludeAttribute(key) {
			span.SetAttributes(attribute.String(fmt.Sprintf("event.label.%s", key), sg.sanitizeValue(value)))
			attrCount++
		}
	}
}

// addEvidenceEvents adds evidence as span events
func (sg *SpanGenerator) addEvidenceEvents(span trace.Span, evidence *correlation.Evidence) {
	eventCount := 0

	// Add timeline entries as events
	for _, entry := range evidence.Timeline {
		if eventCount >= sg.config.MaxEventsPerSpan {
			break
		}

		span.AddEvent("timeline_entry", trace.WithAttributes(
			attribute.String("description", sg.sanitizeValue(entry.Description)),
			attribute.String("source", entry.Source),
			attribute.Int64("timestamp_ns", entry.Timestamp.UnixNano()),
		), trace.WithTimestamp(entry.Timestamp))

		eventCount++
	}

	// Add significant patterns as events
	for _, pattern := range evidence.Patterns {
		if eventCount >= sg.config.MaxEventsPerSpan {
			break
		}

		span.AddEvent("pattern_detected", trace.WithAttributes(
			attribute.String("pattern", sg.sanitizeValue(pattern)),
		))

		eventCount++
	}
}

// addTypedAttribute adds an attribute with proper type handling
func (sg *SpanGenerator) addTypedAttribute(span trace.Span, key string, value interface{}) {
	switch v := value.(type) {
	case string:
		span.SetAttributes(attribute.String(key, sg.sanitizeValue(v)))
	case int:
		span.SetAttributes(attribute.Int(key, v))
	case int64:
		span.SetAttributes(attribute.Int64(key, v))
	case float64:
		span.SetAttributes(attribute.Float64(key, v))
	case bool:
		span.SetAttributes(attribute.Bool(key, v))
	case time.Time:
		span.SetAttributes(attribute.Int64(key+"_ns", v.UnixNano()))
	case time.Duration:
		span.SetAttributes(attribute.Int64(key+"_ms", v.Milliseconds()))
	default:
		// Convert complex types to string
		span.SetAttributes(attribute.String(key, sg.sanitizeValue(fmt.Sprintf("%v", v))))
	}
}

// shouldIncludeAttribute determines if an attribute should be included
func (sg *SpanGenerator) shouldIncludeAttribute(key string) bool {
	// Check exclusion list
	for _, excluded := range sg.config.ExcludeAttributes {
		if key == excluded {
			return false
		}
	}

	// Check inclusion list (if specified)
	if len(sg.config.IncludeOnlyKeys) > 0 {
		for _, included := range sg.config.IncludeOnlyKeys {
			if key == included {
				return true
			}
		}
		return false
	}

	return true
}

// sanitizeValue sanitizes attribute values for safe export
func (sg *SpanGenerator) sanitizeValue(value string) string {
	if !sg.config.SanitizeValues {
		return value
	}

	// Limit value length
	if len(value) > sg.config.AttributeValueLimit {
		return value[:sg.config.AttributeValueLimit] + "..."
	}

	// Remove potential PII or sensitive data patterns
	// This is a basic implementation - extend as needed
	return value
}

// sanitizeSpanName sanitizes span names for consistency
func (sg *SpanGenerator) sanitizeSpanName(name string) string {
	// Replace spaces and special characters with underscores
	result := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}

// GenerateRuleExecutionSpan creates a span for rule execution tracking
func (sg *SpanGenerator) GenerateRuleExecutionSpan(ctx context.Context, ruleID, ruleName string, startTime time.Time) (context.Context, trace.Span) {
	spanName := sg.buildSpanName("rule", ruleName)

	ctx, span := sg.tracer.Start(ctx, spanName,
		trace.WithTimestamp(startTime),
		trace.WithAttributes(
			attribute.String("rule.id", ruleID),
			attribute.String("rule.name", ruleName),
			attribute.Int64("rule.start_time_ns", startTime.UnixNano()),
		),
	)

	return ctx, span
}

// FinishRuleExecutionSpan completes a rule execution span with results
func (sg *SpanGenerator) FinishRuleExecutionSpan(span trace.Span, duration time.Duration, matched bool, err error) {
	span.SetAttributes(
		attribute.Int64("rule.duration_ms", duration.Milliseconds()),
		attribute.Bool("rule.matched", matched),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Rule execution failed")
	} else {
		if matched {
			span.SetStatus(codes.Ok, "Rule matched")
		} else {
			span.SetStatus(codes.Ok, "Rule did not match")
		}
	}

	span.End()
}
