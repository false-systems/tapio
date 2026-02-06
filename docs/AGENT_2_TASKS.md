# Agent 2: Base Observer + Network Observer (eBPF Focus)

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Timeline:** Week 1, Day 1-4 (parallel with Agent 1's decoder work)

**Goal:** Build observer infrastructure and first consolidated network observer

---

## Task 1: Base Observer Infrastructure (Day 1-2)

### Files to Create:

```
internal/base/
  observer.go      # BaseObserver struct + lifecycle
  ebpf.go          # eBPF helper functions (cilium/ebpf)
  pipeline.go      # errgroup-based pipeline
  metrics.go       # OTEL metrics setup
```

### Deliverable 1: `internal/base/observer.go`

```go
package base

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

// BaseObserver provides common infrastructure for all observers
type BaseObserver struct {
    Name   string
    Config ObserverConfig

    // OTEL (MANDATORY - direct OTEL, no wrappers!)
    Tracer          trace.Tracer
    EventsProcessed metric.Int64Counter
    ErrorsTotal     metric.Int64Counter
    ProcessingTime  metric.Float64Histogram

    // Internal channels
    RawEventCh chan []byte              // From eBPF
    ObserverEventCh chan domain.ObserverEvent  // After initial parse
}

type ObserverConfig struct {
    EnableEnrichment bool
    NATSUrl         string
    BufferSize      int
}

// Run starts the observer pipeline using errgroup pattern
func (b *BaseObserver) Run(ctx context.Context) error {
    // Use errgroup (simpler than Beyla's swarm!)
    // See pipeline.go for implementation
}

// Start, Stop, IsHealthy methods...
```

**Requirements:**
- Zero `map[string]interface{}`
- Direct OTEL imports only (no telemetry wrappers)
- errgroup for lifecycle (not swarm pattern)
- 80% test coverage

---

### Deliverable 2: `internal/base/ebpf.go`

```go
package base

import (
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/ringbuf"
)

// LoadBPFProgram loads an eBPF object file
func LoadBPFProgram(objPath string) (*ebpf.Collection, error) {
    // Use cilium/ebpf (NOT libbpf-go)
    // CO-RE support for portability
}

// AttachKprobe attaches kprobe to kernel function
func AttachKprobe(prog *ebpf.Program, symbol string) (link.Link, error) {
    // Attach kprobe
    // Return link for cleanup
}

// AttachTracepoint attaches to tracepoint
func AttachTracepoint(prog *ebpf.Program, group, name string) (link.Link, error) {
    // e.g., tracepoint/syscalls/sys_enter_connect
}

// ReadRingBuffer reads events from ring buffer
func ReadRingBuffer(ctx context.Context, rb *ringbuf.Reader, eventCh chan<- []byte) error {
    // Read loop
    // Respect ctx.Done()
    // Handle errors properly
}

// Helper: DetectKernelVersion, CheckBTFSupport, etc.
```

**Requirements:**
- Use `cilium/ebpf` (Go-native, well-maintained)
- CO-RE support (kernel portability)
- Proper cleanup (defer link.Close())
- Error context wrapping

---

### Deliverable 3: `internal/base/pipeline.go`

```go
package base

import (
    "context"
    "golang.org/x/sync/errgroup"
)

// Pipeline implements errgroup-based stage execution
// Simpler than Beyla's swarm, perfect for Tapio!

type Pipeline struct {
    stages []PipelineStage
}

type PipelineStage func(ctx context.Context) error

func (p *Pipeline) Add(stage PipelineStage) {
    p.stages = append(p.stages, stage)
}

func (p *Pipeline) Run(ctx context.Context) error {
    g, ctx := errgroup.WithContext(ctx)

    for _, stage := range p.stages {
        stage := stage  // Capture for goroutine
        g.Go(func() error {
            return stage(ctx)
        })
    }

    return g.Wait()  // Waits for all, cancels on first error
}
```

**Usage example (for your reference):**
```go
pipeline := &Pipeline{}
pipeline.Add(func(ctx) error { return collectEvents(ctx, eventCh) })
pipeline.Add(func(ctx) error { return enrichEvents(ctx, eventCh, enrichedCh) })
pipeline.Add(func(ctx) error { return publishEvents(ctx, enrichedCh) })
return pipeline.Run(ctx)
```

---

### Deliverable 4: `internal/base/metrics.go`

```go
package base

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

// ObserverMetrics holds all OTEL metrics for an observer
type ObserverMetrics struct {
    EventsProcessed metric.Int64Counter
    ErrorsTotal     metric.Int64Counter
    ProcessingTime  metric.Float64Histogram
    ActiveProbes    metric.Int64Gauge
}

func NewObserverMetrics(observerName string) (*ObserverMetrics, error) {
    meter := otel.Meter("tapio.observer." + observerName)

    eventsProcessed, err := meter.Int64Counter(
        "observer_events_processed_total",
        metric.WithDescription("Total events processed"),
    )
    if err != nil {
        return nil, err
    }

    // Create other metrics...

    return &ObserverMetrics{
        EventsProcessed: eventsProcessed,
        // ...
    }, nil
}
```

**Metric naming standards:**
- Counters: `_total` suffix
- Histograms: unit in name (`_duration_ms`, `_bytes`)
- Gauges: current state (`_active_connections`)

---

## Task 2: Network Observer (Day 3-4)

### Files to Create:

```
internal/observers/network/
  observer.go           # Network observer implementation
  bpf_src/
    network.c           # eBPF programs (TCP/UDP/DNS/HTTP)
    vmlinux.h           # Kernel types (BTF)
  observer_test.go      # Unit tests
  observer_integration_test.go  # Integration tests
```

### Deliverable 5: eBPF Programs - `bpf_src/network.c`

```c
// SPDX-License-Identifier: GPL-2.0
// Consolidated network observer: TCP, UDP, DNS, HTTP
// Uses libbpf CO-RE for kernel portability

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Event structure (matches domain.NetworkEventData)
struct network_event {
    __u32 pid;
    __u32 tid;
    __u8  protocol;  // 6=TCP, 17=UDP
    __u32 saddr;
    __u32 daddr;
    __u16 sport;
    __u16 dport;
    __u64 timestamp_ns;
    __u64 duration_ns;
    __u64 bytes_sent;
    __u64 bytes_received;
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// TCP connection tracking
SEC("kprobe/tcp_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

    struct network_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;

    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->tid = bpf_get_current_pid_tgid();
    event->protocol = 6; // TCP
    event->timestamp_ns = bpf_ktime_get_ns();

    // Read socket addresses using CO-RE
    BPF_CORE_READ_INTO(&event->saddr, sk, __sk_common.skc_rcv_saddr);
    BPF_CORE_READ_INTO(&event->daddr, sk, __sk_common.skc_daddr);
    BPF_CORE_READ_INTO(&event->sport, sk, __sk_common.skc_num);
    BPF_CORE_READ_INTO(&event->dport, sk, __sk_common.skc_dport);

    bpf_ringbuf_submit(event, 0);
    return 0;
}

// UDP sendmsg tracking
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    // Similar to TCP...
}

// DNS query tracking (port 53)
// HTTP tracking (port 80/443) - parse in userspace

char LICENSE[] SEC("license") = "GPL";
```

**Requirements:**
- Use CO-RE (BPF_CORE_READ macros)
- Ring buffer (not perf buffer - newer, faster)
- Emit events matching `domain.NetworkEventData`
- Handle IPv4 (IPv6 in future)

---

### Deliverable 6: Observer Implementation - `observer.go`

```go
package network

import (
    "context"
    "fmt"

    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/ringbuf"
    "github.com/yairfalse/tapio/internal/base"
    "github.com/yairfalse/tapio/pkg/domain"
)

type Observer struct {
    *base.BaseObserver

    // eBPF resources
    collection *ebpf.Collection
    ringbuf    *ringbuf.Reader
}

func NewObserver(name string, cfg base.ObserverConfig) (*Observer, error) {
    baseObs := &base.BaseObserver{
        Name:   name,
        Config: cfg,
        RawEventCh: make(chan []byte, cfg.BufferSize),
        ObserverEventCh: make(chan domain.ObserverEvent, cfg.BufferSize),
    }

    // Load eBPF program
    collection, err := base.LoadBPFProgram("bpf_src/network.o")
    if err != nil {
        return nil, fmt.Errorf("failed to load eBPF: %w", err)
    }

    // Get ring buffer
    ringbuf, err := ringbuf.NewReader(collection.Maps["events"])
    if err != nil {
        collection.Close()
        return nil, fmt.Errorf("failed to open ringbuf: %w", err)
    }

    return &Observer{
        BaseObserver: baseObs,
        collection:   collection,
        ringbuf:      ringbuf,
    }, nil
}

func (o *Observer) Start(ctx context.Context) error {
    pipeline := &base.Pipeline{}

    // Stage 1: Read eBPF events
    pipeline.Add(func(ctx context.Context) error {
        return base.ReadRingBuffer(ctx, o.ringbuf, o.RawEventCh)
    })

    // Stage 2: Parse to ObserverEvent
    pipeline.Add(func(ctx context.Context) error {
        return o.parseEvents(ctx)
    })

    // Stage 3: Enrichment (Agent 1's decoder pipeline!)
    // Will be added after Agent 1 finishes decoders

    return pipeline.Run(ctx)
}

func (o *Observer) parseEvents(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case rawEvent := <-o.RawEventCh:
            // Parse C struct to domain.ObserverEvent
            event := o.parseNetworkEvent(rawEvent)
            o.ObserverEventCh <- event
        }
    }
}

func (o *Observer) parseNetworkEvent(raw []byte) domain.ObserverEvent {
    // Parse C struct network_event -> domain.ObserverEvent
    // Use encoding/binary to read C struct
}
```

---

## Integration Point with Agent 1

**After both agents finish Day 2:**

```go
// Agent 2's observer emits ObserverEvent
event := domain.ObserverEvent{
    Type: "tcp_connect",
    NetworkData: &domain.NetworkEventData{
        Protocol: "TCP",
        SrcIP:    "10.244.1.5",  // Raw IP bytes
        DstIP:    "10.96.0.1",
        SrcPort:  45678,
        DstPort:  443,
    },
}

// Agent 1's decoder pipeline transforms it
decodedEvent := decoder.Decode(event.NetworkData.SrcIP)
// "10.244.1.5" -> k8s_pod decoder -> "nginx-abc123"

// Agent 1's enricher extracts entities
enrichedEvent := enricher.Enrich(event)
// Creates Entity{Type: "pod", Name: "nginx-abc123"}
// Creates Entity{Type: "service", Name: "kubernetes"}
// Creates Relationship{Type: "connects_to"}
```

---

## Success Criteria

**Day 2 checkpoint:**
- [ ] Base infrastructure complete (observer.go, ebpf.go, pipeline.go, metrics.go)
- [ ] Unit tests passing (80% coverage)
- [ ] No TODOs, no stubs
- [ ] `make verify-full` passes

**Day 4 checkpoint:**
- [ ] Network observer working (eBPF + Go)
- [ ] Events flowing: eBPF → RingBuf → ObserverEvent
- [ ] Integration test on k3s (create pod, see TCP events)
- [ ] Ready to integrate with Agent 1's decoder pipeline

---

## Testing Strategy

**Unit tests:**
```go
func TestBaseObserver_Lifecycle(t *testing.T) {
    obs := NewObserver("test", config)
    ctx, cancel := context.WithCancel(context.Background())

    go obs.Start(ctx)
    time.Sleep(100 * time.Millisecond)

    assert.True(t, obs.IsHealthy())

    cancel()
    // Should exit cleanly
}
```

**Integration test (requires Linux + k3s):**
```go
func TestNetworkObserver_Integration(t *testing.T) {
    if runtime.GOOS != "linux" {
        t.Skip("eBPF requires Linux")
    }

    obs := NewObserver("network", config)
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    go obs.Start(ctx)

    // Create TCP connection
    conn, _ := net.Dial("tcp", "google.com:80")
    defer conn.Close()

    // Should see tcp_connect event
    select {
    case event := <-obs.ObserverEventCh:
        assert.Equal(t, "tcp_connect", event.Type)
        assert.Equal(t, uint16(80), event.NetworkData.DstPort)
    case <-time.After(1 * time.Second):
        t.Fatal("no event received")
    }
}
```

---

## References

- `/Users/yair/projects/tapio/pkg/domain/events.go` - Event schemas
- `/Users/yair/projects/ukko/docs/EVENT_SCHEMA.md` - Event type mapping
- `/tmp/beyla/pkg/internal/pipe/` - Pipeline pattern inspiration
- Tapio production standards: `CLAUDE.md`

---

## Coordination with Agent 1

**Daily sync points:**
- End of Day 1: Base infrastructure API review
- End of Day 2: Integration test (Agent 2's observer + Agent 1's decoder)
- End of Day 4: Full flow demo (eBPF → Decoder → Enricher → NATS)

---

**Questions? Check with main agent or refer to:**
- `README.md` - Architecture overview
- `CLAUDE.md` - Production standards (ZERO tolerance for violations!)
- ADR 002 - Observer consolidation plan
