# Tapio Directory Structure

## Overview
Tapio follows a strict 5-level hierarchical architecture for its Go packages, ensuring clean dependencies and modular design.

## Root Directory Files
- `Claude.md` - AI assistant instructions and guidelines
- `Dockerfile`, `Dockerfile.dev` - Container build definitions
- `LICENSE` - Project license
- `Makefile` - Build automation
- `README.md` - Project overview
- `Taskfile.yml` - Task runner configuration
- `codecov.yml` - Code coverage configuration
- `skaffold.yaml` - Kubernetes development workflow
- `go.mod`, `go.sum` - Go module dependencies

## Directory Structure



- **`tapio-collector/`** - Main collector service that runs multiple collectors
- **`tapio-server/`** - gRPC/REST API server for event processing
- **`test-otel/`** - OpenTelemetry testing utilities

### `/pkg` - Core Go Packages (5-Level Architecture)

#### Level 0: `/pkg/domain` - Core Domain
Zero dependencies. Defines core types and interfaces.
- `UnifiedEvent` type definition
- Core interfaces and validation
- Event converter utilities

#### Level 1: `/pkg/collectors` - Data Collection
Depends only on domain. Implements various data collectors.

- **`cni/`** - Container Network Interface collector
  - Monitors container network events
  - Integrates with CNI plugins
- **`ebpf/`** - eBPF-based kernel collector
  - Low-level kernel event monitoring
  - Memory, network, and process tracking
- **`k8s/`** - Kubernetes event collector
  - Watches K8s API for cluster events
  - Pod, deployment, and service monitoring
- **`systemd/`** - SystemD journal collector
  - Collects system logs from journald
  - Service status monitoring

#### Level 2: `/pkg/intelligence` - Analytics & Correlation
Depends on domain + collectors. Provides event analysis.

- **`analytics/`** - Event analytics engine
  - Real-time event processing
  - Impact assessment
  - Confidence scoring
- **`correlation/`** - Semantic correlation engine
  - Groups related events
  - Root cause analysis
  - Pattern detection
- **`context/`** - Context enrichment
  - Adds metadata to events
  - Scoring and validation

#### Level 3: `/pkg/integrations` - External Integrations
Depends on domain + L1 + L2. Manages external systems.

- **`collector-manager/`** - Orchestrates multiple collectors
- **`monitoring/`** - Metrics and monitoring integration
- **`resilience/`** - Circuit breakers, retries, fault tolerance
- **`security/`** - Authentication, authorization, rate limiting

#### Level 4: `/pkg/interfaces` - User Interfaces
Depends on all lower levels. External-facing interfaces.

- **`server/grpc/`** - gRPC service implementation
- **`client/`** - Client libraries
- **`cli/`** - Command-line interface tools
- **`logging/`** - Structured logging configuration

### Additional `/pkg` Directories

- **`dataflow/`** - Event pipeline and streaming
- **`performance/`** - Performance optimization utilities
  - Ring buffers
  - Object pools
  - Per-CPU buffers

### `/proto` - Protocol Buffers
gRPC service definitions and generated code.

- `tapio/v1/` - Source .proto files
- `gen/tapio/v1/` - Generated Go code
- `buf.yaml` - Buf configuration for proto management

### `/ebpf` - eBPF Programs
C source code for kernel-level monitoring.

- `network_monitor.c` - Network packet monitoring
- `oom_detector.c` - Out-of-memory detection
- `headers/vmlinux.h` - Kernel headers

### `/docs` - Documentation
Comprehensive project documentation.

- Architecture decisions and rationale
- Collector-specific documentation
- Operations runbooks
- API documentation
- Migration guides


- **`architecture/`** - Architecture analysis tools
- **`coverage/`** - Code coverage reporting
- **`implementation/`** - Implementation helpers

### `/hack` - Development Scripts
Quick development setup scripts.

### `/install` - Installation Scripts
User-facing installation automation.

## Architecture Principles

1. **Strict Hierarchy**: Lower levels cannot import from higher levels
2. **Independent Modules**: Each module can build/test independently
3. **Domain-Driven**: Core business logic in domain layer
4. **Interface Segregation**: Clean interfaces between layers
5. **Production Ready**: Built-in monitoring, resilience, and security

## Getting Started

```bash
# Format code
make fmt

# Build all
go build ./...

# Run tests
go test ./...

# Run specific collector
go run cmd/tapio-collector/main.go
```
