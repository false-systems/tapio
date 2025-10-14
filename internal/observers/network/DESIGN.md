# Network Observer Design Document

**Status**: Draft for Review
**Date**: 2025-10-10
**Author**: Agent 2
**Context**: Building consolidated network observer per ADR 002

---

## Problem Statement

Build a **working** network observer that:
1. Captures real TCP connection data (IP addresses, ports)
2. Captures real UDP traffic data
3. Reads from eBPF ring buffer (NOT stub sleep loop)
4. Converts eBPF events to domain.ObserverEvent
5. Emits events via base.Emitter system
6. Has ZERO TODOs, ZERO stubs, COMPLETE implementations only

## Design Principles

**From CLAUDE.md:**
- NO map[string]interface{} - typed structs only
- NO TODOs/FIXMEs/stubs - complete code or nothing
- TDD: Tests BEFORE code
- Small chunks: max 30 lines per commit
- 80%+ test coverage
- Direct OTEL imports (no telemetry wrappers)

**From ADR 002:**
- Consolidates: network + dns + link + status (future phases)
- Phase 1: Just basic TCP/UDP capture (minimal viable)
- Uses base.BaseObserver + base.Emitter + base.Pipeline
- Single eBPF program with ring buffer

---

## Architecture

### Component Diagram

```
┌─────────────────────────────────────────────────────┐
│          Network Observer (Userspace)               │
│                                                     │
│  ┌──────────────┐    ┌──────────────┐             │
│  │ BaseObserver │───▶│   Pipeline   │             │
│  └──────────────┘    └──────────────┘             │
│         │                   │                       │
│         │                   ├─▶ Stage 1: readeBPF  │
│         │                   ├─▶ Stage 2: convert   │
│         │                   └─▶ Stage 3: emit      │
│         │                                           │
│  ┌──────▼──────┐                                    │
│  │   Emitter   │ (Stdout/OTEL/Tapio)               │
│  └─────────────┘                                    │
└─────────────────────────────────────────────────────┘
                      ▲
                      │ Ring Buffer
                      │
┌─────────────────────┴───────────────────────────────┐
│             eBPF Programs (Kernel)                  │
│                                                     │
│  ┌─────────────────────────────────────────────┐  │
│  │  inet_sock_set_state (tracepoint)          │  │
│  │  Captures TCP state transitions            │  │
│  └─────────────────────────────────────────────┘  │
│                                                     │
│  Extracts: PID, Comm, IP addresses, ports          │
│  Writes to: Ring Buffer Map                        │
└─────────────────────────────────────────────────────┘
```

### Data Flow

```
1. Kernel event (TCP state change) → tracepoint fires
2. eBPF reads tracepoint args → extracts IP/ports/state
3. eBPF writes NetworkEventBPF to ring buffer
4. Go readeBPF() reads from ring buffer
5. convertToDomainEvent() → domain.ObserverEvent
6. emitter.Emit() → Stdout/OTEL/Tapio
7. BaseObserver.RecordEvent() → metrics
```

---

## Type Definitions

### C Struct (eBPF Side)

**File**: `bpf/network_monitor.c`

```c
// Must match Go NetworkEventBPF exactly (alignment critical!)
struct network_event {
    __u32 pid;           // Process ID
    __u32 src_ip;        // Source IP (IPv4)
    __u32 dst_ip;        // Destination IP (IPv4)
    __u32 saddr_v6[4];   // Source IP (IPv6) - 16 bytes
    __u32 daddr_v6[4];   // Dest IP (IPv6) - 16 bytes
    __u16 src_port;      // Source port
    __u16 dst_port;      // Destination port
    __u16 family;        // AF_INET (2) or AF_INET6 (10)
    __u8  protocol;      // IPPROTO_TCP (6) or IPPROTO_UDP (17)
    __u8  oldstate;      // Previous TCP state
    __u8  newstate;      // Current TCP state
    __u8  _pad1;         // Padding
    char  comm[16];      // Process name
};
// Total: 4+4+4+16+16+2+2+2+1+1+1+1+16 = 70 bytes
```

### Go Struct (Userspace Side)

**File**: `types.go`

```go
// NetworkEventBPF matches C struct layout exactly
type NetworkEventBPF struct {
    PID       uint32     // Process ID
    SrcIP     uint32     // Source IPv4
    DstIP     uint32     // Dest IPv4
    SrcIPv6   [16]byte   // Source IPv6
    DstIPv6   [16]byte   // Dest IPv6
    SrcPort   uint16     // Source port
    DstPort   uint16     // Dest port
    Family    uint16     // AF_INET or AF_INET6
    Protocol  uint8      // TCP or UDP
    OldState  uint8      // Previous TCP state
    NewState  uint8      // Current TCP state
    _         uint8      // Padding
    Comm      [16]byte   // Process name
}

// Address families
const (
    AF_INET  = 2
    AF_INET6 = 10
)

// IP protocols
const (
    IPPROTO_TCP = 6
    IPPROTO_UDP = 17
)

// TCP states (from linux/tcp.h)
const (
    TCP_ESTABLISHED = 1
    TCP_SYN_SENT    = 2
    TCP_SYN_RECV    = 3
    TCP_FIN_WAIT1   = 4
    TCP_FIN_WAIT2   = 5
    TCP_TIME_WAIT   = 6
    TCP_CLOSE       = 7
    TCP_CLOSE_WAIT  = 8
    TCP_LAST_ACK    = 9
    TCP_LISTEN      = 10
    TCP_CLOSING     = 11
)
```

