# TAPIO - Kubernetes Observability Platform

## 🚪 Project Overview
TAPIO is a lean, eBPF + Kubernetes observability platform that generates semantic events from BOTH kernel signals (eBPF) AND Kubernetes API events. Think "Beyla meets Kube-state-metrics" - we observe EVERYTHING happening in your cluster: network flows (eBPF), OOM kills (eBPF), pod scheduling (K8s API), DNS queries (eBPF), deployment changes (K8s API). We combine kernel-level truth with cluster-level context. Named after the Finnish god of forests - he watches over everything growing in the forest.

## 🧠 Hybrid Observer Architecture
**THE CORE INSIGHT**: Kernel visibility alone is blind to orchestration. K8s API alone is blind to reality. Together, they tell the complete story.

```
The Observer Runtime IS the engine. Two worlds feed it:
- eBPF Observers: Kernel/network/container events (what's REALLY happening)
  └─> Network, Container, Node observers
- K8s Observers: API server events (what the orchestrator THINKS is happening)
  └─> Pod, Service, Deployment observers (client-go informers)
- Runtime: Process + correlate events → semantic ObserverEvents
- Emitters: Fan out events (OTLP, NATS, File)
- Intelligence: Correlate eBPF + K8s events → root causes
```

## 🎯 Core Philosophy
- **Hybrid observation** - eBPF for reality + K8s API for context = complete picture
- **Semantic events** - 12 event types (network, kernel, container, pod, service...), typed structs
- **Dual-source correlation** - Match eBPF events to K8s resources (PodUID, namespace, labels)
- **Lean runtime** - Single eBPF program per observer, client-go informers for K8s
- **Multi-tier ready** - SIMPLE (OTLP) → FREE (NATS) → ENTERPRISE (NATS+Intelligence)
- **Direct OTEL** - No wrappers, pure OpenTelemetry instrumentation
- **IPv4 + IPv6** - Both address families supported everywhere
- **TDD mandatory** - Tests first, code second (RED → GREEN → REFACTOR)
- **Zero dependencies** - Minimal external deps, stdlib-first approach
- **Prometheus patterns** - oklog/run, promauto.With(reg), no globals

## 🚨 CRITICAL: ENFORCEMENT STATUS

### ⚠️ CURRENT VIOLATIONS: 82 map[string]interface{} - GOING TO ZERO
**We deleted 124 violations! Down from 206 → 82. Adding even ONE new violation = INSTANT REJECTION**

### AUTOMATED REJECTION SYSTEM ACTIVE
Your code WILL BE AUTOMATICALLY REJECTED if it contains:
- **Code written before tests** - **INSTANT REJECTION - TDD MANDATORY (RED → GREEN → REFACTOR)**
- `map[string]interface{}` - **BANNED - USE TYPED STRUCTS ONLY**
- `interface{}` in public APIs
- `TODO`, `FIXME`, `XXX`, `HACK` comments
- Ignored errors (`_ = someFunc()`)
- Missing tests or <80% coverage
- Stub functions or incomplete implementations
- Commits where tests are not GREEN

```bash
# Pre-commit hooks WILL BLOCK violations:
./scripts/verify-no-interface-abuse.sh
make verify-interface
make verify-todos
make verify
```

## 🏗️ MANDATORY DEVELOPMENT WORKFLOW

### 1. Design Session First
Before writing ANY code:
```markdown
## Design Session Checklist
- [ ] What problem are we solving?
- [ ] What's the simplest solution?
- [ ] Can we break it into smaller functions?
- [ ] What interfaces do we need?
- [ ] What can go wrong?
- [ ] Draw the flow (ASCII or diagram)

## Example Design:
Problem: Detect orphaned AWS resources
Solution: Tag-based lifecycle management

Flow:
┌─────────┐      ┌─────────┐      ┌─────────┐
│  Scan   │ ───> │ Analyze │ ───> │ Decide  │
└─────────┘      └─────────┘      └─────────┘

Interface:
type OrphanHandler interface {
    Scan(context.Context) ([]Resource, error)
    Analyze(Resource) Decision
    Execute(Decision) error
}

Failure modes:
- AWS API timeout → Exponential backoff
- No tags → Mark for review
- Rate limit → Circuit breaker
```

