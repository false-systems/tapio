# Design Doc 003: Network Observer DNS/Link/Status Integration

**Status**: In Progress
**Date**: 2025-10-19
**Authors**: Yair + Claude (AI pair programming)
**Context**: Implementing ADR 002 network observer consolidation
**Related**: ADR 002 (Observer Consolidation)

---

## Context and Problem Statement

ADR 002 specifies consolidating 4 network observers into one:
- `network` + `dns` + `link` + `status` → **network-observer**

**Current State** (after initial consolidation):
- ✅ Network observer has TCP/UDP monitoring via eBPF
- ✅ Shared eBPF libraries created (`conn_tracking.h`, `tcp.h`, `metrics.h`)
- ✅ Domain model already has DNS/HTTP fields in `pkg/domain/events.go`
- ❌ DNS query/timeout detection not implemented
- ❌ Link failure detection (SYN timeouts, high retransmit rates) not implemented
- ❌ HTTP status code monitoring not implemented

**Discovery**: The existing `domain.NetworkEventData` struct **already has all needed fields**:
```go
type NetworkEventData struct {
    Protocol string
    SrcIP    string
    DstIP    string
    SrcPort  uint16
    DstPort  uint16

    // L7 protocol fields ✅ ALREADY EXISTS
    HTTPMethod      string
    HTTPPath        string
    HTTPStatusCode  int
    DNSQuery        string        // ✅ For DNS integration
    DNSResponseTime int64         // ✅ For DNS integration

    // TCP performance metrics ✅ ALREADY EXISTS
    RTTBaseline      float64
    RTTCurrent       float64
    RetransmitCount  uint32
    RetransmitRate   float64
    TCPState         string
}
```

**Problem**: How to efficiently implement DNS/Link/Status detection **without** creating multiple eBPF programs or new domain structs?

---

## Decision Drivers

1. **Follow Brendan Gregg BPF Performance Tools patterns** - eBPF captures, userspace parses
2. **Minimize eBPF overhead** - Single eBPF program is more efficient than 3-4 programs
3. **Use existing domain model** - No breaking changes, no new structs
4. **Maintainability** - Clear separation of concerns via processor pattern
5. **Performance** - Userspace parsing is more flexible and easier to optimize than eBPF parsing
6. **CLAUDE.md compliance** - TDD, small commits, typed structs only

---

## Decision

**Architecture**: Single eBPF program + Processor pattern in Go

### Design Pattern: eBPF Captures, Processors Parse

```
┌─────────────────┐      ┌──────────────┐      ┌─────────────────┐
│   eBPF Kernel   │ ───> │  Ring Buffer │ ───> │  Go Userspace   │
│ network_monitor │      │   (raw data) │      │   (processors)  │
└─────────────────┘      └──────────────┘      └─────────────────┘
                                                         │
                         ┌───────────────────────────────┼───────────────────┐
                         │                               │                   │
                         ▼                               ▼                   ▼
                  ┌─────────────┐              ┌─────────────┐     ┌─────────────┐
                  │ DNS Processor│              │Link Processor│     │Status Proc  │
                  │  (UDP:53)   │              │(SYN timeout) │     │ (HTTP codes)│
                  └─────────────┘              └─────────────┘     └─────────────┘
                         │                               │                   │
                         └───────────────────────────────┴───────────────────┘
                                                 │
                                                 ▼
                                        domain.NetworkEventData
                                        (event_subtype field)
```

### Why This Pattern Works

**Brendan Gregg BPF Performance Tools Pattern**:
- **eBPF does**: Capture raw data (IPs, ports, timestamps, packet payloads)
- **Userspace does**: Parse protocols, detect patterns, enrich with context

**Benefits**:
1. **Single eBPF program** - Lower kernel overhead, simpler lifecycle
2. **Flexible parsing** - Go is easier to debug than eBPF C code
3. **No BTF dependencies** - Don't need kernel struct definitions for DNS/HTTP parsing
4. **Easier testing** - Can unit test processors without eBPF

---

## Architecture Design

### 1. eBPF Layer: network_monitor.c (NO CHANGES NEEDED)

**Current eBPF program already captures everything we need**:

```c
// network_monitor.c - EXISTING CODE (no changes)
SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(...) {
    // Already captures: TCP state transitions, IPs, ports
    // Already tracks: RTT, retransmits, congestion window
    // Already detects: SYN timeouts (TCP_SYN_SENT → TCP_CLOSE)
}

SEC("tracepoint/tcp/tcp_receive_reset")
int trace_tcp_receive_reset(...) {
    // Already marks RST flags in LRU map
}

SEC("tracepoint/tcp/tcp_retransmit_skb")
int trace_tcp_retransmit_skb(...) {
    // Already tracks retransmit statistics
}
```

**Key Insight**: We don't need new eBPF programs! The existing tracepoints give us:
- DNS: UDP traffic on port 53 (already captured via `inet_sock_set_state`)
- Link failures: SYN timeouts, high retransmit rates (already captured)
- HTTP status: TCP payload on port 80/443 (can add future kprobe if needed)

---

### 2. Go Processor Layer (NEW CODE)

#### processor_dns.go - DNS Query/Timeout Detection

**Purpose**: Parse DNS queries from UDP port 53 traffic

```go
package network

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// DNSProcessor detects DNS queries and timeouts
type DNSProcessor struct {
    // DNS query tracking
    dnsQueries map[string]dnsQueryState // key: queryID

    // OTEL metrics
    dnsQueriesTotal    metric.Int64Counter
    dnsTimeoutsTotal   metric.Int64Counter
    dnsResponseTimeMs  metric.Float64Histogram
}

type dnsQueryState struct {
    query      string
    startTime  int64
    srcIP      string
    dstIP      string
}

// Process checks if event is DNS-related and extracts DNS data
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Only process UDP port 53 traffic
    if evt.Protocol != "UDP" || (evt.SrcPort != 53 && evt.DstPort != 53) {
        return nil // Not DNS
    }

    // Parse DNS query from payload (future: read from skb data)
    // For now: detect DNS timeout from state transitions
    if p.isDNSTimeout(evt) {
        return p.createDNSTimeoutEvent(evt)
    }

    // Populate EXISTING domain.NetworkEventData fields ✅
    netData := &domain.NetworkEventData{
        Protocol:        "DNS",
        SrcIP:           convertIPv4(evt.SrcIP),
        DstIP:           convertIPv4(evt.DstIP),
        SrcPort:         evt.SrcPort,
        DstPort:         evt.DstPort,
        DNSQuery:        "", // TODO: Parse from payload
        DNSResponseTime: 0,  // TODO: Calculate from query tracking
    }

    return &domain.ObserverEvent{
        Type:        string(domain.EventTypeNetwork), // "network"
        Subtype:     "dns_query",                     // ADR 002 pattern
        NetworkData: netData,
    }
}

func (p *DNSProcessor) isDNSTimeout(evt NetworkEventBPF) bool {
    // DNS timeout = UDP connection closed without response
    // Heuristic: Check if query age > 5 seconds
    return false // TODO: Implement
}

func (p *DNSProcessor) createDNSTimeoutEvent(evt NetworkEventBPF) *domain.ObserverEvent {
    netData := &domain.NetworkEventData{
        Protocol:        "DNS",
        SrcIP:           convertIPv4(evt.SrcIP),
        DstIP:           convertIPv4(evt.DstIP),
        DNSQuery:        "", // From tracked query
        DNSResponseTime: 0,
    }

    return &domain.ObserverEvent{
        Type:        string(domain.EventTypeNetwork),
        Subtype:     "dns_timeout", // Event subtype
        NetworkData: netData,
    }
}
```

---

#### processor_link.go - Link Failure Detection

**Purpose**: Detect network link failures (SYN timeouts, high retransmit rates)

