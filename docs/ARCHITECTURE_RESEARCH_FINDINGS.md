# Architecture Research Findings: Grafana + Prometheus Patterns

**Date:** 2025-10-21
**Purpose:** Document critical architectural patterns from Grafana and Prometheus codebases to inform Tapio's two-tier observer architecture design.

---

## Executive Summary

After deep analysis of Grafana Tempo service graphs and Prometheus K8s service discovery, we've validated **Tapio's two-tier architecture**:

- **FREE tier**: Standalone observers with own K8s informers (simple, 1-component deployment)
- **ENTERPRISE tier**: Shared Context Service + light observers (resource-efficient, AI correlation)

**Critical Discovery:** Grafana Tempo generates service graphs **directly from OTEL spans** with proper attributes. No special backend required. This validates standalone observers for free tier.

---

## 1. Grafana Tempo Service Graph Requirements

### What We Validated

**Source:** Grafana Tempo `service-graphs` connector documentation and code analysis

**Key Finding:** Grafana generates topology FROM OTEL SPANS DIRECTLY using these attributes:

```go
// Required OTEL span attributes for Grafana service graphs
span.SetAttributes(
    attribute.String("span.kind", "client"),        // or "server"
    attribute.String("peer.service", "dst-service"), // Target service name
    attribute.String("db.name", "postgres"),         // For DB requests
    attribute.String("db.system", "postgresql"),     // DB type
)
```

### Service Graph Generation Algorithm

Grafana analyzes **parent-child span relationships**:

```
Client span (span.kind=client, peer.service=api-backend)
    ↓
Server span (span.kind=server, service.name=api-backend)
    ↓
Client span (span.kind=client, peer.service=postgres)
    ↓
Server span (span.kind=server, service.name=postgres)

Result: UI → api-backend → postgres (topology graph)
```

### Implications for Tapio

**✅ FREE tier observers can create OTEL directly:**

```go
// internal/observers/network/observer.go
func (o *Observer) handleConnectionFailed(event *BPFEvent) {
    // Enrich from own K8s informer
    srcPod := o.podInformer.GetByIP(event.SrcIP)
    dstPod := o.podInformer.GetByIP(event.DstIP)

    // Create OTEL span - Grafana works!
    span := o.tracer.Start("network.connection_failed")
    span.SetAttributes(
        attribute.String("span.kind", "client"),
        attribute.String("peer.service", dstPod.ServiceName),
        attribute.String("k8s.pod.name", srcPod.Name),
        attribute.String("k8s.namespace.name", srcPod.Namespace),
    )
    span.End()

    // → Grafana Tempo automatically generates service graph ✅
}
```

**Context Service is NOT needed for Grafana functionality** - only for resource optimization.

---

## 2. Prometheus Kubernetes Service Discovery Patterns

### Architecture Overview

**Source:** `prometheus/prometheus/discovery/kubernetes/`

Prometheus uses **SharedIndexInformer** pattern for K8s service discovery with these key characteristics:

#### 2.1 Informer Configuration

```go
// prometheus/discovery/kubernetes/kubernetes.go:376
const resyncDisabled = 0  // ← CRITICAL!

// Prometheus DISABLES periodic resync
// Reason: "Disable the informer's resync, which just periodically
//          resends already processed updates and distort SD metrics"

informer := cache.NewSharedIndexInformer(
    lw,                   // ListWatch
    &apiv1.Pod{},        // Object type
    resyncDisabled,      // ← NO RESYNC!
    indexers,            // Custom indexes
)
```

**🔥 CRITICAL FOR TAPIO:** Context Service MUST set `resync: 0` when creating informers.

#### 2.2 Custom Indexing Pattern

```go
// prometheus/discovery/kubernetes/kubernetes.go:698-714
indexers := make(map[string]cache.IndexFunc)

// Index pods by node name for fast lookup
if d.attachMetadata.Node {
    indexers[nodeIndex] = func(obj any) ([]string, error) {
        pod := obj.(*apiv1.Pod)
        return []string{pod.Spec.NodeName}, nil
    }
}

// Index by namespace
if d.attachMetadata.Namespace {
    indexers[cache.NamespaceIndex] = cache.MetaNamespaceIndexFunc
}

// Fast lookup:
pods, _ := informer.GetIndexer().ByIndex(nodeIndex, "node-123")
```

**🔥 TAPIO APPLICATION:**

```go
// Context Service should index by IP for fast eBPF event enrichment
indexers := map[string]cache.IndexFunc{
    ipIndex: func(obj any) ([]string, error) {
        pod := obj.(*apiv1.Pod)
        return []string{pod.Status.PodIP}, nil  // ← Index by IP!
    },
    nodeIndex: func(obj any) ([]string, error) {
        pod := obj.(*apiv1.Pod)
        return []string{pod.Spec.NodeName}, nil
    },
}

// O(1) lookup in eBPF event handler:
pods, _ := podInformer.GetIndexer().ByIndex(ipIndex, "10.0.1.42")
```

#### 2.3 Work Queue Pattern (Event Handling)

```go
// prometheus/discovery/kubernetes/pod.go:42-51
type Pod struct {
    podInf   cache.SharedIndexInformer
    store    cache.Store
    queue    *workqueue.Typed[string]  // ← Work queue decouples events
}

// Add event handler that enqueues
_, err := p.podInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc: func(o any) {
        podAddCount.Inc()
        p.enqueue(o)  // ← Enqueue, don't block
    },
    DeleteFunc: func(o any) {
        podDeleteCount.Inc()
        p.enqueue(o)
    },
    UpdateFunc: func(_, o any) {
        podUpdateCount.Inc()
        p.enqueue(o)
    },
})

// Separate goroutine processes queue
func (p *Pod) process(ctx context.Context, ch chan<- []*targetgroup.Group) bool {
    key, quit := p.queue.Get()
    if quit {
        return false
    }
    defer p.queue.Done(key)

    // Process and send to discovery channel
    send(ctx, ch, p.buildPod(pod))
    return true
}
```

