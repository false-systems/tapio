# CollectorManager ↔ DataFlow Integration

This document explains how the CollectorManager (L3) integrates with the DataFlow system (L2) to provide semantic correlation and OTEL tracing.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     L3: Integration Layer                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐    Event Stream    ┌──────────────────┐   │
│  │ CollectorManager│ ──────────────────▶ │   DataFlow       │   │
│  │                 │                     │  (Semantic       │   │
│  │ • Aggregates    │                     │   Correlation)   │   │
│  │   L1 events     │                     │                  │   │
│  │ • Manages       │                     │ • OTEL tracing   │   │
│  │   lifecycles    │                     │ • Event grouping │   │
│  │ • Health checks │                     │ • Enrichment     │   │
│  └─────────────────┘                     └──────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Integration Flow

### 1. Event Collection (L1 → L3)
```go
// CollectorManager aggregates events from all collectors
for event := range collectorManager.Events() {
    // Raw events from K8s, systemd, eBPF, CNI collectors
    inputEvents <- event
}
```

### 2. Semantic Processing (L3 → L2)
```go
// DataFlow processes events through intelligence layer
dataFlow := dataflow.NewTapioDataFlow(config)
dataFlow.Connect(inputEvents, outputEvents)

// Intelligence layer adds:
// - Semantic correlation
// - OTEL trace context
// - Event grouping
// - Impact assessment
```

### 3. Enriched Output (L2 → L3)
```go
// Enriched events come back with correlation data
for enrichedEvent := range outputEvents {
    // Events now have:
    // - Trace IDs for correlation
    // - Semantic grouping
    // - Business impact scores
    // - Related event chains
}
```

## Data Flow Components

### TapioDataFlow (L2: Intelligence)
- **Semantic Correlation**: Groups related events across collectors
- **OTEL Integration**: Adds distributed tracing context
- **Event Enrichment**: Enhances events with correlation metadata
- **Intelligence Engine**: AI-powered event analysis

### ServerBridge (L3: Integration)
- **Server Forwarding**: Sends enriched events to Tapio server
- **Batch Processing**: Efficient bulk transmission
- **OTEL Propagation**: Maintains trace context across services
- **Error Handling**: Robust delivery guarantees

## Configuration Example

```go
// CollectorManager configuration
manager := NewCollectorManager()
manager.AddCollector("k8s", k8sCollector)
manager.AddCollector("systemd", systemdCollector)

// DataFlow configuration for semantic correlation
dataFlowConfig := dataflow.Config{
    EnableSemanticGrouping: true,
    GroupRetentionPeriod:   30 * time.Minute,
    ServiceName:            "tapio-collector",
    ServiceVersion:         "2.0.0",
    Environment:            "production",
    BufferSize:             1000,
    FlushInterval:          time.Second,
}

// Create channels for event flow
inputEvents := make(chan domain.Event, config.BufferSize)
outputEvents := make(chan domain.Event, config.BufferSize)

// Connect components
dataFlow := dataflow.NewTapioDataFlow(dataFlowConfig)
dataFlow.Connect(inputEvents, outputEvents)

// Bridge to server
bridge, err := dataflow.NewServerBridge(bridgeConfig, dataFlow)
```

## Event Transformation

### Raw Event (from Collectors)
```go
domain.Event{
    ID:        "k8s_pod_created_123",
    Type:      domain.EventTypeKubernetes,
    Source:    "k8s-collector",
    Timestamp: time.Now(),
    // Basic event data
}
```

### Enriched Event (after DataFlow)
```go
domain.Event{
    ID:        "k8s_pod_created_123",
    Type:      domain.EventTypeKubernetes,
    Source:    "k8s-collector",
    Timestamp: time.Now(),
    
    // Added by DataFlow (L2):
    TraceContext: &domain.TraceContext{
        TraceID: "abc123def456",
        SpanID:  "span789",
    },
    
    Correlation: &domain.CorrelationContext{
        CorrelationID: "pod-startup-sequence",
        Pattern:       "kubernetes-deployment",
        RelatedEvents: ["systemd_docker_start", "cni_ip_allocated"],
    },
    
    Impact: &domain.ImpactContext{
        BusinessImpact: 0.7,
        CustomerFacing: true,
        SLOImpact:     false,
    },
}
```

## Semantic Correlation Examples

