# Container Observer Reference Architecture

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Based on**: Network Observer Implementation (Proven Pattern)
**Date**: 2025-10-26
**Purpose**: Implementation guide using existing Tapio patterns

---

## 🏗️ Tapio Observer Architecture (Proven Pattern)

### Directory Structure

```
internal/observers/container/
├── bpf/
│   ├── container_monitor.c          # eBPF program (single file)
│   ├── generate.go                  # bpf2go generation
│   ├── container_x86_bpfel.go       # Generated (x86)
│   └── container_arm64_bpfel.go     # Generated (ARM)
│
├── observer.go                      # Core observer + config
├── observer_ebpf.go                 # eBPF lifecycle (Start, stages)
├── types.go                         # Event structs + constants
├── types_test.go                    # Type tests
│
├── processor_oom.go                 # OOM processor
├── processor_oom_test.go            # OOM tests
├── processor_exit.go                # Exit processor
├── processor_exit_test.go           # Exit tests
├── processor_syscall.go             # Syscall processor
├── processor_syscall_test.go        # Syscall tests
│
├── cgroup_monitor.go                # cgroup reader
├── cgroup_monitor_test.go           # cgroup tests
│
├── observer_unit_test.go            # Unit tests
├── observer_integration_test.go     # Integration tests
├── observer_e2e_test.go             # E2E tests
├── observer_negative_test.go        # Error handling tests
├── observer_performance_test.go     # Performance benchmarks
└── observer_system_test.go          # Linux-specific tests
```

---

## 📦 Key Files from Network Observer (Reference)

### 1. `observer.go` - Core Observer Structure

**Pattern**: BaseObserver + Config + OTEL Metrics

```go
// internal/observers/network/observer.go

package network

import (
    "github.com/cilium/ebpf"
    "github.com/yairfalse/tapio/internal/base"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

// Config holds observer configuration
type Config struct {
    Output           base.OutputConfig
    EventChannelSize int // Ring buffer → processor channel size

    // Optional K8s enrichment
    K8sContextService K8sContextGetter
}

// NetworkObserver tracks network events using eBPF
type NetworkObserver struct {
    *base.BaseObserver
    config  Config
    ebpfMgr *base.EBPFManager  // eBPF lifecycle manager

    // eBPF map references (nil when not loaded)
    connStatsMap *ebpf.Map

    // Network-specific OTEL metrics
    connectionResets  metric.Int64Counter
    synTimeouts       metric.Int64Counter
    // ... more metrics
}

// NewNetworkObserver creates observer with OTEL metrics
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
    // 1. Create base observer
    baseObs, err := base.NewBaseObserver(name)
    if err != nil {
        return nil, fmt.Errorf("failed to create base observer: %w", err)
    }

    // 2. Create OTEL metrics
    meter := otel.Meter("tapio.observer.network")

    connectionResets, err := meter.Int64Counter(
        "connection_resets_total",
        metric.WithDescription("Total TCP resets"),
        metric.WithUnit("{resets}"),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create metric: %w", err)
    }

    // 3. Return observer
    return &NetworkObserver{
        BaseObserver:     baseObs,
        config:           config,
        connectionResets: connectionResets,
    }, nil
}
```

**Key Learnings**:
- ✅ Embed `*base.BaseObserver` (provides lifecycle, pipeline, logging)
- ✅ Store eBPF map references (NOT the full objects - those are in ebpfMgr)
- ✅ Create OTEL metrics in constructor
- ✅ Return errors with context (`fmt.Errorf(...: %w", err)`)

---

### 2. `observer_ebpf.go` - eBPF Lifecycle + Stages

**Pattern**: 2-Stage Pipeline (Load → Process)

