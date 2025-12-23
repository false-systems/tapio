//go:build linux

package base

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// registryMetrics caches metrics per registry to avoid duplicate registration
var (
	registryMetrics   = make(map[prometheus.Registerer]*PromObserverMetrics)
	registryMetricsMu sync.Mutex
)

// PromObserverMetrics holds native Prometheus metrics for observer telemetry.
// This replaces ObserverMetrics (OTEL SDK) with zero-allocation counter increments.
type PromObserverMetrics struct {
	// Event counters
	EventsProcessed *prometheus.CounterVec
	EventsDropped   *prometheus.CounterVec
	ErrorsTotal     *prometheus.CounterVec
	ProcessingTime  *prometheus.HistogramVec

	// Pipeline health metrics
	PipelineStagesActive     *prometheus.GaugeVec
	PipelineStagesFailed     *prometheus.CounterVec
	PipelineQueueDepth       *prometheus.GaugeVec
	PipelineQueueUtilization *prometheus.GaugeVec

	// Data quality metrics
	EventsOutOfOrder       *prometheus.CounterVec
	EventsDuplicate        *prometheus.CounterVec
	EventsEnrichmentFailed *prometheus.CounterVec

	// eBPF health metrics
	EBPFMapSize               *prometheus.GaugeVec
	EBPFMapCapacity           *prometheus.GaugeVec
	EBPFRingBufferLost        *prometheus.CounterVec
	EBPFRingBufferUtilization *prometheus.GaugeVec
}

// NewPromObserverMetrics creates native Prometheus metrics with registry injection.
// Following Cortex/Mimir pattern: promauto.With(reg)
// Caches metrics per registry to avoid duplicate registration errors.
func NewPromObserverMetrics(reg prometheus.Registerer) *PromObserverMetrics {
	registryMetricsMu.Lock()
	defer registryMetricsMu.Unlock()

	// Return cached metrics if they exist for this registry
	if m, ok := registryMetrics[reg]; ok {
		return m
	}

	m := createPromObserverMetrics(reg)
	registryMetrics[reg] = m
	return m
}

// registerVec registers a collector or returns existing one if already registered
func registerVec[T prometheus.Collector](reg prometheus.Registerer, collector T) T {
	if err := reg.Register(collector); err != nil {
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegErr) {
			if existing, ok := alreadyRegErr.ExistingCollector.(T); ok {
				return existing
			}
		}
	}
	return collector
}

// createPromObserverMetrics creates the actual metrics
func createPromObserverMetrics(reg prometheus.Registerer) *PromObserverMetrics {
	return &PromObserverMetrics{
		EventsProcessed: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_processed_total",
				Help: "Total number of events processed by observer",
			},
			[]string{"observer", "event_type", "domain"},
		)),
		EventsDropped: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_dropped_total",
				Help: "Total number of events dropped by observer",
			},
			[]string{"observer", "event_type", "domain"},
		)),
		ErrorsTotal: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_errors_total",
				Help: "Total number of errors in observer",
			},
			[]string{"observer", "event_type", "domain", "error_type"},
		)),
		ProcessingTime: registerVec(reg, prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_observer_processing_duration_ms",
				Help:    "Processing duration for observer events in milliseconds",
				Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 25, 50, 100},
			},
			[]string{"observer", "event_type", "domain"},
		)),
		PipelineStagesActive: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_stages_active",
				Help: "Current number of active pipeline stages",
			},
			[]string{"observer"},
		)),
		PipelineStagesFailed: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_pipeline_stages_failed_total",
				Help: "Total number of pipeline stage failures",
			},
			[]string{"observer", "stage"},
		)),
		PipelineQueueDepth: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_queue_depth",
				Help: "Current depth of observer pipeline work queue",
			},
			[]string{"observer"},
		)),
		PipelineQueueUtilization: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_queue_utilization_ratio",
				Help: "Pipeline queue utilization (0.0-1.0)",
			},
			[]string{"observer"},
		)),
		EventsOutOfOrder: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_out_of_order_total",
				Help: "Events rejected due to out-of-order timestamps",
			},
			[]string{"observer", "event_type", "domain"},
		)),
		EventsDuplicate: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_duplicate_total",
				Help: "Duplicate events detected and dropped",
			},
			[]string{"observer", "event_type", "domain"},
		)),
		EventsEnrichmentFailed: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_enrichment_failed_total",
				Help: "Events failed to enrich with K8s metadata",
			},
			[]string{"observer", "enrichment_type"},
		)),
		EBPFMapSize: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_map_entries",
				Help: "Current number of entries in eBPF maps",
			},
			[]string{"observer"},
		)),
		EBPFMapCapacity: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_map_capacity",
				Help: "Maximum capacity of eBPF maps",
			},
			[]string{"observer"},
		)),
		EBPFRingBufferLost: registerVec(reg, prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_ebpf_ringbuffer_lost_total",
				Help: "Total events lost due to ring buffer overflow",
			},
			[]string{"observer"},
		)),
		EBPFRingBufferUtilization: registerVec(reg, prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_ringbuffer_utilization_ratio",
				Help: "eBPF ring buffer utilization (0.0-1.0)",
			},
			[]string{"observer"},
		)),
	}
}

