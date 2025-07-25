# Migration Guide: Unified Orchestrator

This guide helps you migrate from the old multi-orchestrator architecture to the new unified orchestrator.

## Overview of Changes

### Before (Multiple Orchestrators)
- 4 different orchestrator implementations
- Missing dataflow components causing build failures
- Complex wiring between components
- Unclear which orchestrator to use when

### After (Unified Orchestrator)
- Single orchestrator for all event processing
- No missing dependencies
- Simple, clear architecture
- Better performance and monitoring

## Migration Steps

### 1. Update Imports

**Old:**
```go
import (
    "github.com/yairfalse/tapio/pkg/integrations/collector"
    "github.com/yairfalse/tapio/pkg/integrations/dataflow" // Missing!
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)
```

**New:**
```go
import (
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
    "github.com/yairfalse/tapio/pkg/integrations/collector"
    "github.com/yairfalse/tapio/pkg/integrations/server"
)
```

### 2. Replace Orchestrator Creation

**Old:**
```go
// Multiple orchestrators needed
collectorOrch := collector.NewOrchestrator(collectorConfig)
pipelineOrch := pipeline.NewOrchestrator(pipelineConfig)

// Missing dataflow
dataFlow := dataflow.NewTapioDataFlow(dataFlowConfig)
bridge := dataflow.NewServerBridge(bridgeConfig)
```

**New:**
```go
// Single unified orchestrator
config := pipeline.DefaultUnifiedConfig()
config.BufferSize = 20000
config.EnableCorrelation = true

orchestrator, err := pipeline.NewUnifiedOrchestrator(config)
```

### 3. Update Collector Integration

**Old:**
```go
manager := collector.NewCollectorManager()
manager.AddCollector("ebpf", ebpfCollector)

// Complex dataflow wiring
dataFlow.Connect(manager.Events(), outputChan)
```

**New:**
```go
// Direct integration with orchestrator
orchestrator.AddCollector("ebpf", ebpfCollector)

// Or use manager adapter for existing code
managerAdapter := &collector.ManagerAdapter{
    manager: existingManager,
}
orchestrator.AddCollector("manager", managerAdapter)
```

### 4. Update Server Integration

**Old:**
```go
serverOrch := server.NewOrchestrator(serverConfig)
// No clear way to connect to event processing
```

**New:**
```go
serverManager := server.NewServerManager(serverConfig)
serverManager.WithEventProcessor(orchestrator)
```

### 5. Simplify Lifecycle Management

**Old:**
```go
// Start everything separately
collectorOrch.Start(ctx)
dataFlow.Start()
pipelineOrch.Start(ctx)
serverOrch.Run(ctx)

// Complex shutdown
collectorOrch.Stop()
dataFlow.Stop()
pipelineOrch.Stop()
serverOrch.Shutdown()
```

**New:**
```go
// Start orchestrator (handles collectors and pipeline)
orchestrator.Start(ctx)

// Start server
go serverManager.Run(ctx)

// Simple shutdown
orchestrator.Stop()
serverManager.Shutdown(ctx)
```

### 6. Update Event Processing

**Old:**
```go
// Events scattered across components
for event := range outputEvents {
    // Process correlated events
}
```

**New:**
```go
// Single source for processed events
for event := range orchestrator.ProcessedEvents() {
    // All events are validated, enriched, and correlated
}
```

### 7. Update Monitoring

**Old:**
```go
// Metrics from multiple sources
collectorStats := collectorOrch.Statistics()
pipelineMetrics := pipelineOrch.GetMetrics()
// No unified view
```

**New:**
```go
// Single source for all metrics
metrics := orchestrator.GetMetrics()
health := orchestrator.GetHealth()

log.Printf("Events: %d, Throughput: %.2f/sec", 
    metrics.EventsProcessed, 
    metrics.ThroughputPerSecond)
```

## Configuration Changes

