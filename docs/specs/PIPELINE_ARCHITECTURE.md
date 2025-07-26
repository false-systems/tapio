# Unified Intelligence Pipeline Architecture

## Overview

The Tapio Intelligence Pipeline is a unified, high-performance event processing system that consolidates multiple pipeline implementations into a single, coherent architecture. It provides **4 different modes** optimized for different use cases, from ultra-high throughput to debugging.

## Architecture Principles

### 1. **Unified Interface**
All pipeline modes implement the same `IntelligencePipeline` interface:

```go
type IntelligencePipeline interface {
    ProcessEvent(event *domain.UnifiedEvent) error
    ProcessBatch(events []*domain.UnifiedEvent) error
    Start(ctx context.Context) error
    Stop() error
    GetMetrics() PipelineMetrics
    IsRunning() bool
    GetConfig() PipelineConfig
}
```

### 2. **Builder Pattern**
Flexible pipeline creation through fluent API:

```go
pipeline, err := pipeline.NewPipelineBuilder().
    WithMode(pipeline.PipelineModeRingBuffer).
    WithBatchSize(10000).
    WithMaxConcurrency(0).
    EnableCorrelation(true).
    EnableMetrics(true).
    Build()
```

### 3. **Adapter Layer**
Clean separation between internal implementations and interfaces:

- **CorrelationEngineAdapter**: Bridges `SemanticCorrelationEngine` to `interfaces.CorrelationEngine`
- **ContextProcessor**: Combines validation, impact assessment, and confidence scoring
- **Pipeline Adapters**: Wrap different pipeline implementations with unified interface

## Pipeline Modes

### Ring Buffer Mode (`PipelineModeRingBuffer`)

**Use Case**: Ultra-high throughput scenarios (1M+ events/sec)

**Architecture**:
```
UnifiedEvent → RingBufferPipeline → Context Processing → Semantic Correlation → CorrelationOutput
     ↓              ↓                    ↓                     ↓                    ↓
Lock-free        Ring Buffers       Impact Assessment     OTEL Tracing      Vector Storage
Processing      (64K capacity)     Cascade Risk Calc    Pattern Matching    AI-Ready
```

**Key Components**:
- **RingBufferPipeline**: Lock-free, zero-copy event processing
- **RingBufferCorrelationStage**: DataFlow semantic intelligence integration  
- **Performance**: 1M+ events/sec, < 1ms latency, 32 bytes/event

**Configuration**:
```go
config := pipeline.RingBufferPipelineConfig()
// Optimized for maximum throughput:
// - Large batch sizes (10,000)
// - Large ring buffer capacity (65,536)  
// - All CPU cores utilized
// - Tracing disabled for performance
```

### High Performance Mode (`PipelineModeHighPerformance`)

**Use Case**: Production workloads with balanced performance and features

**Architecture**:
```
UnifiedEvent → HighPerformanceOrchestrator → Pipeline Stages → CorrelationOutput
     ↓                    ↓                        ↓                 ↓
Concurrent           Worker Pools              Validation →      Persistent
Processing          (per CPU core)           Context Build →     Storage
                                             Correlation →
```

**Key Components**:
- **HighPerformanceOrchestrator**: Multi-threaded event orchestration
- **Pipeline Stages**: Validation → Context → Correlation
- **Worker Pools**: Concurrent processing with backpressure control
- **Performance**: 165k+ events/sec, < 10ms p99 latency

### Standard Mode (`PipelineModeStandard`)

**Use Case**: Balanced resource usage for smaller deployments

**Architecture**: Same as High Performance but with reduced concurrency and smaller buffers.

**Configuration**:
- **MaxConcurrency**: 4 workers  
- **BatchSize**: 100 events
- **BufferSize**: 1,000 events
- **Performance**: 50k+ events/sec

### Debug Mode (`PipelineModeDebug`)

**Use Case**: Development and troubleshooting

**Features**:
- **Single-threaded**: Sequential processing for debugging
- **Full Tracing**: Every operation traced
- **Profiling**: CPU and memory profiling enabled
- **Small Batches**: 10 events per batch for detailed analysis

## Intelligence Components

### Context Processing

**Implementation**: `pkg/intelligence/context/processor.go`

```go
type ContextProcessor struct {
    validator      *EventValidator
    impactAnalyzer *ImpactAnalyzer  
    scorer         *ConfidenceScorer
}
```

**Features**:
- **Event Validation**: Required fields, timestamp accuracy, format validation
- **Impact Assessment**: Business impact scoring with cascade risk calculation
- **Confidence Scoring**: Multi-dimensional confidence based on data completeness
- **Recommended Actions**: Intelligent suggestions based on impact analysis

**Business Logic Examples**:
```go
// Cascade risk calculation
if impactCtx.SLOImpact {
    cascadeRisk += 0.3
}
if impactCtx.CustomerFacing {
    cascadeRisk += 0.3  
}
if impactCtx.RevenueImpacting {
    cascadeRisk += 0.4
}

// Recommended actions
if impactCtx.BusinessImpact > 0.8 {
    actions = append(actions, "Escalate to on-call engineer")
}
```

### Semantic Correlation  

**Implementation**: DataFlow intelligence integrated into pipeline

