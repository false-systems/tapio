# Code Standards & Anti-Patterns

> **Purpose**: Code quality rules, anti-patterns to avoid, and verification checklist

## ⛔ ANTI-PATTERNS - DO NOT DO THESE

### ❌ Anti-Pattern #1: map[string]interface{} Abuse

```go
// ❌ NEVER DO THIS
func ProcessEvent(data map[string]interface{}) error {
    eventType := data["type"].(string)  // Runtime panic risk
    timestamp := data["timestamp"].(int64)  // No type safety
}

// ✅ ALWAYS DO THIS
type NetworkEventData struct {
    Protocol  string    // Compile-time type checking
    SrcIP     string    // Proper Go types
    DstIP     string
    SrcPort   uint16
    DstPort   uint16
}

func ProcessEvent(event *domain.ObserverEvent) error {
    if event.NetworkData.Protocol == "DNS" {
        // Typos caught at compile time
    }
}
```

**Why it's bad:**
- No compile-time type checking
- Runtime panics on type assertions
- Typos in field names go undetected
- IDE autocomplete doesn't work
- Violates TAPIO's core principle of "Typed Everything"

**Current State**: 2 usages in test files for JSON unmarshaling (allowed exception)

### ❌ Anti-Pattern #2: Hardcoded Configuration

```go
// ❌ NEVER DO THIS
const (
    polkuEndpoint = "localhost:50051"
    k8sConfigPath = "/home/user/.kube/config"
    nodeName = "worker-1"
)

// ✅ ALWAYS DO THIS
type Config struct {
    PolkuEndpoint string `env:"POLKU_ENDPOINT" yaml:"polku_endpoint"`
    K8sConfigPath string `env:"KUBECONFIG" yaml:"kubeconfig"`
    NodeName      string `env:"NODE_NAME" yaml:"node_name"`
}
```

**Forbidden hardcoded values:**
- URLs, IPs, ports
- File paths
- Credentials, API keys
- Node names, namespaces
- Timeouts, limits
- Any environment-specific data

### ❌ Anti-Pattern #3: Long Functions (God Functions)

```go
// ❌ NEVER DO THIS
func ProcessNetworkEvent(evt NetworkEventBPF) error {
    // 200 lines of code doing:
    // - protocol detection, IP conversion, DNS parsing, correlation...
}

// ✅ ALWAYS DO THIS
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
    linkProc := NewLinkProcessor()
    dnsProc := NewDNSProcessor()
    statusProc := NewStatusProcessor()

    for evt := range eventCh {
        if domainEvent := linkProc.Process(ctx, evt); domainEvent != nil {
            n.emitDomainEvent(ctx, domainEvent)
            continue
        }
        // ... other processors
    }
}

// Each processor is <30 lines, focused on ONE pattern
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent { ... }
```

**Why it's bad:**
- Hard to test (need to test all paths)
- Hard to understand (cognitive overload)
- Hard to debug (too many responsibilities)
- Violates Single Responsibility Principle
- Fails golangci-lint complexity checks

**Rules:**
- Functions MUST be < 50 lines
- Single responsibility per function
- Extract complex logic into processors
- Use meaningful names

### ❌ Anti-Pattern #4: Ignoring Errors

```go
// ❌ NEVER DO THIS
_ = observer.Start(ctx)
conn.Close()  // Error return value ignored

// ✅ ALWAYS DO THIS
if err := observer.Start(ctx); err != nil {
    return fmt.Errorf("failed to start observer: %w", err)
}

defer func() {
    if err := conn.Close(); err != nil {
        slog.Error("failed to close connection", "error", err)
    }
}()
```

**Why it's bad:**
- Silent failures (goroutine leaks, resource leaks)
- Fails errcheck linter
- Makes debugging impossible

### ❌ Anti-Pattern #5: Missing IPv6 Support

```go
// ❌ NEVER DO THIS
func createEvent(evt NetworkEventBPF) *domain.ObserverEvent {
    return &domain.ObserverEvent{
        NetworkData: &domain.NetworkEventData{
            SrcIP: convertIPv4(evt.SrcIP),  // Bug: IPv6 will panic!
            DstIP: convertIPv4(evt.DstIP),
        },
    }
}

// ✅ ALWAYS DO THIS
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
```

**Why it's bad:**
- IPv6 traffic will cause panics or incorrect data
- Production Kubernetes clusters use IPv6
- Violates TAPIO standard "IPv4 + IPv6 everywhere"

**Rules:**
- ALWAYS check evt.Family before IP conversion
- ALWAYS test both AF_INET and AF_INET6
- Use convertIPv4() for IPv4, convertIPv6() for IPv6

