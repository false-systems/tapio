# Learnings from Grafana Beyla (OpenTelemetry eBPF Instrumentation)

## Overview

**Beyla** is Grafana's zero-code automatic instrumentation tool using eBPF and OpenTelemetry. It's been donated to CNCF as **OpenTelemetry eBPF Instrumentation (OBI)**. After exploring the codebase, here are the key architectural patterns and techniques we can adopt for Tapio.

## What Beyla Does

- **Auto-instrumentation**: Zero code changes, attach to running processes
- **Multi-language support**: Go, Java, .NET, NodeJS, Python, Ruby, Rust
- **Protocol coverage**: HTTP/S, gRPC, SQL, Redis, Kafka, MongoDB
- **Observability signals**: Traces (spans) + RED metrics (Rate, Errors, Duration)
- **Discovery**: Automatic process discovery by port, executable name, or PID
- **Export**: Prometheus metrics + OpenTelemetry traces

## Key Architecture Patterns

### 1. **Pipeline-Based Architecture (Swarm Pattern)**

Beyla uses a "swarm" pattern - multiple independent pipelines running concurrently:

```go
// From pkg/internal/pipe/instrumenter.go

type swarm.Instancer struct {
    instances []swarm.InstanceFunc
}

func Build(ctx context.Context, config *Config) (*swarm.Runner, error) {
    swi := &swarm.Instancer{}

    // Add pipeline 1: OBI instrumentation
    swi.Add(func(ctx context.Context) (swarm.RunFunc, error) {
        obiSwarm, err := pipe.Build(ctx, config.AsOBI(), ...)
        return obiSwarm.Start, nil
    })

    // Add pipeline 2: Alloy traces receiver
    swi.Add(alloy.TracesReceiver(ctxInfo, config))

    // Add pipeline 3: Process metrics collector
    swi.Add(ProcessMetricsSwarmInstancer(ctxInfo, config))

    // Add pipeline 4: Cluster connectors (inter-cluster topology)
    clusterConnectorsSubpipeline(swi, ctxInfo, config)

    return swi.Instance(ctx)  // Returns runnable swarm
}
```

**Why this matters for Tapio**:
- **Multiple observers run independently** (network, scheduler, container, etc.)
- **Each observer is a swarm member** - isolated failure domains
- **Easy to add new observers** without touching existing code
- **Graceful degradation** - one observer fails, others continue

**Tapio implementation idea**:
```go
// internal/observers/swarm/swarm.go
type ObserverSwarm struct {
    instances []ObserverInstance
}

func NewSwarm() *ObserverSwarm {
    swarm := &ObserverSwarm{}

    // Add network observer
    swarm.Add(func(ctx context.Context) error {
        obs, _ := network.NewNetworkObserver("network", cfg)
        return obs.Start(ctx)
    })

    // Add scheduler observer
    swarm.Add(func(ctx context.Context) error {
        obs, _ := scheduler.NewSchedulerObserver("scheduler", cfg)
        return obs.Start(ctx)
    })

    return swarm
}

func (s *ObserverSwarm) Run(ctx context.Context) error {
    errCh := make(chan error, len(s.instances))

    for _, inst := range s.instances {
        go func(instance ObserverInstance) {
            if err := instance(ctx); err != nil {
                errCh <- err
            }
        }(inst)
    }

    select {
    case <-ctx.Done():
        return ctx.Err()
    case err := <-errCh:
        return err
    }
}
```

### 2. **Message Queue Between Pipeline Stages**

Beyla uses typed message queues for inter-stage communication:

```go
// From go.opentelemetry.io/obi/pkg/pipe/msg

type Queue[T any] struct {
    ch chan T
}

func NewQueue[T any](bufferLen int) *Queue[T] {
    return &Queue[T]{ch: make(chan T, bufferLen)}
}

func (q *Queue[T]) Subscribe(name string) <-chan T {
    return q.ch
}

func (q *Queue[T]) Publish(item T) {
    q.ch <- item
}

// Usage:
tracesCh := msg.NewQueue[[]request.Span](1000)
processEventsCh := msg.NewQueue[exec.ProcessEvent](1000)

// Stage 1 produces, Stage 2 consumes
stage1Output := tracesCh
stage2Input := tracesCh.Subscribe("stage2")
```

