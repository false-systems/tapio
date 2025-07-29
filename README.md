# Tapio: Cross-Layer Observability Platform

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Passing-green.svg)](#)

Tapio is an observability platform designed to correlate events across multiple system layers - from kernel events to application metrics - providing unified insights into system behavior and root cause analysis.

## Problem Statement

Modern distributed systems generate observability data across disconnected layers:
- Kernel-level events (eBPF)
- Container orchestration (Kubernetes)  
- System services (systemd)
- Network traffic
- Application metrics and traces

Current tools treat these as separate domains. Tapio aims to correlate events across all layers using OpenTelemetry trace context, providing a complete view of system behavior and enabling rapid root cause analysis.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     Event Sources                                │
├─────────────────────┬────────────────────┬──────────────────────┤
│   eBPF Collector    │  K8s Collector     │ Systemd Collector    │
│   (Kernel Events)   │  (K8s API Events)  │  (Journal Logs)      │
└──────────┬──────────┴─────────┬──────────┴───────┬──────────────┘
           │                    │                  │
           └────────────────────┴──────────────────┘
                               │
                    ┌──────────┴──────────┐
                    │   UnifiedEvent      │
                    │   Conversion Layer  │
                    └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    │  Analytics Engine   │
                    │  (165k events/sec)  │
                    └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    │ Correlation Engine  │
                    │ Pattern Detection   │
                    └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    │   gRPC/REST API     │
                    │   Event Storage     │
                    └─────────────────────┘
```

## Key Components

### UnifiedEvent
A standardized event format that represents data from any source with:
- OpenTelemetry trace context for correlation
- Semantic categorization and tagging
- Entity context (pod, service, node)
- Business impact assessment
- Kernel, network, and application-specific data fields

### Collectors
- **eBPF Collector**: Captures kernel events, syscalls, network packets
- **Kubernetes Collector**: Monitors K8s API events and state changes
- **Systemd Collector**: Tracks system service lifecycle and logs
- **CNI Collector**: Container network interface events

### Intelligence Layer
- **Analytics Engine**: Processes 165k+ events/second
- **Correlation Engine**: Groups related events using trace context
- **Pattern Detection**: Identifies known failure patterns
- **Impact Assessment**: Calculates business and operational impact

### API Layer
- **gRPC Services**: High-performance event streaming
- **REST Gateway**: HTTP API for queries and management
- **Event Storage**: Configurable persistence layer

## Current Status

### Implemented
- UnifiedEvent data model with OTEL integration
- eBPF collector with dual-layer architecture
- Analytics engine achieving 165k events/second
- Semantic correlation with trace propagation
- gRPC service infrastructure

## Getting Started

### Prerequisites
- Go 1.24+
- Linux kernel 4.19+ (for eBPF)
- Kubernetes cluster (optional)
- Docker

### Installation

```bash
# Clone repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Build all components
make build

# Run tests
make test

# Format code (required before commits)
make fmt
```

### Running Tapio

#### Local Development
```bash
# Start the server
./bin/tapio-server

# In another terminal, run collectors
sudo ./bin/tapio-collector --enable-ebpf --server localhost:9090
```

#### Kubernetes Deployment
```bash
# Using Skaffold for development
skaffold dev --port-forward

# Access services
# - gRPC: localhost:9090
# - REST: localhost:8080  
# - Jaeger: localhost:16686
```

#### Hybrid Deployment (Recommended for eBPF)
Run eBPF collector on a Linux VM (e.g., Colima) while running services in Kubernetes:

```bash
# On Kubernetes
skaffold dev --port-forward

# On Linux VM (Colima)
sudo ./bin/tapio-collector --enable-ebpf --server host.lima.internal:9090
```

## Configuration

```yaml
# config/tapio.yaml
collectors:
  ebpf:
    enabled: true
    programs:
      - network_monitor
      - memory_tracker
  
  kubernetes:
    enabled: true
    namespace: all
  
intelligence:
  correlation:
    timeWindow: 5m
    minScore: 0.7
    
  analytics:
    batchSize: 1000
    workers: 4
```

## Performance

Current benchmarks on standard hardware:
- **Event Processing**: 165,000+ events/second
- **Correlation Latency**: < 10ms p99
- **Memory Usage**: ~512MB for 100k events/sec
- **CPU Usage**: 4 cores for full throughput

## Documentation

- [Architecture Details](docs/ARCHITECTURE.md)
- [API Reference](docs/API.md)
- [Deployment Guide](docs/DEPLOYMENT.md)
- [Development Setup](docs/DEVELOPMENT.md)
- [Performance Tuning](docs/PERFORMANCE.md)

## Contributing

We welcome contributions. Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Key areas for contribution:
- Additional collector implementations
- Correlation patterns and rules
- Performance optimizations
- Documentation improvements
- Test coverage

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

Built on the work of:
- eBPF community
- OpenTelemetry project
- Kubernetes SIG-Instrumentation
- Go performance community
