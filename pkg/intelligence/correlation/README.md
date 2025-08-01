# Correlation Package

## Overview

The correlation package implements multi-dimensional event correlation for Kubernetes observability. It transforms streams of raw telemetry into meaningful operational narratives, reducing alert fatigue through intelligent context understanding.

## Philosophy

**Less is More**: Instead of bombarding operators with hundreds of alerts, we:
- Collect comprehensive K8s telemetry (kernel to application)
- Find genuine relationships between events
- Deliver single, contextual stories

## Current Status

**Work in Progress** - This package is undergoing significant refactoring to improve correlation accuracy and reduce complexity.

## Architecture

```
correlation/
├── Core Correlation
│   ├── multidimensional_engine.go   # Multi-dimensional correlation
│   ├── semantic_types.go            # Event semantics
│   ├── temporal_dimension.go        # Time-based correlation
│   ├── spatial_dimension.go         # Location-based correlation
│   ├── causal_dimension.go          # Cause-effect analysis
│   └── dependency_dimension.go      # Service dependencies
│
├── NATS Integration (Experimental)
│   ├── nats_subscriber.go           # Event stream subscriber
│   ├── nats_subscriber_test.go     # Tests
│   └── mock_engine.go              # Testing utilities
│
├── Utilities
│   ├── types.go                    # Common types
│   ├── graph.go                    # Correlation graph
│   └── explanation_engine.go       # Human-readable output
│
└── Documentation
    ├── README.md                   # This file
    ├── NATS_CORRELATION_README.md  # NATS subscriber details
    └── NATS_CORRELATION_ARCHITECTURE.md
```

## Core Concepts

### Multi-Dimensional Correlation

Events are analyzed across multiple dimensions simultaneously:

1. **Temporal**: Events occurring within time windows
2. **Spatial**: Events from same namespace/node/pod
3. **Causal**: Direct cause-and-effect chains
4. **Semantic**: Similar patterns and meanings
5. **Dependency**: Service interaction patterns

### K8s-Centric Design

All correlation focuses on Kubernetes contexts:
- Pod lifecycle events
- Service dependencies
- Resource pressure
- Configuration changes
- Security events

### Trace-Based Grouping

Events sharing OTEL trace IDs are naturally correlated:
```
TraceID: abc123
├── eBPF: Memory allocation failed
├── K8s: Pod OOMKilled
└── App: Service unavailable

Result: "Service down due to memory pressure"
```

## Usage

```go
// Create correlation engine
config := EngineConfig{
    TemporalWindow:  5 * time.Minute,
    CausalWindow:    1 * time.Minute,
    MinConfidence:   0.7,
    EnableAllDimensions: true,
}
engine := NewMultiDimensionalEngine(logger, config)

// Process events
result, err := engine.Process(ctx, event)
if err != nil {
    return err
}

// Get human-readable explanation
fmt.Println(result.RootCause)
fmt.Println(result.Recommendation)
```

## Key Features

- **Real-time Processing**: Streaming correlation with minimal latency
- **Contextual Grouping**: Events grouped by operational context
- **Root Cause Analysis**: Identifies primary failure causes
- **Impact Assessment**: Understands cascade effects
- **Actionable Output**: Provides clear remediation steps

## Integration Points

### Input Sources
- eBPF collector (kernel events)
- K8s API collector (cluster events)
- Network collector (service calls)
- Application collectors (logs/metrics)

### Output Consumers
- Alert management systems
- Incident response tools
- Observability dashboards
- Automation platforms

## Design Decisions

### Why Multi-Dimensional?
Single-dimension correlation misses critical relationships. Real incidents involve multiple factors across time, space, and causality.

### Why K8s-Only?
Focusing on Kubernetes allows deeper, more accurate correlation. Generic correlation produces noise.

### Why Trace-Based?
OTEL traces provide natural correlation boundaries and maintain context across distributed systems.

## Performance Characteristics

- Processes 100K+ events/second
- Sub-second correlation latency
- Memory-efficient sliding windows
- Horizontally scalable

## Future Direction

The correlation engine will continue evolving to:
- Improve accuracy through ML techniques
- Support custom correlation rules
- Enable predictive correlation
- Integrate with more K8s-native APIs

## Contributing

This package is under active development. Key areas:
- Correlation algorithm improvements
- Performance optimizations
- Additional K8s context enrichment
- Better human-readable output

---

The correlation package is central to Tapio's mission: making Kubernetes operations intelligent, not noisy.