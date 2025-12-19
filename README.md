# Tapio

**Edge Intelligence for Kubernetes**

eBPF-based agent that captures kernel-level events, filters to anomalies at the edge (~1%), and sends enriched events to AHTI for root cause analysis.

---

## What Makes Tapio Different

**Tapio doesn't just collect data - it learns baselines and only sends what matters.**

| Traditional Observability | Tapio (Edge Intelligence) |
|--------------------------|---------------------------|
| Send everything | Filter to ~1% (anomalies only) |
| Central processing | Edge filtering |
| High bandwidth | Low bandwidth |
| Noise | Signal |

```
eBPF Kernel Events (millions/sec)
        │
        ▼
   Edge Filtering
   (RTT baseline learning,
    memory pressure detection)
        │
        ▼
   ~1% Anomalies ──────▶ AHTI (Central Intelligence)
                         └─▶ Root cause analysis
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    TAPIO (Edge - Per Node)                       │
│                                                                  │
│  ┌──────────────────────┐    ┌──────────────────────┐           │
│  │   eBPF Observers     │    │   K8s Observers      │           │
│  │                      │    │                      │           │
│  │  • network (TCP/DNS) │    │  • deployments       │           │
│  │  • container (OOM)   │    │  • configmaps        │           │
│  │  • node (PMC)        │    │  • scaling events    │           │
│  └──────────┬───────────┘    └──────────┬───────────┘           │
│             │                           │                        │
│             ▼                           ▼                        │
│      Filter (~1%)                 Send ALL (rare)                │
│      (anomalies)                  (causal events)                │
│             │                           │                        │
│             └───────────┬───────────────┘                        │
│                         ▼                                        │
│                    NATS Publish                                  │
└─────────────────────────┬───────────────────────────────────────┘
                          │
                    NATS Cluster
                          │
┌─────────────────────────▼───────────────────────────────────────┐
│                    AHTI (Central)                                │
│                                                                  │
│              Receives → Learns → Correlates                      │
│                                                                  │
│      "Deployment X at T=0 → OOM at T=5min → Root Cause"         │
│                                                                  │
│                   Never watches anything                         │
└──────────────────────────────────────────────────────────────────┘
```

**Key insight:**
- eBPF events: High volume → Filter to 1% (only anomalies)
- K8s events: Low volume → Send 100% (they're causal anchors)

---

## Observers

### eBPF Observers (Kernel Level)

| Observer | Captures | Filters To |
|----------|----------|------------|
| **Network** | TCP states, DNS, retransmits | RTT spikes >2x baseline, connection failures |
| **Container** | OOM kills, process exits | OOM, error exits (code ≠ 0) |
| **Node** | PMC, cgroup metrics | Memory pressure >80%, CPU throttling |

### K8s Observers (API Level)

| Observer | Captures | Sends |
|----------|----------|-------|
| **Deployments** | Creates, updates, deletes | All (causal anchors) |
| **Scheduler** | FailedScheduling events | All (failure events) |

---

## Status

### Production Ready

| Component | Coverage | Description |
|-----------|----------|-------------|
| Deployments Observer | 93.9% | K8s deployment lifecycle |
| Scheduler Observer | 85.2% | Scheduling failures |
| Supervisor | 89.8% | Observer lifecycle |
| Network Observer | 78% | eBPF TCP/DNS/RTT |

### In Progress

| Component | Status |
|-----------|--------|
| Container Observer | eBPF code written, needs compilation |
| Node Observer | PMC metrics, cgroup integration |
| NATS Integration | Scaffolded, not wired |

---

## Quick Start

```bash
# Prerequisites: Linux, Go 1.24+, Kubernetes cluster

git clone https://github.com/yairfalse/tapio
cd tapio

# Build
make build

# Run with OTLP export (FREE tier)
./bin/tapio --observer=deployments

# Run with NATS (connects to AHTI)
./bin/tapio --observer=network --nats=nats://localhost:4222
```

### Environment Variables

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317  # OTLP collector
NATS_URL=nats://localhost:4222               # NATS for AHTI
KUBECONFIG=~/.kube/config                    # K8s access
```

---

## Edge Filtering Examples

### Network Observer

```c
// In eBPF: Only emit when RTT spikes >2x baseline OR >500ms
if (rtt_us > (baseline->baseline_us * 2) || rtt_us > 500000) {
    emit_rtt_spike_event();  // ~1% of traffic
}
```

### Container Observer

```go
// Only send OOM kills and error exits
if evt.Type == EventTypeOOMKill || classification.Category == ExitCategoryError {
    publish(event)  // Skip normal exits
}
```

---

## Project Structure

```
tapio/
├── internal/
│   ├── observers/
│   │   ├── network/        # eBPF TCP/DNS/RTT
│   │   ├── container/      # eBPF OOM/exits
│   │   ├── node/           # eBPF PMC/cgroup
│   │   ├── deployments/    # K8s API
│   │   └── scheduler/      # K8s Events
│   ├── runtime/
│   │   └── supervisor/     # Observer lifecycle
│   └── services/
│       └── k8scontext/     # Pod metadata enrichment
├── pkg/
│   ├── domain/             # Event types (ObserverEvent)
│   └── intelligence/       # NATS routing
└── docs/
    └── designs/            # Architecture docs
```

---

## Documentation

- **[Edge-Central Data Flow](docs/designs/edge-central-data-flow.md)** - TAPIO-AHTI architecture
- **[Network Observer Design](docs/003-network-observer-dns-link-status-integration.md)** - eBPF patterns
- **[Container Observer Design](docs/006-container-observer-design-v2.md)** - OOM/exit detection

---

## Related Projects

| Project | Description |
|---------|-------------|
| **[AHTI](https://github.com/yairfalse/ahti)** | Central Intelligence - receives from TAPIO, builds causality graph |
| **[Sykli](https://github.com/yairfalse/sykli)** | CI in your language |

---

## Why "Tapio"?

Finnish god of forests. Watches over the ecosystem.

Tapio watches Kubernetes at the kernel level - network packets, container lifecycle, node health. It sees what APM tools miss.

---

## License

Apache 2.0
