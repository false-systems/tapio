# Network Observer Consolidation Plan (Phase 4)

## Executive Summary

Consolidate 4 separate observers (network TCP, DNS, status, link) into a unified **network observer** that monitors all network events from L3-L7 using eBPF.

**Goal**: Single eBPF program + single Go observer handling all network observability.

---

## Current State Analysis

### What We Have (Phase 1 Complete)
**Network Observer** - TCP state transitions only:
- **Tracepoint**: `sock:inet_sock_set_state` (stable, TCP-only)
- **Events**: TCP connection lifecycle (ESTABLISHED, CLOSE, etc.)
- **Data**: PID, process name, IP:port (IPv4/IPv6), TCP states
- **Coverage**: ~94.4% test coverage, 45 tests passing

### What Was Deleted (Old Implementation)

#### 1. DNS Observer
- **Hooks**: Kprobes on UDP/TCP syscalls (send/recv)
- **Events**: Slow queries (>100ms), timeouts (>5s), NXDOMAIN, SERVFAIL
- **Data**: Query name, latency, response codes, DNS server IP, K8s service names
- **Special**: CoreDNS detection, port 53 + 9153 monitoring
- **Problem**: Used unstable kprobes

#### 2. Status Observer
- **Hooks**: Kprobes on L7 protocol functions (HTTP/gRPC)
- **Events**: 4xx/5xx errors, timeouts (>30s), slow requests (>5s)
- **Data**: Service/endpoint hashes, HTTP status codes, latency, protocol type
- **Protocols**: HTTP/1.x, HTTP/2, gRPC, MySQL, Redis
- **Problem**: HTTP parsing in eBPF is complex and fragile

#### 3. Link Observer
- **Hooks**: Kprobes on `tcp_v4_connect`, `tcp_send_active_reset`, `tcp_finish_connect`
- **Events**: SYN timeouts, connection RSTs, ARP timeouts
- **Data**: Connection 4-tuple (src/dst IP:port), failure reason
- **Problem**: Overlaps with inet_sock_set_state tracepoint

---

## Consolidation Strategy

### Decision: 3-Stage Consolidation

**Stage 1: Enhance TCP (Link Consolidation)** ← START HERE
- Add RST detection to existing `inet_sock_set_state` tracepoint
- Detect SYN timeouts from TCP state transitions
- **Why first**: Easiest - same tracepoint, just parse more states
- **Complexity**: Low

**Stage 2: Add DNS (DNS Consolidation)**
- Add kprobes for UDP DNS (port 53 only)
- Track query/response pairs in eBPF map
- Calculate latency, detect timeouts/errors
- **Why second**: Well-defined protocol, single port
- **Complexity**: Medium

**Stage 3: Add L7 Status (Status Consolidation)** ← DEFER
- HTTP/gRPC status code extraction
- Requires payload inspection (complex)
- **Why last**: Most complex, may skip for now
- **Complexity**: High - may violate simplicity goal

---

## Stage 1: Link Consolidation (TCP Enhanced)

### What We're Adding

**New Events from inet_sock_set_state:**
1. **Connection RST**: TCP_CLOSE with RST flag
2. **SYN Timeout**: SYN_SENT → CLOSE transition
3. **Connection Refused**: SYN_SENT → CLOSE rapidly (<100ms)
4. **Half-Open**: FIN_WAIT → CLOSE_WAIT timeout

### eBPF Changes (Minimal)

**Current tracepoint** already gives us:
```c
struct trace_event_raw_inet_sock_set_state {
    int oldstate;  // ← We already capture this
    int newstate;  // ← We already capture this
    __u16 sport;
    __u16 dport;
    __u16 family;
    // ... rest we already use
};
```

**New event types** (Go side):
```go
const (
    EventTypeEstablished    = "connection.established"    // ✅ Have
    EventTypeListenStarted  = "listen.started"            // ✅ Have
    EventTypeListenStopped  = "listen.stopped"            // ✅ Have
    EventTypeClosed         = "connection.closed"         // ✅ Have

    // NEW - from state transitions
    EventTypeReset          = "connection.reset"          // NEW: ESTABLISHED → CLOSE
    EventTypeSynTimeout     = "connection.syn_timeout"    // NEW: SYN_SENT → CLOSE (>3s)
    EventTypeRefused        = "connection.refused"        // NEW: SYN_SENT → CLOSE (<100ms)
    EventTypeHalfOpen       = "connection.half_open"      // NEW: FIN_WAIT → CLOSE_WAIT timeout
)
```

### Implementation Plan

**eBPF (bpf/network_monitor.c)**: NO CHANGES NEEDED ✅
- Already captures oldstate/newstate
- Already captures timestamps

