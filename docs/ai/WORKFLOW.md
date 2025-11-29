# AI Agent Workflow Guide

> **Purpose**: How AI coding assistants should work with the TAPIO codebase using TDD

## 🔄 Explicit Agent Workflow

When human says: **"Add DNS query detection to network observer"**

### Step 1: UNDERSTAND (Read First, Code Never)

```bash
# Agent reads these files BEFORE proposing solution:
- internal/observers/network/observer.go (understand current architecture)
- internal/observers/network/processor_link.go (reference processor)
- pkg/domain/events.go (understand ObserverEvent model)
- docs/003-network-observer-dns-link-status-integration.md (design doc)
```

**Agent then ASKS clarifying questions:**
```
"I understand we need DNS query detection in the network observer.
Questions before implementing:

1. Event detection:
   - Should we match UDP port 53 only?
   - Do we need to parse DNS query names from packets?
   - Or just detect DNS traffic by port?

2. Event type:
   - Use domain.EventTypeNetwork with subtype "dns_query"?
   - Or create new domain.EventTypeDNS?

3. IPv4 + IPv6 support:
   - Handle both address families (MANDATORY)?
   - Test cases for ::1 (IPv6 localhost)?

Once clarified, I'll write the failing test."
```

**Agent waits for human approval before coding.**

###Step 2: RED Phase (Failing Test)

```go
// Step 1: Write test that FAILS (RED)
func TestDNSProcessor_DetectQuery(t *testing.T) {
    proc := NewDNSProcessor()  // ❌ Undefined - test fails

    evt := NetworkEventBPF{
        Protocol: IPPROTO_UDP,
        DstPort:  53,  // DNS port
        Family:   AF_INET,
        SrcIP:    0x0100007f,  // 127.0.0.1
        DstIP:    0x0108080808,  // 8.8.8.8
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "dns_query", domainEvt.Subtype)
}
```

**Agent runs and shows output:**
```bash
$ go test ./internal/observers/network/
# internal/observers/network
./processor_dns_test.go:10:13: undefined: NewDNSProcessor
FAIL [build failed]

✅ RED phase confirmed - test doesn't compile
```

### Step 3: GREEN Phase (Minimal Code)

```go
// Minimal implementation to pass
type DNSProcessor struct {}

func NewDNSProcessor() *DNSProcessor {
    return &DNSProcessor{}
}

func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Only process UDP traffic on port 53
    if evt.Protocol != IPPROTO_UDP || (evt.DstPort != 53 && evt.SrcPort != 53) {
        return nil
    }

    return &domain.ObserverEvent{
        Type:    string(domain.EventTypeNetwork),
        Subtype: "dns_query",
        NetworkData: &domain.NetworkEventData{
            Protocol: "DNS",
            SrcIP:    convertIPv4(evt.SrcIP),  // Will add IPv6 in refactor
            DstIP:    convertIPv4(evt.DstIP),
            SrcPort:  evt.SrcPort,
            DstPort:  evt.DstPort,
        },
    }
}
```

**Agent verifies:**
```bash
$ go test ./internal/observers/network/ -v -run TestDNSProcessor
=== RUN   TestDNSProcessor_DetectQuery
--- PASS: TestDNSProcessor_DetectQuery (0.00s)
PASS

✅ GREEN phase confirmed - test passes
```

### Step 4: REFACTOR Phase

