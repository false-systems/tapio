# ADR 002: TAPIO Observer Consolidation (18 → 12 Observers)

**Status**: Accepted
**Date**: 2025-10-05
**Deciders**: Yair + Claude (AI pair programming)
**Context**: TAPIO observer refactoring before Ukko integration
**Related**: ADR 001 (NATS JetStream)

---

## Context and Problem Statement

TAPIO currently has **18 active observers** collecting eBPF and Kubernetes events. Code analysis reveals:

**Current State** (verified by codebase inspection):
- 18 active observers (have `observer.go`)
- 4 infrastructure packages (shared code)
- 37 typed event data structs
- 68 distinct event types
- **Duplication**: `services` and `network` both track TCP connections via eBPF
- **Fragmentation**: `dns`, `link`, `status` are separate but all monitor network problems

**Problems**:
1. **Overlapping concerns**: `services` and `network` both use eBPF to track TCP connections
2. **Scattered network monitoring**: `network`, `dns`, `link`, `status` all monitor different network layers
3. **Too granular**: 68 event types for Ukko storage (inefficient)
4. **Type system bloat**: 37 data structs with significant overlap
5. **Complex correlation**: 18 event sources is hard to correlate

**ahti Impact**:
- 68 event types → 68 BadgerDB key prefixes (inefficient storage)
- Correlation engine needs to understand relationships between 18 observers
- Graph model needs vertices/edges from 18 sources

---

## Decision Drivers

- **Eliminate duplication** (services + network both tracking TCP connections)
- **Simplify before scaling** (no production users, can break things)
- **Cleaner Ukko integration** (fewer schemas = simpler plugin)
- **Performance** (fewer eBPF programs = lower overhead)
- **Maintainability** (12 observers easier than 18)
- **Deadline**: January 2026 (3 months to refactor + build Ukko)

---

## Decision

**Consolidate 18 observers into 12 focused observers**, organized by observability domain:

### Consolidation Strategy

| Consolidation | From (18 observers) | To (12 observers) | Rationale |
|---------------|---------------------|-------------------|-----------|
| **Network** | network + dns + link + status | **network-observer** | All network protocol monitoring (L3-L7) |
| **Topology** | services | **topology-observer** (rename) | Service mesh topology and dependencies |
| **Kernel** | kernel + process-signals + health | **kernel-observer** | All kernel-level events |
| **K8s API** | deployments + lifecycle | **k8s-api-observer** | Single K8s API client for all resources |
| **Keep** | container-runtime | **container-observer** | Well-scoped, no overlap |
| **Keep** | memory | **memory-observer** | Well-scoped, no overlap |
| **Keep** | scheduler | **scheduler-observer** | Well-scoped, no overlap |
| **Keep** | storage-io | **storage-observer** | Well-scoped, no overlap |
| **Keep** | node-runtime | **kubelet-observer** | Well-scoped, no overlap |
| **Keep** | systemd | **systemd-observer** | Well-scoped, no overlap |
| **Keep** | otel | **otel-observer** | Well-scoped, no overlap |
| **Keep** | base | **base-observer** | Infrastructure |

**Total**: 18 observers → 12 observers (6 mergers)

---

## Detailed Consolidation Design

### 1. network-observer (NEW - Merges 4 observers)

**Merges**: network + dns + link + status

**Why these belong together**:
- All monitor network protocols at different layers
- `dns`, `link`, `status` are "negative observers" (detect problems)
- `network` is "positive observer" (tracks normal activity)
- **Together = complete network picture** (normal + failures)

**Architecture**:
```go
type NetworkObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    // L3-L4 monitoring (from: network)
    tcpMonitor *TCPMonitor
    udpMonitor *UDPMonitor

    // L7 protocol parsing (from: network)
    httpParser *HTTPParser
    grpcParser *GRPCParser
    dnsParser  *DNSParser

    // Problem detection (merged negative observers)
    dnsProblems   *DNSProblemDetector   // from: dns observer
    statusErrors  *StatusCodeMonitor    // from: status observer
    linkFailures  *LinkFailureDetector  // from: link observer

    // eBPF state
    ebpfState *NetworkEBPFState
}
```