**Why this matters for Tapio**:
- **Type-safe inter-observer communication**
- **Backpressure handling** (buffered channels)
- **Multiple subscribers** to same event stream
- **Decoupled stages** - easy to test independently

**Tapio correlation use case**:
```go
// Scheduler observer publishes scheduling events
schedulingEvents := msg.NewQueue[SchedulingEvent](1000)

// Network observer subscribes to correlate with connections
networkObs.Subscribe(schedulingEvents, "network-correlation")

// Correlation engine subscribes to build graph
correlationEngine.Subscribe(schedulingEvents, "correlation")
```

### 3. **Process Discovery Matcher**

Beyla's process discovery is sophisticated - match by multiple criteria:

```go
// From pkg/internal/discover/survey.go

type Matcher struct {
    Criteria        []services.Selector  // What to instrument
    ExcludeCriteria []services.Selector  // What to exclude
    ProcessHistory  map[PID]ProcessMatch
    Namespace       uint32               // Network namespace
    HasHostPidAccess bool
}

type Selector struct {
    Name          string  // Executable name glob: "my-app-*"
    OpenPorts     []int   // Listen ports: [8080, 9090]
    Path          string  // Binary path glob: "/usr/bin/python*"
    Namespace     string  // K8s namespace
    PodName       string  // K8s pod name
}

func (m *Matcher) Run(ctx context.Context) {
    for {
        select {
        case events := <-m.Input:
            for _, event := range events {
                if m.matches(event.Attrs, m.Criteria) &&
                   !m.matches(event.Attrs, m.ExcludeCriteria) {
                    m.Output <- ProcessMatch{PID: event.PID, ...}
                }
            }
        case <-ctx.Done():
            return
        }
    }
}
```

**Why this matters for Tapio**:
- **Flexible targeting** - instrument specific services
- **K8s-aware** - target by namespace/pod
- **Exclude patterns** - avoid instrumenting system services
- **Network namespace isolation** - container-aware

**Tapio scheduler observer use case**:
```go
// Only observe scheduler for specific namespaces
cfg := SchedulerConfig{
    Namespaces: []string{"production", "staging"},  // Skip kube-system
    Schedulers: []string{"default-scheduler", "custom-scheduler"},
}
```

### 4. **Rewritable Constants in eBPF**

Beyla rewrites eBPF constants at load time (no recompilation needed):

```go
// From vendor/.../ebpf/tracer.go

// In eBPF C code:
// volatile const u32 sampling = 1;       // Default value
// volatile const u8 trace_messages = 0;

spec, err := LoadNet()

// Rewrite constants based on config
err := convenience.RewriteConstants(spec, map[string]any{
    "sampling":      uint32(100),  // Sample 1 in 100 packets
    "trace_messages": uint8(1),     // Enable debug tracing
})

spec.LoadAndAssign(&objects, nil)
```

**Why this matters for Tapio**:
- **No eBPF recompilation** for config changes
- **Runtime tuning** - sampling rates, thresholds
- **Debug mode toggle** - enable/disable verbose logging

**Tapio network observer use case**:
```c
// network_monitor.c
volatile const __u32 rtt_spike_threshold_pct = 200;  // 2x baseline = spike
volatile const __u32 retransmit_threshold = 5;       // 5% retransmit = congestion
volatile const __u8 debug_mode = 0;

// Go side:
err := convenience.RewriteConstants(spec, map[string]any{
    "rtt_spike_threshold_pct": uint32(cfg.RTTSpikeThreshold),
    "retransmit_threshold":    uint32(cfg.RetransmitThreshold),
    "debug_mode":              uint8(cfg.DebugMode ? 1 : 0),
})
```

### 5. **Multi-Backend Export**

Beyla supports multiple export backends simultaneously:

```
┌─────────────────────┐
│  eBPF Instrumentation│
└──────────┬──────────┘
           │
           ├──▶ Prometheus /metrics endpoint
           ├──▶ OpenTelemetry gRPC (traces + metrics)
           ├──▶ Alloy (Grafana Agent)
           └──▶ Connection Topology Spans
```

```go
// Can enable multiple exporters:
config.Prometheus.Port = 9400        // Prometheus scrape endpoint
config.Traces.Endpoint = "localhost:4317"  // OTLP gRPC
config.TracesReceiver.Enabled = true       // Alloy integration
```

**Why this matters for Tapio**:
- **Flexible backend integration** - Prometheus OR OTLP OR both
- **Migration support** - start with Prometheus, add OTLP later
- **Vendor neutrality** - not locked to one backend

**Tapio configuration**:
```yaml
export:
  prometheus:
    enabled: true
    port: 9090
  otlp:
    enabled: true
    endpoint: "localhost:4317"
  stdout:
    enabled: true  # For debugging
```

### 6. **K8s Metadata Enrichment**

Beyla enriches traces with K8s metadata:

```go
// Check if IP belongs to K8s cluster
store, _ := ctxInfo.K8sInformer.Get(ctx)

for _, span := range spans {
    if meta := store.ObjectMetaByIP(span.Peer.Address); meta != nil {
        span.K8s = K8sMetadata{
            Namespace: meta.Namespace,
            PodName:   meta.Name,
            ClusterID: meta.ClusterID,
        }
    }
}
```

**Why this matters for Tapio**:
- **Correlation with K8s resources** - scheduler events → pod metadata
- **Cross-cluster topology** - detect inter-cluster communication
- **Entity enrichment** - IP → Pod → Deployment → Service

**Tapio enrichment pipeline**:
```go
// Scheduler observer emits: Pod scheduled to Node
scheduledEvent := SchedulingEvent{
    PodUID: "abc-123",
    NodeName: "node-1",
}

// Enrichment service adds K8s metadata
enriched := EnrichWithK8s(scheduledEvent, k8sInformer)
// Result:
// {
//   PodUID: "abc-123",
//   Namespace: "production",
//   Deployment: "frontend",
//   Service: "web",
//   NodeName: "node-1",
//   NodeZone: "us-west-1a",
// }
```

### 7. **Inter-Cluster Topology Detection**

Beyla detects when traffic leaves the cluster:

```go
// Filter for external (non-cluster) connections
swi.Add(traces.SelectExternal(
    func(ip string) bool {
        return store.ObjectMetaByIP(ip) == nil  // No K8s metadata = external
    },
    inputQueue,
    externalQueue,
))

// Emit "connector" spans for inter-cluster service graph
swi.Add(otel.ConnectionSpansExport(ctx, config, externalQueue))
```

**Why this matters for Tapio**:
- **Multi-cluster observability** - detect cross-cluster calls
- **External dependencies** - track calls to cloud APIs, databases
- **Service graph completeness** - connections to services outside cluster

**Tapio use case**:
```go
// Network observer sees TCP connection to external IP
if !k8sInformer.IsClusterIP(dstIP) {
    EmitExternalConnectionEvent(ExternalConnection{
        SrcPod: "frontend-abc",
        DstIP:  "192.0.2.10",  // External IP
        DstPort: 5432,          // PostgreSQL
        Protocol: "tcp",
    })
}
```

### 8. **TC (Traffic Control) Manager**

Beyla attaches eBPF programs to network interfaces using TC hooks:

```go
type TCManager struct {
    programs []TCProgram
}

type TCProgram struct {
    Name       string
    Program    *ebpf.Program
    Attachment AttachmentType  // Ingress or Egress
}

func (m *TCManager) AddProgram(name string, prog *ebpf.Program, attach AttachmentType) {
    m.programs = append(m.programs, TCProgram{name, prog, attach})
}

func (m *TCManager) AttachToInterfaces(ifaces []string) error {
    for _, iface := range ifaces {
        for _, prog := range m.programs {
            if prog.Attachment == AttachmentIngress {
                attachIngress(iface, prog.Program)
            } else {
                attachEgress(iface, prog.Program)
            }
        }
    }
}
```