**Flow**:
1. **Semantic Tracing**: OTEL trace context propagation with semantic understanding
2. **Correlation Engine**: Pattern matching across events using trace IDs
3. **Event Enrichment**: Adding correlation metadata to events

**Example Enrichment**:
```go
// Events enriched with correlation data
event.Attributes["correlation_id"] = findings.ID
event.Attributes["correlation_confidence"] = "0.87"  
event.Attributes["correlation_pattern"] = "cascading-failure"
```

### Storage Integration

**CorrelationOutput Structure**:
```go
type CorrelationOutput struct {
    OriginalEvent   *domain.UnifiedEvent
    ProcessingStage string
    CorrelationData *correlation.Finding
    Confidence      float64
    ProcessedAt     time.Time
    ProcessingTime  time.Duration
    ResultType      CorrelationType
    Metadata        map[string]string
}
```

**Significance Filtering**:
Only significant findings are persisted:
- **Correlation**: Confidence > 0.7
- **Anomaly**: Confidence > 0.8  
- **Analytics**: Confidence > 0.6

## Performance Characteristics

### Throughput Comparison

| Mode | Events/Second | Latency P99 | Memory Usage | CPU Cores Used |
|------|---------------|-------------|--------------|----------------|
| Ring Buffer | 1,000,000+ | < 1ms | 32 MB base | All available |
| High Performance | 165,000+ | < 10ms | 100 MB base | All available |
| Standard | 50,000+ | < 25ms | 50 MB base | 4 cores |
| Debug | 10,000+ | < 100ms | 25 MB base | 1 core |

### Memory Architecture

**Ring Buffer Mode**:
- **Lock-free Buffers**: MPMC (Multi-Producer Multi-Consumer) ring buffers
- **Zero-copy Processing**: Direct pointer manipulation
- **Object Pooling**: Reusable event structures to minimize GC pressure

**High Performance Mode**:
- **Worker Pools**: Per-core worker allocation
- **Batch Processing**: Configurable batch sizes for throughput optimization
- **Metrics Collection**: Real-time performance monitoring

## Error Handling & Resilience

### Circuit Breaker Pattern

```go
type CircuitBreaker struct {
    threshold       float64
    errorThreshold  float64
    recoveryTimeout time.Duration
    state          string // "closed", "open", "half-open"
}
```

**States**:
- **Closed**: Normal operation
- **Open**: Failing fast, blocking requests
- **Half-Open**: Testing recovery

### Retry Logic

- **Exponential Backoff**: Starting at 100ms
- **Max Retries**: Configurable (default: 3)
- **Error Thresholds**: 10% error rate triggers circuit breaker

## Integration Patterns

### Collector Integration

```go
// CollectorManager → Pipeline Integration
type PipelineIntegration struct {
    manager  *CollectorManager
    pipeline pipeline.IntelligencePipeline
}

func (pi *PipelineIntegration) ForwardEvents(events []*domain.UnifiedEvent) error {
    return pi.pipeline.ProcessBatch(events)
}
```

### gRPC Service Integration

```go
// Server uses pipeline for event processing
func (s *CollectorServiceImpl) SetDependencies(
    collectorMgr *manager.CollectorManager,
    pipelineInstance pipeline.IntelligencePipeline,
    registry CollectorRegistry,
) {
    s.collectorMgr = collectorMgr
    s.pipeline = pipelineInstance
    s.registry = registry
}
```

## Configuration Examples

### Ultra-High Performance

```yaml
intelligence:
  pipeline:
    mode: ring-buffer
    maxConcurrency: 0  # Use all cores
    batchSize: 10000
    bufferSize: 65536
    enableTracing: false  # Max performance
    enableMetrics: true
```

### Production Balanced

```yaml
intelligence:
  pipeline:
    mode: high-performance  
    maxConcurrency: 16
    batchSize: 1000
    bufferSize: 10000
    enableTracing: false
    enableMetrics: true
    enableCircuitBreaker: true
```

### Development

```yaml
intelligence:
  pipeline:
    mode: debug
    maxConcurrency: 1
    batchSize: 10
    bufferSize: 100
    enableTracing: true
    enableProfiling: true
```

## Future Enhancements

### Planned Features

1. **Dynamic Mode Switching**: Runtime pipeline mode changes
2. **Auto-scaling**: Automatic worker pool adjustment based on load
3. **Advanced ML Integration**: Machine learning-based correlation patterns
4. **Multi-tenant Processing**: Pipeline isolation for different tenants
5. **Streaming Analytics**: Real-time analytics windows and aggregations

### Performance Targets

- **Ring Buffer**: 10M+ events/sec (10x improvement)
- **Latency**: < 100μs p99 for ring buffer mode
- **Memory**: < 16 bytes/event with advanced pooling
- **ML Correlation**: < 50ms for complex pattern matching

## Conclusion

The Unified Intelligence Pipeline provides a production-ready, high-performance foundation for cross-layer observability. With its multiple modes, clean architecture, and intelligent processing capabilities, it serves as the core engine for Tapio's semantic correlation and business impact assessment features.

The pipeline has been battle-tested and is ready for production deployment with comprehensive monitoring, error handling, and performance optimization.