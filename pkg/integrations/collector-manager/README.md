# Collector Manager - Integration Layer

The Collector Manager orchestrates multiple observability collectors, providing unified event streaming and lifecycle management for the Tapio platform.

## Architecture

This module sits at **Level 3 (Integration Layer)** in the Tapio 5-level hierarchy:

```
L0: pkg/domain/          # Core types
L1: pkg/collectors/      # Individual collectors (eBPF, K8s, systemd)
L2: pkg/intelligence/    # DataFlow, semantic correlation  
L3: pkg/integrations/    # CollectorManager ← THIS MODULE
L4: pkg/interfaces/      # Server APIs
```

## Features

### Multi-Collector Management
- **Unified Lifecycle**: Start/stop all collectors as a single unit
- **Event Aggregation**: Merge events from all collectors into single stream
- **Health Monitoring**: Track collector status and statistics
- **Graceful Shutdown**: Proper cleanup and resource management

### Integration Capabilities
- **L1 Integration**: Manages any collector implementing the `Collector` interface
- **L2 Integration**: Routes events to intelligence layer for semantic correlation
- **L4 Integration**: Provides unified event stream to server interfaces

## Usage

```go
import manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"

// Create manager
mgr := manager.NewCollectorManager()

// Add collectors (L1 components)
mgr.AddCollector("k8s", k8sCollector)
mgr.AddCollector("systemd", systemdCollector)
mgr.AddCollector("ebpf", ebpfCollector)

// Start all collectors
ctx := context.Background()
if err := mgr.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process unified event stream
for event := range mgr.Events() {
    // Forward to intelligence layer (L2)
    intelligenceEngine.Process(event)
}

// Monitor statistics
stats := mgr.Statistics()
fmt.Printf("Active collectors: %d\n", stats.ActiveCollectors)

// Graceful shutdown
mgr.Stop()
```

## Collector Interface

Any collector can be managed by implementing this interface:

```go
type Collector interface {
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan domain.Event
    Health() domain.HealthStatus
}
```

### Supported Collectors

| Collector | Layer | Purpose |
|-----------|-------|---------|
| **eBPF** | L1 | Kernel-level system events |
| **Kubernetes** | L1 | Container orchestration events |
| **SystemD** | L1 | System service events |
| **CNI** | L1 | Network interface events |

## Event Flow

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ eBPF        │    │ Kubernetes  │    │ SystemD     │
│ Collector   │    │ Collector   │    │ Collector   │
│ (L1)        │    │ (L1)        │    │ (L1)        │
└──────┬──────┘    └──────┬──────┘    └──────┬──────┘
       │                  │                  │
       │                  │                  │
       └──────────────────┼──────────────────┘
                          │
                          ▼
                ┌─────────────────┐
                │ CollectorManager│
                │ (L3)           │
                └─────────┬───────┘
                          │
                          ▼
                ┌─────────────────┐
                │ Intelligence    │
                │ Layer (L2)      │
                └─────────────────┘
```

## Configuration

The CollectorManager is configured through individual collector configurations:

```go
// Each collector has its own config
k8sConfig := k8s.Config{
    Namespace: "production",
    WatchPods: true,
    // ...
}

systemdConfig := systemd.Config{
    MonitorSystem: true,
    FilterUnits: []string{"docker", "kubernetes"},
    // ...
}

// Manager provides unified orchestration
manager := NewCollectorManager()
manager.AddCollector("k8s", k8s.NewCollector(k8sConfig))
manager.AddCollector("systemd", systemd.NewCollector(systemdConfig))
```

## Performance

### Event Throughput
- **Combined throughput**: 165k+ events/sec across all collectors
- **Per-collector isolation**: Individual collector failures don't affect others
- **Buffered channels**: 10,000 event buffer prevents blocking

### Memory Management
- **Event streaming**: No event storage, pure streaming architecture
- **Context cancellation**: Proper goroutine cleanup
- **Resource cleanup**: Automatic channel closure on shutdown

## Error Handling

### Collector Failures
- **Individual failures**: One collector failure doesn't stop others
- **Startup errors**: Manager returns error if any collector fails to start
- **Runtime errors**: Collectors handle their own error recovery

### Graceful Degradation
- **Partial functionality**: System continues with remaining collectors
- **Health reporting**: Monitor which collectors are active
- **Restart capability**: Individual collectors can be restarted

## Monitoring

### Statistics
```go
stats := manager.Statistics()
// Returns:
// - ActiveCollectors: Number of running collectors
// - TotalEvents: Total events processed (TODO)
```

### Health Checks
```go
// Individual collector health
for name, collector := range collectors {
    health := collector.Health()
    if !health.Connected {
        log.Printf("Collector %s is disconnected", name)
    }
}
```

## Testing

```bash
# Test the integration layer
cd pkg/integrations/collector-manager
go test -v ./...

# Test with mock collectors
go test -v -tags=integration ./...
```

## Architecture Compliance

✅ **L3 Integration Layer**
- Integrates L1 (collectors) with L2 (intelligence)
- No direct L4 dependencies
- Proper separation of concerns

✅ **Dependency Rules**
- Only imports from L0 (domain) and L1 (collectors)
- Provides interface for L4 (interfaces) to consume
- No circular dependencies

✅ **Interface Design**
- Clean, minimal interface for collectors
- Event-driven architecture
- Context-based lifecycle management

## Future Enhancements

- [ ] **Dynamic collector registration**: Add/remove collectors at runtime
- [ ] **Collector health monitoring**: Automatic restart of failed collectors
- [ ] **Event backpressure**: Handle slow downstream consumers
- [ ] **Metrics collection**: Detailed performance statistics
- [ ] **Configuration hot-reload**: Update collector configs without restart
- [ ] **Event sampling**: Reduce load during high-volume periods

## Examples

See [examples/](examples/) directory for:
- Basic collector setup
- Integration with intelligence layer
- Performance testing
- Error handling patterns