### 2. TDD MANDATORY: RED → GREEN → REFACTOR

**ALL CODE MUST FOLLOW TEST-DRIVEN DEVELOPMENT - ZERO EXCEPTIONS**

#### The Iron Rule: Tests First, Code Second

```go
// ❌ INSTANT REJECTION - Writing code before tests
func NewReconciler() *Reconciler {
    return &Reconciler{}  // Code written first - REJECTED!
}

// ✅ MANDATORY - TDD workflow
// Step 1: RED - Write failing test FIRST
func TestReconciler_HandleOrphans(t *testing.T) {
    reconciler := NewReconciler()  // ❌ Doesn't exist yet - test FAILS
    require.NotNil(t, reconciler)

    orphan := Resource{ID: "i-123", Tags: map[string]string{}}
    decision := reconciler.HandleOrphan(orphan)
    assert.Equal(t, "notify", decision.Action)
}
// Verify: go test ./... → FAILS ✅ RED confirmed

// Step 2: GREEN - Write MINIMAL code to pass
type Reconciler struct {}

func NewReconciler() *Reconciler {
    return &Reconciler{}
}

func (r *Reconciler) HandleOrphan(res Resource) Decision {
    return Decision{Action: "notify"}  // Minimal implementation
}
// Verify: go test ./... → PASS ✅ GREEN confirmed

// Step 3: REFACTOR - Improve code quality, add edge cases
func (r *Reconciler) HandleOrphan(res Resource) Decision {
    if len(res.Tags) == 0 {
        return Decision{Action: "notify"}
    }
    return Decision{Action: "skip"}
}
// Verify: go test ./... → STILL PASS ✅ REFACTOR complete
```

#### TDD Workflow Checklist (MANDATORY for EVERY feature)

**RED Phase** (Write Failing Tests):
- [ ] Write test that defines expected behavior
- [ ] Verify test FAILS (compilation error or test failure)
- [ ] If test passes without implementation → test is wrong!

**GREEN Phase** (Minimal Implementation):
- [ ] Write MINIMAL code to make test pass
- [ ] Verify ALL tests pass
- [ ] NO gold plating, NO premature optimization

**REFACTOR Phase** (Improve Quality):
- [ ] Add edge case tests (IPv4/IPv6, nil checks, errors)
- [ ] Extract helper functions
- [ ] Improve naming and clarity
- [ ] Verify tests STILL pass after refactor

**Commit**:
- [ ] `git add . && git commit -m "feat: ..."` (< 30 lines)
- [ ] Push only when GREEN

#### Observer Test Structure (MANDATORY)

Every observer MUST have these test files:
1. `observer_unit_test.go` - Unit tests for individual methods
2. `observer_e2e_test.go` - End-to-end workflow tests
3. `observer_integration_test.go` - Integration tests with real components
4. `observer_system_test.go` - Linux-specific system tests (eBPF)
5. `observer_performance_test.go` - Performance benchmarks
6. `observer_negative_test.go` - Error handling and edge cases

#### TDD Examples by Use Case

**Example 1: New Feature**
```bash
# RED: Write failing test
$ vim handler_test.go  # Write TestHandler_ProcessEvent
$ go test ./...        # FAILS ✅

# GREEN: Minimal implementation
$ vim handler.go       # Write ProcessEvent() function
$ go test ./...        # PASS ✅

# REFACTOR: Add edge cases
$ vim handler_test.go  # Add TestHandler_ProcessEvent_NilInput
$ vim handler.go       # Add nil check
$ go test ./...        # PASS ✅

# COMMIT
$ git commit -m "feat: add event handler with nil check"
```

**Example 2: Bug Fix**
```bash
# RED: Write test that reproduces bug
$ vim processor_test.go  # TestProcessor_IPv6 (currently fails)
$ go test ./...          # FAILS ✅ Bug reproduced

# GREEN: Fix the bug
$ vim processor.go       # Add IPv6 handling
$ go test ./...          # PASS ✅

# COMMIT
$ git commit -m "fix: handle IPv6 in processor"
```

