# DNS Observer eBPF Design Session

## Problem Statement
We need to detect DNS problems in real-time by intercepting DNS queries and responses at the kernel level using eBPF. Must support:
- Standard DNS (UDP port 53)
- DNS over TCP (port 53)
- CoreDNS (Kubernetes DNS, typically port 53 or 9153)
- Both IPv4 and IPv6

## What's the Simplest Solution?
Hook into both UDP and TCP send/receive syscalls, track query/response pairs across protocols, measure latency, and detect problems (timeouts, NXDOMAIN, SERVFAIL, slow queries). Special handling for CoreDNS metrics and Kubernetes service discovery patterns.

## Component Breakdown

### 1. eBPF Programs (Kernel Space)
```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│UDP/TCP Send │ ───> │Parse DNS Hdr│ ───> │Track Query  │
│   Hooks     │      │ Extract ID  │      │  in Map     │
└─────────────┘      └─────────────┘      └─────────────┘
       ↓                                          ↓
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│UDP/TCP Recv │ ───> │Match Response│───> │  Calculate  │
│   Hooks     │      │  to Query   │      │   Latency   │
└─────────────┘      └─────────────┘      └─────────────┘
       ↓                                          ↓
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  CoreDNS    │ ───> │Check Problem│ ───> │Send to User │
│  Detection  │      │   Types     │      │   Space     │
└─────────────┘      └─────────────┘      └─────────────┘
```

### 2. Data Structures

#### DNS Query State (Kernel)
```c
struct dns_query_state {
    u64 timestamp_ns;      // When query was sent
    u32 pid;              // Process ID
    u32 tid;              // Thread ID
    u16 query_id;        // DNS transaction ID
    u16 query_type;      // A, AAAA, MX, SRV, etc.
    u8 query_name[253];  // Domain name
    u8 server_ip[16];    // DNS server IP
    u16 src_port;        // Source port
    u16 dst_port;        // Destination port (53/9153)
    u8 protocol;         // 0=UDP, 1=TCP
    u8 is_coredns;       // CoreDNS detection flag
    u32 tcp_seq;         // TCP sequence for correlation
    u8 k8s_namespace[64]; // Kubernetes namespace if detected
};
```

#### DNS Event (Kernel → User)
```c
struct dns_event {
    u64 timestamp_ns;
    u64 latency_ns;
    u32 pid;
    u32 tid;
    u16 query_id;
    u16 query_type;
    u8 problem_type;     // 0=OK, 1=Slow, 2=NXDOMAIN, etc.
    u8 response_code;    // DNS RCODE
    u8 query_name[253];
    u8 server_ip[16];
    u8 comm[16];        // Process name
    u8 protocol;        // UDP/TCP
    u8 is_coredns;      // CoreDNS flag
    u16 tcp_flags;      // TCP state if applicable
    u8 k8s_service[128]; // Kubernetes service name if detected
    u8 k8s_namespace[64]; // Kubernetes namespace
    u32 coredns_cache_hit; // CoreDNS cache metrics
};
```

### 3. eBPF Maps

```c
// Track active queries: (pid, query_id, protocol) → query_state
BPF_HASH(active_queries, struct query_key, struct dns_query_state, 20480);

// TCP connection tracking for DNS over TCP
BPF_HASH(tcp_dns_sessions, struct sock *, struct tcp_dns_state, 4096);

// CoreDNS process detection
BPF_HASH(coredns_pids, u32, u8, 128);

// Ring buffer for events to userspace
BPF_RINGBUF_OUTPUT(dns_events, 16384);

// Configuration from userspace
BPF_ARRAY(config, u32, 8);
// [0]=slow_threshold_ns, [1]=timeout_ns, [2]=coredns_port
// [3]=enable_tcp, [4]=enable_k8s_enrichment

// Per-query type statistics
BPF_PERCPU_ARRAY(query_stats, struct dns_stats, 256);
```

## CoreDNS Specific Features

### Detection Patterns
```go
// CoreDNS detection heuristics
type CoreDNSDetector struct {
    // Process name patterns
    ProcessPatterns []string // ["coredns", "kube-dns"]

    // Network patterns
    ListenPorts []uint16 // [53, 9153, 8080(metrics)]

    // Kubernetes service discovery patterns
    K8sPatterns []string // [".cluster.local", ".svc", ".pod"]
}
```

### CoreDNS Metrics Integration
```go
// Parse CoreDNS Prometheus metrics
type CoreDNSMetrics struct {
    CacheHits   uint64
    CacheMisses uint64
    Forwarded   uint64
    PluginLatency map[string]time.Duration
}
```

## TCP DNS Support

### TCP State Machine
```
┌──────────┐     SYN      ┌──────────┐     Query    ┌──────────┐
│   INIT   │──────────────>│CONNECTED │──────────────>│ WAITING  │
└──────────┘               └──────────┘               └──────────┘
                                ↑                           │
                                │                           │Response
                                │                           ↓
┌──────────┐     FIN      ┌──────────┐              ┌──────────┐
│  CLOSED  │<──────────────│   IDLE   │<──────────────│ COMPLETE │
└──────────┘               └──────────┘               └──────────┘
```

