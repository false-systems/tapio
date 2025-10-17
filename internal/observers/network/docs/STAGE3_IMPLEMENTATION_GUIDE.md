# Stage 3: RTT Spike Detection - Complete Implementation Guide

**Status:** Ready for implementation
**Date:** 2025-10-17
**All design decisions resolved - proceed with implementation**

---

## Executive Summary

Implement RTT (Round-Trip Time) spike detection at kernel level to identify network latency degradation before it causes visible failures.

**Key Features:**
- eBPF state machine tracks baseline RTT per connection
- Detect spikes: >2x baseline OR >500ms absolute
- Emit enriched events with severity + context (process/pod/container)
- Zero struct size growth (reuse OldState/NewState fields)
- 400KB memory overhead for 10k connections

**What's Already Done:**
- ✅ EventTypeRTTSpike = 3 added to types.go
- ✅ Field reuse documented (OldState = baseline ms, NewState = current ms)
- ✅ All design decisions resolved (lifecycle, severity, context, monitoring)

---

## Phase 1: Add Fields to Domain Events

### File: `pkg/domain/events.go`

Add RTT fields to `NetworkEventData` struct:

```go
type NetworkEventData struct {
    Protocol string `json:"protocol,omitempty"`
    SrcIP    string `json:"src_ip,omitempty"`
    DstIP    string `json:"dst_ip,omitempty"`
    SrcPort  uint16 `json:"src_port,omitempty"`
    DstPort  uint16 `json:"dst_port,omitempty"`

    // L7 protocol fields
    HTTPMethod      string `json:"http_method,omitempty"`
    HTTPPath        string `json:"http_path,omitempty"`
    HTTPStatusCode  int    `json:"http_status_code,omitempty"`
    DNSQuery        string `json:"dns_query,omitempty"`
    DNSResponseTime int64  `json:"dns_response_time,omitempty"`

    // Connection metadata
    Duration      int64  `json:"duration,omitempty"`
    BytesSent     uint64 `json:"bytes_sent,omitempty"`
    BytesReceived uint64 `json:"bytes_received,omitempty"`

    // NEW: TCP performance metrics (Stage 2 & 3)
    RTTBaseline        float64 `json:"rtt_baseline,omitempty"`        // Baseline RTT in ms
    RTTCurrent         float64 `json:"rtt_current,omitempty"`         // Current RTT in ms
    RTTDegradation     float64 `json:"rtt_degradation,omitempty"`     // % increase from baseline
    RetransmitCount    uint32  `json:"retransmit_count,omitempty"`    // Total retransmits
    RetransmitRate     float64 `json:"retransmit_rate,omitempty"`     // Retransmit percentage
    CongestionWindow   uint32  `json:"congestion_window,omitempty"`   // TCP snd_cwnd
    TCPState           string  `json:"tcp_state,omitempty"`           // ESTABLISHED, CLOSE, etc

    // NEW: Severity and context for Ahti correlation
    PerformanceImpact  string  `json:"performance_impact,omitempty"`  // low, medium, high, critical
    ProcessName        string  `json:"process_name,omitempty"`        // Process name from PID
    ContainerID        string  `json:"container_id,omitempty"`        // Container ID from cgroup
    PodName            string  `json:"pod_name,omitempty"`            // K8s pod name
    Namespace          string  `json:"namespace,omitempty"`           // K8s namespace
}
```

---

## Phase 2: Implement eBPF RTT Tracking

### File: `internal/observers/network/bpf/network_monitor.c`

#### 2.1 Add RTT Baseline Map

```c
// RTT baseline state
struct rtt_baseline {
    __u32 baseline_us;      // Baseline RTT in microseconds
    __u8  sample_count;     // How many samples collected (0-5)
    __u8  state;            // NO_BASELINE=0, LEARNING=1, STABLE=2
    __u64 last_update_ns;   // Last time we updated baseline
    __u64 last_activity_ns; // Last time we saw traffic
};

// RTT baseline tracking map
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);  // Track up to 10k connections
    __type(key, struct conn_key);
    __type(value, struct rtt_baseline);
} baseline_rtt SEC(".maps");
```

#### 2.2 Add State Machine Constants

