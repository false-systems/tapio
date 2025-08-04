package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ServiceInstrumentation provides telemetry for a service
type ServiceInstrumentation struct {
	ServiceName string
	Logger      *zap.Logger

	// Tracing
	Tracer trace.Tracer

	// Common metrics
	RequestsTotal   metric.Int64Counter
	RequestDuration metric.Float64Histogram
	ActiveRequests  metric.Int64UpDownCounter
	ErrorsTotal     metric.Int64Counter

	// Service-specific metrics can be added
	meter metric.Meter
}

// NewServiceInstrumentation creates instrumentation for a service
func NewServiceInstrumentation(serviceName string, logger *zap.Logger) (*ServiceInstrumentation, error) {
	meter := otel.Meter(serviceName)
	tracer := otel.Tracer(serviceName)

	// Create common metrics
	requestsTotal, err := meter.Int64Counter(
		"tapio.requests.total",
		metric.WithDescription("Total number of requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Float64Histogram(
		"tapio.request.duration",
		metric.WithDescription("Request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"tapio.requests.active",
		metric.WithDescription("Number of active requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	errorsTotal, err := meter.Int64Counter(
		"tapio.errors.total",
		metric.WithDescription("Total number of errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	return &ServiceInstrumentation{
		ServiceName:     serviceName,
		Logger:          logger,
		Tracer:          tracer,
		meter:           meter,
		RequestsTotal:   requestsTotal,
		RequestDuration: requestDuration,
		ActiveRequests:  activeRequests,
		ErrorsTotal:     errorsTotal,
	}, nil
}

// StartSpan starts a new span and increments active requests
func (si *ServiceInstrumentation) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := si.Tracer.Start(ctx, name, opts...)
	si.ActiveRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", name),
	))
	return ctx, span
}

// EndSpan ends a span and records metrics
func (si *ServiceInstrumentation) EndSpan(span trace.Span, start time.Time, err error, operation string) {
	duration := time.Since(start).Seconds()

	// Record duration
	si.RequestDuration.Record(context.Background(), duration, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("error", err != nil),
	))

	// Decrement active requests
	si.ActiveRequests.Add(context.Background(), -1, metric.WithAttributes(
		attribute.String("operation", operation),
	))

	// Record request
	si.RequestsTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("error", err != nil),
	))

	// Record error if present
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		si.ErrorsTotal.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("error_type", errorType(err)),
		))
	}

	span.End()
}

// CorrelationInstrumentation provides telemetry specifically for correlation
type CorrelationInstrumentation struct {
	*ServiceInstrumentation

	// Correlation-specific metrics
	RulesEvaluated   metric.Int64Counter
	MatchesFound     metric.Int64Counter
	ProcessingTime   metric.Float64Histogram
	ConfidenceScore  metric.Float64Histogram
	EventsCorrelated metric.Int64Counter
	PatternsDetected metric.Int64Counter
}

// NewCorrelationInstrumentation creates instrumentation for correlation service
func NewCorrelationInstrumentation(logger *zap.Logger) (*CorrelationInstrumentation, error) {
	base, err := NewServiceInstrumentation("correlation-service", logger)
	if err != nil {
		return nil, err
	}

	// Create correlation-specific metrics
	rulesEvaluated, err := base.meter.Int64Counter(
		"tapio.correlation.rules_evaluated",
		metric.WithDescription("Number of correlation rules evaluated"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	matchesFound, err := base.meter.Int64Counter(
		"tapio.correlation.matches_found",
		metric.WithDescription("Number of successful correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	processingTime, err := base.meter.Float64Histogram(
		"tapio.correlation.processing_time",
		metric.WithDescription("Time to perform correlation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	confidenceScore, err := base.meter.Float64Histogram(
		"tapio.correlation.confidence_score",
		metric.WithDescription("Confidence score of correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	eventsCorrelated, err := base.meter.Int64Counter(
		"tapio.correlation.events_correlated",
		metric.WithDescription("Number of events correlated"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	patternsDetected, err := base.meter.Int64Counter(
		"tapio.correlation.patterns_detected",
		metric.WithDescription("Number of patterns detected"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	return &CorrelationInstrumentation{
		ServiceInstrumentation: base,
		RulesEvaluated:         rulesEvaluated,
		MatchesFound:           matchesFound,
		ProcessingTime:         processingTime,
		ConfidenceScore:        confidenceScore,
		EventsCorrelated:       eventsCorrelated,
		PatternsDetected:       patternsDetected,
	}, nil
}

// RecordCorrelation records a successful correlation with rich attributes
func (ci *CorrelationInstrumentation) RecordCorrelation(ctx context.Context, correlationType string, confidence float64, eventCount int, rootCause string) {
	ci.MatchesFound.Add(ctx, 1, metric.WithAttributes(
		attribute.String("correlation.type", correlationType),
		attribute.String("root_cause", rootCause),
	))

	ci.ConfidenceScore.Record(ctx, confidence, metric.WithAttributes(
		attribute.String("correlation.type", correlationType),
	))

	ci.EventsCorrelated.Add(ctx, int64(eventCount), metric.WithAttributes(
		attribute.String("correlation.type", correlationType),
	))

	// Add span event if we're in a span
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		span.AddEvent("correlation_found", trace.WithAttributes(
			attribute.String("correlation.type", correlationType),
			attribute.Float64("confidence", confidence),
			attribute.Int("event_count", eventCount),
			attribute.String("root_cause", rootCause),
		))
	}
}

// TransformerInstrumentation provides telemetry for transformer service
type TransformerInstrumentation struct {
	*ServiceInstrumentation

	// Transformer-specific metrics
	EventsTransformed    metric.Int64Counter
	TransformationTime   metric.Float64Histogram
	TransformationErrors metric.Int64Counter
	EventBatchSize       metric.Int64Histogram
}

// NewTransformerInstrumentation creates instrumentation for transformer service
func NewTransformerInstrumentation(logger *zap.Logger) (*TransformerInstrumentation, error) {
	base, err := NewServiceInstrumentation("transformer-service", logger)
	if err != nil {
		return nil, err
	}

	// Create transformer-specific metrics
	eventsTransformed, err := base.meter.Int64Counter(
		"tapio.transformer.events_transformed",
		metric.WithDescription("Number of events transformed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	transformationTime, err := base.meter.Float64Histogram(
		"tapio.transformer.transformation_time",
		metric.WithDescription("Time to transform events"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	transformationErrors, err := base.meter.Int64Counter(
		"tapio.transformer.errors",
		metric.WithDescription("Number of transformation errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	eventBatchSize, err := base.meter.Int64Histogram(
		"tapio.transformer.batch_size",
		metric.WithDescription("Size of event batches processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	return &TransformerInstrumentation{
		ServiceInstrumentation: base,
		EventsTransformed:      eventsTransformed,
		TransformationTime:     transformationTime,
		TransformationErrors:   transformationErrors,
		EventBatchSize:         eventBatchSize,
	}, nil
}

// errorType extracts a simple error type from an error
func errorType(err error) string {
	if err == nil {
		return "none"
	}
	// In production, you'd want to map specific error types
	return "generic"
}
