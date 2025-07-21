# Tapio Project Onboarding Guide

Welcome to Tapio! This guide will help you understand our project structure and get started quickly.

## 🎯 What is Tapio?

Tapio is an enterprise-grade observability platform that collects events from multiple sources (containers, kernel, Kubernetes, system logs) and provides intelligent correlation and root cause analysis.

## 🏗️ Architecture Overview

Tapio follows a **strict 5-level hierarchical architecture** to ensure clean dependencies and maintainability:

```
Level 0: Domain     → Core types (no dependencies)
Level 1: Collectors → Data collection (depends on Domain)
Level 2: Intelligence → Analytics & correlation (depends on Domain + Collectors)
Level 3: Integrations → External systems (depends on Domain + L1 + L2)
Level 4: Interfaces → User-facing APIs (depends on all above)
```

**Golden Rule**: A package can ONLY import from lower levels, never from the same or higher levels.

## 📁 Directory Structure

### `/cmd` - Executable Applications
These are the main entry points that compile into binaries.

```
cmd/
├── tapio-collector/     # Main collector service
│   └── main.go         # Runs multiple collectors concurrently
├── tapio-server/       # gRPC/REST API server
│   └── main.go        # Handles events and correlations
└── test-otel/         # OpenTelemetry testing tools
    └── main.go        # For testing distributed tracing
```

**Getting Started:**
```bash
# Run the collector
go run cmd/tapio-collector/main.go

# Run the server
go run cmd/tapio-server/main.go
```

### `/pkg` - Core Packages (The Heart of Tapio)

#### Level 0: `/pkg/domain` - Core Domain Types
The foundation - defines what an "event" is.

```
domain/
├── unified_event.go    # Core UnifiedEvent type
├── interfaces.go       # Core interfaces
├── validation/         # Event validation
└── event_converter.go  # Convert between event types
```

**Key Concept**: `UnifiedEvent` is our universal event format that all collectors produce.

#### Level 1: `/pkg/collectors` - Data Collectors
Where we gather data from various sources.

```
collectors/
├── cni/               # Container Network Interface
│   ├── internal/      # Implementation details
│   └── core/         # Interfaces and types
├── ebpf/             # Kernel-level monitoring
│   ├── internal/     # eBPF program management
│   └── linux/        # Linux-specific code
├── k8s/              # Kubernetes events
│   ├── internal/     # K8s API watchers
│   └── core/        # K8s event types
└── systemd/          # System logs
    ├── internal/     # Journal reader
    └── linux/        # Linux journald integration
```

**Each collector**:
- Has its own `core/` package with interfaces
- Implements the `Collector` interface
- Produces `UnifiedEvent` objects
- Can run independently

#### Level 2: `/pkg/intelligence` - Smart Analysis
Where events become insights.

```
intelligence/
├── analytics/         # Event analysis
│   └── engine/       # Analytics engine
├── correlation/       # Find related events
│   ├── semantic_correlation_engine.go
│   └── realtime.go   # Real-time correlation
└── context/          # Enrich events
    ├── impact.go     # Impact assessment
    └── scoring.go    # Event importance scoring
```

**Key Features**:
- Groups related events (e.g., all events from a failing deployment)
- Identifies root causes
- Scores event importance

#### Level 3: `/pkg/integrations` - External Systems
Managing the platform.

```
integrations/
├── collector-manager/  # Orchestrate collectors
│   └── manager.go     # Start/stop collectors
├── monitoring/        # Metrics & monitoring
│   └── metrics.go    # Prometheus integration
├── resilience/       # Fault tolerance
│   ├── circuit_breaker.go
│   └── retry.go
└── security/         # Security features
    ├── auth.go       # Authentication
    └── ratelimit.go  # Rate limiting
```

#### Level 4: `/pkg/interfaces` - User Interfaces
How users interact with Tapio.

```
interfaces/
├── server/grpc/       # gRPC API server
│   ├── tapio_service.go    # Main service
│   └── server.go          # Server setup
├── client/           # Client libraries
│   └── client.go    # Go client
├── cli/             # Command-line tools
└── logging/         # Structured logging
```

### `/proto` - API Definitions
Our gRPC/REST API contracts.

```
proto/
├── tapio/v1/         # Source .proto files
│   ├── events.proto  # Event service
│   └── correlations.proto
└── gen/             # Generated Go code
    └── tapio/v1/    # Don't edit these!
```

