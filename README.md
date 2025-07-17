# Tapio Observability Platform

<div align="center">

![Tapio Logo](https://img.shields.io/badge/Tapio-Observability%20Platform-blue?style=for-the-badge)

**Observability Platform with Semantic Correlation**

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

[Features](#features) • [Architecture](#architecture) • [Development](#development)

</div>

## Overview

Tapio is an observability platform that combines multi-source data collection with semantic correlation capabilities. The platform includes modular collectors for various data sources and a correlation engine that provides insights into system behavior.

## 🚀 Key Features

### **Semantic Correlation**
- **SemanticCorrelationEngine**: Advanced event analysis and correlation
- **Intent Classification**: Automatic categorization of system events
- **Real-time Processing**: Event correlation with configurable time windows
- **OTEL Integration**: OpenTelemetry traces with semantic enrichment

### **Multi-Source Collection**
- **eBPF Collector**: Kernel-level monitoring capabilities
- **Kubernetes Collector**: Cluster event monitoring
- **SystemD Collector**: Service monitoring and health tracking
- **JournalD Collector**: Structured log processing
- **CNI Collector**: Network event collection

### **Modular Architecture**
- **Independent Modules**: Each collector has its own go.mod
- **Pluggable Design**: Collectors can be enabled/disabled independently
- **Clean Interfaces**: Standardized event processing pipeline
- **Production Ready**: Built for reliability and maintainability

## 🏃 Development Setup

### Prerequisites
- Go 1.24+
- Git

### Build from Source
```bash
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Build collector
go build ./cmd/tapio-collector/

# Build server  
go build ./cmd/tapio-server/

# Build individual collector modules
go build ./pkg/collectors/ebpf/
go build ./pkg/collectors/k8s/
go build ./pkg/collectors/systemd/
go build ./pkg/collectors/journald/
```

## 🏗️ Architecture

Tapio follows a modular architecture with independent components:

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Collectors    │───▶│  Correlation     │───▶│    Server       │
│                 │    │     Engine       │    │                 │
│ • eBPF          │    │                  │    │ • gRPC API      │
│ • Kubernetes    │    │ • Semantic       │    │ • REST API      │
│ • SystemD       │    │ • Intent Class   │    │ • Health Checks │
│ • JournalD      │    │ • OTEL Traces    │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

### Repository Structure

```
pkg/
├── collectors/
│   ├── ebpf/           # go.mod - Kernel monitoring
│   ├── k8s/            # go.mod - Kubernetes events  
│   ├── systemd/        # go.mod - Service monitoring
│   ├── journald/       # go.mod - Log processing
│   └── cni/            # Network event collection
├── collector/          # Manager and correlation engine
├── correlation/        # Legacy correlation system
├── intelligence/
│   └── correlation/    # go.mod - Extracted semantic engine
├── domain/             # go.mod - Shared types
├── dataflow/           # Event routing
└── server/             # Server implementation

cmd/
├── tapio-collector/    # Collector service
├── tapio-server/       # Server service
├── tapio-gui/          # GUI application
└── tapio-cli/          # CLI tool
```

### Core Components

#### **SimpleManager** (`pkg/collector/manager.go`)
- Coordinates multiple collectors
- Manages event routing to correlation engine
- Provides health monitoring and statistics

#### **SemanticCorrelationEngine** (`pkg/collector/semantic_correlation_engine.go`)
- Processes events for semantic correlation
- Generates insights with intent classification
- Integrates with OpenTelemetry for trace enrichment

#### **Modular Collectors**
Each collector is independently buildable:
- **eBPF**: `pkg/collectors/ebpf/` - System-level event collection
- **Kubernetes**: `pkg/collectors/k8s/` - Cluster event monitoring
- **SystemD**: `pkg/collectors/systemd/` - Service health tracking
- **JournalD**: `pkg/collectors/journald/` - Structured log processing

## 🔧 Configuration

### Basic Collector Configuration
```yaml
# Example configuration structure
collectors:
  ebpf:
    enabled: true
    enable_memory: true
    enable_network: true
    
  kubernetes:
    enabled: true
    
  systemd:
    enabled: true

correlation:
  batch_size: 100
  batch_timeout: 100ms
```

*Note: Complete configuration examples available in source code*

## 🧪 Development

### Local Development Setup
```bash
# Clone repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Install dependencies
go mod download

# Build components
go build ./cmd/tapio-collector/
go build ./cmd/tapio-server/

# Test individual modules
go test ./pkg/collector/
go test ./pkg/collectors/ebpf/
go test ./pkg/collectors/k8s/
go test ./pkg/collectors/systemd/
go test ./pkg/collectors/journald/
```

### Module Development
Each collector module can be developed independently:
```bash
# Work on eBPF collector
cd pkg/collectors/ebpf
go build ./...
go test ./...

# Work on Kubernetes collector  
cd pkg/collectors/k8s
go build ./...
go test ./...
```

### Testing
```bash
# Test core collector functionality
go test ./pkg/collector/

# Test semantic correlation
go test ./pkg/collector/ -run TestSemanticCorrelation

# Test individual collectors
go test ./pkg/collectors/...
```

### Contributing
We welcome contributions! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/your-feature`
3. Make your changes and add tests
4. Ensure tests pass: `go test ./...`
5. Commit your changes: `git commit -m 'Add your feature'`
6. Push to the branch: `git push origin feature/your-feature`
7. Open a Pull Request

## 📋 Current Status

### Working Components
- ✅ **Modular collector architecture** with independent go.mod files
- ✅ **SemanticCorrelationEngine** integrated into SimpleManager
- ✅ **Core data flow** from collectors through correlation to server
- ✅ **eBPF, K8s, SystemD, JournalD collectors** with basic functionality
- ✅ **Server framework** with gRPC and REST API structure
- ✅ **CLI and GUI applications** (framework present)

### Development Areas
- 🔧 **Interface compatibility** - Some main service interfaces need updating
- 🔧 **Full feature implementation** - Core collectors need feature completion
- 🔧 **Configuration validation** - Enhanced config validation and examples
- 🔧 **Documentation** - Complete API documentation and examples
- 🔧 **Testing** - Comprehensive test suite expansion

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- The eBPF community for kernel observability foundations
- The OpenTelemetry project for observability standards  
- The Kubernetes community for container orchestration
- The Go community for excellent tooling and libraries

---

<div align="center">

**Built with Go and ❤️**

[Repository](https://github.com/yairfalse/tapio) • [Issues](https://github.com/yairfalse/tapio/issues)

</div>
