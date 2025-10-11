# 🔭 TAPIO OPENTELEMETRY OBSERVABILITY REDESIGN

**Status**: Design Phase
**Priority**: HIGH - Foundation for production observability
**Estimated Effort**: 1-2 weeks
**Owner**: Engineering Team
**Created**: 2025-10-11

---

## 📋 EXECUTIVE SUMMARY

After deep analysis of OpenTelemetry specifications and Tapio's current implementation, we identified **10 critical gaps** in our observability architecture. This document provides a phased plan to transform our emitters and metrics into production-grade, OTEL-compliant instrumentation.

**Key Problems**:
1. OTELEmitter creates meaningless point-in-time spans
2. Metrics lack attributes (can't answer "which event types?")
3. No semantic conventions (custom attribute names)
4. Missing context propagation between observers
5. No error tracking in spans
6. Missing resource attributes (cluster, namespace, node)
7. Wrong cardinality management (will crash Prometheus)
8. Fake spans instead of proper metrics + logs
9. No histogram bucket configuration
10. Incomplete OTEL SDK initialization

**Expected Outcomes**:
- **Query events by type**: `rate(tapio_events_total{event_type="tcp_connect"}[5m])`
- **Filter by namespace**: `tapio_events_total{namespace="production"}`
- **Track errors**: `rate(tapio_events_errors[5m]) / rate(tapio_events_total[5m])`
- **Distributed traces**: Link network events → syscalls → K8s events in Jaeger
- **Standard dashboards**: Grafana templates work with semantic conventions
- **Alerting**: SLO-based alerts on error rates, latency percentiles

---

## 🎯 DESIGN PRINCIPLES

### 1. **Signals for the Right Purpose**
- **Metrics**: Aggregated measurements (events/sec, latency percentiles, error rates)
- **Traces**: Operations across services (API request → DB query → cache lookup)
- **Logs**: Individual events (what we currently have as ObserverEvents)

**Our events are LOGS, not TRACES. Stop creating fake spans.**

### 2. **Semantic Conventions Are Mandatory**
Use OTEL standard attributes:
- `network.peer.address` not `network.dst_ip`
- `network.peer.port` not `network.dst_port`
- `http.request.method` not `http_method`
- `process.pid` not `pid`

**Benefits**: Standard Grafana dashboards, queries, and alerts work immediately.

### 3. **Resource vs Span Attributes**
- **Resources**: Who is generating telemetry (cluster, namespace, node, service)
- **Span/Metric Attributes**: What is happening (event type, protocol, status)

**Example**:
```go
// Resource (set once at startup)
semconv.K8SClusterName("prod-us-east")
semconv.K8SNamespaceName("payments")
semconv.K8SNodeName("node-7")

// Metric attributes (set per event)
attribute.String("event.type", "tcp_connect")
attribute.String("network.protocol.name", "tcp")
```

### 4. **Cardinality Management**
**Rules**:
- ✅ LOW: Observer name (~10), event type (68), namespace (~50)
- ✅ MEDIUM: Deployment (~500), service (~200)
- ❌ HIGH: Pod name (thousands), IP addresses (millions), trace IDs (infinite)

**Use aggregation**: Map IPs → Service names, Pod names → Deployment names

### 5. **Context Propagation for Correlation**
Propagate trace context through event pipeline:
```
Network Observer (detects TCP connect)
  └─ Trace ID: abc123
      ├─ Process Observer (detects syscall) [same trace ID]
      ├─ K8s Observer (finds pod info) [same trace ID]
      └─ Correlation Engine (links events) [same trace ID]
```

**Impact**: See full event flow in Jaeger, not isolated spans.

---

## 🏗️ ARCHITECTURE CHANGES

### Current Architecture (Broken)
```
ObserverEvent → OTELEmitter.Emit()
                    ├─ Start span
                    ├─ Add attributes
                    └─ End span (immediately!)

Result: Thousands of 1μs spans with no correlation
```

### New Architecture (Correct)
```
ObserverEvent → MetricsEmitter
                    ├─ Record counter (tapio.events.total{type, domain})
                    ├─ Record histogram (tapio.event.duration{type})
                    ├─ Update gauge (tapio.network.active_connections)
                    └─ Attach span event to parent trace (if exists)

Result: Useful metrics + correlated traces
```

---

## 📦 IMPLEMENTATION PLAN

### Phase 0: Preparation (1 day)
**Goal**: Understand current state, set up testing

#### Tasks
1. **Audit Current Metrics**
   - Run `go test ./internal/base -v` to see what metrics exist
   - Document all current metric names and attributes
   - Identify what dashboards currently use

2. **Set Up OTEL Collector Locally**
   ```bash
   # Docker Compose with OTEL Collector + Prometheus + Jaeger
   docker-compose -f dev/otel-stack.yml up -d
   ```

3. **Create Test Agent**
   ```bash
   # Run Tapio agent with OTEL exporter pointing to local collector
   export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
   go run ./cmd/agent
   ```

**Deliverables**:
- [ ] Current metrics documentation
- [ ] Local OTEL stack running
- [ ] Test agent emitting to collector

**Success Criteria**: Can see current metrics in Prometheus, traces in Jaeger

---

### Phase 1: Add Semantic Conventions (2 days)
**Goal**: Replace custom attributes with OTEL standards

#### 1.1 Create Semantic Convention Helpers
**File**: `internal/base/semconv.go`

```go
package base

import (
    "go.opentelemetry.io/otel/attribute"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// NetworkAttributes converts NetworkEventData to OTEL semantic conventions
func NetworkAttributes(data *domain.NetworkEventData) []attribute.KeyValue {
    if data == nil {
        return nil
    }

    attrs := []attribute.KeyValue{
        semconv.NetworkProtocolName(data.Protocol),
    }

    // Peer (destination) attributes
    if data.DstIP != "" {
        attrs = append(attrs,
            semconv.NetworkPeerAddress(data.DstIP),
            semconv.NetworkPeerPort(int(data.DstPort)),
        )
    }

    // Local (source) attributes
    if data.SrcIP != "" {
        attrs = append(attrs,
            semconv.NetworkLocalAddress(data.SrcIP),
            semconv.NetworkLocalPort(int(data.SrcPort)),
        )
    }

    // HTTP attributes
    if data.HTTPMethod != "" {
        attrs = append(attrs,
            semconv.HTTPRequestMethod(data.HTTPMethod),
            semconv.URLPath(data.HTTPPath),
        )
        if data.HTTPStatusCode > 0 {
            attrs = append(attrs, semconv.HTTPResponseStatusCode(data.HTTPStatusCode))
        }
    }

    // Connection metadata
    if data.Duration > 0 {
        attrs = append(attrs,
            attribute.Int64("network.connection.duration_ns", data.Duration),
        )
    }
    if data.BytesSent > 0 {
        attrs = append(attrs,
            attribute.Int64("network.io.bytes_sent", int64(data.BytesSent)),
        )
    }
    if data.BytesReceived > 0 {
        attrs = append(attrs,
            attribute.Int64("network.io.bytes_received", int64(data.BytesReceived)),
        )
    }

    return attrs
}

// ProcessAttributes converts ProcessEventData to OTEL semantic conventions
func ProcessAttributes(data *domain.ProcessEventData) []attribute.KeyValue {
    if data == nil {
        return nil
    }

    attrs := []attribute.KeyValue{
        semconv.ProcessPID(int(data.PID)),
    }

    if data.PPID > 0 {
        attrs = append(attrs, semconv.ProcessParentPID(int(data.PPID)))
    }
    if data.ProcessName != "" {
        attrs = append(attrs, semconv.ProcessExecutableName(data.ProcessName))
    }
    if data.CommandLine != "" {
        attrs = append(attrs, semconv.ProcessCommand(data.CommandLine))
    }

    return attrs
}

// K8sAttributes converts K8s context to OTEL semantic conventions
func K8sAttributes(clusterID, namespace, podName, deploymentName string) []attribute.KeyValue {
    attrs := []attribute.KeyValue{}

    if clusterID != "" {
        attrs = append(attrs, semconv.K8SClusterName(clusterID))
    }
    if namespace != "" {
        attrs = append(attrs, semconv.K8SNamespaceName(namespace))
    }
    if podName != "" {
        attrs = append(attrs, semconv.K8SPodName(podName))
    }
    if deploymentName != "" {
        attrs = append(attrs, semconv.K8SDeploymentName(deploymentName))
    }

    return attrs
}

// eventDomainPrefixes maps event domains to their event type prefixes
var eventDomainPrefixes = []struct {
    domain   string
    prefixes []string
}{
    {"network",   []string{"tcp_", "udp_", "http_", "dns_"}},
    {"kernel",    []string{"oom_", "syscall_", "signal_"}},
    {"container", []string{"container_", "docker_"}},
    {"kubernetes",[]string{"pod_", "deployment_", "service_"}},
    {"process",   []string{"process_", "exec_"}},
}

// EventDomainAttribute returns event domain for grouping
func EventDomainAttribute(eventType string) attribute.KeyValue {
    domain := "unknown"
    for _, entry := range eventDomainPrefixes {
        for _, prefix := range entry.prefixes {
            if strings.HasPrefix(eventType, prefix) {
                domain = entry.domain
                break
            }
        }
        if domain != "unknown" {
            break
        }
    }
    return attribute.String("event.domain", domain)
}

// ErrorTypeAttribute categorizes errors
func ErrorTypeAttribute(eventType string) attribute.KeyValue {
    errorType := "unknown"

    switch eventType {
    case "oom_kill":
        errorType = "out_of_memory"
    case "connection_timeout", "connection_refused":
        errorType = "network_error"
    case "pod_crash_loop", "container_crash":
        errorType = "crash"
    case "deployment_failed":
        errorType = "deployment_error"
    }

    return attribute.String("error.type", errorType)
}
```

**Tests**: `internal/base/semconv_test.go`

```go
func TestNetworkAttributes(t *testing.T) {
    data := &domain.NetworkEventData{
        Protocol: "tcp",
        SrcIP:    "10.0.1.5",
        DstIP:    "10.0.2.8",
        SrcPort:  44320,
        DstPort:  80,
    }

    attrs := NetworkAttributes(data)

    // Verify semantic conventions
    assert.Contains(t, attrs, semconv.NetworkProtocolName("tcp"))
    assert.Contains(t, attrs, semconv.NetworkPeerAddress("10.0.2.8"))
    assert.Contains(t, attrs, semconv.NetworkPeerPort(80))
}
```

#### 1.2 Create Resource Configuration
**File**: `internal/base/resource.go`

```go
package base

import (
    "context"
    "os"
    "runtime"

    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// TapioResourceConfig holds resource configuration
type TapioResourceConfig struct {
    ClusterID   string
    Namespace   string
    NodeName    string
    ServiceName string
    Version     string
}

// CreateTapioResource creates OTEL resource for Tapio agent
func CreateTapioResource(ctx context.Context, config TapioResourceConfig) (*resource.Resource, error) {
    // Default values
    if config.ServiceName == "" {
        config.ServiceName = "tapio-agent"
    }
    if config.Version == "" {
        config.Version = os.Getenv("TAPIO_VERSION")
        if config.Version == "" {
            config.Version = "dev"
        }
    }

    return resource.New(ctx,
        resource.WithAttributes(
            // Service identification
            semconv.ServiceName(config.ServiceName),
            semconv.ServiceVersion(config.Version),
            semconv.ServiceInstanceID(config.NodeName),

            // Kubernetes context (if available)
            semconv.K8SClusterName(config.ClusterID),
            semconv.K8SNamespaceName(config.Namespace),
            semconv.K8SNodeName(config.NodeName),

            // Host information
            semconv.HostName(config.NodeName),
            semconv.HostArch(runtime.GOARCH),
            semconv.OSType(runtime.GOOS),

            // Telemetry SDK
            semconv.TelemetrySDKName("opentelemetry"),
            semconv.TelemetrySDKLanguageGo,
            semconv.TelemetrySDKVersion("1.24.0"),
        ),
    )
}
```

**Deliverables**:
- [ ] `internal/base/semconv.go` with all conversion functions
- [ ] `internal/base/semconv_test.go` with 80%+ coverage
- [ ] `internal/base/resource.go` with resource creation
- [ ] `internal/base/resource_test.go` with tests

**Success Criteria**:
- All tests pass
- Functions return proper OTEL semantic convention attributes
- Resource includes cluster, namespace, node info

---

### Phase 2: Enhanced Metrics with Attributes (2 days)
**Goal**: Add event type, domain, observer name to all metrics

#### 2.1 Redesign Metrics Structure
**File**: `internal/base/metrics.go` (rewrite)

```go
package base

import (
    "context"
    "fmt"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// ObserverMetrics holds OTEL metrics with proper attributes
type ObserverMetrics struct {
    // Event counters
    eventsTotal   metric.Int64Counter
    eventsDropped metric.Int64Counter
    errorsTotal   metric.Int64Counter

    // Duration histogram
    eventDuration metric.Float64Histogram

    // Active connections (for network observer)
    activeConnections metric.Int64UpDownCounter
}

// NewObserverMetrics creates OTEL metrics for observer
func NewObserverMetrics() (*ObserverMetrics, error) {
    meter := otel.Meter("tapio.observer")

    eventsTotal, err := meter.Int64Counter(
        "tapio.events.total",
        metric.WithDescription("Total events processed by type and domain"),
        metric.WithUnit("{events}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create events counter: %w", err)
    }

    eventsDropped, err := meter.Int64Counter(
        "tapio.events.dropped",
        metric.WithDescription("Events dropped due to buffer full or errors"),
        metric.WithUnit("{events}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create dropped counter: %w", err)
    }

    errorsTotal, err := meter.Int64Counter(
        "tapio.events.errors",
        metric.WithDescription("Errors processing events by type"),
        metric.WithUnit("{errors}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create errors counter: %w", err)
    }

    eventDuration, err := meter.Float64Histogram(
        "tapio.event.duration",
        metric.WithDescription("Event processing duration by type"),
        metric.WithUnit("ms"),
        metric.WithExplicitBucketBoundaries(
            0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000, 5000,
        ),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create duration histogram: %w", err)
    }

    activeConnections, err := meter.Int64UpDownCounter(
        "tapio.network.connections.active",
        metric.WithDescription("Current active network connections"),
        metric.WithUnit("{connections}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create active connections gauge: %w", err)
    }

    return &ObserverMetrics{
        eventsTotal:       eventsTotal,
        eventsDropped:     eventsDropped,
        errorsTotal:       errorsTotal,
        eventDuration:     eventDuration,
        activeConnections: activeConnections,
    }, nil
}

// RecordEvent records a successfully processed event with attributes
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName, eventType, eventDomain string) {
    m.eventsTotal.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("observer.name", observerName),
            attribute.String("event.type", eventType),
            attribute.String("event.domain", eventDomain),
        ),
    )
}

// RecordDrop records a dropped event
func (m *ObserverMetrics) RecordDrop(ctx context.Context, observerName, reason string) {
    m.eventsDropped.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("observer.name", observerName),
            attribute.String("drop.reason", reason),
        ),
    )
}

// RecordError records an error with type
func (m *ObserverMetrics) RecordError(ctx context.Context, observerName, eventType, errorType string) {
    m.errorsTotal.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("observer.name", observerName),
            attribute.String("event.type", eventType),
            attribute.String("error.type", errorType),
        ),
    )
}

// RecordProcessingTime records processing duration
func (m *ObserverMetrics) RecordProcessingTime(ctx context.Context, observerName, eventType string, durationMs float64) {
    m.eventDuration.Record(ctx, durationMs,
        metric.WithAttributes(
            attribute.String("observer.name", observerName),
            attribute.String("event.type", eventType),
        ),
    )
}

// RecordConnectionOpen records new connection
func (m *ObserverMetrics) RecordConnectionOpen(ctx context.Context, protocol, srcService, dstService string) {
    m.activeConnections.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("network.protocol.name", protocol),
            attribute.String("network.src.service", srcService),
            attribute.String("network.dst.service", dstService),
        ),
    )
}

// RecordConnectionClose records closed connection
func (m *ObserverMetrics) RecordConnectionClose(ctx context.Context, protocol, srcService, dstService string) {
    m.activeConnections.Add(ctx, -1,
        metric.WithAttributes(
            attribute.String("network.protocol.name", protocol),
            attribute.String("network.src.service", srcService),
            attribute.String("network.dst.service", dstService),
        ),
    )
}
```

#### 2.2 Update BaseObserver
**File**: `internal/base/observer.go` (modify RecordEvent methods)

```go
// RecordEvent increments events processed counter with full context
func (b *BaseObserver) RecordEvent(ctx context.Context, eventType, eventDomain string) {
    b.eventsProcessed.Add(1)
    b.metrics.RecordEvent(ctx, b.name, eventType, eventDomain)
}

// RecordDrop with reason
func (b *BaseObserver) RecordDrop(ctx context.Context, reason string) {
    b.eventsDropped.Add(1)
    b.metrics.RecordDrop(ctx, b.name, reason)
}

// RecordError with event type and error type
func (b *BaseObserver) RecordError(ctx context.Context, eventType, errorType string) {
    b.errorsTotal.Add(1)
    b.metrics.RecordError(ctx, b.name, eventType, errorType)
}

// RecordProcessingTime with event type
func (b *BaseObserver) RecordProcessingTime(ctx context.Context, eventType string, durationMs float64) {
    b.metrics.RecordProcessingTime(ctx, b.name, eventType, durationMs)
}
```

**Deliverables**:
- [ ] Rewritten `internal/base/metrics.go` with attributes
- [ ] Updated `internal/base/observer.go` method signatures
- [ ] Tests for all metric recording methods
- [ ] Update all observer implementations to pass event type

**Success Criteria**:
- Queries work: `rate(tapio_events_total{event_type="tcp_connect"}[5m])`
- Can filter by observer: `{observer_name="network"}`
- Can group by domain: `sum by (event_domain) (tapio_events_total)`

---

### Phase 3: Rewrite OTELEmitter (2 days)
**Goal**: Stop creating fake spans, emit proper metrics + span events

#### 3.1 Create New Metrics-Based Emitter
**File**: `internal/base/emitter.go` (rewrite OTELEmitter)

```go
// OTELEmitter exports events as OpenTelemetry metrics and span events
type OTELEmitter struct {
    metrics *ObserverMetrics
}

// NewOTELEmitter creates an OTEL emitter (metrics only, no fake spans)
func NewOTELEmitter() (*OTELEmitter, error) {
    metrics, err := NewObserverMetrics()
    if err != nil {
        return nil, fmt.Errorf("failed to create observer metrics: %w", err)
    }

    return &OTELEmitter{
        metrics: metrics,
    }, nil
}

// Emit records metrics and optionally attaches span event to parent trace
func (e *OTELEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
    if e.metrics == nil {
        return fmt.Errorf("metrics not initialized")
    }

    // Extract event domain from type
    eventDomain := extractEventDomain(event.Type)

    // Record event metric
    e.metrics.RecordEvent(ctx, event.Source, event.Type, eventDomain)

    // If this is a network event, track connections
    if event.NetworkData != nil {
        e.recordNetworkMetrics(ctx, event)
    }

    // If there's an active span, attach event details as span event
    e.attachToTrace(ctx, event)

    return nil
}

// recordNetworkMetrics records network-specific metrics
func (e *OTELEmitter) recordNetworkMetrics(ctx context.Context, event *domain.ObserverEvent) {
    data := event.NetworkData

    // Map IPs to service names (to avoid cardinality explosion)
    srcService := lookupServiceName(data.SrcIP) // From K8s context
    dstService := lookupServiceName(data.DstIP)

    switch event.Type {
    case "tcp_connect", "connection_established":
        e.metrics.RecordConnectionOpen(ctx, data.Protocol, srcService, dstService)
    case "tcp_close", "connection_closed":
        e.metrics.RecordConnectionClose(ctx, data.Protocol, srcService, dstService)
    }
}

// attachToTrace adds event as span event to parent trace (if exists)
func (e *OTELEmitter) attachToTrace(ctx context.Context, event *domain.ObserverEvent) {
    span := trace.SpanFromContext(ctx)
    if !span.IsRecording() {
        return // No active trace
    }

    // Build attributes using semantic conventions
    attrs := []attribute.KeyValue{
        attribute.String("event.name", event.Type),
        attribute.String("event.id", event.ID),
        attribute.String("event.source", event.Source),
    }

    // Add domain-specific attributes
    if event.NetworkData != nil {
        attrs = append(attrs, NetworkAttributes(event.NetworkData)...)
    }
    if event.ProcessData != nil {
        attrs = append(attrs, ProcessAttributes(event.ProcessData)...)
    }

    // Add event to span timeline
    span.AddEvent(
        fmt.Sprintf("observer.%s", event.Type),
        trace.WithAttributes(attrs...),
        trace.WithTimestamp(event.Timestamp),
    )

    // If this is an error event, mark span
    if isErrorEvent(event) {
        span.SetStatus(codes.Error, fmt.Sprintf("%s occurred", event.Type))
    }
}

// extractEventDomain maps event type to domain
func extractEventDomain(eventType string) string {
    switch {
    case strings.HasPrefix(eventType, "tcp_"), strings.HasPrefix(eventType, "udp_"),
         strings.HasPrefix(eventType, "http_"), strings.HasPrefix(eventType, "dns_"):
        return "network"
    case strings.HasPrefix(eventType, "oom_"), strings.HasPrefix(eventType, "syscall_"),
         strings.HasPrefix(eventType, "signal_"):
        return "kernel"
    case strings.HasPrefix(eventType, "container_"), strings.HasPrefix(eventType, "pod_"),
         strings.HasPrefix(eventType, "docker_"):
        return "kubernetes"
    case strings.HasPrefix(eventType, "process_"), strings.HasPrefix(eventType, "exec_"):
        return "process"
    default:
        return "unknown"
    }
}

// isErrorEvent determines if event represents an error
func isErrorEvent(event *domain.ObserverEvent) bool {
    errorEvents := map[string]bool{
        "oom_kill":           true,
        "connection_timeout": true,
        "connection_refused": true,
        "pod_crash_loop":     true,
        "container_crash":    true,
        "deployment_failed":  true,
    }

    if errorEvents[event.Type] {
        return true
    }

    // HTTP errors
    if event.NetworkData != nil && event.NetworkData.HTTPStatusCode >= 500 {
        return true
    }

    return false
}

// lookupServiceName maps IP to service name using K8s context
func lookupServiceName(ip string) string {
    // TODO: Implement K8s context lookup in Phase 4
    // Temporary: Return constant to avoid cardinality issues until proper implementation
    return "unknown_service"
}

// Close is a no-op for OTEL
func (e *OTELEmitter) Close() error {
    return nil
}
```

#### 3.2 Update CreateEmitters
**File**: `internal/base/emitter.go` (modify CreateEmitters)

```go
// CreateEmitters creates emitters based on output configuration
func CreateEmitters(config OutputConfig) (Emitter, error) {
    var emitters []Emitter

    if config.Stdout {
        emitters = append(emitters, NewStdoutEmitter())
    }

    if config.OTEL {
        otelEmitter, err := NewOTELEmitter()
        if err != nil {
            return nil, fmt.Errorf("failed to create OTEL emitter: %w", err)
        }
        emitters = append(emitters, otelEmitter)
    }

    if config.Tapio {
        emitters = append(emitters, NewTapioEmitter(1000))
    }

    if len(emitters) == 0 {
        return NewStdoutEmitter(), nil
    }

    if len(emitters) == 1 {
        return emitters[0], nil
    }

    return NewMultiEmitter(emitters...), nil
}
```

**Deliverables**:
- [ ] Rewritten `OTELEmitter` that emits metrics + span events
- [ ] Remove old span creation code
- [ ] Tests for new emitter behavior
- [ ] Update all observers to use new emitter

**Success Criteria**:
- No more fake point-in-time spans in Jaeger
- Metrics appear in Prometheus with proper attributes
- Span events appear in parent traces (when they exist)

---

### Phase 4: Context Propagation (2 days)
**Goal**: Correlate events across observers via trace context

#### 4.1 Add Trace Context to ObserverEvent
**File**: `pkg/domain/events.go` (add fields)

```go
// ObserverEvent is emitted by observers (68 subtypes)
type ObserverEvent struct {
    ID        string    `json:"id"`
    Type      string    `json:"type"`
    Source    string    `json:"source"`
    Timestamp time.Time `json:"timestamp"`

    // OTEL trace context for correlation
    TraceID    string `json:"trace_id,omitempty"`
    SpanID     string `json:"span_id,omitempty"`
    TraceFlags byte   `json:"trace_flags,omitempty"`

    // ... rest of fields ...
}
```

#### 4.2 Propagate Context in BaseObserver
**File**: `internal/base/observer.go` (add helper)

```go
// CreateEventWithContext creates event with trace context from current span
func (b *BaseObserver) CreateEventWithContext(ctx context.Context, eventType string) *domain.ObserverEvent {
    event := &domain.ObserverEvent{
        ID:        generateEventID(),
        Type:      eventType,
        Source:    b.name,
        Timestamp: time.Now(),
    }

    // Extract trace context from current span
    span := trace.SpanFromContext(ctx)
    if span.SpanContext().IsValid() {
        sc := span.SpanContext()
        event.TraceID = sc.TraceID().String()
        event.SpanID = sc.SpanID().String()
        event.TraceFlags = byte(sc.TraceFlags())
    }

    return event
}

// ProcessEventWithContext processes event and restores trace context
func (b *BaseObserver) ProcessEventWithContext(event *domain.ObserverEvent, handler func(context.Context, *domain.ObserverEvent) error) error {
    // Reconstruct trace context if present
    ctx := context.Background()

    if event.TraceID != "" {
        traceID, err := trace.TraceIDFromHex(event.TraceID)
        if err == nil {
            spanID, err := trace.SpanIDFromHex(event.SpanID)
            if err == nil {
                sc := trace.NewSpanContext(trace.SpanContextConfig{
                    TraceID:    traceID,
                    SpanID:     spanID,
                    TraceFlags: trace.TraceFlags(event.TraceFlags),
                    Remote:     true,
                })
                ctx = trace.ContextWithSpanContext(ctx, sc)
            }
        }
    }

    // Create child span for processing
    ctx, span := b.tracer.Start(ctx, fmt.Sprintf("process_%s", event.Type),
        trace.WithAttributes(
            attribute.String("event.id", event.ID),
            attribute.String("event.type", event.Type),
            attribute.String("event.source", event.Source),
        ),
    )
    defer span.End()

    // Call handler with context
    return handler(ctx, event)
}
```

#### 4.3 Initialize Tracer in BaseObserver
**File**: `internal/base/observer.go` (modify NewBaseObserver)

```go
// NewBaseObserver creates a new base observer with OTEL instrumentation
func NewBaseObserver(name string, tracer trace.Tracer) (*BaseObserver, error) {
    metrics, err := NewObserverMetrics()
    if err != nil {
        return nil, fmt.Errorf("failed to create metrics for observer %s: %w", name, err)
    }

    return &BaseObserver{
        name:      name,
        startTime: time.Now(),
        tracer:    tracer,
        metrics:   metrics,
        pipeline:  NewPipeline(),
    }, nil
}
```

**Deliverables**:
- [ ] Add TraceID/SpanID/TraceFlags to ObserverEvent
- [ ] Add CreateEventWithContext() helper
- [ ] Add ProcessEventWithContext() helper
- [ ] Update all observers to use context helpers
- [ ] Tests for context propagation

**Success Criteria**:
- Events carry trace context
- Observers can create child spans linked to parent
- Jaeger shows correlated events across observers

---

### Phase 5: Resource Initialization in Agent (1 day)
**Goal**: Set up OTEL SDK with resources, exporters, providers

#### 5.1 Create OTEL Initialization
**File**: `cmd/agent/telemetry.go` (new file)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"

    "github.com/yairfalse/tapio/internal/base"
)