**Single eBPF Program**:
```c
// bpf_src/network_monitor.c (consolidated)
SEC("kprobe/tcp_v4_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    // TCP connection tracking (from: network)
    record_connection();

    // Link failure detection (from: link)
    if (is_syn_timeout()) {
        report_link_failure();
    }
}

SEC("kprobe/tcp_rcv_established")
int trace_tcp_recv(struct pt_regs *ctx) {
    // HTTP/gRPC parsing (from: network, status)
    parse_l7_protocol();

    // Status code extraction (from: status)
    if (is_http_error()) {
        report_status_error();
    }
}

SEC("kprobe/udp_sendmsg")
int trace_udp_send(struct pt_regs *ctx) {
    // DNS query tracking (from: network, dns)
    if (is_dns_port()) {
        parse_dns_query();

        // DNS problem detection (from: dns)
        if (is_slow_query() || is_timeout()) {
            report_dns_problem();
        }
    }
}
```

**Event Schema** (Ukko):
```go
type NetworkEventData struct {
    EventSubtype string // connection | http_request | http_error | dns_query | dns_timeout | link_failure
    Protocol     string // TCP | UDP | HTTP | DNS | gRPC

    // L3-L4 fields
    SrcIP   string
    DstIP   string
    SrcPort uint16
    DstPort uint16

    // L7 fields (when applicable)
    HTTPMethod     string
    HTTPPath       string
    HTTPStatusCode int
    DNSQuery       string
    DNSResponseTime int64

    // Problem fields
    ErrorType      string // slow_query | timeout | nxdomain | servfail | syn_timeout | connection_rst
    ErrorSeverity  string
}
```

**Benefits**:
- 4 eBPF programs → 1 eBPF program (4x reduction!)
- Unified network monitoring (L3 → L7)
- Complete picture: normal traffic + failures
- Reduces event types: 12 network types → 1 with `event_subtype`

---

### 2. topology-observer (RENAME from services)

**Rename**: `services` → `topology-observer`

**Why keep eBPF**: K8s service mapper NEEDS raw TCP connection data to build service graph

**Architecture**:
```go
type TopologyObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    // TCP connection tracking (eBPF - REQUIRED!)
    connectionsTracker *ConnectionTracker  // Lightweight: just IPs/ports

    // K8s enrichment
    k8sClient       kubernetes.Interface
    serviceInformer cache.SharedIndexInformer
    endpointInformer cache.SharedIndexInformer
    podInformer     cache.SharedIndexInformer

    // Service graph builder
    k8sMapper *K8sServiceMapper  // Maps connections → services
    topology  *ServiceGraph      // Builds dependency graph
}
```

**Lightweight eBPF Program** (different from network-observer):
```c
// bpf_src/topology_tracker.c
SEC("kprobe/tcp_v4_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    // ONLY capture IPs and ports (no L7 parsing!)
    struct connection_t conn = {
        .src_ip = src_ip,
        .dst_ip = dst_ip,
        .src_port = src_port,
        .dst_port = dst_port,
        .timestamp = bpf_ktime_get_ns(),
    };

    // Send to userspace for K8s enrichment
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &conn, sizeof(conn));
    return 0;
}
```

**Event Schema** (Ukko):
```go
type TopologyEventData struct {
    EventSubtype string // service_flow | dependency_detected | topology_change | traffic_anomaly

    // Service flow fields
    SourceService      string
    SourceNamespace    string
    DestinationService string
    DestinationNS      string
    FlowType           string // intra-namespace | inter-namespace | external

    // Traffic metrics
    RequestRate float64
    ErrorRate   float64
    Latency     float64
}
```

**Benefits**:
- Focused: Only tracks service-to-service relationships
- eBPF is lightweight (no L7 parsing, just IP tracking)
- Clear separation from network-observer

**Why both have eBPF is OK**:
- **Different purposes**:
  - `network-observer`: Protocol analysis (parse HTTP, DNS)
  - `topology-observer`: Service mapping (map IPs to K8s services)
