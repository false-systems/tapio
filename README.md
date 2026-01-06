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
│  ┌──────────────────────┐                                        │
│  │   eBPF Observers     │                                        │
│  │                      │                                        │
│  │  • network (TCP/DNS) │                                        │
│  │  • container (OOM)   │                                        │
│  │  • node (PMC)        │                                        │
│  └──────────┬───────────┘                                        │
│             │                                                    │
│             ▼                                                    │
│      Filter (~1%)                                                │
│      (anomalies)                                                 │
│             │                                                    │
│             ▼                                                    │
│        POLKU ────────────────────────────────────────────────────┤
└─────────────────────────┬───────────────────────────────────────┘
                          │
┌─────────────────────────┴───────────────────────────────────────┐
│                    PORTTI (Cluster - 1-2 replicas)               │
│                                                                  │
│  K8s API Watcher: Deployments, Pods, Services, Nodes, Events    │
│  Sends 100% (low volume, causal anchors)                         │
│             │                                                    │
│             ▼                                                    │
│        POLKU ────────────────────────────────────────────────────┤
└─────────────────────────┬───────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
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
- **TAPIO**: eBPF kernel events → Filter to 1% (only anomalies)
- **PORTTI**: K8s API events → Send 100% (they're causal anchors)
- **AHTI**: Never watches - only receives and correlates

---

## Observers

### eBPF Observers (Kernel Level)

| Observer | Captures | Filters To |
|----------|----------|------------|
| **Network** | TCP states, DNS, retransmits | RTT spikes >2x baseline, connection failures |
| **Container** | OOM kills, process exits | OOM, error exits (code ≠ 0) |
| **Node** | PMC, cgroup metrics | Memory pressure >80%, CPU throttling |

### Prometheus Scraping

| Observer | Captures | Sends |
|----------|----------|-------|
| **Scheduler** | kube-scheduler metrics | Scheduling latency, queue depth |

**Note**: K8s API watching (Deployments, Pods, Services, Nodes, Events) moved to **[PORTTI](https://github.com/yairfalse/portti)**.

---

## Status

### Production Ready

| Component | Coverage | Description |
|-----------|----------|-------------|
| Supervisor | 89.8% | Observer lifecycle |
| Network Observer | 78% | eBPF TCP/DNS/RTT |
| Scheduler Observer | 85.2% | Prometheus scraping |
| K8s Context | - | Pod metadata enrichment |

### In Progress

| Component | Status |
|-----------|--------|
| Container Observer | eBPF code written, needs compilation |
| Node Observer | PMC metrics, cgroup integration |

---

## Quick Start

```bash
# Prerequisites: Linux, Go 1.24+, Kubernetes cluster

git clone https://github.com/yairfalse/tapio
cd tapio

# Build
make build

# Run with OTLP export (FREE tier)
./bin/tapio --observer=network

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
| **[PORTTI](https://github.com/yairfalse/portti)** | K8s API watcher - Deployments, Pods, Services, Nodes |
| **[AHTI](https://github.com/yairfalse/ahti)** | Central Intelligence - receives events, builds causality graph |
| **[POLKU](https://github.com/yairfalse/polku)** | Event router - transforms raw events to AhtiEvent |
| **[Sykli](https://github.com/yairfalse/sykli)** | CI in your language |

---

## Why "Tapio"?

Finnish god of forests. Watches over the ecosystem.

Tapio watches Kubernetes at the kernel level - network packets, container lifecycle, node health. It sees what APM tools miss.

---

## License

Apache 2.0
