# Scheduler Observer Design

## Purpose

Monitor Kubernetes scheduler behavior to provide deep observability into pod scheduling decisions, latency, failures, and resource allocation patterns. This observer bridges the gap between "pod was created" and "pod is running" - the critical path where scheduling decisions directly impact application performance.

## Problem Statement

Current K8s observability tools show:
- ✅ Pod created (timestamp)
- ✅ Pod running (timestamp)
- ❌ **WHY** scheduling took 30 seconds
- ❌ **WHICH** nodes were rejected and why
- ❌ **WHAT** plugins caused delays
- ❌ **HOW** preemption decisions were made

**Tapio Scheduler Observer** fills this gap with kernel-level + API-level observability.

## Architecture

### Two-Layer Approach

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 1: K8s API Watch (client-go Informers)                   │
│  - Pod events (Pending → Scheduled → Running)                   │
│  - Node events (resource changes, taints)                       │
│  - Event API (scheduler failure reasons)                        │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Layer 2: eBPF Tracing (optional, requires scheduler pod access)│
│  - kube-scheduler internal function tracing                     │
│  - Plugin execution latency per extension point                 │
│  - Memory allocations during scheduling                         │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Correlation Engine                                             │
│  - Match Pod UID → Scheduling attempts                          │
│  - Calculate end-to-end latency (Created → Bound)               │
│  - Identify root cause (Filter failure, PostFilter, timeout)    │
└─────────────────────────────────────────────────────────────────┘
```

## Layer 1: K8s API Watch (Baseline Implementation)

### Data Sources

#### 1. Pod Informer
```go
// Watch Pod lifecycle transitions
type PodSchedulingEvent struct {
    PodUID        string
    Namespace     string
    Name          string

    // Timestamps (from Pod.Status)
    CreatedAt     time.Time  // metadata.creationTimestamp
    ScheduledAt   time.Time  // status.conditions[type=PodScheduled].lastTransitionTime
    BoundAt       time.Time  // spec.nodeName set

    // Scheduling details
    NodeName      string     // spec.nodeName
    SchedulerName string     // spec.schedulerName (default: "default-scheduler")

    // Failure tracking
    Unschedulable bool       // status.conditions[type=PodScheduled].reason == "Unschedulable"
    FailureReason string     // Extracted from Event API

    // Resource requests
    CPURequest    int64      // Sum of container requests
    MemoryRequest int64

    // Scheduling constraints
    NodeSelector  map[string]string
    Affinity      *v1.Affinity
    Tolerations   []v1.Toleration
}
```

#### 2. Event API Watch
```go
// Kubernetes Events for failure reasons
// Example: "0/5 nodes are available: 2 Insufficient cpu, 3 node(s) had taint..."
type SchedulingFailureEvent struct {
    PodUID       string
    Timestamp    time.Time
    Reason       string  // "FailedScheduling"
    Message      string  // Detailed failure message with node counts

    // Parsed failure breakdown
    InsufficientCPU     int  // Number of nodes
    InsufficientMemory  int
    TaintViolations     int
    AffinityViolations  int
    UnschedulableNodes  int
}
```

#### 3. Node Informer
```go
// Track node capacity changes (for correlation)
type NodeCapacitySnapshot struct {
    NodeName      string
    Timestamp     time.Time

    // Capacity
    CPUCapacity   int64
    MemoryCapacity int64

    // Allocatable (after system reservations)
    CPUAllocatable int64
    MemoryAllocatable int64

    // Current allocation (sum of pod requests)
    CPUAllocated  int64
    MemoryAllocated int64

    // Taints/cordons
    Unschedulable bool
    Taints        []v1.Taint
}
```

### OTEL Metrics (Layer 1)

```go
// Scheduling latency
schedulingLatency := meter.Float64Histogram(
    "scheduler.pod_scheduling_duration_seconds",
    metric.WithDescription("Time from pod creation to successful scheduling"),
    metric.WithUnit("s"),
)