```go
// internal/observers/network/observer_ebpf.go

// Start implements Observer interface
func (n *NetworkObserver) Start(ctx context.Context) error {
    // Create event channel (buffered)
    eventCh := make(chan NetworkEventBPF, n.config.EventChannelSize)

    // Stage 1: Load eBPF + Read ring buffer
    n.AddStage(func(ctx context.Context) error {
        return n.loadAndAttachStage(ctx, eventCh)
    })

    // Stage 2: Process events from channel
    n.AddStage(func(ctx context.Context) error {
        return n.processEventsStage(ctx, eventCh)
    })

    // Let BaseObserver run pipeline
    return n.BaseObserver.Start(ctx)
}

// loadAndAttachStage loads eBPF, attaches tracepoints, reads ring buffer
func (n *NetworkObserver) loadAndAttachStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
    defer close(eventCh)  // Signal processor stage when done

    // 1. Load eBPF objects (bpf2go-generated)
    objs := &bpf.NetworkObjects{}
    if err := bpf.LoadNetworkObjects(objs, nil); err != nil {
        return fmt.Errorf("failed to load eBPF: %w", err)
    }
    defer objs.Close()

    // 2. Store map reference for processors
    n.connStatsMap = objs.ConnStats

    // 3. Create eBPF manager (nil collection - we have typed objects)
    n.ebpfMgr = base.NewEBPFManagerFromCollection(nil)
    defer n.ebpfMgr.Close()

    // 4. Attach tracepoints
    if err := n.ebpfMgr.AttachTracepointWithProgram(
        objs.TraceInetSockSetState,
        "sock",
        "inet_sock_set_state",
    ); err != nil {
        return fmt.Errorf("failed to attach tracepoint: %w", err)
    }

    log.Printf("[%s] eBPF loaded and attached", n.Name())

    // 5. Read ring buffer events
    rb := objs.Events
    reader, err := ringbuf.NewReader(rb)
    if err != nil {
        return fmt.Errorf("failed to open ring buffer: %w", err)
    }
    defer reader.Close()

    for {
        record, err := reader.Read()
        if err != nil {
            if errors.Is(err, ringbuf.ErrClosed) {
                return nil  // Clean shutdown
            }
            log.Printf("[%s] Ring buffer error: %v", n.Name(), err)
            continue
        }

        // Parse event
        var evt NetworkEventBPF
        if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
            log.Printf("[%s] Parse error: %v", n.Name(), err)
            continue
        }

        // Send to processor (non-blocking)
        select {
        case eventCh <- evt:
            // Success
        default:
            // Channel full - drop event
            n.RecordDrop(ctx, "network_event")
        }
    }
}

// processEventsStage processes events from channel
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
    // Initialize processors (Brendan Gregg pattern)
    linkProc := NewLinkProcessor()
    dnsProc := NewDNSProcessor()
    statusProc := NewStatusProcessor()

    for {
        select {
        case <-ctx.Done():
            return nil

        case evt, ok := <-eventCh:
            if !ok {
                return nil  // Channel closed
            }

            // Try processors in order (fast exit on match)
            if domainEvent := linkProc.Process(ctx, evt); domainEvent != nil {
                n.emitDomainEvent(ctx, domainEvent)
                continue
            }

            if domainEvent := dnsProc.Process(ctx, evt); domainEvent != nil {
                n.emitDomainEvent(ctx, domainEvent)
                continue
            }

            // ... more processors

            // Fallback: legacy event handling
            n.processLegacyEvent(ctx, evt)
        }
    }
}
```

**Key Learnings**:
- ✅ 2-stage pipeline: `loadAndAttachStage` → `processEventsStage`
- ✅ Buffered channel between stages (backpressure)
- ✅ `defer close(eventCh)` in stage 1 signals stage 2
- ✅ `defer objs.Close()` cleans up eBPF resources
- ✅ Non-blocking send to channel (drop on full)
- ✅ Processor chain pattern (try each, fast exit)

---

### 3. `processor_*.go` - Userspace Parsing (Brendan Gregg Pattern)

**Pattern**: eBPF Captures, Userspace Parses

```go
// internal/observers/network/processor_link.go

type LinkProcessor struct {
    // Future: OTEL metrics
}

func NewLinkProcessor() *LinkProcessor {
    return &LinkProcessor{}
}

// Process checks if event indicates link failure
func (p *LinkProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Detect SYN timeout: TCP_SYN_SENT → TCP_CLOSE
    if p.isSYNTimeout(evt) {
        return p.createLinkFailureEvent(evt, "syn_timeout")
    }

    return nil  // Not a link failure
}

func (p *LinkProcessor) isSYNTimeout(evt NetworkEventBPF) bool {
    return evt.OldState == TCP_SYN_SENT && evt.NewState == TCP_CLOSE
}

func (p *LinkProcessor) createLinkFailureEvent(evt NetworkEventBPF, failureType string) *domain.ObserverEvent {
    // Convert IP addresses (handle IPv4 AND IPv6)
    var srcIP, dstIP string
    if evt.Family == AF_INET {
        srcIP = convertIPv4(evt.SrcIP)
        dstIP = convertIPv4(evt.DstIP)
    } else {
        srcIP = convertIPv6(evt.SrcIPv6)
        dstIP = convertIPv6(evt.DstIPv6)
    }

    return &domain.ObserverEvent{
        Type:    "network",
        Subtype: "link_failure",
        NetworkData: &domain.NetworkEventData{
            Protocol: "TCP",
            SrcIP:    srcIP,
            DstIP:    dstIP,
            SrcPort:  evt.SrcPort,
            DstPort:  evt.DstPort,
            TCPState: tcpStateName(evt.NewState),
        },
    }
}
```