**Why this matters for Tapio**:
- **Network-level observability** - see all traffic (not just traced apps)
- **Container network visibility** - attach to veth pairs
- **Bidirectional monitoring** - ingress + egress

**Tapio network observer enhancement**:
```go
// Current: Tracepoint-based (inet_sock_set_state, tcp_retransmit_skb)
// Enhancement: Add TC hooks for full packet visibility

type NetworkObserver struct {
    tracepointProgs []TracepointProgram
    tcManager       *TCManager  // NEW: TC-based packet capture
}

func (n *NetworkObserver) Start(ctx context.Context) error {
    // Existing tracepoint attachment
    n.attachTracepoints()

    // NEW: Attach TC programs for packet-level visibility
    if n.config.EnableTC {
        n.tcManager.AttachToInterfaces([]string{"eth0", "veth+"})
    }
}
```

### 9. **Namespace-Aware Operation**

Beyla handles network namespaces correctly:

```go
// Get Beyla's own network namespace
beylaNamespace, _ := ebpfcommon.FindNetworkNamespace(int32(os.Getpid()))

// Check if running with host PID access
hasHostAccess := ebpfcommon.HasHostPidAccess()

// Match processes only in same namespace (or if has host access)
func (m *Matcher) matches(proc ProcessAttrs) bool {
    if m.HasHostPidAccess {
        return true  // Can instrument any process
    }
    return proc.Namespace == m.Namespace  // Same namespace only
}
```

**Why this matters for Tapio**:
- **Container isolation** - only observe processes we have access to
- **Security boundary** - respect namespace boundaries
- **Multi-tenancy** - avoid observing other tenants' processes

**Tapio security check**:
```go
func (o *BaseObserver) CanObservePID(pid int32) bool {
    targetNs, _ := FindNetworkNamespace(pid)
    observerNs, _ := FindNetworkNamespace(int32(os.Getpid()))

    if HasHostPidAccess() {
        return true  // Privileged mode
    }

    return targetNs == observerNs  // Same namespace only
}
```

### 10. **Language-Specific Instrumentation**

Beyla has specialized instrumentation for each language:

```
pkg/internal/
├── goexec/       # Go uprobes (function entry/exit)
├── nodejs/       # Node.js async hooks
├── sqlprune/     # SQL query sanitization
└── traces/       # Trace span generation
```

**Go instrumentation** - uprobes on compiled binaries:
```go
// Attach to Go HTTP handler functions
uprobe, _ := link.Uprobe("main.handlerFunc", prog, &link.UprobeOptions{
    Address: symbolAddr,
})
```

**Why this matters for Tapio**:
- **Go-specific optimizations** - Tapio is likely Go-heavy
- **No dependency on tracepoints** - works on any kernel
- **Function-level visibility** - see internal function calls

**Tapio use case** (if we add application-level tracing):
```go
// Attach to user's Go application functions
type AppTracer struct {
    uprobes map[string]*link.Link
}

func (t *AppTracer) AttachToFunction(binary, symbol string) error {
    addr, _ := getSymbolAddress(binary, symbol)
    prog, _ := t.loadProbe()
    uprobe, _ := link.Uprobe(symbol, prog, &link.UprobeOptions{
        Address: addr,
    })
    t.uprobes[symbol] = uprobe
}
```

## Architectural Lessons for Tapio

### 1. **Adopt Swarm Pattern for Observers**

**Current Tapio**: Monolithic observer management
```go
// Current approach (monolithic)
func main() {
    networkObs := network.NewObserver()
    schedulerObs := scheduler.NewObserver()

    go networkObs.Start()
    go schedulerObs.Start()

    // Manual error handling, no coordination
}
```

**Beyla-inspired approach** (swarm):
```go
// New approach (swarm)
func main() {
    swarm := observers.NewSwarm()

    swarm.Add(network.NewObserver)
    swarm.Add(scheduler.NewObserver)
    swarm.Add(container.NewObserver)

    if err := swarm.Run(ctx); err != nil {
        // Centralized error handling
    }
}
```

