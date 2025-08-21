# Tapio - Observability for Small Teams

> "In the midst of winter, I found there was, within me, an invincible summer." — Albert Camus

## What is Tapio?

Tapio is an observability platform designed for small engineering teams who find enterprise solutions overwhelming and expensive. Built from experience managing Kubernetes clusters at scale, we're creating a simpler path to understanding what's happening in your infrastructure.

**Status: Under active development. Not production-ready.**

## The Problem

If you're a small team running Kubernetes, you've likely experienced this:
- Datadog/New Relic costs more than your infrastructure
- Setting up Prometheus + Grafana + Loki + Tempo + Jaeger requires a dedicated SRE
- You're drowning in metrics but still can't answer "why is production slow?"
- Your dashboards look impressive but don't help during incidents

We've been there. After years of building and operating cloud-native systems, we're building what we wished existed: observability that just works, without the complexity.

## How It Works

┌─────────────────────────────────────────────────────────────┐
│                     Your Kubernetes Cluster                  │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
│  │  Pods   │  │Services │  │  Nodes  │  │   DNS   │       │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘       │
│       │            │            │            │              │
└───────┼────────────┼────────────┼────────────┼──────────────┘
        │            │            │            │
        ▼            ▼            ▼            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Tapio Collectors (Level 1)               │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
