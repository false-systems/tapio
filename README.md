# Tapio - Observability for Small Teams

> "In the midst of winter, I found there was, within me, an invincible summer." — Albert Camus

## What is Tapio?

Tapio is an observability platform designed for small engineering teams who find enterprise solutions overwhelming and expensive. Built from experience managing Kubernetes clusters at scale, we're creating a simpler path to understanding what's happening in your infrastructure.

**Status: Under active development. Not production-ready.**

## The Problem

If you're a small team running Kubernetes, you've likely experienced this:
- Datadog/New Relic costs are expensive.
- Setting up Prometheus + Grafana + Loki + Tempo + Jaeger requires a dedicated SRE
- You're drowning in metrics but still can't answer "why is production slow?"
- Your dashboards look impressive, but they don't help during incidents

We've been there. After years of building and operating cloud-native systems, we're building what we wished existed: observability that just works, without the complexity.

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                   Your Kubernetes Cluster                   │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
│  │  Pods   │  │Services │  │ Nodes   │  │  etcd   │       │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘       │
│       │            │            │            │              │
└───────┼────────────┼────────────┼────────────┼──────────────┘
        │            │            │            │
        ▼            ▼            ▼            ▼
┌─────────────────────────────────────────────────────────────┐
│                 Tapio Collectors (Level 1)                 │
│                                                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │   eBPF   │ │   CRI    │ │ Kubelet  │ │   DNS    │      │
│  │ Kernel   │ │Container │ │   API    │ │ Monitor  │      │
│  └─────┬────┘ └─────┬────┘ └─────┬────┘ └─────┬────┘      │
│        │            │            │            │             │
│        └────────────┴────────────┴────────────┘             │
│                           │                                 │
│                           ▼                                 │
│                 ┌──────────────────┐                       │
│                 │  Unified Events  │                       │
│                 └────────┬─────────┘                       │
└──────────────────────────┼─────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│             Intelligence Layer (Level 2)                   │
│                                                             │
│     ┌─────────────────────────────────────────┐            │
│     │      Correlation & Root Cause Engine    │            │
│     │   "DNS → etcd → CRI → Pod restart"      │            │
│     └───────────────────┬─────────────────────┘            │
│                         │                                   │
└─────────────────────────┼───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                Storage Layer (Level 3)                     │
│                                                             │
│     ┌─────────────────────────────────────────┐            │
│     │        Neo4j Graph Database             │            │
│     │    "Everything is connected"            │            │
│     └───────────────────┬─────────────────────┘            │
└─────────────────────────┼───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                     Your Team                               │
│                                                             │
│     "Oh, the DNS resolver in pod-xyz is failing            │
│      because etcd is under pressure from CRI events"       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## The Collectors

We gather telemetry from multiple sources to build a complete picture:

### **Kernel & System Level**
- **Kernel Collector** (`/pkg/collectors/kernel`) - eBPF-based system call, file operations, and process lifecycle tracking
- **Storage I/O Collector** (`/pkg/collectors/storage-io`) - VFS-level storage operations, latency, and performance bottlenecks  
- **Syscall Errors Collector** (`/pkg/collectors/syscall-errors`) - System call error tracking and failure analysis
- **OOM Collector** (`/pkg/collectors/oom`) - Out-of-memory events and memory pressure detection

### **Container & Namespace**  
- **CRI Collector** (`/pkg/collectors/cri`) - Container Runtime Interface for lifecycle and resource monitoring
- **CRI-eBPF Collector** (`/pkg/collectors/cri-ebpf`) - Deep container runtime visibility using eBPF probes
- **Namespace Collector** (`/pkg/collectors/namespace-collector`) - Network namespace monitoring and container networking

### **Kubernetes & Orchestration**
- **Kubelet Collector** (`/pkg/collectors/kubelet`) - Pod phases, conditions, and resource allocation vs usage
- **KubeAPI Collector** (`/pkg/collectors/kubeapi`) - Kubernetes API server events and cluster state changes