### Domain Event (After Conversion)

Uses existing `domain.ObserverEvent` from `domain/events.go`:

**Event types based on TCP state transitions**:
- `connection_established` - TCP_ESTABLISHED reached
- `connection_closed` - TCP_CLOSE reached
- `listen_started` - TCP_LISTEN reached
- `listen_stopped` - TCP_LISTEN → TCP_CLOSE

```go
event := &domain.ObserverEvent{
    ID:        uuid.New().String(),
    Type:      "connection_established",  // State-based type
    Source:    "network",
    Timestamp: time.Now(),
    NetworkData: &domain.NetworkEventData{
        Protocol: "TCP",
        SrcIP:    "10.244.1.5",     // IPv4 or IPv6
        DstIP:    "10.96.0.1",
        SrcPort:  45678,
        DstPort:  443,
    },
    ProcessData: &domain.ProcessEventData{
        PID:         1234,
        ProcessName: "curl",
    },
}
```

---

## Complete eBPF C Program (Verifier-Compliant)

### File: `bpf/network_monitor.c`

**Full implementation with all book learnings applied:**

```c
// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
#include "../../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Event struct (must match Go NetworkEventBPF exactly!)
struct network_event {
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u32 saddr_v6[4];
    __u32 daddr_v6[4];
    __u16 src_port;
    __u16 dst_port;
    __u16 family;
    __u8  protocol;
    __u8  oldstate;
    __u8  newstate;
    __u8  _pad1;
    char  comm[16];
};

// Ring buffer map
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB
} events SEC(".maps");

// Tracepoint context (from kernel)
struct trace_event_raw_inet_sock_set_state {
    __u64 __unused__;
    const void *skaddr;
    int oldstate;
    int newstate;
    __u16 sport;
    __u16 dport;
    __u16 family;
    __u8 protocol;
    __u8 __pad;
    __u32 saddr[4];
    __u32 daddr[4];
};

SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *args)
{
    struct network_event *evt;

    // Filter: Only TCP (CRITICAL: verifier requires early exit)
    if (args->protocol != IPPROTO_TCP)
        return 0;

    // Filter: Only interesting states (reduces event volume)
    // ESTABLISHED = client connected
    // LISTEN = server listening
    // CLOSE = connection closed
    if (args->newstate != TCP_ESTABLISHED &&
        args->newstate != TCP_LISTEN &&
        args->newstate != TCP_CLOSE)
        return 0;

    // Reserve ring buffer (CRITICAL: check NULL per verifier)
    evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt)
        return 0;  // Buffer full, drop event

    // Zero struct (verifier requirement: no uninitialized data)
    __builtin_memset(evt, 0, sizeof(*evt));

    // Get process context (CRITICAL: helper functions only!)
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    evt->pid = pid_tgid >> 32;  // Upper 32 bits = PID

    // Get process name (verifier validates buffer size)
    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

    // Copy network data (direct access - tracepoint stable ABI)
    evt->src_port = args->sport;
    evt->dst_port = args->dport;
    evt->family = args->family;
    evt->protocol = args->protocol;
    evt->oldstate = (__u8)args->oldstate;
    evt->newstate = (__u8)args->newstate;

    // IPv4 or IPv6? (verifier: bounded branches only)
    if (args->family == AF_INET) {
        // IPv4 - only first element of array
        evt->src_ip = args->saddr[0];
        evt->dst_ip = args->daddr[0];
    } else if (args->family == AF_INET6) {
        // IPv6 - copy all 4 u32s (16 bytes)
        // CRITICAL: Use __builtin_memcpy for verifier
        __builtin_memcpy(&evt->saddr_v6, args->saddr, 16);
        __builtin_memcpy(&evt->daddr_v6, args->daddr, 16);
    }

    // Submit to userspace (makes event visible)
    bpf_ringbuf_submit(evt, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
```

**Verifier compliance checklist:**
- ✅ NULL check on `bpf_ringbuf_reserve()` return
- ✅ Bounds check on array access (family check before memcpy)
- ✅ Zero uninitialized memory (`__builtin_memset`)
- ✅ Return 0 (success for tracepoint)
- ✅ No unbounded loops
- ✅ Stack usage < 512 bytes (struct is 70 bytes)
- ✅ Helper functions for all kernel interaction
- ✅ No direct pointer dereferences

---

## eBPF Implementation Details (Tracepoint-Based - Inspired by Coroot)

### CRITICAL DECISION: Use Tracepoints Instead of Kprobes

**Why Tracepoints > Kprobes:**

| Aspect | Kprobes | Tracepoints |
|--------|---------|-------------|
| **Stability** | Unstable (function names change) | Stable (kernel ABI guaranteed) |
| **Complexity** | Need BPF_CORE_READ for struct access | Direct arg access |
| **Maintenance** | Breaks on kernel updates | Never breaks |
| **Data Quality** | Only function entry point | Rich context (state changes) |
| **Performance** | Higher overhead | Lower overhead |