// Attributes: scheduler_name, namespace, pod_priority_class
// Buckets: 0.1, 0.5, 1, 5, 10, 30, 60, 300 seconds

// Scheduling attempts before success
schedulingAttempts := meter.Int64Histogram(
    "scheduler.pod_scheduling_attempts",
    metric.WithDescription("Number of scheduling attempts before pod was bound"),
    metric.WithUnit("{attempts}"),
)

// Failure reasons
schedulingFailures := meter.Int64Counter(
    "scheduler.pod_scheduling_failures_total",
    metric.WithDescription("Total scheduling failures by reason"),
    metric.WithUnit("{failures}"),
)
// Attributes: reason (InsufficientCPU, InsufficientMemory, Taint, Affinity, etc.)

// Queue depth (unscheduled pods)
unscheduledPods := meter.Int64Gauge(
    "scheduler.unscheduled_pods",
    metric.WithDescription("Number of pods pending scheduling"),
    metric.WithUnit("{pods}"),
)
// Attributes: scheduler_name, priority_class

// Preemption events
preemptionEvents := meter.Int64Counter(
    "scheduler.preemption_events_total",
    metric.WithDescription("Total number of pod preemptions"),
    metric.WithUnit("{preemptions}"),
)
// Attributes: preemptor_priority, victim_priority, reason
```

### Implementation (Go client-go)

```go
package scheduler

import (
    "context"
    "time"

    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/cache"

    "github.com/yairfalse/tapio/internal/base"
)

type SchedulerObserver struct {
    *base.BaseObserver

    clientset kubernetes.Interface

    // Informers
    podInformer   cache.SharedIndexInformer
    nodeInformer  cache.SharedIndexInformer
    eventInformer cache.SharedIndexInformer

    // Tracking map: PodUID → SchedulingState
    pendingPods sync.Map  // map[string]*PodSchedulingState

    // OTEL metrics
    schedulingLatency  metric.Float64Histogram
    schedulingAttempts metric.Int64Histogram
    schedulingFailures metric.Int64Counter
    unscheduledPods    metric.Int64Gauge
    preemptionEvents   metric.Int64Counter
}

type PodSchedulingState struct {
    Pod           *v1.Pod
    CreatedAt     time.Time
    Attempts      int
    LastFailure   string
    FailureReasons map[string]int  // "InsufficientCPU" → count
}

func (s *SchedulerObserver) Start(ctx context.Context) error {
    // Setup informers with event handlers
    s.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: s.onPodAdd,
        UpdateFunc: s.onPodUpdate,
        DeleteFunc: s.onPodDelete,
    })

    s.eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: s.onEvent,
    })

    // Start informers
    go s.podInformer.Run(ctx.Done())
    go s.eventInformer.Run(ctx.Done())
    go s.nodeInformer.Run(ctx.Done())

    // Periodic metrics collection
    go s.collectMetrics(ctx)

    return s.BaseObserver.Start(ctx)
}

func (s *SchedulerObserver) onPodAdd(obj interface{}) {
    pod := obj.(*v1.Pod)

    // Only track pods that need scheduling
    if pod.Spec.NodeName == "" && pod.DeletionTimestamp == nil {
        s.pendingPods.Store(string(pod.UID), &PodSchedulingState{
            Pod:       pod,
            CreatedAt: pod.CreationTimestamp.Time,
            Attempts:  0,
        })
    }
}

func (s *SchedulerObserver) onPodUpdate(oldObj, newObj interface{}) {
    oldPod := oldObj.(*v1.Pod)
    newPod := newObj.(*v1.Pod)

    // Pod was just scheduled (NodeName set)
    if oldPod.Spec.NodeName == "" && newPod.Spec.NodeName != "" {
        if state, ok := s.pendingPods.LoadAndDelete(string(newPod.UID)); ok {
            s.recordSuccessfulScheduling(newPod, state.(*PodSchedulingState))
        }
    }

    // Check for PodScheduled condition changes
    oldScheduled := getPodScheduledCondition(oldPod)
    newScheduled := getPodScheduledCondition(newPod)

    if oldScheduled == nil && newScheduled != nil && newScheduled.Status == v1.ConditionFalse {
        // Scheduling attempt failed
        if state, ok := s.pendingPods.Load(string(newPod.UID)); ok {
            st := state.(*PodSchedulingState)
            st.Attempts++
            st.LastFailure = newScheduled.Message
        }
    }
}

