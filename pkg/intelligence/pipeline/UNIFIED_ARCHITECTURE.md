# Unified Orchestrator Architecture

## Overview

The Unified Orchestrator consolidates all event orchestration logic into a single, simple, and performant component. This replaces the previous fragmented approach with 4 different orchestrators and missing dataflow components.

## Architecture Design

### Core Principles

1. **Simplicity First**: Based on the clean CollectorManager design
2. **Performance When Needed**: Worker pools and batch processing available
3. **Resilience Built-in**: Health monitoring, metrics, graceful shutdown
4. **Clear Interfaces**: Well-defined boundaries between components

### Component Structure

```
┌─────────────────────────────────────────────────────────┐
│                   Collectors                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  │   eBPF   │ │   K8s    │ │ SystemD  │ │   CNI    │  │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘  │
│       │            │            │            │          │
│       └────────────┴────────────┴────────────┘          │
│                         │                                │
│                    UnifiedEvent                          │
└─────────────────────────┬────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────┐
│                 CollectorManager                          │
│  - Aggregates events from all collectors                 │
│  - Provides unified health status                        │
│  - Simple channel-based merging                          │
└─────────────────────────┬────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────┐
│               UnifiedOrchestrator                         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Core Components:                                 │    │
│  │ - Event routing with buffered channels          │    │
│  │ - Worker pool for parallel processing           │    │
│  │ - Health monitoring across all components       │    │
│  │ - Real-time metrics collection                  │    │
│  └─────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Configuration:                                   │    │
│  │ - BufferSize: 10000 (default)                   │    │
│  │ - Workers: NumCPU (default)                     │    │
│  │ - ProcessingTimeout: 5s                         │    │
│  │ - MetricsInterval: 10s                          │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────┬────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────┐
│             IntelligencePipeline                          │
│  - Validation Stage                                       │
│  - Context Building Stage                                 │
│  - Correlation Stage                                      │
│  - Analytics Stage                                        │
└─────────────────────────┬────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────┐
│                   Output Systems                          │
│  - gRPC Server                                           │
│  - Persistence/WAL                                       │
│  - Monitoring/Alerting                                   │
└───────────────────────────────────────────────────────────┘
```

## Key Interfaces

### Collector Interface
```go
type Collector interface {
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan *domain.UnifiedEvent
    Health() domain.HealthStatus
}
```

### Pipeline Interface
```go
type IntelligencePipeline interface {
    Start(ctx context.Context) error
    Stop() error
    ProcessEvent(event *domain.UnifiedEvent) error
}
```

## Data Flow

1. **Event Generation**: Collectors generate UnifiedEvent instances
2. **Aggregation**: CollectorManager merges all event streams
3. **Orchestration**: UnifiedOrchestrator routes events to workers
4. **Processing**: Workers process events through the pipeline
5. **Output**: Processed events available via ProcessedEvents channel

## Performance Characteristics

- **Throughput**: Designed for 165k+ events/sec
- **Latency**: Sub-millisecond processing per event
- **Memory**: Configurable buffers to prevent OOM
- **CPU**: Automatic scaling to available cores

## Configuration

```go
config := &UnifiedConfig{
    // Core settings
    BufferSize: 10000,        // Channel buffer size
    Workers:    0,            // 0 = NumCPU
    
    // Pipeline settings
    EnableValidation:  true,
    EnableContext:     true,
    EnableCorrelation: true,
    
    // Resilience settings
    ProcessingTimeout: 5 * time.Second,
    ShutdownTimeout:   30 * time.Second,
    
    // Monitoring
    MetricsInterval: 10 * time.Second,
}
```

## Health Monitoring

The orchestrator provides comprehensive health monitoring:

- **Component Health**: Each collector's health status
- **Pipeline Health**: Processing pipeline status
- **Overall Health**: Aggregated system health

Health states:
- `Healthy`: All components operational
- `Degraded`: Some components unhealthy but system functional
- `Unhealthy`: System not functional

## Metrics

Real-time metrics available via `GetMetrics()`:

```go
type UnifiedMetrics struct {
    EventsReceived   int64
    EventsProcessed  int64
    EventsDropped    int64
    ProcessingErrors int64
    
    TotalProcessingTime   time.Duration
    AverageProcessingTime time.Duration
    MaxProcessingTime     time.Duration
    ThroughputPerSecond   float64
}
```

## Usage Example

```go
// Create configuration
config := pipeline.DefaultUnifiedConfig()
config.BufferSize = 20000
config.Workers = 8

// Create orchestrator
orchestrator, err := pipeline.NewUnifiedOrchestrator(config)
if err != nil {
    log.Fatal(err)
}

// Add collectors
orchestrator.AddCollector("ebpf", ebpfCollector)
orchestrator.AddCollector("k8s", k8sCollector)

// Start processing
if err := orchestrator.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range orchestrator.ProcessedEvents() {
    // Handle processed events
}

// Graceful shutdown
orchestrator.Stop()
```

## Migration from Old Architecture

### Before (4 separate orchestrators):
1. `/pkg/integrations/collector/orchestrator.go` - Missing dataflow deps
2. `/pkg/integrations/server/orchestrator.go` - Complex service orchestration  
3. `/pkg/intelligence/pipeline/orchestrator.go` - Performance focused
4. `/pkg/integrations/collector-manager/` - Simple aggregation

### After (unified):
- Single orchestrator in `/pkg/intelligence/pipeline/unified_orchestrator.go`
- Combines best features from all previous implementations
- No missing dependencies
- Clear, maintainable code

## Benefits

1. **Simplicity**: Single orchestration point, clear data flow
2. **Performance**: Worker pools, batch processing, minimal overhead
3. **Resilience**: Health monitoring, graceful degradation, proper shutdown
4. **Maintainability**: 489 lines vs 1300+ lines across 4 files
5. **Flexibility**: Configurable for different workloads

## Future Enhancements

1. **Dynamic Scaling**: Auto-adjust workers based on load
2. **Back-pressure**: Implement flow control
3. **Circuit Breakers**: Protect against cascading failures
4. **Distributed Mode**: Support for multi-node deployments