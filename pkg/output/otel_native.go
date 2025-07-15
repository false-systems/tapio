package output

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/health"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// OTELNativeOutput implements OTEL as the DEFAULT output format for Tapio
// This is the BEST way to consume Tapio's intelligence in enterprise environments
type OTELNativeOutput struct {
	tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	config         *OTELOutputConfig
}

// OTELOutputConfig configures native OTEL output
type OTELOutputConfig struct {
	// Collector endpoint (default: localhost:4317)
	Endpoint string
	
	// Service identification
	ServiceName     string
	ServiceVersion  string
	ServiceInstance string
	
	// Export settings
	Insecure bool
	Headers  map[string]string
	Timeout  time.Duration
	
	// Feature flags
	IncludeHumanExplanations bool
	IncludePredictions      bool
	IncludeRecommendations  bool
	IncludeBusinessImpact   bool
}

// NewOTELNativeOutput creates the native OTEL output formatter
func NewOTELNativeOutput(config *OTELOutputConfig) (*OTELNativeOutput, error) {
	if config == nil {
		config = DefaultOTELOutputConfig()
	}
	
	// Create resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			semconv.ServiceInstanceID(config.ServiceInstance),
			attribute.String("tapio.intelligence", "enabled"),
			attribute.String("tapio.output", "native-otel"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	
	// Create exporter
	exporter, err := otlptracegrpc.New(
		context.Background(),
		otlptracegrpc.WithEndpoint(config.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL exporter: %w", err)
	}
	
	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	
	otel.SetTracerProvider(tp)
	
	return &OTELNativeOutput{
		tracer:         tp.Tracer("tapio-check"),
		tracerProvider: tp,
		config:         config,
	}, nil
}

// DefaultOTELOutputConfig returns default configuration
func DefaultOTELOutputConfig() *OTELOutputConfig {
	return &OTELOutputConfig{
		Endpoint:                 "localhost:4317",
		ServiceName:              "tapio",
		ServiceVersion:           "1.0.0",
		ServiceInstance:          "tapio-cli",
		Insecure:                 true,
		Timeout:                  10 * time.Second,
		IncludeHumanExplanations: true,
		IncludePredictions:       true,
		IncludeRecommendations:   true,
		IncludeBusinessImpact:    true,
	}
}

// OutputHealthCheck outputs health check results as OTEL traces
func (ono *OTELNativeOutput) OutputHealthCheck(ctx context.Context, analysis *health.Analysis) error {
	// Create root span for health check
	ctx, span := ono.tracer.Start(ctx, "tapio.check",
		trace.WithAttributes(
			attribute.String("check.target", analysis.Target),
			attribute.String("check.namespace", analysis.Namespace),
			attribute.String("check.status", string(analysis.Status)),
			attribute.Float64("check.health_score", analysis.HealthScore),
			attribute.Int("check.issues_found", len(analysis.Issues)),
		),
	)
	defer span.End()
	
	// Create spans for each issue with full Tapio intelligence
	for _, issue := range analysis.Issues {
		ono.createIssueSpan(ctx, issue)
	}
	
	// Create span for predictions if available
	if len(analysis.Predictions) > 0 && ono.config.IncludePredictions {
		ono.createPredictionSpan(ctx, analysis.Predictions)
	}
	
	// Create span for recommendations
	if len(analysis.Recommendations) > 0 && ono.config.IncludeRecommendations {
		ono.createRecommendationSpan(ctx, analysis.Recommendations)
	}
	
	// Set overall span status
	if analysis.Status == health.StatusCritical {
		span.SetStatus(codes.Error, "Critical health issues detected")
	} else if analysis.Status == health.StatusDegraded {
		span.SetStatus(codes.Error, "Degraded health detected")
	} else {
		span.SetStatus(codes.Ok, "System healthy")
	}
	
	return nil
}

// createIssueSpan creates a span for a health issue with rich Tapio metadata
func (ono *OTELNativeOutput) createIssueSpan(ctx context.Context, issue health.Issue) {
	ctx, span := ono.tracer.Start(ctx, fmt.Sprintf("issue.%s", issue.Type),
		trace.WithAttributes(
			// Core issue attributes
			attribute.String("issue.id", issue.ID),
			attribute.String("issue.type", issue.Type),
			attribute.String("issue.severity", string(issue.Severity)),
			attribute.Float64("issue.confidence", issue.Confidence),
			
			// Entity information
			attribute.String("entity.type", issue.Entity.Type),
			attribute.String("entity.name", issue.Entity.Name),
			attribute.String("entity.namespace", issue.Entity.Namespace),
			
			// Tapio intelligence attributes
			attribute.String("tapio.pattern", issue.Pattern),
			attribute.Bool("tapio.is_predicted", issue.IsPredicted),
			attribute.Float64("tapio.risk_score", issue.RiskScore),
		),
	)
	defer span.End()
	
	// Add human explanation if enabled
	if ono.config.IncludeHumanExplanations && issue.HumanExplanation != nil {
		span.SetAttributes(
			attribute.String("human.what_happened", issue.HumanExplanation.WhatHappened),
			attribute.String("human.why_it_happened", issue.HumanExplanation.WhyItHappened),
			attribute.String("human.what_to_do", issue.HumanExplanation.WhatToDo),
			attribute.String("human.how_to_prevent", issue.HumanExplanation.HowToPrevent),
			attribute.Bool("human.is_urgent", issue.HumanExplanation.IsUrgent),
		)
	}
	
	// Add business impact if enabled
	if ono.config.IncludeBusinessImpact && issue.BusinessImpact != nil {
		span.SetAttributes(
			attribute.Float64("business.impact_score", issue.BusinessImpact.Score),
			attribute.String("business.affected_services", fmt.Sprintf("%v", issue.BusinessImpact.AffectedServices)),
			attribute.Int("business.affected_users", issue.BusinessImpact.AffectedUsers),
			attribute.Float64("business.revenue_risk", issue.BusinessImpact.RevenueRisk),
		)
	}
	
	// Add correlation information
	if issue.CorrelationGroup != nil {
		span.SetAttributes(
			attribute.String("correlation.group_id", issue.CorrelationGroup.ID),
			attribute.String("correlation.root_cause", issue.CorrelationGroup.RootCause),
			attribute.Int("correlation.related_events", issue.CorrelationGroup.RelatedEvents),
			attribute.Float64("correlation.confidence", issue.CorrelationGroup.Confidence),
		)
		
		// Create span link to correlation group
		if issue.CorrelationGroup.TraceID != "" {
			// Link to semantic correlation trace
			span.AddLink(trace.Link{
				SpanContext: trace.SpanContext{},
				Attributes: []attribute.KeyValue{
					attribute.String("link.type", "correlation_group"),
					attribute.String("link.group_id", issue.CorrelationGroup.ID),
				},
			})
		}
	}
	
	// Add evidence as span events
	for i, evidence := range issue.Evidence {
		span.AddEvent(fmt.Sprintf("evidence_%d", i),
			trace.WithAttributes(
				attribute.String("evidence.type", evidence.Type),
				attribute.String("evidence.description", evidence.Description),
				attribute.Float64("evidence.confidence", evidence.Confidence),
			),
			trace.WithTimestamp(evidence.Timestamp),
		)
	}
	
	// Set span status based on severity
	switch issue.Severity {
	case health.SeverityCritical:
		span.SetStatus(codes.Error, "Critical issue detected")
	case health.SeverityHigh:
		span.SetStatus(codes.Error, "High severity issue")
	case health.SeverityMedium:
		span.SetStatus(codes.Error, "Medium severity issue")
	default:
		span.SetStatus(codes.Ok, "Low severity issue")
	}
}

// createPredictionSpan creates a span for predictions
func (ono *OTELNativeOutput) createPredictionSpan(ctx context.Context, predictions []health.Prediction) {
	ctx, span := ono.tracer.Start(ctx, "tapio.predictions",
		trace.WithAttributes(
			attribute.Int("predictions.count", len(predictions)),
		),
	)
	defer span.End()
	
	for _, prediction := range predictions {
		// Create child span for each prediction
		_, predSpan := ono.tracer.Start(ctx, fmt.Sprintf("prediction.%s", prediction.Type),
			trace.WithAttributes(
				attribute.String("prediction.id", prediction.ID),
				attribute.String("prediction.type", prediction.Type),
				attribute.String("prediction.scenario", prediction.Scenario),
				attribute.Float64("prediction.probability", prediction.Probability),
				attribute.Float64("prediction.confidence", prediction.Confidence),
				attribute.Int64("prediction.time_to_event_seconds", int64(prediction.TimeToEvent.Seconds())),
				attribute.String("prediction.severity", prediction.Severity),
			),
		)
		
		// Add prevention actions
		for i, action := range prediction.PreventionActions {
			predSpan.AddEvent(fmt.Sprintf("prevention_action_%d", i),
				trace.WithAttributes(
					attribute.String("action.type", "prevention"),
					attribute.String("action.command", action),
				),
			)
		}
		
		predSpan.End()
	}
}

// createRecommendationSpan creates a span for recommendations
func (ono *OTELNativeOutput) createRecommendationSpan(ctx context.Context, recommendations []health.Recommendation) {
	ctx, span := ono.tracer.Start(ctx, "tapio.recommendations",
		trace.WithAttributes(
			attribute.Int("recommendations.count", len(recommendations)),
		),
	)
	defer span.End()
	
	for i, rec := range recommendations {
		span.AddEvent(fmt.Sprintf("recommendation_%d", i),
			trace.WithAttributes(
				attribute.String("recommendation.type", rec.Type),
				attribute.String("recommendation.action", rec.Action),
				attribute.String("recommendation.command", rec.Command),
				attribute.Float64("recommendation.priority", rec.Priority),
				attribute.Float64("recommendation.expected_improvement", rec.ExpectedImprovement),
			),
		)
	}
}

// OutputCorrelation outputs correlation results as OTEL traces
func (ono *OTELNativeOutput) OutputCorrelation(ctx context.Context, result *correlation.CorrelationResult) error {
	// Create span for correlation analysis
	ctx, span := ono.tracer.Start(ctx, "tapio.correlation",
		trace.WithAttributes(
			attribute.String("correlation.id", result.ID),
			attribute.String("correlation.type", result.Type),
			attribute.Float64("correlation.confidence", result.Confidence),
			attribute.Int("correlation.events_analyzed", result.EventsAnalyzed),
			attribute.Int("correlation.patterns_found", len(result.Patterns)),
		),
	)
	defer span.End()
	
	// Add patterns as child spans
	for _, pattern := range result.Patterns {
		ono.createPatternSpan(ctx, pattern)
	}
	
	// Add insights
	for i, insight := range result.Insights {
		span.AddEvent(fmt.Sprintf("insight_%d", i),
			trace.WithAttributes(
				attribute.String("insight.type", insight.Type),
				attribute.String("insight.description", insight.Description),
				attribute.Float64("insight.confidence", insight.Confidence),
				attribute.Bool("insight.actionable", insight.IsActionable),
			),
		)
	}
	
	return nil
}

// createPatternSpan creates a span for a detected pattern
func (ono *OTELNativeOutput) createPatternSpan(ctx context.Context, pattern correlation.Pattern) {
	_, span := ono.tracer.Start(ctx, fmt.Sprintf("pattern.%s", pattern.Type),
		trace.WithAttributes(
			attribute.String("pattern.id", pattern.ID),
			attribute.String("pattern.type", pattern.Type),
			attribute.String("pattern.name", pattern.Name),
			attribute.Float64("pattern.confidence", pattern.Confidence),
			attribute.Bool("pattern.is_anomaly", pattern.IsAnomaly),
			attribute.Int("pattern.occurrence_count", pattern.OccurrenceCount),
			attribute.Int64("pattern.time_window_seconds", int64(pattern.TimeWindow.Seconds())),
		),
	)
	defer span.End()
	
	// Add affected entities
	if len(pattern.AffectedEntities) > 0 {
		entities := make([]string, len(pattern.AffectedEntities))
		for i, entity := range pattern.AffectedEntities {
			entities[i] = fmt.Sprintf("%s:%s", entity.Type, entity.Name)
		}
		span.SetAttributes(attribute.StringSlice("pattern.affected_entities", entities))
	}
}

// Close closes the OTEL output
func (ono *OTELNativeOutput) Close() error {
	if ono.tracerProvider != nil {
		return ono.tracerProvider.Shutdown(context.Background())
	}
	return nil
}

// FormatHealthCheck formats health check results for display (fallback to text)
func (ono *OTELNativeOutput) FormatHealthCheck(analysis *health.Analysis) string {
	// This is only used when --human flag is specified
	return fmt.Sprintf("OTEL traces exported to %s\nHealth Status: %s\nIssues Found: %d\n",
		ono.config.Endpoint,
		analysis.Status,
		len(analysis.Issues),
	)
}