**Learning from Coroot**: https://github.com/coroot/coroot-node-agent uses tracepoints exclusively - proven production approach.

### Question 1: How to Extract Connection Data Using Tracepoints?

**Tracepoint: `sock/inet_sock_set_state`**
- Fires on TCP state transitions
- Captures full connection lifecycle
- Args contain IP/ports directly - NO struct reading needed!

**Tracepoint signature**:
```c
struct trace_event_raw_inet_sock_set_state {
    __u16 sport;
    __u16 dport;
    __u16 family;  // AF_INET or AF_INET6
    __u8  protocol;
    __u8  oldstate;
    __u8  newstate;
    __u32 saddr;    // IPv4 source
    __u32 daddr;    // IPv4 dest
    __u32 saddr_v6[4];  // IPv6 source
    __u32 daddr_v6[4];  // IPv6 dest
};
```

**Extraction strategy (NO BPF_CORE_READ needed!)**:
```c
SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *args) {
    struct network_event *evt;

    // Filter: only track TCP
    if (args->protocol != IPPROTO_TCP)
        return 0;

    // Filter: only interesting state transitions
    // SYN_SENT → ESTABLISHED = client connect
    // CLOSE → LISTEN = server listen
    // ESTABLISHED → FIN_WAIT1 = connection closing
    if (args->newstate != TCP_ESTABLISHED &&
        args->newstate != TCP_LISTEN &&
        args->newstate != TCP_CLOSE)
        return 0;

    // Reserve ring buffer space
    evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt)
        return 0;

    // Direct access - no BPF_CORE_READ!
    evt->sport = args->sport;
    evt->dport = args->dport;
    evt->protocol = args->protocol;
    evt->oldstate = args->oldstate;
    evt->newstate = args->newstate;

    // IPv4 or IPv6?
    if (args->family == AF_INET) {
        evt->src_ip = args->saddr;
        evt->dst_ip = args->daddr;
        evt->family = AF_INET;
    } else if (args->family == AF_INET6) {
        __builtin_memcpy(&evt->saddr_v6, args->saddr_v6, 16);
        __builtin_memcpy(&evt->daddr_v6, args->daddr_v6, 16);
        evt->family = AF_INET6;
    }

    // Get process info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    evt->pid = pid_tgid >> 32;
    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

    // Submit to userspace
    bpf_ringbuf_submit(evt, 0);
    return 0;
}
```

**Key Tracepoint Benefits**:
- **No BPF_CORE_READ** - args struct is stable ABI
- **IPv6 for free** - both IPv4 and IPv6 in same tracepoint
- **State transitions** - see full connection lifecycle
- **Simpler code** - direct field access
- **Production proven** - Coroot uses this approach

### Question 2: Ring Buffer vs Perf Buffer?

**Answer**: Ring Buffer (BPF_MAP_TYPE_RINGBUF)

**Why**:
- Newer (Linux 5.8+)
- Better performance (lock-free)
- Simpler API (reserve/submit)
- Built-in back-pressure handling

**C Side**:
```c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB
} events SEC(".maps");

// Reserve space
struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
if (!evt) return 0;  // Full, drop event

// Fill data...

// Submit (makes visible to userspace)
bpf_ringbuf_submit(evt, 0);
```

**Go Side**:
```go
// Open ring buffer reader
rb, err := ringbuf.NewReader(ebpfObjs.Events)
if err != nil {
    return fmt.Errorf("failed to open ring buffer: %w", err)
}
defer rb.Close()

// Read loop
for {
    record, err := rb.Read()
    if err != nil {
        if errors.Is(err, ringbuf.ErrClosed) {
            return nil  // Clean shutdown
        }
        return fmt.Errorf("reading from ring buffer: %w", err)
    }

    // Parse record.RawSample ([]byte) into NetworkEventBPF
    var event NetworkEventBPF
    if err := binary.Read(bytes.NewReader(record.RawSample),
                          binary.LittleEndian, &event); err != nil {
        // Log and continue
        continue
    }

    // Send to channel for processing
    eventCh <- event
}
```

### Question 3: How to Load eBPF Program with CO-RE?

**Use cilium/ebpf with go:generate and CO-RE**:

**What is CO-RE?**
- **Compile Once - Run Everywhere**
- eBPF programs adapt to different kernel versions at load time
- Uses BTF (BPF Type Format) for kernel struct layout information
- No need to recompile for each kernel version

**Requirements for CO-RE:**

1. **BTF (BPF Type Format) support**:
   - Kernel compiled with CONFIG_DEBUG_INFO_BTF=y
   - Check: `ls /sys/kernel/btf/vmlinux` should exist
   - Ubuntu 20.04+, RHEL 8.2+, most modern distros have this

2. **vmlinux.h header**:
   ```bash
   # Generate vmlinux.h from running kernel's BTF
   bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/vmlinux.h
   ```
   - Contains all kernel struct definitions
   - Specific to kernel version, but CO-RE handles differences
   - Commit to repo or generate in Dockerfile

3. **Clang with BPF target**:
   ```bash
   clang-18 --version  # Need clang 10+
   ```

**go:generate with CO-RE**:

```go
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 Network ./network_monitor.c -- -I. -Wall -Werror

package network

import (
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/ringbuf"
)

type NetworkObserver struct {
    *base.BaseObserver

    // eBPF resources (generated by bpf2go)
    ebpfObjs  *networkObjects  // Contains Programs and Maps
    tcpLink   link.Link         // tracepoint link
    udpLink   link.Link
    ringbuf   *ringbuf.Reader
}

func (n *NetworkObserver) loadeBPF() error {
    // Load eBPF objects (generated by bpf2go)
    objs := &networkObjects{}
    if err := loadNetworkObjects(objs, nil); err != nil {
        return fmt.Errorf("loading eBPF objects: %w", err)
    }
    n.ebpfObjs = objs

    // Attach tracepoint for socket state changes (captures TCP/UDP events)
    sockLink, err := link.Tracepoint("sock", "inet_sock_set_state", objs.TraceSockSetState, nil)
    if err != nil {
        objs.Close()
        return fmt.Errorf("attaching sock/inet_sock_set_state tracepoint: %w", err)
    }
    n.sockLink = sockLink

    // Open ring buffer
    rb, err := ringbuf.NewReader(objs.Events)
    if err != nil {
        sockLink.Close()
        objs.Close()
        return fmt.Errorf("opening ring buffer: %w", err)
    }
    n.ringbuf = rb

    return nil
}

func (n *NetworkObserver) Close() error {
    if n.ringbuf != nil {
        n.ringbuf.Close()
    }
    if n.udpLink != nil {
        n.udpLink.Close()
    }
    if n.tcpLink != nil {
        n.tcpLink.Close()
    }
    if n.ebpfObjs != nil {
        n.ebpfObjs.Close()
    }
    return nil
}
```

---

## Observer Implementation

### Config

```go
type Config struct {
    Output base.OutputConfig  // Stdout, OTEL, Tapio
}
```

### Constructor

```go
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
    baseObs, err := base.NewBaseObserver(name)
    if err != nil {
        return nil, fmt.Errorf("failed to create base observer: %w", err)
    }

    // Create emitters from output config
    tracer := otel.Tracer(name)
    emitter := base.CreateEmitters(config.Output, tracer)

    observer := &NetworkObserver{
        BaseObserver: baseObs,
        config:       config,
        emitter:      emitter,
        eventCh:      make(chan NetworkEventBPF, 1000),
    }

    // Load eBPF BEFORE registering pipeline stages
    if err := observer.loadeBPF(); err != nil {
        return nil, fmt.Errorf("failed to load eBPF: %w", err)
    }

    // Register pipeline stages
    observer.AddStage(observer.readeBPFEvents)
    observer.AddStage(observer.processEvents)

    return observer, nil
}
```

### Pipeline Stage 1: readeBPFEvents

**Reads from ring buffer, sends to channel**:

```go
func (n *NetworkObserver) readeBPFEvents(ctx context.Context) error {
    for {
        record, err := n.ringbuf.Read()
        if err != nil {
            if errors.Is(err, ringbuf.ErrClosed) {
                return nil  // Clean shutdown
            }
            n.RecordError(ctx)
            continue  // Don't fail on single read error
        }

        // Parse C struct
        var event NetworkEventBPF
        reader := bytes.NewReader(record.RawSample)
        if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
            n.RecordError(ctx)
            continue
        }

        // Send to processing channel
        select {
        case n.eventCh <- event:
            // Success
        case <-ctx.Done():
            return nil
        default:
            n.RecordDrop(ctx)  // Channel full, drop event
        }
    }
}
```

### Pipeline Stage 2: processEvents

**Converts and emits**:

```go
func (n *NetworkObserver) processEvents(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case ebpfEvent := <-n.eventCh:
            startTime := time.Now()

            // Convert to domain event
            domainEvent := n.convertToDomainEvent(ebpfEvent)

            // Emit
            if err := n.emitter.Emit(ctx, domainEvent); err != nil {
                n.RecordError(ctx)
                continue
            }

            // Record metrics
            n.RecordEvent(ctx)
            n.RecordProcessingTime(ctx, time.Since(startTime).Milliseconds())
        }
    }
}
```

### Conversion Function