### TCP DNS Packet Structure
```c
struct tcp_dns_packet {
    u16 length;          // TCP DNS has 2-byte length prefix
    struct dns_header {
        u16 id;
        u16 flags;
        u16 qdcount;
        u16 ancount;
        u16 nscount;
        u16 arcount;
    } header;
    u8 data[];          // Variable length query/response
};
```

## Interfaces

```go
// DNSeBPF manages the eBPF DNS monitoring
type DNSeBPF interface {
    Load() error
    Attach() error
    AttachTCP() error  // TCP-specific hooks
    DetachAll() error
    ReadEvents(ctx context.Context) (<-chan *DNSEvent, error)
    UpdateConfig(config *DNSConfig) error
    GetStats() (*DNSStats, error)
    SetCoreDNSPIDs(pids []uint32) error
    Close() error
}

// CoreDNSEnricher adds Kubernetes context
type CoreDNSEnricher interface {
    EnrichWithK8sContext(event *DNSEvent) error
    DetectServiceDiscovery(queryName string) (*K8sService, error)
    GetNamespaceFromQuery(queryName string) string
    IsCoreDNSQuery(event *DNSEvent) bool
}

// TCPDNSTracker manages TCP DNS sessions
type TCPDNSTracker interface {
    TrackTCPSession(conn *TCPConnection) error
    MatchTCPResponse(seq uint32, response []byte) (*DNSQuery, error)
    CleanupStale() error
    GetActiveSessions() int
}
```

## Failure Modes & Handling

| Failure Mode | Detection | Recovery Strategy |
|-------------|-----------|-------------------|
| eBPF load fails | Error on Load() | Fall back to mock mode |
| Verifier rejects program | VerifierError | Simplify program, reduce complexity |
| Permission denied | EPERM | Check CAP_BPF/CAP_SYS_ADMIN |
| Map full | ENOSPC on update | LRU eviction, increase map size |
| Ring buffer full | Dropped events counter | Increase buffer, add backpressure |
| DNS packet malformed | Invalid header check | Skip packet, increment error metric |
| TCP fragmentation | Incomplete packet | Buffer and reassemble |
| TCP out-of-order | Sequence mismatch | Reorder buffer with timeout |
| CoreDNS not detected | No process match | Manual PID configuration |
| CoreDNS restarts | PID changes | Periodic PID refresh |
| K8s API unavailable | Connection refused | Cache last known state |
| Query without response | Timeout checker | Generate timeout event |
| Response without query | No matching state | Track orphan responses |

## Test Plan (TDD)

### Phase 1: Unit Tests (No eBPF)
```go
// DNS packet parsing (UDP & TCP)
func TestParseDNSQueryUDP(t *testing.T)
func TestParseDNSQueryTCP(t *testing.T)
func TestParseDNSResponseWithMultipleAnswers(t *testing.T)
func TestParseTruncatedResponse(t *testing.T)
func TestParseMalformedPacket(t *testing.T)

// TCP DNS specific
func TestTCPDNSLengthPrefix(t *testing.T)
func TestTCPFragmentation(t *testing.T)
func TestTCPSessionTracking(t *testing.T)
func TestTCPConnectionReuse(t *testing.T)

// CoreDNS detection
func TestDetectCoreDNSProcess(t *testing.T)
func TestParseCoreDNSMetrics(t *testing.T)
func TestDetectK8sServiceQuery(t *testing.T)
func TestExtractK8sNamespace(t *testing.T)

// Problem detection
func TestDetectSlowQuery(t *testing.T)
func TestDetectTimeout(t *testing.T)
func TestDetectNXDOMAIN(t *testing.T)
func TestDetectSERVFAIL(t *testing.T)
func TestDetectTruncation(t *testing.T)
func TestTrackRepeatedProblems(t *testing.T)
```

### Phase 2: Integration Tests (Mock eBPF)
```go
// eBPF lifecycle
func TestLoadeBPFProgram(t *testing.T)
func TestAttachUDPHooks(t *testing.T)
func TestAttachTCPHooks(t *testing.T)
func TestDetachAllHooks(t *testing.T)

// Event flow
func TestUDPQueryResponseFlow(t *testing.T)
func TestTCPQueryResponseFlow(t *testing.T)
func TestMixedProtocolQueries(t *testing.T)
func TestCoreDNSEventEnrichment(t *testing.T)

// High volume
func TestHighVolumeUDP(t *testing.T)
func TestHighVolumeTCP(t *testing.T)
func TestTCPConnectionPool(t *testing.T)
```

### Phase 3: System Tests (Real eBPF - Linux only)
```go
//go:build linux

// Real capture tests
func TestRealUDPDNSCapture(t *testing.T)
func TestRealTCPDNSCapture(t *testing.T)
func TestRealCoreDNSCapture(t *testing.T)

// Container tests
func TestContainerDNS(t *testing.T)
func TestKubernetesPodDNS(t *testing.T)
func TestCoreDNSInKubernetes(t *testing.T)

// Protocol tests
func TestIPv6DNS(t *testing.T)
func TestDNSSEC(t *testing.T)
func TestEDNS0(t *testing.T)

// CoreDNS specific
func TestCoreDNSPluginChain(t *testing.T)
func TestCoreDNSCache(t *testing.T)
func TestCoreDNSForwarding(t *testing.T)
```