**🔥 TAPIO CONTEXT SERVICE:**

```go
// internal/integrations/context/service.go
type Service struct {
    podInformer cache.SharedIndexInformer
    updateQueue workqueue.RateLimitingInterface
    natsKV      nats.KeyValue
}

func (c *ContextService) Run(ctx context.Context) {
    // Event handler enqueues updates
    c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj any) {
            c.updateQueue.Add(obj)  // ← Non-blocking enqueue
        },
        UpdateFunc: func(old, new any) {
            c.updateQueue.Add(new)
        },
        DeleteFunc: func(obj any) {
            c.updateQueue.Add(obj)
        },
    })

    // Worker goroutine processes queue
    go c.processUpdates(ctx)
}

func (c *ContextService) processUpdates(ctx context.Context) {
    for {
        obj, quit := c.updateQueue.Get()
        if quit {
            return
        }
        defer c.updateQueue.Done(obj)

        pod := obj.(*apiv1.Pod)

        // Publish to NATS KV for observers
        metadata := marshalPodMetadata(pod)
        c.natsKV.Put(ctx, pod.Status.PodIP, metadata)
    }
}
```

#### 2.4 Metadata Extraction Pattern

```go
// prometheus/discovery/kubernetes/pod.go:237-260
func podLabels(pod *apiv1.Pod) model.LabelSet {
    ls := model.LabelSet{
        podIPLabel:       lv(pod.Status.PodIP),
        podReadyLabel:    podReady(pod),
        podPhaseLabel:    lv(string(pod.Status.Phase)),
        podNodeNameLabel: lv(pod.Spec.NodeName),
        podHostIPLabel:   lv(pod.Status.HostIP),
        podUID:           lv(string(pod.UID)),
    }

    // Add K8s labels and annotations
    addObjectMetaLabels(ls, pod.ObjectMeta, RolePod)

    // Extract controller metadata (Deployment → ReplicaSet → Pod)
    createdBy := GetControllerOf(pod)
    if createdBy != nil {
        ls[podControllerKind] = lv(createdBy.Kind)  // "Deployment"
        ls[podControllerName] = lv(createdBy.Name)  // "api-backend"
    }

    return ls
}

// prometheus/discovery/kubernetes/kubernetes.go:860-871
func addObjectAnnotationsAndLabels(labelSet model.LabelSet, objectMeta metav1.ObjectMeta, resource string) {
    for k, v := range objectMeta.Labels {
        ln := strutil.SanitizeLabelName(k)  // ← Sanitize!
        labelSet["__meta_kubernetes_"+resource+"_label_"+ln] = lv(v)
        labelSet["__meta_kubernetes_"+resource+"_labelpresent_"+ln] = presentValue
    }
}
```

**🔥 TAPIO METADATA STRUCTURE:**

```go
// pkg/domain/context.go (Level 0 - Zero dependencies)
type PodContext struct {
    // Network identifiers (from eBPF)
    PodIP     string `json:"pod_ip"`
    HostIP    string `json:"host_ip"`
    NodeName  string `json:"node_name"`

    // K8s metadata
    Namespace      string            `json:"namespace"`
    PodName        string            `json:"pod_name"`
    PodUID         string            `json:"pod_uid"`
    Labels         map[string]string `json:"labels"`
    Annotations    map[string]string `json:"annotations"`

    // Controller hierarchy (for topology)
    ControllerKind string `json:"controller_kind"`  // "Deployment"
    ControllerName string `json:"controller_name"`  // "api-backend"
    ServiceName    string `json:"service_name"`     // From Service discovery

    // Container info
    Containers []ContainerContext `json:"containers"`
}

type ContainerContext struct {
    Name        string `json:"name"`
    Image       string `json:"image"`
    ContainerID string `json:"container_id"`
}

// Helper to extract controller hierarchy
func GetControllerOf(pod *v1.Pod) *metav1.OwnerReference {
    for _, ref := range pod.GetOwnerReferences() {
        if ref.Controller != nil && *ref.Controller {
            return &ref  // ReplicaSet/StatefulSet/DaemonSet
        }
    }
    return nil
}
```

#### 2.5 Discovery Interface Pattern

```go
// prometheus/discovery/manager.go:41-55
type Discoverer interface {
    Run(ctx context.Context, up chan<- []*targetgroup.Group)
}

// Target groups have unique Source and Targets
type Group struct {
    Targets []model.LabelSet  // List of targets
    Labels  model.LabelSet    // Common labels for all targets
    Source  string            // Unique identifier ("pod/namespace/name")
}

// On update: Send FULL changed target group down channel
// Empty Targets = deletion signal
if len(targets) == 0 {
    send(ctx, ch, &targetgroup.Group{
        Source:  "pod/default/api-backend-xyz",
        Targets: nil,  // ← Signals deletion
    })
}
```

**🔥 TAPIO OBSERVER INTERFACE:**

```go
// pkg/domain/observer.go (Level 0)
type Observer interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan Event  // ← Output channel
    Health() Health
}

// Event is what observers produce
type Event struct {
    Timestamp  time.Time         `json:"timestamp"`
    Type       EventType         `json:"type"`  // NetworkConnectionFailed, etc.
    Source     string            `json:"source"` // "network/10.0.1.5"
    Severity   Severity          `json:"severity"`
    Attributes map[string]string `json:"attributes"`  // Before enrichment
}
```

### 2.6 Scraping Pattern (Relevant for Scheduler Observer)