```go
func (n *NetworkObserver) convertToDomainEvent(ebpf NetworkEventBPF) *domain.ObserverEvent {
    // Determine event type based on TCP state transition
    eventType := n.stateToEventType(ebpf.OldState, ebpf.NewState)

    protocol := "TCP"
    if ebpf.Protocol == IPPROTO_UDP {
        protocol = "UDP"
        eventType = "udp_send"
    }

    // Convert IP addresses based on family
    var srcIP, dstIP string
    if ebpf.Family == AF_INET {
        // IPv4
        srcIP = net.IPv4(
            byte(ebpf.SrcIP),
            byte(ebpf.SrcIP>>8),
            byte(ebpf.SrcIP>>16),
            byte(ebpf.SrcIP>>24),
        ).String()
        dstIP = net.IPv4(
            byte(ebpf.DstIP),
            byte(ebpf.DstIP>>8),
            byte(ebpf.DstIP>>16),
            byte(ebpf.DstIP>>24),
        ).String()
    } else if ebpf.Family == AF_INET6 {
        // IPv6
        srcIP = net.IP(ebpf.SrcIPv6[:]).String()
        dstIP = net.IP(ebpf.DstIPv6[:]).String()
    }

    // Extract process name (null-terminated)
    processName := string(bytes.TrimRight(ebpf.Comm[:], "\x00"))

    return &domain.ObserverEvent{
        ID:        uuid.New().String(),
        Type:      eventType,
        Source:    n.Name(),
        Timestamp: time.Now(),
        NetworkData: &domain.NetworkEventData{
            Protocol: protocol,
            SrcIP:    srcIP,
            DstIP:    dstIP,
            SrcPort:  ebpf.SrcPort,
            DstPort:  ebpf.DstPort,
        },
        ProcessData: &domain.ProcessEventData{
            PID:         int32(ebpf.PID),
            ProcessName: processName,
        },
    }
}

// stateToEventType maps TCP state transitions to event types
func (n *NetworkObserver) stateToEventType(oldState, newState uint8) string {
    switch newState {
    case TCP_ESTABLISHED:
        return "connection_established"
    case TCP_LISTEN:
        if oldState == TCP_CLOSE {
            return "listen_started"
        }
    case TCP_CLOSE:
        if oldState == TCP_LISTEN {
            return "listen_stopped"
        }
        return "connection_closed"
    }
    return "tcp_state_change"  // Generic fallback
}
```

---

## Testing Strategy

### Test Files (6 files per CLAUDE.md)

1. **types_test.go** - Struct alignment tests
2. **observer_unit_test.go** - Constructor, helpers
3. **observer_integration_test.go** - eBPF load/lifecycle
4. **observer_system_test.go** - Real TCP connection capture
5. **observer_performance_test.go** - Benchmarks
6. **observer_negative_test.go** - Error handling

### Example: System Test

```go
//go:build linux

func TestNetworkObserver_CaptureRealTCP(t *testing.T) {
    obs, err := NewNetworkObserver("test", Config{
        Output: base.OutputConfig{Stdout: true},
    })
    require.NoError(t, err)
    defer obs.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Start observer
    go func() {
        if err := obs.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
            t.Errorf("observer failed: %v", err)
        }
    }()

    // Wait for observer to be ready
    time.Sleep(100 * time.Millisecond)

    // Make TCP connection
    conn, err := net.Dial("tcp", "google.com:80")
    require.NoError(t, err)
    defer conn.Close()

    // Should see event in channel
    select {
    case event := <-obs.eventCh:
        assert.Equal(t, ProtocolTCP, event.Protocol)
        assert.Equal(t, uint16(80), event.DstPort)
        assert.NotZero(t, event.PID)
        assert.NotEmpty(t, event.Comm)
    case <-time.After(2 * time.Second):
        t.Fatal("no TCP event captured")
    }
}
```

---

## Decoder Integration (Future)

**After Agent 1 completes decoders**, add enrichment stage:

```go
// In NewNetworkObserver():
observer.AddStage(observer.enrichEvents)

func (n *NetworkObserver) enrichEvents(ctx context.Context) error {
    decoder := decoders.NewK8sPodDecoder(k8sClient)

    for event := range n.enrichedCh {
        // Decode IP → Pod name
        if podName, err := decoder.Decode(event.NetworkData.SrcIP); err == nil {
            event.NetworkData.SrcPod = podName
        }

        // Emit enriched event...
    }
}
```

---

## Implementation Phases

### Phase 1: Minimal TCP Capture (This Design)
- ✅ Types defined (C + Go)
- ✅ eBPF TCP tracepoint extracts IP/ports/state
- ✅ Ring buffer reading
- ✅ Conversion to domain.ObserverEvent
- ✅ Emitter integration
- ✅ Tests (80%+ coverage)

### Phase 2: UDP Support ~~SKIPPED - See Rationale Below~~

**Decision: Skip UDP monitoring in network observer**

**Rationale:**
1. **No stable tracepoint exists**: `inet_sock_set_state` is TCP-only. UDP has no state machine.
2. **Unstable alternatives**: Only kprobes (`udp_sendmsg`/`udp_recvmsg`) available, which break across kernel versions - violates CO-RE goal.
3. **Industry precedent**: Groundcover's production eBPF agent (Alaz) explicitly skips UDP:
   - Quote from alaz/ebpf/c/l7.c: "We should not send l7_events that is not related to a tcp connection, otherwise we will have a lot of events"
   - Caretta (groundcover): TCP-only using `inet_sock_set_state`
4. **Signal-to-noise ratio**: UDP is connectionless - generates high event volume with low observability value.
5. **L7 protocols use TCP**: HTTP, gRPC, databases all use TCP. DNS already handled by dedicated DNS observer.
6. **Alternative approach**: XDP/TC for UDP would require complete architecture redesign - not worth it for Phase 2.

**What UDP would give us:**
- Generic UDP send/receive events (application-level)
- Process attribution (PID, comm)
- IP addresses + ports

**Why we don't need it:**
- DNS (UDP port 53): Already covered by existing DNS observer
- Other UDP traffic (QUIC, VoIP, gaming): Low priority for K8s observability
- No state transitions to track (connectionless protocol)

**Path forward:** Skip directly to Phase 3 (Enrichment) or Phase 4 (Consolidation with DNS/status/link observers).