```c
// RTT states
#define RTT_STATE_NO_BASELINE 0
#define RTT_STATE_LEARNING    1
#define RTT_STATE_STABLE      2

// Thresholds
#define LEARNING_SAMPLES 5           // Collect 5 samples before going STABLE
#define STALE_THRESHOLD_NS 3600000000000ULL  // 1 hour
#define IDLE_THRESHOLD_NS  300000000000ULL   // 5 minutes

// Event types (must match types.go)
#define EVENT_TYPE_STATE_CHANGE 0
#define EVENT_TYPE_RST_RECEIVED 1
#define EVENT_TYPE_RETRANSMIT   2
#define EVENT_TYPE_RTT_SPIKE    3  // NEW
```

#### 2.3 Add RTT Tracking to inet_sock_set_state Tracepoint

```c
SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *args)
{
    // ... existing code to extract connection info ...

    // Only process TCP connections
    if (args->protocol != IPPROTO_TCP) {
        return 0;
    }

    // Get tcp_sock from skaddr
    const struct sock *sk = (const struct sock *)args->skaddr;
    struct tcp_sock *tp = (struct tcp_sock *)sk;

    // Read smoothed RTT from tcp_sock
    __u32 srtt_us = 0;
    bpf_core_read(&srtt_us, sizeof(srtt_us), &tp->srtt_us);

    // srtt_us is in microseconds, divided by 8 (kernel smoothing)
    __u32 rtt_us = srtt_us >> 3;

    // Skip if RTT is zero (no data yet)
    if (rtt_us == 0) {
        return 0;
    }

    // Get current time
    __u64 now_ns = bpf_ktime_get_ns();

    // Create connection key
    struct conn_key key = {0};
    if (args->family == AF_INET) {
        key.saddr = args->saddr;
        key.daddr = args->daddr;
    } else {
        // IPv6 handling omitted for brevity
    }
    key.sport = args->sport;
    key.dport = args->dport;

    // Lookup or create baseline entry
    struct rtt_baseline *baseline = bpf_map_lookup_elem(&baseline_rtt, &key);

    if (!baseline) {
        // First measurement - initialize to LEARNING state
        struct rtt_baseline new_baseline = {
            .baseline_us = rtt_us,
            .sample_count = 1,
            .state = RTT_STATE_LEARNING,
            .last_update_ns = now_ns,
            .last_activity_ns = now_ns,
        };
        bpf_map_update_elem(&baseline_rtt, &key, &new_baseline, BPF_NOEXIST);

        // Emit regular state change event
        // ... existing state change event code ...
        return 0;
    }

    // Update last activity timestamp
    baseline->last_activity_ns = now_ns;

    // State machine logic
    switch (baseline->state) {
        case RTT_STATE_LEARNING:
            // Collect samples to establish baseline
            baseline->sample_count++;

            // Calculate running average
            baseline->baseline_us = (baseline->baseline_us * (baseline->sample_count - 1) + rtt_us) / baseline->sample_count;

            // Transition to STABLE after collecting enough samples
            if (baseline->sample_count >= LEARNING_SAMPLES) {
                baseline->state = RTT_STATE_STABLE;
            }

            bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
            return 0;  // Don't emit spike events during learning

        case RTT_STATE_STABLE:
            // Check for staleness (baseline older than 1 hour)
            if (now_ns - baseline->last_update_ns > STALE_THRESHOLD_NS) {
                // Slowly update baseline (90% old + 10% new)
                baseline->baseline_us = (baseline->baseline_us * 9 + rtt_us) / 10;
                baseline->last_update_ns = now_ns;
                bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
            }

            // Check for RTT spike: >2x baseline OR >500ms absolute
            if (rtt_us > (baseline->baseline_us * 2) || rtt_us > 500000) {
                // Emit RTT spike event
                struct network_event_bpf *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
                if (!evt) {
                    return 0;
                }

                // Populate event
                evt->pid = bpf_get_current_pid_tgid() >> 32;
                evt->src_ip = args->saddr;
                evt->dst_ip = args->daddr;
                evt->src_port = args->sport;
                evt->dst_port = args->dport;
                evt->family = args->family;
                evt->protocol = args->protocol;
                evt->event_type = EVENT_TYPE_RTT_SPIKE;

                // Reuse OldState/NewState for RTT data
                // Convert microseconds to milliseconds, clamp to 255ms
                __u32 baseline_ms = baseline->baseline_us / 1000;
                __u32 current_ms = rtt_us / 1000;
                evt->old_state = baseline_ms > 255 ? 255 : baseline_ms;  // Baseline RTT
                evt->new_state = current_ms > 255 ? 255 : current_ms;    // Current RTT

                // Get process name
                bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

                // Submit event
                bpf_ringbuf_submit(evt, 0);
            }
            break;
    }

    return 0;
}
```

