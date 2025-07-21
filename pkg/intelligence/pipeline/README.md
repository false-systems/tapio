# Pipeline Package

The `pipeline` package provides high-performance event processing orchestration for the Tapio observability platform. It implements a configurable, multi-stage pipeline that can process 165,000+ events per second while maintaining data quality and reliability.

## Overview

This package is responsible for:
- **Pipeline Orchestration**: Coordinating multi-stage event processing
- **Performance Optimization**: Achieving 165k+ events/sec throughput
- **Worker Pool Management**: Parallel processing with dynamic scaling
- **Metrics Collection**: Comprehensive performance monitoring
- **Circuit Breaking**: Automatic failure handling and recovery

## Architecture

```
pipeline/
├── interface.go         # Pipeline interfaces and configuration
├── builder.go          # Builder pattern for pipeline creation
├── orchestrator.go     # Core orchestration engine
├── worker_pool.go      # Parallel processing implementation
├── metrics.go          # Performance metrics collection
├── orchestrator_test.go # Orchestrator tests
├── builder_test.go     # Builder and integration tests
└── README.md           # This file
```

## Key Components

### IntelligencePipeline Interface

Core interface for all pipeline implementations:

```go
type IntelligencePipeline interface {
    ProcessEvent(event *domain.UnifiedEvent) error
    ProcessBatch(events []*domain.UnifiedEvent) error
    Start(ctx context.Context) error
    Stop() error
    Shutdown() error
    GetMetrics() PipelineMetrics
    IsRunning() bool
    GetConfig() PipelineConfig
}
```

### Pipeline Modes

Three optimized modes for different use cases:

1. **High-Performance Mode**: Maximum throughput (165k+ events/sec)
2. **Standard Mode**: Balanced performance and resource usage
3. **Debug Mode**: Enhanced logging and validation

### PipelineBuilder

Fluent API for pipeline construction:

```go
pipeline, err := NewPipelineBuilder().
    WithMode(PipelineModeHighPerformance).
    WithBatchSize(1000).
    WithMaxConcurrency(runtime.NumCPU()).
    EnableCorrelation(true).
    EnableCircuitBreaker(true).
    WithErrorThreshold(0.1).
    Build()
```

### HighPerformanceOrchestrator

Core processing engine with three stages:
1. **Validation Stage**: Structural and temporal validation
2. **Context Stage**: Enrichment and scoring
3. **Correlation Stage**: Pattern detection (async)

### WorkerPool

Efficient parallel processing:
- Dynamic worker scaling
- Job queue management
- Load balancing
- Metrics collection

## Usage Examples

### Basic Pipeline Setup

```go
import "github.com/yairfalse/tapio/pkg/intelligence/pipeline"

// Create high-performance pipeline
pipeline, err := pipeline.NewHighPerformancePipeline()
if err != nil {
    return err
}

// Start processing
ctx := context.Background()
if err := pipeline.Start(ctx); err != nil {
    return err
}
defer pipeline.Shutdown()

// Process events
event := &domain.UnifiedEvent{
    ID:        "evt-123",
    Type:      domain.EventTypeSystem,
    Timestamp: time.Now(),
    Source:    "collector",
}

if err := pipeline.ProcessEvent(event); err != nil {
    log.Printf("Processing failed: %v", err)
}
```

### Custom Configuration

```go
// Create custom pipeline configuration
config := &pipeline.PipelineConfig{
    Mode:               pipeline.PipelineModeStandard,
    BatchSize:          500,
    BufferSize:         5000,
    MaxConcurrency:     8,
    ProcessingTimeout:  10 * time.Second,
    MetricsInterval:    500 * time.Millisecond,
    EnableValidation:   true,
    EnableContext:      true,
    EnableCorrelation:  true,
    EnableCircuitBreaker: true,
    ErrorThreshold:     0.05,
}

pipeline, err := pipeline.NewPipeline(config)
```

### Batch Processing

```go
// Process events in batches for efficiency
events := make([]*domain.UnifiedEvent, 1000)
for i := range events {
    events[i] = generateEvent(i)
}

if err := pipeline.ProcessBatch(events); err != nil {
    return fmt.Errorf("batch processing failed: %w", err)
}
```