**Example 3: Refactoring**
```bash
# GREEN: Ensure all tests pass BEFORE refactor
$ go test ./...  # PASS ✅

# REFACTOR: Improve code
$ vim handler.go  # Extract helper function
$ go test ./...   # PASS ✅

# COMMIT
$ git commit -m "refactor: extract validation logic"
```

#### Why TDD is MANDATORY

1. **Prevents bugs** - Tests define correct behavior before code
2. **Enables refactoring** - Tests catch regressions immediately
3. **Documents behavior** - Tests show how code should be used
4. **Forces good design** - Hard-to-test code = bad design
5. **Builds confidence** - Green tests = ship with confidence

**See Section 4 for detailed TDD workflow examples**

**NO EXCEPTIONS. NO EXCUSES. RED → GREEN → REFACTOR.**

### 3. Code in Small Chunks (Following TDD)

**MANDATORY**: Every commit follows RED → GREEN → REFACTOR

```bash
# Work on dedicated branches
git checkout -b feat/orphan-detection

# TDD iterations (RED → GREEN → REFACTOR)
# Iteration 1: Core functionality
$ vim handler_test.go    # RED: Write failing test
$ go test ./...          # FAILS ✅
$ vim handler.go         # GREEN: Minimal implementation
$ go test ./...          # PASS ✅
$ go fmt ./... && go vet ./...
$ git commit -m "feat: add handler core logic" # ≤30 lines

# Iteration 2: Edge case (nil check)
$ vim handler_test.go    # RED: Add TestHandler_NilInput
$ go test ./...          # FAILS ✅
$ vim handler.go         # GREEN: Add nil check
$ go test ./...          # PASS ✅
$ go fmt ./... && go vet ./...
$ git commit -m "feat: add nil check to handler" # ≤30 lines

# Iteration 3: Refactor
$ go test ./...          # GREEN: All tests pass ✅
$ vim handler.go         # REFACTOR: Extract helper function
$ go test ./...          # PASS ✅
$ go fmt ./... && go vet ./...
$ git commit -m "refactor: extract validation logic" # ≤30 lines

# MANDATORY before EVERY commit:
go fmt ./...
go vet ./...
golangci-lint run
go test ./...  # Must be GREEN before commit!

# Push and PR when feature is complete
git push origin feat/orphan-detection
```

**TDD Commit Pattern**:
- Each commit = 1 complete RED → GREEN → REFACTOR cycle
- Commit size: ≤30 lines
- Commit message: `feat:`, `fix:`, `refactor:`, `test:`
- All tests GREEN before push

**NO STUBS. NO TODOs. COMPLETE CODE ONLY.**

### 4. TDD Workflow (RED → GREEN → REFACTOR)

**MANDATORY**: All code must follow strict Test-Driven Development

#### RED Phase: Write Failing Tests First
```go
// Step 1: Write test that FAILS (RED)
func TestLinkProcessor_SYNTimeout(t *testing.T) {
    proc := NewLinkProcessor()  // ❌ Undefined - test fails
    require.NotNil(t, proc)

    evt := NetworkEventBPF{
        OldState: TCP_SYN_SENT,
        NewState: TCP_CLOSE,
        SrcIP:    0x0100007f,
        DstIP:    0x6401a8c0,
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "link_failure", domainEvt.Subtype)
}

// Step 2: Verify test compilation FAILS
// $ go test ./...
// # undefined: NewLinkProcessor ✅ RED phase confirmed
```

#### GREEN Phase: Minimal Implementation
```go
// Step 3: Write MINIMAL code to pass test
type LinkProcessor struct {}

func NewLinkProcessor() *LinkProcessor {
    return &LinkProcessor{}
}

func (p *LinkProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    if evt.OldState == TCP_SYN_SENT && evt.NewState == TCP_CLOSE {
        return &domain.ObserverEvent{
            Type:    "network",
            Subtype: "link_failure",
            NetworkData: &domain.NetworkEventData{
                SrcIP: convertIPv4(evt.SrcIP),
                DstIP: convertIPv4(evt.DstIP),
            },
        }
    }
    return nil
}

// Step 4: Verify tests PASS
// $ go test ./...
// PASS ✅ GREEN phase confirmed
```

