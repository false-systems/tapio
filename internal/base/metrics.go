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
	// Existing - Event counters
	EventsProcessed metric.Int64Counter
	EventsDropped   metric.Int64Counter
	ErrorsTotal     metric.Int64Counter
	ProcessingTime  metric.Float64Histogram

	// NEW - Pipeline health metrics (Prometheus pattern)
	PipelineStagesActive     metric.Int64Gauge   // Current running stages
	PipelineStagesFailed     metric.Int64Counter // Total stage failures
	PipelineQueueDepth       metric.Int64Gauge   // Work queue depth
	PipelineQueueUtilization metric.Float64Gauge // Queue utilization (0.0-1.0)

	// NEW - Data quality metrics (Prometheus pattern)
	EventsOutOfOrder       metric.Int64Counter // Timestamp issues
	EventsDuplicate        metric.Int64Counter // Duplicate detection
	EventsEnrichmentFailed metric.Int64Counter // K8s lookup failures

	// NEW - eBPF health metrics (optional, only for eBPF observers)
	EBPFMapSize               metric.Int64Gauge   // Current entries
	EBPFMapCapacity           metric.Int64Gauge   // Max entries
	EBPFRingBufferLost        metric.Int64Counter // Lost events
	EBPFRingBufferUtilization metric.Float64Gauge // Buffer % (0.0-1.0)
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

	// Pipeline health metrics
	pipelineStagesActive, err := meter.Int64Gauge(
		"observer_pipeline_stages_active",
		metric.WithDescription("Current number of active pipeline stages"),
		metric.WithUnit("{stages}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline_stages_active gauge: %w", err)
	}

	pipelineStagesFailed, err := meter.Int64Counter(
		"observer_pipeline_stages_failed_total",
		metric.WithDescription("Total number of pipeline stage failures"),
		metric.WithUnit("{failures}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline_stages_failed counter: %w", err)
	}

	pipelineQueueDepth, err := meter.Int64Gauge(
		"observer_pipeline_queue_depth",
		metric.WithDescription("Current depth of observer pipeline work queue"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline_queue_depth gauge: %w", err)
	}

	pipelineQueueUtilization, err := meter.Float64Gauge(
		"observer_pipeline_queue_utilization_ratio",
		metric.WithDescription("Pipeline queue utilization (0.0-1.0)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline_queue_utilization gauge: %w", err)
	}

	// Data quality metrics
	eventsOutOfOrder, err := meter.Int64Counter(
		"observer_events_out_of_order_total",
		metric.WithDescription("Events rejected due to out-of-order timestamps"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_out_of_order counter: %w", err)
	}

	eventsDuplicate, err := meter.Int64Counter(
		"observer_events_duplicate_total",
		metric.WithDescription("Duplicate events detected and dropped"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_duplicate counter: %w", err)
	}

	eventsEnrichmentFailed, err := meter.Int64Counter(
		"observer_events_enrichment_failed_total",
		metric.WithDescription("Events failed to enrich with K8s metadata"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_enrichment_failed counter: %w", err)
	}

	// eBPF health metrics
	ebpfMapSize, err := meter.Int64Gauge(
		"observer_ebpf_map_entries",
		metric.WithDescription("Current number of entries in eBPF maps"),
		metric.WithUnit("{entries}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_map_entries gauge: %w", err)
	}

	ebpfMapCapacity, err := meter.Int64Gauge(
		"observer_ebpf_map_capacity",
		metric.WithDescription("Maximum capacity of eBPF maps"),
		metric.WithUnit("{entries}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_map_capacity gauge: %w", err)
	}

	ebpfRingBufferLost, err := meter.Int64Counter(
		"observer_ebpf_ringbuffer_lost_total",
		metric.WithDescription("Total events lost due to ring buffer overflow"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_ringbuffer_lost counter: %w", err)
	}

	ebpfRingBufferUtilization, err := meter.Float64Gauge(
		"observer_ebpf_ringbuffer_utilization_ratio",
		metric.WithDescription("eBPF ring buffer utilization (0.0-1.0)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_ringbuffer_utilization gauge: %w", err)
	}

	return &ObserverMetrics{
		EventsProcessed:           eventsProcessed,
		EventsDropped:             eventsDropped,
		ErrorsTotal:               errorsTotal,
		ProcessingTime:            processingTime,
		PipelineStagesActive:      pipelineStagesActive,
		PipelineStagesFailed:      pipelineStagesFailed,
		PipelineQueueDepth:        pipelineQueueDepth,
		PipelineQueueUtilization:  pipelineQueueUtilization,
		EventsOutOfOrder:          eventsOutOfOrder,
		EventsDuplicate:           eventsDuplicate,
		EventsEnrichmentFailed:    eventsEnrichmentFailed,
		EBPFMapSize:               ebpfMapSize,
		EBPFMapCapacity:           ebpfMapCapacity,
		EBPFRingBufferLost:        ebpfRingBufferLost,
		EBPFRingBufferUtilization: ebpfRingBufferUtilization,
	}, nil
}

// RecordEvent records a successfully processed event with OTEL semantic conventions
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event.type", event.Type),
			EventDomainAttribute(event.Type),
		)
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
// Exemplars (trace_id, span_id) are attached automatically by the OTel SDK when a valid span
// is present in ctx and the metric exporter supports exemplars (e.g., Prometheus, OTLP)
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

	// Record with metric attributes only; exemplars are attached automatically by OTel SDK/exporter
	// when ctx contains a valid span. This avoids cardinality explosion from adding trace_id/span_id
	// as metric attributes (which would create a new time series per span).
	m.ProcessingTime.Record(ctx, durationMs, metric.WithAttributes(attrs...))
}

// RecordPipelineQueueDepth records the current depth of the observer's work queue
func (m *ObserverMetrics) RecordPipelineQueueDepth(ctx context.Context, observerName string, depth int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.PipelineQueueDepth.Record(ctx, depth, metric.WithAttributes(attrs...))
}

// RecordPipelineQueueUtilization records the queue utilization ratio (0.0-1.0)
func (m *ObserverMetrics) RecordPipelineQueueUtilization(ctx context.Context, observerName string, utilization float64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.PipelineQueueUtilization.Record(ctx, utilization, metric.WithAttributes(attrs...))
}

// RecordPipelineStagesActive records the current number of active pipeline stages
func (m *ObserverMetrics) RecordPipelineStagesActive(ctx context.Context, observerName string, activeStages int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.PipelineStagesActive.Record(ctx, activeStages, metric.WithAttributes(attrs...))
}

// RecordPipelineStageFailed records a pipeline stage failure
func (m *ObserverMetrics) RecordPipelineStageFailed(ctx context.Context, observerName string, stageName string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("stage.name", stageName),
	}
	m.PipelineStagesFailed.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordEventOutOfOrder records an event rejected due to out-of-order timestamp
func (m *ObserverMetrics) RecordEventOutOfOrder(ctx context.Context, observerName string, eventType string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("event.type", eventType),
		EventDomainAttribute(eventType),
	}
	m.EventsOutOfOrder.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordEventDuplicate records a duplicate event detection
func (m *ObserverMetrics) RecordEventDuplicate(ctx context.Context, observerName string, eventType string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("event.type", eventType),
		EventDomainAttribute(eventType),
	}
	m.EventsDuplicate.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordEnrichmentFailed records a failed event enrichment attempt
func (m *ObserverMetrics) RecordEnrichmentFailed(ctx context.Context, observerName string, enrichmentType string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
		attribute.String("enrichment.type", enrichmentType),
	}
	m.EventsEnrichmentFailed.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordEBPFMapSize records the current number of entries in eBPF maps
func (m *ObserverMetrics) RecordEBPFMapSize(ctx context.Context, observerName string, size int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.EBPFMapSize.Record(ctx, size, metric.WithAttributes(attrs...))
}

// RecordEBPFMapCapacity records the maximum capacity of eBPF maps
func (m *ObserverMetrics) RecordEBPFMapCapacity(ctx context.Context, observerName string, capacity int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.EBPFMapCapacity.Record(ctx, capacity, metric.WithAttributes(attrs...))
}

// RecordEBPFRingBufferLost records events lost due to ring buffer overflow
func (m *ObserverMetrics) RecordEBPFRingBufferLost(ctx context.Context, observerName string, count int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.EBPFRingBufferLost.Add(ctx, count, metric.WithAttributes(attrs...))
}

// RecordEBPFRingBufferUtilization records the eBPF ring buffer utilization (0.0-1.0)
func (m *ObserverMetrics) RecordEBPFRingBufferUtilization(ctx context.Context, observerName string, utilization float64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}
	m.EBPFRingBufferUtilization.Record(ctx, utilization, metric.WithAttributes(attrs...))
}
