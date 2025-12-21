# TAPIO Telemetry Migration Plan

**Goal**: Migrate observer metrics from OTEL SDK to native Prometheus client

**Why**: OTEL SDK counter increments are 4-26x slower than native Prometheus in Go. For an eBPF agent processing thousands of kernel events per second, this matters.

**Reference**: Julius Volz article on OTEL vs native Prometheus instrumentation

---

## Current State

| Component | Current | Target |
|-----------|---------|--------|
| `internal/base/metrics.go` | OTEL SDK | Native Prometheus |
| `internal/base/metrics_builder.go` | OTEL SDK | Native Prometheus |
| `internal/base/telemetry.go` | OTEL meter provider | Remove meter provider |
| `internal/observers/*` | Uses OTEL metrics | Use Prometheus metrics |
| `internal/runtime/supervisor` | OTEL SDK | Native Prometheus |
| `pkg/intelligence/metrics.go` | Native Prometheus | Keep as-is (already correct) |

---

## Phase 1: Create Central Registry

**File**: `internal/base/registry.go` (NEW)

```go
package base

import "github.com/prometheus/client_golang/prometheus"

// GlobalRegistry is the central Prometheus registry for all TAPIO metrics.
// Following Cortex/Mimir pattern of registry injection.
var GlobalRegistry = prometheus.NewRegistry()

func init() {
	// Register default collectors for Go runtime and process metrics
	GlobalRegistry.MustRegister(prometheus.NewGoCollector())
	GlobalRegistry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
}
```

---

## Phase 2: Create Prometheus Observer Metrics

**File**: `internal/base/prom_metrics.go` (NEW)

```go
package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
func NewPromObserverMetrics(reg prometheus.Registerer) *PromObserverMetrics {
	return &PromObserverMetrics{
		EventsProcessed: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_processed_total",
				Help: "Total number of events processed by observer",
			},
			[]string{"observer", "event_type", "domain"},
		),
		EventsDropped: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_dropped_total",
				Help: "Total number of events dropped by observer",
			},
			[]string{"observer", "event_type", "domain"},
		),
		ErrorsTotal: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_errors_total",
				Help: "Total number of errors in observer",
			},
			[]string{"observer", "event_type", "domain", "error_type"},
		),
		ProcessingTime: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_observer_processing_duration_ms",
				Help:    "Processing duration for observer events in milliseconds",
				Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 25, 50, 100},
			},
			[]string{"observer", "event_type", "domain"},
		),
		PipelineStagesActive: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_stages_active",
				Help: "Current number of active pipeline stages",
			},
			[]string{"observer"},
		),
		PipelineStagesFailed: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_pipeline_stages_failed_total",
				Help: "Total number of pipeline stage failures",
			},
			[]string{"observer", "stage"},
		),
		PipelineQueueDepth: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_queue_depth",
				Help: "Current depth of observer pipeline work queue",
			},
			[]string{"observer"},
		),
		PipelineQueueUtilization: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_pipeline_queue_utilization_ratio",
				Help: "Pipeline queue utilization (0.0-1.0)",
			},
			[]string{"observer"},
		),
		EventsOutOfOrder: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_out_of_order_total",
				Help: "Events rejected due to out-of-order timestamps",
			},
			[]string{"observer", "event_type", "domain"},
		),
		EventsDuplicate: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_duplicate_total",
				Help: "Duplicate events detected and dropped",
			},
			[]string{"observer", "event_type", "domain"},
		),
		EventsEnrichmentFailed: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_events_enrichment_failed_total",
				Help: "Events failed to enrich with K8s metadata",
			},
			[]string{"observer", "enrichment_type"},
		),
		EBPFMapSize: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_map_entries",
				Help: "Current number of entries in eBPF maps",
			},
			[]string{"observer"},
		),
		EBPFMapCapacity: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_map_capacity",
				Help: "Maximum capacity of eBPF maps",
			},
			[]string{"observer"},
		),
		EBPFRingBufferLost: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_observer_ebpf_ringbuffer_lost_total",
				Help: "Total events lost due to ring buffer overflow",
			},
			[]string{"observer"},
		),
		EBPFRingBufferUtilization: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_observer_ebpf_ringbuffer_utilization_ratio",
				Help: "eBPF ring buffer utilization (0.0-1.0)",
			},
			[]string{"observer"},
		),
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
```

---

## Phase 3: Create Prometheus Metric Builder

**File**: `internal/base/prom_metrics_builder.go` (NEW)