**Go (observer_ebpf.go)**: Enhance `stateToEventType()` function
```go
func stateToEventType(oldState, newState uint8) string {
    // Existing logic...

    // NEW: Detect failures
    if oldState == TCP_SYN_SENT && newState == TCP_CLOSE {
        // Check timing to distinguish timeout vs refused
        if latency > 3*time.Second {
            return EventTypeSynTimeout
        }
        return EventTypeRefused
    }

    if oldState == TCP_ESTABLISHED && newState == TCP_CLOSE {
        // Could be RST or normal close
        // Rapid transition = likely RST
        if latency < 100*time.Millisecond {
            return EventTypeReset
        }
    }

    // ... existing transitions
}
```

### Testing Requirements

**New tests** in `observer_integration_test.go`:
1. `TestLinkFailure_SynTimeout` - Connect to blackhole IP
2. `TestLinkFailure_ConnectionRefused` - Connect to closed port
3. `TestLinkFailure_ConnectionReset` - Server sends RST
4. `TestLinkFailure_HalfOpenTimeout` - FIN without ACK

**Metrics to add**:
```go
connectionResets    metric.Int64Counter  // connection_resets_total
synTimeouts         metric.Int64Counter  // syn_timeouts_total
connectionRefused   metric.Int64Counter  // connection_refused_total
```

### Success Criteria - Stage 1
- [ ] Detect SYN timeouts (>3s)
- [ ] Detect connection refused (<100ms from SYN)
- [ ] Detect connection resets (rapid ESTABLISHED → CLOSE)
- [ ] 4 new test cases passing
- [ ] 3 new OTEL metrics exported
- [ ] Zero eBPF code changes (Go only)
- [ ] Coverage remains >80%

---

## Stage 2: DNS Consolidation

### Architecture Decision

**Approach**: Separate eBPF program + shared ring buffer

**Why separate program**:
1. DNS uses kprobes (unstable API) vs TCP uses tracepoints (stable)
2. Different event structures (DNS has query names, TCP has states)
3. Allows independent failure (DNS fails → TCP still works)
4. Cleaner separation of concerns

**Why shared ring buffer**:
1. Single reader goroutine in Go (simpler)
2. Unified event ordering
3. Less memory overhead

### DNS eBPF Design

**Kprobes to use**:
```c
// UDP DNS only (port 53)
SEC("kprobe/udp_sendmsg")       // Track DNS queries
SEC("kprobe/udp_recvmsg")       // Track DNS responses
```

**Event structure**:
```c
struct dns_event {
    __u64 timestamp;
    __u32 pid;
    __u16 query_id;           // DNS transaction ID
    __u16 query_type;         // A=1, AAAA=28, etc.
    __u32 latency_us;         // Query → Response latency
    __u8  response_code;      // 0=OK, 3=NXDOMAIN, 2=SERVFAIL
    __u8  event_type;         // 0=OK, 1=Slow, 2=Timeout, 3=Error
    __u8  query_name[253];    // Domain name (max DNS label)
    __u8  server_ip[16];      // DNS server (IPv4/IPv6)
    __u16 src_port;
    __u16 dst_port;           // 53
    __u8  comm[16];           // Process name
    __u8  family;             // AF_INET or AF_INET6
} __attribute__((packed));
```

**Maps needed**:
```c
// Track active queries: query_id → query_state
BPF_HASH(dns_queries, __u16, struct dns_query_state, 4096);

// Same ring buffer as TCP
BPF_RINGBUF(events, 256 * 1024);  // Shared with TCP events
```

### DNS Go Implementation

**New file**: `observer_dns.go`
```go
func (n *NetworkObserver) loadDNSeBPF() error {
    // Load DNS eBPF program
    // Attach kprobes: udp_sendmsg, udp_recvmsg
    // Port filtering in eBPF: only port 53
}

func (n *NetworkObserver) processDNSEvent(evt DNSEventBPF) {
    // Convert DNS event to domain.ObserverEvent
    // Detect problems: latency > 100ms, NXDOMAIN, SERVFAIL
    // Emit event with DNS-specific metadata
}
```

**Event type mapping**:
```go
const (
    EventTypeDNSQuery      = "dns.query"           // Query sent
    EventTypeDNSResponse   = "dns.response"        // Response OK
    EventTypeDNSSlow       = "dns.slow"            // >100ms
    EventTypeDNSTimeout    = "dns.timeout"         // >5s no response
    EventTypeDNSNXDomain   = "dns.nxdomain"        // Domain not found
    EventTypeDNSServFail   = "dns.servfail"        // Server failure
)
```

### DNS Testing Strategy