### **Networking**
- **DNS Collector** (`/pkg/collectors/dns`) - Because DNS is always the problem. Resolution times, failures, and patterns
- **Network Collector** (`/pkg/collectors/network`) - L3/L4/L7 network intelligence with eBPF-based traffic analysis

### **Service Discovery & Coordination**
- **etcd-API Collector** (`/pkg/collectors/etcd-api`) - etcd API-level monitoring  
- **etcd-eBPF Collector** (`/pkg/collectors/etcd-ebpf`) - Deep etcd performance monitoring with eBPF

### **System Services**
- **Systemd Collector** (`/pkg/collectors/systemd`) - eBPF-based systemd service monitoring and journal analysis
- **Systemd-API Collector** (`/pkg/collectors/systemd-api`) - Systemd D-Bus API monitoring for service lifecycle

### **Observability**
- **OTEL Collector** (`/pkg/collectors/otel`) - OpenTelemetry integration for metrics, traces, and logs

Each collector implements the same interface and outputs unified events that flow into our correlation engine. No vendor lock-in, no complex configuration - just plug and observe.

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

5. **eBPF Native**: Deep visibility without overhead. We see everything from kernel syscalls to Kubernetes API calls.

## Our Philosophy

### Motorcycle vs. Car

Observability tools have become like modern cars - packed with features you'll never use, expensive to maintain, and requiring specialized knowledge to operate.

Tapio is more like a motorcycle:
- **Simpler**: One engine (correlation), not twelve different systems
- **Focused**: Built for the journey, not the parking lot
- **Built by riders**: We've been on-call at 3am debugging production issues

### Small Teams, Big Problems

We're not building the next Datadog. We're building what small teams need:
- **Clarity in chaos** - When everything is broken, show us the one thing to fix first
- **Context over metrics** - Don't show us CPU graphs, tell us why the CPU is high
- **Speed over features** - Get to root cause in seconds, not hours

### Real-World Engineering

- **No TODOs in production code** - If it's not done, it's not shipped
- **No `interface{}` abuse** - Type safety prevents midnight debugging sessions  
- **No magic** - eBPF and graph databases, not "AI-powered insights"
- **No vendor lock-in** - Standard protocols, open interfaces

## Current State

### What Works
- All 18 collectors compile and run ✅
- eBPF-based kernel, container, and network monitoring ✅
- Unified event schema across all telemetry sources ✅
- Basic correlation engine for root cause analysis ✅
- Type-safe CollectorEvent architecture (zero map[string]interface{}) ✅
- Direct OpenTelemetry instrumentation (no wrappers) ✅

### Being Built
- Neo4j integration for relationship mapping
- API layer for queries and insights
- Advanced correlation algorithms
- Web UI for investigation workflows


## Getting Started

**Warning**: This is under active development. For production use today, we recommend:
- Small teams: Grafana Cloud
- Cost-conscious: VictoriaMetrics + Grafana
- Enterprise: Keep paying for Datadog

If you want to contribute or try early builds:

```bash
# Clone the repository
git clone https://github.com/yairfalse/tapio

# Build all collectors
cd pkg/collectors
go build ./...

# Run tests
go test ./... -race

# Check architecture compliance
make verify

# Generate eBPF programs (Linux only)
go generate ./...
```

## Contributing

We follow strict code standards (see CLAUDE.md):
- No TODOs, no `interface{}`, no shortcuts
- 80% test coverage minimum
- Architecture hierarchy compliance
- All eBPF programs must compile on multiple kernel versions

If you've operated production systems and felt the pain, we'd love your help.

## License

Apache 2.0 - Because observability should be open.

---

*"Simplicity is the ultimate sophistication" - Leonardo da Vinci*

*Built with experience from managing thousands of pods, millions of requests, and too many 3am incidents.*

*"In the depth of winter, I finally learned that there was in me an invincible summer." - Albert Camus*