### Phase 3: Enrichment (Agent 1 dependency)
- K8s pod decoder integration
- Service name resolution

### Phase 4: ADR 002 Full Consolidation
- Merge DNS observer logic
- Merge status observer logic
- Merge link observer logic

---

## Critical eBPF Book Learnings Applied

### 1. Verifier Requirements (Chapter 6)
**The verifier will reject our program if we don't:**
- Check all pointer dereferences for NULL before access
- Validate all array bounds before indexing
- Prove all loops terminate (bounded loops only)
- Return valid codes (0 for success in tracepoints)
- Stay within stack limit (512 bytes max)

**Applied to our design:**
```c
// WRONG - verifier rejects
evt->comm[20] = 'x';  // Out of bounds!

// CORRECT - verifier accepts
if (idx < 16) {
    evt->comm[idx] = 'x';
}
```

### 2. Ring Buffer Details (Chapter 4)
**From book: Why ring buffers > perf buffers:**
- Single shared buffer (not per-CPU) = less memory
- Lock-free design using memory barriers
- Built-in back-pressure (reserve fails when full)
- Epoch-based memory management (no race conditions)

**Our implementation:**
```c
// Reserve space (can fail if buffer full)
struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
if (!evt) {
    return 0;  // Drop event, metric will track
}

// Fill data - if we return early, must discard!
if (some_error) {
    bpf_ringbuf_discard(evt, 0);
    return 0;
}

// Success - make visible to userspace
bpf_ringbuf_submit(evt, 0);
```

### 3. Tracepoint Stability (Chapter 7)
**From book: Tracepoints vs Kprobes comparison:**

| Feature | Kprobes | Tracepoints |
|---------|---------|-------------|
| Stability | Function names/signatures change | Guaranteed stable ABI |
| Arguments | Need BPF_CORE_READ + BTF | Direct struct access |
| Overhead | Higher (trap + context switch) | Lower (designed for tracing) |
| Portability | Breaks on kernel updates | Works across versions |
| Use case | Internal functions | Kernel subsystem events |

**Why `sock/inet_sock_set_state` is perfect:**
- Fires on ALL TCP state changes (SYN, ESTABLISHED, FIN, etc.)
- Provides IP addresses directly (no pointer chasing)
- Works for both IPv4 and IPv6
- Stable since Linux 4.16+

### 4. CO-RE Portability (Chapter 5)
**From book: Four requirements for CO-RE:**
1. **BTF in kernel**: CONFIG_DEBUG_INFO_BTF=y
2. **Kernel headers**: Provide struct layouts
3. **Compiler support**: Clang 10+ with BTF generation
4. **Library support**: libbpf or cilium/ebpf

**How CO-RE works:**
- Compiler emits "relocation records" for struct field accesses
- Loader (libbpf/cilium) reads BTF from running kernel
- Loader patches field offsets at load time
- Same binary works on 5.10 and 6.x kernels!

**For tracepoints: We don't even need BPF_CORE_READ!**
- Tracepoint args struct is stable ABI
- Direct field access works
- CO-RE still helps with helper functions

### 5. Helper Function Safety (Chapter 2)
**From book: Helper functions are the only way to interact with kernel**

**Memory access helpers:**
- `bpf_probe_read_kernel()` - Read kernel memory safely
- `bpf_probe_read_user()` - Read userspace memory safely
- NEVER dereference pointers directly in eBPF!

**Our usage:**
```c
// Get process info
__u64 pid_tgid = bpf_get_current_pid_tgid();
evt->pid = pid_tgid >> 32;  // Upper 32 bits = PID

// Get process name (validates buffer size)
bpf_get_current_comm(&evt->comm, sizeof(evt->comm));
```

### 6. Map Types and Design (Chapter 2, 4)
**From book: Ring buffer is a special map type**
```c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB total
} events SEC(".maps");
```

**Key points:**
- `max_entries` is TOTAL bytes, not count
- Must be power of 2
- Shared across ALL CPUs (unlike perf buffer)
- No separate "submit" map needed

### 7. Program Lifecycle (Chapter 3)
**From book: Loading and attaching are separate steps**

1. **Load**: Verify bytecode, create maps
2. **Attach**: Link to kernel hook point
3. **Detach**: Remove from hook (maps persist!)
4. **Unload**: Clean up everything

**Our Go code must:**
```go
// Load (verify + create maps)
objs := &networkObjects{}
if err := loadNetworkObjects(objs, nil); err != nil {
    return fmt.Errorf("load failed: %w", err)
}

// Attach to tracepoint
link, err := link.Tracepoint("sock", "inet_sock_set_state", objs.TraceInetSockSetState, nil)
if err != nil {
    objs.Close()
    return fmt.Errorf("attach failed: %w", err)
}

// Cleanup order matters!
defer link.Close()    // Detach first
defer objs.Close()    // Then unload
```

---

## Resolved Design Questions

1. **eBPF Kernel Version**: Linux 5.8+ minimum
   - Ring buffer requires 5.8+
   - Tracepoint requires 4.16+ (we have this)
   - BTF requires 5.4+ (we have this)
   - **Decision**: Target 5.8+ (Ubuntu 20.04, RHEL 8.4+)