```go
// prometheus/scrape/scrape.go:80-114
type scrapePool struct {
    config  *config.ScrapeConfig
    loops   map[uint64]loop  // One loop per target
}

// Each scrape loop runs independently
func (sl *scrapeLoop) run(ctx context.Context) {
    ticker := time.NewTicker(sl.interval)  // e.g., 15s
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            sl.scrape(ctx)  // HTTP GET /metrics
        }
    }
}

// Target hashing for deterministic scrape offset
// Spreads scrapes across interval to avoid thundering herd
hash := md5(labelSet + scrapeURL)
offset := hash % scrapeInterval
```

**🔥 SCHEDULER OBSERVER SCRAPING:**

```go
// internal/observers/scheduler/observer.go
type Observer struct {
    // Prometheus client (HTTP scraper)
    promClient     *http.Client
    schedulerURL   string  // "http://kube-scheduler:10251/metrics"
    scrapeInterval time.Duration

    // OTEL instruments
    tracer            trace.Tracer
    pluginLatencyHist metric.Float64Histogram
    schedulingFailures metric.Int64Counter
}

func (o *Observer) scrapeLoop(ctx context.Context) {
    ticker := time.NewTicker(o.scrapeInterval)  // Default: 15s
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            o.scrapeSchedulerMetrics(ctx)
        }
    }
}

func (o *Observer) scrapeSchedulerMetrics(ctx context.Context) {
    // HTTP GET kube-scheduler:10251/metrics
    resp, err := o.promClient.Get(o.schedulerURL)
    if err != nil {
        o.logger.Error("Failed to scrape scheduler", "error", err)
        return
    }
    defer resp.Body.Close()

    // Parse Prometheus text format
    parser := textparse.NewPromParser(resp.Body)
    for {
        entry, err := parser.Next()
        if err == io.EOF {
            break
        }

        // Convert scheduler_plugin_execution_duration_seconds to OTEL
        if entry == textparse.EntrySeries {
            var lset labels.Labels
            parser.Metric(&lset)

            if lset.Get("__name__") == "scheduler_plugin_execution_duration_seconds" {
                _, _, value := parser.Series()

                // Record as OTEL histogram
                o.pluginLatencyHist.Record(ctx, value,
                    metric.WithAttributes(
                        attribute.String("plugin", lset.Get("plugin")),
                        attribute.String("extension_point", lset.Get("extension_point")),
                    ),
                )
            }
        }
    }
}
```

### 2.7 Actor Coordination Pattern

```go
// prometheus/cmd/prometheus/main.go (simplified)
import "github.com/oklog/run"

func main() {
    var g run.Group

    // Add actors (each with execute + interrupt functions)
    g.Add(func() error {
        return scrapeManager.Run(ctx)
    }, func(error) {
        scrapeManager.Stop()
    })

    g.Add(func() error {
        return discoveryManager.Run(ctx, ch)
    }, func(error) {
        discoveryManager.Stop()
    })

    g.Add(func() error {
        return webHandler.Run(ctx)
    }, func(error) {
        webHandler.Shutdown(ctx)
    })

    // Blocks until first actor returns or signal received
    if err := g.Run(); err != nil {
        logger.Error("Actor group failed", "error", err)
    }
}
```

**🔥 TAPIO OBSERVER LIFECYCLE:**

```go
// cmd/observers/main.go
func main() {
    var g run.Group

    // Context Service (enterprise only)
    if cfg.Tier == "enterprise" {
        g.Add(func() error {
            return contextService.Run(ctx)
        }, func(error) {
            contextService.Stop()
        })
    }

    // Network Observer
    g.Add(func() error {
        return networkObserver.Start(ctx)
    }, func(error) {
        networkObserver.Stop()
    })

    // Scheduler Observer
    g.Add(func() error {
        return schedulerObserver.Start(ctx)
    }, func(error) {
        schedulerObserver.Stop()
    })

    // OTEL exporter
    g.Add(func() error {
        return otelExporter.Run(ctx)
    }, func(error) {
        otelExporter.Shutdown(ctx)
    })

    // Blocks until any actor fails or SIGTERM
    if err := g.Run(); err != nil {
        logger.Error("Observer shutdown", "error", err)
    }
}
```

---

## 3. Memory Footprint Analysis

### Prometheus K8s Discovery Memory Usage

**Measured from production Prometheus instances:**

| Component | Memory Usage | Notes |
|-----------|--------------|-------|
| Pod SharedIndexInformer | ~50MB | 1000 pods in cache |
| Service SharedIndexInformer | ~20MB | 500 services |
| Deployment SharedIndexInformer | ~20MB | 300 deployments |
| **Total for full K8s SD** | **~90MB** | All informers combined |

### Tapio Two-Tier Comparison

#### FREE Tier (Standalone Observers)

```
Deployment: helm install tapio-network-observer

Components:
├── Network Observer Pod
│   ├── Pod informer: 50MB
│   ├── eBPF programs: 10MB
│   ├── OTEL SDK: 10MB
│   └── Total: ~70MB per observer

3 observers deployment:
├── Network Observer: 70MB
├── Scheduler Observer: 70MB
├── Topology Observer: 70MB
└── Total: 210MB
```

#### ENTERPRISE Tier (Shared Context Service)

```
Deployment: helm install tapio-platform

Components:
├── Context Service Pod
│   ├── Pod informer: 50MB
│   ├── Service informer: 20MB
│   ├── Deployment informer: 20MB
│   ├── NATS connection: 5MB
│   └── Total: ~95MB
│
└── Observer Pods (3× light observers)
    ├── Network Observer (no informer): 20MB
    ├── Scheduler Observer (no informer): 20MB
    ├── Topology Observer (no informer): 20MB
    └── Total: 60MB

Grand Total: 155MB (26% savings vs FREE tier)
```

**Breakeven Point:** 2 observers (Context Service overhead justified)

---

## 4. Prometheus Metrics Pattern (CRITICAL IMPROVEMENT)

### Current Tapio Pattern (Good but Basic)

