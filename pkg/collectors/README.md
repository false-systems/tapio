# Tapio Collector Manager

The Collector Manager coordinates multiple data collectors (K8s, eBPF, systemd) and provides a unified event stream for the intelligence pipeline.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Collector Manager                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │ K8s         │  │ eBPF        │  │ Systemd     │  ...   │
│  │ Collector   │  │ Collector   │  │ Collector   │        │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘        │
│         │                 │                 │                │
│         └─────────────────┴─────────────────┘                │
│                           │                                   │
│                    Merged Event Stream                        │
│                           │                                   │
└───────────────────────────┴───────────────────────────────────┘
                            │
                            ▼
                    Intelligence Pipeline
```

## Features

- **Unified Interface**: Common interface for all collector types
- **Event Merging**: Combines events from multiple collectors into a single stream
- **Health Monitoring**: Tracks health status of all collectors
- **Statistics Aggregation**: Provides aggregated metrics across all collectors
- **Graceful Lifecycle**: Coordinated start/stop of all collectors
- **Backpressure Handling**: Drops events when buffer is full to prevent memory issues

## Usage

### Basic Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors"
    k8score "github.com/yairfalse/tapio/pkg/collectors/k8s/core"
)

func main() {
    // Create manager
    config := collectors.DefaultManagerConfig()
    manager := collectors.NewManager(config)
    
    // Create and register collectors
    k8sConfig := k8score.Config{
        Name:            "k8s-main",
        Enabled:         true,
        EventBufferSize: 10000,
        InCluster:       true,
        WatchPods:       true,
        WatchServices:   true,
    }
    
    k8sCollector, err := collectors.CreateK8sCollector("k8s", k8sConfig)
    if err != nil {
        log.Fatal(err)
    }
    
    if err := manager.Register("k8s", k8sCollector); err != nil {
        log.Fatal(err)
    }
    
    // Start manager
    ctx := context.Background()
    if err := manager.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer manager.Stop()
    
    // Process events
    events := manager.Events()
    for event := range events {
        log.Printf("Event: %s from %s", event.ID, event.Source)
    }
}
```

### Running the Manager

The manager can be run as a standalone service:

```bash
# Run with default configuration (K8s collector only)
go run cmd/manager/main.go

# Enable specific collectors
ENABLE_K8S_COLLECTOR=true \
ENABLE_EBPF_COLLECTOR=true \
ENABLE_SYSTEMD_COLLECTOR=true \
go run cmd/manager/main.go

# Run in Kubernetes cluster
kubectl apply -f deployments/collector-manager.yaml
```

## Configuration

### Manager Configuration

```go
type ManagerConfig struct {
    // Event buffer size for merged stream
    EventBufferSize int
    
    // Health check interval
    HealthCheckInterval time.Duration
    
    // Resource limits (shared across all collectors)
    MaxMemoryMB int
    MaxCPUMilli int
}
```

### Environment Variables

- `ENABLE_K8S_COLLECTOR`: Enable Kubernetes collector (default: true)
- `ENABLE_EBPF_COLLECTOR`: Enable eBPF collector (default: false)
- `ENABLE_SYSTEMD_COLLECTOR`: Enable systemd collector (default: false)

## Collector Adapters

Each collector type has an adapter that implements the common `CollectorInterface`:

- `K8sCollectorAdapter`: Adapts Kubernetes collector
- `EBPFCollectorAdapter`: Adapts eBPF collector
- `SystemdCollectorAdapter`: Adapts systemd collector

## Health Monitoring

The manager provides health status for all collectors:

```go
health := manager.Health()
for name, h := range health {
    fmt.Printf("%s: %s - %s\n", name, h.Status, h.Message)
}
```

Health statuses:
- `healthy`: Collector is functioning normally
- `degraded`: Collector is experiencing issues but still operational
- `unhealthy`: Collector has failed or is not responding
- `unknown`: Health status cannot be determined

## Statistics

Get aggregated statistics:

```go
stats := manager.Statistics()
for name, s := range stats {
    fmt.Printf("%s: Events=%d, Dropped=%d\n", 
        name, s.EventsCollected, s.EventsDropped)
}
```

## Integration with Intelligence Pipeline

The manager's event stream can be connected to the intelligence pipeline:

```go
// Create manager and start collectors
manager := setupCollectorManager()

// Create intelligence pipeline
pipeline := setupIntelligencePipeline()

// Connect events
go func() {
    events := manager.Events()
    for event := range events {
        if err := pipeline.Process(ctx, event); err != nil {
            log.Printf("Pipeline error: %v", err)
        }
    }
}()
```

## Testing

Run the test suite:

```bash
go test ./...
```

The package includes comprehensive tests for:
- Manager lifecycle (start/stop)
- Event forwarding and merging
- Health monitoring
- Statistics aggregation
- Error handling

## Performance Considerations

1. **Event Buffer Size**: Configure based on expected event rate
2. **Health Check Interval**: Balance between responsiveness and overhead
3. **Resource Limits**: Set appropriate limits for your environment
4. **Collector Selection**: Only enable collectors you need

## Troubleshooting

### No Events Received

1. Check collector health: `manager.Health()`
2. Verify collectors are registered: `manager.CollectorCount()`
3. Check individual collector logs
4. Ensure collectors have necessary permissions

### High Memory Usage

1. Reduce `EventBufferSize`
2. Check for event processing bottlenecks
3. Monitor drop rates in statistics
4. Consider enabling fewer collectors

### Collector Failures

1. Check collector-specific configuration
2. Verify required resources (K8s API access, eBPF permissions)
3. Review error messages in health status
4. Check collector logs for details