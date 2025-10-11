# 🔍 TAPIO OTEL CURRENT STATE AUDIT

**Date**: 2025-10-11
**Branch**: `feat/otel-observability-redesign`
**Auditor**: Datner

---

## 📊 CURRENT METRICS IMPLEMENTATION

### Metric Names (internal/base/metrics.go)

```go
// Current metric names
observer_events_processed_total  // Counter
observer_events_dropped_total    // Counter
observer_errors_total            // Counter
observer_processing_duration_ms  // Histogram
```

### Current Recording Methods

```go
// internal/base/metrics.go:68-85
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string)
func (m *ObserverMetrics) RecordDrop(ctx context.Context, observerName string)
func (m *ObserverMetrics) RecordError(ctx context.Context, observerName string)
func (m *ObserverMetrics) RecordProcessingTime(ctx context.Context, observerName string, durationMs float64)
```

**Problem**: `observerName` parameter is passed but **NEVER USED**. All metrics recorded with **EMPTY attributes**:
```go
m.EventsProcessed.Add(ctx, 1, metric.WithAttributes())  // ❌ No attributes!
```

### Current BaseObserver Methods

```go
// internal/base/observer.go:104-124
func (b *BaseObserver) RecordEvent(ctx context.Context)
func (b *BaseObserver) RecordDrop(ctx context.Context)
func (b *BaseObserver) RecordError(ctx context.Context)
func (b *BaseObserver) RecordProcessingTime(ctx context.Context, durationMs float64)
```

**Problem**: Methods don't accept `eventType` or `eventDomain` parameters. Can't track which events are being processed.

---

## ❌ CRITICAL GAPS IDENTIFIED

### Gap 1: No Metric Attributes
**Current**:
```go
m.EventsProcessed.Add(ctx, 1, metric.WithAttributes())
```

**Cannot Query**:
- ❌ Events by type: `rate(observer_events_processed_total{event_type="tcp_connect"}[5m])`
- ❌ Events by domain: `sum by (event_domain) (observer_events_processed_total)`
- ❌ Events by observer: `rate(observer_events_processed_total{observer="network"}[5m])`

**Impact**: Metrics are useless for debugging. Can only see total events/sec, not WHICH events.

### Gap 2: No Semantic Conventions
**Current Attributes** (if we had any):
```go
// What we would do now:
attribute.String("src_ip", "10.0.1.5")
attribute.String("dst_ip", "10.0.2.8")
attribute.Int("src_port", 44320)
attribute.Int("dst_port", 80)
```

**Should Be** (OTEL semantic conventions):
```go
semconv.NetworkPeerAddress("10.0.2.8")
semconv.NetworkPeerPort(80)
semconv.NetworkLocalAddress("10.0.1.5")
semconv.NetworkLocalPort(44320)
semconv.NetworkProtocolName("tcp")
```

**Impact**: Grafana dashboards won't work with our metrics. No standard queries.

### Gap 3: No Resource Attributes
**Current**: No resource configuration at all.

**Missing**:
```go
semconv.K8SClusterName("prod-us-east")
semconv.K8SNamespaceName("payments")
semconv.K8SNodeName("node-7")
semconv.ServiceName("tapio-agent")
semconv.ServiceVersion("v1.0.0")
```

**Impact**: Can't filter metrics by cluster, namespace, or node in Grafana.

### Gap 4: Wrong Histogram Buckets
**Current**:
```go
processingTime, err := meter.Float64Histogram(
    "observer_processing_duration_ms",
    metric.WithDescription("Processing duration for observer events"),
    metric.WithUnit("ms"),
    // ❌ No explicit buckets! Using defaults
)
```

**Default Buckets** (likely): `[0, 5, 10, 25, 50, 75, 100, 250, 500, 1000, 2500, 5000, 7500, 10000]`

**Problem**:
- eBPF processing: ~0.1-1ms (need finer buckets)
- K8s API calls: ~50-500ms (default buckets OK)
- Network parsing: ~0.5-5ms (need finer buckets)

**Should Be**:
```go
metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000, 5000)
```

### Gap 5: No Context Propagation
**Current**: No trace context in `ObserverEvent` struct.

**Missing from pkg/domain/events.go**:
```go
type ObserverEvent struct {
    // ... existing fields ...

    // ❌ MISSING: Trace context for correlation
    // TraceID    string `json:"trace_id,omitempty"`
    // SpanID     string `json:"span_id,omitempty"`
    // TraceFlags byte   `json:"trace_flags,omitempty"`
}
```

**Impact**: Events across observers aren't correlated in Jaeger. Can't see: Network Observer → Process Observer → K8s Observer traces.

### Gap 6: OTELEmitter Creates Fake Spans
**Current**: `internal/base/emitter.go:73-112`

```go
func (e *OTELEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
    _, span := e.tracer.Start(ctx, event.Type,
        trace.WithTimestamp(event.Timestamp),
    )
    defer span.End()  // ❌ Ends immediately! Point-in-time span.

    // Add attributes...
    span.SetStatus(codes.Ok, "event emitted")
    return nil
}
```

**Problem**: Every event creates a span that starts and ends in microseconds. These aren't real "operations" - they're discrete events.

**Should Be**: Emit metrics + add span events to parent trace (if exists).

### Gap 7: No Cardinality Management
**Current**: No strategy for high-cardinality attributes.