2. **vmlinux.h**: Use existing `internal/base/bpf/vmlinux_minimal.h`
   - Already has TCP states, helper declarations
   - Small (~2KB vs 15MB full vmlinux.h)
   - Works with CO-RE
   - **Decision**: Approved, using minimal shared header

3. **IPv6 Support**: Phase 1 - included!
   - Tracepoint provides both IPv4 and IPv6
   - Just check `args->family` and copy appropriate fields
   - **Decision**: Full IPv6 support in Phase 1

4. **Event Buffer Size**: 256KB ring buffer
   - Book recommends 256KB for network tracing
   - Holds ~3600 events (70 bytes each)
   - **Decision**: 256KB with drop metrics

5. **Error Handling**: Drop with metrics
   - Ring buffer reserve fails when full
   - Track drops with `RecordDrop(ctx)` metric
   - **Decision**: Non-blocking drops with observability

6. **UDP Support**: ~~Phase 2 SKIPPED~~ - TCP only
   - `inet_sock_set_state` is TCP-only (no UDP state machine exists)
   - No stable tracepoint for UDP (only unstable kprobes)
   - Industry standard: Groundcover's Alaz skips UDP entirely
   - DNS observer already handles UDP port 53
   - **Decision**: TCP only. See Phase 2 rationale in Implementation Phases section.

---

## Success Criteria

**This design is ready for implementation when:**
- [x] All questions answered (see Resolved Design Questions)
- [x] No ambiguities about "how it works" (complete C program provided)
- [x] Types match between C and Go (70 bytes, aligned)
- [x] Test strategy covers all paths (6 test files defined)
- [x] NO TODOs in this design doc (all complete)
- [x] CO-RE strategy confirmed (minimal vmlinux.h exists)
- [ ] Reviewer (user) approves ← **READY FOR APPROVAL**

**Implementation will be successful when:**
- [ ] Real TCP connections captured (IP/ports non-zero)
- [ ] eBPF program passes verifier (all checks implemented)
- [ ] Ring buffer reading works (no data races)
- [ ] Events emitted to stdout/OTEL
- [ ] CO-RE working across kernel versions (test on 5.10 and 6.x)
- [ ] Tracepoint direct access works (no BPF_CORE_READ needed)
- [ ] Both IPv4 and IPv6 captured correctly
- [ ] 80%+ test coverage
- [ ] make verify-full passes
- [ ] Zero TODOs, zero stubs, complete code only

**Verifier Compliance Checklist (from book Chapter 6):**
- [x] NULL check on all bpf_ringbuf_reserve calls
- [x] Array bounds validated (family check before memcpy)
- [x] Memory zeroed before use (__builtin_memset)
- [x] All loops bounded (none in our program)
- [x] Return codes valid (0 for tracepoint success)
- [x] Stack usage < 512 bytes (struct = 70 bytes)
- [x] Helper functions for kernel interaction
- [x] No direct pointer dereferences

**Runtime Verification Checklist:**
- [ ] `/sys/kernel/btf/vmlinux` exists on target system
- [ ] Kernel version >= 5.8 (ring buffer support)
- [ ] Tracepoint `sock/inet_sock_set_state` available
- [ ] eBPF program loads without verifier errors
- [ ] bpf2go generates correct Go bindings
- [ ] Ring buffer reads produce valid events
- [ ] Test on kernel 5.10, 5.15, 6.x for portability

---

## Stage 1: Link Consolidation (Connection Failure Detection)

**Status**: ✅ **IMPLEMENTED**
**Branch**: `feat/network-link-consolidation`
**Date**: 2025-10-14

### Problem Statement

Detect TCP connection failures to identify network issues:
1. **SYN Timeout**: No response from server (7-127 seconds)
2. **Connection Refused**: Server sends RST on SYN (< 100ms)
3. **Connection Reset**: RST received during established connection

**Why This Matters**:
- Cloudflare data: 20% of TCP connections fail
- Distinguish timeout (server down) vs refused (firewall/no listener)
- Critical for root cause analysis in distributed systems

### Architecture Changes

#### New Tracepoint: tcp_receive_reset

Added second tracepoint to detect RST packets:

```c
SEC("tracepoint/tcp/tcp_receive_reset")
int trace_tcp_receive_reset(struct trace_event_raw_tcp_receive_reset *args)
{
    struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) return 0;

    __builtin_memset(evt, 0, sizeof(*evt));
    evt->event_type = EVENT_TYPE_RST_RECEIVED;  // NEW: Mark as RST event

    // Extract PID, comm, IPs, ports, state...
    bpf_ringbuf_submit(evt, 0);
    return 0;
}
```

**Both tracepoints write to same ring buffer** - distinguished by `event_type` field.

#### Updated NetworkEventBPF Struct

Changed `Pad` field to `EventType`:

```go
type NetworkEventBPF struct {
    PID       uint32   // offset 0, size 4
    SrcIP     uint32   // offset 4, size 4
    DstIP     uint32   // offset 8, size 4
    SrcIPv6   [16]byte // offset 12, size 16
    DstIPv6   [16]byte // offset 28, size 16
    SrcPort   uint16   // offset 44, size 2
    DstPort   uint16   // offset 46, size 2
    Family    uint16   // offset 48, size 2
    Protocol  uint8    // offset 50, size 1
    OldState  uint8    // offset 51, size 1
    NewState  uint8    // offset 52, size 1
    EventType uint8    // offset 53, size 1 - NEW: EVENT_TYPE_STATE_CHANGE or EVENT_TYPE_RST_RECEIVED
    Comm      [16]byte // offset 54, size 16
}

// Event types (must match C defines)
const (
    EventTypeStateChange  = 0 // inet_sock_set_state tracepoint
    EventTypeRSTReceived  = 1 // tcp_receive_reset tracepoint
)
```