#### 2.4 Add Cleanup on Connection Close

```c
SEC("tracepoint/sock/inet_sock_set_state")
int trace_sock_close(struct trace_event_raw_inet_sock_set_state *args)
{
    // Only clean up on TCP_CLOSE state
    if (args->newstate != TCP_CLOSE) {
        return 0;
    }

    // Create connection key
    struct conn_key key = {0};
    if (args->family == AF_INET) {
        key.saddr = args->saddr;
        key.daddr = args->daddr;
    }
    key.sport = args->sport;
    key.dport = args->dport;

    // Delete baseline entry to free memory
    bpf_map_delete_elem(&baseline_rtt, &key);

    return 0;
}
```

---

## Phase 3: Process RTT Spike Events in Go

### File: `internal/observers/network/observer.go`

#### 3.1 Add Context Enrichment Fields

```go
import (
    lru "github.com/hashicorp/golang-lru/v2"
    "k8s.io/client-go/kubernetes"
)

type NetworkObserver struct {
    // ... existing fields ...

    // Context enrichment
    processCache   *lru.Cache[uint32, string]        // PID → process name
    containerCache *lru.Cache[uint32, string]        // PID → container ID
    podCache       *lru.Cache[string, *PodInfo]      // container ID → pod info

    k8sClient      kubernetes.Interface
    nodeName       string

    // New OTEL metrics for RTT
    rttSpikes      metric.Int64Counter
    rttCurrent     metric.Float64Gauge
    rttDegradation metric.Float64Gauge
    mapUsageMB     metric.Float64Gauge
}

type PodInfo struct {
    Name      string
    Namespace string
    NodeName  string
}
```

#### 3.2 Initialize Caches and Metrics

```go
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
    // ... existing initialization ...

    // Initialize LRU caches
    processCache, err := lru.New[uint32, string](1000)
    if err != nil {
        return nil, fmt.Errorf("failed to create process cache: %w", err)
    }

    containerCache, err := lru.New[uint32, string](1000)
    if err != nil {
        return nil, fmt.Errorf("failed to create container cache: %w", err)
    }

    podCache, err := lru.New[string, *PodInfo](1000)
    if err != nil {
        return nil, fmt.Errorf("failed to create pod cache: %w", err)
    }

    // Get K8s client (if running in cluster)
    var k8sClient kubernetes.Interface
    if config, err := rest.InClusterConfig(); err == nil {
        k8sClient, _ = kubernetes.NewForConfig(config)
    }

    // Get node name from env (set by K8s downward API)
    nodeName := os.Getenv("NODE_NAME")

    // Create RTT metrics
    meter := otel.Meter("tapio.network")

    rttSpikes, err := meter.Int64Counter(
        "tapio_rtt_spikes_total",
        metric.WithDescription("Total RTT spike events detected"),
        metric.WithUnit("{events}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create rtt_spikes metric: %w", err)
    }

    rttCurrent, err := meter.Float64Gauge(
        "tapio_rtt_current_ms",
        metric.WithDescription("Current RTT in milliseconds"),
        metric.WithUnit("ms"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create rtt_current metric: %w", err)
    }

    rttDegradation, err := meter.Float64Gauge(
        "tapio_rtt_degradation_percent",
        metric.WithDescription("RTT increase percentage from baseline"),
        metric.WithUnit("%"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create rtt_degradation metric: %w", err)
    }

    mapUsageMB, err := meter.Float64Gauge(
        "tapio_rtt_baseline_map_usage_mb",
        metric.WithDescription("RTT baseline map memory usage"),
        metric.WithUnit("MB"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create map_usage metric: %w", err)
    }

    observer := &NetworkObserver{
        // ... existing fields ...
        processCache:   processCache,
        containerCache: containerCache,
        podCache:       podCache,
        k8sClient:      k8sClient,
        nodeName:       nodeName,
        rttSpikes:      rttSpikes,
        rttCurrent:     rttCurrent,
        rttDegradation: rttDegradation,
        mapUsageMB:     mapUsageMB,
    }

    return observer, nil
}
```

