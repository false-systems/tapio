# Node Runtime Observer

**Status: Production Ready**

## Overview

The Node Runtime observer monitors the health and performance of Kubernetes nodes by tracking critical system-level metrics and runtime conditions. It provides early warning signals for node-level issues that could impact workload stability.

## What This Observer Does

- **Node Health Monitoring**: Tracks node conditions (Ready, MemoryPressure, DiskPressure, PIDPressure)
- **Resource Utilization**: Monitors CPU, memory, disk, and network usage at the node level
- **Kubelet Health**: Tracks kubelet status and performance metrics
- **Container Runtime**: Monitors Docker/containerd daemon health
- **System Services**: Tracks critical system services (systemd units)
- **Kernel Metrics**: Monitors kernel-level statistics and pressures

## Features

- ✅ Real-time node condition monitoring
- ✅ Resource pressure detection
- ✅ Kubelet performance tracking
- ✅ Container runtime health checks
- ✅ Network connectivity monitoring
- ✅ Disk space and inode tracking
- ✅ Process and thread monitoring
- ✅ System load tracking

## Architecture

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   Node Metrics   │     │   Kubelet API    │     │  System Probes   │
│                  │     │                  │     │                  │
│  CPU/Memory/Disk │────▶│  Node Status     │────▶│  Service Health  │
│  Network/IO      │     │  Pod Statistics  │     │  Runtime Status  │
└──────────────────┘     └──────────────────┘     └──────────────────┘
           │                      │                         │
           └──────────────────────┼─────────────────────────┘
                                  ▼
                        ┌──────────────────┐
                        │   Node Runtime   │
                        │     Observer     │
                        └──────────────────┘
                                  │
                                  ▼
                        ┌──────────────────┐
                        │     Events       │
                        └──────────────────┘
```

## Events Generated

```go
domain.EventTypeNodeNotReady       // Node transitions to NotReady
domain.EventTypeNodeMemoryPressure // Memory pressure detected
domain.EventTypeNodeDiskPressure   // Disk pressure detected
domain.EventTypeNodePIDPressure    // PID exhaustion risk
domain.EventTypeNodeNetworkIssue   // Network connectivity problems
```

## Configuration

```go
type Config struct {
    Name                string        // Observer name
    BufferSize          int          // Event buffer size
    
    // Monitoring intervals
    NodeCheckInterval   time.Duration // Node status check interval (default: 30s)
    MetricsInterval     time.Duration // Metrics collection interval (default: 15s)
    
    // Thresholds
    CPUThreshold        float64      // CPU usage threshold (default: 80%)
    MemoryThreshold     float64      // Memory usage threshold (default: 85%)
    DiskThreshold       float64      // Disk usage threshold (default: 90%)
    
    // Features
    EnableKubeletCheck  bool         // Monitor kubelet health (default: true)
    EnableRuntimeCheck  bool         // Monitor container runtime (default: true)
    EnableSystemdCheck  bool         // Monitor systemd services (default: true)
}
```

## Usage Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/observers/node-runtime"
)

func main() {
    config := noderuntime.NewDefaultConfig("node-runtime")
    config.NodeCheckInterval = 20 * time.Second
    config.MemoryThreshold = 90.0
    
    observer, err := noderuntime.NewObserver("node-runtime", config)
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    if err := observer.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Process events
    for event := range observer.Events() {
        switch event.Type {
        case domain.EventTypeNodeNotReady:
            log.Printf("CRITICAL: Node %s is not ready", event.K8sContext.NodeName)
        case domain.EventTypeNodeMemoryPressure:
            log.Printf("WARNING: Memory pressure on node %s", event.K8sContext.NodeName)
        }
    }
}
```

## Metrics (OpenTelemetry)

```
# Node conditions
node_runtime_condition{node, condition, status}

# Resource utilization
node_runtime_cpu_usage_percent{node}
node_runtime_memory_usage_percent{node}
node_runtime_disk_usage_percent{node, device}

# Service health
node_runtime_service_healthy{node, service}

# Event metrics
node_runtime_events_total{node, event_type}
node_runtime_errors_total{node, error_type}
```

## Integration with Other Observers

The Node Runtime observer provides crucial context for other observers:

- **Container Runtime**: Node conditions affect container scheduling
- **Scheduler**: Node pressure impacts scheduling decisions
- **Storage I/O**: Disk pressure correlates with I/O performance
- **Network**: Network issues detected at node level

## Platform Support

- ✅ Linux (all distributions with systemd)
- ✅ Kubernetes nodes (with kubelet API access)
- ⚠️ macOS (limited functionality, for development only)
- ❌ Windows (not supported)

## Performance Impact

- **CPU**: < 0.5% overhead
- **Memory**: ~20MB baseline
- **Network**: Minimal (local API calls only)

## Troubleshooting

### Common Issues

1. **Cannot access kubelet API**
   - Ensure kubelet is configured with `--read-only-port=10255` or proper authentication
   - Check firewall rules for port 10250 (secure) or 10255 (read-only)

2. **Missing system metrics**
   - Verify `/proc` and `/sys` filesystems are accessible
   - Check permissions for reading system files

3. **Systemd service monitoring fails**
   - Ensure systemd D-Bus socket is accessible
   - Verify the observer has permissions to query systemd

## Testing

```bash
# Run unit tests
go test ./pkg/observers/node-runtime/...

# Run with mock data
go test -tags=mock ./pkg/observers/node-runtime/...

# Benchmark performance
go test -bench=. ./pkg/observers/node-runtime/...
```

## Dependencies

- Kubernetes client-go (for node API access)
- Prometheus client (for metrics export)
- Go-systemd (for systemd monitoring)
- Procfs (for system metrics)