### ❌ Anti-Pattern #6: Custom Telemetry Wrappers

```go
// ❌ NEVER DO THIS
package telemetry

type Metrics struct {
    counters map[string]int64
}
func (m *Metrics) IncrementCounter(name string) { ... }

// ✅ ALWAYS DO THIS - Direct OTEL
import (
    "go.opentelemetry.io/otel/metric"
)

type NetworkObserver struct {
    eventsProcessed metric.Int64Counter
    processingTime  metric.Float64Histogram
}

func NewNetworkObserver(meter metric.Meter) *NetworkObserver {
    return &NetworkObserver{
        eventsProcessed: meter.Int64Counter("tapio_network_events_processed_total"),
        processingTime: meter.Float64Histogram("tapio_network_processing_duration_ms"),
    }
}
```

**Why it's bad:**
- Reinvents the wheel
- Incompatible with OTEL ecosystem
- Violates "Direct OTEL Only" rule

### ❌ Anti-Pattern #7: eBPF Protocol Parsing

```go
// ❌ NEVER DO THIS (in eBPF C code)
// network_monitor.c
SEC("tp/sock/inet_sock_set_state")
int trace_sock_state(struct trace_event_raw_inet_sock_set_state *ctx) {
    // 100 lines of DNS packet parsing in eBPF...
    // Slow, error-prone, hard to test!
}

// ✅ ALWAYS DO THIS (in Go userspace)
// processor_dns.go
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    if evt.Protocol != IPPROTO_UDP || (evt.DstPort != 53 && evt.SrcPort != 53) {
        return nil
    }
    // Fast Go parsing, easy to test!
}
```

**Why it's bad:**
- eBPF parsing: ~500ns per packet (slow!)
- Go parsing: ~50ns per packet (10x faster!)
- eBPF harder to debug and test
- Violates "eBPF captures, Go parses" pattern

**Rules:**
- eBPF: Capture raw data ONLY (TCP states, IPs, ports)
- Go: Parse protocols (DNS, HTTP, etc.)
- Single eBPF program per observer
- Processor chain in userspace

## 🔧 OpenTelemetry Patterns (MANDATORY)

### Direct OTEL Only (No Wrappers)

```go
// ✅ REQUIRED - Direct OTEL metric creation
import (
    "go.opentelemetry.io/otel/metric"
)

type Observer struct {
    eventsTotal metric.Int64Counter
    errorTotal  metric.Int64Counter
}

func NewObserver(meter metric.Meter) *Observer {
    return &Observer{
        eventsTotal: meter.Int64Counter("tapio_observer_events_total"),
        errorTotal:  meter.Int64Counter("tapio_observer_errors_total"),
    }
}
```

### Metric Naming Standards

```go
// ✅ CORRECT metric names
eventsCounter := "tapio_observer_events_processed_total"  // _total suffix for counters
durationHist  := "tapio_observer_processing_duration_ms"  // unit in name
activeGauge   := "tapio_observer_active_connections"      // current state

// ❌ WRONG
eventsCounter := "events"  // No prefix, no suffix
durationHist  := "processing_time"  // No unit, vague name
```

### Testing Metrics

```go
func TestObserver_Metrics(t *testing.T) {
    // Use test meter provider
    reader := metric.NewManualReader()
    provider := metric.NewMeterProvider(metric.WithReader(reader))
    meter := provider.Meter("test")

    observer := NewObserver(meter)
    observer.ProcessEvent(ctx, event)

    // Verify metrics
    var rm metricdata.ResourceMetrics
    err := reader.Collect(ctx, &rm)
    require.NoError(t, err)
    // ... assertions on metrics
}
```

## 🎯 Architecture Constraints

### 5-Level Dependency Hierarchy (IMMUTABLE)

```
Level 0: pkg/domain/       # ZERO dependencies
Level 1: internal/observers/    # Domain ONLY
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

**VIOLATION = INSTANT REJECTION**

```go
// ❌ WRONG - Level 1 importing Level 3
package observers
import "github.com/yairfalse/tapio/pkg/integrations/polku"  // VIOLATION!

// ✅ CORRECT - Level 1 imports Level 0 only
package observers
import "github.com/yairfalse/tapio/pkg/domain"
```

### Event Schema (IMMUTABLE)

- **12 base event types** - network, kernel, container, deployment, pod, service, volume, config, health, performance, resource, cluster
- **Typed data structs** - NetworkEventData, KernelEventData, ContainerEventData
- **NO map[string]interface{}** - Use typed structs

## 📋 Pre-Commit Verification (MANDATORY)

```bash
# 1. Format - MANDATORY
go fmt ./...