**Will Crash Prometheus**:
```go
// ❌ Don't do this!
metric.WithAttributes(
    attribute.String("src_ip", "10.0.1.5"),      // Millions of unique IPs
    attribute.String("dst_ip", "10.0.2.8"),      // Millions of unique IPs
    attribute.String("pod_name", "nginx-abc123"), // Thousands of pods
)
```

**Should Do**:
```go
// ✅ Bounded cardinality
metric.WithAttributes(
    attribute.String("src_service", "frontend"),   // ~100 services
    attribute.String("dst_service", "backend-api"), // ~100 services
    attribute.String("deployment", "nginx"),        // ~500 deployments
)
```

---

## 📈 WHAT QUERIES WORK TODAY

### Working Query (but useless)
```promql
# Total events per second (all observers, all types)
rate(observer_events_processed_total[5m])
```

Result: `42.5` events/sec

**Problem**: Can't answer:
- Which observer is busiest?
- What event types are most common?
- Are network events increasing or decreasing?
- Which namespace generates most events?

### Queries That DON'T Work
```promql
# ❌ Events by type
rate(observer_events_processed_total{event_type="tcp_connect"}[5m])
# Error: label event_type not found

# ❌ Events by observer
rate(observer_events_processed_total{observer="network"}[5m])
# Error: label observer not found

# ❌ Events by domain
sum by (event_domain) (observer_events_processed_total)
# Error: label event_domain not found

# ❌ Filter by cluster
observer_events_processed_total{cluster="prod-us-east"}
# Error: label cluster not found
```

---

## 🎯 REQUIRED CHANGES (Priority Order)

### Priority 1: Add Metric Attributes (PHASE 2)
**Files**: `internal/base/metrics.go`, `internal/base/observer.go`

**Changes**:
1. Rewrite `RecordEvent()` to accept `eventType`, `eventDomain`
2. Rewrite `RecordError()` to accept `errorType`
3. Add attributes to all metric recordings
4. Update all observer implementations to pass event type

**Impact**: Enables all queries by type, domain, observer.

### Priority 2: Implement Semantic Conventions (PHASE 1)
**Files**: `internal/base/semconv.go` (NEW), `internal/base/resource.go` (NEW)

**Changes**:
1. Create semantic convention helpers
2. Replace custom attributes with OTEL standards
3. Configure resource attributes

**Impact**: Standard Grafana dashboards work immediately.

### Priority 3: Rewrite OTELEmitter (PHASE 3)
**Files**: `internal/base/emitter.go`

**Changes**:
1. Remove fake span creation
2. Emit proper metrics with attributes
3. Optionally attach span events to parent trace

**Impact**: No more meaningless spans in Jaeger.

### Priority 4: Context Propagation (PHASE 4)
**Files**: `pkg/domain/events.go`, `internal/base/observer.go`

**Changes**:
1. Add TraceID/SpanID to ObserverEvent
2. Add context helpers to BaseObserver
3. Propagate trace context through event pipeline

**Impact**: Distributed tracing across observers.

### Priority 5: Resource Configuration (PHASE 5)
**Files**: `cmd/agent/telemetry.go` (NEW), `cmd/agent/main.go`

**Changes**:
1. Create OTEL SDK initialization
2. Configure tracer provider, meter provider
3. Set resource attributes (cluster, namespace, node)

**Impact**: All metrics auto-tagged with K8s context.

---

## 📊 EXPECTED RESULTS AFTER CHANGES

### Queries That Will Work

```promql
# Events per second by type
rate(tapio_events_total{event_type="tcp_connect"}[5m])

# Events by observer
sum by (observer_name) (rate(tapio_events_total[5m]))

# Events by domain
sum by (event_domain) (rate(tapio_events_total[5m]))

# Error rate
rate(tapio_events_errors[5m]) / rate(tapio_events_total[5m])

# P99 latency by observer
histogram_quantile(0.99, rate(tapio_event_duration_bucket{observer="network"}[5m]))

# Filter by cluster + namespace
tapio_events_total{cluster="prod-us-east",namespace="payments"}
```

### Grafana Dashboards Enabled

**Observer Health Dashboard**:
- Total events/sec (gauge)
- Events/sec by type (stacked graph)
- Events/sec by observer (line graph)
- Error rate (gauge with threshold)
- P50/P95/P99 latency (graph)

**Network Traffic Dashboard**:
- Connections/sec by protocol
- Active connections by service
- Network bytes/sec
- Connection duration histogram

**Kubernetes Events Dashboard**:
- Pod events by action (created, updated, deleted)
- Deployment changes
- OOM kills
- Container restarts

---

## 🚀 NEXT STEPS

1. ✅ **COMPLETED**: Audit current state (this document)
2. **NEXT**: Phase 1 - Create semantic conventions (`internal/base/semconv.go`)
3. **THEN**: Phase 2 - Add attributes to metrics
4. **THEN**: Phase 3 - Rewrite OTELEmitter
5. **THEN**: Phase 4 - Context propagation
6. **THEN**: Phase 5 - Agent initialization
7. **THEN**: Phase 6 - Testing & validation

---

## 📝 SUMMARY

**Current State**: Metrics exist but are **unusable for observability**
- No attributes on any metrics
- No semantic conventions
- No resource configuration
- Fake point-in-time spans
- No context propagation

**After Redesign**: Production-grade OTEL observability
- Query by event type, domain, observer
- Standard Grafana dashboards work
- Distributed tracing across observers
- Auto-tagged with cluster/namespace/node
- SLO-based alerting enabled

**Effort**: 1-2 weeks (6 phases)
**Priority**: HIGH - Foundation for production observability