**Integration tests**:
1. `TestDNS_SuccessfulQuery` - nslookup google.com
2. `TestDNS_SlowQuery` - Query with artificial delay
3. `TestDNS_NXDomain` - Query non-existent domain
4. `TestDNS_Timeout` - Query to blackhole DNS server

**System tests** (Linux only):
```go
func TestDNS_RealQuery(t *testing.T) {
    if testing.Short() { t.Skip() }

    observer, _ := NewNetworkObserver("test-dns", cfg)
    observer.Start(ctx)

    // Real DNS query
    net.LookupHost("google.com")

    // Should see DNS events
    events := collectEvents(observer, 5*time.Second)
    assert.Contains(t, events, "dns.query")
    assert.Contains(t, events, "dns.response")
}
```

### Success Criteria - Stage 2
- [ ] Capture UDP port 53 queries/responses
- [ ] Calculate query-response latency
- [ ] Detect slow queries (>100ms)
- [ ] Detect timeouts (>5s)
- [ ] Detect NXDOMAIN responses
- [ ] Detect SERVFAIL responses
- [ ] Extract query name (first 253 bytes)
- [ ] 6 new test cases passing
- [ ] DNS metrics exported to OTEL
- [ ] Coverage >80% (including DNS code)
- [ ] DNS failures don't break TCP monitoring

---

## Stage 3: Status Consolidation (DEFERRED)

**Rationale for deferring**:
1. **Complexity**: HTTP parsing in eBPF is fragile
2. **Instability**: Requires kprobes on kernel functions that change
3. **Signal-to-noise**: Most K8s apps have their own metrics (Prometheus)
4. **Alternative**: OpenTelemetry instrumentation is better for L7

**If we do it later**:
- Use `kprobe/tcp_sendmsg` + `kprobe/tcp_recvmsg`
- Parse HTTP response line: `HTTP/1.1 200 OK`
- Extract status code (200, 404, 500, etc.)
- Emit status events for errors only (4xx, 5xx)

**Decision**: Skip Status consolidation for now. Focus on TCP + DNS.

---

## Unified Event Model

### Domain Event Structure

All network events flow through `domain.ObserverEvent`:

```go
type ObserverEvent struct {
    Type        string                 // "connection.established", "dns.query", etc.
    Timestamp   time.Time
    Source      string                 // Observer name: "network"
    Severity    Severity               // Info, Warning, Error
    Message     string                 // Human-readable
    Metadata    map[string]interface{} // ← VIOLATION! Need typed struct
}
```

**PROBLEM**: `map[string]interface{}` violates CLAUDE.md!

**Solution**: Typed network metadata:
```go
type NetworkMetadata struct {
    // Common fields (TCP + DNS)
    Protocol    string  // "tcp", "udp"
    SrcIP       string
    DstIP       string
    SrcPort     uint16
    DstPort     uint16
    PID         uint32
    ProcessName string

    // TCP-specific
    TCPOldState *string // "SYN_SENT", "ESTABLISHED", etc.
    TCPNewState *string

    // DNS-specific
    DNSQueryName    *string
    DNSQueryType    *string  // "A", "AAAA", etc.
    DNSResponseCode *string  // "OK", "NXDOMAIN", "SERVFAIL"
    DNSLatencyMs    *float64
    DNSServerIP     *string
}
```

---

## Architecture Diagrams

### Current State (Phase 1)
```
┌───────────────────────────────────────┐
│      Network Observer (Go)            │
├───────────────────────────────────────┤
│  ┌─────────────────────────────────┐  │
│  │   loadAndAttachStage()          │  │
│  │   - Load TCP eBPF               │  │
│  │   - Attach inet_sock_set_state  │  │
│  │   - Read ring buffer            │  │
│  └──────────────┬──────────────────┘  │
│                 │ events               │
│                 ▼                      │
│  ┌─────────────────────────────────┐  │
│  │   processEventsStage()          │  │
│  │   - Parse TCP events            │  │
│  │   - Convert to domain events    │  │
│  │   - Emit to OTEL                │  │
│  └─────────────────────────────────┘  │
└───────────────────────────────────────┘
                 │
                 ▼
         ┌──────────────┐
         │  Ring Buffer │ (256KB)
         └──────────────┘
                 ▲
      ┌──────────┴──────────┐
      │  eBPF (Kernel)      │
      │  inet_sock_set_state│
      │  (tracepoint)       │
      └─────────────────────┘
```

