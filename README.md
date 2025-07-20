# Tapio: Cross-Layer Observability (Early Development)

<div align="center">

![Tapio Logo](https://img.shields.io/badge/Tapio-Cross--Layer%20Observability-blue?style=for-the-badge)
![Status](https://img.shields.io/badge/Status-Early%20Development-orange?style=for-the-badge)

**Building an observability platform that aims to correlate events across ALL layers - from kernel to user**

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Passing-green.svg)](#)

[Current State](#-current-state) • [Vision](#-our-vision) • [Architecture](docs/ARCHITECTURE.md) • [Contributing](#-contributing)

</div>

## 🔍 The Problem We're Trying to Solve

When your payment service fails at 3 AM, you need answers:
- **Why** did it fail? (root cause)
- **What** is the business impact? (revenue loss)  
- **How** do we fix it? (actionable steps)

Current tools show symptoms across disconnected dashboards. **Our goal is to show the complete story:**

```
User Request → API Error → Database Timeout → Pod OOMKill → Memory Leak → Kernel Syscalls
                     All connected by trace_id: abc123
```

## 📍 Current State

**What we have built so far:**

✅ **UnifiedEvent Format** - A single event structure that can represent different observability signals  
✅ **Basic Analytics Engine** - Foundation for real-time event processing  
✅ **Correlation Framework** - Early semantic correlation between events  
✅ **gRPC/REST APIs** - Basic service interfaces  
✅ **Single Module Architecture** - Clean, maintainable codebase  
✅ **CNI Collector** - First collector using UnifiedEvent format  

**What we're actively working on:**

🔄 **Event Collectors** - eBPF, Kubernetes, SystemD data sources  
🔄 **Performance Optimization** - Targeting high-throughput processing  
🔄 **Cross-Layer Correlation** - Linking events from kernel to application  
🔄 **Developer Experience** - Making it easy to run and test  

**What we haven't built yet:**

❌ Full production deployment  
❌ Complete end-to-end correlation  
❌ Business impact assessment  
❌ Automated remediation  
❌ UI/Dashboard  
❌ Distributed deployment

## 🎯 Our Vision

### 1. **UnifiedEvent Format** ✅ *Built*
A single event structure that can represent different observability signals:
- eBPF kernel events  
- OpenTelemetry traces
- Kubernetes events
- Network packets
- Application logs

[Learn more about UnifiedEvent →](docs/UNIFIED_EVENT_DESIGN.md)

### 2. **Cross-Layer Correlation** 🔄 *Building*
Using OTEL trace context, we want to automatically link:
```
HTTP 500 error → DB timeout → OOM kill → memory leak syscalls
```
*Goal: No manual correlation needed!*

### 3. **Semantic Understanding** 🔄 *Prototyping*
Instead of just collecting data, we want to understand it:
```go
// Traditional: "connection refused error"  
// Our Goal: "Payment database unreachable, affecting 127 customers, $12K revenue at risk"
```

### 4. **Business Impact Assessment** ❌ *Future*
Vision: Every event scored for business impact:
- Customer facing?
- Revenue impacting?  
- SLO violation?
- Cascade risk?

## 🏃 Getting Started

### For Developers

```bash
# Clone the repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Build components (what's working so far)
go build ./...

# Run tests to see current functionality
go test ./...

# Format code (required for contributions)
gofmt -w .
```

### Current Working Components

```bash
# Test the UnifiedEvent format
go test ./pkg/domain/ -v

# Test analytics engine foundation  
go test ./pkg/analytics/ -v

# Test correlation framework
go test ./pkg/intelligence/correlation/ -v

# Test gRPC service foundations
go test ./pkg/interfaces/server/grpc/ -v
```

**Note:** Full end-to-end system integration is still in development. We're building this incrementally and testing each component thoroughly.

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
| **UnifiedEvent** | Universal event format with OTEL context | ✅ Built |
| **Analytics Engine** | Foundation for real-time processing | 🔄 In Progress |
| **Correlation Engine** | Early semantic correlation framework | 🔄 In Progress |
| **CNI Collector** | Container network interface events | ✅ Prototype |
| **eBPF Collector** | Kernel-level event collection | 🔄 In Progress |
| **K8s Collector** | Kubernetes event monitoring | 🔄 In Progress |
| **SystemD Collector** | System service monitoring | 🔄 In Progress |
| **gRPC/REST API** | Service interfaces | ✅ Basic Framework |
| **End-to-End Integration** | Full system working together | ❌ Not Yet |
| **Performance Optimization** | High-throughput processing | 🔄 Building |
| **Production Deployment** | Ready for real workloads | ❌ Future |

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

## Example: What We're Building Towards

### Vision: Payment Service Issue Correlation

*This is our goal for what end-to-end correlation might look like:*

```bash
# Future vision of tapio-cli
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
```

**Note**: This end-to-end correlation doesn't exist yet. We're building the foundation to make this possible.

## Intended Use Cases

### 1. **Root Cause Analysis**
Goal: From symptom to root cause in seconds, not hours.

### 2. **Predictive Failure Detection**
Goal: Detect cascading failures before they impact customers.

### 3. **Business Impact Assessment**
Goal: Understand the real cost of technical issues.

### 4. **SLO/SLA Compliance**
Goal: Track and predict SLO violations across all layers.

### 5. **Cost Correlation**
Goal: Link performance issues to cloud costs.

## 🎯 Performance Goals

*These are our targets as we build towards production:*

- **Throughput Target**: 165,000+ events/second *(not yet achieved)*
- **Latency Target**: < 1ms p99 event processing *(in development)*
- **Correlation Target**: < 100ms for 1000-event chains *(prototyping)*
- **Memory Goal**: ~4GB for 1M events in memory *(optimizing)*
- **Storage Goal**: Configurable retention *(future feature)*

**Current Performance**: We're building the foundation and haven't benchmarked full end-to-end performance yet. Our focus is on correctness first, then optimization.

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

## Development Roadmap

### Current (Q1 2025)
- [x] UnifiedEvent format foundation
- [x] Basic analytics engine structure
- [x] Early correlation framework
- [ ] Production-ready eBPF collector
- [ ] End-to-end integration
- [ ] Performance optimization

### Next Steps (Q2 2025)
- [ ] Complete cross-layer correlation
- [ ] ML-powered anomaly detection
- [ ] Automated remediation triggers
- [ ] Multi-cluster federation
- [ ] Cost correlation features

### Future Vision (Q3 2025)
- [ ] SaaS offering consideration
- [ ] Enterprise features
- [ ] Compliance reporting
- [ ] Advanced visualizations

**Note**: This roadmap represents our goals and may change based on development progress and user feedback.

## Acknowledgments

Building on excellent foundations:
- **eBPF** community for kernel observability
- **OpenTelemetry** for standardizing observability
- **Kubernetes** for container orchestration
- **Go** community for excellent tooling

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

---

<div align="center">

**Built with Go for the SRE debugging at 3 AM**

[Repository](https://github.com/yairfalse/tapio) • [Issues](https://github.com/yairfalse/tapio/issues) • [Contributing](CONTRIBUTING.md)

</div>
