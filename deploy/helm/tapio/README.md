# Tapio Helm Chart

Kubernetes intelligence that speaks human - predict failures, understand problems, get answers.

## Installation

```bash
# Add the Tapio repository
helm repo add tapio https://charts.tapio.io
helm repo update

# Install Tapio with default values
helm install tapio tapio/tapio

# Install with custom values
helm install tapio tapio/tapio --set server.replicas=3
```

## Quick Start

```bash
# Install from local directory
helm install tapio ./deploy/helm/tapio

# Install in specific namespace
helm install tapio ./deploy/helm/tapio --namespace tapio-system --create-namespace

# Install with custom memory thresholds
helm install tapio ./deploy/helm/tapio \
  --set config.memoryThresholds.warning=70 \
  --set config.memoryThresholds.critical=85
```

## Configuration

Key configuration options:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nodeAgent.enabled` | Deploy eBPF collectors on nodes | `true` |
| `server.replicas` | Number of server replicas | `1` |
| `config.updateInterval` | Metrics update interval | `30s` |
| `config.memoryThresholds.warning` | Memory warning threshold | `80` |
| `config.memoryThresholds.critical` | Memory critical threshold | `90` |
| `server.metrics.enabled` | Enable Prometheus metrics | `true` |

See `values.yaml` for full configuration options.

## Architecture

The chart deploys two main components:

1. **Node Agent (DaemonSet)**: Runs on every node to collect kernel-level metrics using eBPF
2. **Server (Deployment)**: Aggregates data and provides API/metrics endpoints

## Requirements

- Kubernetes 1.19+
- Linux kernel 4.18+ (for eBPF support)
- Helm 3.0+

## Uninstall

```bash
helm uninstall tapio
```