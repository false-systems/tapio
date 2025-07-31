# Intelligence Package

The `intelligence` package is the brain of the Tapio observability platform, providing advanced event analysis, correlation, and real-time insights. It transforms raw observability data into actionable intelligence through a high-performance processing pipeline.

## Overview

The intelligence layer provides:
- **Real-time Event Processing**: 165,000+ events/second throughput
- **Contextual Enrichment**: Semantic understanding of individual events
- **Pattern Recognition**: Multi-event correlation and anomaly detection
- **Impact Assessment**: Infrastructure and operational impact analysis
- **Root Cause Analysis**: Automated problem identification

## Architecture

```
pkg/intelligence/
├── context/        # Individual event intelligence
│   ├── validation.go      # Event structure validation
│   ├── confidence.go      # Reliability scoring
│   ├── impact.go         # Impact assessment
│   └── builder.go        # Context orchestration
│
├── correlation/    # Multi-event relationships
│   ├── patterns.go       # Pattern definitions
│   ├── detector.go       # Detection algorithms
│   ├── temporal.go       # Time-based correlation
│   └── processor.go      # Real-time processing
│
└── pipeline/       # High-performance orchestration
    ├── interface.go      # Core interfaces
    ├── builder.go        # Pipeline construction
    ├── orchestrator.go   # Event orchestration
    ├── worker_pool.go    # Parallel processing
    └── metrics.go        # Performance monitoring
```

## Design Principles

### 1. **Layered Processing**
Events flow through three distinct stages:
- **Validation & Enrichment** (context package)
- **Correlation & Patterns** (correlation package)
- **Orchestration & Scaling** (pipeline package)

### 2. **Performance First**
- Lock-free algorithms where possible
- Zero-allocation hot paths
- Parallel processing by default
- Configurable batch processing

### 3. **Reliability**
- Circuit breakers for failure isolation
- Graceful degradation
- Comprehensive error tracking
- Non-blocking processing

### 4. **Extensibility**
- Plugin architecture for custom stages
- Configurable processing modes
- Builder pattern for flexibility
- Interface-based design

## Quick Start

### Basic Usage

```go
import (
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
    "github.com/yairfalse/tapio/pkg/domain"
)

// Create and start pipeline
pipeline, err := pipeline.NewHighPerformancePipeline()
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := pipeline.Start(ctx); err != nil {
    log.Fatal(err)
}
defer pipeline.Shutdown()

// Process events
event := &domain.UnifiedEvent{
    ID:        "evt-123",
    Type:      domain.EventTypeSystem,
    Timestamp: time.Now(),
    Source:    "kubernetes",
    Entity: &domain.EntityContext{
        Type: "pod",
        Name: "api-server-1",
    },
}

if err := pipeline.ProcessEvent(event); err != nil {
    log.Printf("Failed to process: %v", err)
}
```

### Custom Pipeline Configuration

```go
import (
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
    "github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Build custom pipeline
p, err := pipeline.NewPipelineBuilder().
    WithMode(pipeline.PipelineModeHighPerformance).
    WithBatchSize(1000).
    WithMaxConcurrency(16).
    EnableValidation(true).
    EnableContext(true).
    EnableCorrelation(true).
    EnableCircuitBreaker(true).
    WithErrorThreshold(0.05).
    Build()
```

### Event Correlation

```go
import "github.com/yairfalse/tapio/pkg/intelligence/correlation"

// Configure correlation processor
config := &correlation.ProcessorConfig{
    BufferSize:        10000,
    TimeWindow:        5 * time.Minute,
    CorrelationWindow: 10 * time.Minute,
}

processor, err := correlation.NewRealTimeProcessor(config)
if err != nil {
    return err
}

// Process events for correlation
result := processor.ProcessEvent(ctx, event)
if result.Pattern != "" {
    log.Printf("Detected pattern: %s (confidence: %.2f)",
        result.Pattern, result.Score)
}
```

## Performance Benchmarks

### Throughput
| Component | Events/sec | Latency P99 |
|-----------|------------|-------------|
| Validation | 500,000+ | < 0.1ms |
| Context Building | 200,000+ | < 0.5ms |
| Correlation | 100,000+ | < 1ms |
| Full Pipeline | 165,000+ | < 10ms |

### Resource Usage
- **Memory**: ~100MB base + dynamic based on buffer sizes
- **CPU**: Scales linearly with configured workers
- **Goroutines**: Minimal, proportional to worker count

## Integration Points

### Input Sources
- Kubernetes collectors
- eBPF agents
- Application instrumentation
- Infrastructure monitors
- Log aggregators

### Output Consumers
- Real-time dashboards
- Alert managers
- Storage systems
- Analytics engines
- Automation systems

## Configuration

### Environment Variables
```bash
TAPIO_PIPELINE_MODE=high-performance
TAPIO_PIPELINE_WORKERS=16
TAPIO_PIPELINE_BATCH_SIZE=1000
TAPIO_CORRELATION_WINDOW=5m
TAPIO_CIRCUIT_BREAKER_THRESHOLD=0.5
```

### Configuration File
```yaml
intelligence:
  pipeline:
    mode: high-performance
    workers: 16
    batch_size: 1000
    buffer_size: 10000
  
  correlation:
    time_window: 5m
    min_correlation: 0.7
    max_patterns: 100
  
  context:
    max_event_age: 24h
    confidence_weights:
      completeness: 0.4
      reliability: 0.3
      temporal: 0.2
      semantic: 0.1
```

## Monitoring

### Metrics Exposed
- `tapio_intelligence_events_total` - Total events processed
- `tapio_intelligence_events_failed` - Failed events by stage
- `tapio_intelligence_latency_seconds` - Processing latency histogram
- `tapio_intelligence_patterns_detected` - Correlation patterns found
- `tapio_intelligence_circuit_breaker_state` - Circuit breaker status

### Health Checks
```go
// Check pipeline health
metrics := pipeline.GetMetrics()
if metrics.ErrorRate > 0.1 {
    log.Warn("High error rate detected")
}
if !pipeline.IsRunning() {
    log.Error("Pipeline is not running")
}
```

## Best Practices

1. **Start Simple**: Begin with standard mode and tune based on metrics
2. **Monitor Continuously**: Set up alerts on error rates and latency
3. **Size Appropriately**: Buffer sizes should be 10x peak event rate
4. **Test Patterns**: Validate correlation patterns with real data
5. **Handle Failures**: Implement proper error handling and retries

## Testing

### Unit Tests
```bash
cd pkg/intelligence
go test -v ./...
```

### Integration Tests
```bash
go test -v -tags=integration ./...
```

### Performance Tests
```bash
go test -bench=. -benchmem ./...
```

### Load Testing
```bash
# Generate 1M events and measure throughput
go run cmd/loadtest/main.go -events=1000000 -workers=16
```

## Troubleshooting

### High Memory Usage
- Reduce buffer sizes
- Enable batch processing
- Check for correlation pattern leaks

### Low Throughput
- Increase worker count
- Enable high-performance mode
- Check CPU throttling

### High Error Rate
- Check validation rules
- Monitor circuit breaker trips
- Review error logs by stage

## Contributing

1. Follow the architecture patterns
2. Maintain test coverage above 80%
3. Run benchmarks before/after changes
4. Update documentation
5. Follow Go best practices

## Future Roadmap

- **Machine Learning Integration**: Automated pattern discovery
- **Distributed Processing**: Multi-node correlation
- **Custom Stages**: Plugin system for user-defined processing
- **Query Language**: DSL for correlation patterns
- **Streaming SQL**: Real-time analytics queries
- **GraphQL API**: Flexible data access