func (s *SchedulerObserver) onEvent(obj interface{}) {
    event := obj.(*v1.Event)

    // Only process scheduler events
    if event.Source.Component != "default-scheduler" {
        return
    }

    // Parse failure reasons from event message
    // Example: "0/5 nodes available: 2 Insufficient cpu, 3 node(s) had taint {key=value}"
    if event.Reason == "FailedScheduling" {
        reasons := parseFailureReasons(event.Message)

        for reason, count := range reasons {
            s.schedulingFailures.Add(context.Background(), int64(count),
                metric.WithAttributes(
                    attribute.String("reason", reason),
                    attribute.String("namespace", event.InvolvedObject.Namespace),
                ))
        }

        // Update pending pod state
        podUID := string(event.InvolvedObject.UID)
        if state, ok := s.pendingPods.Load(podUID); ok {
            st := state.(*PodSchedulingState)
            for reason, count := range reasons {
                st.FailureReasons[reason] += count
            }
        }
    }
}

func (s *SchedulerObserver) recordSuccessfulScheduling(pod *v1.Pod, state *PodSchedulingState) {
    ctx := context.Background()

    // Calculate scheduling latency
    scheduledTime := getScheduledTime(pod)
    latency := scheduledTime.Sub(state.CreatedAt).Seconds()

    s.schedulingLatency.Record(ctx, latency,
        metric.WithAttributes(
            attribute.String("scheduler_name", pod.Spec.SchedulerName),
            attribute.String("namespace", pod.Namespace),
            attribute.String("node", pod.Spec.NodeName),
        ))

    // Record attempts
    s.schedulingAttempts.Record(ctx, int64(state.Attempts),
        metric.WithAttributes(
            attribute.String("namespace", pod.Namespace),
        ))

    // Log structured event
    s.Logger(ctx).Info().
        Str("pod", pod.Name).
        Str("namespace", pod.Namespace).
        Str("node", pod.Spec.NodeName).
        Float64("latency_seconds", latency).
        Int("attempts", state.Attempts).
        Msg("pod scheduled successfully")
}

func parseFailureReasons(message string) map[string]int {
    // Parse: "0/5 nodes available: 2 Insufficient cpu, 3 node(s) had taint..."
    reasons := make(map[string]int)

    // Regex patterns for common failure reasons
    patterns := map[string]*regexp.Regexp{
        "InsufficientCPU":    regexp.MustCompile(`(\d+) Insufficient cpu`),
        "InsufficientMemory": regexp.MustCompile(`(\d+) Insufficient memory`),
        "Taint":              regexp.MustCompile(`(\d+) node\(s\) had taint`),
        "NodeAffinity":       regexp.MustCompile(`(\d+) node\(s\) didn't match.*affinity`),
        // Add more patterns...
    }

    for reason, pattern := range patterns {
        if matches := pattern.FindStringSubmatch(message); len(matches) > 1 {
            count, _ := strconv.Atoi(matches[1])
            reasons[reason] = count
        }
    }

    return reasons
}
```

## Layer 2: eBPF Tracing (Advanced Implementation)

**Note**: Requires access to kube-scheduler pod's PID namespace. Only works if:
1. Observer runs on control plane node, OR
2. Observer runs as privileged daemonset with host PID namespace

### What We Can Trace

#### 1. Scheduler Plugin Latency
```c
// Trace function entry/exit for scheduler plugins
// Using uprobes on kube-scheduler binary

