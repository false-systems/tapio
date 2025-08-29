# 🚀 Tapio Collectors

## Overview

Tapio collectors gather observability data from various sources in your Kubernetes and Linux environments. They use a **plug-and-play registration system** that makes adding collectors as simple as editing a YAML file.

## 🎯 Quick Start

### For Users: Enable Collectors via YAML

```yaml
# config.yaml
collectors:
  dns:
    enabled: true
    config:
      buffer_size: 1000
      enable_ebpf: true
  
  kernel:
    enabled: true
    config:
      buffer_size: 5000
```

That's it! Any collector with `enabled: true` will automatically start.

### For Developers: Add a New Collector

1. Create your collector package in `pkg/collectors/your-collector/`
2. Add an `init.go` file:

```go
package yourcollector

func init() {
    RegisterYourCollector()
}

func RegisterYourCollector() {
    factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
        // Create and return your collector
    }
    orchestrator.RegisterCollectorFactory("your-collector", factory)
}
```

3. Import it in `cmd/collectors/main_with_yaml.go`:

```go
_ "github.com/yairfalse/tapio/pkg/collectors/your-collector"
```

Done! Your collector now works with YAML config.

## 📦 Available Collectors

### Cross-Platform Collectors (Work on macOS/Linux)

| Collector | Purpose | Status |
|-----------|---------|--------|
| **cri** | Container Runtime Interface monitoring | ✅ Ready |
| **kubelet** | Kubernetes node metrics via Kubelet API | ✅ Ready |
| **otel** | OpenTelemetry integration | ✅ Ready |
| **etcd-metrics** | etcd cluster health monitoring | ✅ Ready |
| **kubeapi** | Kubernetes API events | ✅ Ready |

### Linux-Only Collectors (eBPF-based)

| Collector | Purpose | Status |
|-----------|---------|--------|
| **dns** | DNS query monitoring via eBPF | 🐧 Linux |
| **kernel** | Kernel events and syscalls | 🐧 Linux |
| **systemd** | SystemD service monitoring | 🐧 Linux |
| **network** | Network connections via eBPF | 🐧 Linux |
| **storage-io** | Disk I/O monitoring | 🐧 Linux |
| **runtime-signals** | Runtime signals detection | 🐧 Linux |
| **memory-leak-hunter** | Memory leak detection | 🐧 Linux |
| **resource-starvation** | Resource starvation detection | 🐧 Linux |
| **syscall-errors** | System call error tracking | 🐧 Linux |
| **cri-ebpf** | Container runtime via eBPF | 🐧 Linux |

## 🏗️ Architecture

### Registration Flow

```
init() → RegisterCollectorFactory() → YAML Config → Auto-create & Start
```

1. **Package init()**: Each collector registers its factory on import
2. **Factory Registry**: Orchestrator maintains a map of collector factories
3. **YAML Config**: User enables/configures collectors
4. **Auto-instantiation**: Orchestrator creates enabled collectors from factories

### Collector Interface

Every collector must implement:

```go
type Collector interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan *domain.CollectorEvent
    IsHealthy() bool
}
```

## 🔧 Configuration

### Global Orchestrator Config

```yaml
orchestrator:
  workers: 4          # Number of worker goroutines
  buffer_size: 10000  # Event buffer size
  log_level: "info"   # Logging level
  nats:
    url: "nats://localhost:4222"
    subject: "tapio.events"
```

### Collector-Specific Config

Each collector can have its own configuration:

```yaml
collectors:
  dns:
    enabled: true
    config:
      buffer_size: 500
      enable_ebpf: true
      # DNS-specific settings
  
  kernel:
    enabled: true
    config:
      buffer_size: 1000
      enable_ebpf: true
      syscall_sampling_rate: 100
```

## 🐧 Platform Limitations

### Why eBPF Collectors are Linux-Only

Most advanced collectors use **eBPF (Extended Berkeley Packet Filter)** for zero-overhead kernel observability. eBPF allows:

- Kernel event tracing without kernel modules
- Zero-copy data collection
- Minimal performance impact
- Deep system visibility

**macOS doesn't support eBPF**, so these collectors only work on Linux.

### Running on macOS

For development on macOS, use the cross-platform collectors (CRI, Kubelet, OTEL, etcd-metrics). These collectors use APIs instead of kernel instrumentation.

### Running on Linux

All collectors work on Linux. For production deployments, enable the eBPF-based collectors for comprehensive observability.

## 📝 Adding a New Collector

### Step 1: Create Collector Package

```bash
mkdir -p pkg/collectors/my-collector
```

### Step 2: Implement Collector

```go
// pkg/collectors/my-collector/collector.go
package mycollector

type Collector struct {
    name   string
    events chan *domain.CollectorEvent
    // ... your fields
}

func NewCollector(name string, config *Config) (*Collector, error) {
    // Implementation
}

func (c *Collector) Start(ctx context.Context) error { /* ... */ }
func (c *Collector) Stop() error { /* ... */ }
func (c *Collector) Events() <-chan *domain.CollectorEvent { /* ... */ }
func (c *Collector) IsHealthy() bool { /* ... */ }
func (c *Collector) Name() string { return c.name }
```

### Step 3: Create init.go

```go
// pkg/collectors/my-collector/init.go
package mycollector

import (
    "github.com/yairfalse/tapio/pkg/collectors"
    "github.com/yairfalse/tapio/pkg/collectors/orchestrator"
    "go.uber.org/zap"
)

func init() {
    RegisterMyCollector()
}

func RegisterMyCollector() {
    factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
        myConfig := DefaultConfig()
        
        // Map YAML config to your collector's config
        if config != nil {
            if config.BufferSize > 0 {
                myConfig.BufferSize = config.BufferSize
            }
            // ... map other fields
        }
        
        return NewCollector(name, myConfig)
    }
    
    orchestrator.RegisterCollectorFactory("my-collector", factory)
}
```

### Step 4: Import in main

```go
// cmd/collectors/main_with_yaml.go
import (
    // ... other imports
    _ "github.com/yairfalse/tapio/pkg/collectors/my-collector"
)
```

### Step 5: Configure in YAML

```yaml
collectors:
  my-collector:
    enabled: true
    config:
      buffer_size: 1000
      # your custom settings
```

## 🧪 Testing

### Unit Tests

Each collector should have comprehensive unit tests:

```bash
cd pkg/collectors/dns
go test -v ./...
```

### Integration Tests

For eBPF collectors, run as root on Linux:

```bash
sudo go test -tags=integration ./...
```

### Test Configuration

Use the provided `test-config.yaml`:

```bash
./tapio-collector -config test-config.yaml
```

## 🚨 Troubleshooting

### Collector Not Starting

1. Check if the collector is imported in `main_with_yaml.go`
2. Verify `enabled: true` in YAML config
3. Check logs for registration errors
4. On macOS, ensure you're not using Linux-only collectors

### eBPF Errors on Linux

1. Ensure you're running as root or with CAP_BPF capability
2. Check kernel version (>= 4.18 recommended)
3. Verify BPF is enabled in kernel config

### Configuration Issues

1. Validate YAML syntax
2. Check field names match CollectorConfigData struct
3. Review collector's init.go for config mapping

## 📚 Further Reading

- [Individual Collector READMEs](./*/README.md) - Detailed documentation per collector
- [Orchestrator Documentation](./orchestrator/README.md) - Pipeline and event flow
- [BPF Common Standards](./bpf_common/CORE_STANDARDS.md) - eBPF development guidelines

## 🤝 Contributing

1. Follow the collector pattern described above
2. Ensure cross-platform compatibility where possible
3. Add comprehensive tests
4. Update this README with your collector

## 📜 License

See main repository LICENSE file.