### Metrics Monitoring

```go
// Monitor pipeline performance
ticker := time.NewTicker(10 * time.Second)
defer ticker.Stop()

for range ticker.C {
    metrics := pipeline.GetMetrics()
    log.Printf("Throughput: %.2f events/sec", metrics.ThroughputPerSecond)
    log.Printf("Latency P99: %v", metrics.P99Latency)
    log.Printf("Error Rate: %.2f%%", metrics.ErrorRate * 100)
    
    if metrics.CircuitBreakerState == "open" {
        log.Warn("Circuit breaker is open!")
    }
}
```

## Configuration Options

### PipelineConfig

```go
type PipelineConfig struct {
    // Mode selection
    Mode PipelineMode
    
    // Performance tuning
    MaxConcurrency    int
    BatchSize         int
    BufferSize        int
    
    // Timeouts
    ProcessingTimeout time.Duration
    MetricsInterval   time.Duration
    ShutdownTimeout   time.Duration
    
    // Features
    EnableValidation     bool
    EnableContext        bool
    EnableCorrelation    bool
    EnableMetrics        bool
    EnableCircuitBreaker bool
    
    // Error handling
    MaxRetries              int
    RetryBackoff            time.Duration
    ErrorThreshold          float64
    CircuitBreakerThreshold float64
}
```

### Performance Tuning Guide

| Use Case | Mode | Batch Size | Workers | Buffer Size |
|----------|------|------------|---------|-------------|
| Real-time Analytics | High-Performance | 1000 | CPU count | 10000 |
| Standard Processing | Standard | 100 | 4 | 1000 |
| Development/Debug | Debug | 10 | 1 | 100 |
| Batch Import | High-Performance | 5000 | CPU count * 2 | 50000 |

## Performance Characteristics

### Throughput

- **High-Performance Mode**: 165,000+ events/second
- **Standard Mode**: 50,000+ events/second  
- **Debug Mode**: 1,000+ events/second

### Latency

- **P50**: < 1ms
- **P95**: < 5ms
- **P99**: < 10ms

### Resource Usage

- **Memory**: ~100MB base + 1KB per buffered event
- **CPU**: Scales linearly with worker count
- **Goroutines**: 3 + (2 * worker count)

## Circuit Breaker

Automatic failure protection:
- Opens at 50% error rate (configurable)
- Half-open state for recovery testing
- Automatic recovery after timeout
- Metrics tracking for trips

## Best Practices

1. **Choose the Right Mode**: Use High-Performance only when needed
2. **Size Buffers Appropriately**: 10x peak event rate
3. **Monitor Metrics**: Set up alerts on error rates
4. **Handle Backpressure**: Check ProcessEvent errors
5. **Graceful Shutdown**: Always call Shutdown()

## Testing

```bash
cd pkg/intelligence/pipeline
go test -v ./...
go test -bench=. -benchmem
go test -race ./...
```

### Benchmark Results

```
BenchmarkPipeline_ProcessEvent-8     10000000    150 ns/op    48 B/op    1 allocs/op
BenchmarkOrchestrator_ProcessBatch-8  2000000    850 ns/op   320 B/op    8 allocs/op
```

## Integration

This package integrates with:
- **Context Package**: For validation and enrichment
- **Correlation Package**: For pattern detection
- **Collectors**: As the processing backend
- **Interfaces**: Through gRPC/REST APIs

## Monitoring and Observability

### Available Metrics

- Events: received, processed, dropped, failed
- Latency: average, P50, P95, P99, max
- Throughput: events per second
- Errors: by stage (validation, context, correlation)
- Circuit breaker: state and trip count
- Resources: queue depth, active workers

### OpenTelemetry Integration

```go
// Enable tracing
pipeline, _ := NewPipelineBuilder().
    EnableTracing(true).
    Build()
```

## Error Handling

- Non-blocking event processing
- Detailed error metrics by stage
- Circuit breaker for cascading failures
- Configurable retry with backoff

## Future Enhancements

- Adaptive concurrency control
- Multi-region pipeline federation
- Custom stage injection
- Pipeline composition/chaining
- Persistent queue option
- WebAssembly stage support