# Tapio

> "Experience without theory is blind, but theory without experience is mere intellectual play." — Immanuel Kant

**Observability that understands your Kubernetes clusters, not just measures them.**

## The Idea

Most observability tools collect metrics and hope you can figure out what went wrong. Tapio watches the kernel and correlates events to tell you what actually happened.

When your service starts failing, you don't need another dashboard showing CPU is high. You need to know that the memory allocator is fragmenting, causing garbage collection storms, which trigger circuit breakers in your API gateway, leading to cascading timeouts across your service mesh.

That's correlation. That's understanding.

## Observer Architecture

```
                     🏢 Kubernetes Cluster
    ┌─────────────────────────────────────────────────────────┐
    │                                                         │
    │  📦 Pod            📦 Pod            📦 Pod             │
    │  api-gateway       redis-cache       worker-service     │
    │                                                         │
    └─────────────┬───────────────┬───────────────┬───────────┘
                  │               │               │
    ┌─────────────▼───────────────▼───────────────▼───────────┐
    │                 🔍 Observer Layer                       │
    │  ┌─────────────────────────────────────────────────────┐ │
    │  │          Specialized Observers                      │ │
    │  │                                                     │ │
    │  │  Network ────┬──── Status ────┬──── Memory          │ │
    │  │      │       │        │       │        │            │ │
    │  │  Services ───┼──── Health ────┼──── Kernel          │ │
    │  │      │       │        │       │        │            │ │
    │  │  Storage ────┴──── Runtime ───┴──── Scheduler       │ │
    │  │                                                     │ │
    │  └─────────────────┬───────────────────────────────────┘ │
    │                    │ Structured Events                   │
    │                    ▼                                     │
    │  ┌─────────────────────────────────────────────────────┐ │
    │  │           🧠 Intelligence Layer                      │ │
    │  │                                                     │ │
    │  │   Event         Pattern        Root Cause          │ │
    │  │   Correlation ─▶ Recognition ─▶ Analysis           │ │
    │  │                                                     │ │
    │  └─────────────────┬───────────────────────────────────┘ │
    └────────────────────┼─────────────────────────────────────┘
                         │
                         ▼
        📊 Understanding: "Memory leak in redis caused 
            API timeouts leading to user retry storm"
```

## What We Actually Built

**14 observers** organized by domain, each with deep understanding of what they watch:

### Network & Communication
- **Network** - TCP/UDP connection monitoring, HTTP/DNS traffic analysis, application protocol parsing
- **Status** - L7 status codes (HTTP/gRPC errors), timeouts, latency tracking via network interception
- **Services** - Service dependency mapping using Kubernetes API and eBPF network monitoring
- **Link** - Network failure detection: TCP SYN timeouts, ARP failures, packet retransmissions, connection resets

### Memory & Storage
- **Memory** - Memory allocation/deallocation tracking, RSS growth monitoring, intelligent leak detection
- **Storage I/O** - VFS layer I/O monitoring, slow storage detection, Kubernetes volume issue analysis

### Process & Runtime  
- **Kernel** - ConfigMap/Secret access tracking, process lifecycle events via eBPF
- **Process Signals** - Runtime signal monitoring, OOM kill detection, crash loop correlation
- **Node Runtime** - Kubelet metrics collection (CPU, memory, storage), pod lifecycle events

### Health & Monitoring
- **Health** - Syscall error pattern tracking (ENOSPC, ENOMEM, ECONNREFUSED), resource exhaustion detection
- **OTEL** - OpenTelemetry OTLP protocol receiver, distributed tracing and service dependency mapping

### Platform & Orchestration
- **Scheduler** - CPU scheduling delays, CFS throttling, core migrations, noisy neighbor detection
- **Lifecycle** - Kubernetes resource state transitions, breaking change detection, cascade effects
- **Systemd** - Service state monitoring, failure tracking, restart pattern analysis

Each observer understands its domain deeply. The Status Observer doesn't just count HTTP 500s—it detects cascading failure patterns and retry storms. The Memory Observer doesn't just track allocations—it identifies leak patterns and fragmentation issues.

## Why This Matters

> "The task of the critic is to transform experience into memory." — Walter Benjamin

Traditional monitoring gives you the present moment. Tapio gives you the story of how you got there.

Instead of:
- "CPU is at 80%"
- "Response time increased"  
- "Error rate spiked"

You get:
- "Memory fragmentation triggered GC pressure, causing API timeouts, leading to client retry storms"

## Real Deployment

Tapio is designed specifically for **Kubernetes clusters**. It runs as a DaemonSet with one agent per node.

```bash
git clone https://github.com/yairfalse/tapio
cd tapio
make build
kubectl apply -f k8s/
```

Each agent collects kernel-level events from its node and correlates them into understanding about your pods, services, and cluster behavior.

## Current State

We have the observer layer working and the correlation foundation built. The intelligence layer is being developed to connect events into meaningful patterns.

This is systems software for people who understand that observability is about comprehension, not collection.

---

*Built for engineers who need to understand, not just monitor.*