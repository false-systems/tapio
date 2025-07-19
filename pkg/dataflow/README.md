# Tapio DataFlow - OTEL Semantic Correlation Integration

## Overview

The `dataflow` package integrates OpenTelemetry (OTEL) semantic correlation into Tapio's existing production collector system, creating an end-to-end observability pipeline with intelligent correlation.

## Architecture

### Current Flow (Production)
```
Collectors → Events → Basic Correlation → gRPC → Server → REST/gRPC APIs → GUI/CLI
```

### Enhanced Flow (With OTEL Semantic Correlation)
```
Collectors → Events → OTEL Semantic Correlation → Enriched Events → gRPC → Server → APIs → GUI/CLI
                              ↓
                    Semantic Groups, Impact Assessment, Predictions
```

## Key Components

### 1. TapioDataFlow
The main integration component that:
- Connects to existing event streams
- Applies semantic correlation using `SemanticOTELTracer`
- Enriches events with correlation metadata
- Manages OTEL trace context propagation
- Provides metrics and monitoring

### 2. ServerBridge
Forwards semantic findings to Tapio server:
- Batches and sends enriched events
- Propagates OTEL trace context via gRPC
- Sends semantic group updates
- Maintains high throughput (165k+ events/sec target)

### 3. SemanticOTELTracer (from intelligence/correlation)
Revolutionary multi-dimensional correlation:
- Groups events by MEANING, not just time
- Causal, spatial, and temporal correlation
- Business impact assessment
- Predictive outcomes with prevention actions

## Integration Guide

### Basic Setup

```go
import (
    "github.com/yairfalse/tapio/pkg/dataflow"
    "github.com/yairfalse/tapio/pkg/domain"
)

// 1. Create event channels
inputEvents := make(chan domain.Event, 1000)
outputEvents := make(chan domain.Event, 1000)

// 2. Configure data flow
config := dataflow.Config{
    EnableSemanticGrouping: true,
    GroupRetentionPeriod:   30 * time.Minute,
    ServiceName:            "tapio-collector",
    ServiceVersion:         "2.0.0",
    Environment:            "production",
}

// 3. Create and connect data flow
df := dataflow.NewTapioDataFlow(config)
df.Connect(inputEvents, outputEvents)

// 4. Start processing
if err := df.Start(); err != nil {
    log.Fatal(err)
}
defer df.Stop()
```

### Server Integration

```go
// Configure server bridge
bridgeConfig := dataflow.BridgeConfig{
    ServerAddress: "localhost:9090",
    BufferSize:    500,
    MaxBatchSize:  100,
    EnableTracing: true,
}

// Create bridge
bridge, err := dataflow.NewServerBridge(bridgeConfig, df)
if err != nil {
    log.Fatal(err)
}

// Start forwarding
bridge.Start()
defer bridge.Stop()
```

### Processing Enriched Events

```go
// Process enriched events
for event := range outputEvents {
    // Events now contain:
    metadata := event.Context.Metadata
    
    // Correlation data
    correlationID := metadata["correlation_id"]
    confidence := metadata["correlation_confidence"]
    patternType := metadata["correlation_pattern"]
    
    // Semantic grouping
    semanticGroup := metadata["semantic_group_id"]
    semanticIntent := metadata["semantic_intent"]
    
    // Impact assessment
    businessImpact := metadata["impact_business"]
    cascadeRisk := metadata["impact_cascade_risk"]
    
    // Predictions
    scenario := metadata["prediction_scenario"]
    probability := metadata["prediction_probability"]
}
```

## Event Enrichment

Each event is enriched with:

### Correlation Metadata
- `correlation_id`: Unique correlation identifier
- `correlation_confidence`: Confidence score (0.0-1.0)
- `correlation_pattern`: Detected pattern type
- `related_event_count`: Number of related events

### Semantic Grouping
- `semantic_group_id`: Semantic group identifier
- `semantic_intent`: What the group is trying to achieve
- `semantic_type`: Type of semantic group (e.g., "memory_pressure_cascade")

### Impact Assessment
- `impact_business`: Business impact score (0.0-1.0)
- `impact_cascade_risk`: Risk of cascade failure (0.0-1.0)
- `impact_severity`: Technical severity level

### Predictions
- `prediction_scenario`: Predicted outcome scenario
- `prediction_probability`: Probability of prediction (0.0-1.0)

## OTEL Integration

### Trace Context Propagation
- Each event processing creates OTEL spans
- Trace context propagated through gRPC metadata
- Semantic groups maintain trace relationships

### Metrics Export
```go
metrics := df.GetMetrics()
// Returns:
// - events_processed: Total events processed
// - semantic_groups_active: Active semantic groups
// - traces_exported: OTEL traces exported
// - events_per_second: Current throughput
```

## Performance Considerations

### Throughput
- Designed for 165,000+ events/second
- Buffered channels prevent blocking
- Batch processing for efficiency

### Memory Management
- Configurable buffer sizes
- Automatic cleanup of old semantic groups
- Efficient event batching

### Latency
- Sub-millisecond event enrichment
- Asynchronous server forwarding
- Minimal impact on event flow

## Example Scenarios

### Memory Pressure Cascade
```
1. Memory pressure event detected
2. Semantically grouped with OOM kills
3. Pod eviction events correlated
4. Service restart events linked
5. Impact: High business impact, cascade risk
6. Prediction: "oom_kill_cascade" with prevention actions
```

### Network Connectivity Issue
```
1. Network timeout detected
2. Related connection failures grouped
3. Service communication issues correlated
4. Impact: Medium business impact
5. Prediction: "service_isolation" scenario
```

## Monitoring

### OTEL Traces
- View semantic correlation in action
- Track event flow through pipeline
- Identify bottlenecks and issues

### Metrics
- Monitor throughput and latency
- Track semantic group formation
- Measure correlation effectiveness

## Best Practices

1. **Buffer Sizing**: Set buffers based on expected load
2. **Flush Intervals**: Balance latency vs efficiency
3. **Retention Period**: Clean up old groups appropriately
4. **Error Handling**: Monitor bridge errors
5. **Trace Sampling**: Configure OTEL sampling for production

## Future Enhancements

- ML-based intent classification
- Advanced causality detection
- Real-time anomaly detection
- Custom correlation patterns
- Enhanced prediction models