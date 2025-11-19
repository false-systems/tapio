 # Intelligence Service Implementation Plan

**Status:** Planning
**Date:** 2025-01-10
**Author:** Yair + Claude
**Related:** PR #512 (NATS emitter architecture mismatch)

---

## Executive Summary

**Problem:** PR #512 added `emitter_nats.go` to Runtime layer, publishing `ObserverEvent` directly to NATS. This violates architecture:
- Runtime should NOT publish to NATS
- NATS/TapioEvent are ENTERPRISE features (with Ahti)
- Missing "Intelligence Service" to do ObserverEvent → TapioEvent enrichment

**Solution:** Implement Intelligence Service (Level 1.5) as the bridge between FREE and ENTERPRISE tiers.

**Scope:**
- Delete `internal/runtime/emitter_nats.go` (wrong layer)
- Implement `cmd/intelligence/main.go` (new service)
- Update all observers to write `ObserverEvent` to NATS KV
- Implement FREE tier: OTLP Exporter (reads from NATS KV)
- Implement ENTERPRISE tier: Intelligence Service (enriches + publishes to Ahti)

---

## Architecture Overview

### Current Architecture (BROKEN)

```
Observers → ObserverEvent → Runtime → NATS JetStream ❌ WRONG
                                   ↓
                                 Ahti
```

**Problems:**
1. Runtime layer publishing to NATS (should be dumb)
2. Publishing raw ObserverEvent (Ahti needs TapioEvent with entities)
3. No FREE tier output (only ENTERPRISE gets events)

### Target Architecture (CORRECT)

```
FREE Tier:
Observers → ObserverEvent → NATS KV → OTLP Exporter → Prometheus/Grafana

ENTERPRISE Tier:
Observers → ObserverEvent → NATS KV → Intelligence Service → TapioEvent → NATS JetStream → Ahti
                                           ↑
                                      K8s Context (gRPC)
```

**Components:**
- **Observers** (DaemonSet/Deployment) - Write ObserverEvent to NATS KV bucket "observer-events"
- **OTLP Exporter** (FREE) - Reads ObserverEvent from NATS KV, exports as structured logs
- **Intelligence Service** (ENTERPRISE) - Reads ObserverEvent, enriches to TapioEvent, publishes to NATS JetStream
- **Context Service** - Provides K8s metadata via gRPC (already exists)

---

## Deployment Options (CRITICAL DECISION)

**Key Insight:** Not all users want to deploy NATS. We need to support multiple deployment modes.

Tapio supports **three deployment modes** based on infrastructure preferences:

### Option 1: Simple Mode (No NATS) ⭐ RECOMMENDED FOR GETTING STARTED

For users who want **zero infrastructure dependencies**:

```
Observers → Runtime → OTLPEmitter → OTLP Collector → Prometheus/Grafana
```

**How it works:**
- Observers use `OTLPEmitter` that sends events **directly** to OpenTelemetry Collector via OTLP/HTTP
- Uses official OpenTelemetry SDK (`go.opentelemetry.io/otel`)
- No NATS, no extra services - just observers + OTEL Collector

**Use when:**
- Small clusters (< 10 nodes)
- Don't want to manage NATS infrastructure
- Acceptable to lose events if OTLP Collector restarts (no buffering)
- Want simplest possible deployment (`kubectl apply -f observer.yaml` and done)

**Pros:**
- ✅ Zero infrastructure dependencies (most users already have OTEL Collector)
- ✅ Simplest deployment
- ✅ Direct export (low latency ~10-50ms)
- ✅ Standard OTLP protocol (works with any OTLP backend)

