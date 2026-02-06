# TAPIO - Kubernetes eBPF Observability Agent

> **For AI Agents**: Read [docs/ai/WORKFLOW.md](docs/ai/WORKFLOW.md) for TDD workflow and [docs/ai/STANDARDS.md](docs/ai/STANDARDS.md) for code quality rules.

## 🚪 Project Overview
TAPIO is **Edge Intelligence** for Kubernetes - an eBPF-based agent that captures kernel-level events, filters to anomalies at the edge (~1%), and sends enriched events to AHTI (Central Intelligence) for root cause analysis.

**What makes TAPIO special:** It doesn't just collect data - it learns baselines (RTT, memory patterns) and only sends what matters.

## 🧠 Architecture-First Development
```
The 5-Level Dependency Hierarchy IS the law. No circular dependencies, no shortcuts:
- Level 0: pkg/domain/       (ZERO dependencies - domain models)
- Level 1: internal/observers/    (Domain ONLY - eBPF observers)
- Level 2: pkg/intelligence/ (Domain + L1 - event enrichment)
- Level 3: pkg/integrations/ (Domain + L1 + L2 - POLKU, K8s)
- Level 4: pkg/interfaces/   (All above - OTLP, REST APIs)
```

## 🎯 Core Philosophy
- **eBPF captures, Go parses** - Single eBPF program, userspace processors
- **TDD mandatory** - RED → GREEN → REFACTOR (NO exceptions)
- **Small commits** - ≤30 lines per commit, ≤200 lines per PR
- **Typed everything** - ZERO map[string]interface{} (2 test exceptions allowed)
- **Direct OTEL** - No wrappers, pure OpenTelemetry
- **Linux-only** - Mock mode for Mac development, Colima for eBPF testing

## 🎯 The Problem We Solve

**Kubernetes observability requires kernel-level visibility** - APM tools miss network failures, container lifecycle events, and node-level issues.

**The Cost**: Blind spots in production, slow root cause analysis, vendor lock-in.

**Our Solution**:
1. **eBPF Kernel Capture** - Zero overhead observability at kernel level
2. **Semantic Correlation** - Automatic causality tracking between events
3. **Flexible Export** - OTLP (Simple), POLKU (gRPC gateway to AHTI)

## 🔗 TAPIO-PORTTI-AHTI Architecture

**TAPIO = eBPF Edge Intelligence. PORTTI = K8s API Watcher. AHTI = Central Intelligence.**

```
TAPIO (per node)
├── eBPF Observers ──filter──→ ~1% anomalies ─┐
│   (network, container, node)                │
│                                             ├──→ POLKU ──→ AHTI
PORTTI (cluster-wide, 1-2 replicas)           │
└── K8s API Watcher ─────────→ 100% events ───┘
    (deployments, pods, nodes, services)
```

- **TAPIO**: eBPF kernel events → filter to 1% anomalies (OOM, connection failures, RTT spikes)
- **PORTTI**: K8s API events → send 100% (deployments, pods - they're rare but causal)
- **AHTI**: Central intelligence - receives from both, builds causality graph

See: **[Edge-Central Data Flow](docs/designs/edge-central-data-flow.md)**

## 📚 Documentation

- **[AI Workflow](docs/ai/WORKFLOW.md)** - TDD process, commit workflow, eBPF patterns
- **[Code Standards](docs/ai/STANDARDS.md)** - Anti-patterns, quality rules, verification
- **[Architecture](docs/002-tapio-observer-consolidation.md)** - Observer consolidation ADR
- **[Edge-Central Data Flow](docs/designs/edge-central-data-flow.md)** - Edge vs Central intelligence
- **[Intelligence Service](docs/designs/intelligence-service-foundation.md)** - Tier architecture

## 🏗️ Quick Start for AI Agents

### TDD Workflow (Mandatory)
```bash
# RED: Write failing test
go test ./internal/observers/network -v -run TestProcessor  # Should FAIL

# GREEN: Minimal implementation
go test ./internal/observers/network -v -run TestProcessor  # Should PASS

# REFACTOR: Clean up
go fmt ./... && go vet ./... && golangci-lint run
```

### Pre-Commit Checklist
- [ ] `go fmt ./...` + `go vet ./...` + `golangci-lint run`
- [ ] `go test ./... -race`
- [ ] No map[string]interface{} (except JSON tests)
- [ ] Functions < 50 lines
- [ ] No TODOs/stubs
- [ ] Follow 5-level hierarchy

## ⛔ Core Rules (INSTANT REJECTION)

```go
// ❌ NEVER
var data map[string]interface{}        // Untyped data
import "tapio/pkg/integrations/telemetry"  // Custom OTEL wrapper
func Process() { /* 200 lines */ }     // God functions

// ✅ ALWAYS
type NetworkEventData struct { ... }   // Typed structs
import "go.opentelemetry.io/otel/metric"  // Direct OTEL
func validate() error { /* 30 lines */ }  // Small functions
```

## 📦 Package Structure

```
pkg/
  ├── domain/          # Core types (ObserverEvent, NetworkEventData)
  ├── intelligence/    # Event enrichment (POLKU bridge, correlation)
  ├── integrations/    # POLKU, K8s clients
  └── interfaces/      # OTLP exporter, REST APIs

internal/
  ├── observers/       # eBPF observers (network, container, node)
  ├── runtime/         # Observer lifecycle (supervisor, emitters)
  └── services/        # K8s context, decoders
```

## 🎯 eBPF Development Pattern (Brendan Gregg Approach)

**MANDATORY**: Single eBPF program + Go processor chain

```
┌─────────────────────────────────────┐
│  eBPF (network_monitor.c)          │
│  - Captures TCP states, UDP, IPs   │
│  - NO parsing (just capture data)  │
└──────────┬──────────────────────────┘
           │ Ring Buffer
           ▼
┌─────────────────────────────────────┐
│  Go Userspace (processEventsStage) │
│  Processor Chain:                   │
│  1. LinkProcessor   → link_failure  │
│  2. DNSProcessor    → dns_query     │
│  3. StatusProcessor → connection    │
└─────────────────────────────────────┘
```

**Why**: eBPF parsing is 10x slower than Go, single program reduces kernel overhead, easier to test.

## 🔥 Current State (2024-11-30)

### ✅ Production Ready
- Supervisor with health monitoring (PR #536 merged)
- eBPF observers: network, container (eBPF)
- Scheduler observer (Prometheus scraping)
- K8s context service (pod metadata enrichment for eBPF events)
- CI/CD with GitHub Actions
- ZERO map[string]interface{} violations (2 test exceptions)

**Note**: K8s API watching (deployments, pods, nodes, services) moved to **PORTTI**.

### 🚧 In Progress
- **Intelligence Service** - POLKU gRPC publisher with batching, backpressure, TLS
  - Design: docs/designs/intelligence-service-foundation.md
  - Status: Implemented (polkuService + publisher)

- **Observer Runtime Refactor** (ADR 009) - Unified infrastructure
  - Status: In progress

### 📈 Quality Metrics
- map[string]interface{}: **2** (test exceptions only) ✅
- Test Coverage: Variable per package (no enforcement)
- All PRs: TDD workflow, <30 line commits
- Architecture: 5-level hierarchy enforced

## 🔥 Remember

> "eBPF captures, Go parses. One program, many processors."

> "Format, vet, lint - every single commit"

> "Architecture hierarchy is law. No circular dependencies."

---

**False Systems** 🇫🇮