### Target State (After Stage 1 + 2)
```
┌────────────────────────────────────────────┐
│      Network Observer (Go)                 │
├────────────────────────────────────────────┤
│  ┌──────────────────────────────────────┐  │
│  │   loadAndAttachStage()               │  │
│  │   - Load TCP eBPF (tracepoint)       │  │
│  │   - Load DNS eBPF (kprobes)          │  │
│  │   - Read SHARED ring buffer          │  │
│  └────────────┬─────────────────────────┘  │
│               │ events (TCP + DNS)          │
│               ▼                             │
│  ┌──────────────────────────────────────┐  │
│  │   processEventsStage()               │  │
│  │   - Route by event type              │  │
│  │   - processTCPEvent()                │  │
│  │   - processDNSEvent()                │  │
│  │   - Convert to typed domain events   │  │
│  │   - Emit to OTEL                     │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
                 │
                 ▼
         ┌──────────────┐
         │  Ring Buffer │ (512KB - doubled)
         │   SHARED     │
         └──────────────┘
          ▲            ▲
    ┌─────┴─────┐  ┌───┴──────┐
    │ TCP eBPF  │  │ DNS eBPF │
    │inet_sock_ │  │udp_sendmsg│
    │set_state  │  │udp_recvmsg│
    │(tracepoint│  │(kprobes)  │
    └───────────┘  └───────────┘
```

---

## Implementation Roadmap

### Milestone 1: Stage 1 - Link Consolidation (1-2 days)
- [ ] **Day 1 Morning**: Enhance `stateToEventType()` with failure detection
- [ ] **Day 1 Afternoon**: Add 3 new OTEL metrics (resets, timeouts, refused)
- [ ] **Day 2 Morning**: Write 4 new integration tests
- [ ] **Day 2 Afternoon**: Update DESIGN.md, commit, PR

**Success**: Detect TCP connection failures without changing eBPF.

### Milestone 2: Stage 2 - DNS Consolidation (3-5 days)
- [ ] **Day 1**: Design DNS eBPF program (kprobes, maps, event struct)
- [ ] **Day 2**: Implement DNS eBPF C code, generate Go bindings
- [ ] **Day 3**: Integrate DNS into observer (loader, processor)
- [ ] **Day 4**: Write DNS tests (4 integration, 2 system)
- [ ] **Day 5**: Fix Metadata typing violation, update docs, PR

**Success**: Capture DNS queries on port 53 alongside TCP events.

### Milestone 3: Validation & Documentation (1 day)
- [ ] **Morning**: Run full test suite (TCP + DNS + Link)
- [ ] **Afternoon**: Update DESIGN.md with consolidation details
- [ ] **EOD**: Create Phase 4 completion announcement

**Success**: Unified network observer with TCP + DNS + Link monitoring.

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| DNS kprobes unstable across kernels | High | Test on kernel 5.10, 5.15, 6.x. Fall back gracefully. |
| Ring buffer overflow with 2 programs | Medium | Double ring buffer size (256KB → 512KB). Monitor drops. |
| DNS parsing complexity in eBPF | Medium | Keep simple: only extract query_id, name, rcode. No full parsing. |
| Performance regression | Low | Benchmark before/after. DNS sampling if needed. |
| Metadata typing violation | High | Fix NetworkMetadata struct BEFORE merging. |

---

## Deferred Features (Phase 5+)

**Not in this consolidation**:
1. **Status/L7 monitoring** - Too complex, defer to OpenTelemetry instrumentation
2. **DNS over HTTPS/TLS** - Requires SSL decryption (not in scope)
3. **IPv6 DNS** - Focus on IPv4 first (most common)
4. **CoreDNS special handling** - Generic DNS first, CoreDNS later
5. **K8s enrichment (Phase 3)** - Add pod/service names AFTER consolidation

---

## Definition of Done

**Phase 4 is complete when**:
- [x] Phase 1: TCP state monitoring (DONE)
- [ ] Stage 1: TCP failure detection (resets, timeouts, refused)
- [ ] Stage 2: DNS query/response monitoring (port 53 UDP)
- [ ] Stage 3: Status monitoring (DEFERRED)
- [ ] Unified event model with typed NetworkMetadata
- [ ] All tests passing (>80% coverage)
- [ ] DESIGN.md updated with consolidation details
- [ ] Zero map[string]interface{} violations
- [ ] Performance benchmarks show <5% regression

**Deliverable**: Single network observer handling TCP + DNS + Link failures.

---

## Next Steps

**Immediate action** (you decide):
1. **Start Stage 1** - Enhance TCP with failure detection (easiest win)
2. **Start Stage 2** - Add DNS monitoring (bigger feature)
3. **Design session** - Review this plan, refine approach

**Recommendation**: Start with Stage 1 (TCP failures). It's:
- Zero eBPF changes (Go only)
- Quick win (1-2 days)
- Validates consolidation approach
- Provides immediate value (detect SYN timeouts, RSTs)

What do you want to do?