**Benefits**:
- **Graceful degradation** - one observer fails, others continue
- **Easy to add observers** - just `swarm.Add()`
- **Centralized lifecycle** - start/stop coordination

### 2. **Message Queues for Correlation**

**Current approach**: Direct function calls or shared maps
```go
// Current (tightly coupled)
networkObs.OnSchedulingEvent(schedulingEvent)
```

**Beyla-inspired** (message queues):
```go
// New (decoupled)
schedulingEvents := msg.NewQueue[SchedulingEvent](1000)
networkEvents := msg.NewQueue[NetworkEvent](1000)

schedulerObs.SetOutput(schedulingEvents)
networkObs.SetOutput(networkEvents)

// Correlation engine subscribes to both
correlationEngine.Subscribe(schedulingEvents, "scheduler")
correlationEngine.Subscribe(networkEvents, "network")
```

**Benefits**:
- **Testability** - mock message queues easily
- **Backpressure** - buffered channels prevent overload
- **Multiple consumers** - many subscribers to same stream

### 3. **Rewritable eBPF Constants**

**Current approach**: Hardcoded thresholds
```c
// Current (requires recompilation)
#define RTT_SPIKE_THRESHOLD 200  // 2x baseline
```

**Beyla-inspired** (runtime configuration):
```c
// New (runtime configurable)
volatile const __u32 rtt_spike_threshold = 200;

// Go side:
RewriteConstants(spec, map[string]any{
    "rtt_spike_threshold": cfg.RTTSpikeThreshold,
})
```

**Benefits**:
- **No recompilation** for tuning
- **A/B testing** - different thresholds per deployment
- **Debug mode** - toggle verbose logging

### 4. **K8s Metadata Enrichment Pipeline**

**Current approach**: Direct K8s API calls in observer
```go
// Current (in observer, blocking)
pod, _ := clientset.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
```

**Beyla-inspired** (enrichment stage):
```go
// New (separate enrichment stage)
type EnrichmentStage struct {
    k8sInformer K8sInformer
    input       <-chan Event
    output      chan<- EnrichedEvent
}

func (e *EnrichmentStage) Run(ctx context.Context) {
    for event := range e.input {
        if meta := e.k8sInformer.Get(event.PodUID); meta != nil {
            enriched := event.WithMetadata(meta)
            e.output <- enriched
        }
    }
}
```

**Benefits**:
- **Non-blocking observers** - enrichment happens downstream
- **Caching** - informer cache, no API calls
- **Retry logic** - enrichment stage can retry

### 5. **Multi-Backend Export**

**Current approach**: Single export target
```go
// Current
exporter := prometheus.NewExporter(cfg)
```

**Beyla-inspired** (multi-backend):
```go
// New
type ExportSwarm struct {
    exporters []Exporter
}

swarm.Add(prometheus.NewExporter(cfg.Prometheus))
swarm.Add(otlp.NewExporter(cfg.OTLP))
swarm.Add(stdout.NewExporter(cfg.Stdout))
```

**Benefits**:
- **Migration support** - gradually move Prometheus → OTLP
- **Multiple consumers** - different teams use different backends
- **Vendor neutrality** - not locked in

## Specific Features to Adopt

### 1. **Process Discovery for Scheduler Observer**

```go
// Target specific schedulers
type SchedulerDiscovery struct {
    TargetProcesses []string  // ["kube-scheduler", "custom-scheduler"]
    ExcludeProcesses []string  // ["test-scheduler"]
    TargetNamespaces []string  // ["kube-system"]
}

func (d *SchedulerDiscovery) FindSchedulers(ctx context.Context) []ProcessMatch {
    // Scan /proc for matching processes
    // Return PID, namespace, binary path
}
```

### 2. **TC-Based Network Observability**

Add packet-level visibility alongside tracepoints:

```go
type NetworkObserver struct {
    // Existing tracepoints
    tracepointProgs []TracepointProgram

    // NEW: TC packet capture
    tcManager *TCManager
}

// Attach to eth0 (host) and veth+ (containers)
tcManager.AddProgram("tc/packet_monitor", prog, AttachmentIngress)
tcManager.AttachToInterfaces([]string{"eth0", "veth+"})
```