│  │  eBPF   │  │   CRI   │  │ Kubelet │  │   DNS   │       │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘       │
│       │            │            │            │              │
│       └────────────┴────────────┴────────────┘              │
│                         │                                    │
│                         ▼                                    │
│                 ┌──────────────┐                            │
│                 │ Unified Event│                            │
│                 └───────┬──────┘                            │
└─────────────────────────┼────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│               Intelligence Layer (Level 2)                   │
│                                                              │
│     ┌──────────────────────────────────────┐                │
│     │    Correlation & Root Cause Engine   │                │
│     └──────────────────┬────────────────────┘               │
│                        │                                     │
└────────────────────────┼─────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                Storage Layer (Level 3)                       │
│                                                              │
│     ┌──────────────────────────────────────┐                │
│     │         Neo4j Graph Database         │                │
│     └──────────────────┬────────────────────┘               │
└────────────────────────┼─────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                   Your Team                                  │
│                                                              │
│     "Oh, the DNS resolver in pod-xyz is failing             │
│      because the upstream service is throttling"             │
│                                                              │
└───────────────────────────────────────────────────────────────┘
```

## The Collectors

We gather telemetry from multiple sources to build a complete picture:

### 1. **Kernel Collector** (`/pkg/collectors/kernel`)
Tracks system calls, file operations, and process lifecycle using eBPF. Helps answer: "What is this container actually doing?"

### 2. **CRI Collector** (`/pkg/collectors/cri`)
Interfaces with the Container Runtime Interface to monitor container lifecycle, resource usage, and health. Knows when containers are OOMKilled before you do.

### 3. **Kubelet Collector** (`/pkg/collectors/kubelet`)
Pulls metrics directly from the kubelet API. Tracks pod phases, ready conditions, and resource allocation vs actual usage.

### 4. **DNS Collector** (`/pkg/collectors/dns`)
Because DNS is always the problem. Monitors resolution times, failures, and patterns. Correlates DNS issues with service degradation.

### 5. **Cgroup Collector** (`/pkg/collectors/cgroup`)
Reads cgroup metrics for accurate resource consumption. Shows you what's really using CPU/memory, not what Kubernetes thinks.

### 6. **eBPF Collector** (`/pkg/collectors/ebpf`)
Deep kernel-level visibility without overhead. Tracks network flows, security events, and performance bottlenecks.

### 7. **OpenTelemetry Collector** (`/pkg/collectors/otel`)
Ingests traces and metrics from your instrumented applications. Bridges application and infrastructure observability.

## Architecture

We follow a strict 5-level hierarchy to keep complexity manageable:

```
Level 0: Domain       - Core types and events (zero dependencies)
Level 1: Collectors   - Gather telemetry (depends on domain only)
Level 2: Intelligence - Correlation and analysis (depends on L0-L1)
Level 3: Integrations - Neo4j, storage (depends on L0-L2)
Level 4: Interfaces   - APIs and UI (depends on L0-L3)
```

This isn't architectural astronautics - it's how we prevent the codebase from becoming the spaghetti we're trying to debug.

## What Makes Tapio Different

1. **Built for Reality**: We're not trying to monitor Netflix. This is for teams of 3-20 engineers with 50-500 pods.

2. **Correlation First**: Instead of 30 dashboards, we focus on connecting the dots. When your API is slow, we tell you it's because DNS to your database is timing out.

3. **Resource Efficient**: Runs on a single node. Your observability shouldn't cost more than what you're observing.

4. **No Query Languages**: You shouldn't need to learn PromQL, KQL, or GraphQL to understand why production is down.

## Current State

### What Works
- eBPF-based kernel monitoring
- Container runtime integration
- Basic correlation engine
- Event unified schema

### Being Built
- Neo4j integration for relationship mapping
- API layer for queries
- Root cause analysis engine
- Automated remediation suggestions

### Won't Build
- Machine learning magic (it's usually linear regression anyway)
- Infinite retention (30 days is enough for most teams)
- Multi-region federation (we're not trying to be Thanos)
- Custom dashboarding (Grafana already exists)

## Getting Started

**Warning**: This is under active development. For production use today, we recommend:
- Small teams: Grafana Cloud
- Cost-conscious: VictoriaMetrics + Grafana
- Enterprise: Keep paying for Datadog

If you want to contribute or try early builds:
=======
- Datadog/New Relic are expensive and hard to set up.
- Setting up Prometheus + Grafana + Loki + Tempo + Jaeger requires a dedicated SRE
- You're drowning in metrics but still can't answer "why is production slow?"
- Your dashboards look impressive, but they don't help during incidents

We've been there. After years of building and operating cloud-native systems, we're building what we wished existed: observability that works, without the complexity.

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                     Your Kubernetes Cluster                  │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
│  │  Pods   │  │Services │  │  Nodes  │  │   DNS   │       │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘       │
│       │            │            │            │              │
└───────┼────────────┼────────────┼────────────┼──────────────┘
        │            │            │            │
        ▼            ▼            ▼            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Tapio Collectors (Level 1)               │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
│  │  eBPF   │  │   CRI   │  │ Kubelet │  │   DNS   │       │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘       │
│       │            │            │            │              │
│       └────────────┴────────────┴────────────┘              │
│                         │                                    │
│                         ▼                                    │
│                 ┌──────────────┐                            │
│                 │ Unified Event│                            │
│                 └───────┬──────┘                            │
└─────────────────────────┼────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│               Intelligence Layer (Level 2)                   │
│                                                              │
│     ┌──────────────────────────────────────┐                │
│     │    Correlation & Root Cause Engine   │                │
│     └──────────────────┬────────────────────┘               │
│                        │                                     │
└────────────────────────┼─────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                Storage Layer (Level 3)                       │
│                                                              │
│     ┌──────────────────────────────────────┐                │
│     │         Neo4j Graph Database         │                │
│     └──────────────────┬────────────────────┘               │
└────────────────────────┼─────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                   Your Team                                  │
│                                                              │
│     "Oh, the DNS resolver in pod-xyz is failing             │
│      because the upstream service is throttling"             │
│                                                              │
└───────────────────────────────────────────────────────────────┘
```

## The Collectors

We gather telemetry from multiple sources to build a complete picture:

### 1. **Kernel Collector** (`/pkg/collectors/kernel`)
Tracks system calls, file operations, and process lifecycle using eBPF. Helps answer: "What is this container actually doing?"

### 2. **CRI Collector** (`/pkg/collectors/cri`)
Interfaces with the Container Runtime Interface to monitor container lifecycle, resource usage, and health. Knows when containers are OOMKilled before you do.

