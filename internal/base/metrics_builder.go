package base

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MetricBuilder provides fluent API for creating OTEL metrics
// Eliminates boilerplate by chaining metric creation calls
type MetricBuilder struct {
	meter metric.Meter
	errs  []error
}

// NewMetricBuilder creates a new metric builder for an observer
func NewMetricBuilder(observerName string) *MetricBuilder {
	return &MetricBuilder{
		meter: otel.Meter(fmt.Sprintf("tapio.observer.%s", observerName)),
	}
}

// Counter adds an Int64Counter with fluent API
// Example: builder.Counter(&myCounter, "requests_total", "Total requests")
func (mb *MetricBuilder) Counter(target *metric.Int64Counter, name, description string) *MetricBuilder {
	counter, err := mb.meter.Int64Counter(
		name,
		metric.WithDescription(description),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		mb.errs = append(mb.errs, fmt.Errorf("failed to create counter %s: %w", name, err))
	} else {
		*target = counter
	}
	return mb
}

// Gauge adds a Float64Gauge with fluent API
// Example: builder.Gauge(&myGauge, "queue_size", "Current queue size")
func (mb *MetricBuilder) Gauge(target *metric.Float64Gauge, name, description string) *MetricBuilder {
	gauge, err := mb.meter.Float64Gauge(
		name,
		metric.WithDescription(description),
	)
	if err != nil {
		mb.errs = append(mb.errs, fmt.Errorf("failed to create gauge %s: %w", name, err))
	} else {
		*target = gauge
	}
	return mb
}

// Int64Gauge adds an Int64Gauge with fluent API
// Example: builder.Int64Gauge(&myGauge, "connections", "Active connections")
func (mb *MetricBuilder) Int64Gauge(target *metric.Int64Gauge, name, description string) *MetricBuilder {
	gauge, err := mb.meter.Int64Gauge(
		name,
		metric.WithDescription(description),
	)
	if err != nil {
		mb.errs = append(mb.errs, fmt.Errorf("failed to create int64 gauge %s: %w", name, err))
	} else {
		*target = gauge
	}
	return mb
}

// Histogram adds a Float64Histogram with fluent API
// Example: builder.Histogram(&myHisto, "latency_ms", "Request latency")
func (mb *MetricBuilder) Histogram(target *metric.Float64Histogram, name, description string) *MetricBuilder {
	histogram, err := mb.meter.Float64Histogram(
		name,
		metric.WithDescription(description),
	)
	if err != nil {
		mb.errs = append(mb.errs, fmt.Errorf("failed to create histogram %s: %w", name, err))
	} else {
		*target = histogram
	}
	return mb
}

// Build returns error if any metric creation failed
// Call this at the end of the chain to check for errors
func (mb *MetricBuilder) Build() error {
	if len(mb.errs) > 0 {
		return fmt.Errorf("failed to create metrics: %v", mb.errs)
	}
	return nil
}