Add IPv6 support, edge cases, improve design:
```go
// Add IPv6 test (MANDATORY!)
func TestDNSProcessor_DetectQuery_IPv6(t *testing.T) {
    proc := NewDNSProcessor()

    evt := NetworkEventBPF{
        Protocol: IPPROTO_UDP,
        DstPort:  53,
        Family:   AF_INET6,
        SrcIPv6:  [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},  // ::1
        DstIPv6:  [16]byte{0x26, 0x06, 0x47, 0x00, ...},  // 2606:4700::...
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "::1", domainEvt.NetworkData.SrcIP)
}

// Refactored implementation with IPv6 support
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    if evt.Protocol != IPPROTO_UDP || (evt.DstPort != 53 && evt.SrcPort != 53) {
        return nil
    }

    // Handle IPv4 AND IPv6 (MANDATORY!)
    var srcIP, dstIP string
    if evt.Family == AF_INET {
        srcIP = convertIPv4(evt.SrcIP)
        dstIP = convertIPv4(evt.DstIP)
    } else {
        srcIP = convertIPv6(evt.SrcIPv6)
        dstIP = convertIPv6(evt.DstIPv6)
    }

    return &domain.ObserverEvent{
        Type:    string(domain.EventTypeNetwork),
        Subtype: "dns_query",
        NetworkData: &domain.NetworkEventData{
            Protocol: "DNS",
            SrcIP:    srcIP,
            DstIP:    dstIP,
            SrcPort:  evt.SrcPort,
            DstPort:  evt.DstPort,
        },
    }
}
```

**Verify tests still pass:**
```bash
$ go test ./internal/observers/network/ -v -run TestDNSProcessor
=== RUN   TestDNSProcessor_DetectQuery
--- PASS: TestDNSProcessor_DetectQuery (0.00s)
=== RUN   TestDNSProcessor_DetectQuery_IPv6
--- PASS: TestDNSProcessor_DetectQuery_IPv6 (0.00s)
PASS

✅ REFACTOR complete
```

### Step 5: COMMIT

```bash
git add internal/observers/network/processor_dns.go
git add internal/observers/network/processor_dns_test.go
git commit -m "feat(network): add DNS query processor (TDD)

- Implement DNSProcessor for UDP port 53 detection
- Support IPv4 and IPv6 address families
- Use domain.EventTypeNetwork with subtype dns_query
- Tests: TestDNSProcessor_DetectQuery (IPv4 + IPv6)

🚀 Generated with Claude Code"
```

## 📋 Agent Response Format (Required)

Every implementation MUST include:

**FILES MODIFIED:**
```
internal/observers/network/processor_dns.go (new, 45 lines)
internal/observers/network/processor_dns_test.go (new, 67 lines)
internal/observers/network/observer.go (modified, +3 lines)
```

**VERIFICATION COMMANDS:**
```bash
go test ./internal/observers/network/ -v -run TestDNSProcessor
go test ./... -race
go fmt ./... && go vet ./... && golangci-lint run
```

**COMMIT MESSAGE:**
```
feat(network): add DNS query processor (TDD)

- Implementation details
- IPv4 + IPv6 support
- Tests passing
```

## ✅ Agent Pre-Submission Checklist

Before submitting ANY code:

- [ ] **Read existing code first** (never code blind)
- [ ] **Wrote failing test** (RED phase confirmed)
- [ ] **Minimal implementation** (GREEN phase confirmed)
- [ ] **IPv4 + IPv6 tests** (BOTH address families)
- [ ] **No `panic()`** (always return errors)
- [ ] **Structured logging** (use slog, not fmt.Println)
- [ ] **Godoc comments** (all exported symbols)
- [ ] **All tests pass** (including existing tests)
- [ ] **No TODOs/stubs** (complete or don't commit)
- [ ] **Commit < 30 lines** (split if larger)

## ⚠️ WHEN TO ASK (Don't Guess!)

### Architecture Decisions → ASK

**STOP and ASK:**
- "Should I create a new observer or extend existing?"
- "Should this be eBPF or K8s API based observer?"
- "Should I add a new event type or use existing with subtype?"
- "Should I create new eBPF program or use existing?"

**DO NOT:**
- Guess and implement
- Create new eBPF programs (violates single program pattern!)
- Change core interfaces (Observer, ObserverEvent, NetworkEventData)

### eBPF Design → ASK

**STOP and ASK:**
- "Should parsing happen in eBPF or userspace Go?" (Answer: ALWAYS userspace!)
- "Do we need to capture more data in existing eBPF program?"
- "What eBPF maps do we need to share data with userspace?"

**IMPLEMENT:**
- Go processors in userspace (DNS, Link, Status patterns)
- Standard error wrapping (`fmt.Errorf(...: %w, err)`)
- IPv4 + IPv6 support (MANDATORY!)

### Performance Trade-offs → ASK

**STOP and ASK:**
- "Should I batch these events?" (affects latency vs throughput)
- "What's acceptable event processing latency?" (<1ms? <10ms?)
- "Should I cache K8s lookups?" (memory vs CPU trade-off)

**IMPLEMENT:**
- Standard patterns (use existing ring buffer, existing processors)
- Obvious optimizations (preallocate slices, reuse buffers)

## 🌳 DECISION TREES

### When to Create a New Processor?

```
Is this a new network event pattern?
├─ Yes: Create new processor
│   └─ Implement Process(context.Context, NetworkEventBPF) *domain.ObserverEvent
│       ├─ Return nil if event doesn't match
│       ├─ Convert to domain.ObserverEvent if match
│       └─ Add to processEventsStage chain
└─ No: Can existing processor handle it?
    ├─ Yes: Extend existing processor logic
    └─ No: Ask human (might need new processor)
```

### When to Add New eBPF Program?

```
Do we need to capture NEW kernel data?
├─ Yes: Can existing program be extended?
│   ├─ Yes: ADD to existing network_monitor.c (preferred!)
│   │   └─ Example: Add UDP capture to TCP monitoring
│   └─ No: Is it a different kernel subsystem?
│       ├─ Yes: Ask human (might need new program)
│       └─ No: Extend existing program
└─ No: Use existing eBPF data
    └─ Create Go processor in userspace (10x faster!)
```

### When to Use eBPF vs K8s API?

```
What are we observing?
├─ Kernel events (network, syscalls, OOM):
│   └─ Use eBPF observer (network, container, node)
├─ K8s resource changes (pods, deployments, services):
│   └─ Use K8s API observer (client-go informers)
└─ Correlation between both:
    └─ Use Intelligence Service (Level 2)
```

### When to Use IPv4 vs IPv6?

```
Are you handling IP addresses?
├─ Yes: ALWAYS support BOTH (MANDATORY!)
│   └─ Check evt.Family == AF_INET vs AF_INET6
│       ├─ AF_INET → convertIPv4(evt.SrcIP)
│       └─ AF_INET6 → convertIPv6(evt.SrcIPv6)
└─ No: Not applicable
```

### When to Ask Human vs Implement?

```
Is this a new architectural pattern?
├─ Yes: ASK HUMAN
│   └─ Examples: new observer type, new eBPF program, new tier
└─ No: Is it well-defined by existing code?
    ├─ Yes: IMPLEMENT with TDD
    │   └─ Examples: new processor, new filter, new converter
    └─ No: Is documentation clear?
        ├─ Yes: IMPLEMENT following patterns
        └─ No: ASK HUMAN for clarification
```

## 🎯 eBPF-Specific Workflow

### eBPF Capture Design Pattern

**MANDATORY**: eBPF captures, Go parses

```
┌──────────────────────────────────────┐
│ eBPF Program (network_monitor.c)    │
│ - Captures raw data (TCP, UDP, IPs) │
│ - NO protocol parsing                │
│ - Minimal filtering                  │
└─────────────┬────────────────────────┘
              │ Ring Buffer
              ▼
┌──────────────────────────────────────┐
│ Go Userspace (processEventsStage)   │
│ - Parse protocols (DNS, HTTP, etc.)  │
│ - Chain processors (Link, DNS, etc.) │
│ - Convert to domain events           │
└──────────────────────────────────────┘
```

**Why**: eBPF parsing ~500ns/packet, Go parsing ~50ns/packet (10x faster!)

### Adding New Event Detection

**Example: Detect HTTP connections**

1. **Check existing eBPF** - Does network_monitor.c capture TCP already? (YES)
2. **Write failing test** - TestStatusProcessor_DetectHTTP
3. **Create Go processor** - processor_status.go (NOT eBPF parsing!)
4. **Add to chain** - processEventsStage → statusProc.Process()
5. **Test IPv4 + IPv6** - Both address families
6. **Commit** - Small commit (<30 lines)

**DON'T**: Add HTTP parsing to eBPF C code (too slow!)

---

**Remember**: "eBPF captures, Go parses. One program, many processors."