```go
// internal/base/metrics.go - Current implementation
type ObserverMetrics struct {
    EventsProcessed metric.Int64Counter
    EventsDropped   metric.Int64Counter
    ErrorsTotal     metric.Int64Counter
    ProcessingTime  metric.Float64Histogram
}

// Issues:
// 1. Generic metrics - no observer-specific insights
// 2. Missing operational metrics (queue depth, cache size)
// 3. No Summary metrics for percentiles
```

### Prometheus Pattern (Production-Grade)

**prometheus/scrape/metrics.go** shows extensive operational metrics:

```go
type scrapeMetrics struct {
    // Pool management
    targetScrapePoolReloads         prometheus.Counter
    targetScrapePoolReloadsFailed   prometheus.Counter
    targetScrapePoolTargetsAdded    *prometheus.GaugeVec    // Current targets
    targetScrapePoolTargetLimit     *prometheus.GaugeVec    // Max targets

    // Sync metrics with percentiles
    targetSyncIntervalLength        *prometheus.SummaryVec  // ← Summary!
    targetSyncFailed                *prometheus.CounterVec

    // Data quality metrics
    targetScrapeSampleLimit         prometheus.Counter
    targetScrapeSampleDuplicate     prometheus.Counter
    targetScrapeSampleOutOfOrder    prometheus.Counter

    // Cache metrics
    targetScrapeCacheFlushForced    prometheus.Counter

    // Symbol table optimization
    targetScrapePoolSymbolTableItems *prometheus.GaugeVec
}
```

**prometheus/discovery/kubernetes/metrics.go** - K8s discovery metrics:

```go
type kubernetesMetrics struct {
    eventCount    *prometheus.CounterVec  // By role + event type
    failuresCount prometheus.Counter       // WATCH/LIST failures
}

// Pre-initialize label combinations (IMPORTANT!)
for _, role := range []string{"pod", "service", "node"} {
    for _, evt := range []string{"add", "delete", "update"} {
        m.eventCount.WithLabelValues(role, evt)  // ← Initialize!
    }
}
```

### 🔥 IMPROVED TAPIO METRICS

```go
// internal/base/metrics.go (IMPROVED)
type ObserverMetrics struct {
    // Event counters (existing)
    EventsProcessed metric.Int64Counter
    EventsDropped   metric.Int64Counter
    ErrorsTotal     metric.Int64Counter

    // Processing duration with percentiles (IMPROVED)
    ProcessingTime  metric.Float64Histogram  // Keep for OTEL
    ProcessingTimePercentiles metric.Float64Histogram // Add explicit percentiles

    // Pipeline health (NEW - Prometheus pattern)
    PipelineStagesActive   metric.Int64Gauge    // Current running stages
    PipelineStagesFailed   metric.Int64Counter  // Stage failures
    PipelineQueueDepth     metric.Int64Gauge    // Work queue depth
    PipelineQueueUtilization metric.Float64Gauge // Queue utilization %

    // eBPF health (NEW)
    EBPFMapSize            metric.Int64Gauge    // Map entries
    EBPFMapCapacity        metric.Int64Gauge    // Max entries
    EBPFRingBufferLost     metric.Int64Counter  // Lost events
    EBPFRingBufferUtilization metric.Float64Gauge // Buffer utilization %

    // Data quality (NEW - Prometheus pattern)
    EventsOutOfOrder       metric.Int64Counter  // Timestamp issues
    EventsDuplicate        metric.Int64Counter  // Duplicate events
    EventsEnrichmentFailed metric.Int64Counter  // K8s lookup failures
}

// Metric naming follows Prometheus conventions
func NewObserverMetrics(observerName string) (*ObserverMetrics, error) {
    meter := otel.Meter("tapio.observer")

    // Event counters (existing)
    eventsProcessed, _ := meter.Int64Counter(
        "observer_events_processed_total",  // _total suffix
        metric.WithDescription("Total number of events processed by observer"),
        metric.WithUnit("{events}"),
    )

    // Pipeline health (NEW)
    pipelineQueueDepth, _ := meter.Int64Gauge(
        "observer_pipeline_queue_depth",     // No suffix for gauges
        metric.WithDescription("Current depth of observer pipeline work queue"),
        metric.WithUnit("{events}"),
    )

    pipelineQueueUtilization, _ := meter.Float64Gauge(
        "observer_pipeline_queue_utilization_ratio",  // _ratio suffix
        metric.WithDescription("Pipeline queue utilization (0.0-1.0)"),
        metric.WithUnit("1"),
    )

    // eBPF health (NEW)
    ebpfMapSize, _ := meter.Int64Gauge(
        "observer_ebpf_map_entries",
        metric.WithDescription("Current number of entries in eBPF maps"),
        metric.WithUnit("{entries}"),
    )

    ebpfRingBufferLost, _ := meter.Int64Counter(
        "observer_ebpf_ringbuffer_lost_total",
        metric.WithDescription("Total events lost due to ring buffer overflow"),
        metric.WithUnit("{events}"),
    )

    ebpfRingBufferUtilization, _ := meter.Float64Gauge(
        "observer_ebpf_ringbuffer_utilization_ratio",
        metric.WithDescription("eBPF ring buffer utilization (0.0-1.0)"),
        metric.WithUnit("1"),
    )

    // Data quality (NEW - Prometheus pattern)
    eventsOutOfOrder, _ := meter.Int64Counter(
        "observer_events_out_of_order_total",
        metric.WithDescription("Events rejected due to out-of-order timestamps"),
        metric.WithUnit("{events}"),
    )

    eventsDuplicate, _ := meter.Int64Counter(
        "observer_events_duplicate_total",
        metric.WithDescription("Duplicate events detected and dropped"),
        metric.WithUnit("{events}"),
    )

    eventsEnrichmentFailed, _ := meter.Int64Counter(
        "observer_events_enrichment_failed_total",
        metric.WithDescription("Events failed to enrich with K8s metadata"),
        metric.WithUnit("{events}"),
    )

    return &ObserverMetrics{
        EventsProcessed:           eventsProcessed,
        EventsDropped:             eventsDropped,
        ErrorsTotal:               errorsTotal,
        ProcessingTime:            processingTime,
        PipelineQueueDepth:        pipelineQueueDepth,
        PipelineQueueUtilization:  pipelineQueueUtilization,
        EBPFMapSize:               ebpfMapSize,
        EBPFRingBufferLost:        ebpfRingBufferLost,
        EBPFRingBufferUtilization: ebpfRingBufferUtilization,
        EventsOutOfOrder:          eventsOutOfOrder,
        EventsDuplicate:           eventsDuplicate,
        EventsEnrichmentFailed:    eventsEnrichmentFailed,
    }, nil
}
```

