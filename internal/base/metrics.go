package base

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

// RecordEvent records a successfully processed event with OTEL semantic conventions
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	if event == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("event.type", event.Type),
		EventDomainAttribute(event.Type),
	}

	m.EventsProcessed.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordDrop records a dropped event with OTEL semantic conventions
func (m *ObserverMetrics) RecordDrop(ctx context.Context, observerName string, eventType string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("event.type", eventType),
		EventDomainAttribute(eventType),
	}

	m.EventsDropped.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordError records an error with OTEL semantic conventions
func (m *ObserverMetrics) RecordError(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event.type", event.Type),
			EventDomainAttribute(event.Type),
		)
		if IsErrorEvent(event) {
			attrs = append(attrs, ErrorTypeAttribute(event.Type))
		}
	}

	m.ErrorsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordProcessingTime records processing duration in milliseconds with OTEL semantic conventions
func (m *ObserverMetrics) RecordProcessingTime(ctx context.Context, observerName string, event *domain.ObserverEvent, durationMs float64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event.type", event.Type),
			EventDomainAttribute(event.Type),
		)
	}

	m.ProcessingTime.Record(ctx, durationMs, metric.WithAttributes(attrs...))
}
