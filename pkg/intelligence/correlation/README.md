# Correlation Package

The `correlation` package provides multi-event relationship detection and pattern recognition for the Tapio observability platform. It identifies connections between events across time, services, and infrastructure layers to provide holistic system understanding.

## Overview

This package is responsible for:
- **Pattern Detection**: Identifying recurring event sequences and anomalies
- **Temporal Correlation**: Linking events within time windows
- **Causal Analysis**: Determining cause-effect relationships
- **Real-time Processing**: Streaming correlation with minimal latency

## Architecture

```
correlation/
├── patterns.go          # Pattern matching algorithms
├── detector.go          # Correlation detection engine
├── temporal.go          # Time-based correlation logic
├── processor.go         # Real-time event processor
├── processor_test.go    # Comprehensive test suite
└── README.md           # This file
```

## Key Components

### Pattern Types

The package recognizes four primary correlation patterns:

1. **Sequence Pattern**: Ordered event chains (A→B→C)
2. **Temporal Pattern**: Time-proximity relationships
3. **Anomaly Pattern**: Deviation from baselines
4. **Escalation Pattern**: Progressive severity increases

### RealTimeProcessor

High-performance streaming correlation engine:

```go
config := &ProcessorConfig{
    BufferSize:        10000,
    TimeWindow:        5 * time.Minute,
    CorrelationWindow: 10 * time.Minute,
}

processor, err := NewRealTimeProcessor(config)
if err != nil {
    return err
}

// Process events
result := processor.ProcessEvent(ctx, event)
```

### PatternMatcher

Efficient pattern matching with configurable rules:

```go
matcher := NewPatternMatcher()

// Define custom pattern
pattern := &CorrelationPattern{
    Type: PatternTypeSequence,
    Rules: []PatternRule{
        {EventType: "pod-crash", Window: 30 * time.Second},
        {EventType: "node-pressure", Window: 60 * time.Second},
    },
}

matcher.AddPattern("cascade-failure", pattern)
```

### CorrelationDetector

Core detection algorithms:

```go
detector := &CorrelationDetector{
    sensitivity: 0.8,
    minEvents:   3,
}

correlation := detector.DetectCorrelation(events)
```

## Usage Examples

### Basic Event Correlation

```go
import "github.com/yairfalse/tapio/pkg/intelligence/correlation"

// Create processor
processor, _ := correlation.NewRealTimeProcessor(nil)

// Process events
event1 := &domain.UnifiedEvent{
    ID:   "evt-1",
    Type: domain.EventTypeMemory,
    Entity: &domain.EntityContext{
        Type: "pod",
        Name: "api-server",
    },
}

result := processor.ProcessEvent(ctx, event1)
if result.Pattern != "" {
    fmt.Printf("Detected pattern: %s\n", result.Pattern)
}
```

### Custom Pattern Detection

```go
// Define escalation pattern
processor.AddCustomPattern(&CorrelationPattern{
    Type: PatternTypeEscalation,
    Name: "memory-cascade",
    Rules: []PatternRule{
        {
            EventType: "memory-warning",
            Threshold: 0.7,
            Window:    2 * time.Minute,
        },
        {
            EventType: "memory-critical",
            Threshold: 0.9,
            Window:    5 * time.Minute,
        },
    },
})
```

### Batch Correlation Analysis

```go
// Analyze historical events
events := loadHistoricalEvents()
results := processor.BatchCorrelate(events, BatchConfig{
    MaxWindow: 1 * time.Hour,
    MinScore:  0.7,
})

for _, correlation := range results {
    fmt.Printf("Found correlation: %s (confidence: %.2f)\n",
        correlation.Pattern, correlation.Score)
}
```

## Configuration

### ProcessorConfig Options

```go
type ProcessorConfig struct {
    BufferSize        int           // Event buffer size (default: 1000)
    TimeWindow        time.Duration // Analysis window (default: 5m)
    CorrelationWindow time.Duration // Max correlation span (default: 10m)
    MinCorrelation    float64       // Minimum score (default: 0.7)
    MaxPatterns       int           // Pattern cache size (default: 100)
}
```

### Performance Tuning

```go
// High-throughput configuration
config := &ProcessorConfig{
    BufferSize:        50000,
    TimeWindow:        1 * time.Minute,
    CorrelationWindow: 5 * time.Minute,
}

// High-accuracy configuration
config := &ProcessorConfig{
    BufferSize:        5000,
    TimeWindow:        10 * time.Minute,
    CorrelationWindow: 30 * time.Minute,
}
```

## Correlation Algorithms

### Temporal Correlation

Uses sliding time windows with exponential decay:
- Events closer in time have stronger correlation
- Decay factor: e^(-Δt/τ) where τ is the time constant

### Sequence Detection

Implements modified Aho-Corasick algorithm:
- O(n + m) complexity for n events and m patterns
- Supports wildcards and partial matches

### Anomaly Detection

Statistical approach using:
- Z-score calculation for metric deviations
- Adaptive baselines with EWMA
- Seasonal decomposition for periodic patterns

## Performance Characteristics

- **Throughput**: 50,000+ events/second
- **Latency**: < 1ms for pattern matching
- **Memory**: O(n) where n is buffer size
- **CPU**: Scales linearly with event rate

## Best Practices

1. **Right-size Buffers**: Match buffer size to event rate
2. **Tune Time Windows**: Shorter windows for real-time, longer for accuracy
3. **Pattern Specificity**: More specific patterns perform better
4. **Batch When Possible**: Batch processing is more efficient than streaming

## Testing

```bash
cd pkg/intelligence/correlation
go test -v ./...
go test -bench=. -benchmem
go test -race ./...
```

## Integration

This package integrates with:
- **Context Package**: Uses enriched events with confidence scores
- **Pipeline Package**: Plugs into correlation stage
- **Domain Package**: Works with UnifiedEvent structure

## Metrics and Monitoring

The processor exposes metrics:
- Events processed per second
- Patterns detected by type
- Correlation confidence distribution
- Processing latency percentiles

## Error Handling

- Non-blocking: Errors don't stop processing
- Graceful degradation: Falls back to simple patterns
- Error events are tracked in metrics

## Future Enhancements

- Machine learning pattern discovery
- Distributed correlation across nodes
- Custom pattern DSL
- GraphQL API for correlation queries
- Persistent pattern storage