**Cons:**
- ❌ No buffering (if OTLP Collector down, events lost)
- ❌ No fan-out (can't send to multiple consumers simultaneously)
- ❌ Can't upgrade to ENTERPRISE tier without redeployment

**Example Configuration:**
```go
// cmd/observers/network/main.go
func main() {
    processor := NewNetworkProcessor()

    // Direct OTLP export (no NATS!)
    endpoint := os.Getenv("OTLP_ENDPOINT") // "otel-collector:4318"
    otlpEmitter, _ := runtime.NewOTLPEmitter(endpoint)

    rt, _ := runtime.NewObserverRuntime(
        processor,
        runtime.WithEmitters(otlpEmitter),
    )

    rt.Run(context.Background())
}
```

**Deployment:**
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-network-observer
spec:
  template:
    spec:
      containers:
      - name: network-observer
        env:
        - name: OTLP_ENDPOINT
          value: "http://otel-collector:4318"
        - name: EMITTER_TYPE
          value: "otlp"  # Use OTLPEmitter
```

**What you get:**
- Events sent as **OTLP Logs** with structured attributes
- Query in Loki: `{job="tapio-observer"} |= "tcp_connect"`
- Metrics in Prometheus: `tapio_observer_events_total{event_type="tcp_connect"}`

---

### Option 2: FREE Tier with NATS (Buffered + Resilient)

For users who want **buffering and resilience**:

```
Observers → Runtime → NATSKVEmitter → NATS KV → OTLP Exporter → Prometheus/Grafana
```

**How it works:**
- Observers write events to NATS KV bucket
- Separate OTLP Exporter service reads from NATS KV and exports to OTLP Collector
- NATS provides buffering and decoupling

**Use when:**
- Medium/large clusters (> 10 nodes)
- Need event buffering (resilience if collector restarts)
- Want fan-out (send events to multiple consumers)
- Plan to upgrade to ENTERPRISE tier later

**Pros:**
- ✅ Event buffering via NATS KV (resilient to restarts)
- ✅ Fan-out (multiple consumers can read same events)
- ✅ Decoupling (observers don't need to know about consumers)
- ✅ Easy upgrade path to ENTERPRISE tier

**Cons:**
- ❌ Requires NATS deployment
- ❌ Higher latency (~50-200ms with NATS hop)
- ❌ More infrastructure to manage

**Deployment:**
```yaml
# 1. Deploy NATS
kubectl apply -f deploy/nats.yaml

# 2. Deploy Observers (with NATSKVEmitter)
kubectl apply -f deploy/free-tier/network-observer.yaml

# 3. Deploy OTLP Exporter Service
kubectl apply -f deploy/free-tier/otlp-exporter.yaml
```

---

### Option 3: ENTERPRISE Tier (Graph Enrichment + Ahti)

For users who want **graph enrichment and correlation**:

```
Observers → Runtime → NATSKVEmitter → NATS KV → Intelligence Service → TapioEvent → Ahti
                                                        ↑
                                                   K8s Context (gRPC)
```

**How it works:**
- Same as Option 2, but replace OTLP Exporter with Intelligence Service
- Intelligence Service enriches ObserverEvent → TapioEvent with graph entities
- Publishes to NATS JetStream for Ahti correlation

**Use when:**
- Need K8s context enrichment (pod names, namespaces, labels)
- Want graph entity extraction (entities + relationships)
- Using Ahti correlation engine for root cause analysis

**Pros:**
- ✅ Full graph enrichment (entities + relationships)
- ✅ K8s context metadata (pod, service, deployment info)
- ✅ Ahti correlation for advanced analysis
- ✅ Production-grade event processing

**Cons:**
- ❌ Requires NATS + Intelligence Service + Context Service
- ❌ Higher latency (~100-200ms with enrichment)
- ❌ Most complex deployment

---

## Deployment Matrix

| Tier | NATS? | Observer Emitter | Consumer Service | Latency | Use Case |
|------|-------|------------------|------------------|---------|----------|
| **Simple** | No | `OTLPEmitter` | None (direct) | ~10-50ms | Getting started, small clusters |
| **FREE** | Yes | `NATSKVEmitter` | OTLP Exporter | ~50-200ms | Resilience, buffering, fan-out |
| **ENTERPRISE** | Yes | `NATSKVEmitter` | Intelligence | ~100-200ms | Graph enrichment, Ahti |

---

## Why This Matters: Lowering the Barrier to Entry

**Problem with original plan:** Forcing NATS on all users means:
- Users must deploy NATS before trying Tapio ❌
- Increased complexity for simple use cases ❌
- Harder to adopt ("I just want to try it!") ❌

**Solution with Simple Mode:**
- Users can `kubectl apply -f observer.yaml` and immediately get events in their existing OTLP Collector ✅
- No NATS setup required ✅
- Upgrade to NATS when they need buffering/resilience ✅
- Upgrade to ENTERPRISE when they need graph enrichment ✅

**This mirrors successful OSS adoption patterns:**
- **Prometheus:** Start with single binary, scale to federation
- **NATS:** Start with nats-server, scale to JetStream
- **Kubernetes:** Start with minikube, scale to production

**RAUTA parallel:** RAUTA's ADR 001 (hostNetwork DaemonSet) optimized for **zero extra network hops**. Similarly, Tapio's Simple Mode optimizes for **zero infrastructure dependencies**.

---

## Phase 0: Simple Mode - OTLPEmitter (Week 1)

### Objective
Implement OTLPEmitter for direct OTLP export (no NATS dependency)

### Why First?
- **Lowest barrier to entry** - Users can try Tapio immediately
- **Validates Runtime emitter pattern** - Foundation for NATSKVEmitter
- **Enables FREE tier users** - Don't need NATS for basic observability

### Implementation

#### 0.1: Create OTLPEmitter

**File:** `internal/runtime/emitter_otlp.go`

```go
package runtime

import (
    "context"
    "fmt"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/yairfalse/tapio/pkg/domain"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
    "go.opentelemetry.io/otel/log"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

type OTLPEmitter struct {
    endpoint    string
    logExporter *otlploghttp.Exporter
    logProvider *sdklog.LoggerProvider

    // Metrics
    logsExported prometheus.Counter
    exportErrors prometheus.Counter
}

func NewOTLPEmitter(endpoint string) (*OTLPEmitter, error) {
    // Create OTLP HTTP exporter
    exporter, err := otlploghttp.New(context.Background(),
        otlploghttp.WithEndpoint(endpoint),
        otlploghttp.WithInsecure(), // Use TLS in production
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
    }

    // Create log provider with batch processor
    provider := sdklog.NewLoggerProvider(
        sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
    )

    return &OTLPEmitter{
        endpoint:    endpoint,
        logExporter: exporter,
        logProvider: provider,
    }, nil
}

func (e *OTLPEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
    logger := e.logProvider.Logger("tapio.observer")

    // Build OTLP log attributes from ObserverEvent
    attrs := []attribute.KeyValue{
        attribute.String("event.id", event.ID),
        attribute.String("event.type", event.Type),
        attribute.String("event.source", event.Source),
        attribute.Int64("event.timestamp", event.Timestamp.Unix()),
    }

    // Add type-specific data as structured attributes
    if event.NetworkData != nil {
        attrs = append(attrs,
            attribute.String("network.src_ip", event.NetworkData.SrcIP),
            attribute.String("network.dst_ip", event.NetworkData.DstIP),
            attribute.Int("network.src_port", event.NetworkData.SrcPort),
            attribute.Int("network.dst_port", event.NetworkData.DstPort),
            attribute.String("network.protocol", event.NetworkData.Protocol),
        )
    }

    if event.SchedulingData != nil {
        attrs = append(attrs,
            attribute.String("scheduling.pod_name", event.SchedulingData.PodName),
            attribute.String("scheduling.namespace", event.SchedulingData.Namespace),
            attribute.String("scheduling.reason", event.SchedulingData.Reason),
        )
    }

    // Emit OTLP log record
    logger.Emit(ctx, log.Record{
        Timestamp:  event.Timestamp,
        Severity:   log.SeverityInfo,
        Body:       log.StringValue(fmt.Sprintf("%s: %s", event.Source, event.Type)),
        Attributes: attrs,
    })

    e.logsExported.Inc()
    return nil
}

func (e *OTLPEmitter) Close() error {
    ctx := context.Background()
    if err := e.logProvider.Shutdown(ctx); err != nil {
        return fmt.Errorf("failed to shutdown log provider: %w", err)
    }
    return nil
}
```

**TDD Tests:**

```go
// RED Phase
func TestOTLPEmitter_EmitNetworkEvent(t *testing.T) {
    // Create OTLP emitter (will fail - not implemented yet)
    emitter, err := NewOTLPEmitter("localhost:4318")
    require.NoError(t, err)
    defer emitter.Close()

    // Create network event
    event := &domain.ObserverEvent{
        ID:     "test-123",
        Type:   "tcp_connect",
        Source: "network-observer",
        NetworkData: &domain.NetworkEventData{
            SrcIP:   "10.0.1.5",
            DstIP:   "10.0.2.10",
            DstPort: 443,
        },
    }

    // Emit event
    err = emitter.Emit(context.Background(), event)
    require.NoError(t, err)

    // Verify metrics
    assert.Equal(t, 1, testutil.ToFloat64(emitter.logsExported))
}

// GREEN Phase: Implement NewOTLPEmitter() and Emit()
// REFACTOR Phase: Add more event types, edge cases
```

#### 0.2: Update ObserverRuntime to Support Multiple Emitters

**File:** `internal/runtime/runtime.go`

```go
type ObserverRuntime struct {
    processor EventProcessor
    emitters  []Emitter  // Support multiple emitters!
    queue     chan []byte
    sampler   *Sampler
}

func NewObserverRuntime(processor EventProcessor, opts ...RuntimeOption) (*ObserverRuntime, error) {
    rt := &ObserverRuntime{
        processor: processor,
        emitters:  []Emitter{},
        queue:     make(chan []byte, 1000),
    }

    // Apply options
    for _, opt := range opts {
        opt(rt)
    }

    // Default: If no emitters configured, use FileEmitter
    if len(rt.emitters) == 0 {
        rt.emitters = append(rt.emitters, NewFileEmitter("/tmp/tapio-events.log", false))
    }

    return rt, nil
}

// WithEmitters option
func WithEmitters(emitters ...Emitter) RuntimeOption {
    return func(rt *ObserverRuntime) {
        rt.emitters = append(rt.emitters, emitters...)
    }
}

// Emit to all configured emitters (fan-out)
func (r *ObserverRuntime) emitEvent(ctx context.Context, event *domain.ObserverEvent) error {
    var firstErr error

    for _, emitter := range r.emitters {
        if err := emitter.Emit(ctx, event); err != nil {
            if firstErr == nil {
                firstErr = err
            }
            // Continue to other emitters even if one fails (resilience)
        }
    }

    return firstErr
}
```

**Deliverables:**
- [ ] `internal/runtime/emitter_otlp.go` - OTLPEmitter implementation
- [ ] Runtime supports multiple emitters via `WithEmitters()` option
- [ ] Tests for OTLP export with structured attributes
- [ ] Example observer configuration (network-observer)
- [ ] Deployment manifest for Simple Mode
- [ ] Documentation: "Getting Started with Simple Mode"

---

## Phase 1: Cleanup and Foundation (Week 2)

### Objective
Remove broken NATS emitter, ensure all observers write to NATS KV

### Tasks

#### 1.1: Delete Runtime NATS Emitter
```bash
# Remove the file from PR #512
git rm internal/runtime/emitter_nats.go
git commit -m "refactor: remove NATS emitter from Runtime layer

Runtime should not publish to NATS. Intelligence Service will handle
enrichment and publishing in later phase.

Related: PR #512"
```

**Files affected:**
- `internal/runtime/emitter_nats.go` - DELETE
- Any tests referencing it - DELETE

**Verification:**
```bash
grep -r "emitter_nats" internal/runtime/
# Should return nothing
```

---

#### 1.2: Ensure Observers Write to NATS KV

**Pattern (for all observers):**

```go
// internal/observers/network/observer.go
type NetworkObserver struct {
    *base.BaseObserver

    // Event storage
    natsKV nats.KeyValue  // NATS KV for ObserverEvent storage
}

func (o *NetworkObserver) emitEvent(ctx context.Context, event *domain.ObserverEvent) error {
    // Marshal event
    data, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("failed to marshal event: %w", err)
    }

    // Write to NATS KV with event ID as key
    key := fmt.Sprintf("%s.%s", event.Source, event.ID)
    _, err = o.natsKV.Put(key, data)
    if err != nil {
        return fmt.Errorf("failed to write event to NATS KV: %w", err)
    }

    return nil
}
```

**Observers to update:**
- `internal/observers/network/observer.go` - Network Observer
- `internal/observers/scheduler/observer.go` - Scheduler Observer (already uses NATS KV)
- `internal/observers/container-runtime/observer.go` - Container Runtime Observer (when implemented)

**NATS KV Bucket Schema:**
```
Bucket: observer-events
Keys: <observer-name>.<event-id>
      network-observer.evt-123
      scheduler-observer.evt-456
Values: JSON-serialized ObserverEvent
TTL: 5 minutes (events consumed by Intelligence/OTLP Exporter)
```

**TDD Approach:**

```go
// RED Phase
func TestNetworkObserver_EmitToNATSKV(t *testing.T) {
    // Setup NATS KV mock
    kv := setupMockNATSKV(t)

    observer := NewNetworkObserver(/* ... */)
    observer.natsKV = kv

    event := &domain.ObserverEvent{
        ID:     "test-123",
        Type:   "tcp_connect",
        Source: "network-observer",
    }

    err := observer.emitEvent(context.Background(), event)
    require.NoError(t, err)

    // Verify written to KV
    val, err := kv.Get("network-observer.test-123")
    require.NoError(t, err)

    var stored domain.ObserverEvent
    json.Unmarshal(val.Value(), &stored)
    assert.Equal(t, event.ID, stored.ID)
}

// GREEN Phase: Implement emitEvent()
// REFACTOR Phase: Add error handling, metrics
```

**Metrics to add:**
```go
type ObserverMetrics struct {
    eventsWrittenToKV   prometheus.Counter
    kvWriteErrors       prometheus.Counter
    kvWriteDurationMs   prometheus.Histogram
}
```

**Deliverables:**
- [ ] All observers write to NATS KV bucket "observer-events"
- [ ] Tests verify KV writes
- [ ] Metrics for KV write success/failure
- [ ] Documentation in each observer's README

---

## Phase 2: FREE Tier - OTLP Exporter (Week 2)

### Objective
Implement OTLP Exporter to consume ObserverEvent from NATS KV and export as structured logs

### Architecture

```
NATS KV ("observer-events")
    ↓ Watch for new events
OTLP Exporter (Deployment, 1 replica)
    ↓ Convert ObserverEvent → OTLP Log
OpenTelemetry Collector
    ↓
Prometheus/Grafana/Loki
```

### Implementation

#### 2.1: Create OTLP Exporter Service

**File:** `cmd/otlp-exporter/main.go`

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

type OTLPExporter struct {
    natsConn     *nats.Conn
    natsKV       nats.KeyValue
    logExporter  *otlploghttp.Exporter
    logProvider  *sdklog.LoggerProvider

    // Metrics
    eventsExported   prometheus.Counter
    exportErrors     prometheus.Counter
}

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // Connect to NATS
    natsURL := os.Getenv("NATS_URL")
    nc, err := nats.Connect(natsURL)
    if err != nil {
        panic(err)
    }
    defer nc.Close()

    // Get KV bucket
    js, _ := nc.JetStream()
    kv, err := js.KeyValue("observer-events")
    if err != nil {
        panic(err)
    }

    // Create OTLP exporter
    exporter, err := NewOTLPExporter(nc, kv)
    if err != nil {
        panic(err)
    }

    // Run exporter
    if err := exporter.Run(ctx); err != nil {
        panic(err)
    }
}

func (e *OTLPExporter) Run(ctx context.Context) error {
    // Watch NATS KV for new events
    watcher, err := e.natsKV.WatchAll()
    if err != nil {
        return fmt.Errorf("failed to watch KV: %w", err)
    }
    defer watcher.Stop()

    for {
        select {
        case entry := <-watcher.Updates():
            if entry == nil {
                continue
            }

            // Parse ObserverEvent
            var event domain.ObserverEvent
            if err := json.Unmarshal(entry.Value(), &event); err != nil {
                e.exportErrors.Inc()
                continue
            }

            // Export as OTLP log
            if err := e.exportEvent(ctx, &event); err != nil {
                e.exportErrors.Inc()
                continue
            }

            e.eventsExported.Inc()

            // Delete from KV after successful export
            e.natsKV.Delete(entry.Key())

        case <-ctx.Done():
            return nil
        }
    }
}

func (e *OTLPExporter) exportEvent(ctx context.Context, event *domain.ObserverEvent) error {
    // Convert ObserverEvent to OTLP Log record
    logger := e.logProvider.Logger("tapio.observer")

    // Build structured log attributes
    attrs := []attribute.KeyValue{
        attribute.String("event.id", event.ID),
        attribute.String("event.type", event.Type),
        attribute.String("event.source", event.Source),
        attribute.Int64("event.timestamp", event.Timestamp.Unix()),
    }

    // Add event-specific data as JSON
    if event.NetworkData != nil {
        data, _ := json.Marshal(event.NetworkData)
        attrs = append(attrs, attribute.String("network_data", string(data)))
    }

    // Emit log
    logger.Emit(ctx, log.Record{
        Timestamp:  event.Timestamp,
        Severity:   log.SeverityInfo,
        Body:       log.StringValue(fmt.Sprintf("%s: %s", event.Source, event.Type)),
        Attributes: attrs,
    })

    return nil
}
```

**Configuration:**
```yaml
# deploy/otlp-exporter.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-otlp-exporter
  namespace: tapio
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: otlp-exporter
        image: ghcr.io/yairfalse/tapio-otlp-exporter:latest
        env:
        - name: NATS_URL
          value: "nats://tapio-nats:4222"
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://otel-collector:4318"
```

**TDD Tests:**

```go
// RED Phase
func TestOTLPExporter_WatchAndExport(t *testing.T) {
    // Setup mock NATS KV
    kv := setupMockNATSKV(t)

    // Put test event in KV
    event := &domain.ObserverEvent{
        ID:     "test-123",
        Type:   "tcp_connect",
        Source: "network-observer",
    }
    data, _ := json.Marshal(event)
    kv.Put("network-observer.test-123", data)

    // Setup mock OTLP exporter
    exporter := NewOTLPExporter(nil, kv)

    // Run exporter (should export event)
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    go exporter.Run(ctx)

    // Wait for export
    time.Sleep(500 * time.Millisecond)

    // Verify event was exported
    assert.Equal(t, 1, exporter.eventsExported.Value())

    // Verify event deleted from KV
    _, err := kv.Get("network-observer.test-123")
    assert.Error(t, err) // Should not exist
}
```

**Deliverables:**
- [ ] `cmd/otlp-exporter/main.go` - OTLP Exporter service
- [ ] NATS KV watcher implementation
- [ ] OTLP log export with structured attributes
- [ ] Tests for event consumption and export
- [ ] Deployment manifests
- [ ] Metrics (events exported, errors)
- [ ] FREE tier users can use Tapio without ENTERPRISE features

---

## Phase 3: ENTERPRISE Tier - Intelligence Service (Week 3-4)

### Objective
Implement Intelligence Service to enrich ObserverEvent → TapioEvent with graph entities

### Architecture

```
NATS KV ("observer-events")
    ↓ Watch for new events
Intelligence Service (Deployment, 1 replica)
    ↓ ObserverEvent
    ↓ Get K8s Context (gRPC to Context Service)
    ↓ EnrichWithK8sContext() [already exists in pkg/domain/enrichment.go]
    ↓ TapioEvent (with entities + relationships)
    ↓ Publish to NATS JetStream
NATS JetStream ("tapio.events.<cluster-id>")
    ↓
Ahti Correlation Engine
```

### Implementation

#### 3.1: Create Intelligence Service

**File:** `cmd/intelligence/main.go`

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
    pb "github.com/yairfalse/tapio/pkg/proto/context"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

type Intelligence struct {
    // Input: ObserverEvent from NATS KV
    natsConn *nats.Conn
    natsKV   nats.KeyValue

    // Enrichment: K8s context via gRPC
    contextClient pb.ContextServiceClient
    clusterID     string

    // Output: TapioEvent to NATS JetStream
    natsJS        nats.JetStreamContext
    publisher     *domain.NATSPublisher

    // Metrics
    eventsEnriched   prometheus.Counter
    enrichmentFails  prometheus.Counter
    publishSuccesses prometheus.Counter
    publishFailures  prometheus.Counter
}

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // Connect to NATS
    natsURL := os.Getenv("NATS_URL")
    nc, err := nats.Connect(natsURL)
    if err != nil {
        panic(err)
    }
    defer nc.Close()

    // Connect to Context Service
    contextURL := os.Getenv("CONTEXT_SERVICE_URL") // "tapio-context-service:50051"
    conn, err := grpc.Dial(contextURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    contextClient := pb.NewContextServiceClient(conn)

    // Create Intelligence Service
    intel, err := NewIntelligence(nc, contextClient, os.Getenv("CLUSTER_ID"))
    if err != nil {
        panic(err)
    }

    // Run
    if err := intel.Run(ctx); err != nil {
        panic(err)
    }
}

func (i *Intelligence) Run(ctx context.Context) error {
    // Watch NATS KV for new ObserverEvents
    watcher, err := i.natsKV.WatchAll()
    if err != nil {
        return fmt.Errorf("failed to watch KV: %w", err)
    }
    defer watcher.Stop()

    for {
        select {
        case entry := <-watcher.Updates():
            if entry == nil {
                continue
            }

            // Parse ObserverEvent
            var obsEvent domain.ObserverEvent
            if err := json.Unmarshal(entry.Value(), &obsEvent); err != nil {
                i.enrichmentFails.Inc()
                continue
            }

            // Enrich ObserverEvent → TapioEvent
            tapioEvent, err := i.enrichEvent(ctx, &obsEvent)
            if err != nil {
                i.enrichmentFails.Inc()
                continue
            }

            i.eventsEnriched.Inc()

            // Publish TapioEvent to NATS JetStream
            subject := fmt.Sprintf("tapio.events.%s", i.clusterID)
            if err := i.publisher.Publish(ctx, subject, tapioEvent); err != nil {
                i.publishFailures.Inc()
                continue
            }

            i.publishSuccesses.Inc()

            // Delete from KV after successful publish
            i.natsKV.Delete(entry.Key())

        case <-ctx.Done():
            return nil
        }
    }
}

func (i *Intelligence) enrichEvent(ctx context.Context, event *domain.ObserverEvent) (*domain.TapioEvent, error) {
    // Get K8s context from Context Service
    k8sCtx, err := i.getK8sContext(ctx, event)
    if err != nil {
        // Graceful degradation: continue without K8s context
        return i.enrichWithoutContext(event), nil
    }

    // Use existing enrichment function
    return domain.EnrichWithK8sContext(event, k8sCtx)
}

func (i *Intelligence) getK8sContext(ctx context.Context, event *domain.ObserverEvent) (*domain.K8sContext, error) {
    // Determine lookup strategy based on event type
    switch {
    case event.NetworkData != nil:
        // Network events: lookup by IP
        resp, err := i.contextClient.GetPodByIP(ctx, &pb.IPRequest{
            IP: event.NetworkData.SrcIP,
        })
        if err != nil {
            return nil, err
        }
        return convertToK8sContext(resp), nil

    case event.SchedulingData != nil:
        // Scheduler events: lookup by UID
        resp, err := i.contextClient.GetPodByUID(ctx, &pb.UIDRequest{
            UID: event.SchedulingData.PodUID,
        })
        if err != nil {
            return nil, err
        }
        return convertToK8sContext(resp), nil

    default:
        return nil, fmt.Errorf("unknown event type for K8s lookup")
    }
}
```

**Configuration:**
```yaml
# deploy/intelligence-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-intelligence
  namespace: tapio
spec:
  replicas: 1  # Singleton per cluster
  template:
    spec:
      containers:
      - name: intelligence
        image: ghcr.io/yairfalse/tapio-intelligence:latest
        env:
        - name: NATS_URL
          value: "nats://tapio-nats:4222"
        - name: CONTEXT_SERVICE_URL
          value: "tapio-context-service:50051"
        - name: CLUSTER_ID
          value: "prod-us-east-1"
        - name: TIER
          value: "enterprise"
        resources:
          requests:
            memory: 128Mi
            cpu: 100m
          limits:
            memory: 256Mi
```

**TDD Tests:**

```go
// RED Phase
func TestIntelligence_EnrichAndPublish(t *testing.T) {
    // Setup mocks
    kv := setupMockNATSKV(t)
    contextClient := setupMockContextClient(t)
    publisher := setupMockPublisher(t)

    // Put ObserverEvent in KV
    obsEvent := &domain.ObserverEvent{
        ID:   "test-123",
        Type: "tcp_connect",
        NetworkData: &domain.NetworkEventData{
            SrcIP: "10.0.1.5",
        },
    }
    data, _ := json.Marshal(obsEvent)
    kv.Put("network-observer.test-123", data)

    // Mock K8s context response
    contextClient.On("GetPodByIP", mock.Anything, &pb.IPRequest{IP: "10.0.1.5"}).
        Return(&pb.PodMetadata{
            Name:      "web-server-abc",
            Namespace: "production",
        }, nil)

    // Create Intelligence Service
    intel := NewIntelligence(nil, contextClient, "test-cluster")
    intel.natsKV = kv
    intel.publisher = publisher

    // Run enrichment
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    go intel.Run(ctx)

    // Wait for enrichment
    time.Sleep(500 * time.Millisecond)

    // Verify TapioEvent published
    assert.Equal(t, 1, publisher.PublishCount())

    publishedEvent := publisher.LastEvent().(*domain.TapioEvent)
    assert.Equal(t, "test-123", publishedEvent.ID)
    assert.Len(t, publishedEvent.Entities, 2) // Pod + Service
    assert.Len(t, publishedEvent.Relationships, 1) // connects_to
}
```

**Deliverables:**
- [ ] `cmd/intelligence/main.go` - Intelligence Service
- [ ] Integration with `pkg/domain/enrichment.go` (already exists)
- [ ] Integration with `pkg/domain/publisher.go` (already exists)
- [ ] K8s context lookup via gRPC
- [ ] NATS JetStream publishing
- [ ] Tests for enrichment pipeline
- [ ] Deployment manifests
- [ ] Metrics (enriched, failed, published)
- [ ] ENTERPRISE tier users get TapioEvent with graph entities

---

## Phase 4: Integration and Testing (Week 5)

### Objective
End-to-end testing of complete architecture

### 4.1: Integration Tests

**Test Scenario: FREE Tier**
```go
func TestE2E_FreeTier_OTLPExport(t *testing.T) {
    // Setup: Deploy Context Service, Network Observer, OTLP Exporter
    ctx := setupE2EEnvironment(t, "free")

    // Trigger network event (eBPF or mock)
    triggerTCPConnect(t, "10.0.1.5", "10.0.2.10", 443)

    // Wait for observer to detect and write to NATS KV
    time.Sleep(1 * time.Second)

    // Verify OTLP Exporter consumed event
    logs := queryOTLPCollector(t, "event.type=tcp_connect")
    require.Len(t, logs, 1)

    // Verify event has K8s context
    assert.Equal(t, "web-server-abc", logs[0].Attributes["pod_name"])
}
```

**Test Scenario: ENTERPRISE Tier**
```go
func TestE2E_EnterpriseTier_AhtiCorrelation(t *testing.T) {
    // Setup: Full stack including Intelligence Service
    ctx := setupE2EEnvironment(t, "enterprise")

    // Trigger network event
    triggerTCPConnect(t, "10.0.1.5", "10.0.2.10", 443)

    // Wait for Intelligence Service to enrich
    time.Sleep(2 * time.Second)

    // Verify TapioEvent published to NATS JetStream
    events := consumeNATSJetStream(t, "tapio.events.test-cluster")
    require.Len(t, events, 1)

    // Verify graph entities extracted
    tapioEvent := events[0].(*domain.TapioEvent)
    assert.Len(t, tapioEvent.Entities, 2)
    assert.Equal(t, "pod", tapioEvent.Entities[0].Type)
    assert.Equal(t, "service", tapioEvent.Entities[1].Type)

    // Verify relationships
    assert.Len(t, tapioEvent.Relationships, 1)
    assert.Equal(t, "connects_to", tapioEvent.Relationships[0].Type)
}
```

### 4.2: Performance Testing

**Metrics to measure:**
- Event latency: Observer detection → NATS KV write → Export/Publish
- Throughput: Events/sec at each stage
- Resource usage: CPU/Memory for OTLP Exporter and Intelligence Service

**Load Test:**
```go
func BenchmarkIntelligence_Throughput(b *testing.B) {
    // Setup Intelligence Service
    intel := setupIntelligence(b)

    // Generate N events
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        event := generateTestEvent(i)
        intel.processEvent(context.Background(), event)
    }

    // Measure: events/sec, latency p50/p95/p99
}
```

**Target Performance:**
- Latency: < 100ms (observer → export/publish)
- Throughput: 1000 events/sec per cluster
- Resource: < 256Mi memory for Intelligence Service

### Deliverables:
- [ ] E2E tests for FREE tier (OTLP export)
- [ ] E2E tests for ENTERPRISE tier (TapioEvent + Ahti)
- [ ] Performance benchmarks
- [ ] Load testing results
- [ ] Documentation updates

---

## Phase 5: Documentation and Deployment (Week 6)

### 5.1: Documentation

**Files to create/update:**
- [ ] `docs/010-intelligence-service-implementation-plan.md` (this doc)
- [ ] `docs/011-intelligence-service-architecture.md` - Detailed architecture
- [ ] `README.md` - Update with Intelligence Service (already done)
- [ ] `deploy/README.md` - Deployment guide
- [ ] `docs/FREE_TIER_GUIDE.md` - How to use FREE tier (OTLP export)
- [ ] `docs/ENTERPRISE_TIER_GUIDE.md` - How to use ENTERPRISE tier (Ahti)

### 5.2: Deployment Manifests

**Files to create:**
```
deploy/
├── free-tier/
│   ├── context-service.yaml
│   ├── network-observer.yaml
│   ├── scheduler-observer.yaml
│   └── otlp-exporter.yaml
│
└── enterprise-tier/
    ├── context-service.yaml
    ├── network-observer.yaml
    ├── scheduler-observer.yaml
    └── intelligence-service.yaml