#### REFACTOR Phase: Improve Code Quality
```go
// Step 5: Add edge cases (IPv6, validation, etc.)
func TestLinkProcessor_SYNTimeout_IPv6(t *testing.T) {
    // Test IPv6 handling
}

// Step 6: Refactor for better design
func (p *LinkProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    if !p.isSYNTimeout(evt) {
        return nil
    }
    return p.createLinkFailureEvent(evt, "syn_timeout")
}

// Step 7: Verify tests still PASS after refactor
// $ go test ./...
// PASS ✅ REFACTOR complete
```

#### TDD Checklist
- [ ] **RED**: Write failing test first
- [ ] **RED**: Verify compilation fails or test fails
- [ ] **GREEN**: Write minimal implementation
- [ ] **GREEN**: Verify all tests pass
- [ ] **REFACTOR**: Add edge cases, improve design
- [ ] **REFACTOR**: Verify tests still pass
- [ ] **Commit**: `git add . && git commit -m "feat: ..."` (< 30 lines)

**Example Session** (Network Observer Processors):
```bash
# LinkProcessor (TDD - 3 commits)
1. RED:   Write TestLinkProcessor_SYNTimeout → FAIL ✅
2. GREEN: Implement processor_link.go → PASS ✅
3. COMMIT: git commit -m "feat: add LinkProcessor (TDD)"

# Add IPv6 support (TDD - 2 commits)
1. RED:   Write TestLinkProcessor_SYNTimeout_IPv6 → FAIL ✅
2. GREEN: Add IPv6 handling to createLinkFailureEvent → PASS ✅
3. COMMIT: git commit -m "fix: handle IPv6 in LinkProcessor"
```

### 5. eBPF Development Pattern (Brendan Gregg Approach)

**MANDATORY**: Follow single eBPF program + Go processor pattern

#### Architecture: eBPF Captures, Userspace Parses

```
┌─────────────────────────────────────────────────┐
│   eBPF Kernel (network_monitor.c)              │
│   - Single eBPF program (NO new programs!)     │
│   - Captures: TCP states, UDP traffic, IPs     │
│   - Minimal processing (just capture data)     │
└──────────────────┬──────────────────────────────┘
                   │ Ring Buffer
                   ▼
┌─────────────────────────────────────────────────┐
│   Go Userspace (processEventsStage)            │
│   Processor Chain:                              │
│   1. LinkProcessor   → link_failure             │
│   2. DNSProcessor    → dns_query, dns_response  │
│   3. StatusProcessor → http_connection          │
│   4. Fallback        → legacy events            │
└──────────────────┬──────────────────────────────┘
                   │
                   ▼
         domain.NetworkEventData
         (Type + Subtype pattern)
```

#### Why This Pattern Works

**Brendan Gregg BPF Performance Tools (Chapter 10)**:
> "eBPF should capture, userspace should parse. Parsing complex protocols in eBPF is slow and error-prone. Let eBPF collect the raw data, then parse it in userspace where you have full language features."

**Performance**:
- eBPF parsing: ~500ns per packet (slow, limited instructions)
- Go parsing: ~50ns per packet (10x faster!)
- Ring buffer already copies to userspace - parsing there is free

**Benefits**:
1. **Single eBPF program** - Lower kernel overhead, simpler lifecycle
2. **Flexible parsing** - Go is easier to debug than eBPF C code
3. **No BTF dependencies** - Don't need kernel struct definitions for DNS/HTTP parsing
4. **Easier testing** - Can unit test processors without eBPF
5. **IPv4 + IPv6 support** - Handle both address families in Go

#### Implementation Pattern

**Step 1: Design Processor** (following TDD RED phase)
```go
// processor_dns.go - RED phase (write test first!)
func TestDNSProcessor_DetectQuery(t *testing.T) {
    proc := NewDNSProcessor()  // Will fail - doesn't exist yet
    evt := NetworkEventBPF{
        Protocol: IPPROTO_UDP,
        DstPort:  53,  // DNS port
    }

    domainEvt := proc.Process(context.Background(), evt)
    require.NotNil(t, domainEvt)
    assert.Equal(t, "dns_query", domainEvt.Subtype)
}
```

