package base

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// ObserverMetrics holds OTEL metrics for observer telemetry
type ObserverMetrics struct {
	EventsProcessed metric.Int64Counter
	EventsDropped   metric.Int64Counter
	ErrorsTotal     metric.Int64Counter
	ProcessingTime  metric.Float64Histogram
}

// NewObserverMetrics creates OTEL metrics for an observer
func NewObserverMetrics(observerName string) (*ObserverMetrics, error) {
	meter := otel.Meter("tapio.observer")

	eventsProcessed, err := meter.Int64Counter(
		"observer_events_processed_total",
		metric.WithDescription("Total number of events processed by observer"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_processed counter: %w", err)
	}

	eventsDropped, err := meter.Int64Counter(
		"observer_events_dropped_total",
		metric.WithDescription("Total number of events dropped by observer"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_dropped counter: %w", err)
	}

	errorsTotal, err := meter.Int64Counter(
		"observer_errors_total",
		metric.WithDescription("Total number of errors in observer"),
		metric.WithUnit("{errors}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create errors_total counter: %w", err)
	}

	processingTime, err := meter.Float64Histogram(
		"observer_processing_duration_ms",
		metric.WithDescription("Processing duration for observer events"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing_duration histogram: %w", err)
	}

	return &ObserverMetrics{
		EventsProcessed: eventsProcessed,
		EventsDropped:   eventsDropped,
		ErrorsTotal:     errorsTotal,
		ProcessingTime:  processingTime,
	}, nil
}

// RecordEvent records a successfully processed event
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string) {
	m.EventsProcessed.Add(ctx, 1, metric.WithAttributes())
}

// RecordDrop records a dropped event
func (m *ObserverMetrics) RecordDrop(ctx context.Context, observerName string) {
	m.EventsDropped.Add(ctx, 1, metric.WithAttributes())
}

// RecordError records an error
func (m *ObserverMetrics) RecordError(ctx context.Context, observerName string) {
	m.ErrorsTotal.Add(ctx, 1, metric.WithAttributes())
}

// RecordProcessingTime records processing duration in milliseconds
func (m *ObserverMetrics) RecordProcessingTime(ctx context.Context, observerName string, durationMs float64) {
	m.ProcessingTime.Record(ctx, durationMs, metric.WithAttributes())
}
