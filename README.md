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
- You see network spikes but can't tell which pod is responsible
- OOM kills happen but you find out hours later from angry users

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

We're building collectors that gather telemetry from multiple sources. Most now use a base architecture for consistency.

### **Working Towards: PID → Container → Pod Correlation**

We're implementing the foundation to connect:
```
Network Event (PID 1234) → Which Container? → Which Pod?
```

The **CRI-eBPF collector** can track PIDs to containers, and we're working on integrating this with other collectors.

### **Kernel & System Level**
- **Kernel Collector** - eBPF syscalls, file ops, process lifecycle. Base architecture with ring buffers.
- **Storage I/O Collector** - VFS operations and latency. Migrated to base package for consistent metrics.
- **Syscall Errors Collector** - System call failures with automatic error rate tracking
- **Resource Starvation** - CPU throttling and scheduling delays that actually impact performance
- **Memory Leak Hunter** - Track allocations and find leaks before they cause OOMs

### **Container & Runtime**  
- **CRI Collector** - Container lifecycle via CRI API
- **CRI-eBPF Collector** - Tracks processes to containers using eBPF
  - OOM detection
  - Container start with PID extraction from /proc
  - Process fork tracking
  - Memory pressure monitoring

### **Kubernetes & Orchestration**
- **Kubelet Collector** - Pod lifecycle, conditions, actual vs requested resources
- **KubeAPI Collector** - Cluster state with relationship tracking and trace management
  - Automatic relationship caching for performance
  - Resource trace management for debugging
  - Configurable namespace filtering

### **Networking**
- **DNS Collector** - DNS resolution monitoring and failure tracking
- **Network Collector** - L3/L4/L7 traffic monitoring with eBPF
- **Service Map Collector** - Service discovery and dependency mapping
  - Automatic service detection from K8s resources
  - Connection tracking between services
  - Service type identification (database, cache, API, etc.)

### **Runtime Signals**
- **Runtime Signals Collector** - Go runtime internals, GC pressure, goroutine leaks

### **System Services**
- **Systemd Collector** - Service lifecycle with journal integration
- **Systemd-API Collector** - D-Bus monitoring for service state changes

### **Observability**
- **OTEL Collector** - OpenTelemetry bridge for existing instrumentation

Each collector embeds `BaseCollector` for:
- Automatic health monitoring (degraded after timeout)
- Event/error/drop statistics
- OTEL metrics and tracing
- Consistent lifecycle management

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

## What We're Building

1. **For Small Teams**: Designed for teams of 3-20 engineers with moderate-scale deployments

2. **Event Correlation**: Working to connect kernel events to Kubernetes resources so you know which pod caused what

3. **Process Tracking**: Building PID to container mapping to eliminate blind spots in monitoring

4. **Fast Detection**: Using eBPF to detect issues like OOM kills faster than traditional polling

5. **Simple Queries**: Aiming to eliminate complex query languages - just ask questions in plain terms

6. **eBPF-Based**: Using eBPF where possible for low-overhead monitoring

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
- Most collectors compile and run
- Basic eBPF monitoring for kernel, container, and network events
- Unified event schema across collectors
- Type-safe CollectorEvent architecture
- OpenTelemetry instrumentation
- BaseCollector architecture for consistency
- Initial PID to container tracking implementation

### Being Built
- Integration between collectors for full correlation
- Neo4j integration for relationship mapping
- API layer for queries and insights
- Correlation algorithms
- Web UI for investigation workflows
- Production stability and testing


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