**Step 2: Implement Processor** (GREEN phase)
```go
// processor_dns.go
type DNSProcessor struct {
    // Future: OTEL metrics
}

func NewDNSProcessor() *DNSProcessor {
    return &DNSProcessor{}
}

func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    // Only process UDP traffic
    if evt.Protocol != IPPROTO_UDP {
        return nil
    }

    // Check if DNS port (53)
    if evt.DstPort != 53 && evt.SrcPort != 53 {
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

    // Use EXISTING domain model (no new structs!)
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

**Step 3: Integrate into Observer** (processEventsStage)
```go
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
    // Initialize all processors
    linkProc := NewLinkProcessor()
    dnsProc := NewDNSProcessor()
    statusProc := NewStatusProcessor()

    for evt := range eventCh {
        // Try processors in order (fast exit on match)
        if domainEvent := linkProc.Process(ctx, evt); domainEvent != nil {
            n.emitDomainEvent(ctx, domainEvent)
            continue
        }

        if domainEvent := dnsProc.Process(ctx, evt); domainEvent != nil {
            n.emitDomainEvent(ctx, domainEvent)
            continue
        }

        if domainEvent := statusProc.Process(ctx, evt); domainEvent != nil {
            n.emitDomainEvent(ctx, domainEvent)
            continue
        }

        // Fallback: legacy event handling
        n.processLegacyEvent(ctx, evt)
    }
}
```

#### eBPF Development Checklist

- [ ] **Design**: Document processor pattern in Design Doc (e.g., docs/003-*.md)
- [ ] **TDD RED**: Write processor tests first (IPv4 + IPv6!)
- [ ] **TDD GREEN**: Implement processor with IPv4/IPv6 support
- [ ] **Integration**: Add to processEventsStage chain
- [ ] **Verify**: Check existing eBPF program captures needed data
- [ ] **NO NEW eBPF**: Reuse existing network_monitor.c (single program!)
- [ ] **Test Coverage**: Add IPv6 tests for all processors
- [ ] **Commit**: Small commits (<30 lines each)

#### IPv4 + IPv6 Support (MANDATORY)

**ALWAYS** handle both address families:
```go
// ❌ WRONG - IPv6 will break!
func createEvent(evt NetworkEventBPF) *domain.ObserverEvent {
    return &domain.ObserverEvent{
        NetworkData: &domain.NetworkEventData{
            SrcIP: convertIPv4(evt.SrcIP),  // Bug: No Family check!
            DstIP: convertIPv4(evt.DstIP),
        },
    }
}

// ✅ CORRECT - Handles both IPv4 and IPv6
func createEvent(evt NetworkEventBPF) *domain.ObserverEvent {
    var srcIP, dstIP string
    if evt.Family == AF_INET {
        srcIP = convertIPv4(evt.SrcIP)
        dstIP = convertIPv4(evt.DstIP)
    } else {
        srcIP = convertIPv6(evt.SrcIPv6)
        dstIP = convertIPv6(evt.DstIPv6)
    }

    return &domain.ObserverEvent{
        NetworkData: &domain.NetworkEventData{
            SrcIP: srcIP,
            DstIP: dstIP,
        },
    }
}

// ✅ TESTS - Always test both address families
func TestProcessor_IPv4(t *testing.T) {
    evt := NetworkEventBPF{
        Family: AF_INET,
        SrcIP:  0x0100007f,  // 127.0.0.1
    }
    // ... test IPv4
}

func TestProcessor_IPv6(t *testing.T) {
    evt := NetworkEventBPF{
        Family:  AF_INET6,
        SrcIPv6: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},  // ::1
    }
    // ... test IPv6
}
```

#### References

- **Design Doc 003**: DNS/Link/Status Integration (`docs/003-network-observer-dns-link-status-integration.md`)
- **README.md**: Processor Pattern Examples (`internal/observers/common/bpf/lib/README.md`)
- **Brendan Gregg BPF Performance Tools**: https://github.com/brendangregg/bpf-perf-tools-book
- **ADR 002**: Observer Consolidation (`docs/002-tapio-observer-consolidation.md`)

## ⛔ BANNED PATTERNS - AUTOMATIC REJECTION

### map[string]interface{} IS BANNED
```go
// ❌ NEVER - INSTANT REJECTION
func Process(data map[string]interface{}) error
config := map[string]interface{}{"timeout": 30}