- **Different eBPF complexity**:
  - `network-observer`: Heavy (parses L7 protocols)
  - `topology-observer`: Light (just captures IPs/ports)
- **Total overhead**: ~1-2% CPU (acceptable for clarity)

---

### 3. kernel-observer (NEW - Merges 3 observers)

**Merges**: kernel + process-signals + health

**Why these belong together**:
- All kernel-level observability
- `kernel`: Syscall tracking
- `process-signals`: Signal delivery (SIGKILL, SIGTERM)
- `health`: System error patterns (ENOSPC, ENOMEM)
- **Together = complete kernel observability**

**Architecture**:
```go
type KernelObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    // eBPF state
    ebpfState *KernelEBPFState
}
```

**Single eBPF Program**:
```c
// bpf_src/kernel_monitor.c
SEC("tracepoint/raw_syscalls/sys_enter")
int trace_syscall(struct trace_event_raw_sys_enter *ctx) {
    // Syscall tracking (from: kernel)
    record_syscall();
}

SEC("tracepoint/signal/signal_deliver")
int trace_signal(struct trace_event_raw_signal_deliver *ctx) {
    // Signal delivery tracking (from: process-signals)
    record_signal_delivery();
}

SEC("kprobe/do_exit")
int trace_exit(struct pt_regs *ctx) {
    // Process exit + error codes (from: health)
    int exit_code = PT_REGS_PARM1(ctx);
    if (is_error(exit_code)) {
        report_health_error(exit_code);
    }
}
```

**Event Schema** (Ukko):
```go
type KernelEventData struct {
    EventSubtype string // syscall | signal | error_pattern | process_exit

    // Syscall fields
    SyscallName string
    SyscallArgs []uint64

    // Signal fields
    SignalType int
    SourcePID  int32
    TargetPID  int32

    // Error pattern fields
    ErrorCode  int
    ErrorType  string // ENOSPC | ENOMEM | ECONNREFUSED
    Frequency  int
}
```

**Benefits**:
- 3 eBPF programs → 1 eBPF program
- Unified kernel-level observability
- Easier correlation (all kernel events in one schema)

---

### 4. k8s-api-observer (NEW - Merges 2 observers)

**Merges**: deployments + lifecycle

**Why these belong together**:
- Both watch K8s API
- `deployments`: Deployment/ConfigMap/Secret changes
- `lifecycle`: Pod/Service state transitions
- **Together = single K8s API client**

**Architecture**:
```go
type K8sAPIObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    // Single K8s client
    client kubernetes.Interface

    // Shared informer factory
    informerFactory informers.SharedInformerFactory

    // Informers for all resource types
    deploymentInformer cache.SharedIndexInformer
    podInformer        cache.SharedIndexInformer
    configMapInformer  cache.SharedIndexInformer
    secretInformer     cache.SharedIndexInformer
    serviceInformer    cache.SharedIndexInformer
}
```

**Event Schema** (Ukko):
```go
type K8sAPIEventData struct {
    EventSubtype string // deployment_change | config_change | state_transition
    ResourceKind string // Deployment | Pod | ConfigMap | Secret | Service
    ResourceName string
    Namespace    string
    Action       string // created | updated | deleted

    // Deployment change fields
    ImageChanged     bool
    ReplicasChanged  bool
    OldImage         string
    NewImage         string

    // State transition fields
    OldState string
    NewState string
    Reason   string
}
```

**Benefits**:
- 2 K8s clients → 1 K8s client (half the API calls)
- Single informer factory (shared watch connections)
- Unified K8s event stream
- Better correlation (deployment → pod state in same observer)

---

## Event Type Simplification

### Before (68 event types)
```go
const (
    EventTypeStorageIORead    EventType = "storage_io_read"
    EventTypeStorageIOWrite   EventType = "storage_io_write"
    EventTypeStorageIOFsync   EventType = "storage_io_fsync"
    EventTypeDNSQuery         EventType = "dns_query"
    EventTypeDNSTimeout       EventType = "dns_timeout"
    EventTypeDNSNXDOMAIN      EventType = "dns_nxdomain"
    // ... 62 more types
)
```

