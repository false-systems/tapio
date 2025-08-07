package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// APIInstrumentation provides OpenTelemetry instrumentation for API service
type APIInstrumentation struct {
	tracer  trace.Tracer
	meter   metric.Meter
	logger  *zap.Logger
	metrics *apiMetrics
}

// apiMetrics holds all API metrics
type apiMetrics struct {
	// HTTP metrics
	httpRequests        metric.Int64Counter
	httpRequestDuration metric.Float64Histogram
	httpActiveRequests  metric.Int64UpDownCounter

	// API-specific metrics
	apiCalls           metric.Int64Counter
	correlationQueries metric.Int64Counter
	feedbackSubmitted  metric.Int64Counter
	errorRate          metric.Float64Counter
}

// NewAPIInstrumentation creates new API instrumentation
func NewAPIInstrumentation(logger *zap.Logger) (*APIInstrumentation, error) {
	if logger == nil {
		return nil, ErrLoggerRequired
	}

	tracer := GetTracer("api")
	meter := GetMeter("api")

	// Create HTTP metrics
	httpRequests, err := meter.Int64Counter("api.http.requests",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	httpRequestDuration, err := meter.Float64Histogram("api.http.request_duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	httpActiveRequests, err := meter.Int64UpDownCounter("api.http.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	// Create API-specific metrics
	apiCalls, err := meter.Int64Counter("api.calls",
		metric.WithDescription("Total number of API calls by endpoint"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	correlationQueries, err := meter.Int64Counter("api.correlation_queries",
		metric.WithDescription("Number of correlation queries"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	feedbackSubmitted, err := meter.Int64Counter("api.feedback_submitted",
		metric.WithDescription("Number of feedback submissions"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	errorRate, err := meter.Float64Counter("api.error_rate",
		metric.WithDescription("API error rate"),
		metric.WithUnit("1"))
	if err != nil {
		return nil, err
	}

	return &APIInstrumentation{
		tracer: tracer,
		meter:  meter,
		logger: logger,
		metrics: &apiMetrics{
			httpRequests:        httpRequests,
			httpRequestDuration: httpRequestDuration,
			httpActiveRequests:  httpActiveRequests,
			apiCalls:            apiCalls,
			correlationQueries:  correlationQueries,
			feedbackSubmitted:   feedbackSubmitted,
			errorRate:           errorRate,
		},
	}, nil
}

// StartSpan starts a new span
func (i *APIInstrumentation) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return i.tracer.Start(ctx, name, opts...)
}

// RecordHTTPRequest records HTTP request metrics
func (i *APIInstrumentation) RecordHTTPRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.path", path),
		attribute.Int("http.status_code", statusCode),
	}

	// Record request count
	i.metrics.httpRequests.Add(ctx, 1, metric.WithAttributes(attrs...))

	// Record duration in milliseconds
	i.metrics.httpRequestDuration.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(attrs...))

	// Record error if status >= 400
	if statusCode >= 400 {
		i.metrics.errorRate.Add(ctx, 1, metric.WithAttributes(attrs...))
	}

	i.logger.Debug("Recorded HTTP request",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status", statusCode),
		zap.Duration("duration", duration))
}

// RecordActiveRequest increments/decrements active request counter
func (i *APIInstrumentation) RecordActiveRequest(ctx context.Context, delta int64) {
	i.metrics.httpActiveRequests.Add(ctx, delta)
}

// RecordAPICall records an API call
func (i *APIInstrumentation) RecordAPICall(ctx context.Context, endpoint, resourceType string) {
	attrs := []attribute.KeyValue{
		attribute.String("api.endpoint", endpoint),
		attribute.String("resource.type", resourceType),
	}

	i.metrics.apiCalls.Add(ctx, 1, metric.WithAttributes(attrs...))

	// Record correlation query specifically
	if endpoint == "why" {
		i.metrics.correlationQueries.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordFeedback records feedback submission
func (i *APIInstrumentation) RecordFeedback(ctx context.Context, useful bool) {
	attrs := []attribute.KeyValue{
		attribute.Bool("feedback.useful", useful),
	}

	i.metrics.feedbackSubmitted.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordError records an error
func (i *APIInstrumentation) RecordError(ctx context.Context, endpoint string, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("api.endpoint", endpoint),
		attribute.String("error.type", getErrorType(err)),
	}

	i.metrics.errorRate.Add(ctx, 1, metric.WithAttributes(attrs...))

	i.logger.Error("API error",
		zap.String("endpoint", endpoint),
		zap.Error(err))
}

// getErrorType returns a categorized error type
func getErrorType(err error) string {
	if err == nil {
		return "none"
	}
	// Add error categorization logic here
	return "internal"
}