```

### 5.3: Helm Chart (Future)

**Structure:**
```
charts/tapio-stack/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── context-service/
│   ├── observers/
│   ├── otlp-exporter/      # FREE tier
│   └── intelligence/       # ENTERPRISE tier
```

**values.yaml:**
```yaml
tier: free  # or "enterprise"

observers:
  network:
    enabled: true
  scheduler:
    enabled: false

# FREE tier config
otlp:
  enabled: true
  endpoint: "http://otel-collector:4318"

# ENTERPRISE tier config
intelligence:
  enabled: false  # Only if tier=enterprise
  clusterID: "prod-us-east-1"
```

---

## Migration Path

### For Existing Users (if any)

**Step 1: Update Observers**
```bash
# Redeploy observers to write to NATS KV
kubectl apply -f deploy/free-tier/network-observer.yaml
```

**Step 2: Deploy OTLP Exporter (FREE) OR Intelligence Service (ENTERPRISE)**
```bash
# FREE tier
kubectl apply -f deploy/free-tier/otlp-exporter.yaml

# OR ENTERPRISE tier
kubectl apply -f deploy/enterprise-tier/intelligence-service.yaml
```

**Step 3: Verify Events Flowing**
```bash
# FREE tier: Check OTLP Collector
kubectl logs -n tapio deploy/otel-collector