### After (12 base types with event_subtype field)
```go
const (
    EventTypeKernel          EventType = "kernel"
    EventTypeContainer       EventType = "container"
    EventTypeMemory          EventType = "memory"
    EventTypeNetwork         EventType = "network"
    EventTypeTopology        EventType = "topology"
    EventTypeStorage         EventType = "storage"
    EventTypeScheduler       EventType = "scheduler"
    EventTypeK8sAPI          EventType = "k8s_api"
    EventTypeKubelet         EventType = "kubelet"
    EventTypeSystemd         EventType = "systemd"
    EventTypeOTELTrace       EventType = "otel_trace"
    EventTypeOTELMetric      EventType = "otel_metric"
)

type CollectorEvent struct {
    Type         EventType // One of 12 base types
    EventSubtype string    // Specific operation (e.g., "read", "write", "dns_timeout")
    // ...
}
```

**Benefits for Ukko**:
- 12 BadgerDB key prefixes instead of 68
- Simpler queries: `SELECT * FROM storage WHERE event_subtype='read'`
- Better compression (event_subtype is enum)
- Easy to add new subtypes without schema changes

---

## Migration Strategy

### Phase 0: Preparation (Week 1)

**Goal**: Prepare codebase without breaking existing code

**Tasks**:
1. Add `EventSubtype string` field to all event data structs
2. Create new consolidated structs in `pkg/domain/events_v2.go`
3. Dual-write: Populate both old and new data structures
4. Update tests to validate both formats

**Success Criteria**:
- ✅ All events have `event_subtype` field
- ✅ 12 new consolidated structs defined
- ✅ All tests passing with both old and new structures

---

### Phase 1: Observer Consolidation (Weeks 2-3)

**Consolidation Order** (easiest → hardest):

**Week 2, Day 1-2: k8s-api-observer**
- Create `internal/observers/k8s-api/`
- Move deployment watch logic from `deployments/`
- Move lifecycle watch logic from `lifecycle/`
- Single `informerFactory`
- Test: Verify all K8s events captured
- Delete old `deployments/` and `lifecycle/` directories

**Week 2, Day 3-4: kernel-observer**
- Create `internal/observers/kernel/`
- Merge eBPF programs from `kernel/`, `process-signals/`, `health/`
- Consolidate into single `bpf_src/kernel_monitor.c`
- Test: Verify syscalls, signals, errors captured
- Delete old directories

**Week 2, Day 5: topology-observer**
- Rename `services/` → `topology/`
- Update package name
- Update imports across codebase
- Test: Verify service flows still working

**Week 3, Day 1-3: network-observer** (most complex)
- Create `internal/observers/network/`
- Merge eBPF from `network/`, `dns/`, `link/`, `status/`
- Consolidate into single `bpf_src/network_monitor.c`
- Integrate DNS problem detection
- Integrate status code monitoring
- Integrate link failure detection
- Test: Verify all network events captured
- Performance test: Verify eBPF overhead acceptable
- Delete old directories

**Week 3, Day 4-5: Testing & Cleanup**
- Integration tests for consolidated observers
- Performance benchmarks
- Update documentation
- Update README with new observer list

**Success Criteria**:
- ✅ 18 observers → 12 observers
- ✅ eBPF programs reduced (10 → 7)
- ✅ All tests passing
- ✅ Performance same or better
- ✅ Old directories deleted

---

### Phase 2: Ukko Integration (Week 4)

**Goal**: Integrate refactored TAPIO with Ukko

**Tasks**:
1. Add NATS client to TAPIO
2. Replace `observer.Events()` channel with `nats.Publish()`
3. Create Ukko TAPIO plugin (12 schemas)
4. Test end-to-end: TAPIO → NATS → Ukko → Query

**Success Criteria**:
- ✅ TAPIO publishes to NATS
- ✅ Ukko ingests and stores events
- ✅ Cross-node correlation working
- ✅ Queries return correct data

---

## Consequences

### Positive

