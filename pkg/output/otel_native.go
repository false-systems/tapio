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
	
	// Create exporter with proper configuration
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.Endpoint),
	}
	
	if config.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	
	if config.Headers != nil && len(config.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
	}
	
	if config.Timeout > 0 {
		opts = append(opts, otlptracegrpc.WithTimeout(config.Timeout))
	}
	
	exporter, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL exporter: %w", err)
	}
	
	// Create tracer provider with batching for performance
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(time.Second),  // Flush every second
			sdktrace.WithMaxExportBatchSize(512),    // Max batch size
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample everything (we filter intelligently)
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
func (ono *OTELNativeOutput) OutputHealthCheck(ctx context.Context, report *health.Report) error {
	// Create root span for health check
	ctx, span := ono.tracer.Start(ctx, "tapio.check",
		trace.WithAttributes(
			attribute.String("check.status", string(report.OverallStatus)),
			attribute.Int("check.total_pods", report.TotalPods),
			attribute.Int("check.healthy_pods", report.HealthyPods),
			attribute.Int("check.issues_found", len(report.Issues)),
		),
	)
	defer span.End()
	
	// Create spans for each issue with full Tapio intelligence
	for _, issue := range report.Issues {
		ono.createHealthIssueSpan(ctx, issue)
	}
	
	// Set overall span status
	if report.OverallStatus == health.StatusCritical {
		span.SetStatus(codes.Error, "Critical health issues detected")
	} else if report.OverallStatus == health.StatusWarning {
		span.SetStatus(codes.Error, "Warning health issues detected")
	} else {
		span.SetStatus(codes.Ok, "System healthy")
	}
	
	return nil
}

// createHealthIssueSpan creates a span for a health issue
func (ono *OTELNativeOutput) createHealthIssueSpan(ctx context.Context, issue health.Issue) {
	ctx, span := ono.tracer.Start(ctx, "health.issue",
		trace.WithAttributes(
			// Core issue attributes
			attribute.String("issue.severity", string(issue.Severity)),
			attribute.String("issue.message", issue.Message),
			attribute.String("issue.resource", issue.Resource),
		),
	)
	defer span.End()
	
	// Set span status based on severity
	switch issue.Severity {
	case health.SeverityCritical:
		span.SetStatus(codes.Error, "Critical issue detected")
	case health.SeverityWarning:
		span.SetStatus(codes.Error, "Warning issue detected")
	default:
		span.SetStatus(codes.Ok, "Info issue")
	}
}

// Additional methods for predictions and recommendations would be added here
// when the health package is extended with these types

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

// Close closes the OTEL output and ensures all traces are sent
func (ono *OTELNativeOutput) Close() error {
	if ono.tracerProvider != nil {
		// Create a timeout context to ensure we don't hang forever
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		// Force flush any pending traces before shutdown
		if err := ono.tracerProvider.ForceFlush(ctx); err != nil {
			return fmt.Errorf("failed to flush traces: %w", err)
		}
		
		// Shutdown the provider which also flushes remaining traces
		if err := ono.tracerProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown tracer provider: %w", err)
		}
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