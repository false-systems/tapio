package otel

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsCollector provides methods for collecting Tapio-specific metrics
type MetricsCollector struct {
	meter metric.Meter

	// Counters
	eventsProcessed   metric.Int64Counter
	eventsDropped     metric.Int64Counter
	correlationsFound metric.Int64Counter
	errorCount        metric.Int64Counter

	// Histograms
	eventProcessingDuration metric.Float64Histogram
	batchSize               metric.Int64Histogram
	correlationConfidence   metric.Float64Histogram

	// Gauges
	activeSemanticGroups metric.Int64UpDownCounter
	bufferUtilization    metric.Float64ObservableGauge
	connectionStatus     metric.Int64ObservableGauge

	// Callbacks for observable instruments
	bufferCallback      func() float64
	connectionCallback  func() int64
	observableCallbacks []metric.Registration
	mu                  sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(meter metric.Meter) (*MetricsCollector, error) {
	mc := &MetricsCollector{
		meter:               meter,
		observableCallbacks: make([]metric.Registration, 0),
	}

	// Initialize counters
	eventsProcessed, err := meter.Int64Counter(
		"tapio.events.processed.total",
		metric.WithDescription("Total number of events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.eventsProcessed = eventsProcessed

	eventsDropped, err := meter.Int64Counter(
		"tapio.events.dropped.total",
		metric.WithDescription("Total number of events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.eventsDropped = eventsDropped

	correlationsFound, err := meter.Int64Counter(
		"tapio.correlations.found.total",
		metric.WithDescription("Total number of correlations found"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.correlationsFound = correlationsFound

	errorCount, err := meter.Int64Counter(
		"tapio.errors.total",
		metric.WithDescription("Total number of errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.errorCount = errorCount

	// Initialize histograms
	eventProcessingDuration, err := meter.Float64Histogram(
		"tapio.event.processing.duration",
		metric.WithDescription("Duration of event processing in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	mc.eventProcessingDuration = eventProcessingDuration

	batchSize, err := meter.Int64Histogram(
		"tapio.batch.size",
		metric.WithDescription("Size of event batches"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.batchSize = batchSize

	correlationConfidence, err := meter.Float64Histogram(
		"tapio.correlation.confidence",
		metric.WithDescription("Confidence score of correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.correlationConfidence = correlationConfidence

	// Initialize gauges
	activeSemanticGroups, err := meter.Int64UpDownCounter(
		"tapio.semantic.groups.active",
		metric.WithDescription("Number of active semantic groups"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.activeSemanticGroups = activeSemanticGroups

	// Initialize observable gauges
	bufferUtilization, err := meter.Float64ObservableGauge(
		"tapio.buffer.utilization",
		metric.WithDescription("Buffer utilization percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return nil, err
	}
	mc.bufferUtilization = bufferUtilization

	connectionStatus, err := meter.Int64ObservableGauge(
		"tapio.connection.status",
		metric.WithDescription("Connection status (1=connected, 0=disconnected)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	mc.connectionStatus = connectionStatus

	return mc, nil
}

// RegisterBufferCallback sets the callback for buffer utilization metric
func (mc *MetricsCollector) RegisterBufferCallback(ctx context.Context, callback func() float64, attrs ...attribute.KeyValue) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.bufferCallback = callback

	reg, err := mc.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			if mc.bufferCallback != nil {
				o.ObserveFloat64(mc.bufferUtilization, mc.bufferCallback(), metric.WithAttributes(attrs...))
			}
			return nil
		},
		mc.bufferUtilization,
	)
	if err != nil {
		return err
	}

	mc.observableCallbacks = append(mc.observableCallbacks, reg)
	return nil
}

// RegisterConnectionCallback sets the callback for connection status metric
func (mc *MetricsCollector) RegisterConnectionCallback(ctx context.Context, callback func() int64, attrs ...attribute.KeyValue) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.connectionCallback = callback

	reg, err := mc.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			if mc.connectionCallback != nil {
				o.ObserveInt64(mc.connectionStatus, mc.connectionCallback(), metric.WithAttributes(attrs...))
			}
			return nil
		},
		mc.connectionStatus,
	)
	if err != nil {
		return err
	}

	mc.observableCallbacks = append(mc.observableCallbacks, reg)
	return nil
}

// RecordEventProcessed records a processed event with attributes
func (mc *MetricsCollector) RecordEventProcessed(ctx context.Context, eventType string, source string) {
	mc.eventsProcessed.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("event.type", eventType),
			attribute.String("event.source", source),
		),
	)
}

// RecordEventDropped records a dropped event with reason
func (mc *MetricsCollector) RecordEventDropped(ctx context.Context, reason string) {
	mc.eventsDropped.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("drop.reason", reason),
		),
	)
}

// RecordEventProcessingDuration records the duration of event processing
func (mc *MetricsCollector) RecordEventProcessingDuration(ctx context.Context, duration time.Duration, eventType string) {
	mc.eventProcessingDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("event.type", eventType),
		),
	)
}

// RecordBatchSize records the size of an event batch
func (mc *MetricsCollector) RecordBatchSize(ctx context.Context, size int, collectorID string) {
	mc.batchSize.Record(ctx, int64(size),
		metric.WithAttributes(
			attribute.String("collector.id", collectorID),
		),
	)
}

// RecordCorrelationFound records a found correlation with confidence
func (mc *MetricsCollector) RecordCorrelationFound(ctx context.Context, confidence float64, patternType string) {
	mc.correlationsFound.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("pattern.type", patternType),
		),
	)
	mc.correlationConfidence.Record(ctx, confidence,
		metric.WithAttributes(
			attribute.String("pattern.type", patternType),
		),
	)
}

// RecordError records an error with type and component
func (mc *MetricsCollector) RecordError(ctx context.Context, errorType string, component string) {
	mc.errorCount.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("error.type", errorType),
			attribute.String("component", component),
		),
	)
}

// UpdateActiveSemanticGroups updates the count of active semantic groups
func (mc *MetricsCollector) UpdateActiveSemanticGroups(ctx context.Context, delta int64) {
	mc.activeSemanticGroups.Add(ctx, delta)
}

// MeasureOperation provides a helper to measure operation duration
func (mc *MetricsCollector) MeasureOperation(ctx context.Context, operationName string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		mc.eventProcessingDuration.Record(ctx, duration.Seconds(),
			metric.WithAttributes(
				attribute.String("operation", operationName),
			),
		)
	}
}

// Shutdown unregisters all callbacks
func (mc *MetricsCollector) Shutdown() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for _, reg := range mc.observableCallbacks {
		if err := reg.Unregister(); err != nil {
			return err
		}
	}
	mc.observableCallbacks = nil
	return nil
}