1. **Eliminates duplication** - services/network no longer both tracking TCP
2. **Simpler architecture** - 12 observers easier than 18
3. **Better performance** - Fewer eBPF programs (10 → 7)
4. **Cleaner Ukko plugin** - 12 schemas vs 37 structs
5. **Easier correlation** - Related events in same observer
6. **Domain-aligned** - Observers match observability domains

### Negative

1. **Larger files** - network-observer will be large (~35 files)
2. **Migration risk** - Could break integrations (mitigated: no production users)
3. **Testing complexity** - Each consolidated observer needs more tests
4. **More work upfront** - 3-4 weeks refactoring

### Neutral

1. **Both network and topology use eBPF** - Accept ~1% extra CPU overhead for clarity
2. **Breaking change** - Event types change (acceptable: no production)

---

## Alternatives Considered and Rejected

### Alternative 1: Keep 18 Observers, Just Fix Types

**Pros**: Less work, no migration risk
**Cons**: Doesn't solve duplication or complexity
**Verdict**: Rejected - Kicks can down the road

### Alternative 2: Shared Connection Tracker (Option B)

**Idea**: Single shared eBPF connection tracker for network + topology
**Pros**: Optimal performance (single eBPF program)
**Cons**: Tight coupling between observers
**Verdict**: Rejected - Prefer simplicity (Option A)

### Alternative 3: Merge network + topology into One Observer

**Idea**: Single mega network-and-topology observer
**Pros**: Maximum simplification
**Cons**: Mixing concerns (protocols vs service graph)
**Verdict**: Rejected - Too coarse-grained

---

## Validation

### Success Metrics

**Week 1** (Preparation):
- [ ] All events have `event_subtype` field
- [ ] 12 consolidated structs defined
- [ ] All tests passing

**Week 3** (Consolidation):
- [ ] 12 active observers (down from 18)
- [ ] 7 eBPF programs (down from 10)
- [ ] Test coverage >= 80%
- [ ] Performance benchmarks show <5% overhead increase

**Week 4** (Integration):
- [ ] TAPIO → NATS → Ukko working end-to-end
- [ ] Cross-node correlation verified
- [ ] Queries return correct data
- [ ] Zero data loss under normal operation

---

## Related Decisions

- **ADR 001**: NATS JetStream for Event Streaming (this refactor enables NATS integration)
- **ADR 003**: BadgerDB + Arrow storage for Ukko (to be written)
- **ADR 004**: MVCC design for time-travel queries (to be written)

---

## References

- [TAPIO Observer Code Analysis](../analysis/tapio-observer-analysis.md)
- [Ukko Architecture](../ARCHITECTURE.md)
- [eBPF Best Practices](https://ebpf.io/what-is-ebpf/)

---

## Final Observer Mapping

| Current (18) | Files | Action | New (12) | eBPF Programs |
|--------------|-------|--------|----------|---------------|
| network | 9 | **MERGE** | network-observer | 1 (consolidated) |
| dns | 15 | **MERGE** | network-observer | - |
| link | 4 | **MERGE** | network-observer | - |
| status | 7 | **MERGE** | network-observer | - |
| services | 7 | **RENAME** | topology-observer | 1 (lightweight) |
| kernel | 7 | **MERGE** | kernel-observer | 1 (consolidated) |
| process-signals | 8 | **MERGE** | kernel-observer | - |
| health | 7 | **MERGE** | kernel-observer | - |
| deployments | 3 | **MERGE** | k8s-api-observer | 0 (K8s API only) |
| lifecycle | 6 | **MERGE** | k8s-api-observer | - |
| container-runtime | 17 | **KEEP** | container-observer | 1 |
| memory | 7 | **KEEP** | memory-observer | 1 |
| scheduler | 6 | **KEEP** | scheduler-observer | 1 |
| storage-io | 7 | **KEEP** | storage-observer | 1 |
| node-runtime | 6 | **KEEP** | kubelet-observer | 0 (kubelet API) |
| systemd | 8 | **KEEP** | systemd-observer | 0 (systemd API) |
| otel | 9 | **KEEP** | otel-observer | 0 (OTLP receiver) |
| base | 18 | **KEEP** | base-observer | 0 (infrastructure) |

**Total eBPF Programs**: 10 → 7 (30% reduction)
