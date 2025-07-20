# eBPF Collector Documentation

The eBPF collector provides deep kernel-level observability using eBPF (Extended Berkeley Packet Filter) technology, capturing system events with minimal overhead and unprecedented visibility into system behavior.

## Table of Contents

- [Architecture](#architecture)
- [Features](#features)
- [Dual-Path Processing](#dual-path-processing)
- [Event Types](#event-types)
- [Installation & Setup](#installation--setup)
- [Configuration](#configuration)
- [Usage Examples](#usage-examples)
- [Platform Support](#platform-support)
- [Security & Permissions](#security--permissions)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)
- [API Reference](#api-reference)

## Architecture

The eBPF collector implements a sophisticated dual-path architecture that preserves raw kernel events while enabling semantic correlation:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         eBPF Collector Architecture                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Kernel Space                                                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐│
│  │ Network BPF │  │ Process BPF │  │ File BPF    │  │Memory BPF  ││
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └─────┬──────┘│
│         │                 │                 │                │       │
│         └─────────────────┴─────────────────┴────────────────┘      │
│                                    │                                 │
│                          ┌─────────▼──────────┐                     │
│                          │  Ring Buffer Maps  │                     │
│                          └─────────┬──────────┘                     │
│ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ │
│  User Space                       │                                  │
│                          ┌────────▼─────────┐                       │
│                          │  Event Receiver  │                       │
│                          └────────┬─────────┘                       │
│                                   │                                  │
│                 ┌─────────────────┴────────────────┐                │
│                 │                                   │                │
│        ┌────────▼────────┐              ┌──────────▼─────────┐     │
│        │  Event Filter   │              │  Event Enricher    │     │
│        └────────┬────────┘              └──────────┬─────────┘     │
│                 │                                   │                │
│        ┌────────▼────────┐              ┌──────────▼─────────┐     │
│        │ Adaptive Sample │              │ Context Addition   │     │
│        └────────┬────────┘              └──────────┬─────────┘     │
│                 │                                   │                │
│      ┌──────────┴───────────────┬──────────────────┴──────────┐    │
│      │                          │                              │    │
│ ┌────▼─────┐          ┌────────▼────────┐          ┌─────────▼──┐ │
│ │Raw Events│          │Semantic Events  │          │Correlation │ │
│ │ Storage  │          │   (High-Value)  │          │  Engine    │ │
│ └──────────┘          └─────────────────┘          └────────────┘ │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Module Structure

```
pkg/collectors/ebpf/
├── go.mod                    # Independent module
├── core/                     # Public interfaces and types
│   ├── interfaces.go         # Collector contracts
│   ├── types.go             # eBPF event types
│   └── errors.go            # Error definitions
├── internal/                # Internal implementation
│   ├── collector.go         # Main collector logic
│   ├── processor.go         # Event processing pipeline
│   ├── stream.go           # Event streaming
│   ├── platform_linux.go   # Linux platform implementation
│   └── platform_other.go   # Non-Linux stub
├── linux/                   # Linux-specific eBPF programs
│   └── implementation.go    # BPF program loading and management
├── stub/                    # Stub for non-Linux platforms
│   └── implementation.go    # Graceful degradation
├── types.go                # Public event types
├── filter.go              # Advanced event filtering
├── enricher.go            # Event enrichment logic
├── processor.go           # Dual-path processor
├── tapio_client.go        # gRPC client for Tapio integration
├── raw_event_formatter.go # Human-readable event formatting
└── collector.go           # Public API exports
```

## Features

### Core Capabilities

- **Kernel-Level Visibility**: Direct observation of system calls, network packets, and kernel events
- **Zero-Copy Performance**: Ring buffer maps for efficient event transfer
- **Dual-Path Processing**: Preserve raw events while enabling semantic analysis
- **Adaptive Sampling**: Intelligent rate limiting based on event importance
- **Real-Time Streaming**: Bidirectional gRPC streaming to Tapio server
- **Context Enrichment**: Automatic addition of process, container, and Kubernetes metadata

### Event Categories

1. **Network Events**
   - TCP/UDP connections and state changes
   - DNS queries and responses
   - Network latency and packet loss
   - Protocol-specific analysis (HTTP, gRPC, etc.)

2. **Process Events**
   - Process lifecycle (fork, exec, exit)
   - System call monitoring
   - Signal handling
   - CPU and memory usage

3. **File System Events**
   - File operations (open, read, write, close)
   - Directory changes
   - Permission modifications
   - I/O patterns and performance

4. **Security Events**
   - Privilege escalations
   - Suspicious system calls
   - Network security violations
   - File access violations

5. **Container Events**
   - Container lifecycle
   - Namespace operations
   - cgroup changes
   - Container escapes

6. **Memory Events**
   - Memory allocations and deallocations
   - OOM (Out of Memory) events
   - Memory pressure
   - Page faults

## Dual-Path Processing

The eBPF collector implements a sophisticated dual-path architecture:

### Path 1: Raw Event Storage
- Preserves complete kernel event data
- Enables forensic analysis and debugging
- Supports custom tooling integration
- Maintains event fidelity

### Path 2: Semantic Processing
- Filters high-value events for correlation
- Enriches with context and metadata
- Integrates with Tapio's OTEL semantic correlation
- Enables real-time alerting

### Path 3: Correlation Integration
- Connects events across different sources
- Identifies causal relationships
- Supports root cause analysis
- Provides business impact assessment

## Event Types

### Raw Event Structure

```go
type RawEvent struct {
    Type      EventType `json:"type"`
    Timestamp uint64    `json:"timestamp"`
    CPU       uint32    `json:"cpu"`
    PID       uint32    `json:"pid"`
    TID       uint32    `json:"tid"`
    UID       uint32    `json:"uid"`
    GID       uint32    `json:"gid"`
    Comm      string    `json:"comm"`
    Details   interface{} `json:"details,omitempty"`
}
```

### Enriched Event Structure

```go
type EnrichedEvent struct {
    *RawEvent
    ProcessInfo   *ProcessContext   `json:"process_info,omitempty"`
    ContainerInfo *ContainerContext `json:"container_info,omitempty"`
    K8sInfo       *K8sContext      `json:"k8s_info,omitempty"`
    NetworkInfo   *NetworkContext  `json:"network_info,omitempty"`
    Severity      EventSeverity    `json:"severity"`
    Tags          []string         `json:"tags,omitempty"`
    Metadata      map[string]string `json:"metadata,omitempty"`
}
```

## Installation & Setup

### Prerequisites

- Linux kernel 4.18+ (5.8+ recommended for full features)
- BPF/BTF support enabled in kernel
- Root or CAP_SYS_ADMIN capability
- Go 1.24+ for building from source

### Kernel Configuration Check

```bash
# Check kernel version
uname -r

# Verify BPF support
grep CONFIG_BPF /boot/config-$(uname -r)

# Check BTF support
ls /sys/kernel/btf/vmlinux

# Verify required capabilities
getcap /usr/local/bin/tapio-collector
```

### Installation Methods

#### 1. From Source

```bash
# Clone repository
git clone https://github.com/yairfalse/tapio.git
cd tapio/pkg/collectors/ebpf

# Build collector
go build -o ebpf-collector ./cmd/collector

# Install with capabilities
sudo setcap cap_sys_admin=eip ./ebpf-collector
```

#### 2. Using Docker

```bash
docker run --privileged \
  --pid=host \
  --network=host \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  tapio/ebpf-collector:latest
```

#### 3. Kubernetes DaemonSet

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-ebpf-collector
  namespace: tapio-system
spec:
  selector:
    matchLabels:
      app: tapio-ebpf-collector
  template:
    metadata:
      labels:
        app: tapio-ebpf-collector
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: ebpf-collector
        image: tapio/ebpf-collector:latest
        securityContext:
          privileged: true
        volumeMounts:
        - name: sys-kernel-debug
          mountPath: /sys/kernel/debug
          readOnly: true
        - name: cgroup
          mountPath: /sys/fs/cgroup
          readOnly: true
      volumes:
      - name: sys-kernel-debug
        hostPath:
          path: /sys/kernel/debug
      - name: cgroup
        hostPath:
          path: /sys/fs/cgroup
```

## Configuration

### Default Configuration

```go
config := ebpf.DefaultConfig()
// Balanced configuration for general use
```

### High-Performance Configuration

```go
config := ebpf.HighPerformanceConfig()
// Optimized for high-throughput environments
// - Larger ring buffers
// - Aggressive filtering
// - Adaptive sampling enabled
```

### Security-Focused Configuration

```go
config := ebpf.SecurityConfig()
// Enhanced security event capture
// - All security-relevant syscalls
// - File integrity monitoring
// - Network security events
```

### Custom Configuration

```go
config := ebpf.Config{
    Name:            "production-ebpf",
    Enabled:         true,
    EventBufferSize: 10000,
    
    // Feature Selection
    Programs: ebpf.ProgramConfig{
        Network: ebpf.NetworkConfig{
            Enabled:          true,
            CapturePackets:   false,
            TrackConnections: true,
            MonitorDNS:       true,
            Protocols:        []string{"TCP", "UDP", "ICMP"},
        },
        Process: ebpf.ProcessConfig{
            Enabled:           true,
            TrackExec:         true,
            TrackForkExit:     true,
            MonitorSyscalls:   []string{"open", "connect", "execve"},
            ExcludeComms:      []string{"systemd-*", "kernel-*"},
        },
        File: ebpf.FileConfig{
            Enabled:         true,
            MonitorPaths:    []string{"/etc", "/var/log", "/home"},
            ExcludePaths:    []string{"/proc", "/sys"},
            CaptureContent:  false,
        },
        Memory: ebpf.MemoryConfig{
            Enabled:       true,
            TrackOOM:      true,
            MonitorMmap:   false,
            ThresholdMB:   1024,
        },
    },
    
    // Dual-Path Configuration
    DualPath: ebpf.DualPathConfig{
        Enabled:              true,
        RawStorageEnabled:    true,
        RawStoragePath:       "/var/lib/tapio/ebpf/raw",
        RawRetentionHours:    24,
        SemanticEnabled:      true,
        CorrelationEnabled:   true,
        ImportanceThreshold:  0.7,
    },
    
    // Performance Tuning
    Performance: ebpf.PerformanceConfig{
        RingBufferSizeMB:     16,
        EventRateLimit:       10000,
        CPULimit:            2,
        MemoryLimitMB:       512,
        SamplingInterval:     time.Millisecond * 100,
        AdaptiveSampling:     true,
    },
    
    // Filtering
    Filtering: ebpf.FilterConfig{
        MinSeverity:      ebpf.SeverityInfo,
        NamespaceFilter:  []string{"production", "staging"},
        ContainerFilter:  []string{"app-*", "service-*"},
        ProcessFilter:    []string{"nginx", "postgres", "redis"},
    },
    
    // Integration
    Integration: ebpf.IntegrationConfig{
        TapioServer:      "tapio-server:9090",
        StreamBatchSize:  100,
        StreamTimeout:    time.Second,
        EnableTLS:        true,
        TLSSkipVerify:    false,
    },
}
```

## Usage Examples

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

func main() {
    // Create collector
    config := ebpf.DefaultConfig()
    collector, err := ebpf.NewCollector(config)
    if err != nil {
        log.Fatal(err)
    }
    defer collector.Stop()
    
    // Start collection
    ctx := context.Background()
    if err := collector.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Process events
    for event := range collector.Events() {
        fmt.Printf("Event: Type=%s PID=%d Comm=%s\n", 
            event.Type, event.Context.ProcessID, event.Context.ProcessName)
    }
}
```

### Dual-Path Processing

```go
// Create dual-path processor
processor := ebpf.NewDualPathProcessor(
    ebpf.WithRawStorage("/var/lib/tapio/raw"),
    ebpf.WithSemanticFiltering(0.8),
    ebpf.WithCorrelation(tapioClient),
)

// Start processing
processor.Start(ctx)

// Access different paths
rawEvents := processor.RawEvents()      // All events
semanticEvents := processor.SemanticEvents() // High-value events

// Custom raw event handling
go func() {
    formatter := ebpf.NewRawEventFormatter(ebpf.FormatterConfig{
        ColorOutput: true,
        Verbose:     true,
    })
    
    for event := range rawEvents {
        fmt.Println(formatter.Format(event))
    }
}()
```

### Advanced Filtering

```go
// Create custom filter
filter := ebpf.NewEventFilter(ebpf.FilterConfig{
    Rules: []ebpf.FilterRule{
        {
            Type:     ebpf.EventTypeNetwork,
            Subtype:  ebpf.NetworkConnect,
            MinSeverity: ebpf.SeverityWarning,
            Conditions: map[string]interface{}{
                "dest_port": []int{22, 443, 3306},
                "protocol":  "TCP",
            },
        },
        {
            Type:    ebpf.EventTypeProcess,
            Subtype: ebpf.ProcessExec,
            ProcessNameRegex: "^(wget|curl|nc|ncat)$",
        },
    },
    AdaptiveSampling: ebpf.AdaptiveSamplingConfig{
        Enabled:         true,
        BaseRate:        0.1,
        ImportanceBoost: 10.0,
        BurstProtection: true,
    },
})

collector.SetFilter(filter)
```

### Integration with Tapio

```go
// Create Tapio client
tapioClient := ebpf.NewTapioGRPCClient(ebpf.TapioClientConfig{
    ServerAddress: "tapio-server:9090",
    EnableTLS:     true,
    BatchSize:     100,
    StreamTimeout: time.Second,
})

// Connect client to collector
collector.SetOutput(tapioClient)

// Or use the builder pattern
collector := ebpf.NewCollector(config).
    WithFilter(filter).
    WithEnricher(enricher).
    WithOutput(tapioClient).
    Build()
```

## Platform Support

### Linux

Full eBPF functionality with kernel version requirements:

| Feature | Minimum Kernel | Recommended |
|---------|---------------|-------------|
| Basic eBPF | 4.9 | 5.8+ |
| BTF Support | 5.2 | 5.8+ |
| Ring Buffer | 5.8 | 5.10+ |
| BPF LSM | 5.7 | 5.10+ |
| Sleepable BPF | 5.10 | 5.15+ |

### Container Environments

| Platform | Support Level | Notes |
|----------|--------------|-------|
| Docker | Full | Requires --privileged |
| Kubernetes | Full | Requires privileged DaemonSet |
| containerd | Full | Requires privileged |
| Podman | Full | Requires --privileged |

### Non-Linux Platforms

The collector provides graceful degradation:

```go
// On macOS/Windows
collector, err := ebpf.NewCollector(config)
// err: "eBPF collector is only supported on Linux"

// Check platform support
if ebpf.IsSupported() {
    // Full eBPF functionality
} else {
    // Use alternative collection method
}
```

## Security & Permissions

### Required Capabilities

The eBPF collector requires elevated privileges:

1. **CAP_SYS_ADMIN**: Load BPF programs
2. **CAP_NET_ADMIN**: Network program attachment
3. **CAP_PERFMON**: Performance monitoring (kernel 5.8+)
4. **CAP_BPF**: BPF operations (kernel 5.8+)

### Security Best Practices

1. **Least Privilege**
   ```bash
   # Grant only required capabilities
   sudo setcap cap_sys_admin,cap_net_admin=eip ./ebpf-collector
   ```

2. **Namespace Isolation**
   ```yaml
   securityContext:
     allowPrivilegeEscalation: false
     readOnlyRootFilesystem: true
     capabilities:
       add: ["SYS_ADMIN", "NET_ADMIN"]
       drop: ["ALL"]
   ```

3. **Resource Limits**
   ```bash
   # Set locked memory limit
   ulimit -l unlimited
   
   # Or in systemd
   LimitMEMLOCK=infinity
   ```

4. **BPF Program Verification**
   - All programs pass kernel verifier
   - No unbounded loops
   - Memory safety guaranteed
   - No kernel data leaks

### Security Monitoring

The collector can monitor its own security:

```go
health := collector.Health()
fmt.Printf("BPF Programs Loaded: %d\n", health.LoadedPrograms)
fmt.Printf("Verification Failures: %d\n", health.VerificationFailures)
fmt.Printf("Security Violations: %d\n", health.SecurityViolations)
```

## Performance Tuning

### Memory Optimization

```go
// Configure memory limits
config.Performance.MemoryLimitMB = 256
config.Performance.RingBufferSizeMB = 8
config.Performance.MapEntriesLimit = 10000

// Monitor memory usage
stats := collector.Stats()
fmt.Printf("Memory Usage: %d MB\n", stats.MemoryUsageMB)
fmt.Printf("Ring Buffer Usage: %.2f%%\n", stats.RingBufferUsagePercent)
```

### CPU Optimization

```go
// Limit CPU usage
config.Performance.CPULimit = 1.5 // 1.5 cores
config.Performance.SamplingInterval = time.Millisecond * 100

// Use BPF sampling
config.Programs.Process.SamplingRate = 0.1 // Sample 10% of events
```

### Event Rate Management

```go
// Configure rate limiting
config.Performance.EventRateLimit = 5000 // events/sec
config.Performance.BurstSize = 1000

// Adaptive sampling
config.Filtering.AdaptiveSampling = ebpf.AdaptiveSamplingConfig{
    Enabled:          true,
    BaseRate:         0.01,  // 1% baseline
    ImportanceBoost:  100.0, // 100x for important events
    BurstProtection:  true,
    BurstThreshold:   10000,
}
```

### Benchmarking

```bash
# Run performance benchmarks
cd pkg/collectors/ebpf
go test -bench=. -benchmem

# Profile CPU usage
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Profile memory usage
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

## Troubleshooting

### Common Issues

#### 1. Permission Denied

**Error**: `operation not permitted`

**Solutions**:
```bash
# Run as root
sudo ./ebpf-collector

# Or add capabilities
sudo setcap cap_sys_admin=eip ./ebpf-collector

# Check current capabilities
getcap ./ebpf-collector
```

#### 2. BPF Program Load Failures

**Error**: `failed to load BPF program: invalid argument`

**Solutions**:
```bash
# Check kernel version
uname -r

# Verify BPF support
ls /sys/fs/bpf

# Check kernel config
zgrep CONFIG_BPF /proc/config.gz

# Enable BPF JIT (performance)
echo 1 > /proc/sys/net/core/bpf_jit_enable
```

#### 3. Ring Buffer Errors

**Error**: `failed to create ring buffer`

**Solutions**:
```bash
# Increase locked memory limit
ulimit -l unlimited

# Or in /etc/security/limits.conf
* hard memlock unlimited
* soft memlock unlimited

# Check current limit
ulimit -l
```

#### 4. High CPU Usage

**Diagnostics**:
```bash
# Check BPF program stats
bpftool prog show

# Monitor map usage
bpftool map show

# Profile specific program
bpftool prog profile id <ID> duration 10
```

**Solutions**:
- Enable sampling
- Increase sampling interval
- Use more specific filters
- Enable adaptive sampling

#### 5. Missing Events

**Diagnostics**:
```go
// Check drop statistics
stats := collector.Stats()
fmt.Printf("Events Dropped: %d\n", stats.EventsDropped)
fmt.Printf("Ring Buffer Full: %d\n", stats.RingBufferFull)
```

**Solutions**:
- Increase ring buffer size
- Reduce event rate
- Enable sampling
- Add event filtering

### Debug Tools

#### BPF Tooling

```bash
# List loaded programs
bpftool prog list

# Show program details
bpftool prog show id <ID>

# Dump program instructions
bpftool prog dump xlated id <ID>

# Monitor map contents
bpftool map dump id <ID>

# Trace BPF events
bpftool prog tracelog
```

#### Collector Debugging

```go
// Enable debug logging
config.Debug = ebpf.DebugConfig{
    Enabled:        true,
    LogLevel:       "trace",
    DumpPrograms:   true,
    DumpMaps:       true,
    TraceEvents:    true,
    ProfileEnabled: true,
}

// Access debug information
debug := collector.Debug()
fmt.Printf("Loaded Programs: %v\n", debug.LoadedPrograms)
fmt.Printf("Active Maps: %v\n", debug.ActiveMaps)
fmt.Printf("Event Pipeline: %v\n", debug.EventPipeline)
```

#### System Diagnostics

```bash
# Check kernel messages
dmesg | grep -i bpf

# Monitor system calls
strace -e bpf ./ebpf-collector

# Check CPU usage by BPF
perf top -p $(pgrep ebpf-collector)

# Analyze flame graphs
perf record -F 99 -p $(pgrep ebpf-collector) -g -- sleep 30
perf script | stackcollapse-perf.pl | flamegraph.pl > flame.svg
```

### Production Checklist

Before deploying to production:

- [ ] Kernel version verified (5.8+ recommended)
- [ ] Security capabilities configured
- [ ] Resource limits set appropriately
- [ ] Monitoring and alerting configured
- [ ] Event filtering rules tested
- [ ] Performance benchmarks completed
- [ ] Failure recovery tested
- [ ] Security audit performed

## API Reference

### Core Types

```go
// Collector is the main eBPF collector interface
type Collector interface {
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan domain.Event
    Stats() Statistics
    Health() HealthStatus
    SetFilter(filter EventFilter)
    SetOutput(output EventOutput)
}

// EventFilter defines filtering rules
type EventFilter interface {
    Match(event *RawEvent) FilterDecision
    UpdateRules(rules []FilterRule) error
    GetStats() FilterStats
}

// EventEnricher adds context to events
type EventEnricher interface {
    Enrich(event *RawEvent) (*EnrichedEvent, error)
    AddProvider(provider EnrichmentProvider)
}

// EventOutput handles event destinations
type EventOutput interface {
    Send(events []domain.Event) error
    SendRaw(events []*RawEvent) error
    Close() error
}
```

### Configuration Options

See the [Configuration](#configuration) section for detailed options.

### Event Types

See the [Event Types](#event-types) section for event structures.

## Contributing

See [CONTRIBUTING.md](../../../CONTRIBUTING.md) for guidelines.

## License

This project is licensed under the terms specified in the [LICENSE](../../../LICENSE) file.