### Old Configuration Structure
```go
type CollectorConfig struct {
    ServerAddress   string
    CorrelationMode string
    // ...
}

type PipelineConfig struct {
    BatchSize   int
    WorkerCount int
    // ...
}
```

### New Unified Configuration
```go
type UnifiedConfig struct {
    // Core settings
    BufferSize int           // Replaces various buffer configs
    Workers    int           // Replaces WorkerCount
    
    // Pipeline settings
    EnableValidation  bool   // New: explicit validation control
    EnableContext     bool   // New: context building control
    EnableCorrelation bool   // Replaces CorrelationMode
    
    // Resilience settings
    ProcessingTimeout time.Duration
    ShutdownTimeout   time.Duration
    
    // Monitoring
    MetricsInterval time.Duration
}
```

## Common Migration Patterns

### Pattern 1: Simple Collector Setup

**Old:**
```go
func setupCollectors() {
    manager := collector.NewCollectorManager()
    // Add collectors
    // Complex dataflow setup
}
```

**New:**
```go
func setupCollectors(orchestrator *pipeline.UnifiedOrchestrator) {
    // Add collectors directly to orchestrator
    orchestrator.AddCollector("ebpf", ebpfCollector)
    orchestrator.AddCollector("k8s", k8sCollector)
}
```

### Pattern 2: Event Processing Pipeline

**Old:**
```go
func processingPipeline() {
    // Multiple channels and goroutines
    rawEvents := make(chan domain.Event)
    enrichedEvents := make(chan domain.Event)
    correlatedEvents := make(chan domain.Event)
    
    // Manual pipeline stages
}
```

**New:**
```go
func processingPipeline(orchestrator *pipeline.UnifiedOrchestrator) {
    // Orchestrator handles all pipeline stages internally
    for event := range orchestrator.ProcessedEvents() {
        // Events are already validated, enriched, and correlated
    }
}
```

### Pattern 3: Health Monitoring

**Old:**
```go
func healthCheck() {
    // Check each component separately
    collectorHealth := collector.Health()
    pipelineHealth := pipeline.Health()
    // Aggregate manually
}
```

**New:**
```go
func healthCheck(orchestrator *pipeline.UnifiedOrchestrator) {
    health := orchestrator.GetHealth()
    // Unified health status for entire system
    
    if health.Status() != domain.HealthHealthy {
        log.Printf("System degraded: %s", health.Message())
    }
}
```

## Troubleshooting

### Build Errors

**Error:** `undefined: dataflow`
**Solution:** Remove all dataflow imports and use UnifiedOrchestrator

**Error:** `cannot use domain.Event as domain.UnifiedEvent`
**Solution:** Update collectors to emit UnifiedEvent or use adapters

### Runtime Issues

**Issue:** Events not flowing
**Check:** 
- Orchestrator.Start() called?
- Collectors added before Start()?
- Context not cancelled?

**Issue:** High memory usage
**Check:**
- BufferSize configuration
- ProcessedEvents() channel being consumed?
- Metrics show dropped events?

## Performance Tuning

### Buffer Sizing
```go
config.BufferSize = 10000  // Default
config.BufferSize = 50000  // High throughput
config.BufferSize = 1000   // Low memory
```

### Worker Count
```go
config.Workers = 0              // Auto (NumCPU)
config.Workers = runtime.NumCPU() * 2  // CPU intensive
config.Workers = 4              // Fixed workers
```

### Timeout Configuration
```go
config.ProcessingTimeout = 5 * time.Second    // Default
config.ProcessingTimeout = 1 * time.Second    // Low latency
config.ProcessingTimeout = 30 * time.Second   // Complex processing
```

## Full Example

See `/pkg/integrations/examples/unified_architecture_example.go` for a complete working example.

## Support

If you encounter issues during migration:
1. Check the architecture documentation in `/pkg/intelligence/pipeline/UNIFIED_ARCHITECTURE.md`
2. Review the consolidation plan in `/pkg/intelligence/pipeline/ORCHESTRATOR_CONSOLIDATION_PLAN.md`
3. File an issue with your specific use case