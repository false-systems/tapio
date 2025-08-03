# Tapio: Modular Kubernetes Observability

<div align="center">

![Status](https://img.shields.io/badge/Status-Early%20Development-orange?style=for-the-badge)

**A modular observability platform exploring intelligent correlation of Kubernetes events**

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

[What We're Building](#-what-were-building) • [Current Status](#-current-status) • [Architecture](#-architecture) • [Contributing](#-contributing)

</div>

## 🎯 What We're Building

Tapio is an **experimental** platform for Kubernetes observability that attempts to correlate events across different infrastructure layers. We're exploring whether we can:

- Connect related events (pod failures → node issues → network problems)
- Detect common failure patterns automatically  
- Store event relationships in a graph database
- Answer "why did this fail?" questions

**This is early-stage research.** We make no promises about production readiness, performance, or reliability.

## 📍 Current Status

**What actually works today:**

✅ **Event Collection**: Multiple collectors (K8s API, eBPF, etcd, systemd, CNI)  
✅ **Event Pipeline**: NATS-based event streaming with correlation IDs  
✅ **Intelligence Engine**: Neo4j-based pattern detection with 6 basic patterns  
✅ **Graph Storage**: Stores K8s relationships and events in Neo4j  
✅ **Pattern Detection**: Basic failure pattern recognition (OOM kills, crash loops, etc.)  
✅ **Modular Architecture**: Clean separation between collectors, intelligence, and integrations  

**What we're experimenting with:**

🔬 **Semantic Correlation**: Attempting to automatically link related events  
🔬 **Root Cause Analysis**: Exploring graph-based "why did this fail" queries  
🔬 **Pattern Library**: Building detection for common K8s failure scenarios  

**What doesn't exist yet:**

❌ Production deployment guides  
❌ Performance guarantees  
❌ Backward compatibility promises  
❌ Enterprise features  
❌ SLA or support  

## 🏗️ Architecture

Tapio follows a 5-level modular architecture:

```
Level 0: pkg/domain/          # Core data structures (UnifiedEvent, etc.)
Level 1: pkg/collectors/      # Data collection (K8s, eBPF, etcd, CNI, systemd)
Level 2: pkg/intelligence/    # Pattern detection and correlation
Level 3: pkg/integrations/    # External system connectors
Level 4: pkg/interfaces/      # APIs and user interfaces
```

### Core Components

- **UnifiedEvent**: Standard event format with K8s context and trace correlation
- **Collectors**: Modular event collection from different sources
- **Intelligence Engine**: Neo4j-based correlation and pattern detection
- **Event Pipeline**: NATS streaming for event distribution

[Detailed Architecture →](docs/ARCHITECTURE.md)

## 🚀 Quick Start

**Prerequisites**: Go 1.24+, Minikube, Neo4j

```bash
# Clone and build
git clone https://github.com/yairfalse/tapio.git
cd tapio
go build ./...

# Start Neo4j (using provided manifests)
kubectl apply -f k8s/neo4j.yaml

# Run intelligence demo
go run cmd/intelligence-demo/main.go
```

### Individual Collectors

```bash
# K8s events collector
go run cmd/collectors/kubeapi/main.go

# eBPF kernel events (requires root)
sudo go run cmd/collectors/ebpf/main.go

# systemd service events
go run cmd/collectors/systemd/main.go
```

## 🧪 Intelligence Engine Demo

The intelligence engine can detect basic failure patterns:

```bash
$ go run cmd/intelligence-demo/main.go

🧠 Tapio Intelligence Engine Test Drive
=====================================

1️⃣ Connecting to Neo4j...
✅ Connected and indexes created!

2️⃣ Scenario: Memory pressure causing OOM kills
💥 OOM Kill event for web-app-pod-1

🔍 Querying: Why did web-app-pod-1 fail?
📊 Root Cause Analysis: [Shows detected patterns]

✅ Test drive complete!
```

The demo creates synthetic scenarios and shows pattern detection working.

## 🔧 Development

### Building

```bash
# Format code (required)
gofmt -w .

# Build all modules
go build ./...

# Test individual modules
go test ./pkg/intelligence/...
go test ./pkg/collectors/...
```

### Module Structure

Each module builds independently:
```bash
cd pkg/collectors && go build ./...
cd pkg/intelligence && go build ./...
cd pkg/integrations && go build ./...
```

## 🤝 Contributing

We welcome experiments and improvements! See [CONTRIBUTING.md](CONTRIBUTING.md).

**Good first contributions:**
- Add new failure patterns to `pkg/intelligence/patterns/`
- Improve collector reliability
- Add integration tests
- Document failure scenarios

## ⚠️ Important Notes

- **Early Development**: APIs will change, data formats may break
- **Experimental**: We're exploring what's possible, not shipping a product
- **No Warranties**: Use at your own risk in non-production environments
- **Research Focus**: We prioritize learning over stability

## 📖 Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/ARCHITECTURE.md) | Modular system design |
| [UnifiedEvent](docs/UNIFIED_EVENT_DESIGN.md) | Core event format |
| [Intelligence](docs/INTELLIGENCE.md) | Pattern detection approach |
| [Collectors](docs/COLLECTORS.md) | Building data collectors |

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

---

<div align="center">

**An experiment in infrastructure observability**

[Repository](https://github.com/yairfalse/tapio) • [Issues](https://github.com/yairfalse/tapio/issues)

</div>