### File: `internal/observers/network/observer_ebpf.go`

#### 3.3 Add Connection Context Helper

```go
type ConnectionContext struct {
    ProcessName string
    ContainerID string
    PodName     string
    Namespace   string
}

// getConnectionContext enriches event with process/pod/container info
func (n *NetworkObserver) getConnectionContext(pid uint32) ConnectionContext {
    ctx := ConnectionContext{}

    // 1. Get process name from cache or /proc
    if processName, ok := n.processCache.Get(pid); ok {
        ctx.ProcessName = processName
    } else {
        data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
        if err == nil {
            processName := strings.TrimSpace(string(data))
            n.processCache.Add(pid, processName)
            ctx.ProcessName = processName
        }
    }

    // 2. Get container ID from cgroup
    if containerID, ok := n.containerCache.Get(pid); ok {
        ctx.ContainerID = containerID
    } else {
        data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
        if err == nil {
            containerID := parseContainerID(string(data))
            if containerID != "" {
                n.containerCache.Add(pid, containerID)
                ctx.ContainerID = containerID
            }
        }
    }

    // 3. Get pod info from K8s API (if we have container ID)
    if ctx.ContainerID != "" {
        if podInfo, ok := n.podCache.Get(ctx.ContainerID); ok {
            ctx.PodName = podInfo.Name
            ctx.Namespace = podInfo.Namespace
        } else if n.k8sClient != nil {
            podInfo := n.findPodByContainerID(ctx.ContainerID)
            if podInfo != nil {
                n.podCache.Add(ctx.ContainerID, podInfo)
                ctx.PodName = podInfo.Name
                ctx.Namespace = podInfo.Namespace
            }
        }
    }

    return ctx
}

// parseContainerID extracts container ID from cgroup data
func parseContainerID(cgroupData string) string {
    // Parse cgroup path to extract container ID
    // Format: 0::/kubepods/besteffort/pod<uuid>/<container-id>
    lines := strings.Split(cgroupData, "\n")
    for _, line := range lines {
        if strings.Contains(line, "kubepods") {
            parts := strings.Split(line, "/")
            if len(parts) > 0 {
                lastPart := parts[len(parts)-1]
                if len(lastPart) == 64 {  // Docker container ID length
                    return lastPart
                }
            }
        }
    }
    return ""
}

// findPodByContainerID queries K8s API to find pod
func (n *NetworkObserver) findPodByContainerID(containerID string) *PodInfo {
    if n.k8sClient == nil {
        return nil
    }

    // List all pods on this node
    pods, err := n.k8sClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
        FieldSelector: fmt.Sprintf("spec.nodeName=%s", n.nodeName),
    })
    if err != nil {
        return nil
    }

    // Find pod with matching container ID
    for _, pod := range pods.Items {
        for _, containerStatus := range pod.Status.ContainerStatuses {
            // containerStatus.ContainerID format: "docker://<id>"
            parts := strings.SplitN(containerStatus.ContainerID, "://", 2)
            if len(parts) == 2 && strings.HasPrefix(parts[1], containerID[:12]) {
                return &PodInfo{
                    Name:      pod.Name,
                    Namespace: pod.Namespace,
                    NodeName:  pod.Spec.NodeName,
                }
            }
        }
    }

    return nil
}
```

#### 3.4 Add Severity Classification

```go
type PerformanceImpact string

const (
    ImpactLow      PerformanceImpact = "low"
    ImpactMedium   PerformanceImpact = "medium"
    ImpactHigh     PerformanceImpact = "high"
    ImpactCritical PerformanceImpact = "critical"
)

// calculateRTTImpact determines severity based on RTT metrics
func calculateRTTImpact(currentMs, baselineMs, degradationPercent float64) PerformanceImpact {
    // Critical: >1s absolute OR >500% degradation
    if currentMs > 1000 || degradationPercent > 500 {
        return ImpactCritical
    }

    // High: >500ms absolute OR >200% degradation
    if currentMs > 500 || degradationPercent > 200 {
        return ImpactHigh
    }

    // Medium: >200ms absolute OR >100% degradation
    if currentMs > 200 || degradationPercent > 100 {
        return ImpactMedium
    }

    return ImpactLow
}
```