**Key Learnings**:
- ✅ Processor returns `*domain.ObserverEvent` or `nil`
- ✅ `nil` = "not my event, try next processor"
- ✅ Handle both IPv4 AND IPv6 (MANDATORY!)
- ✅ Use existing `domain.*EventData` structs (NO new structs!)
- ✅ Keep processors simple (single responsibility)

---

### 4. `bpf/network_monitor.c` - eBPF Program (Minimal Capture)

**Pattern**: Brendan Gregg - Capture Raw Data, Don't Parse

```c
// internal/observers/network/bpf/network_monitor.c

//go:build ignore

#include "../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Shared libraries (Cilium pattern)
#include "../../common/bpf/lib/tcp.h"
#include "../../common/bpf/lib/metrics.h"

// Event types
#define EVENT_TYPE_STATE_CHANGE  0
#define EVENT_TYPE_RST_RECEIVED  1
#define EVENT_TYPE_RETRANSMIT    2

// Event struct (MUST match Go NetworkEventBPF exactly)
struct network_event {
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u8  src_ipv6[16];
    __u8  dst_ipv6[16];
    __u16 src_port;
    __u16 dst_port;
    __u16 family;
    __u8  protocol;
    __u8  old_state;
    __u8  new_state;
    __u8  event_type;
    __u8  comm[16];
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB
} events SEC(".maps");

// LRU map for connection tracking
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, struct conn_key);      // From tcp.h
    __type(value, struct retransmit_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);  // Persist across restarts
} conn_stats SEC(".maps");

// Tracepoint hook
SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *args)
{
    // Filter: Only TCP
    if (args->protocol != IPPROTO_TCP) {
        return 0;
    }

    // Allocate event from ring buffer
    struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;  // Buffer full, drop event
    }

    // Capture raw data (NO PARSING!)
    evt->pid = bpf_get_current_pid_tgid() >> 32;
    evt->old_state = args->oldstate;
    evt->new_state = args->newstate;
    evt->src_port = args->sport;
    evt->dst_port = args->dport;
    evt->family = args->family;
    evt->protocol = args->protocol;
    evt->event_type = EVENT_TYPE_STATE_CHANGE;

    if (args->family == AF_INET) {
        __builtin_memcpy(&evt->src_ip, args->saddr, 4);
        __builtin_memcpy(&evt->dst_ip, args->daddr, 4);
    } else {
        __builtin_memcpy(evt->src_ipv6, args->saddr_v6, 16);
        __builtin_memcpy(evt->dst_ipv6, args->daddr_v6, 16);
    }

    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

    // Submit event to ring buffer
    bpf_ringbuf_submit(evt, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
```

**Key Learnings**:
- ✅ Include shared libraries (`tcp.h`, `metrics.h`)
- ✅ Event struct packed + matches Go struct EXACTLY
- ✅ Ring buffer for events (per-CPU for performance)
- ✅ LRU maps for connection tracking (auto-evict old entries)
- ✅ Capture raw data only (NO parsing, NO string operations)
- ✅ `bpf_ringbuf_reserve` → `bpf_ringbuf_submit` pattern
- ✅ Always check return values (verifier requirement)

---

### 5. `bpf/generate.go` - bpf2go Code Generation

**Pattern**: Auto-generate Go bindings from C

```go
// internal/observers/network/bpf/generate.go

package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 Network ./network_monitor.c -- -I. -I../../../base/bpf -I../../../observers/common/bpf/lib -Wall -Werror
```

**Generates**:
- `network_x86_bpfel.go` - x86 eBPF bytecode
- `network_arm64_bpfel.go` - ARM eBPF bytecode
- `NetworkObjects`, `NetworkPrograms`, `NetworkMaps` structs

**Run**: `go generate ./internal/observers/network/bpf`

