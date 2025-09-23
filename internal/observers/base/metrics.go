package base

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// initializeMetrics registers standard OTEL metrics for all observers
func (bc *BaseObserver) initializeMetrics() {
	var err error

	// Events processed counter
	bc.eventsProcessedCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", bc.name),
		metric.WithDescription("Total events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		// Log but don't fail - metrics are optional
		if bc.logger != nil {
			bc.logger.Debug("Failed to create events processed counter",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.eventsProcessedCounter = nil
	}

	// Events dropped counter
	bc.eventsDroppedCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", bc.name),
		metric.WithDescription("Total events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create events dropped counter",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.eventsDroppedCounter = nil
	}

	// Events filtered counter
	bc.eventsFilteredCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_filtered_total", bc.name),
		metric.WithDescription("Total events filtered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create events filtered counter",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.eventsFilteredCounter = nil
	}

	// Error counter
	bc.errorCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", bc.name),
		metric.WithDescription("Total errors encountered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create error counter",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.errorCounter = nil
	}

	// Processing duration histogram
	bc.processingDuration, err = bc.meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_seconds", bc.name),
		metric.WithDescription("Event processing duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create processing duration histogram",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.processingDuration = nil
	}

	// Event size histogram
	bc.eventSizeHistogram, err = bc.meter.Int64Histogram(
		fmt.Sprintf("%s_event_size_bytes", bc.name),
		metric.WithDescription("Event size distribution"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 500, 1000, 5000, 10000, 50000, 100000),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create event size histogram",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.eventSizeHistogram = nil
	}

	// Health status gauge (0=unhealthy, 1=degraded, 2=healthy)
	bc.healthStatus, err = bc.meter.Int64Gauge(
		fmt.Sprintf("%s_health_status", bc.name),
		metric.WithDescription("Health status (0=unhealthy, 1=degraded, 2=healthy)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		if bc.logger != nil {
			bc.logger.Debug("Failed to create health status gauge",
				zap.String("observer", bc.name),
				zap.Error(err))
		}
		bc.healthStatus = nil
	}
}

// RecordEvent should be called when an event is successfully processed
func (bc *BaseObserver) RecordEvent() {
	bc.eventsProcessed.Add(1)
	bc.lastEventTime.Store(time.Now())

	// Update OTEL metric if available
	if bc.eventsProcessedCounter != nil {
		bc.eventsProcessedCounter.Add(context.Background(), 1)
	}
}

// RecordEventWithContext records an event with trace context
func (bc *BaseObserver) RecordEventWithContext(ctx context.Context) {
	bc.eventsProcessed.Add(1)
	bc.lastEventTime.Store(time.Now())

	// Update OTEL metric with context for trace correlation
	if bc.eventsProcessedCounter != nil {
		bc.eventsProcessedCounter.Add(ctx, 1)
	}
}

// RecordError should be called when an error occurs
func (bc *BaseObserver) RecordError(err error) {
	bc.errorCount.Add(1)
	if err != nil {
		bc.lastError.Store(err)
	}

	// Update OTEL metric if available
	if bc.errorCounter != nil {
		attrs := []attribute.KeyValue{}
		if err != nil {
			attrs = append(attrs, attribute.String("error_type", fmt.Sprintf("%T", err)))
		}
		bc.errorCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	}
}

// RecordErrorWithContext records an error with trace context
func (bc *BaseObserver) RecordErrorWithContext(ctx context.Context, err error) {
	bc.errorCount.Add(1)
	if err != nil {
		bc.lastError.Store(err)
	}

	// Record error in span if tracing
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.RecordError(err)
	}

	// Update OTEL metric with context
	if bc.errorCounter != nil {
		attrs := []attribute.KeyValue{}
		if err != nil {
			attrs = append(attrs, attribute.String("error_type", fmt.Sprintf("%T", err)))
		}
		bc.errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordDrop should be called when an event is dropped
func (bc *BaseObserver) RecordDrop() {
	bc.eventsDropped.Add(1)

	// Update OTEL metric if available
	if bc.eventsDroppedCounter != nil {
		bc.eventsDroppedCounter.Add(context.Background(), 1)
	}
}

// RecordDropWithReason records a dropped event with a reason
func (bc *BaseObserver) RecordDropWithReason(ctx context.Context, reason string) {
	bc.eventsDropped.Add(1)

	// Update OTEL metric with reason
	if bc.eventsDroppedCounter != nil {
		bc.eventsDroppedCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.String("reason", reason)))
	}
}

// RecordProcessingDuration records the time taken to process an event
func (bc *BaseObserver) RecordProcessingDuration(ctx context.Context, duration time.Duration) {
	if bc.processingDuration != nil {
		bc.processingDuration.Record(ctx, duration.Seconds())
	}
}

// RecordEventSize records the size of an event in bytes
func (bc *BaseObserver) RecordEventSize(ctx context.Context, sizeBytes int64) {
	if bc.eventSizeHistogram != nil {
		bc.eventSizeHistogram.Record(ctx, sizeBytes)
	}
}

// RecordFilteredEvent records that an event was filtered
func (bc *BaseObserver) RecordFilteredEvent(event *domain.CollectorEvent) {
	if bc.eventsFilteredCounter != nil {
		ctx := context.Background()
		bc.eventsFilteredCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
				attribute.String("severity", string(event.Severity)),
			))
	}
}

// StartSpan starts a new span for event processing
func (bc *BaseObserver) StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return bc.tracer.Start(ctx, spanName, opts...)
}

// GetTracer returns the tracer for custom instrumentation
func (bc *BaseObserver) GetTracer() trace.Tracer {
	return bc.tracer
}

// GetMeter returns the meter for custom metrics
func (bc *BaseObserver) GetMeter() metric.Meter {
	return bc.meter
}