#### 3.5 Add RTT Spike Event Processing

```go
// processRTTSpikeEvent handles RTT spike events from eBPF
func (n *NetworkObserver) processRTTSpikeEvent(ctx context.Context, evt NetworkEventBPF, srcIP, dstIP string) {
    // Extract RTT data from event
    baselineMs := float64(evt.OldState)
    currentMs := float64(evt.NewState)

    // Calculate degradation percentage
    degradation := 0.0
    if baselineMs > 0 {
        degradation = ((currentMs - baselineMs) / baselineMs) * 100
    }

    // Calculate severity
    impact := calculateRTTImpact(currentMs, baselineMs, degradation)

    // Only emit if medium or higher (filter noise)
    if impact == ImpactLow {
        return
    }

    // Get connection context
    connCtx := n.getConnectionContext(evt.PID)

    // Build connection key for logging
    connKey := fmt.Sprintf("%s:%d->%s:%d", srcIP, evt.SrcPort, dstIP, evt.DstPort)

    // Build domain event
    domainEvent := &domain.ObserverEvent{
        ID:        generateEventID(),
        Type:      "network.rtt_spike",
        Source:    n.Name(),
        Timestamp: time.Now(),
        NetworkData: &domain.NetworkEventData{
            Protocol:          "TCP",
            SrcIP:             srcIP,
            DstIP:             dstIP,
            SrcPort:           evt.SrcPort,
            DstPort:           evt.DstPort,
            RTTBaseline:       baselineMs,
            RTTCurrent:        currentMs,
            RTTDegradation:    degradation,
            TCPState:          tcpStateName(evt.NewState),
            PerformanceImpact: string(impact),
            ProcessName:       connCtx.ProcessName,
            ContainerID:       connCtx.ContainerID,
            PodName:           connCtx.PodName,
            Namespace:         connCtx.Namespace,
        },
    }

    // Emit domain event (if emitter configured)
    if n.emitter != nil {
        if err := n.emitter.Emit(ctx, domainEvent); err != nil {
            log.Error().
                Err(err).
                Str("connection", connKey).
                Msg("Failed to emit RTT spike event")
        }
    }

    // Record OTEL metrics
    attrs := []attribute.KeyValue{
        attribute.String("severity", string(impact)),
        attribute.String("src_ip", srcIP),
        attribute.String("dst_ip", dstIP),
        attribute.String("pod", connCtx.PodName),
        attribute.String("namespace", connCtx.Namespace),
    }

    n.rttSpikes.Add(ctx, 1, metric.WithAttributes(attrs...))
    n.rttCurrent.Record(ctx, currentMs, metric.WithAttributes(attrs...))
    n.rttDegradation.Record(ctx, degradation, metric.WithAttributes(attrs...))

    // Log if stdout enabled
    if n.config.Output.Stdout {
        log.Info().
            Str("connection", connKey).
            Str("pod", connCtx.PodName).
            Str("process", connCtx.ProcessName).
            Float64("baseline_ms", baselineMs).
            Float64("current_ms", currentMs).
            Float64("degradation_pct", degradation).
            Str("severity", string(impact)).
            Msg("RTT spike detected")
    }
}

// generateEventID creates unique event ID
func generateEventID() string {
    return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), rand.Uint32())
}
```

#### 3.6 Update Event Processing Loop

```go
// In processEvents() method, add RTT spike handling:

func (n *NetworkObserver) processEvents(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            record, err := n.reader.Read()
            if err != nil {
                if errors.Is(err, ringbuf.ErrClosed) {
                    return
                }
                continue
            }

            var evt NetworkEventBPF
            if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
                continue
            }

            // Extract IPs
            srcIP, dstIP := extractIPs(evt)

            // Process based on event type
            switch evt.EventType {
            case EventTypeStateChange:
                n.processStateChangeEvent(ctx, evt, srcIP, dstIP)
            case EventTypeRSTReceived:
                n.processRSTEvent(ctx, evt, srcIP, dstIP)
            case EventTypeRetransmit:
                n.processRetransmitEvent(ctx, evt, srcIP, dstIP)
            case EventTypeRTTSpike:  // NEW
                n.processRTTSpikeEvent(ctx, evt, srcIP, dstIP)
            }
        }
    }
}
```