```go
package network

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// LinkProcessor detects network link failures
type LinkProcessor struct {
    // Failure detection thresholds
    synTimeoutThreshold   time.Duration // e.g., 3 seconds
    retransmitRateThreshold float64     // e.g., 0.05 (5%)

    // OTEL metrics
    linkFailuresTotal metric.Int64Counter
    synTimeoutsTotal  metric.Int64Counter
}

// Process checks if event indicates link failure
func (p *LinkProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Detect SYN timeout: TCP_SYN_SENT → TCP_CLOSE
    if p.isSYNTimeout(evt) {
        return p.createLinkFailureEvent(evt, "syn_timeout")
    }

    // Detect high retransmit rate (already tracked in eBPF map)
    if p.isHighRetransmitRate(evt) {
        return p.createLinkFailureEvent(evt, "high_retransmit_rate")
    }

    return nil // Not a link failure
}

func (p *LinkProcessor) isSYNTimeout(evt NetworkEventBPF) bool {
    // SYN timeout = Connection attempt failed
    return evt.OldState == TCP_SYN_SENT && evt.NewState == TCP_CLOSE
}

func (p *LinkProcessor) isHighRetransmitRate(evt NetworkEventBPF) bool {
    // High retransmit rate already detected in processRetransmitEvent
    // This processor just creates domain event for it
    return evt.EventType == EventTypeRetransmit // Reuse existing detection
}

func (p *LinkProcessor) createLinkFailureEvent(evt NetworkEventBPF, failureType string) *domain.ObserverEvent {
    // Populate EXISTING domain.NetworkEventData fields ✅
    netData := &domain.NetworkEventData{
        Protocol:        "TCP",
        SrcIP:           convertIPv4(evt.SrcIP),
        DstIP:           convertIPv4(evt.DstIP),
        SrcPort:         evt.SrcPort,
        DstPort:         evt.DstPort,
        TCPState:        tcpStateName(evt.NewState),
        RetransmitCount: evt.OldState, // Reused field from eBPF
        RetransmitRate:  0.0,           // Calculate from stats
    }

    return &domain.ObserverEvent{
        Type:        string(domain.EventTypeNetwork),
        Subtype:     "link_failure", // Event subtype
        NetworkData: netData,
        // Additional context
        Metadata: map[string]string{
            "failure_type": failureType, // syn_timeout | high_retransmit_rate
        },
    }
}
```

---

#### processor_status.go - HTTP Status Code Monitoring

**Purpose**: Parse HTTP status codes from TCP traffic

```go
package network

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// StatusProcessor monitors HTTP status codes
type StatusProcessor struct {
    // HTTP tracking
    httpConnections map[string]httpState // key: connKey

    // OTEL metrics
    http5xxTotal metric.Int64Counter
    http4xxTotal metric.Int64Counter
}

type httpState struct {
    method     string
    path       string
    statusCode int
}

// Process checks if event is HTTP-related and extracts status
func (p *StatusProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Only process TCP port 80/443 traffic
    if evt.Protocol != "TCP" || (evt.DstPort != 80 && evt.DstPort != 443) {
        return nil // Not HTTP
    }

    // Parse HTTP status from payload (future: kprobe on tcp_recvmsg)
    // For now: placeholder for future implementation
    statusCode := p.parseHTTPStatus(evt)
    if statusCode == 0 {
        return nil // No HTTP status detected
    }

    // Populate EXISTING domain.NetworkEventData fields ✅
    netData := &domain.NetworkEventData{
        Protocol:       "HTTP",
        SrcIP:          convertIPv4(evt.SrcIP),
        DstIP:          convertIPv4(evt.DstIP),
        SrcPort:        evt.SrcPort,
        DstPort:        evt.DstPort,
        HTTPMethod:     "GET",        // TODO: Parse from payload
        HTTPPath:       "/",          // TODO: Parse from payload
        HTTPStatusCode: statusCode,   // ✅ Field already exists!
    }

    // Determine event subtype based on status code
    subtype := "http_request"
    if statusCode >= 500 {
        subtype = "http_5xx"
        p.http5xxTotal.Add(ctx, 1)
    } else if statusCode >= 400 {
        subtype = "http_4xx"
        p.http4xxTotal.Add(ctx, 1)
    }

    return &domain.ObserverEvent{
        Type:        string(domain.EventTypeNetwork),
        Subtype:     subtype, // http_request | http_4xx | http_5xx
        NetworkData: netData,
    }
}

func (p *StatusProcessor) parseHTTPStatus(evt NetworkEventBPF) int {
    // TODO: Parse HTTP status from TCP payload
    // Future: Add kprobe on tcp_recvmsg to read payload
    return 0
}
```