### Phase 4: Performance Tests
```go
func BenchmarkUDPEventProcessing(b *testing.B)
func BenchmarkTCPEventProcessing(b *testing.B)
func BenchmarkCoreDNSEnrichment(b *testing.B)
func BenchmarkMapOperations(b *testing.B)
func TestMemoryLeaks(t *testing.T)
func TestCPUOverhead(t *testing.T)
func TestTCPStateMemory(t *testing.T)
```

### Phase 5: Negative Tests
```go
// Error conditions
func TestInvalidDNSPacket(t *testing.T)
func TestTCPConnectionAbort(t *testing.T)
func TestCoreDNSCrash(t *testing.T)
func TestMapOverflow(t *testing.T)
func TestRingBufferFull(t *testing.T)

// Attack scenarios
func TestDNSAmplification(t *testing.T)
func TestDNSTunneling(t *testing.T)
func TestCachePoisoning(t *testing.T)
```

## Implementation Steps (Following TDD)

### Step 1: Write Core Tests First
1. DNS packet parser tests (UDP & TCP)
2. CoreDNS detection tests
3. Problem detection tests
4. K8s enrichment tests

### Step 2: Minimal Implementation (No eBPF)
1. DNS packet structures
2. Basic parsing for UDP/TCP
3. CoreDNS detection logic
4. Make all tests pass

### Step 3: Add eBPF Layer Tests
1. Mock eBPF loader tests
2. Map operation tests
3. Event pipeline tests

### Step 4: Implement eBPF
1. Write C code for hooks
2. UDP send/recv hooks
3. TCP send/recv hooks
4. Connect to ring buffer

### Step 5: CoreDNS Integration
1. Process detection
2. Metrics scraping
3. K8s context enrichment

### Step 6: Production Hardening
1. Performance optimization
2. Memory leak fixes
3. Error recovery
4. Monitoring metrics

## Success Criteria
- [ ] All unit tests pass (100% of tests)
- [ ] 80%+ code coverage
- [ ] No memory leaks (valgrind clean)
- [ ] <1% CPU overhead
- [ ] <20MB memory usage (including TCP state)
- [ ] Handles 50K queries/sec (mixed UDP/TCP)
- [ ] Detects all DNS problem types
- [ ] Correctly identifies CoreDNS queries
- [ ] Enriches K8s service discovery
- [ ] Zero false positives on healthy DNS
- [ ] TCP DNS fully supported
- [ ] CoreDNS cache metrics captured

## Code Structure
```
internal/observers/dns/
├── DESIGN.md              (this file)
├── dns_packet.go          (UDP/TCP packet structures)
├── dns_packet_test.go
├── dns_tcp.go            (TCP DNS specific logic)
├── dns_tcp_test.go
├── dns_detector.go       (problem detection logic)
├── dns_detector_test.go
├── coredns.go           (CoreDNS specific features)
├── coredns_test.go
├── k8s_enricher.go      (Kubernetes context)
├── k8s_enricher_test.go
├── ebpf_loader.go       (eBPF program management)
├── ebpf_loader_test.go
├── bpf_src/
│   ├── dns_common.h     (shared structures)
│   ├── dns_udp.c        (UDP hooks)
│   ├── dns_tcp.c        (TCP hooks)
│   ├── dns_maps.h       (BPF maps)
│   └── coredns.c        (CoreDNS detection)
└── testdata/
    ├── dns_udp_query.pcap
    ├── dns_tcp_session.pcap
    ├── coredns_trace.json
    └── k8s_dns_queries.pcap
```

## CoreDNS Query Patterns

### Kubernetes Service Discovery
```
# Standard Kubernetes DNS patterns
<service>.<namespace>.svc.cluster.local
<pod-ip>.<namespace>.pod.cluster.local
<service>.<namespace>.svc
_<port>._<proto>.<service>.<namespace>.svc.cluster.local

# CoreDNS specific
cluster.local:53
kube-system.svc.cluster.local:53
```

### CoreDNS Plugin Chain Events
```
Query Flow through CoreDNS plugins:
1. errors       → log errors
2. health       → health endpoint
3. kubernetes   → service discovery
4. prometheus   → metrics
5. forward      → upstream DNS
6. cache        → caching layer
7. loop         → loop detection
8. reload       → config reload
9. loadbalance  → upstream LB
```

## TCP DNS Considerations

### When TCP is Used
1. Response > 512 bytes (traditional)
2. Response > EDNS0 size
3. Truncation flag set
4. Zone transfers (AXFR/IXFR)
5. Client preference
6. DNS over TLS/HTTPS base

### TCP Challenges
1. Connection pooling
2. Keep-alive handling
3. Concurrent queries per connection
4. Head-of-line blocking
5. Connection state tracking
6. Memory overhead per connection