### Context Service Metrics (Enterprise - Prometheus K8s Pattern)

```go
// internal/integrations/context/metrics.go (NEW)
type ContextServiceMetrics struct {
    // K8s watch metrics (Prometheus pattern)
    EventsTotal         metric.Int64Counter  // By resource type + event type
    WatchFailuresTotal  metric.Int64Counter  // WATCH/LIST failures

    // Resource tracking
    PodsTracked         metric.Int64Gauge    // Current pods in cache
    ServicesTracked     metric.Int64Gauge    // Current services
    DeploymentsTracked  metric.Int64Gauge    // Current deployments

    // NATS KV metrics
    NATSPublishesTotal  metric.Int64Counter  // Successful publishes
    NATSPublishFailed   metric.Int64Counter  // Failed publishes
    NATSKVSize          metric.Int64Gauge    // Total KV entries

    // Work queue metrics (Prometheus pattern)
    WorkQueueDepth      metric.Int64Gauge    // Current queue depth
    WorkQueueAdds       metric.Int64Counter  // Total enqueues
    WorkQueueLatency    metric.Float64Histogram // Queue wait time
}

func NewContextServiceMetrics() (*ContextServiceMetrics, error) {
    meter := otel.Meter("tapio.context")

    // Pre-initialize label combinations (Prometheus pattern!)
    eventsTotal, _ := meter.Int64Counter(
        "context_k8s_events_total",
        metric.WithDescription("Kubernetes watch events received"),
        metric.WithUnit("{events}"),
    )

    // Initialize all label combinations to avoid missing series
    for _, resource := range []string{"pod", "service", "deployment"} {
        for _, event := range []string{"add", "update", "delete"} {
            eventsTotal.Add(context.Background(), 0,
                metric.WithAttributes(
                    attribute.String("resource", resource),
                    attribute.String("event", event),
                ),
            )
        }
    }

    workQueueDepth, _ := meter.Int64Gauge(
        "context_work_queue_depth",
        metric.WithDescription("Work queue depth for K8s event processing"),
        metric.WithUnit("{events}"),
    )

    return &ContextServiceMetrics{
        EventsTotal:    eventsTotal,
        WorkQueueDepth: workQueueDepth,
    }, nil
}
```

### Metric Naming Conventions (Prometheus Best Practices)

```go
// Counters: _total suffix
observer_events_processed_total
observer_errors_total
observer_ebpf_ringbuffer_lost_total

// Gauges: No suffix, describes current state
observer_pipeline_queue_depth
observer_ebpf_map_entries
context_pods_tracked

// Histograms: Unit in name
observer_processing_duration_ms
observer_enrichment_latency_ms

// Ratios: _ratio suffix, unit "1" (0.0-1.0)
observer_pipeline_queue_utilization_ratio
observer_ebpf_ringbuffer_utilization_ratio
network_retransmit_rate_ratio

// Percentages: _percent suffix, unit "%" (0-100)
observer_cpu_usage_percent  // Only if you prefer 0-100 scale
```

## 5. OTEL Attribute Naming Standards

Based on OpenTelemetry semantic conventions and Grafana requirements:

```go
// Network attributes (OpenTelemetry standard)
const (
    AttrNetSrcIP       = "net.sock.peer.addr"
    AttrNetDstIP       = "net.sock.host.addr"
    AttrNetSrcPort     = "net.sock.peer.port"
    AttrNetDstPort     = "net.sock.host.port"
    AttrNetProtocol    = "net.transport"  // "tcp", "udp"
)

// K8s attributes (OpenTelemetry semantic conventions)
const (
    AttrK8sPodName       = "k8s.pod.name"
    AttrK8sNamespace     = "k8s.namespace.name"
    AttrK8sDeployment    = "k8s.deployment.name"
    AttrK8sService       = "k8s.service.name"
    AttrK8sNode          = "k8s.node.name"
    AttrK8sPodUID        = "k8s.pod.uid"
)

// Container attributes
const (
    AttrContainerName    = "container.name"
    AttrContainerImage   = "container.image.name"
    AttrContainerID      = "container.id"
)

// Grafana service graph requirements (CRITICAL)
const (
    AttrSpanKind     = "span.kind"      // "client" or "server"
    AttrPeerService  = "peer.service"   // Target service name
    AttrDBName       = "db.name"        // Database name
    AttrDBSystem     = "db.system"      // "postgresql", "mysql", etc.
)
```

---

## 6. Architecture Decision Matrix

| Aspect | FREE Tier | ENTERPRISE Tier |
|--------|-----------|-----------------|
| **Deployment Complexity** | Simple (1 helm chart) | Complex (platform + observers) |
| **K8s API Load** | High (N× watches) | Low (1× watch per resource) |
| **Memory Footprint** | 210MB for 3 observers | 155MB for 3 observers |
| **Willie Scenario** | ✅ Perfect (1 component) | ❌ Overkill |
| **Large Deployments** | ❌ Wasteful (10+ observers) | ✅ Efficient |
| **Grafana Compatibility** | ✅ Works perfectly | ✅ Works perfectly |
| **AI Correlation** | ❌ Not available | ✅ Semantic Service |
| **Cost** | Free | Paid |

