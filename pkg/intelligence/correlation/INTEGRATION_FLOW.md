# Analytics and OTEL Integration Flow

## Overview

This document describes how the new correlation system integrates with the analytics engine and OTEL trace propagation.

## Architecture Flow

```
┌─────────────────────┐
│   UnifiedEvent      │
│ (with OTEL Context) │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Analytics Engine   │
│                     │
│ ┌─────────────────┐ │
│ │ Event Pipeline  │ │
│ │                 │ │
│ │ 1. Validation   │ │
│ │ 2. Enrichment   │ │
│ │ 3. Correlation  │ │──┐
│ │ 4. Analytics    │ │  │
│ └─────────────────┘ │  │
└─────────────────────┘  │
                         │
                         ▼
        ┌────────────────────────────┐
        │ AnalyticsCorrelationAdapter│
        │                            │
        │ Implements:                │
        │ - CorrelationEngine        │
        │ - OTEL Trace Grouping      │
        │ - Finding Generation       │
        └────────────┬───────────────┘
                     │
                     ▼
        ┌────────────────────────────┐
        │  SimpleCorrelationSystem   │
        │                            │
        │ ┌──────────────────────┐  │
        │ │ K8sNativeCorrelator  │  │
        │ └──────────────────────┘  │
        │                            │
        │ ┌──────────────────────┐  │
        │ │ TemporalCorrelator   │  │
        │ └──────────────────────┘  │
        │                            │
        │ ┌──────────────────────┐  │
        │ │ SequenceDetector     │  │
        │ └──────────────────────┘  │
        └────────────┬───────────────┘
                     │
                     ▼
        ┌────────────────────────────┐
        │    Correlation Store       │
        │  (Historical Analysis)     │
        └────────────────────────────┘
```

## Data Flow

### 1. Event Ingestion with Enhanced OTEL
```go
UnifiedEvent {
    ID: "evt-123",
    Type: EventTypeKubernetes,
    TraceContext: {
        TraceID: "trace-456",
        SpanID: "span-789",
        ParentSpanID: "span-456", // Hierarchical tracing
        Sampled: true,
        Baggage: {
            "deployment.name": "api-server",
            "user.id": "deploy-bot"
        }
    },
    Semantic: {
        Intent: "memory_exhaustion",
        Category: "resource_management", 
        Tags: ["oom", "pod-failure", "critical"],
        Narrative: "API server pod exceeded memory limit",
        Confidence: 0.95,
        Concepts: ["resource-limits", "container-lifecycle"]
    },
    KubernetesData: {
        EventType: "pod_oom_killed",
        ObjectKind: "Pod",
        Object: "api-server-xyz"
    },
    Impact: {
        Severity: "critical",
        BusinessImpact: 0.8,
        CustomerFacing: true,
        SLOImpact: true
    }
}
```

### 2. Analytics Processing
The event flows through the pipeline stages:
- **Validation**: Ensures required fields exist
- **Enrichment**: Adds semantic/impact context
- **Correlation**: Routes to our correlation adapter
- **Analytics**: Anomaly detection and scoring

### 3. Correlation Discovery
The AnalyticsCorrelationAdapter:
1. Passes event to SimpleCorrelationSystem
2. Tracks OTEL trace context for semantic grouping
3. Maintains event buffer for correlation context

SimpleCorrelationSystem discovers:
- K8s native correlations (owner references, selectors)
- Temporal patterns (co-occurrence)
- Sequence patterns (cascading failures)

### 4. Finding Generation
Correlation insights are converted to analytics findings:
```go
Finding {
    ID: "k8s-corr-123",
    PatternType: "owner_reference",
    Confidence: 0.95,
    Description: "Pod api-server-xyz is owned by Deployment api-server",
    SemanticGroup: {
        ID: "trace-456",
        Intent: "resource_exhaustion",
        Type: "k8s_Pod"
    }
}
```

### 5. Enhanced OTEL Context Propagation
The system leverages the enhanced UnifiedEvent structure:

#### Trace Hierarchy
- **TraceID**: Groups all events in a distributed operation
- **SpanID/ParentSpanID**: Creates causal chains within traces
- **Baggage**: Propagates deployment context across boundaries
- **Sampled**: Ensures consistent sampling decisions

#### Semantic Enhancement
- **Intent**: Machine-readable purpose (e.g., "memory_exhaustion")
- **Category**: High-level grouping (e.g., "resource_management")
- **Narrative**: Human-readable story
- **Concepts**: Related domain concepts for ML/search
- **Confidence**: Semantic interpretation confidence

#### Multi-Layer Context
The adapter tracks:
1. **Span Hierarchy**: Parent-child relationships for causality
2. **Semantic Cache**: Best semantic interpretation per trace
3. **Impact Propagation**: Business impact flows through traces
4. **Correlation Patterns**: Discovered patterns enhance future events

## Key Integration Points

### 1. CorrelationEngine Interface
```go
type CorrelationEngine interface {
    Start() error
    Stop() error
    ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error
    GetLatestFindings() *Finding
    GetSemanticGroups() []*SemanticGroup
}
```

### 2. Semantic Grouping
Events with the same TraceID are grouped semantically:
- Represents a single user operation or system action
- Preserves causality across distributed components
- Enables end-to-end correlation

### 3. Persistence Layer
All discovered correlations are persisted for:
- Historical analysis
- Pattern learning
- Feedback incorporation
- Trend detection

## Configuration

### Analytics Engine
```go
config := engine.DefaultConfig()
config.EnableSemanticGrouping = true
config.ConfidenceThreshold = 0.7

// Create correlation adapter
correlationSystem := correlation.NewSimpleCorrelationSystem(logger, correlationConfig)
adapter := correlation.NewAnalyticsCorrelationAdapter(correlationSystem, logger)

// Create analytics engine with adapter
analyticsEngine, err := engine.NewAnalyticsEngine(
    config,
    logger,
    eventPipeline,
    adapter,  // Our correlation adapter
    semanticTracer,
)
```

### Correlation System
```go
correlationConfig := correlation.DefaultSimpleSystemConfig()
correlationConfig.EnableK8sNative = true  // Zero-config K8s correlations
correlationConfig.EnableTemporal = true   // Time-based patterns
correlationConfig.EnableSequence = true   // Sequential patterns
```

## Benefits

1. **Zero Configuration**: K8s correlations work out of the box
2. **OTEL Native**: Full trace context propagation
3. **Multi-Dimensional**: Combines K8s, temporal, and sequence correlations
4. **Historical Learning**: Persists and learns from correlations
5. **High Performance**: Sub-microsecond K8s correlations (61ns/op)

## Next Steps

1. **API Endpoints** (Optional):
   - Query historical correlations
   - Provide correlation feedback
   - Export correlation patterns

2. **Enhanced OTEL Integration**:
   - Custom OTEL attributes for correlations
   - Correlation spans in traces
   - Metrics for correlation discovery

3. **Advanced Correlation**:
   - Graph-based correlation algorithms
   - ML-based pattern discovery
   - Cross-cluster correlation