### `/ebpf` - Kernel Programs
Low-level C programs for kernel monitoring.

```
ebpf/
├── network_monitor.c  # Network packet tracking
├── oom_detector.c    # Memory pressure detection
└── headers/          # Kernel headers
```

### `/docs` - Documentation
Everything you need to know.

```
docs/
├── ARCHITECTURE.md    # System design
├── collectors/       # Per-collector guides
├── operations/       # Deployment & operations
└── architecture-history/  # Decision records
```

### `/deploy` - Deployment Files
Ready-to-use deployment configurations.

```
deploy/
├── k8s/              # Kubernetes manifests
├── helm/tapio/       # Helm chart
└── docker/           # Dockerfiles
```

## 🚀 Getting Started

### 1. Prerequisites
```bash
# Required
- Go 1.21+
- Docker (for eBPF development)
- Make

# Optional
- Kubernetes cluster (for K8s collector)
- Linux system (for eBPF/systemd collectors)
```

### 2. Initial Setup
```bash
# Clone the repo
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Install dependencies
go mod download

# Format code (ALWAYS do this before commits!)
make fmt

# Build everything
go build ./...

# Run tests
go test ./...
```

### 3. Running Tapio

**Option A: Run a specific collector**
```bash
# Run CNI collector only
go run cmd/tapio-collector/main.go --collectors=cni

# Run with multiple collectors
go run cmd/tapio-collector/main.go --collectors=cni,k8s,systemd
```

**Option B: Run the full stack**
```bash
# Terminal 1: Start the server
go run cmd/tapio-server/main.go

# Terminal 2: Start collectors
go run cmd/tapio-collector/main.go
```

### 4. Development Workflow

1. **Pick a collector to work on** (e.g., `pkg/collectors/cni`)
2. **Understand its structure**:
   - `core/` - Interfaces and types
   - `internal/` - Implementation
   - Tests alongside code
3. **Make changes**
4. **Test locally**:
   ```bash
   cd pkg/collectors/cni
   go test ./...
   ```
5. **Format and verify**:
   ```bash
   make fmt
   go build ./...
   go test ./...
   ```

## 📋 Common Tasks

### Adding a New Event Type
1. Define it in the collector's `core/types.go`
2. Convert to `UnifiedEvent` in the processor
3. Add tests

### Creating a New Collector
1. Create directory: `pkg/collectors/mynew/`
2. Add `core/interfaces.go` with `Collector` interface
3. Implement in `internal/collector.go`
4. Add to collector manager

### Debugging
```bash
# Run with debug logging
TAPIO_LOG_LEVEL=debug go run cmd/tapio-collector/main.go

# Run specific collector with verbose output
go run cmd/tapio-collector/main.go --collectors=ebpf --verbose
```

## 🏛️ Architecture Rules

### DO ✅
- Keep imports flowing downward (lower levels only)
- Write tests alongside code
- Use interfaces for dependencies
- Run `make fmt` before commits
- Each package should build independently

### DON'T ❌
- Import from same or higher levels
- Use `interface{}` in public APIs
- Leave TODO comments
- Skip tests
- Create circular dependencies

## 📚 Key Concepts

### UnifiedEvent
Our universal event format. All collectors produce these:
```go
type UnifiedEvent struct {
    ID        string
    Type      string
    Timestamp time.Time
    Source    string
    Message   string
    Metadata  map[string]interface{}
}
```

### Collector Interface
Every collector implements:
```go
type Collector interface {
    Start(context.Context) error
    Stop(context.Context) error
    GetMetrics() Metrics
}
```

### Production Hardening
All collectors include:
- Rate limiting
- Circuit breakers
- Resource monitoring
- Graceful degradation
- Comprehensive metrics

## 🆘 Need Help?

1. **Check the docs**: `/docs` has extensive documentation
2. **Read the tests**: Great examples of how to use each component
3. **Follow existing patterns**: Each collector has similar structure
4. **Ask questions**: Open an issue on GitHub

## 🎯 Next Steps

1. **Explore a collector**: Pick one from `/pkg/collectors` and understand it
2. **Run the examples**: Check `examples/` directories
3. **Read the architecture**: `docs/ARCHITECTURE.md`
4. **Try the full stack**: Get server + collectors running together

Welcome to Tapio! 🚀