### Cross-Collector Correlation
```
Timeline: Pod Startup Sequence

1. SystemD: docker.service started
   ├─ TraceID: abc123
   ├─ CorrelationID: pod-startup-seq-1
   └─ Pattern: container-runtime-start

2. CNI: IP allocated to pod
   ├─ TraceID: abc123  (same trace!)
   ├─ CorrelationID: pod-startup-seq-1
   └─ Pattern: network-setup

3. K8s: Pod running
   ├─ TraceID: abc123  (same trace!)
   ├─ CorrelationID: pod-startup-seq-1
   └─ Pattern: workload-ready

DataFlow Intelligence:
└─ Semantic Group: "pod-deployment-success"
   ├─ Events: [1, 2, 3]
   ├─ Duration: 2.3s
   ├─ Business Impact: 0.3 (routine operation)
   └─ Root Cause: "scheduled deployment"
```

### Failure Correlation
```
Timeline: Service Failure Cascade

1. SystemD: nginx.service failed
   ├─ Severity: critical
   ├─ CorrelationID: service-failure-cascade
   └─ Pattern: service-failure

2. K8s: Pod restart loop
   ├─ TraceID: def456
   ├─ CorrelationID: service-failure-cascade  
   └─ Pattern: workload-degradation

3. eBPF: High error rate detected
   ├─ TraceID: def456
   ├─ CorrelationID: service-failure-cascade
   └─ Pattern: performance-degradation

DataFlow Intelligence:
└─ Semantic Group: "cascade-failure"
   ├─ Events: [1, 2, 3]
   ├─ Business Impact: 0.9 (customer-affecting)
   ├─ Root Cause: "nginx configuration error"
   └─ Recommended Actions: ["restart nginx", "check config"]
```

## Performance Characteristics

### Throughput
- **Input**: 165k+ events/sec from all collectors combined
- **Processing**: Real-time semantic correlation
- **Output**: Enriched events with <10ms added latency

### Buffering Strategy
```go
// Multi-stage buffering prevents blocking
CollectorManager → [Buffer 10k] → DataFlow → [Buffer 10k] → ServerBridge
```

### Memory Efficiency
- **Streaming architecture**: No event storage
- **Bounded channels**: Prevents memory leaks
- **Context cancellation**: Clean shutdown

## Monitoring Integration

### Metrics Collection
```go
// CollectorManager metrics
stats := manager.Statistics()
log.Printf("Active collectors: %d", stats.ActiveCollectors)

// DataFlow metrics  
dataFlowStats := dataFlow.Statistics()
log.Printf("Events processed: %d", dataFlowStats.EventsProcessed)
log.Printf("Semantic groups: %d", dataFlowStats.GroupsCreated)

// ServerBridge metrics
bridgeStats := bridge.Statistics()
log.Printf("Events forwarded: %d", bridgeStats.EventsSent)
```

### Health Checks
```go
// Check entire pipeline health
if !manager.IsHealthy() {
    log.Error("Collector manager unhealthy")
}

if !dataFlow.IsHealthy() {
    log.Error("DataFlow processing stalled")
}

if !bridge.IsHealthy() {
    log.Error("Server bridge disconnected")
}
```

## Error Handling

### Collector Failures
- **Isolation**: One collector failure doesn't affect others
- **Graceful degradation**: Correlation continues with remaining data
- **Automatic recovery**: Failed collectors can restart independently

### DataFlow Errors
- **Event buffering**: Temporary processing failures don't lose events
- **Retry logic**: Failed correlations are retried
- **Fallback mode**: Raw events forwarded if correlation fails

### Server Bridge Errors
- **Local buffering**: Events queued during server outages
- **Exponential backoff**: Intelligent retry strategy
- **Circuit breaker**: Prevents cascade failures

## Development Workflow

### Adding New Collectors
1. Implement `Collector` interface in L1
2. Register with CollectorManager in L3
3. Events automatically flow through DataFlow (L2)
4. Semantic correlation works immediately

### Extending Correlation Rules
1. Add rules to intelligence layer (L2)
2. CollectorManager integration unchanged
3. Enhanced correlations appear in output stream

### Testing Integration
```bash
# Test full pipeline
make test-integration

# Test collector → dataflow → server flow
make test-e2e

# Performance testing
make test-performance
```

This integration provides the foundation for Tapio's semantic correlation platform, connecting raw observability events with intelligent analysis and enterprise-grade delivery.