// RecordEvent records a successfully processed event - ZERO ALLOCATION
func (m *PromObserverMetrics) RecordEvent(observerName, eventType string) {
	domain := EventDomainFromType(eventType)
	m.EventsProcessed.WithLabelValues(observerName, eventType, domain).Inc()
}

// RecordDrop records a dropped event - ZERO ALLOCATION
func (m *PromObserverMetrics) RecordDrop(observerName, eventType string) {
	domain := EventDomainFromType(eventType)
	m.EventsDropped.WithLabelValues(observerName, eventType, domain).Inc()
}

// RecordError records an error - ZERO ALLOCATION
func (m *PromObserverMetrics) RecordError(observerName, eventType, errorType string) {
	domain := EventDomainFromType(eventType)
	m.ErrorsTotal.WithLabelValues(observerName, eventType, domain, errorType).Inc()
}

// RecordProcessingTime records processing duration in milliseconds
func (m *PromObserverMetrics) RecordProcessingTime(observerName, eventType string, durationMs float64) {
	domain := EventDomainFromType(eventType)
	m.ProcessingTime.WithLabelValues(observerName, eventType, domain).Observe(durationMs)
}

// RecordPipelineQueueDepth records the current queue depth
func (m *PromObserverMetrics) RecordPipelineQueueDepth(observerName string, depth int64) {
	m.PipelineQueueDepth.WithLabelValues(observerName).Set(float64(depth))
}

// RecordPipelineQueueUtilization records queue utilization (0.0-1.0)
func (m *PromObserverMetrics) RecordPipelineQueueUtilization(observerName string, utilization float64) {
	m.PipelineQueueUtilization.WithLabelValues(observerName).Set(utilization)
}

// RecordPipelineStagesActive records active pipeline stages
func (m *PromObserverMetrics) RecordPipelineStagesActive(observerName string, activeStages int64) {
	m.PipelineStagesActive.WithLabelValues(observerName).Set(float64(activeStages))
}

// RecordPipelineStageFailed records a pipeline stage failure
func (m *PromObserverMetrics) RecordPipelineStageFailed(observerName, stageName string) {
	m.PipelineStagesFailed.WithLabelValues(observerName, stageName).Inc()
}

// RecordEventOutOfOrder records an out-of-order event
func (m *PromObserverMetrics) RecordEventOutOfOrder(observerName, eventType string) {
	domain := EventDomainFromType(eventType)
	m.EventsOutOfOrder.WithLabelValues(observerName, eventType, domain).Inc()
}

// RecordEventDuplicate records a duplicate event
func (m *PromObserverMetrics) RecordEventDuplicate(observerName, eventType string) {
	domain := EventDomainFromType(eventType)
	m.EventsDuplicate.WithLabelValues(observerName, eventType, domain).Inc()
}

// RecordEnrichmentFailed records a failed enrichment
func (m *PromObserverMetrics) RecordEnrichmentFailed(observerName, enrichmentType string) {
	m.EventsEnrichmentFailed.WithLabelValues(observerName, enrichmentType).Inc()
}

// RecordEBPFMapSize records eBPF map size
func (m *PromObserverMetrics) RecordEBPFMapSize(observerName string, size int64) {
	m.EBPFMapSize.WithLabelValues(observerName).Set(float64(size))
}

// RecordEBPFMapCapacity records eBPF map capacity
func (m *PromObserverMetrics) RecordEBPFMapCapacity(observerName string, capacity int64) {
	m.EBPFMapCapacity.WithLabelValues(observerName).Set(float64(capacity))
}

// RecordEBPFRingBufferLost records lost ring buffer events
func (m *PromObserverMetrics) RecordEBPFRingBufferLost(observerName string, count int64) {
	m.EBPFRingBufferLost.WithLabelValues(observerName).Add(float64(count))
}

// RecordEBPFRingBufferUtilization records ring buffer utilization
func (m *PromObserverMetrics) RecordEBPFRingBufferUtilization(observerName string, utilization float64) {
	m.EBPFRingBufferUtilization.WithLabelValues(observerName).Set(utilization)
}

// EventDomainFromType extracts the domain from an event type string.
// E.g., "network.connection" -> "network", "container.oom" -> "container"
func EventDomainFromType(eventType string) string {
	for i, c := range eventType {
		if c == '.' {
			return eventType[:i]
		}
	}
	return eventType
}