---

### 6. Shared eBPF Libraries (Cilium Pattern)

**Location**: `internal/observers/common/bpf/lib/`

**Available**:
- `tcp.h` - TCP states, protocol numbers, CO-RE socket structs
- `conn_tracking.h` - `struct conn_key`, `struct retransmit_stats`
- `metrics.h` - Per-CPU metrics (lock-free counters)

**Usage**:
```c
#include "../../common/bpf/lib/tcp.h"   // ✅ TCP-specific
#include "../../common/bpf/lib/metrics.h"  // ✅ Metrics

// Use shared structs
struct conn_key key;
key.saddr = src_ip;
key.daddr = dst_ip;

// Use shared constants
if (args->newstate == TCP_ESTABLISHED) { ... }
```

---

### 7. BaseObserver Features (Inherited)

**From**: `internal/base/observer.go`

**Provides**:
- ✅ `Start(ctx) error` - Pipeline execution
- ✅ `Stop() error` - Graceful shutdown
- ✅ `Name() string` - Observer name
- ✅ `IsHealthy() bool` - Health check
- ✅ `AddStage(fn)` - Add pipeline stage
- ✅ `RecordEvent(ctx)` - Increment events counter
- ✅ `RecordDrop(ctx, type)` - Increment drops counter
- ✅ `RecordError(ctx, evt)` - Increment errors counter
- ✅ `RecordProcessingTime(ctx, evt, ms)` - Record latency
- ✅ `SendObserverEvent(ctx, evt)` - OTLP export (community)
- ✅ `PublishEvent(ctx, subject, evt)` - NATS publish (enterprise)
- ✅ Structured logging with trace context
- ✅ OTEL metrics (events_total, drops_total, errors_total)

**No Need to Implement**:
- Lifecycle management → BaseObserver handles it
- Metrics → BaseObserver provides base metrics
- Logging → BaseObserver provides logger
- Pipeline → BaseObserver runs stages

**Only Implement**:
- `Start()` - Set up your pipeline stages
- Observer-specific OTEL metrics
- Event processing logic

---

## 🎯 Container Observer Implementation Checklist

### Files to Create (Based on Network Observer)

**Core**:
- [ ] `observer.go` - Config + NewContainerObserver + OTEL metrics
- [ ] `observer_ebpf.go` - Start + loadAndAttachStage + processEventsStage
- [ ] `types.go` - ContainerEventBPF struct + constants
- [ ] `types_test.go` - Type tests

**Processors** (Brendan Gregg pattern):
- [ ] `processor_oom.go` + `processor_oom_test.go`
- [ ] `processor_exit.go` + `processor_exit_test.go`
- [ ] `processor_syscall.go` + `processor_syscall_test.go`

**cgroup Monitor**:
- [ ] `cgroup_monitor.go` - Read cgroupfs every 1s
- [ ] `cgroup_monitor_test.go` - cgroup tests

**eBPF**:
- [ ] `bpf/container_monitor.c` - eBPF program (3 hooks: OOM, exit, syscall)
- [ ] `bpf/generate.go` - bpf2go generation

**Tests** (MANDATORY per CLAUDE.md):
- [ ] `observer_unit_test.go` - Unit tests
- [ ] `observer_integration_test.go` - Integration tests
- [ ] `observer_e2e_test.go` - E2E tests
- [ ] `observer_negative_test.go` - Error handling
- [ ] `observer_performance_test.go` - Benchmarks
- [ ] `observer_system_test.go` - Linux-specific tests

---

## 🔑 Key Patterns to Follow

### 1. Pipeline Stages (BaseObserver Pattern)

```go
func (c *ContainerObserver) Start(ctx context.Context) error {
    eventCh := make(chan ContainerEventBPF, c.config.EventChannelSize)

    // Stage 1: eBPF + ring buffer
    c.AddStage(func(ctx context.Context) error {
        return c.loadAndAttachStage(ctx, eventCh)
    })

    // Stage 2: Event processing
    c.AddStage(func(ctx context.Context) error {
        return c.processEventsStage(ctx, eventCh)
    })

    return c.BaseObserver.Start(ctx)
}
```

### 2. Processor Chain (Brendan Gregg Pattern)