### 3. **Kubelet Collector** (`/pkg/collectors/kubelet`)
Pulls metrics directly from the kubelet API. Tracks pod phases, ready conditions, and resource allocation vs actual usage.

### 4. **DNS Collector** (`/pkg/collectors/dns`)
Because DNS is always the problem. Monitors resolution times, failures, and patterns. Correlates DNS issues with service degradation.

### 5. **Cgroup Collector** (`/pkg/collectors/cgroup`)
Reads cgroup metrics for accurate resource consumption. Shows you what's really using CPU/memory, not what Kubernetes thinks.

### 6. **eBPF Collector** (`/pkg/collectors/ebpf`)
Deep kernel-level visibility without overhead. Tracks network flows, security events, and performance bottlenecks.

### 7. **OpenTelemetry Collector** (`/pkg/collectors/otel`)
Ingests traces and metrics from your instrumented applications. Bridges application and infrastructure observability.

## Architecture

We follow a strict 5-level hierarchy to keep complexity manageable:

```
Level 0: Domain       - Core types and events (zero dependencies)
Level 1: Collectors   - Gather telemetry (depends on domain only)
Level 2: Intelligence - Correlation and analysis (depends on L0-L1)
Level 3: Integrations - Neo4j, storage (depends on L0-L2)
Level 4: Interfaces   - APIs and UI (depends on L0-L3)
```

This isn't architectural astronautics - it's how we prevent the codebase from becoming the spaghetti we're trying to debug.

## What Makes Tapio Different

1. **Built for Reality**: We're not trying to monitor Netflix. This is for teams of 3-20 engineers with 50-500 pods.

2. **Correlation First**: Instead of 30 dashboards, we focus on connecting the dots. When your API is slow, we tell you it's because DNS to your database is timing out.

4. **No Query Languages**: You shouldn't need to learn PromQL, KQL, or GraphQL to understand why production is down.

## Current State

### What Works
- eBPF-based kernel monitoring
- Container runtime integration
- Basic correlation engine
- Event unified schema

### Being Built
- Neo4j integration for relationship mapping
- API layer for queries
- Root cause analysis engine
- Automated remediation suggestions


If you want to contribute or try early builds:

```bash
# Clone the repository
git clone https://github.com/yourusername/tapio

# Build collectors
cd pkg/collectors
go build ./...

# Run tests
go test ./... -race

# Check architecture compliance
make verify
```

## Philosophy

Observability tools have become like modern cars - packed with features you'll never use, expensive to maintain, and require specialized knowledge to operate. 

Tapio is more like a motorcycle - simpler, focused on what matters, and built by people who ride.

We're not building the next Datadog. We're building what small teams need: clarity in chaos, without the complexity.

```bash
# Clone the repository
git clone https://github.com/yourusername/tapio

# Build collectors
cd pkg/collectors
go build ./...

# Run tests
go test ./... -race

# Check architecture compliance
make verify
```

## Philosophy

Observability tools have become like modern cars - packed with features you'll never use, expensive to maintain, and require specialized knowledge to operate. 
We follow strict code standards (see CLAUDE.md). No TODOs, no `interface{}`, no shortcuts. If you've operated production systems and felt the pain, we'd love your help.

Tapio is more like a motorcycle - simpler, focused on what matters, and built by people who actually ride.

We're not building the next Datadog. We're building what small teams need: clarity in chaos, without the complexity.

## Contributing

We follow strict code standards (see CLAUDE.md). No TODOs, no `interface{}`, no shortcuts. If you've operated production systems and felt the pain, we'd love your help.

## License


Apache 2.0 - Because observability should be open.

---

*"Simplicity is the ultimate sophistication" - Leonardo da Vinci*

*Built with experience from managing thousands of pods, millions of requests, and too many 3am incidents.*

*"In the depth of winter, I finally learned that there was in me an invincible summer." - Albert Camus*
