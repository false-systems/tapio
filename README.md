# Tapio

> "Experience without theory is blind, but theory without experience is mere intellectual play." — Immanuel Kant

**Observability that understands your Kubernetes clusters, not just measures them.**

## The Idea

Most observability tools collect metrics and hope you can figure out what went wrong. Tapio watches the kernel and correlates events to tell you what actually happened.

When your service starts failing, you don't need another dashboard showing CPU is high. You need to know that the memory allocator is fragmenting, causing garbage collection storms, which trigger circuit breakers in your API gateway, leading to cascading timeouts across your service mesh.

That's correlation. That's understanding.

## Observer Architecture

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                                     │
│                                                                                │
│    Pod: api-gateway          Pod: redis-cache         Pod: worker-service     │
│    ├─ nginx:1.21            ├─ redis:7.0              ├─ app:v2.3             │
│    ├─ 3 replicas            ├─ memory: 2GB limit      ├─ CPU throttled        │
│    └─ HTTP 500s ↑           └─ RSS growing ↑          └─ OOM killed ↑         │
│                                                                                │
└────────────────────────────────┬──────────────────────────────────────────────┘
                                 │
                                 │ eBPF hooks at kernel level
                                 │ K8s API watches at cluster level
                                 ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                          Observer Layer (17 Observers)                         │
├───────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│  Network & Communication          Memory & Storage        Process & Runtime   │
│  ┌─────────────────────┐          ┌────────────────┐     ┌─────────────────┐ │
│  │ Network   │ DNS     │          │ Memory         │     │ Kernel          │ │
│  │ Status    │ Link    │          │ Storage I/O    │     │ Process Signals │ │
│  └─────────────────────┘          └────────────────┘     │ Container RT    │ │
│                                                           └─────────────────┘ │
│                                                                                │
│  Kubernetes & Orchestration       System & Platform                           │
│  ┌─────────────────────┐          ┌────────────────┐                         │
│  │ Deployments         │          │ Health         │                         │
│  │ Lifecycle           │          │ Systemd        │                         │
│  │ Scheduler           │          │ OTEL           │                         │
│  │ Node Runtime        │          │ Base           │                         │
│  └─────────────────────┘          └────────────────┘                         │
│                                                                                │
│  Each observer produces typed, structured events:                             │
│  • Network: TCP connections, HTTP requests, DNS queries                       │
│  • Memory: Allocations, leaks, OOM events                                     │
│  • Deployments: Image changes, scale events, config updates                   │
│  • Scheduler: CPU delays, throttling, noisy neighbors                         │
│                                                                                │
└────────────────────────────────┬──────────────────────────────────────────────┘
                                 │
                                 │ Typed events with correlation hints
                                 ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                          Intelligence Layer                                    │
├───────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│   ┌──────────────────┐      ┌──────────────────┐      ┌──────────────────┐  │
│   │ Event Correlation│─────▶│ Pattern Detection│─────▶│ Root Cause       │  │
│   │                  │      │                  │      │ Analysis         │  │
│   │ • Time windows   │      │ • Cascading fail │      │ • Causal chains  │  │
│   │ • Process graphs │      │ • Retry storms   │      │ • Blast radius   │  │
│   │ • Service mesh   │      │ • Memory leaks   │      │ • Impact score   │  │
│   └──────────────────┘      └──────────────────┘      └──────────────────┘  │
│                                                                                │
└────────────────────────────────┬──────────────────────────────────────────────┘
                                 │
                                 ▼
                    ┌────────────────────────────────┐
                    │       Understanding            │
                    ├────────────────────────────────┤
                    │ "Deployment update to redis    │
                    │  image v7.0 at 14:32:15        │
                    │  caused memory leak,           │
                    │  triggering OOM kills,         │
                    │  leading to connection         │
                    │  failures in api-gateway,      │
                    │  resulting in user retry       │
                    │  storm and 500 errors"         │
                    │                                │
                    │ Confidence: 94%                │
                    │ Recommendation: Rollback       │
                    └────────────────────────────────┘
```

## What We Actually Built

**17 production-ready observers** organized by domain, each with deep understanding of what they watch:

### Network & Communication (4 observers)
- **Network** - L3-L7 protocol monitoring (TCP/UDP/ICMP, HTTP/DNS/gRPC), zero-copy eBPF architecture, connection tracking with Kubernetes enrichment
- **DNS** - DNS problem detection (slow queries, timeouts, NXDOMAIN), negative observer pattern tracking only failures
- **Status** - L7 status codes (HTTP/gRPC errors), cascading timeout detection, retry storm identification, protocol-level failure analysis
- **Link** - Network failure detection: TCP SYN timeouts, ARP failures, packet retransmissions, connection resets (referenced but not yet documented)

### Memory & Storage (2 observers)
- **Memory** - CO-RE eBPF memory leak detector, malloc/free tracking, stack trace capture, long-lived allocation detection with K8s enrichment
- **Storage I/O** - Block device I/O latency tracking, throughput monitoring, queue depth analysis, per-container attribution, I/O pattern detection

### Process & Runtime (3 observers)
- **Kernel** - Focused ConfigMap/Secret access monitoring, pod correlation infrastructure, security audit trail for configuration access
- **Process Signals** - Complete signal attribution (WHO killed WHOM and WHY), OOM kill detection, exit code decoding, death intelligence
- **Container Runtime** - Real-time OOM kill detection (microsecond precision), memory pressure monitoring, process exit tracking via eBPF

### Kubernetes & Orchestration (4 observers)
- **Deployments** - Deployment/ConfigMap/Secret change tracking, impact classification, restart detection, rich correlation context for the intelligence engine
- **Lifecycle** - Kubernetes resource state transitions (pods, services, nodes), breaking change detection, cascade effects
- **Scheduler** - CPU scheduling delays, CFS throttling, noisy neighbor detection, core migration tracking, invisible latency identification
- **Node Runtime** - Node health monitoring, kubelet metrics, resource pressure detection, system services tracking

### System & Platform (4 observers)
- **Health** - Syscall error pattern tracking (ENOSPC, ENOMEM, ECONNREFUSED), resource exhaustion detection, critical system health indicators
- **Systemd** - Systemd service state monitoring, failure tracking, restart pattern analysis, cgroup event correlation
- **OTEL** - OpenTelemetry OTLP receiver (gRPC/HTTP), distributed tracing, service dependency mapping, cross-platform support
- **Base** - Shared observer infrastructure providing consistent metrics, lifecycle management, and event channels (not standalone)

Each observer understands its domain deeply. The Status Observer doesn't just count HTTP 500s—it detects cascading failure patterns and retry storms. The Memory Observer doesn't just track allocations—it identifies leak patterns with stack traces. The Deployments Observer doesn't just watch changes—it classifies impact and predicts which events will correlate.

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