```go
package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PromMetricBuilder provides fluent API for creating observer-specific Prometheus metrics.
// Replaces MetricBuilder (OTEL SDK) with native Prometheus.
type PromMetricBuilder struct {
	reg          prometheus.Registerer
	observerName string
	err          error
}

// NewPromMetricBuilder creates a builder for observer-specific metrics
func NewPromMetricBuilder(reg prometheus.Registerer, observerName string) *PromMetricBuilder {
	return &PromMetricBuilder{
		reg:          reg,
		observerName: observerName,
	}
}

// Counter creates a counter metric
func (b *PromMetricBuilder) Counter(target **prometheus.Counter, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	counter := promauto.With(b.reg).NewCounter(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	})
	*target = &counter
	return b
}

// CounterVec creates a counter vector metric
func (b *PromMetricBuilder) CounterVec(target **prometheus.CounterVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	*target = promauto.With(b.reg).NewCounterVec(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	}, labels)
	return b
}

// Gauge creates a gauge metric
func (b *PromMetricBuilder) Gauge(target **prometheus.Gauge, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	gauge := promauto.With(b.reg).NewGauge(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	})
	*target = &gauge
	return b
}

// GaugeVec creates a gauge vector metric
func (b *PromMetricBuilder) GaugeVec(target **prometheus.GaugeVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	*target = promauto.With(b.reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	}, labels)
	return b
}

// Histogram creates a histogram metric
func (b *PromMetricBuilder) Histogram(target **prometheus.Histogram, name, help string, buckets []float64) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	histogram := promauto.With(b.reg).NewHistogram(prometheus.HistogramOpts{
		Name:    fullName,
		Help:    help,
		Buckets: buckets,
	})
	*target = &histogram
	return b
}

// Build returns any error that occurred during metric creation
func (b *PromMetricBuilder) Build() error {
	return b.err
}
```

---

## Phase 4: Update BaseObserver

**File**: `internal/base/observer.go`

**Changes**:
1. Replace `*ObserverMetrics` with `*PromObserverMetrics`
2. Update `NewBaseObserver` to accept registry
3. Update `RecordEvent`, `RecordError`, etc. to use new metrics

```go
// Before
type BaseObserver struct {
	name    string
	metrics *ObserverMetrics
	// ...
}

func NewBaseObserver(name string) (*BaseObserver, error) {
	metrics, err := NewObserverMetrics(name)
	// ...
}

// After
type BaseObserver struct {
	name    string
	metrics *PromObserverMetrics
	// ...
}

func NewBaseObserver(name string, reg prometheus.Registerer) (*BaseObserver, error) {
	if reg == nil {
		reg = GlobalRegistry
	}
	metrics := NewPromObserverMetrics(reg)
	// ...
}

// Update RecordEvent to not need context (Prometheus doesn't use it)
func (o *BaseObserver) RecordEvent() {
	// Get event type from current event if available
	o.metrics.RecordEvent(o.name, o.currentEventType)
}
```

---

## Phase 5: Update All Observers

Each observer needs signature updates. Example for network:

**File**: `internal/observers/network/observer.go`

```go
// Before
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name)

// After
func NewNetworkObserver(name string, config Config, reg prometheus.Registerer) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name, reg)
```

**Observers to update**:
- `internal/observers/network/observer.go`
- `internal/observers/container/observer.go`
- `internal/observers/container-api/observer.go`
- `internal/observers/container-runtime/observer.go`
- `internal/observers/node/observer.go`
- `internal/observers/scheduler/observer.go`
- `internal/observers/deployments/observer.go`

---

## Phase 6: Update Supervisor

**File**: `internal/runtime/supervisor/supervisor.go`

Same pattern - replace OTEL metrics with Prometheus, accept registry.

---

## Phase 7: Simplify Telemetry Init

**File**: `internal/base/telemetry.go`

**Remove**:
- Meter provider setup
- OTLP metric exporter
- `go.opentelemetry.io/otel/exporters/prometheus` (OTEL bridge)

**Keep**:
- Trace provider (OTEL)
- Logger provider (OTEL)
- Prometheus HTTP server (but use native handler)

```go
// Before
mux.Handle("/metrics", promhttp.Handler())  // Uses OTEL bridge

// After
mux.Handle("/metrics", promhttp.HandlerFor(
	GlobalRegistry,
	promhttp.HandlerOpts{EnableOpenMetrics: true},
))
```

---

## Phase 8: Update Main

**File**: `cmd/tapio/main.go`

Pass registry to observer constructors:

```go
// Before
networkObs, err := network.NewNetworkObserver("network", networkConfig)

// After
networkObs, err := network.NewNetworkObserver("network", networkConfig, base.GlobalRegistry)
```

---

## Phase 9: Clean Up

**Remove files**:
- `internal/base/metrics.go` (old OTEL metrics)
- `internal/base/metrics_builder.go` (old OTEL builder)

**Remove imports**:
- `go.opentelemetry.io/otel/metric`
- `go.opentelemetry.io/otel/exporters/prometheus`
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`

**Update go.mod**:
```bash
go mod tidy
```

---

## Verification Checklist

- [ ] All observers use `PromObserverMetrics`
- [ ] No OTEL metric imports in observer code
- [ ] `/metrics` endpoint returns Prometheus format
- [ ] `go test ./...` passes
- [ ] `golangci-lint run` passes
- [ ] Metrics visible in Prometheus (if running locally)

---

## Estimated Effort

| Phase | Files | Effort |
|-------|-------|--------|
| 1. Registry | 1 new | Small |
| 2. PromObserverMetrics | 1 new | Medium |
| 3. PromMetricBuilder | 1 new | Small |
| 4. BaseObserver | 1 modify | Medium |
| 5. All observers | 7 modify | Large |
| 6. Supervisor | 1 modify | Small |
| 7. Telemetry init | 1 modify | Small |
| 8. Main | 1 modify | Small |
| 9. Cleanup | 2 delete | Small |
| **Total** | ~15 files | ~800 lines |

---

## What to Keep (OTEL)

- **Traces**: Keep OTEL trace provider and OTLP exporter
- **Logs**: Keep OTEL logger provider (or migrate to slog later)
- **Propagation**: Keep context propagation for distributed tracing

---

**False Systems** 🇫🇮