// initTelemetry sets up OpenTelemetry SDK
func initTelemetry(ctx context.Context, config *Config) (func(), error) {
    // 1. Create resource with cluster/namespace/node context
    res, err := base.CreateTapioResource(ctx, base.TapioResourceConfig{
        ClusterID:   config.ClusterID,
        Namespace:   config.Namespace,
        NodeName:    config.NodeName,
        ServiceName: "tapio-agent",
        Version:     config.Version,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create resource: %w", err)
    }

    // 2. Set up trace exporter
    traceExporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
        otlptracegrpc.WithInsecure(), // TODO: Use TLS in production
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create trace exporter: %w", err)
    }

    // 3. Create tracer provider with sampling
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithResource(res),
        sdktrace.WithBatcher(traceExporter,
            sdktrace.WithBatchTimeout(5*time.Second),
            sdktrace.WithMaxExportBatchSize(512),
        ),
        sdktrace.WithSampler(
            sdktrace.ParentBased(
                sdktrace.TraceIDRatioBased(0.1), // Sample 10% of traces
            ),
        ),
    )
    otel.SetTracerProvider(tp)

    // 4. Set up metric exporter
    metricExporter, err := otlpmetricgrpc.New(ctx,
        otlpmetricgrpc.WithEndpoint(config.OTLPEndpoint),
        otlpmetricgrpc.WithInsecure(),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create metric exporter: %w", err)
    }

    // 5. Create meter provider with periodic reader
    mp := sdkmetric.NewMeterProvider(
        sdkmetric.WithResource(res),
        sdkmetric.WithReader(
            sdkmetric.NewPeriodicReader(
                metricExporter,
                sdkmetric.WithInterval(10*time.Second), // Export every 10s
            ),
        ),
    )
    otel.SetMeterProvider(mp)

    // 6. Set up context propagation for distributed tracing
    otel.SetTextMapPropagator(
        propagation.NewCompositeTextMapPropagator(
            propagation.TraceContext{},
            propagation.Baggage{},
        ),
    )

    log.Printf("✅ OpenTelemetry initialized (endpoint: %s)", config.OTLPEndpoint)

    // Return shutdown function
    return func() {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        log.Println("Shutting down OpenTelemetry...")

        if err := tp.Shutdown(ctx); err != nil {
            log.Printf("Error shutting down tracer provider: %v", err)
        }
        if err := mp.Shutdown(ctx); err != nil {
            log.Printf("Error shutting down meter provider: %v", err)
        }

        log.Println("✅ OpenTelemetry shutdown complete")
    }, nil
}
```

#### 5.2 Update Agent Main
**File**: `cmd/agent/main.go` (add telemetry init)

```go
func main() {
    ctx := context.Background()

    // Load configuration
    config := loadConfig()

    // Initialize OpenTelemetry (FIRST!)
    shutdownTelemetry, err := initTelemetry(ctx, config)
    if err != nil {
        log.Fatalf("Failed to initialize telemetry: %v", err)
    }
    defer shutdownTelemetry()

    // Now create observers (they will use global OTEL providers)
    observers := createObservers(config)

    // Run agent...
}
```

#### 5.3 Add Configuration
**File**: `cmd/agent/config.go` (add OTEL fields)

```go
type Config struct {
    // Existing fields...

    // OTEL configuration
    OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"localhost:4317"`
    ClusterID    string `env:"TAPIO_CLUSTER_ID" default:"default"`
    Namespace    string `env:"TAPIO_NAMESPACE" default:"tapio-system"`
    NodeName     string `env:"TAPIO_NODE_NAME" default:""`
    Version      string `env:"TAPIO_VERSION" default:"dev"`
}

func loadConfig() *Config {
    config := &Config{}

    // Load from environment
    config.OTLPEndpoint = getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
    config.ClusterID = getEnv("TAPIO_CLUSTER_ID", "default")
    config.Namespace = getEnv("TAPIO_NAMESPACE", "tapio-system")
    config.NodeName = getEnv("TAPIO_NODE_NAME", getHostname())
    config.Version = getEnv("TAPIO_VERSION", "dev")

    return config
}
```

**Deliverables**:
- [ ] `cmd/agent/telemetry.go` with OTEL initialization
- [ ] Updated `cmd/agent/main.go` to call init
- [ ] Updated `cmd/agent/config.go` with OTEL settings
- [ ] Environment variables documented

**Success Criteria**:
- Agent starts with OTEL initialized
- All metrics automatically tagged with cluster/namespace/node
- Graceful shutdown flushes pending telemetry

---

### Phase 6: Testing & Validation (2 days)
**Goal**: Verify all changes work end-to-end

#### 6.1 Integration Tests
**File**: `test/integration/otel_test.go`

```go
func TestOTELMetricsExport(t *testing.T) {
    // Start test OTEL collector
    collector := startTestCollector(t)
    defer collector.Stop()

    // Initialize agent with test config
    config := &Config{
        OTLPEndpoint: collector.Endpoint(),
        ClusterID:    "test-cluster",
        Namespace:    "test-ns",
        NodeName:     "test-node",
    }

    shutdown, err := initTelemetry(context.Background(), config)
    require.NoError(t, err)
    defer shutdown()

    // Create observer and emit event
    observer, err := base.NewBaseObserver("test-observer", otel.Tracer("test"))
    require.NoError(t, err)

    ctx := context.Background()
    observer.RecordEvent(ctx, "tcp_connect", "network")

    // Wait for export
    time.Sleep(2 * time.Second)

    // Verify metrics in collector
    metrics := collector.GetMetrics()
    assert.Contains(t, metrics, "tapio.events.total")

    // Verify attributes
    attrs := collector.GetAttributes("tapio.events.total")
    assert.Equal(t, "test-observer", attrs["observer.name"])
    assert.Equal(t, "tcp_connect", attrs["event.type"])
    assert.Equal(t, "network", attrs["event.domain"])
}

func TestContextPropagation(t *testing.T) {
    // Create parent trace
    ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
    defer span.End()

    // Create event with context
    observer, _ := base.NewBaseObserver("test", otel.Tracer("test"))
    event := observer.CreateEventWithContext(ctx, "test_event")

    // Verify trace ID propagated
    assert.NotEmpty(t, event.TraceID)
    assert.NotEmpty(t, event.SpanID)

    // Process event and verify child span created
    err := observer.ProcessEventWithContext(event, func(ctx context.Context, e *domain.ObserverEvent) error {
        childSpan := trace.SpanFromContext(ctx)
        assert.True(t, childSpan.SpanContext().IsValid())

        // Verify parent-child relationship
        parentTraceID := span.SpanContext().TraceID().String()
        childTraceID := childSpan.SpanContext().TraceID().String()
        assert.Equal(t, parentTraceID, childTraceID)

        return nil
    })
    require.NoError(t, err)
}
```

#### 6.2 Manual Testing
**Test Plan**:

1. **Metrics Query Test**
   ```bash
   # Start agent with OTEL collector
   docker-compose up -d otel-collector prometheus grafana
   go run ./cmd/agent

   # Query Prometheus
   curl 'http://localhost:9090/api/v1/query?query=tapio_events_total'

   # Expected: Metrics with labels {observer_name, event_type, event_domain}
   ```

2. **Trace Correlation Test**
   ```bash
   # Generate network event
   curl http://localhost:8080/test

   # Open Jaeger UI
   open http://localhost:16686

   # Expected: See trace with:
   # - HTTP request span
   # - Network observer span event
   # - Process observer span event
   # All linked by trace ID
   ```

3. **Dashboard Test**
   ```bash
   # Import Grafana dashboard
   # dashboards/tapio-observer-metrics.json

   # Expected: See graphs for:
   # - Events/sec by type
   # - Events/sec by observer
   # - Latency percentiles
   # - Error rate
   # - Active connections
   ```

**Deliverables**:
- [ ] Integration tests for OTEL metrics
- [ ] Integration tests for context propagation
- [ ] Manual test plan executed
- [ ] Sample Grafana dashboard
- [ ] Documentation updated

**Success Criteria**:
- All integration tests pass
- Metrics appear in Prometheus with correct attributes
- Traces show correlated events in Jaeger
- Grafana dashboard displays data correctly

---

## 📊 EXPECTED RESULTS

### Metrics Available After Implementation

```promql
# Total events per second
rate(tapio_events_total[5m])

# Events by type
rate(tapio_events_total{event_type="tcp_connect"}[5m])

# Events by domain
sum by (event_domain) (rate(tapio_events_total[5m]))

# Events by observer
sum by (observer_name) (rate(tapio_events_total[5m]))

# Error rate
rate(tapio_events_errors[5m]) / rate(tapio_events_total[5m])

# Processing latency P99
histogram_quantile(0.99, rate(tapio_event_duration_bucket[5m]))

# Active connections by service
sum by (network_dst_service) (tapio_network_connections_active)
```

### Grafana Dashboard Panels

1. **Overview**
   - Total events/sec (gauge)
   - Error rate (gauge with threshold)
   - Active connections (gauge)
   - Latency P50/P95/P99 (graph)

2. **Events by Type**
   - Events/sec by event type (stacked graph)
   - Top 10 event types (bar chart)

3. **Events by Observer**
   - Events/sec by observer (line graph)
   - Observer health status (stat panels)

4. **Network Metrics**
   - Connections/sec by protocol (stacked graph)
   - Active connections by service (heatmap)
   - Network bytes/sec (graph)

5. **Errors**
   - Error rate over time (graph)
   - Errors by type (bar chart)
   - Recent errors (logs panel)

---

## 🚨 BREAKING CHANGES

### API Changes

#### BaseObserver Method Signatures
```go
// OLD
func (b *BaseObserver) RecordEvent(ctx context.Context)

// NEW
func (b *BaseObserver) RecordEvent(ctx context.Context, eventType, eventDomain string)
```

**Migration**: All observers must pass event type and domain:
```go
// Before
observer.RecordEvent(ctx)

// After
observer.RecordEvent(ctx, "tcp_connect", "network")
```

#### ObserverEvent Structure
```go
// NEW FIELDS ADDED
type ObserverEvent struct {
    // ... existing fields ...
    TraceID    string `json:"trace_id,omitempty"`
    SpanID     string `json:"span_id,omitempty"`
    TraceFlags byte   `json:"trace_flags,omitempty"`
}
```

**Migration**: Serialization still works (new fields optional)

#### CreateEmitters Signature
```go
// OLD
func CreateEmitters(config OutputConfig, tracer trace.Tracer) Emitter

// NEW
func CreateEmitters(config OutputConfig) (Emitter, error)
```

**Migration**: Remove tracer parameter, use global provider

### Environment Variables

New required variables:
```bash
# OTEL exporter endpoint
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317

# Resource attributes
TAPIO_CLUSTER_ID=prod-us-east
TAPIO_NAMESPACE=tapio-system
TAPIO_NODE_NAME=node-7
TAPIO_VERSION=v1.0.0
```

---

## 🎯 SUCCESS METRICS

### Coverage Targets
- [ ] 80%+ test coverage for new code
- [ ] All existing tests pass
- [ ] Integration tests pass

### Performance Targets
- [ ] <1ms overhead per event (metric recording)
- [ ] <100MB memory for OTEL buffers
- [ ] 10s metric export interval

### Observability Targets
- [ ] 100% of events have event.type attribute
- [ ] 100% of metrics have observer.name attribute
- [ ] 100% of traces have resource attributes
- [ ] <1% dropped metrics/traces under load

---

## 📚 REFERENCES

- [OpenTelemetry Traces](https://opentelemetry.io/docs/concepts/signals/traces/)
- [OpenTelemetry Metrics](https://opentelemetry.io/docs/concepts/signals/metrics/)
- [Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
- [Go SDK](https://opentelemetry.io/docs/languages/go/)
- [Go Instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/)

---

## ✅ CHECKLIST

### Phase 0: Preparation
- [ ] Audit current metrics
- [ ] Set up local OTEL stack (Collector + Prometheus + Jaeger)
- [ ] Run test agent

### Phase 1: Semantic Conventions
- [ ] Create `internal/base/semconv.go`
- [ ] Create `internal/base/resource.go`
- [ ] Write tests (80%+ coverage)

### Phase 2: Enhanced Metrics
- [ ] Rewrite `internal/base/metrics.go`
- [ ] Update `internal/base/observer.go`
- [ ] Update all observers

### Phase 3: Rewrite Emitter
- [ ] Rewrite `OTELEmitter` (metrics + span events)
- [ ] Remove fake span creation
- [ ] Write tests

### Phase 4: Context Propagation
- [ ] Add trace fields to `ObserverEvent`
- [ ] Add context helpers to `BaseObserver`
- [ ] Update observers to propagate context

### Phase 5: Agent Initialization
- [ ] Create `cmd/agent/telemetry.go`
- [ ] Update `cmd/agent/main.go`
- [ ] Add OTEL config

### Phase 6: Testing
- [ ] Write integration tests
- [ ] Manual testing
- [ ] Create Grafana dashboard
- [ ] Update documentation

---

**READY TO BEGIN? Start with Phase 0 - Preparation.**