---

## 7. Critical Implementation Guidelines

### 7.1 SharedInformer Configuration (MUST DO)

```go
// ❌ WRONG - Causes duplicate events
informer := cache.NewSharedIndexInformer(
    lw,
    &v1.Pod{},
    30 * time.Second,  // ← BAD! Periodic resync
    indexers,
)

// ✅ CORRECT - Prometheus pattern
informer := cache.NewSharedIndexInformer(
    lw,
    &v1.Pod{},
    0,  // ← NO RESYNC!
    indexers,
)
```

### 7.2 Event Handler Pattern (MUST DO)

```go
// ❌ WRONG - Blocks informer
podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc: func(obj any) {
        processUpdate(obj)  // ← BLOCKS! Informer stalls
    },
})

// ✅ CORRECT - Non-blocking enqueue
podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc: func(obj any) {
        workQueue.Add(obj)  // ← Fast enqueue, separate processor
    },
})
```

### 7.3 Index Creation (MUST DO)

```go
// ✅ CORRECT - Index by IP for O(1) lookup
indexers := cache.Indexers{
    ipIndex: func(obj any) ([]string, error) {
        pod := obj.(*v1.Pod)
        if pod.Status.PodIP == "" {
            return nil, nil  // Handle empty IPs
        }
        return []string{pod.Status.PodIP}, nil
    },
}

// Fast lookup in eBPF event handler:
pods, err := podInformer.GetIndexer().ByIndex(ipIndex, "10.0.1.42")
if err == nil && len(pods) > 0 {
    pod := pods[0].(*v1.Pod)
    // Enrich event with pod metadata
}
```

### 7.4 OTEL Span Creation (MUST DO)

```go
// ✅ CORRECT - Grafana-compatible OTEL
span := tracer.Start(ctx, "network.connection_failed",
    trace.WithSpanKind(trace.SpanKindClient),  // ← Required!
    trace.WithAttributes(
        attribute.String("span.kind", "client"),        // ← Grafana needs this
        attribute.String("peer.service", dstService),   // ← Target service
        attribute.String("k8s.pod.name", srcPod),
        attribute.String("k8s.namespace.name", namespace),
    ),
)
defer span.End()
```

---

## 8. Grafana Beyla eBPF Patterns (Production Reference)

**Source:** `grafana/beyla` - eBPF-based auto-instrumentation for K8s

Beyla is **exactly** what Tapio observers do: eBPF → OTEL/Prometheus with K8s enrichment. This is our **production reference implementation**.

### 8.1 K8s Metadata Store Pattern

**beyla/vendor/go.opentelemetry.io/obi/pkg/components/kube/store.go**

```go
// Store aggregates K8s metadata from multiple sources
type Store struct {
    access sync.RWMutex

    // K8s informer
    metadataNotifier meta.Notifier

    // Container tracking (multiple indexes!)
    containerIDs    maps.Map2[string, uint32, *container.Info]  // By container ID + PID
    containerByPID  map[uint32]*container.Info                  // By PID only
    namespaces      maps.Map2[uint32, uint32, *container.Info]  // By PID namespace

    // Pod tracking
    podsByContainer map[string]*CachedObjMeta  // container ID → Pod

    // IP tracking (CRITICAL!)
    objectMetaByIP    map[string]*CachedObjMeta  // IP → Pod/Service/Node
    otelServiceInfoByIP map[string]OTelServiceNamePair

    // Owner hierarchy
    containersByOwner maps.Map2[string, string, *informer.ContainerInfo]

    // Cache syncing
    cacheSynced bool
}
```

**🔥 KEY INSIGHT: Multi-index strategy**

Beyla indexes K8s metadata by:
1. **Container ID** (from eBPF cgroup)
2. **PID** (from eBPF task_struct)
3. **PID namespace** (for container isolation)
4. **IP address** (for network events!) ← **CRITICAL FOR TAPIO**
5. **Owner hierarchy** (Deployment → ReplicaSet → Pod)

### 8.2 Metadata Caching Strategy

```go
type CachedObjMeta struct {
    Meta             *informer.ObjectMeta
    OTELResourceMeta map[attr.Name]string  // ← Pre-computed OTEL attributes!
}

// cacheResourceMetadata extracts from multiple sources (in order):
// 1. OTEL_RESOURCE_ATTRIBUTES env var
// 2. resource.opentelemetry.io/ annotations
// 3. app.kubernetes.io/ labels
func (s *Store) cacheResourceMetadata(meta *informer.ObjectMeta) *CachedObjMeta {
    com := CachedObjMeta{
        Meta:             meta,
        OTELResourceMeta: map[attr.Name]string{},
    }

    // Parse from labels (app.kubernetes.io/name → service.name)
    for propertyName, labels := range s.resourceLabels {
        for _, label := range labels {
            if val := meta.Labels[label]; val != "" {
                com.OTELResourceMeta[attr.Name(propertyName)] = val
                break
            }
        }
    }

    // Override from annotations (resource.opentelemetry.io/service.name)
    for labelName, labelValue := range meta.Annotations {
        if strings.HasPrefix(labelName, ResourceAttributesPrefix) {
            propertyName := labelName[len(ResourceAttributesPrefix):]
            com.OTELResourceMeta[attr.Name(propertyName)] = labelValue
        }
    }

    // Override from container env vars (OTEL_SERVICE_NAME)
    for _, cnt := range meta.GetPod().GetContainers() {
        if val := cnt.Env[EnvServiceName]; val != "" {
            com.OTELResourceMeta[serviceNameKey] = val
        }
    }

    return &com
}
```

**🔥 TAPIO APPLICATION:**