### 3. **Inter-Cluster Detection**

```go
// Detect connections leaving K8s cluster
func (n *NetworkObserver) classifyConnection(evt NetworkEvent) ConnectionType {
    if k8sInformer.IsClusterIP(evt.DstIP) {
        return IntraCluster
    }
    if isCloudProvider(evt.DstIP) {
        return CloudAPI  // AWS, GCP, Azure
    }
    return External
}

// Emit topology events for inter-cluster connections
if connType == External {
    EmitTopologyEvent(ExternalConnection{...})
}
```

### 4. **Runtime Configuration via eBPF Constants**

```yaml
# config.yaml
network_observer:
  rtt_spike_threshold_pct: 200
  retransmit_threshold_pct: 5
  debug_mode: false
  sampling_rate: 10  # Sample 1 in 10 packets
```

```c
// network_monitor.c
volatile const __u32 rtt_spike_threshold_pct = 200;
volatile const __u32 retransmit_threshold_pct = 5;
volatile const __u8 debug_mode = 0;
volatile const __u32 sampling_rate = 1;
```

```go
// observer_ebpf.go
RewriteConstants(spec, map[string]any{
    "rtt_spike_threshold_pct": cfg.RTTSpikeThreshold,
    "retransmit_threshold_pct": cfg.RetransmitThreshold,
    "debug_mode": boolToU8(cfg.DebugMode),
    "sampling_rate": cfg.SamplingRate,
})
```

## Performance Optimizations from Beyla

### 1. **Ring Buffer Size Tuning**

```go
// Resize ring buffer based on expected load
spec.Maps["events"].MaxEntries = uint32(cfg.RingBufferSize)

// High throughput: 1MB+
// Low throughput: 256KB (default)
```

### 2. **Sampling for High Volume**

```c
// Sample 1 in N events to reduce overhead
volatile const __u32 sampling_rate = 10;

if (bpf_get_prandom_u32() % sampling_rate != 0) {
    return 0;  // Skip this event
}
```

### 3. **Per-CPU Event Processing**

```go
// Process ring buffer events per-CPU for parallelism
for cpu := 0; cpu < numCPUs; cpu++ {
    go func(cpuID int) {
        reader := ringbuf.NewReaderForCPU(objects.Events, cpuID)
        for {
            record, _ := reader.Read()
            processEvent(record)
        }
    }(cpu)
}
```

## Summary: Top 5 Beyla Patterns for Tapio

1. **Swarm Architecture** - Independent observer pipelines with graceful degradation
2. **Message Queues** - Typed channels for inter-observer correlation
3. **Rewritable eBPF Constants** - Runtime tuning without recompilation
4. **K8s Enrichment Pipeline** - Separate stage for metadata enrichment
5. **Multi-Backend Export** - Prometheus + OTLP simultaneously

## Implementation Roadmap

### Phase 1: Swarm Architecture (Week 1)
- Create `internal/observers/swarm` package
- Convert existing observers to swarm members
- Centralized error handling and lifecycle

### Phase 2: Message Queues (Week 2)
- Implement `pkg/pipe/msg` package
- Refactor observers to use typed queues
- Build correlation engine as queue subscriber

### Phase 3: Runtime Config (Week 3)
- Add `volatile const` to eBPF programs
- Implement `RewriteConstants()` helper
- Make thresholds/sampling configurable

### Phase 4: Enrichment Pipeline (Week 4)
- Separate K8s enrichment stage
- Non-blocking metadata fetching
- Retry/fallback logic

### Phase 5: Multi-Export (Week 5)
- Prometheus exporter (existing)
- OTLP exporter (new)
- Stdout exporter (debug)

## References

- [Grafana Beyla GitHub](https://github.com/grafana/beyla)
- [OpenTelemetry eBPF Instrumentation](https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation)
- [Beyla Documentation](https://grafana.com/docs/beyla/)
- [TC (Traffic Control) eBPF](https://docs.kernel.org/bpf/prog_types.html#bpf-prog-type-sched-cls)
