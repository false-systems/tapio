# Multi-Dimensional OTEL Events: The UnifiedEvent Philosophy

## Overview

The UnifiedEvent is not just an event with OTEL context - it's a **multi-dimensional observability signal** that represents a holistic view of system behavior across all layers and perspectives.

## The Dimensions

### 1. **Trace Dimension** (OTEL Core)
```go
TraceContext: {
    TraceID: "trace-456",      // Distributed operation ID
    SpanID: "span-789",        // This event's place in the operation
    ParentSpanID: "span-456",  // Causal parent
    Sampled: true,             // Sampling decision
    Baggage: {                 // Cross-boundary context
        "deployment.name": "api-server",
        "user.id": "deploy-bot",
        "tenant.id": "acme-corp"
    }
}
```

### 2. **Semantic Dimension** (Meaning & Intent)
```go
Semantic: {
    Intent: "memory_exhaustion",           // What's happening
    Category: "resource_management",       // Domain category
    Tags: ["oom", "critical"],            // Searchable tags
    Narrative: "API server pod...",       // Human story
    Confidence: 0.95,                     // How sure we are
    Concepts: ["resource-limits"],        // Related concepts
    Embedding: [0.1, 0.2, ...],          // Vector for similarity
}
```

### 3. **Layer Dimension** (Technical Detail)
Each event can have data from multiple layers simultaneously:
```go
// Same event can have multiple layer data
Kernel: { Syscall: "oom_kill", PID: 1234 },
Network: { SourceIP: "10.0.0.1", Latency: 500ms },
Application: { Level: "error", Message: "Out of memory" },
Kubernetes: { EventType: "pod_oom_killed", Object: "api-server" }
```

### 4. **Impact Dimension** (Business Context)
```go
Impact: {
    Severity: "critical",
    BusinessImpact: 0.8,              // 0-1 scale
    AffectedServices: ["api", "web"],
    AffectedUsers: 1000,
    SLOImpact: true,
    CustomerFacing: true,
    RevenueImpacting: true
}
```

### 5. **Correlation Dimension** (Relationships)
```go
Correlation: {
    CorrelationID: "corr-deployment-456",
    GroupID: "deployment-lifecycle",
    ParentEventID: "evt-deploy-001",
    CausalChain: ["evt-1", "evt-2", "evt-3"],
    Pattern: "scaling-cascade",
    Stage: "consequence"  // Where in the pattern
}
```

### 6. **Entity Dimension** (What It's About)
```go
Entity: {
    Type: "pod",
    Name: "api-server-abc",
    Namespace: "production",
    UID: "k8s-uid-123",
    Labels: {"app": "api", "version": "v2"},
    Attributes: {"owner": "platform-team"}
}
```

### 7. **Temporal Dimension** (Time Patterns)
```go
Temporal: {
    Period: "5m",
    Frequency: 0.8,
    Patterns: [{
        Name: "memory-spike-pattern",
        Window: "30m",
        Confidence: 0.9
    }],
    Seasonality: {"hourly": 0.7}
}
```

### 8. **Behavioral Dimension** (Anomalies)
```go
Behavioral: {
    Pattern: "unusual-memory-growth",
    Frequency: 0.1,  // Rare
    Confidence: 0.85,
    TimeWindow: {Start: "-1h", End: "now"}
}

Anomaly: {
    Score: 0.9,
    Type: "statistical",
    Description: "3x normal memory usage",
    BaselineComparison: {
        Deviation: 3.2,
        Significance: 0.95,
        ZScore: 3.5
    }
}
```

## How Correlation Leverages All Dimensions

The correlation system uses ALL dimensions to find relationships:

### K8s Native Correlations
- Uses **Entity** dimension for owner references
- Uses **Layer** dimension for K8s-specific data
- Uses **Trace** dimension for deployment causality

### Temporal Correlations
- Uses **Temporal** dimension for time patterns
- Uses **Behavioral** dimension for anomaly co-occurrence
- Uses **Impact** dimension to prioritize critical patterns

### Sequence Correlations
- Uses **Correlation** dimension for causal chains
- Uses **Trace** dimension for span hierarchy
- Uses **Semantic** dimension for intent patterns

## Example: Full Multi-Dimensional Event

```go
// This single event tells a complete story across all dimensions
event := &UnifiedEvent{
    // Identity
    ID: "evt-oom-123",
    Type: EventTypeKubernetes,
    
    // OTEL Trace Dimension
    TraceContext: &TraceContext{
        TraceID: "trace-deploy-456",
        SpanID: "span-oom-789",
        ParentSpanID: "span-scale-456",
        Baggage: map[string]string{
            "operation": "rolling-update",
            "triggered_by": "auto-scaler",
        },
    },
    
    // Semantic Dimension
    Semantic: &SemanticContext{
        Intent: "resource_exhaustion",
        Category: "reliability",
        Narrative: "API server pod killed due to memory exhaustion during rolling update",
        Concepts: []string{"memory-management", "pod-lifecycle", "scaling"},
        Confidence: 0.95,
    },
    
    // Multiple Layer Dimensions
    Kernel: &KernelData{
        Syscall: "oom_kill",
        PID: 12345,
    },
    Application: &ApplicationData{
        Level: "error",
        Message: "Java heap space",
        StackTrace: "...",
    },
    Kubernetes: &KubernetesData{
        EventType: "pod_oom_killed",
        ObjectKind: "Pod",
        Object: "api-server-v2-abc",
    },
    
    // Impact Dimension
    Impact: &ImpactContext{
        Severity: "critical",
        BusinessImpact: 0.9,
        CustomerFacing: true,
        AffectedServices: []string{"api", "mobile-app"},
    },
    
    // Correlation Dimension
    Correlation: &CorrelationContext{
        Pattern: "deployment-memory-cascade",
        CausalChain: []string{"evt-deploy-1", "evt-scale-2", "evt-oom-123"},
        Stage: "failure",
    },
    
    // Behavioral Dimension
    Anomaly: &AnomalyInfo{
        Score: 0.92,
        Type: "resource-spike",
        Description: "Memory usage 5x above baseline",
    },
}
```

## Benefits of Multi-Dimensional Events

1. **Complete Context**: Every event carries its full story
2. **Rich Correlations**: Can correlate across any dimension
3. **No Information Loss**: All perspectives preserved
4. **Single Source of Truth**: One event type for everything
5. **ML-Ready**: Multiple features for pattern detection
6. **Human-Readable**: Semantic narratives for operators

## Correlation System Integration

The correlation system leverages all dimensions:

```go
// K8s correlations use Entity + K8s dimensions
k8sCorr := correlator.FindK8sRelationships(event.Entity, event.Kubernetes)

// Temporal correlations use Time + Behavioral dimensions
tempCorr := correlator.FindTemporalPatterns(event.Temporal, event.Behavioral)

// Trace correlations use OTEL + Correlation dimensions
traceCorr := correlator.FindTraceRelationships(event.TraceContext, event.Correlation)

// Impact correlations use Impact + Semantic dimensions
impactCorr := correlator.FindImpactChains(event.Impact, event.Semantic)
```

## Future Dimensions

The UnifiedEvent structure is extensible for future dimensions:
- **Security Dimension**: Attack patterns, vulnerabilities
- **Cost Dimension**: Resource costs, budget impact
- **Compliance Dimension**: Policy violations, audit trails
- **ML Dimension**: Predictions, classifications

This multi-dimensional approach makes UnifiedEvent the foundation for next-generation observability.