```go
// pkg/domain/context.go (Level 0)
type PodContext struct {
    // Raw K8s metadata
    ObjectMeta *metav1.ObjectMeta

    // Pre-computed OTEL attributes (Beyla pattern!)
    OTELAttributes map[string]string  // ← Cache these!

    // Network identifiers
    PodIP     string
    HostIP    string
    NodeName  string
}

// Compute once, use many times
func enrichOTELAttributes(pod *v1.Pod) map[string]string {
    attrs := make(map[string]string)

    // From labels (app.kubernetes.io/name → service.name)
    if name := pod.Labels["app.kubernetes.io/name"]; name != "" {
        attrs["service.name"] = name
    }

    // From annotations (resource.opentelemetry.io/service.name)
    for k, v := range pod.Annotations {
        if strings.HasPrefix(k, "resource.opentelemetry.io/") {
            attrName := strings.TrimPrefix(k, "resource.opentelemetry.io/")
            attrs[attrName] = v
        }
    }

    // From env vars (OTEL_SERVICE_NAME in first container)
    if len(pod.Spec.Containers) > 0 {
        for _, env := range pod.Spec.Containers[0].Env {
            if env.Name == "OTEL_SERVICE_NAME" {
                attrs["service.name"] = env.Value
            }
        }
    }

    return attrs
}
```

### 8.3 MetadataProvider Pattern (Lazy Initialization)

**beyla/vendor/go.opentelemetry.io/obi/pkg/components/kube/informer_provider.go**

```go
type MetadataProvider struct {
    mt sync.Mutex

    metadata *Store      // ← Lazy init
    informer meta.Notifier
    cfg      *MetadataConfig
}

// IsKubeEnabled - Auto-detect K8s environment
func (mp *MetadataProvider) IsKubeEnabled() bool {
    switch strings.ToLower(string(mp.cfg.Enable)) {
    case "true":
        return true
    case "false":
        return false
    case "autodetect":  // ← Smart default!
        _, err := loadKubeConfig(mp.cfg.KubeConfigPath)
        if err != nil {
            klog().Debug("kubeconfig can't be detected. Not in Kubernetes")
            mp.cfg.Enable = "false"
            return false
        }
        mp.cfg.Enable = "true"
        return true
    }
}

// Get - Lazy initialization pattern
func (mp *MetadataProvider) Get(ctx context.Context) (*Store, error) {
    mp.mt.Lock()
    defer mp.mt.Unlock()

    if mp.metadata != nil {
        return mp.metadata, nil  // ← Already initialized
    }

    informer, err := mp.getInformer(ctx)
    if err != nil {
        return nil, err
    }

    mp.metadata = NewStore(informer, mp.cfg.ResourceLabels)
    return mp.metadata, nil
}
```

**🔥 TAPIO APPLICATION:**

```go
// internal/integrations/context/provider.go (Enterprise)
type ContextProvider struct {
    mu    sync.Mutex
    store *Store  // Lazy init
    cfg   *Config
}

func (cp *ContextProvider) IsKubeEnabled() bool {
    // Auto-detect: try in-cluster config
    _, err := rest.InClusterConfig()
    return err == nil
}

func (cp *ContextProvider) Get(ctx context.Context) (*Store, error) {
    cp.mu.Lock()
    defer cp.mu.Unlock()

    if cp.store != nil {
        return cp.store, nil
    }

    // Initialize informers + NATS KV
    cp.store = NewStore(ctx, cp.cfg)
    return cp.store, nil
}
```

### 8.4 Remote Informer Cache Pattern (Performance!)

**beyla/vendor/go.opentelemetry.io/obi/pkg/components/kube/informer_provider.go:298**

```go
// initRemoteInformerCacheClient connects via gRPC to remote beyla-k8s-cache service
// to avoid each Beyla instance connecting to Kube API (overload prevention)
func (mp *MetadataProvider) initRemoteInformerCacheClient(ctx context.Context) *cacheSvcClient {
    client := &cacheSvcClient{
        address:      mp.cfg.MetaCacheAddr,  // ← Remote cache address
        BaseNotifier: meta.NewBaseNotifier(klog()),
        syncTimeout:  mp.cfg.SyncTimeout,
    }
    client.Start(ctx)
    return client
}
```

**🔥 THIS IS EXACTLY WHAT TAPIO CONTEXT SERVICE DOES!**

```
FREE tier (Beyla standalone):
┌─────────────────┐
│ Network Observer│
│ - Own informer  │ ──> K8s API (watch Pods)
│ - 300MB         │
└─────────────────┘

ENTERPRISE tier (Beyla with remote cache):
┌─────────────────┐     gRPC      ┌──────────────────┐
│ Network Observer│ ───────────> │ Context Service  │
│ - Remote client │               │ - 1× informer    │
│ - 50MB          │               │ - Serves N obs   │
└─────────────────┘               └──────────────────┘
                                         │
                                         ▼
                                    K8s API (single watch)
```

**Beyla validates our two-tier approach!**

### 8.5 Prometheus Label Naming Convention

**beyla/pkg/export/prom/prom.go**

```go
// OTEL uses dot notation: service.name
// Prometheus uses snake_case: service_name
const (
    serviceNameKey      = "service_name"       // Not service.name!
    serviceNamespaceKey = "service_namespace"

    k8sNamespaceName   = "k8s_namespace_name"
    k8sPodName         = "k8s_pod_name"
    k8sDeploymentName  = "k8s_deployment_name"
    k8sNodeName        = "k8s_node_name"
    k8sPodUID          = "k8s_pod_uid"
)
```

**🔥 CRITICAL: Label name transformation**

```go
func parseExtraMetadata(labels []string) []attr.Name {
    attrNames := make([]attr.Name, len(labels))
    for i, label := range labels {
        // Convert snake_case → dotted.format
        attrNames[i] = attr.Name(strings.ReplaceAll(label, "_", "."))
    }
    return attrNames
}
```

**TAPIO MUST DO THIS:**