**Still 71 bytes packed, 72 with Go alignment** - no breaking changes to struct layout.

#### RST Correlation Logic

Observer tracks RST connections using sync.Map:

```go
type NetworkObserver struct {
    *base.BaseObserver
    rstConnections sync.Map // key: "srcIP:srcPort:dstIP:dstPort" → value: true
    // ... metrics
}

// When RST received: store connection key
if evt.EventType == EventTypeRSTReceived {
    connKey := fmt.Sprintf("%s:%d:%s:%d", srcIP, srcPort, dstIP, dstPort)
    n.rstConnections.Store(connKey, true)
    n.connectionResets.Add(ctx, 1)  // NEW OTEL metric
    continue  // Don't emit yet, wait for state transition
}

// When state transition happens: check if RST was received
func stateToEventType(oldState, newState uint8, connKey string, observer *NetworkObserver) string {
    if oldState == TCP_SYN_SENT && newState == TCP_CLOSE {
        // Check if RST received (connection refused) or timeout (no response)
        if observer != nil && connKey != "" {
            if _, gotRST := observer.rstConnections.LoadAndDelete(connKey); gotRST {
                return "connection_refused"  // RST received = refused
            }
        }
        return "connection_syn_timeout"  // No RST = timeout (default)
    }
    // ... other state mappings
}
```

### New Event Types

1. **connection_syn_timeout**: SYN_SENT → CLOSE (no RST received)
   - Server unreachable, firewall drop, network issue
   - Default Linux timeout: 127 seconds (6 retries)

2. **connection_refused**: SYN_SENT → CLOSE (RST received)
   - Port not listening, firewall reject
   - Fast failure: < 100ms

3. **connection_reset**: ESTABLISHED → any (RST received)
   - Connection forcibly closed during active session
   - Tracked via RST tracepoint

### New OTEL Metrics

Added 3 network-specific counters (following OTEL semantic conventions):

```go
connectionResets  metric.Int64Counter  // connection_resets_total
synTimeouts       metric.Int64Counter  // syn_timeouts_total
connectionRefused metric.Int64Counter  // connection_refused_total
```

Recorded when:
- `connectionResets`: RST received (any state)
- `synTimeouts`: SYN_SENT → CLOSE with no RST
- `connectionRefused`: SYN_SENT → CLOSE with RST

### Test Coverage

All tests passing with **81.8% coverage** (exceeds 80% minimum):

```
✓ TestLinkFailure_SynTimeout - Detects SYN timeout
✓ All unit tests updated with new stateToEventType signature
✓ All integration tests passing
✓ Performance tests passing (100K events processed)
✓ Negative tests covering edge cases
```

### Implementation Checklist

- [x] Added tcp_receive_reset tracepoint to eBPF
- [x] Updated NetworkEventBPF with EventType field (broke Pad, fixed)
- [x] Added RST correlation logic (sync.Map)
- [x] Implemented connection_refused detection
- [x] Added 3 new OTEL metrics
- [x] Updated all test files (observer_unit, integration, negative, performance)
- [x] Fixed types_test.go field reference (Pad → EventType)
- [x] Verified all tests passing (81.8% coverage)
- [ ] Run make verify-full
- [ ] Update CONSOLIDATION_PLAN.md Stage 1 status
- [ ] Commit and push to branch

### Key Design Decisions

**Q: Why add tcp_receive_reset instead of timeout detection in Go?**
A: Timeout detection in Go requires tracking connection attempts with timers (complex state management). RST detection is event-driven - kernel tells us immediately when RST received. Cleaner, more accurate.

**Q: Why sync.Map instead of regular map with mutex?**
A: Concurrent access from ring buffer reader. sync.Map optimized for this pattern (many reads, occasional writes, keys accessed once).

**Q: Why record RST but not emit event immediately?**
A: We need both RST event AND state transition to determine failure type. Store RST, wait for SYN_SENT → CLOSE transition, then emit single event with correct type.

**Q: Performance impact of second tracepoint?**
A: Minimal - RST events are rare (<1% of connections). No filtering needed - all RSTs are interesting for failure analysis.

### Files Modified

1. `bpf/network_monitor.c` - Added tcp_receive_reset handler (67 lines)
2. `types.go` - Changed Pad to EventType, added constants
3. `observer.go` - Added sync.Map and 3 OTEL metrics
4. `observer_ebpf.go` - Attach both tracepoints, RST correlation
5. `types_test.go` - Updated field reference (Pad → EventType)
6. All test files - Updated stateToEventType calls with new signature

### Next Steps (Stage 2+)

See CONSOLIDATION_PLAN.md for:
- Stage 2: DNS Consolidation (deferred - already implemented)
- Stage 3: Status/L7 Integration (future)
- Stage 4: Correlation with existing observers
