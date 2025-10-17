# Commit Explanation: Multi-Backend Export System

**Commit:** `e333dbd` - feat(telemetry): add multi-backend export and event publisher interface
**Branch:** `feat/percpu-metrics-shared`
**Author:** Claude (via Yair)

---

## What I Did:

I implemented a **pluggable export system** that allows Tapio observers to send data to multiple backends simultaneously. This supports our freemium business model: give OSS users all the data for free, charge enterprise users for intelligent correlation.

---

## Files Changed:

### 1. `pkg/domain/publisher.go` (NEW FILE)

**What:** Interface for publishing domain events

```go
type EventPublisher interface {
    Publish(ctx context.Context, subject string, event any) error
    Close() error
}

type NoOpPublisher struct{}  // Default implementation that does nothing
```

**Why:**
- Clean abstraction for enterprise features (NATS publishing)
- OSS builds use `NoOpPublisher` (does nothing)
- Enterprise builds can inject real NATS publisher
- Follows dependency injection pattern - observers don't know about NATS

**Example Usage:**
```go
// OSS: Uses NoOpPublisher by default
observer, _ := base.NewBaseObserver("network")

// Enterprise: Inject NATS publisher
publisher := nats.NewPublisher("nats://ahti:4222")
observer, _ := base.NewBaseObserverWithConfig("network", cfg, publisher)
```

---

### 2. `internal/base/telemetry.go` (MODIFIED)

**What:** Added two new exporters to existing OTLP setup

#### Change 2a: Prometheus Exporter
```go
// Added to TelemetryConfig
PrometheusEnabled bool  // Enable Prometheus /metrics endpoint
PrometheusPort    int   // Port for scraping (default: 9090)

// In InitTelemetry():
if config.PrometheusEnabled {
    promExporter, _ := prometheus.New()
    metricReaders = append(metricReaders, promExporter)

    // Start HTTP server for /metrics endpoint
    httpServer := &http.Server{Addr: ":9090", Handler: promhttp.Handler()}
    go httpServer.ListenAndServe()
}
```

**Why:**
- OSS users often have Prometheus already deployed
- Lets them scrape Tapio metrics directly without OTLP collector
- Same metrics go to BOTH OTLP and Prometheus (no duplication of code)

#### Change 2b: OTLP Log Exporter
```go
// Added to InitTelemetry():
logExporter, _ := otlploggrpc.New(ctx, opts...)
lp := log.NewLoggerProvider(
    log.WithResource(res),
    log.WithProcessor(log.NewBatchProcessor(logExporter)),
)
global.SetLoggerProvider(lp)
```

**Why:**
- We now send full `ObserverEvent` as structured logs to OTLP
- OSS users get ALL raw event data (not just aggregated metrics)
- They can query individual events, build custom analytics
- This is the "generous free tier" - give them the data!

#### Change 2c: Shutdown Handling
```go
type TelemetryShutdown struct {
    tracerProvider *trace.TracerProvider
    meterProvider  *metric.MeterProvider
    loggerProvider *log.LoggerProvider  // NEW
    httpServer     *http.Server         // NEW
}

func (ts *TelemetryShutdown) Shutdown(ctx context.Context) error {
    // Shutdown HTTP server first (stop accepting requests)
    if ts.httpServer != nil {
        ts.httpServer.Shutdown(ctx)
    }
    // Then shutdown OTEL providers (flush pending data)
    ts.tracerProvider.Shutdown(ctx)
    ts.meterProvider.Shutdown(ctx)
    ts.loggerProvider.Shutdown(ctx)  // NEW
}
```

**Why:** Proper cleanup of all resources - HTTP server and log provider

---

### 3. `internal/base/observer.go` (MODIFIED)

**What:** Added event publishing capabilities to base observer

#### Change 3a: EventPublisher Field
```go
type BaseObserver struct {
    // ... existing fields
    eventPublisher domain.EventPublisher  // NEW - defaults to NoOpPublisher
}

func NewBaseObserverWithConfig(name string, cfg *TelemetryConfig, publisher domain.EventPublisher) (*BaseObserver, error) {
    // Default to NoOp if not provided (OSS builds)
    if publisher == nil {
        publisher = &domain.NoOpPublisher{}
    }

    return &BaseObserver{
        eventPublisher: publisher,
        // ...
    }
}
```

**Why:**
- Every observer can now optionally publish events
- OSS: `publisher = nil` → uses NoOp → nothing published
- Enterprise: `publisher = natsPublisher` → events go to Ahti

#### Change 3b: PublishEvent Method
```go
func (b *BaseObserver) PublishEvent(ctx context.Context, subject string, event any) error {
    return b.eventPublisher.Publish(ctx, subject, event)
}
```

**Why:** Convenience method for observers to publish domain events (enterprise feature)

#### Change 3c: SendObserverEvent Method (THE BIG ONE!)
```go
func (b *BaseObserver) SendObserverEvent(ctx context.Context, event *domain.ObserverEvent) {
    logger := global.GetLoggerProvider().Logger(b.name)

    // Marshal full event to JSON
    eventJSON, _ := json.Marshal(event)

    // Add queryable attributes
    attrs := []log.KeyValue{
        log.String("event.type", event.Type),
        log.String("event.source", event.Source),
        log.String("k8s.pod.name", event.NetworkData.PodName),
        log.String("k8s.namespace", event.NetworkData.Namespace),
        // ... more attributes
    }

    // Send to OTLP as structured log
    var logRecord log.Record
    logRecord.SetTimestamp(event.Timestamp)
    logRecord.SetBody(log.StringValue(string(eventJSON)))
    logRecord.AddAttributes(attrs...)
    logger.Emit(ctx, logRecord)
}
```

