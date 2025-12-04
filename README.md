# Tapio

**Kubernetes Observability Agent**

Pattern detection for K8s failures. Observers detect issues, emit events to your stack.

---

## Status

**In Development** - Core infrastructure works. Wiring in progress.

### What Works Today

| Component | Coverage | Status |
|-----------|----------|--------|
| Deployments Observer | 93.9% | Production-ready |
| Scheduler Observer | 85.2% | Production-ready |
| Supervisor | 89.8% | Production-ready |
| OTLP Emitter | 82.1% | Works |
| K8s Context Service | 81.0% | Works (not wired yet) |

### What's In Progress

| Component | Status |
|-----------|--------|
| Network Observer | Code exists, needs integration tests |
| Intelligence Service | NATS routing scaffolded, not wired to observers |
| Ahti integration | Designed, not connected |

### What's Not Started

- Helm charts
- Container/Node observers
- End-to-end pipeline (Observer → NATS → Ahti)

---

## Architecture

### Current (Simple Tier - Works)

```
Observers ──▶ OTLP Emitter ──▶ Your Collector (Prometheus/Grafana/etc)
```

### Target (Not Wired Yet)

```
Observers ──▶ Context Service ──▶ Emitters
                (enrichment)         │
                                     ├──▶ OTLP ──▶ Your Stack
                                     │
                                     └──▶ NATS ──▶ Intelligence ──▶ Ahti
                                                     Service        (correlation)
```

---

## Quick Start

```bash
# Prerequisites: Go 1.24+, Kubernetes cluster

git clone https://github.com/yairfalse/tapio
cd tapio

# Build
make build

# Run Deployments Observer (the one that works)
./bin/tapio --observer=deployments
```

### Environment Variables

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317  # Your OTLP collector
KUBECONFIG=~/.kube/config                    # K8s access
```

---

## Observers

### Deployments Observer (Production)

Detects deployment issues via K8s API:
- Stuck rollouts
- Scaling failures
- Replica mismatches

```go
// Example event
{
  "type": "deployment",
  "subtype": "rollout_stuck",
  "deployment_data": {
    "name": "my-app",
    "namespace": "production",
    "replicas_desired": 3,
    "replicas_ready": 1
  }
}
```

### Scheduler Observer (Production)

Detects scheduling issues via K8s Events API:
- FailedScheduling events
- Resource constraints
- Node affinity issues

### Network Observer (In Progress)

eBPF-based pattern detection:
- SYN timeout → unreachable service
- DNS failures
- Connection refused

**Status:** Pattern detection code exists. Needs integration testing with real eBPF.

---

## The Gap

We have good components that aren't connected:

```
BUILT:                           MISSING:
─────                            ───────
✅ Observers detect patterns     ❌ Events don't flow to Intelligence Service
✅ OTLP emitter works            ❌ No NATS emitter
✅ Context Service works         ❌ Observers don't call it
✅ Intelligence Service works    ❌ Nothing sends it events
```

### What Needs Wiring

1. **NATSEmitter** - Send events to Intelligence Service
2. **Context enrichment** - Observers call Context Service before emitting
3. **Tier config** - Choose Simple (OTLP) vs Free (OTLP + NATS)
4. **Integration tests** - Full pipeline Observer → NATS → Ahti

---

## Development

```bash
# Run tests
make test

# Lint
make lint

# Build all
make build
```

### Test Coverage

```
pkg/decoders              99.1%
internal/observers/deploy 93.9%
internal/runtime/super    89.8%
internal/observers/sched  85.2%
internal/base             82.1%
internal/services/k8s     81.0%
pkg/intelligence          35.9%  <- needs work
```

---

## Project Structure

```
tapio/
├── cmd/                    # Binaries
├── internal/
│   ├── observers/          # Pattern detection
│   │   ├── deployments/    # ✅ Production
│   │   ├── scheduler/      # ✅ Production
│   │   ├── network/        # 🔄 In progress
│   │   └── ...
│   ├── runtime/            # Observer lifecycle
│   │   ├── supervisor/     # ✅ Production
│   │   ├── emitter_otlp.go # ✅ Works
│   │   └── emitter_file.go # ✅ Works
│   └── services/
│       └── k8scontext/     # ✅ Works (unused)
├── pkg/
│   ├── domain/             # Event types
│   ├── intelligence/       # NATS routing (unused)
│   └── decoders/           # Protocol parsing
└── docs/                   # Design docs
```

---

## Roadmap

**Now:** Wire the pipeline
1. Create NATSEmitter
2. Connect Context Service to observers
3. Integration test full flow

**Next:** Production deployment
- Helm charts
- DaemonSet manifests
- Operator (maybe)

**Later:** More observers
- Network (finish eBPF integration)
- Container lifecycle
- Node metrics

---

## Why "Tapio"?

Finnish god of forests. Watches over the ecosystem.

Tapio watches Kubernetes - pods, services, nodes, deployments.

---

## Related Projects

- **[Ahti](https://github.com/yairfalse/ahti)** - Graph correlation backend (Tapio → Ahti)
- **[Sykli](https://github.com/yairfalse/sykli)** - CI in your language

---

## License

Apache 2.0
