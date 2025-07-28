# Collector Intelligence Pipeline

The Collector Intelligence Pipeline connects the data collection layer to the intelligence/correlation layer, enabling real-time event correlation and semantic analysis.

## Architecture

```
┌─────────────────┐     ┌────────────────┐     ┌──────────────────┐
│   Collectors    │────▶│  Collector     │────▶│  Intelligence    │
│  (K8s, eBPF,   │     │   Manager      │     │   Pipeline       │
│   Systemd)      │     │                │     │                  │
└─────────────────┘     └────────────────┘     └──────────────────┘
                              │                         │
                              ▼                         ▼
                        ┌──────────┐            ┌──────────────┐
                        │  Event   │            │ Correlation  │
                        │  Stream  │            │   Engine     │
                        └──────────┘            └──────────────┘
                                                       │
                                                       ▼
                                                ┌──────────────┐
                                                │  Findings &  │
                                                │   Insights   │
                                                └──────────────┘
```

## Features

- **Event Stream Processing**: Processes events from multiple collectors in real-time
- **Event Enrichment**: Adds semantic context, entity information, and impact analysis
- **Batch Processing**: Configurable batch size and timeout for optimized throughput
- **Correlation Integration**: Seamlessly integrates with the correlation engine
- **OTEL Trace Context**: Preserves and utilizes OpenTelemetry trace context for correlation
- **Semantic Grouping**: Groups related events based on trace context and semantic analysis

## Usage

### Basic Setup

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors"
    "github.com/yairfalse/tapio/pkg/integrations/pipeline"
    "go.uber.org/zap"
)

func main() {
    // Create logger
    logger, _ := zap.NewProduction()
    defer logger.Sync()
    
    // Create collector manager
    manager := collectors.NewManager(collectors.DefaultManagerConfig())
    
    // Create pipeline with configuration
    config := pipeline.DefaultConfig()
    config.EnrichmentEnabled = true
    config.BatchSize = 100
    config.BatchTimeout = 5 * time.Second
    
    collectorPipeline, err := pipeline.NewCollectorIntelligencePipeline(
        manager, logger, config)
    if err != nil {
        log.Fatal(err)
    }
    
    // Register collectors
    // ... register your collectors ...
    
    // Start manager and pipeline
    ctx := context.Background()
    manager.Start(ctx)
    collectorPipeline.Start()
    
    // Process events...
    
    // Shutdown
    collectorPipeline.Stop()
    manager.Stop()
}
```

### Configuration Options

```go
type Config struct {
    // Enable event enrichment (adds semantic context, entities, impact)
    EnrichmentEnabled bool
    
    // Number of events to batch before processing
    BatchSize int
    
    // Maximum time to wait before processing a partial batch
    BatchTimeout time.Duration
}
```

### Event Enrichment

The pipeline automatically enriches events based on their type:

1. **Kubernetes Events**:
   - Adds entity information (pod, deployment, service)
   - Infers impact based on event type (OOM kills, crashes, etc.)
   - Preserves namespace and label context

2. **Log Events**:
   - Maps log levels to severity
   - Extracts service information
   - Adds application entity context

3. **Network Events**:
   - Creates connection entities
   - Preserves source/destination information

4. **System Events**:
   - Adds node entity information
   - Maps system metrics to impact

### Monitoring

The pipeline provides comprehensive statistics:

```go
stats := pipeline.GetStatistics()
// Returns:
// - processed_events: Total events processed
// - correlation_errors: Number of correlation failures
// - enrichment_enabled: Whether enrichment is active
// - batch_size: Current batch configuration
// - correlation_stats: Detailed correlation statistics
```

### Finding Correlations

Access correlation findings and semantic groups:

```go
// Get latest correlation findings
findings := pipeline.GetLatestFindings()
if findings != nil {
    fmt.Printf("Pattern: %s\n", findings.PatternType)
    fmt.Printf("Confidence: %.2f\n", findings.Confidence)
    fmt.Printf("Description: %s\n", findings.Description)
}

// Get semantic groups (OTEL trace-based grouping)
groups := pipeline.GetSemanticGroups()
for _, group := range groups {
    fmt.Printf("Group %s: %s\n", group.ID, group.Intent)
}
```

## Example: Full Integration

See `cmd/example/main.go` for a complete example that demonstrates:
- Setting up the pipeline
- Registering collectors
- Processing events with correlation
- Monitoring pipeline health
- Graceful shutdown

## Testing

The pipeline includes comprehensive tests:

```bash
# Run all pipeline tests
go test -tags experimental ./pkg/integrations/pipeline/...

# Run with verbose output
go test -v -tags experimental ./pkg/integrations/pipeline/...

# Run specific test
go test -tags experimental -run TestCollectorIntelligencePipeline_EventProcessing ./pkg/integrations/pipeline/
```

## Performance Considerations

1. **Batch Size**: Larger batches improve throughput but increase latency
2. **Enrichment**: Disable enrichment if not needed to reduce CPU usage
3. **Event Buffer**: The collector manager has its own buffer; coordinate sizes
4. **Correlation Load**: Complex correlation patterns may require tuning

## Troubleshooting

### No Events Flowing
- Check collector health: `manager.Health()`
- Verify collectors are registered and started
- Check pipeline statistics for errors

### High Memory Usage
- Reduce batch size
- Check event buffer sizes in collector manager
- Monitor correlation engine memory usage

### Correlation Not Working
- Ensure events have proper trace context
- Check correlation engine is started
- Verify enrichment is adding semantic context

## Architecture Compliance

This integration follows Tapio's 5-level architecture:
- Level 3 (Integrations): Connects Level 1 (Collectors) to Level 2 (Intelligence)
- No direct imports from Level 4 (Interfaces)
- Proper separation of concerns