---

## Phase 4: Add Periodic Cleanup

### File: `internal/observers/network/observer.go`

```go
// Start starts the network observer
func (n *NetworkObserver) Start(ctx context.Context) error {
    // ... existing start code ...

    // Start baseline cleanup goroutine
    go n.startBaselineCleanup(ctx)

    return nil
}

// startBaselineCleanup monitors and cleans up stale baselines
func (n *NetworkObserver) startBaselineCleanup(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            n.monitorMapUsage(ctx)
        }
    }
}

// monitorMapUsage tracks RTT baseline map usage
func (n *NetworkObserver) monitorMapUsage(ctx context.Context) {
    if n.objs.BaselineRtt == nil {
        return
    }

    // Get map info
    info, err := n.objs.BaselineRtt.Info()
    if err != nil {
        log.Error().Err(err).Msg("Failed to get baseline map info")
        return
    }

    // Calculate usage
    usage := float64(info.ValueSize*info.MaxEntries) / (1024 * 1024) // MB
    n.mapUsageMB.Record(ctx, usage)

    // Alert if near capacity
    if info.MaxEntries > 9000 { // >90% full
        log.Warn().
            Int("entries", info.MaxEntries).
            Int("max", 10000).
            Msg("RTT baseline map near capacity")
    }
}
```

---

## Phase 5: Testing

### File: `internal/observers/network/observer_test.go`

```go
func TestRTTImpactClassification(t *testing.T) {
    tests := []struct {
        name              string
        currentMs         float64
        baselineMs        float64
        degradationPercent float64
        expectedImpact    PerformanceImpact
    }{
        {
            name:              "Critical - absolute >1s",
            currentMs:         1100,
            baselineMs:        50,
            degradationPercent: 2100,
            expectedImpact:    ImpactCritical,
        },
        {
            name:              "Critical - degradation >500%",
            currentMs:         350,
            baselineMs:        50,
            degradationPercent: 600,
            expectedImpact:    ImpactCritical,
        },
        {
            name:              "High - absolute >500ms",
            currentMs:         600,
            baselineMs:        100,
            degradationPercent: 500,
            expectedImpact:    ImpactHigh,
        },
        {
            name:              "High - degradation >200%",
            currentMs:         250,
            baselineMs:        50,
            degradationPercent: 400,
            expectedImpact:    ImpactHigh,
        },
        {
            name:              "Medium - absolute >200ms",
            currentMs:         250,
            baselineMs:        50,
            degradationPercent: 400,
            expectedImpact:    ImpactMedium,
        },
        {
            name:              "Medium - degradation >100%",
            currentMs:         150,
            baselineMs:        50,
            degradationPercent: 200,
            expectedImpact:    ImpactMedium,
        },
        {
            name:              "Low - below thresholds",
            currentMs:         80,
            baselineMs:        50,
            degradationPercent: 60,
            expectedImpact:    ImpactLow,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            impact := calculateRTTImpact(tt.currentMs, tt.baselineMs, tt.degradationPercent)
            assert.Equal(t, tt.expectedImpact, impact)
        })
    }
}

func TestConnectionContextEnrichment(t *testing.T) {
    // Test process name extraction
    // Test container ID parsing
    // Test pod lookup
    // (Implementation depends on test environment)
}

func TestRTTSpikeEventEmission(t *testing.T) {
    // Test event is emitted for medium+ severity
    // Test low severity is filtered
    // Test OTEL metrics are recorded
    // Test stdout logging works
    // (Implementation depends on observer setup)
}
```

### Integration Test

Create `internal/observers/network/stage3_test.sh`:

```bash
#!/bin/bash
# Integration test for Stage 3 RTT spike detection

set -e

echo "Stage 3 Integration Test: RTT Spike Detection"

# 1. Start observer
echo "Starting network observer..."
./tapio-observer network &
OBSERVER_PID=$!
sleep 5

# 2. Simulate RTT spike using tc (traffic control)
echo "Simulating 600ms RTT spike..."
sudo tc qdisc add dev eth0 root netem delay 600ms

# 3. Generate some traffic
echo "Generating traffic..."
curl -s https://example.com > /dev/null

# 4. Wait for event
sleep 2

# 5. Check for RTT spike event
echo "Checking for RTT spike event..."
if tapio-cli events --type network.rtt_spike --last 1m | grep -q "rtt_spike"; then
    echo "✅ RTT spike event detected"
else
    echo "❌ RTT spike event NOT detected"
    exit 1
fi

# 6. Check OTEL metrics
echo "Checking OTEL metrics..."
if curl -s localhost:9090/metrics | grep -q "tapio_rtt_spikes_total"; then
    echo "✅ OTEL metrics exposed"
else
    echo "❌ OTEL metrics NOT found"
    exit 1
fi

# 7. Cleanup
echo "Cleaning up..."
sudo tc qdisc del dev eth0 root
kill $OBSERVER_PID

echo "✅ Stage 3 integration test PASSED"
```

---

## Phase 6: Documentation

### Update DESIGN.md

Add Stage 3 section documenting:
- RTT baseline state machine
- Spike detection thresholds
- Map lifecycle and cleanup
- Performance impact
- Monitoring and alerting

### Create STAGE3_RTT_TRACKING.md

Document:
- Map configuration and limits
- Memory usage (400KB for 10k connections)
- Cleanup strategies
- Tuning options for large clusters
- Troubleshooting guide

---

## Deployment Checklist

Before deploying Stage 3 to production:

- [ ] All unit tests pass
- [ ] Integration tests pass with simulated RTT spikes
- [ ] OTEL metrics exposed and validated
- [ ] Stdout logging works correctly
- [ ] K8s context enrichment works (if available)
- [ ] Map usage monitoring works
- [ ] No performance regression (run benchmarks)
- [ ] Documentation updated
- [ ] CLAUDE.md compliance verified (no violations)

---

## Monitoring & Alerting

### Prometheus Alerts

```yaml
groups:
  - name: tapio_rtt
    rules:
      # Alert on high RTT spike rate
      - alert: HighRTTSpikeRate
        expr: rate(tapio_rtt_spikes_total[5m]) > 10
        for: 5m
        annotations:
          summary: "High rate of RTT spikes detected"
          description: "{{ $value }} RTT spikes per second"

      # Alert on map capacity
      - alert: RTTBaselineMapNearFull
        expr: tapio_rtt_baseline_map_usage_percent > 90
        for: 5m
        annotations:
          summary: "RTT baseline map >90% full"
          description: "May miss RTT spikes for new connections"

      # Alert on critical RTT spikes
      - alert: CriticalRTTSpike
        expr: tapio_rtt_current_ms{severity="critical"} > 1000
        for: 1m
        annotations:
          summary: "Critical RTT spike detected (>1s)"
          description: "Connection {{ $labels.src_ip }}->{{ $labels.dst_ip }}"
```

### Grafana Dashboard

Panels to add:
- RTT spike rate over time
- Current RTT by connection
- RTT degradation percentage
- Severity distribution (low/medium/high/critical)
- Top pods by RTT spikes
- Map usage percentage

---

## Success Criteria

Stage 3 is complete when:

1. ✅ eBPF state machine tracks RTT baselines per connection
2. ✅ Spike events emitted for >2x baseline OR >500ms
3. ✅ Events enriched with process/pod/container context
4. ✅ Severity classification (low/medium/high/critical)
5. ✅ OTEL metrics exposed (spikes, current RTT, degradation)
6. ✅ Map usage monitored and alerted
7. ✅ Domain events sent to Ahti (if configured)
8. ✅ All tests pass (unit + integration)
9. ✅ Documentation complete
10. ✅ No CLAUDE.md violations

---

## What Comes After Stage 3?

**Stage 4 (Future):** DNS latency tracking
**Stage 5 (Future):** HTTP request latency
**Stage 6 (Future):** Adaptive thresholds (ML-based)

---

## Questions? Issues?

Refer to:
- `/Users/yair/projects/tapio/internal/observers/network/docs/INTEGRATION_DESIGN.md`
- `/Users/yair/projects/elava/docs/INTEGRATION_DESIGN.md`
- CLAUDE.md for coding standards

**Now go implement it! All design decisions are made. Just code.** 🚀