# 2. Vet - MANDATORY
go vet ./...

# 3. Lint - MANDATORY
golangci-lint run

# 4. Test with race detector
go test ./... -race

# 5. Coverage (variable per package)
go test ./... -cover

# 6. No TODOs, FIXMEs, or stubs
grep -r "TODO\|FIXME\|XXX\|HACK" internal/ pkg/ cmd/

# 7. No map[string]interface{} (except JSON tests)
grep -r "map\[string\]interface{}" --include="*.go" . | grep -v vendor | grep -v "_test.go"
```

## ✅ Definition of Done

A feature is complete when:

- [ ] Design documented (docs/designs/*.md or ADR)
- [ ] Functions are small (<50 lines)
- [ ] Tests written FIRST (TDD: RED → GREEN → REFACTOR)
- [ ] IPv4 + IPv6 support (if handling IPs)
- [ ] `go fmt` applied
- [ ] `go vet` passes
- [ ] `golangci-lint` passes
- [ ] Error handling with context
- [ ] No map[string]interface{}
- [ ] No hardcoded data
- [ ] No TODOs or stubs
- [ ] OTEL metrics added (if applicable)
- [ ] Follows 5-level hierarchy
- [ ] Commit < 30 lines

## 🏛️ eBPF Development Standards

### Single eBPF Program Pattern

```
✅ CORRECT: One eBPF program, multiple Go processors
- network_monitor.c (captures TCP, UDP, IPs)
  └─> LinkProcessor (Go)
  └─> DNSProcessor (Go)
  └─> StatusProcessor (Go)

❌ WRONG: Multiple eBPF programs for each protocol
- dns_monitor.c (eBPF DNS parsing)
- http_monitor.c (eBPF HTTP parsing)
- tcp_monitor.c (eBPF TCP parsing)
```

### eBPF Capture, Go Parse

```go
// ✅ CORRECT - eBPF captures, Go parses
// network_monitor.c - Just capture raw data
struct network_event {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
};

// processor_dns.go - Parse protocol in Go
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
    if evt.Protocol == IPPROTO_UDP && evt.DstPort == 53 {
        return createDNSEvent(evt)  // Fast Go logic
    }
    return nil
}
```

## 📝 Code Style Guidelines

### Go Code Standards
- **Imports order**: standard lib → external packages → tapio packages
- **Error handling**: Always check and wrap errors with context
- **Logging**: Use structured logging with slog
- **Naming**: CamelCase for exported, camelCase for non-exported
- **Documentation**: Document ALL exported functions, types, variables
- **Table-driven tests**: Use for multiple test cases

### Error Handling - Always Context

```go
// ❌ BAD
return err
return fmt.Errorf("failed")

// ✅ GOOD
return fmt.Errorf("failed to process network event from %s:%d: %w", srcIP, srcPort, err)
```

### Small Commits (<30 lines)

```bash
# ✅ GOOD - Each commit is one TDD cycle
git commit -m "feat(network): add DNSProcessor (RED)"      # 15 lines
git commit -m "feat(network): implement DNSProcessor (GREEN)"  # 25 lines
git commit -m "feat(network): add IPv6 support (REFACTOR)"   # 18 lines

# ❌ BAD - One huge commit
git commit -m "feat(network): add DNS and HTTP processors"  # 250 lines
```

## 🔥 Instant Rejection Checklist

Code will be INSTANTLY REJECTED if it contains:

- [ ] map[string]interface{} (except JSON tests)
- [ ] Hardcoded URLs, IPs, paths, credentials
- [ ] Functions > 50 lines
- [ ] Ignored errors (`_ = someFunc()`)
- [ ] Missing IPv6 support (if handling IPs)
- [ ] Custom telemetry wrappers
- [ ] TODO/FIXME/XXX/HACK comments
- [ ] eBPF protocol parsing (use Go processors!)
- [ ] Architecture hierarchy violations
- [ ] Commits > 30 lines
- [ ] Missing tests or test-after-code

## 🎯 Quality Metrics

Every component MUST maintain:
- **map[string]interface{}**: 0 (2 test exceptions allowed)
- **Cyclomatic Complexity**: < 10 per function
- **Function Length**: < 50 lines
- **Commit Size**: < 30 lines
- **PR Size**: < 200 lines
- **IPv4 + IPv6**: Both tested if handling IPs
- **Architecture**: Follow 5-level hierarchy
- **TDD**: RED → GREEN → REFACTOR for every feature

---

**Remember**: "Format, vet, lint - every single commit"