// ✅ ALWAYS - TYPED STRUCTS ONLY
type ProcessConfig struct {
    Timeout   time.Duration `json:"timeout"`
    BatchSize int          `json:"batch_size"`
}
func Process(config ProcessConfig) error
```

### NO TODOs OR STUBS - ZERO TOLERANCE
```go
// ❌ INSTANT REJECTION
func Process() error {
    // TODO: implement
    return nil
}

// ❌ INSTANT REJECTION
func Handle() error {
    panic("not implemented")
}

// ✅ COMPLETE IMPLEMENTATION ONLY
func Process() error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    if err := execute(); err != nil {
        return fmt.Errorf("execution failed: %w", err)
    }
    return cleanup()
}
```

## 🎯 SMART DESIGN PRINCIPLES

1. **Hybrid Observer-First** - Every feature starts with "what signal do we need?" (eBPF kernel event OR K8s API event)
2. **eBPF captures, userspace parses** - eBPF collects raw data, Go processes it (10x faster!)
3. **K8s informers for context** - Use client-go informers, not polling API server
4. **Correlate both worlds** - Match eBPF events to K8s resources (PodUID, namespace, labels)
5. **Small functions** - If you can't understand it in 10 seconds, split it
6. **Interface-driven** - Define interfaces first, implement later
7. **Single eBPF program per observer** - Don't add new eBPF programs, enhance existing ones
8. **IPv4 + IPv6 everywhere** - Every network processor must handle both address families
9. **Composition over inheritance** - Use interfaces and composition
10. **Fail fast** - Validate early, return errors immediately
11. **No magic** - Code should be obvious, not clever
12. **Brendan Gregg's wisdom** - "eBPF should capture, userspace should parse"
13. **Typed everything** - Use domain types, not primitives or interface{}
14. **Test both sources** - Unit tests for eBPF observers AND K8s observers
15. **Test both families** - Every network test needs IPv4 AND IPv6 variants

## 🏛️ ARCHITECTURE RULES (IMMUTABLE)

### 5-Level Dependency Hierarchy
```
Level 0: pkg/domain/       # ZERO dependencies
Level 1: pkg/collectors/   # Domain ONLY
         pkg/observers/    # Domain ONLY
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

**VIOLATION = IMMEDIATE TASK REASSIGNMENT**

## 💀 PLATFORM REALITY: LINUX-ONLY WITH MOCK MODE

### Development Setup
```bash
# Production: Linux with eBPF (all observers working)
sudo go run ./cmd/observers

# Mac Development: Use mock mode for local iteration
export TAPIO_MOCK_MODE=true
go run ./cmd/observers

# Real Testing: Colima VM for eBPF
colima start --mount $HOME/tapio:w
colima ssh
cd /tapio && sudo go run ./cmd/observers
```

### Observer Architecture (NO STUBS!)
```go
//go:build linux
// +build linux

package dns

func NewObserver(name string, cfg Config) (*Observer, error) {
    // Check for mock mode
    mockMode := os.Getenv("TAPIO_MOCK_MODE") == "true"
    if mockMode {
        logger.Info("Running in MOCK MODE")
    }
    // COMPLETE IMPLEMENTATION - NO STUBS
}
```

## 🔭 OPENTELEMETRY STANDARDS (MANDATORY)

### Direct OTEL Only - NO WRAPPERS
```go
// ❌ BANNED - Custom telemetry wrappers
import "github.com/yairfalse/tapio/pkg/integrations/telemetry"

// ✅ REQUIRED - Direct OpenTelemetry
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

type Observer struct {
    // Required OTEL fields
    tracer          trace.Tracer
    eventsProcessed metric.Int64Counter
    errorsTotal     metric.Int64Counter
    processingTime  metric.Float64Histogram
}

// Metric naming standards
eventsCounter := "observer_events_processed_total"      // _total suffix
durationHist := "observer_processing_duration_ms"       // unit in name
activeGauge := "observer_active_connections"           // current state
```

## 🧪 TESTING REQUIREMENTS

