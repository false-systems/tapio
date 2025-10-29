# Design Doc 004: WASM Observer Research & Foundations

**Status**: Research Complete
**Date**: 2025-01-25
**Authors**: Yair + Claude (AI pair programming)
**Context**: Market research and technical foundations for WASM observability
**Related**: ADR 002 (Observer Consolidation), Doc 003 (Network Observer)

---

## Executive Summary

WebAssembly (WASM) on Kubernetes is in **early adoption phase** (2024-2025) with <1,000 production clusters, but showing **compelling ROI** (ZEISS: 60% cost reduction, 10x density). This document captures comprehensive research on WASM technology, market timing, and architectural foundations for building a Tapio WASM observer.

**Key Decision**: Build WASM observer as **Tapio v1.1 feature** (post-launch), not separate product. Effort: 4 weeks. Market timing: 2-3 years early for mass adoption, but **first-mover advantage** outweighs risk.

---

## Table of Contents

1. [Market Analysis](#market-analysis)
2. [WASM Technology Fundamentals](#wasm-technology-fundamentals)
3. [WASM on Kubernetes Architecture](#wasm-on-kubernetes-architecture)
4. [SpinKube Platform Deep Dive](#spinkube-platform-deep-dive)
5. [Observability Gap Analysis](#observability-gap-analysis)
6. [Strategic Positioning](#strategic-positioning)
7. [Implementation Readiness](#implementation-readiness)

---

## 1. Market Analysis

### 1.1 Adoption Metrics (2024)

**CNCF Survey Data**:
| Metric | 2022 | 2023 | Trend |
|--------|------|------|-------|
| No WASM experience | 50% | **57%** | ⚠️ Declining adoption awareness |
| Using WASM | ~15-20% | ~12-15% | ⚠️ Shrinking user base |
| Can deploy <1 month | N/A | 71% | ✅ Among adopters |
| Can deploy <5 days | N/A | 23% | ✅ Among adopters |

**Platform Statistics**:
- **Fermyon Spin**: 250,000+ downloads
- **SpinKube operator**: 80,000+ downloads (launched March 2024)
- **Production deployments**: <10 confirmed companies

**Reality**: Market is **2-3 years premature** for mass adoption, but early indicators are strong.

---

### 1.2 Production Use Cases (Confirmed)

| Company | Industry | Use Case | Results |
|---------|----------|----------|---------|
| **ZEISS Group** | Manufacturing | Batch order processing (10K orders/day) | **60% cost reduction**, 43% perf gain |
| **Orange Telecom** | Telecom | Edge deployment (184 PoPs, 31 countries) | Pilot with wasmCloud |
| **SigScale** | Telecom | Telecom workloads | Production (wasmCloud) |
| **MachineMetrics** | Manufacturing | Industrial IoT | Production (wasmCloud) |
| **Adobe** | Enterprise Software | K8s + wasmCloud integration | Experimental |

**Total confirmed production users**: 3-5 companies

**Key insight**: ZEISS 60% cost reduction is REAL ROI, not hype.

---

### 1.3 TAM (Total Addressable Market)

**Current TAM (2024)**:
```
Kubernetes clusters: 5.6M
Running WASM: <1,000 (<0.02%)
Willing to pay for observability: ~100 clusters

TAM = 100 × $20K/year = $2M/year
```
**NOT venture-scale today**

**Projected TAM (2027)** (assuming 10% K8s adoption):
```
Kubernetes clusters: 7M
Running WASM: 700K (10%)
Willing to pay: 70K clusters

TAM = 70K × $20K = $1.4B/year
```
**Venture-scale in 3 years**

---

### 1.4 Competitive Landscape

**Who's NOT building WASM observability**:
- Datadog: ❌ No WASM support
- New Relic: ❌ No WASM support
- Dynatrace: ❌ No WASM support
- Grafana Beyla: ❌ No WASM support

**Why**: Market too early, not profitable yet

**Who IS building adjacent tools**:
- **Dylibso Observe**: Application-level tracing (SDK-based, no K8s context)
- **Fermyon**: Spin platform (basic metrics endpoint, no observability product)
- **Cosmonic**: wasmCloud platform (basic monitoring, not K8s-specific)

**Gap**: NO ONE is building infrastructure-level WASM on K8s observability

**Tapio would be FIRST**.

---

### 1.5 Strategic Timing

**Historical Precedent - Datadog & Kubernetes (2014-2018)**:
```
2014: K8s launched (<1,000 clusters)
├── Datadog ships K8s integration (FIRST)
├── Competitors: "Too early, market too small"
└── Result (2018): Datadog dominates K8s observability, IPO $8B

Timeline to mainstream:
2014 - Launch (early adopters)
2016 - 10K clusters (acceleration)
2018 - 100K+ clusters (mainstream)
```

**WASM on K8s (2024-2028)**:
```
2024: SpinKube launched (<1,000 clusters)
├── Tapio ships WASM observer (FIRST)
├── Competitors: "Too early, market too small"
└── Projected (2027): Tapio leads WASM observability market

Timeline:
2024 - Launch (early adopters) ← WE ARE HERE
2026 - 10K clusters (acceleration)
2028 - 100K+ clusters (mainstream)
```

**Same pattern, 10 years later**.

**Recommendation**: Build NOW to capture first-mover advantage before Datadog/Grafana enter.

---

## 2. WASM Technology Fundamentals

### 2.1 What is WebAssembly?

**Definition**: Binary instruction format for a stack-based virtual machine

```
Traditional:
Source (C/Rust/Go) → Native Binary (x86/ARM) → OS-specific

WebAssembly:
Source (C/Rust/Go) → WASM Binary (.wasm) → Runs ANYWHERE
```

**Key Properties**:
- **Binary format**: Compact, optimized for fast parsing
- **Stack-based VM**: Like JVM, but simpler and faster
- **Near-native speed**: 10-20% slower than native (JIT/AOT compilation)
- **Sandboxed**: Memory-safe, can't access host by default
- **Language-agnostic**: Compile from 40+ languages

**Performance**:
- **Startup time**: <1ms (vs 100-1000ms for containers)
- **Memory footprint**: 10-50MB (vs 512MB+ for containers)
- **Binary size**: 1-5MB (vs 100-500MB container images)

---

### 2.2 WASI: WebAssembly System Interface

**Problem**: WASM is sandboxed - can't access filesystem, network, env vars

**Solution**: WASI provides standardized "syscall" interface

```
┌─────────────────────────────────────┐
│   WASM Module (your code)          │
│   fn main() {                       │
│     let file = open("/tmp/data")    │ ← WASI call
│   }                                 │
└──────────────┬──────────────────────┘
               │ WASI API
               ▼
┌─────────────────────────────────────┐
│   WASM Runtime (Wasmtime/WasmEdge) │
│   Provides capabilities:            │
│   - Filesystem access               │
│   - Network sockets                 │
│   - Environment variables           │
│   - Random numbers                  │
└─────────────────────────────────────┘
```

**Capability-based Security**:
```bash
# WASM can ONLY access what you explicitly grant
wasmtime run app.wasm --dir=/tmp      # Only /tmp access
wasmtime run app.wasm --env=DATABASE  # Only DATABASE env var
```

**WASI Versions**:
- **Preview 1**: Basic filesystem, networking
- **Preview 2 (0.2)** (current - 2024): Component Model support
- **Preview 3** (future): Async communication

---

### 2.3 Component Model: "Microservices for WASM"

**Problem**: WASM modules can't communicate (isolated, different languages)

**Solution**: Component Model = Standard composition framework

```
┌────────────────────────────────────────┐
│  WASM Component                        │
│  ┌──────────┐  ┌──────────┐  ┌──────┐ │
│  │  Auth    │→ │  Cart    │→ │ Pay  │ │
│  │ (Rust)   │  │  (Go)    │  │ (C++) │
│  └──────────┘  └──────────┘  └──────┘ │
│         ▲                              │
│         └── WIT interface (typed)      │
└────────────────────────────────────────┘
```

**WIT (WebAssembly Interface Types)** - IDL for WASM:
```wit
// auth.wit - Interface definition
interface auth {
  validate-token: func(token: string) -> result<user, error>
}

// Rust implements this interface
// Go calls this interface
// Type-safe, cross-language!
```

**Status (2024)**:
- WASI 0.2 released (incorporates Component Model)
- SpinKube supports Component Model
- Dylibso: "Component Model support coming soon"

**Implication for Tapio**: Future feature - observe inter-component calls

---

## 3. WASM on Kubernetes Architecture

### 3.1 The Full Stack

```
┌─────────────────────────────────────────────────────┐
│         Kubernetes Control Plane                     │
│  "Schedule this WASM pod to a WASM-capable node"   │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │   RuntimeClass       │
        │  handler: wasmtime   │
        │  nodeSelector:       │
        │    kwasm: enabled    │
        └──────────┬───────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│         Worker Node (WASM-enabled)                  │
│  ┌───────────────────────────────────────────────┐  │
│  │  Kubelet (K8s agent)                          │  │
│  │  "Start this WASM pod"                        │  │
│  └────────────────┬──────────────────────────────┘  │
│                   │ CRI                              │
│                   ▼                                  │
│  ┌───────────────────────────────────────────────┐  │
│  │  containerd (high-level runtime)              │  │
│  │  "Which shim for this pod?"                   │  │
│  └────────────────┬──────────────────────────────┘  │
│                   │                                  │
│                   ├─ runc shim (containers)         │
│                   │                                  │
│                   └─ runwasi shim (WASM) ← NEW!     │
│                      ▼                               │
│  ┌───────────────────────────────────────────────┐  │
│  │  containerd-shim-wasmtime-v1                  │  │
│  │  (or wasmedge, spin, lunatic)                 │  │
│  └────────────────┬──────────────────────────────┘  │
│                   │                                  │
│                   ▼                                  │
│  ┌───────────────────────────────────────────────┐  │
│  │  WASM Runtime (Wasmtime/WasmEdge)             │  │
│  │  ┌──────────────────────────────────────────┐ │  │
│  │  │  app.wasm (WASI application)             │ │  │
│  │  └──────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

**Key insight**: Kubernetes thinks it's running a container, but it's actually running WASM!

---

### 3.2 RuntimeClass: The Detection Mechanism

**Without RuntimeClass** (fails):
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: wasm-app
spec:
  containers:
  - name: app
    image: ghcr.io/myapp:wasm  # ❌ K8s tries runc, fails
```

**With RuntimeClass** (works):
```yaml
# Step 1: Define RuntimeClass
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: wasmtime-spin-v2
handler: spin  # ← Tells containerd to use spin shim
scheduling:
  nodeSelector:
    kwasm.sh/kwasm-node: "true"  # ← Only WASM nodes

---
# Step 2: Use in Pod
apiVersion: v1
kind: Pod
metadata:
  name: wasm-app
spec:
  runtimeClassName: wasmtime-spin-v2  # ← Detection point!
  containers:
  - name: app
    image: ghcr.io/myapp:wasm
```

**Tapio detection strategy**: Watch pods with `runtimeClassName` matching WASM patterns:
- `wasmtime-*`
- `wasmedge-*`
- `spin-*`
- `lunatic-*`

---

### 3.3 runwasi: The Bridge

**What runwasi does**:
```
containerd: "Run this OCI image"
runwasi: "Is this WASM or Linux binary?"

If WASM:
├── Extract .wasm from OCI image
├── Call Wasmtime/WasmEdge to execute
├── Provide WASI capabilities
└── Report status to containerd

If Linux:
├── Fall back to youki/crun
└── Run as normal container
```

**Dual execution mode**: Same node can run containers + WASM side-by-side!

**runwasi Architecture**:
```rust
// Engine trait - Core abstraction
trait Engine {
    fn run_wasm(module: &[u8], wasi_ctx: WasiCtx) -> Result<()>;
    fn run_container(image: OciImage) -> Result<()>;
}

// Auto-detection
if is_wasm_module(image) {
    engine.run_wasm(extract_wasm(image), wasi_ctx)
} else {
    engine.run_container(image)
}
```

**Tapio implication**: Can't assume all pods on "WASM node" are WASM - must check `runtimeClassName`.

---

## 4. SpinKube Platform Deep Dive

### 4.1 Spin Framework: "Serverless for WASM"

**Architecture**:
```
Spin = Event-driven framework for WASM

Event → Trigger → WASM Component → Response

Triggers:
├── HTTP (incoming request)
├── Redis (pub/sub message)
├── Cron (timer)
└── Custom (plugins)
```

**Spin Application Structure**:
```
my-spin-app/
├── spin.toml           # ← Manifest
├── cart/
│   └── cart.wasm      # ← Compiled from Rust/Go/TS
├── auth/
│   └── auth.wasm
└── payment/
    └── payment.wasm
```

**spin.toml** (manifest):
```toml
spin_manifest_version = 2

[application]
name = "ecommerce"
version = "1.0.0"

# HTTP trigger for cart
[[trigger.http]]
route = "/cart/..."
component = "cart"

[component.cart]
source = "cart/cart.wasm"
allowed_outbound_hosts = ["https://api.stripe.com"]

# HTTP trigger for auth
[[trigger.http]]
route = "/auth/..."
component = "auth"

[component.auth]
source = "auth/auth.wasm"
```

**HTTP routing (in WASM runtime)**:
```
Request: GET /cart/123
└── Spin framework parses route
    └── Matches "/cart/..." → cart.wasm
        └── Executes cart.wasm
            └── Returns response (<1ms!)
```

---

### 4.2 SpinKube Components

```
┌─────────────────────────────────────────────┐
│          SpinKube Platform                  │
├─────────────────────────────────────────────┤
│                                             │
│  1. Spin Operator                          │
│     - Watches SpinApp CRDs                 │
│     - Creates: Deployment, Service, Pods   │
│                                             │
│  2. containerd-shim-spin                   │
│     - Executes .wasm via Wasmtime          │
│     - Provides WASI capabilities           │
│                                             │
│  3. Runtime Class Manager                   │
│     - Installs shim on nodes               │
│     - Labels nodes: kwasm.sh/kwasm-node    │
│                                             │
└─────────────────────────────────────────────┘
```

---

### 4.3 SpinApp CRD (Custom Resource)

**Instead of raw Pod**:
```yaml
apiVersion: v1
kind: Pod
spec:
  runtimeClassName: wasmtime-spin-v2
  containers:
  - name: cart
    image: ghcr.io/shop/cart:v1
```

**SpinKube users write**:
```yaml
apiVersion: core.spinoperator.dev/v1alpha1
kind: SpinApp
metadata:
  name: ecommerce
spec:
  image: ghcr.io/shop/ecommerce:v1
  executor: containerd-shim-spin
  replicas: 100  # ← 100 replicas!

  resources:
    limits:
      memory: 50Mi  # ← Tiny!
      cpu: 50m

  env:
  - name: DATABASE_URL
    value: "postgres://..."
```

**Spin Operator reconciliation**:
```
SpinApp created
└── Operator watches event
    └── Creates Deployment (100 replicas)
    └── Creates Service (exposes HTTP routes)
    └── Schedules Pods to WASM nodes (RuntimeClass)
    └── containerd-shim-spin executes WASM
```

**Tapio must watch**: `SpinApp` CRDs, not just Pods!

---

### 4.4 Density: The 250 Apps/Node Reality

**ZEISS Demo** (KubeCon 2024):
- Node: 2 vCPU, 4GB RAM
- WASM apps: 250 SpinApps
- Memory per app: 50Mi × 250 = 12.5GB
- **Wait, that's 3x the node capacity?**

**Explanation**: WASM apps are **sparse** - most memory is allocated but not used:
```
Allocated: 50Mi (limit in SpinApp spec)
Resident:  5-10Mi (actual usage)

250 apps × 10Mi = 2.5GB actual usage (fits in 4GB node)
```

**Implication for Tapio**:
- Can't rely on `limits` alone
- Must track **actual memory usage** (from cgroups/metrics-server)
- Detect when apps approach their limits (90%+)

---

## 5. Observability Gap Analysis

### 5.1 Current State (What Exists)

| Tool | Type | Observes | Limitations |
|------|------|----------|-------------|
| **Dylibso Observe** | SDK | Function calls, memory allocation | ❌ No K8s context, requires SDK |
| **WASI-OTEL** | Standard | Proposed OTEL integration | ❌ Not implemented yet |
| **kubectl logs** | K8s native | Stdout/stderr | ❌ No WASM-specific insights |
| **Prometheus** | Metrics | Spin metrics (if exposed) | ❌ Manual setup, no auto-discovery |

**Gap**: **ZERO** infrastructure-level observability

---

### 5.2 What's Missing (Tapio's Opportunity)

**Questions no one can answer today**:
1. ❌ "Which WASM apps are running on which nodes?"
2. ❌ "Why did my SpinApp get OOM killed?"
3. ❌ "Which node has 250 apps (max density)?"
4. ❌ "Why can't my SpinApp schedule?" (no WASM node)
5. ❌ "Correlate WASM failure with K8s events"
6. ❌ "Resource consumption per WASM component"

**Tapio would be the FIRST to answer these**.

---

### 5.3 Observability Challenges

**Challenge 1: WASM components invisible to K8s**
```yaml
# K8s sees this
apiVersion: v1
kind: Pod
spec:
  containers:
  - image: ghcr.io/ecommerce:v1  # ← Black box!

# Actually contains
# - cart.wasm (consuming 30Mi)
# - auth.wasm (consuming 10Mi)
# - payment.wasm (consuming 5Mi)
```

**Tapio must**:
- Parse spin.toml from OCI image
- Map resource usage to components

**Challenge 2: No standard metrics**
```toml
# Optional in spin.toml
[observability]
metrics_exporter = "prometheus"  # ← May not exist!
metrics_port = 9091
```

**Tapio must**:
- Detect if endpoint exists
- Gracefully degrade if missing

**Challenge 3: OCI image format**
```
WASM app packaged as OCI:
ghcr.io/shop/app:v1
├── manifest.json
└── layers/
    ├── spin.toml.tar.gz
    ├── cart.wasm.tar.gz
    └── auth.wasm.tar.gz
```

**Tapio must**:
- Pull/read from containerd cache
- Extract and parse spin.toml

---

## 6. Strategic Positioning

### 6.1 Tapio vs Dylibso (Complementary)

| Layer | Tool | Observes | Use Case |
|-------|------|----------|----------|
| **Application** | Dylibso Observe | Function calls, memory allocation | "Debug my WASM code" |
| **Infrastructure** | Tapio WASM Observer | Pod lifecycle, K8s context, density | "Why did my WASM infrastructure fail?" |
| **Correlation** | Tapio Ahti (ENTERPRISE) | Cross-layer root cause | "What caused the cascading failure?" |

**Joint value prop**:
> "Dylibso shows WHAT your WASM code does. Tapio shows WHY your WASM infrastructure fails. Together: Complete observability."

**Partnership opportunity**: Co-marketing with Dylibso

---

### 6.2 First-Mover Advantage

**Historical pattern (Datadog + K8s)**:
```
2014: Datadog ships K8s support (first)
2015: Adoption accelerates
2016: Competitors catch up (too late)
2018: Datadog IPO ($8B, K8s story central)
```

**WASM trajectory (projected)**:
```
2024: Tapio ships WASM support (first)
2025: SpinKube grows (5K clusters)
2026: Adoption accelerates (50K clusters)
2027: Datadog enters market (too late - Tapio already standard)
2028: Tapio = "WASM observability platform"
```

**Risk is LOW** (4 weeks investment), **upside is HIGH** (market leadership).

---

### 6.3 Platform Positioning

**Without WASM**:
```
Investor: "What's Tapio?"
You: "Kubernetes observability"
Investor: "Like Datadog?"
You: "Yes, but diagnostic-first"
Investor: "Pass. Crowded market."
```

**With WASM**:
```
Investor: "What's Tapio?"
You: "Cloud-native observability platform - K8s + WASM"
Investor: "WASM observability? No one does that."
You: "Exactly. ZEISS case study: 60% cost reduction."
Investor: "Tell me more..." 💰
```

**Platform > Point solution**

---

## 7. Implementation Readiness

### 7.1 What Tapio Needs to Observe

```
┌─────────────────────────────────────────┐
│   Tapio WASM Observer                  │
├─────────────────────────────────────────┤
│                                         │
│  1. SpinApp Lifecycle                  │
│     - Watch SpinApp CRDs               │
│     - Track replicas, scaling          │
│                                         │
│  2. WASM Pod Detection                 │
│     - Watch pods with WASM runtime     │
│     - Detect OOM, failures             │
│                                         │
│  3. Node Density Tracking              │
│     - Count WASM pods per node         │
│     - Alert >200 apps/node             │
│                                         │
│  4. Resource Attribution               │
│     - Parse spin.toml                  │
│     - Map usage to components          │
│                                         │
│  5. Metrics Scraping (optional)        │
│     - Spin Prometheus endpoint         │
│     - HTTP rates, latencies            │
│                                         │
│  6. K8s Context Enrichment             │
│     - Via Context Service              │
│     - Add ns, deployment, labels       │
│                                         │
└─────────────────────────────────────────┘
```

---

### 7.2 Event Schema (Proposed)

```go
// pkg/domain/events.go
type WasmEventData struct {
    // Runtime
    Runtime      string // spin, wasmtime, wasmedge
    RuntimeClass string

    // SpinApp (if using SpinKube)
    SpinAppName  string
    ComponentName string
    Executor     string // containerd-shim-spin

    // K8s context (from Context Service)
    PodName        string
    Namespace      string
    DeploymentName string
    Labels         map[string]string

    // Resources
    MemoryLimit   string
    MemoryUsage   string
    MemoryPercent float64
    CPULimit      string
    CPUUsage      string

    // Density (unique to WASM!)
    Replicas    int64
    NodeName    string
    AppsOnNode  int     // Total WASM apps on node
    NodeDensity float64 // % of 250 max

    // Lifecycle
    Phase        string
    RestartCount int32
    ExitCode     int32

    // Metrics (from Spin)
    Metrics map[string]float64
}

// Event subtypes
const (
    WasmSpinAppDeployed  = "spinapp_deployed"
    WasmSpinAppScaled    = "spinapp_scaled"
    WasmComponentStarted = "component_started"
    WasmComponentOOM     = "component_oom"
    WasmResourceAnomaly  = "resource_anomaly"  // >90% limit
    WasmHighDensity      = "high_density"      // >200 apps
    WasmSchedulingFailed = "scheduling_failed"
)
```

---

### 7.3 Implementation Complexity

**Easy** (1-2 weeks):
- ✅ SpinApp CRD watching (dynamic client)
- ✅ Pod detection (runtimeClassName filter)
- ✅ K8s context enrichment (via Context Service)
- ✅ Basic lifecycle events

**Medium** (1 week):
- 🟡 Density tracking (count pods per node)
- 🟡 Resource anomaly detection (>90% usage)
- 🟡 Spin metrics scraping (Prometheus client)

**Hard** (1 week):
- 🔴 OCI image parsing (extract spin.toml)
- 🔴 Component-level attribution
- 🔴 Inter-component call tracing (future - Component Model)

**Total effort**: 4 weeks (feasible for v1.1)

---

### 7.4 Technical Dependencies

**Required**:
- K8s dynamic client (SpinApp CRDs)
- Prometheus client (metrics scraping)
- TOML parser (spin.toml)
- Context Service (K8s enrichment)

**Optional** (nice-to-have):
- OCI client (containerd image cache)
- Component Model support (future)

**No new eBPF programs needed** - Pure K8s API + metrics scraping!

---

## 8. Recommendations

### 8.1 Strategic Decision

**Build WASM observer as Tapio v1.1 feature** (not separate product)

**Rationale**:
1. Market is 2-3 years early (TAM $2M today, $1.4B in 2027)
2. First-mover advantage outweighs risk (Datadog/K8s precedent)
3. Low implementation cost (4 weeks)
4. Validates platform architecture (not just K8s tool)
5. Partnership opportunities (Fermyon, SpinKube, ZEISS)

---

### 8.2 Timeline

```
2025 Q1-Q3: Ship Tapio v1.0 (K8s observers)
            Focus: 5.6M cluster market

2025 Q4:    Add WASM observer (v1.1)
            Effort: 4 weeks
            Users: 10-20 early adopters

2026-2027:  Market matures
            Tapio = WASM observability standard
            Competitors enter (too late)
```

---

### 8.3 Success Metrics

**Technical**:
- SpinApp lifecycle tracking
- Density alerts (>200 apps/node)
- Resource anomaly detection
- K8s context enrichment
- Spin metrics scraping

**Business**:
- 10+ SpinKube users by EOY 2025
- ZEISS partnership/case study
- KubeCon talk acceptance
- CNCF blog post
- Fermyon integration partnership

---

## 9. References

### Market Research
- CNCF Annual Survey 2023/2024
- Fermyon SpinKube announcement (March 2024)
- ZEISS cost reduction case study (KubeCon EU 2024)
- wasmCloud adoption blog (Oct 2024)

### Technical Documentation
- WebAssembly Specification (webassembly.github.io)
- WASI Documentation (wasi.dev)
- SpinKube Architecture (spinkube.dev/docs/topics/architecture)
- runwasi Architecture (runwasi.dev/developer/architecture)

### Industry Analysis
- "WebAssembly: Still waiting for its moment" (LeadDev)
- "Server-side Wasm to-do list lengthens for 2024" (TechTarget)
- Datadog K8s integration history (2014-2018)

---

## Appendix A: WASM vs Containers

| Aspect | Containers | WASM |
|--------|-----------|------|
| **Startup** | 100-1000ms | <1ms |
| **Memory** | 512MB+ | 10-50MB |
| **Image size** | 100-500MB | 1-5MB |
| **Density** | 20/node | 250/node |
| **Security** | OS-level isolation | Sandboxed VM |
| **Portability** | Linux-specific | Runs anywhere |

---

## Appendix B: Glossary

- **WASM**: WebAssembly - Binary instruction format
- **WASI**: WebAssembly System Interface - Syscall API
- **Component Model**: Multi-module composition framework
- **WIT**: WebAssembly Interface Types - IDL
- **runwasi**: containerd shim for WASM runtimes
- **Spin**: Fermyon's serverless WASM framework
- **SpinKube**: Kubernetes platform for Spin apps
- **SpinApp**: Kubernetes CRD for Spin applications
- **RuntimeClass**: K8s mechanism for alternate runtimes

---

**End of Document**