**Why:** This is the FREE VALUE we give OSS users!
- Full `ObserverEvent` sent to OTLP as structured log
- Contains all fields: NetworkData, K8sData, ContainerData, etc.
- OSS users can query by pod name, namespace, event type, etc.
- They get the raw data - they can build their own analytics

**Example OTLP Log Output:**
```json
{
  "timestamp": "2025-10-17T12:34:56Z",
  "body": "{\"type\":\"tcp_rtt_spike\",\"source\":\"network-observer\",\"network_data\":{\"src_ip\":\"10.0.1.5\",\"dst_ip\":\"10.0.2.10\",\"rtt_current\":50.3,\"rtt_baseline\":10.2,\"pod_name\":\"web-server-abc\"}}",
  "attributes": {
    "event.type": "tcp_rtt_spike",
    "event.source": "network-observer",
    "k8s.pod.name": "web-server-abc",
    "network.protocol": "tcp"
  }
}
```

---

### 4. `go.mod` / `go.sum` (MODIFIED)

**What:** Added dependencies

```
github.com/prometheus/client_golang v1.23.2
go.opentelemetry.io/otel/exporters/prometheus v0.60.0
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.14.0
go.opentelemetry.io/otel/sdk/log v0.14.0
```

**Why:** Required for new exporters

---

## Data Flow Architecture:

```
┌────────────────────────────────────────────────────────────┐
│ Network Observer (Your eBPF Code)                         │
│                                                            │
│  eBPF Event → domain.ObserverEvent                        │
│               ↓              ↓                             │
│               │              │                             │
│               │              └─→ Extract metrics           │
│               │                  observer.metrics.podRTT.Record()
│               │                  → OTLP + Prometheus       │
│               │                                            │
│               └─→ Send full event (NEW!)                  │
│                   observer.SendObserverEvent(ctx, event)  │
│                   → OTLP as structured log                │
│                                                            │
│  Enriched? Create TapioEvent                              │
│            observer.PublishEvent(ctx, "tapio.events", tapioEvent)
│            → NATS (Enterprise only, via EventPublisher)   │
└────────────────────────────────────────────────────────────┘
```

---

## What Observers Should Do Now:

### In Your Network Observer:

```go
func (o *Observer) processEvent(ctx context.Context, raw NetworkEventBPF) {
    // 1. Create domain.ObserverEvent from eBPF data
    event := &domain.ObserverEvent{
        Type:   "tcp_rtt_spike",
        Source: "network-observer",
        NetworkData: &domain.NetworkEventData{
            SrcIP:       raw.SrcIP,
            DstIP:       raw.DstIP,
            RTTCurrent:  raw.RTT,
            RTTBaseline: baseline,
            PodName:     getPodName(raw),
            Namespace:   getNamespace(raw),
        },
    }

    // 2. Send to OTLP as log (OSS users get this!)
    o.SendObserverEvent(ctx, event)

    // 3. Record metrics (goes to OTLP + Prometheus)
    o.metrics.podRTT.Record(ctx, float64(raw.RTT))
    o.metrics.rttSpikes.Add(ctx, 1)

    // 4. (Enterprise only) Publish enriched event to NATS
    // This will be NoOp in OSS builds
    tapioEvent := enrichWithK8sContext(event)
    o.PublishEvent(ctx, "tapio.events.network.rtt_spike", tapioEvent)
}
```

---

## Freemium Business Model:

### Open Source (FREE):
```
✅ Full ObserverEvent as OTLP logs
   - All fields: NetworkData, K8sData, ContainerData, etc.
   - Can query: "show me all RTT spikes for pod X"
   - Can build: Custom dashboards, alerts, analytics

✅ Metrics via Prometheus
   - Pre-aggregated counters/histograms
   - Standard Prometheus queries

✅ Traces for debugging
   - See Tapio's own operation traces
```

**OSS users can do a LOT with this data!** They just have to build their own tools.

### Enterprise (PAID):
```
✅ Everything from OSS

PLUS:
✅ TapioEvents → NATS → Ahti
   - Enriched with K8s metadata
   - Graph context (entities, relationships)
   - We do correlation FOR them

✅ Root cause analysis
✅ Anomaly detection
✅ Graph visualization
✅ Predictive insights
```

**Enterprise users pay for INTELLIGENCE, not data.**

---

## Testing:

```bash
# 1. Start Tapio with Prometheus enabled
export PROMETHEUS_ENABLED=true
export PROMETHEUS_PORT=9090

# 2. Check Prometheus metrics
curl http://localhost:9090/metrics
# Should see: observer_events_processed_total, network_pod_rtt_ms, etc.

# 3. Check OTLP logs (need OTLP collector running)
# Full ObserverEvent should appear as structured logs with attributes
```

---

## Next Steps (What You Might Want to Do):

1. **Update network observer** to call `SendObserverEvent()` when processing eBPF events
2. **Add more attributes** to `SendObserverEvent()` based on what's useful for querying
3. **Test with OTLP collector** to see full events in logs backend
4. **Add Prometheus metrics** for your Stage 3 RTT spike detection

---

## Questions?

This is a foundational change that makes Tapio's export system pluggable. All your existing observer code still works - I just added new capabilities.

The key insight: **Give OSS users the raw data (logs), charge enterprise users for the intelligence (correlation).**

Let me know if anything is unclear!

— Claude