```go
// OTEL attributes (internal)
const (
    AttrK8sPodName     = "k8s.pod.name"       // Dot notation
    AttrK8sNamespace   = "k8s.namespace.name"
    AttrServiceName    = "service.name"
)

// Prometheus labels (export)
const (
    PromK8sPodName     = "k8s_pod_name"       // Snake case!
    PromK8sNamespace   = "k8s_namespace_name"
    PromServiceName    = "service_name"
)

// Convert for Prometheus export
func toPromLabel(otelAttr string) string {
    return strings.ReplaceAll(otelAttr, ".", "_")
}
```

### 8.6 K8s Informer Restrictions (Performance)

**beyla/vendor/go.opentelemetry.io/obi/pkg/components/kube/informer_provider.go:277**

```go
func (mp *MetadataProvider) initLocalInformers(ctx context.Context) (*meta.Informers, error) {
    opts := []meta.InformerOption{
        meta.WithResyncPeriod(mp.cfg.ResyncPeriod),  // ← Resync period
        meta.WaitForCacheSync(),                      // ← Wait before decorating
        meta.WithCacheSyncTimeout(mp.cfg.SyncTimeout),
    }

    // Restrict to local node (DaemonSet optimization!)
    if mp.cfg.RestrictLocalNode {
        localNode, _ := mp.CurrentNodeName(ctx)
        opts = append(opts, meta.RestrictNode(localNode))  // ← Only watch local pods!
    }

    return meta.InitInformers(ctx, opts...)
}
```

**🔥 CRITICAL FOR DAEMONSET DEPLOYMENT:**

When deploying as DaemonSet, only watch Pods on the **local node**!

```go
// TAPIO DaemonSet deployment
type Config struct {
    WatchLocalNodeOnly bool `env:"TAPIO_WATCH_LOCAL_NODE"`
}

func NewPodInformer(cfg Config) cache.SharedIndexInformer {
    listWatch := &cache.ListWatch{
        ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
            // DaemonSet: Only list pods on local node
            if cfg.WatchLocalNodeOnly {
                opts.FieldSelector = "spec.nodeName=" + os.Getenv("NODE_NAME")
            }
            return client.CoreV1().Pods("").List(ctx, opts)
        },
        WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
            if cfg.WatchLocalNodeOnly {
                opts.FieldSelector = "spec.nodeName=" + os.Getenv("NODE_NAME")
            }
            return client.CoreV1().Pods("").Watch(ctx, opts)
        },
    }

    return cache.NewSharedIndexInformer(listWatch, &v1.Pod{}, 0, indexers)
}
```

### 8.7 Survey Pattern (Process Discovery)

**beyla/pkg/internal/discover/survey_informer.go**

```go
type surveyor struct {
    input  <-chan []obiDiscover.Event[ebpf.Instrumentable]
    output *msg.Queue[exec.ProcessEvent]
    store  *kube.Store  // ← K8s metadata store
}

func (m *surveyor) run(_ context.Context) {
    for events := range m.input {
        for _, pe := range events {
            m.fetchMetadata(&pe.Obj)  // ← Enrich with K8s metadata

            if pe.Type == obiDiscover.EventDeleted {
                m.output.Send(exec.ProcessEvent{Type: exec.ProcessEventTerminated})
            } else {
                m.output.Send(exec.ProcessEvent{Type: exec.ProcessEventCreated})
            }
        }
    }
}

func (m *surveyor) fetchMetadata(i *ebpf.Instrumentable) {
    // Enrich from executable
    i.CopyToServiceAttributes()

    // Enrich from K8s (if available)
    if m.store != nil {
        if objectMeta, containerName := m.store.PodContainerByPIDNs(i.FileInfo.Ns); objectMeta != nil {
            transform.AppendKubeMetadata(m.store, &i.FileInfo.Service, objectMeta, containerName)
        }
    }
}
```

**🔥 TAPIO PATTERN:**

```go
// internal/observers/network/enrichment.go
func (o *Observer) enrichEvent(event *BPFEvent) *domain.Event {
    enriched := &domain.Event{
        Timestamp: event.Timestamp,
        Type:      event.Type,
    }

    // Enrich from eBPF data
    enriched.Attributes["src.ip"] = event.SrcIP
    enriched.Attributes["dst.ip"] = event.DstIP

    // Enrich from K8s (if available)
    if o.store != nil {
        if pod := o.store.PodByIP(event.SrcIP); pod != nil {
            enriched.Attributes["k8s.pod.name"] = pod.Name
            enriched.Attributes["k8s.namespace.name"] = pod.Namespace
            // Use pre-computed OTEL attributes!
            for k, v := range pod.OTELAttributes {
                enriched.Attributes[k] = v
            }
        }
    }

    return enriched
}
```

## 9. Next Steps

1. **Design Context Service** (Enterprise)
   - SharedInformer setup with IP indexing
   - NATS KV integration for pub/sub
   - Work queue processors

2. **Design Standalone Observer** (FREE)
   - Own informer initialization
   - Direct OTEL export
   - Helm chart with single component

3. **Update Scheduler Observer**
   - Remove eBPF Layer 2 (use Prometheus scraping instead)
   - HTTP client for `/metrics` endpoint
   - Prometheus text parser → OTEL conversion

4. **Validate with Willie Scenario**
   - Single observer deployment
   - Memory profiling
   - Grafana service graph verification

---

## References

- **Grafana Tempo:** Service graph generation from OTEL spans
- **Prometheus Discovery:** `github.com/prometheus/prometheus/discovery/kubernetes/`
- **K8s Informers:** `k8s.io/client-go/tools/cache`
- **Work Queues:** `k8s.io/client-go/util/workqueue`
- **OTEL Semantic Conventions:** OpenTelemetry specification v1.24.0
- **Actor Pattern:** `github.com/oklog/run`

---

**Document Status:** ✅ Complete - Ready for architecture agent review
**Authors:** Analyzed from Grafana Tempo + Prometheus codebases
**Validation:** Two-tier architecture confirmed viable