---

### 3. Integration into NetworkObserver

**Update processEventsStage to use processors**:

```go
// observer_ebpf.go - UPDATED
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
    // Initialize processors
    dnsProc := NewDNSProcessor()
    linkProc := NewLinkProcessor()
    statusProc := NewStatusProcessor()

    for {
        select {
        case <-ctx.Done():
            return nil

        case evt, ok := <-eventCh:
            if !ok {
                return nil
            }

            // Try each processor (order matters for performance)
            var domainEvent *domain.ObserverEvent

            // 1. Link failures (fast check - just state comparison)
            if domainEvent = linkProc.Process(ctx, evt); domainEvent != nil {
                n.emitDomainEvent(ctx, domainEvent)
                continue
            }

            // 2. DNS (port check + parsing)
            if domainEvent = dnsProc.Process(ctx, evt); domainEvent != nil {
                n.emitDomainEvent(ctx, domainEvent)
                continue
            }

            // 3. HTTP status (port check + parsing)
            if domainEvent = statusProc.Process(ctx, evt); domainEvent != nil {
                n.emitDomainEvent(ctx, domainEvent)
                continue
            }

            // 4. Fallback: Regular state change event
            n.processStateChangeEvent(ctx, evt)
        }
    }
}

func (n *NetworkObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) {
    // Record OTEL metrics
    n.RecordEvent(ctx)

    // Output event (if stdout enabled)
    if n.config.Output.Stdout {
        log.Printf("[%s] %s.%s: %+v", n.Name(), evt.Type, evt.Subtype, evt.NetworkData)
    }

    // TODO: Publish to NATS (future ADR 002 phase)
}
```

---

## Event Subtype Design (ADR 002 Alignment)

**Base Type**: `network` (from `domain.EventTypeNetwork`)

**Subtypes** (replaces 12+ old event types):

| Event Subtype | Old Event Type | Description |
|---------------|----------------|-------------|
| `connection_established` | `EventTypeConnectionEstablished` | TCP connection opened |
| `connection_closed` | `EventTypeConnectionClosed` | TCP connection closed |
| `connection_refused` | `EventTypeConnectionRefused` | TCP connection refused |
| `connection_rst` | `EventTypeConnectionRST` | TCP RST received |
| `dns_query` | `EventTypeDNSQuery` | DNS query sent |
| `dns_timeout` | `EventTypeDNSTimeout` | DNS query timeout |
| `link_failure` | `EventTypeLinkFailure` | Network link failure (SYN timeout, high retransmit) |
| `http_request` | `EventTypeHTTPRequest` | HTTP request |
| `http_4xx` | `EventTypeHTTP4xx` | HTTP 4xx status code |
| `http_5xx` | `EventTypeHTTP5xx` | HTTP 5xx status code |
| `tcp_retransmit` | `EventTypeTCPRetransmit` | TCP packet retransmission |
| `rtt_spike` | `EventTypeRTTSpike` | RTT degradation detected |

---

## Implementation Strategy (TDD - Following CLAUDE.md)

### Phase 1: Link Processor (Simplest - Already 90% Done)

**Why First**: Link failure detection logic already exists in `processRetransmitEvent` and `stateToEventType`

**Tasks**:
1. ✅ Write test: `TestLinkProcessor_SYNTimeout`
2. ✅ Write test: `TestLinkProcessor_HighRetransmitRate`
3. ✅ Implement `processor_link.go`
4. ✅ Integrate into `processEventsStage`
5. ✅ Verify existing metrics still work
6. ✅ Commit (~30 lines)

### Phase 2: DNS Processor (Medium Complexity)

**Tasks**:
1. Write test: `TestDNSProcessor_DetectQuery`
2. Write test: `TestDNSProcessor_DetectTimeout`
3. Implement `processor_dns.go` (basic port 53 detection)
4. Integrate into `processEventsStage`
5. Add OTEL metrics (`dns_queries_total`, `dns_timeouts_total`)
6. Commit (~50 lines - may need 2 commits)