SEC("uprobe/k8s.io/kubernetes/pkg/scheduler/framework.(*frameworkImpl).RunFilterPlugins")
int trace_run_filter_plugins_enter(struct pt_regs *ctx) {
    u64 ts = bpf_ktime_get_ns();
    u64 pid_tgid = bpf_get_current_pid_tgid();

    // Store start time keyed by goroutine ID
    filter_start_times.update(&pid_tgid, &ts);
    return 0;
}

SEC("uretprobe/k8s.io/kubernetes/pkg/scheduler/framework.(*frameworkImpl).RunFilterPlugins")
int trace_run_filter_plugins_exit(struct pt_regs *ctx) {
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u64 *start_ts = filter_start_times.lookup(&pid_tgid);

    if (start_ts) {
        u64 delta = bpf_ktime_get_ns() - *start_ts;

        struct plugin_latency_event evt = {
            .extension_point = EXTENSION_POINT_FILTER,
            .duration_ns = delta,
        };

        events.ringbuf_output(&evt, sizeof(evt), 0);
        filter_start_times.delete(&pid_tgid);
    }
    return 0;
}
```

#### 2. Memory Allocations During Scheduling
```c
// Track heap allocations in scheduling hot path
// Using uprobe on runtime.mallocgc

SEC("uprobe/runtime.mallocgc")
int trace_malloc(struct pt_regs *ctx) {
    u64 size = PT_REGS_PARM1(ctx);  // First arg: allocation size

    // Filter for scheduler goroutines (check task comm)
    char comm[16];
    bpf_get_current_comm(&comm, sizeof(comm));

    if (strncmp(comm, "kube-scheduler", 14) == 0) {
        struct alloc_event evt = {
            .size = size,
            .timestamp = bpf_ktime_get_ns(),
        };
        events.ringbuf_output(&evt, sizeof(evt), 0);
    }
    return 0;
}
```

#### 3. Scheduling Cycle Breakdown
```go
// Extension point timing breakdown
type SchedulingCycleTrace struct {
    PodUID string

    // Extension point durations (nanoseconds)
    PreFilterDuration  uint64
    FilterDuration     uint64
    PostFilterDuration uint64  // Preemption
    PreScoreDuration   uint64
    ScoreDuration      uint64
    ReserveDuration    uint64
    PermitDuration     uint64
    BindDuration       uint64

    // Total cycle time
    TotalDuration uint64

    // Plugin breakdown (which plugin was slowest?)
    SlowestFilterPlugin string
    SlowestScorePlugin  string
}
```

### OTEL Metrics (Layer 2)

```go
// Plugin latency breakdown
pluginLatency := meter.Float64Histogram(
    "scheduler.plugin_duration_seconds",
    metric.WithDescription("Scheduler plugin execution duration by extension point"),
    metric.WithUnit("s"),
)
// Attributes: extension_point (PreFilter, Filter, Score, etc.), plugin_name

// Scheduling cycle breakdown
cycleBreakdown := meter.Float64Histogram(
    "scheduler.cycle_phase_duration_seconds",
    metric.WithDescription("Duration of each scheduling cycle phase"),
    metric.WithUnit("s"),
)
// Attributes: phase (filter, score, reserve, bind)