```go
func (c *ContainerObserver) processEventsStage(ctx context.Context, eventCh chan ContainerEventBPF) error {
    oomProc := NewOOMProcessor()
    exitProc := NewExitProcessor(c.cgroupMonitor)
    syscallProc := NewSyscallProcessor()

    for evt := range eventCh {
        // Try processors in order
        if domainEvent := oomProc.Process(ctx, evt); domainEvent != nil {
            c.emitDomainEvent(ctx, domainEvent)
            continue
        }

        if domainEvent := exitProc.Process(ctx, evt); domainEvent != nil {
            c.emitDomainEvent(ctx, domainEvent)
            continue
        }

        // ... more processors
    }
}
```

### 3. eBPF Event Struct (MUST Match Go)

```c
// C struct (eBPF)
struct container_event {
    __u64 timestamp_ns;
    __u32 pid;
    __u64 cgroup_id;
    __u8  event_type;
    // ... more fields
} __attribute__((packed));
```

```go
// Go struct (MUST match C layout EXACTLY)
type ContainerEventBPF struct {
    TimestampNs uint64
    PID         uint32
    CgroupID    uint64
    EventType   uint8
    // ... same fields, same order
}
```

### 4. Shared Libraries (Cilium Pattern)

```c
// container_monitor.c
#include "../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Shared libraries
#include "../../common/bpf/lib/metrics.h"  // Per-CPU metrics
// Add container-specific helpers if needed
```

### 5. LRU Maps (Auto-Eviction)

```c
// Container PIDs map (updated by userspace)
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);  // Fixed size
    __type(key, __u32);          // PID
    __type(value, __u64);        // cgroup_id
} container_pids SEC(".maps");

// Lookup in eBPF
__u64 *cgroup_id = bpf_map_lookup_elem(&container_pids, &pid);
if (!cgroup_id) {
    return 0;  // Not a container, skip
}
```

---

## 📋 Implementation Order (TDD)

### Week 1: eBPF Foundation

**Day 1-2**: Basic eBPF program
```bash
# RED: Write test expecting OOM event
func TestContainerObserver_OOMEvent(t *testing.T) { ... }

# GREEN: Implement container_monitor.c (kprobe/oom_kill_process)
# Implement loadAndAttachStage()

# Commit: "feat: add eBPF OOM detection" (≤30 lines)
```

**Day 3-4**: Process exit detection
```bash
# RED: Test expecting process exit event
func TestContainerObserver_ProcessExit(t *testing.T) { ... }

# GREEN: Add tracepoint/sched/sched_process_exit hook
# Commit: "feat: add process exit detection"
```

**Day 5**: Syscall failures
```bash
# RED: Test syscall failure capture
func TestContainerObserver_SyscallFailure(t *testing.T) { ... }

# GREEN: Add tracepoint/raw_syscalls/sys_exit hook
# Commit: "feat: add syscall failure tracking"
```

---

### Week 2: cgroup + Correlation

**Day 1-2**: cgroup Monitor
```bash
# RED: Test cgroup metric reading
func TestCgroupMonitor_ReadMetrics(t *testing.T) { ... }

# GREEN: Implement cgroup_monitor.go
# Read memory.current, memory.pressure, cpu.stat

# Commit: "feat: add cgroup monitor"
```

**Day 3-4**: Exit Processor (Root Cause Analysis)
```bash
# RED: Test exit categorization
func TestExitProcessor_OOMKill(t *testing.T) {
    // Mock: exit_code 137 + memory pressure 95%
    // Expect: category = "exit_oom", root_cause = "..."
}

# GREEN: Implement processor_exit.go
# Implement categorizeExit() with all cases

# Commit: "feat: add exit processor with root cause analysis"
```

---

### Week 3: Polish + Integration

**Day 1-2**: OTEL Metrics
```bash
# Add observer-specific metrics:
# - container_exits_total (by exit_code, category)
# - oom_kills_total
# - syscall_failures_total (by errno)

# Commit: "feat: add OTEL metrics"
```

**Day 3-5**: Integration + E2E Tests
```bash
# Test full flow:
# 1. Start observer
# 2. Trigger OOM in test container
# 3. Verify event with full context

# Commit: "test: add integration and e2e tests"
```

---

## 🚀 Next Steps

1. **Review this reference architecture** ✅
2. **Understand network observer code** ✅
3. **Start Week 1 TDD implementation**:
   - Create `internal/observers/container/` directory
   - Copy network observer structure
   - Adapt for container use case
   - Follow TDD: RED → GREEN → REFACTOR

**Ready to implement?**

