# Simple Mode Deployment

**Simple Mode** is the fastest way to start using Tapio. It requires **zero infrastructure dependencies** - no external gateways, no databases, just observers sending directly to an OTLP collector.

## Architecture

```
┌──────────────────┐
│  Network Observer │───┐
└──────────────────┘   │
                       ├─> OTLPEmitter ──> OTLP Collector ──> Grafana/Prometheus
┌──────────────────┐   │
│ Scheduling Observer│───┘
└──────────────────┘
```

## Quick Start

### 1. Deploy OTLP Collector (Grafana Alloy)

```bash
kubectl apply -f grafana-alloy.yaml
```

### 2. Deploy Tapio Observers

```bash
kubectl apply -f tapio-observers.yaml
```

### 3. View Events in Grafana

```bash
# Forward Grafana port
kubectl port-forward svc/grafana 3000:3000 -n tapio

# Login: admin/admin
# Navigate to Explore → Select "Loki" → Query: {job="tapio"}
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TAPIO_OTLP_ENDPOINT` | `alloy:4318` | OTLP HTTP endpoint |
| `TAPIO_SAMPLING_RATE` | `1.0` | Sample rate (0.0-1.0) |

### Example: 10% Sampling

```yaml
env:
- name: TAPIO_SAMPLING_RATE
  value: "0.1"  # Keep only 10% of events
```

## Deployment Options

| Tier | Observer Emitter | Backend | Latency | Use Case |
|------|------------------|---------|---------|----------|
| **Simple** (this) | OTLPEmitter | OTLP Collector (direct) | ~10-50ms | Getting started |
| **POLKU** | PolkuPublisher | POLKU → AHTI | ~50-200ms | Edge intelligence, graph enrichment |

## When to Upgrade

**Upgrade to POLKU tier when you need:**
- Event buffering with backpressure (survives gateway restarts)
- K8s context enrichment (pod names, labels, etc.)
- Graph-based root cause analysis via AHTI
- Cross-service correlation
- Semantic event relationships

## Troubleshooting

### No events appearing in Grafana

```bash
# Check observer logs
kubectl logs -f daemonset/tapio-network-observer -n tapio

# Check OTLP collector logs
kubectl logs -f deployment/alloy -n tapio

# Verify endpoint is reachable
kubectl exec -it daemonset/tapio-network-observer -n tapio -- \
  wget -O- http://alloy:4318/health
```

### High memory usage

Reduce sampling rate:
```yaml
env:
- name: TAPIO_SAMPLING_RATE
  value: "0.1"  # 10% sampling
```

## Next Steps

- [View Example Queries](./queries.md)
- [Upgrade to FREE Tier](../free-mode/README.md)
- [Configure Dashboards](./dashboards/README.md)