# ENTERPRISE tier: Check NATS JetStream
nats stream view tapio-events
```

---

## Success Criteria

### Phase 1 (Cleanup)
- [ ] `emitter_nats.go` deleted
- [ ] All observers write to NATS KV
- [ ] Tests verify KV writes

### Phase 2 (FREE Tier)
- [ ] OTLP Exporter deployed and running
- [ ] Events exported as OTLP structured logs
- [ ] FREE tier users can query events in Grafana/Prometheus
- [ ] Performance: < 100ms latency, 1000 events/sec

### Phase 3 (ENTERPRISE Tier)
- [ ] Intelligence Service deployed and running
- [ ] ObserverEvent enriched to TapioEvent with graph entities
- [ ] TapioEvent published to NATS JetStream
- [ ] Ahti can consume and correlate events
- [ ] Performance: < 200ms latency (includes enrichment)

### Phase 4 (Testing)
- [ ] E2E tests for both tiers passing
- [ ] Load tests show acceptable performance
- [ ] No regressions in existing functionality

### Phase 5 (Deployment)
- [ ] Documentation complete
- [ ] Deployment manifests tested
- [ ] Migration guide for existing users

---

## Timeline (Updated)

| Week | Phase | Deliverables |
|------|-------|--------------|
| 1 | **Phase 0: Simple Mode** | OTLPEmitter, multi-emitter Runtime, Simple Mode deployment |
| 2 | **Phase 1: Cleanup** | Delete emitter_nats, NATSKVEmitter, observers write to KV |
| 3 | **Phase 2: FREE Tier** | OTLP Exporter service, NATS-based FREE tier |
| 4 | **Phase 3: ENTERPRISE (Part 1)** | Intelligence Service structure, K8s context lookup |
| 5 | **Phase 3: ENTERPRISE (Part 2)** | Enrichment pipeline, NATS publishing, tests |
| 6 | **Phase 4: Integration** | E2E tests for all 3 modes, performance benchmarks |
| 7 | **Phase 5: Documentation** | Docs, deployment manifests, migration guide |

**Total: 7 weeks** (added 1 week for Phase 0: Simple Mode)

---

## Open Questions

1. **NATS KV TTL:** How long to keep ObserverEvents in KV before deletion?
   - Proposal: 5 minutes (enough for Intelligence/OTLP to consume)

2. **Event Ordering:** Does Ahti require ordered TapioEvents?
   - If yes: Use NATS JetStream with sequence numbers
   - If no: Current approach is fine

3. **Backpressure:** What if Intelligence Service can't keep up with event rate?
   - Proposal: Monitor KV size, alert if > 1000 events
   - Proposal: Add rate limiting to observers

4. **K8s Context Miss:** What if K8s context not found (pod deleted)?
   - Current: Graceful degradation, publish event without full context
   - Alternative: Drop event? Retry?

5. **Multi-Cluster:** How to route TapioEvents from multiple clusters to Ahti?
   - Proposal: NATS subject per cluster: `tapio.events.<cluster-id>`
   - Ahti subscribes to `tapio.events.*`

---

## Rollout Strategy

### Alpha (Internal Testing)
- Deploy on dev cluster
- Test with synthetic events
- Validate FREE and ENTERPRISE tiers work

### Beta (Early Adopters)
- Deploy on staging cluster
- Real workload testing
- Performance validation
- Fix bugs

### GA (General Availability)
- Document architecture clearly
- Publish Helm charts
- Migration guide for users
- Blog post explaining FREE vs ENTERPRISE

---

## Metrics to Track

**Operational Metrics:**
- Events written to NATS KV (per observer)
- Events exported via OTLP (FREE tier)
- Events enriched and published (ENTERPRISE tier)
- Enrichment failures (K8s context not found)
- NATS KV size (backpressure indicator)

**Performance Metrics:**
- Latency: Observer → NATS KV → Export/Publish
- Throughput: Events/sec
- Resource usage: CPU/Memory

**Business Metrics:**
- FREE tier users (OTLP Exporter deployments)
- ENTERPRISE tier users (Intelligence Service deployments)
- Event volume per tier

---

## References

- PR #512: NATS emitter architecture mismatch
- `pkg/domain/enrichment.go`: Existing EnrichWithK8sContext implementation
- `pkg/domain/publisher.go`: Existing NATSPublisher implementation
- `docs/PLATFORM_ARCHITECTURE.md`: Overall platform design
- `docs/adr/001-hostnetwork-daemonset-architecture.md` (RAUTA): Inspiration for zero-dependency approach
- `README.md`: Updated architecture diagrams
- OpenTelemetry OTLP Specification: https://opentelemetry.io/docs/specs/otlp/

---

## Changelog

### 2025-01-10 (Revision 2): Added Simple Mode (No NATS)

**Why this change?**

Original plan **forced NATS on all users**, creating a barrier to entry:
- Users must deploy NATS before trying Tapio ❌
- Increased complexity for simple use cases ❌
- Harder to adopt ("I just want to try it!") ❌

**What changed:**

1. **Added Phase 0: Simple Mode (OTLPEmitter)**
   - Direct OTLP export from observers to OTLP Collector
   - No NATS dependency required
   - Uses official OpenTelemetry SDK
   - **Recommended path for new users**

2. **Three deployment tiers instead of two:**
   - **Simple Mode** (no NATS): Direct OTLP export
   - **FREE Tier** (with NATS): Buffered OTLP export via NATS KV
   - **ENTERPRISE Tier** (with NATS): Graph enrichment via Intelligence Service

3. **Updated Runtime architecture:**
   - Support multiple emitters via `WithEmitters()` option
   - Default to FileEmitter if no emitters configured
   - Fan-out pattern: emit to all emitters (resilience)

4. **Timeline adjusted:** 6 weeks → 7 weeks (added Phase 0)

**Impact:**

- **Barrier to entry removed**: Users can `kubectl apply` and immediately get events in existing OTLP Collector
- **Adoption funnel**: Simple → FREE → ENTERPRISE (gradual complexity)
- **Validates emitter pattern**: OTLPEmitter is foundation for NATSKVEmitter
- **Aligns with OSS best practices**: Prometheus (single → federated), NATS (server → JetStream), K8s (minikube → production)

**Inspired by:** RAUTA's ADR 001 (hostNetwork DaemonSet) optimized for **zero extra network hops**. Tapio's Simple Mode optimizes for **zero infrastructure dependencies**.

---

**Next Steps:**
1. Review updated plan (especially "Deployment Options" section)
2. Start Phase 0: Implement OTLPEmitter
3. TDD approach: RED → GREEN → REFACTOR for each component
4. Small commits (≤30 lines), verify before push