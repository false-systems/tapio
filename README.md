# Tapio: Cross-Layer Observability with Semantic Correlation

<div align="center">

![Tapio Logo](https://img.shields.io/badge/Tapio-Cross--Layer%20Observability-blue?style=for-the-badge)

**Revolutionary observability platform that correlates events across ALL layers - from kernel to user**

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Performance](https://img.shields.io/badge/Throughput-165k%20events%2Fsec-green.svg)](docs/ARCHITECTURE.md)

[Quick Start](#-quick-start) • [Why Tapio](#-why-tapio) • [Architecture](docs/ARCHITECTURE.md) • [Documentation](#-documentation)

</div>

## 🚀 The Problem We Solve

When your payment service fails at 3 AM, you need answers:
- **Why** did it fail? (root cause)
- **What** is the business impact? (revenue loss)
- **How** do we fix it? (actionable steps)

Current tools show symptoms across disconnected dashboards. **Tapio shows the complete story:**

```
User Request → API Error → Database Timeout → Pod OOMKill → Memory Leak → Kernel Syscalls
                     All connected by trace_id: abc123
```

## ✨ Key Innovations

### 1. **UnifiedEvent Format**
One event format that can represent ANY observability signal:
- eBPF kernel events
- OpenTelemetry traces
- Kubernetes events  
- Network packets
- Application logs

[Learn more about UnifiedEvent →](docs/UNIFIED_EVENT_DESIGN.md)

### 2. **Automatic Cross-Layer Correlation**
Using OTEL trace context, we automatically link:
```
HTTP 500 error → DB timeout → OOM kill → memory leak syscalls
```
No manual correlation needed!

### 3. **Semantic Understanding**
We don't just collect data, we understand it:
```go
// Traditional: "connection refused error"
// Tapio: "Payment database unreachable, affecting 127 customers, $12K revenue at risk"
```

### 4. **Business Impact Assessment**
Every event is scored for business impact:
- Customer facing?
- Revenue impacting?
- SLO violation?
- Cascade risk?

## 🏃 Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Build the platform
make build

# Run with example configuration
./tapio-server --config examples/config.yaml
```

### Docker Compose

```bash
# Start entire stack
docker-compose up -d

# View real-time events
tapio-cli stream events

# Check system health
tapio-cli health
```

## 🏗️ Architecture

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Event Sources                              │
├───────────┬────────────┬─────────────┬────────────┬─────────────┤
│   eBPF    │    OTEL    │ Kubernetes  │  Network   │ Application │
└─────┬─────┴──────┬─────┴──────┬──────┴─────┬──────┴──────┬──────┘
      │            │            │            │             │
      └────────────┴────────────┴────────────┴─────────────┘
                              │
                    ┌─────────┴──────────┐
                    │  UnifiedEvent      │
                    │ Conversion Layer   │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │ Analytics Engine   │
                    │ 165k events/sec    │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │ Correlation Engine │
                    │ Semantic Analysis  │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │   gRPC/REST API    │
                    └────────────────────┘
```

[Detailed Architecture →](docs/ARCHITECTURE.md)

### Core Components

| Component | Description | Status |
|-----------|-------------|--------|
| **UnifiedEvent** | Universal event format with OTEL context | ✅ Production |
| **Analytics Engine** | 165k events/sec real-time processing | ✅ Production |
| **Correlation Engine** | Semantic correlation across layers | ✅ Production |
| **eBPF Collector** | Kernel-level event collection | ✅ Beta |
| **OTEL Collector** | OpenTelemetry integration | ✅ Production |
| **K8s Collector** | Kubernetes event monitoring | ✅ Production |
| **gRPC/REST API** | High-performance streaming API | ✅ Production |

## 🔧 Configuration

### Basic Configuration

```yaml
# config/tapio.yaml
analytics:
  maxEventsPerSecond: 165000
  enableSemanticGrouping: true
  enableImpactAssessment: true

collectors:
  ebpf:
    enabled: true
    programs:
      - syscalls
      - network
      - memory
  
  kubernetes:
    enabled: true
    watchNamespaces:
      - production
      - staging
  
  otel:
    enabled: true
    endpoint: "0.0.0.0:4317"

correlation:
  enableRealTime: true
  confidenceThreshold: 0.7
  groupRetentionPeriod: 30m
```

## 📊 Example: Real-World Correlation

### Scenario: Payment Service Degradation

```bash
# What you see in Tapio
$ tapio-cli correlate --trace-id abc123

CORRELATION SUMMARY
═══════════════════
Root Cause: Memory leak in payment-service v2.1.0
Business Impact: HIGH - $45K revenue at risk
Affected Users: 1,247
Time to Detection: 12 seconds

EVENT CHAIN (5 events correlated)
═════════════════════════════════
1. [APP] OutOfMemoryError in payment-service
   └─ Heap exhausted after 3 days uptime
   
2. [K8S] Pod payment-service-7d8f9 OOMKilled
   └─ Container exceeded 2Gi memory limit
   
3. [NET] Connection refused on payment-db:5432
   └─ 47 failed connection attempts
   
4. [APP] PaymentException: Database unavailable
   └─ Affecting checkout flow
   
5. [BIZ] Revenue impact detected
   └─ 312 failed transactions, avg $144.23

RECOMMENDED ACTIONS
═══════════════════
1. IMMEDIATE: Scale payment-service to 5 replicas
2. SHORT-TERM: Increase memory limit to 4Gi
3. LONG-TERM: Fix memory leak in v2.1.0 (goroutine leak detected)

$ tapio-cli apply recommendation 1
✓ Scaled payment-service to 5 replicas
✓ Service recovering, latency dropping
✓ Estimated recovery: 2 minutes
```

## 🎯 Use Cases

### 1. **Root Cause Analysis**
From symptom to root cause in seconds, not hours.

### 2. **Predictive Failure Detection**
Detect cascading failures before they impact customers.

### 3. **Business Impact Assessment**
Understand the real cost of technical issues.

### 4. **SLO/SLA Compliance**
Track and predict SLO violations across all layers.

### 5. **Cost Correlation**
Link performance issues to cloud costs.

## 🚀 Performance

- **Throughput**: 165,000+ events/second
- **Latency**: < 1ms p99 event processing
- **Correlation Time**: < 100ms for 1000-event chains
- **Memory**: ~4GB for 1M events in memory
- **Storage**: Configurable retention (default 7 days)

## 📖 Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/ARCHITECTURE.md) | Complete system architecture |
| [UnifiedEvent Design](docs/UNIFIED_EVENT_DESIGN.md) | Deep dive into the event format |
| [Rationale](docs/RATIONALE.md) | Why we built Tapio this way |
| [API Reference](docs/API.md) | gRPC and REST API documentation |
| [Collector Guide](docs/COLLECTORS.md) | How to write custom collectors |
| [Deployment](docs/DEPLOYMENT.md) | Production deployment guide |

## 🧪 Development

### Prerequisites
- Go 1.24+
- Docker & Docker Compose
- Protocol Buffers compiler
- Make

### Building from Source

```bash
# Clone repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Install dependencies
make deps

# Build all components
make build

# Run tests
make test

# Run with race detector
make test-race

# Generate protobufs
make proto

# Build specific component
make build-server
make build-collector
make build-cli
```

### Development Workflow

```bash
# Start development environment
make dev

# Watch for changes and rebuild
make watch

# Run linters
make lint

# Format code
make fmt
```

## 🤝 Contributing

We love contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Good First Issues
- [ ] Add Prometheus remote-write support
- [ ] Implement Grafana datasource plugin
- [ ] Add more semantic patterns
- [ ] Improve CLI output formatting
- [ ] Add integration tests

## 📈 Roadmap

### Q1 2025
- [x] UnifiedEvent format
- [x] Analytics engine (165k events/sec)
- [x] Basic correlation engine
- [ ] Production-ready eBPF collector
- [ ] Grafana integration

### Q2 2025
- [ ] ML-powered anomaly detection
- [ ] Automated remediation triggers
- [ ] Multi-cluster federation
- [ ] Cost correlation features

### Q3 2025
- [ ] SaaS offering
- [ ] Enterprise features
- [ ] Compliance reporting
- [ ] Advanced visualizations

## 🙏 Acknowledgments

Standing on the shoulders of giants:
- **eBPF** community for kernel observability
- **OpenTelemetry** for standardizing observability
- **Kubernetes** for container orchestration
- **Go** community for amazing tools

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

---

<div align="center">

**Built with Go and ❤️ for the frustrated SRE at 3 AM**

[Website](https://tapio.dev) • [Documentation](https://docs.tapio.dev) • [Blog](https://blog.tapio.dev)

[![Star History](https://api.star-history.com/svg?repos=yairfalse/tapio&type=Date)](https://github.com/yairfalse/tapio/stargazers)

</div>