### Minimum 80% Coverage - NO EXCEPTIONS
```go
// Every public function needs tests
func TestObserverLifecycle(t *testing.T) {
    observer, err := NewObserver("test")
    require.NoError(t, err)
    require.NotNil(t, observer)

    ctx := context.Background()
    err = observer.Start(ctx)
    require.NoError(t, err)

    assert.True(t, observer.IsHealthy())

    err = observer.Stop()
    require.NoError(t, err)
}

// Test error paths
func TestObserverErrors(t *testing.T) {
    observer := &Observer{}
    err := observer.ProcessEvent(nil)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "nil event")
}
```

## 📋 VERIFICATION COMMANDS (BEFORE EVERY COMMIT)

```bash
# Quick verification during development
make verify-quick      # 5-30 seconds

# Full verification before PR
make verify-full       # Complete validation

# Manual verification
gofmt -l . | grep -v vendor | wc -l    # Must return 0
go build ./...                          # Must pass
go test ./... -race                     # Must pass
go test ./... -cover                    # Must show >80%
golangci-lint run                       # No warnings
```

## 🎯 DEFINITION OF DONE

A task is ONLY complete when:
- [ ] Design doc written and reviewed
- [ ] Tests written BEFORE implementation
- [ ] All tests passing with -race detector
- [ ] Coverage >= 80% per package
- [ ] NO TODOs, FIXMEs, or stub functions
- [ ] NO map[string]interface{} anywhere
- [ ] All errors handled with context
- [ ] Resources properly cleaned up
- [ ] Each commit <= 30 lines
- [ ] PR <= 200 lines total
- [ ] make verify-full passes
- [ ] Branch builds in CI

## 🚫 INSTANT REJECTION CRITERIA

Your PR/commit will be INSTANTLY REJECTED for:

1. **ANY map[string]interface{} usage** (except JSON unmarshaling)
2. **ANY TODO/FIXME/stub functions**
3. **Missing tests or <80% coverage**
4. **Ignored errors** (`_ = func()`)
5. **Architecture violations** (importing from higher level)
6. **Commits > 30 lines**
7. **PRs > 200 lines**
8. **No design doc**
9. **interface{} in public APIs**
10. **Missing verification output**
11. **Hardcoded values** (paths, IPs, credentials)
12. **No error context** (bare errors without wrapping)

## 🔥 ERROR HANDLING STANDARDS

```go
// ❌ BAD - No context
return fmt.Errorf("failed")

// ❌ BAD - Ignored error
_ = collector.Start()

// ✅ GOOD - Contextual error with wrapping
if err := collector.Start(ctx); err != nil {
    return fmt.Errorf("failed to start collector %s: %w", name, err)
}

// ✅ GOOD - Proper error handling chain
func Process(ctx context.Context) error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }

    result, err := execute(ctx)
    if err != nil {
        return fmt.Errorf("execution failed for ID %s: %w", result.ID, err)
    }

    return nil
}
```

## 🔒 RESOURCE MANAGEMENT

```go
// ❌ BAD - Resource leak
func Process() error {
    conn := getConnection()
    return doWork(conn)  // Connection never closed!
}

// ✅ GOOD - Proper cleanup with defer
func Process() error {
    conn, err := getConnection()
    if err != nil {
        return fmt.Errorf("failed to get connection: %w", err)
    }
    defer conn.Close()

    return doWork(conn)
}

// ✅ GOOD - Context-aware cleanup
func Process(ctx context.Context) error {
    conn, err := getConnection(ctx)
    if err != nil {
        return fmt.Errorf("connection failed: %w", err)
    }
    defer func() {
        if err := conn.Close(); err != nil {
            log.Printf("failed to close connection: %v", err)
        }
    }()

    return doWork(ctx, conn)
}
```

## 🚀 PERFORMANCE STANDARDS

```go
// Memory pooling for hot paths
var eventPool = sync.Pool{
    New: func() interface{} {
        return &Event{Data: make([]byte, 0, 1024)}
    },
}

// Buffered channels for producers
events := make(chan Event, 1000)  // Never unbuffered

// Preallocate slices when size is known
results := make([]Result, 0, expectedSize)
```