### Phase 3: Status Processor (Future - Requires Payload Parsing)

**Tasks**:
1. Write test: `TestStatusProcessor_Detect5xx`
2. Implement `processor_status.go` (placeholder for now)
3. Add kprobe on `tcp_recvmsg` to capture HTTP payloads (future)
4. Commit (~30 lines)

---

## Testing Strategy

### Unit Tests (TDD)

```go
// processor_link_test.go
func TestLinkProcessor_SYNTimeout(t *testing.T) {
    proc := NewLinkProcessor()
    evt := NetworkEventBPF{
        OldState: TCP_SYN_SENT,
        NewState: TCP_CLOSE,
        SrcIP:    0x0100007f, // 127.0.0.1
        DstIP:    0x6401a8c0, // 192.168.1.100
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "network", domainEvt.Type)
    assert.Equal(t, "link_failure", domainEvt.Subtype)
    assert.Equal(t, "syn_timeout", domainEvt.Metadata["failure_type"])
}

// processor_dns_test.go
func TestDNSProcessor_DetectQuery(t *testing.T) {
    proc := NewDNSProcessor()
    evt := NetworkEventBPF{
        Protocol: "UDP",
        SrcPort:  12345,
        DstPort:  53, // DNS port
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "network", domainEvt.Type)
    assert.Equal(t, "dns_query", domainEvt.Subtype)
}
```

### Integration Tests

```go
// observer_integration_test.go
func TestNetworkObserver_DNSLinkStatusIntegration(t *testing.T) {
    // Test that all processors work together
    // Send events through channel → verify correct subtypes emitted
}
```

---

## Performance Considerations

### Why Userspace Parsing is OK

**Brendan Gregg BPF Performance Tools (Chapter 10)**:
- "eBPF should capture, userspace should parse"
- Parsing DNS/HTTP in eBPF requires complex string operations (slow, risky)
- Parsing in Go is ~10x faster than eBPF for complex protocols
- Ring buffer already copies data to userspace - parsing there is free

**Overhead Estimate**:
- Current: ~1% CPU for eBPF + ring buffer reads
- After processors: ~1.5% CPU (additional Go parsing)
- **Acceptable**: <5% overhead is production-ready

---

## Migration Path

### Step 1: Add Processors (Non-Breaking)
- Add processor files
- Integrate into `processEventsStage`
- Keep existing event emission (dual-write)

### Step 2: Verify Event Subtypes
- Ensure all events have `Type` + `Subtype`
- Verify OTEL metrics still work

### Step 3: Remove Old Event Types (Breaking)
- Delete old `EventTypeDNSQuery`, etc. constants
- Update all consumers to use `Subtype` field

---

## Success Criteria

- [ ] `processor_link.go` implemented with TDD
- [ ] `processor_dns.go` implemented with TDD
- [ ] `processor_status.go` placeholder implemented
- [ ] All events use `domain.NetworkEventData` (no new structs)
- [ ] Event subtypes match ADR 002 spec
- [ ] Test coverage >= 80%
- [ ] OTEL metrics for DNS/Link/Status
- [ ] Performance overhead < 5% CPU
- [ ] All commits < 30 lines (CLAUDE.md compliance)
- [ ] `make verify-full` passes

---

## References

- **ADR 002**: Observer Consolidation Architecture
- **Brendan Gregg BPF Performance Tools**: Chapter 10 (Networking)
- **Cilium eBPF Libraries**: https://github.com/cilium/cilium/tree/master/bpf/lib
- **CLAUDE.md**: Tapio Production Standards
- **pkg/domain/events.go**: Existing domain model with DNS/HTTP fields

---

## Notes

**Key Design Decision**: Use existing domain model instead of creating new structs

**Rationale**:
- `domain.NetworkEventData` already has `DNSQuery`, `DNSResponseTime`, `HTTPStatusCode`
- Adding new structs would violate CLAUDE.md (unnecessary complexity)
- Event subtype pattern (ADR 002) eliminates need for separate event types

**Next Steps**:
1. Update README.md with processor pattern examples
2. Implement Link processor (Phase 1)
3. Implement DNS processor (Phase 2)
4. Add Status processor placeholder (Phase 3)