// Memory allocations
schedulingAllocations := meter.Int64Counter(
    "scheduler.memory_allocations_bytes",
    metric.WithDescription("Memory allocated during scheduling cycles"),
    metric.WithUnit("By"),
)
```

## Correlation Opportunities

### 1. Scheduling → Network Events
```go
// When pod scheduled, correlate with network observer
type SchedulingNetworkCorrelation struct {
    PodUID    string
    NodeName  string

    // From Scheduler Observer
    ScheduledAt time.Time

    // From Network Observer (within 5s window)
    FirstConnection time.Time  // Container network namespace created
    FirstDNSQuery   time.Time  // Pod started resolving names
    FirstTCPConn    time.Time  // Pod connected to service

    // Calculated latencies
    NetworkSetupLatency time.Duration  // ScheduledAt → FirstConnection
    AppStartLatency     time.Duration  // FirstConnection → FirstTCPConn
}
```

### 2. Scheduling → Container Events
```go
// Correlate with Container observer (if exists)
type SchedulingContainerCorrelation struct {
    PodUID string

    ScheduledAt    time.Time  // From Scheduler
    ImagePulled    time.Time  // From Container observer
    ContainerStart time.Time  // From Container observer
    ContainerReady time.Time  // From kubelet

    // Identify bottlenecks
    ImagePullLatency      time.Duration
    ContainerStartLatency time.Duration
}
```

### 3. Node Pressure → Scheduling Failures
```go
// Correlate node resource pressure with scheduling failures
type NodePressureCorrelation struct {
    NodeName string
    Timestamp time.Time

    // From Node metrics
    CPUPressure    bool
    MemoryPressure bool
    DiskPressure   bool

    // From Scheduler failures
    PodsFailedDueToCPU    int  // Within 1min window
    PodsFailedDueToMemory int
}
```

## Success Criteria

### Layer 1 (Baseline)
- [ ] Pod scheduling latency tracked (Created → Scheduled)
- [ ] Failure reasons extracted from Event API
- [ ] Queue depth monitored (unscheduled pods)
- [ ] Per-scheduler metrics (multi-scheduler support)
- [ ] OTEL metrics exported
- [ ] Structured logging with zerolog

### Layer 2 (Advanced)
- [ ] eBPF uprobes attached to kube-scheduler binary
- [ ] Plugin latency per extension point
- [ ] Scheduling cycle phase breakdown
- [ ] Memory allocation tracking
- [ ] Zero overhead when eBPF disabled (graceful fallback)

### Correlation
- [ ] Match Pod UID across observers
- [ ] Network events within scheduling window
- [ ] Container lifecycle correlation
- [ ] Node pressure analysis

## Implementation Phases

### Phase 1: API Watch (Week 1)
- Client-go informers for Pods, Events, Nodes
- Basic scheduling latency metrics
- Failure reason parsing
- Queue depth tracking

### Phase 2: Enhanced Metrics (Week 2)
- Preemption detection
- Multi-scheduler support
- Priority class tracking
- Node capacity correlation

### Phase 3: eBPF Tracing (Week 3)
- Uprobe attachment to kube-scheduler
- Plugin latency tracking
- Scheduling cycle breakdown
- Memory profiling

### Phase 4: Correlation (Week 4)
- Cross-observer correlation engine
- Scheduling → Network events
- Scheduling → Container events
- Root cause analysis

## Open Questions

1. **Scheduler Binary Access**: Do we require kube-scheduler binary for eBPF uprobes, or can we rely solely on API watch?
   - **Decision**: Layer 1 (API) is baseline, Layer 2 (eBPF) is optional enhancement

2. **Multi-Scheduler Support**: How to track custom schedulers (e.g., volcano, kube-batch)?
   - **Solution**: Filter by `pod.spec.schedulerName`, support configurable scheduler list

3. **Historical Data**: Should we persist scheduling history for trend analysis?
   - **Solution**: Export to OTLP, let backend (Prometheus, Jaeger) handle storage

4. **Preemption Detection**: How to identify which pod caused preemption?
   - **Solution**: Parse Event API messages, match timestamps

5. **Correlation Window**: What's the acceptable time window for correlating events?
   - **Proposed**: 5 seconds for scheduling → network, 60 seconds for scheduling → container ready

## References

- [Kubernetes Scheduler Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [client-go Informers](https://pkg.go.dev/k8s.io/client-go/informers)
- [Scheduler Configuration](https://kubernetes.io/docs/reference/scheduling/config/)
- [OTEL Semantic Conventions for K8s](https://opentelemetry.io/docs/specs/semconv/system/k8s-metrics/)