## 📝 GIT WORKFLOW ENFORCEMENT

### Branch Naming
```bash
feat/feature-name     # New feature
fix/bug-description   # Bug fix
perf/optimization     # Performance improvement
docs/what-changed     # Documentation only
test/what-testing     # Test additions
refactor/what-changed # Code refactoring
```

### Commit Message Format
```bash
type(scope): description

- Detailed point 1
- Detailed point 2

Closes #123
```

### PR Rules
- **Max 200 lines** (split larger changes)
- **Must pass CI** (all checks green)
- **Must include verification output**
- **Design doc linked**
- **Tests included**

## 🛡️ SECURITY STANDARDS

```go
// NEVER hardcode secrets
password := os.Getenv("DB_PASSWORD")  // Good
password := "admin123"                 // INSTANT REJECTION

// NEVER trust user input
func Process(userInput string) error {
    sanitized := sanitize(userInput)
    if err := validate(sanitized); err != nil {
        return fmt.Errorf("invalid input: %w", err)
    }
    return execute(sanitized)
}

// ALWAYS use context for cancellation
func LongRunning(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            // Do work
        }
    }
}
```

## 🎖️ QUALITY METRICS

Every component MUST maintain:
- **Test Coverage**: >= 80%
- **Cyclomatic Complexity**: < 10 per function
- **Function Length**: < 50 lines
- **File Length**: < 500 lines
- **Package Dependencies**: Follow 5-level hierarchy
- **Error Rate**: < 0.1% in production
- **Memory Leaks**: ZERO tolerance
- **Data Races**: ZERO tolerance

## 🏆 FINAL ENFORCEMENT

**NO EXCUSES. NO SHORTCUTS. NO STUBS. NO COMPROMISES.**

Every line of code represents Tapio's quality. Incomplete code, TODOs, or stubs are NEVER acceptable. Write complete, tested, production-ready code or don't write anything at all.

**Remember:**
- **NO STUBS** - Complete implementations only
- **NO TODOs** - Finish it or don't start
- **Test first** - TDD is mandatory
- **Small commits** - 30 lines maximum
- **Format always** - `go fmt` before every commit
- **No map[string]interface{}** - Typed structs only
- **80% coverage minimum** - Test everything

**DELIVER EXCELLENCE OR GET REASSIGNED.**

## 📦 PKG/ REFACTORING DESIGN SESSION (Architecture Compliance)

### Problem
Current `pkg/` structure violates the 5-Level Dependency Hierarchy by exposing implementation details as public APIs.

### Solution
Restructure to follow mandatory architecture levels:

```
Level 0: pkg/domain/       # ZERO dependencies ✅ KEEP
Level 1: internal/observers/    # Domain ONLY ❌ MOVE FROM pkg/
Level 2: internal/intelligence/ # Domain + L1 ❌ MOVE FROM pkg/
Level 3: internal/integrations/ # Domain + L1 + L2 ❌ MOVE FROM pkg/
Level 4: pkg/interfaces/   # All above ✅ KEEP
```

### Refactoring Flow
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Analyze   │───▶│  Restructure │───▶│   Verify    │
│ Current pkg │    │  Following   │    │ Standards   │
│ Structure   │    │ Architecture │    │ Compliance  │
└─────────────┘    └─────────────┘    └─────────────┘
```

### Implementation Strategy
1. **Move Level 3 first** (`pkg/integrations/` → `internal/integrations/`)
2. **Move Level 2** (`pkg/intelligence/` → `internal/intelligence/`)
3. **Move Level 1** (`pkg/observers/` → `internal/observers/`)
4. **Keep Level 0 & 4** (`pkg/domain/`, `pkg/interfaces/`)

### Failure Prevention
- **Breaking imports** → Move in reverse dependency order (Level 3 → 1)
- **Architecture violations** → Pre-verify each package's dependency level
- **Test failures** → Run `make verify-full` after each move

### Success Criteria
- [ ] Only public APIs remain in `pkg/` (domain, interfaces, config)
- [ ] All implementation details moved to `internal/`
- [ ] Architecture hierarchy properly enforced
- [ ] Zero import breaks
- [ ] All tests passing
- [ ] `make verify-full` passes