# NATS → Correlation System Flow

## How Events Flow Through the System

### 1. Event Publishing (eBPF Collector → NATS)

```go
// In eBPF collector (already implemented)
event := &domain.UnifiedEvent{
    ID: "ebpf-oom-001",
    Type: "ebpf",
    TraceContext: &TraceContext{
        TraceID: "trace-mysql-failure-123",
    },
    K8sContext: &K8sContext{
        Name: "mysql-pod-abc",
        Namespace: "production",
    },
    Kernel: &KernelData{
        Syscall: "oom_kill",
    },
}

// Publishes to: traces.trace-mysql-failure-123.ebpf
natsPublisher.PublishEvent(event)
```

### 2. NATS Subscription & Routing

```go
// NATS Integration subscribes to all trace events
nats.Subscribe("traces.>", func(msg *nats.Msg) {
    // Extract trace ID from subject
    // traces.trace-mysql-failure-123.ebpf → trace-mysql-failure-123
    
    // Parse event
    event := parseEvent(msg.Data)
    
    // Send to correlation system
    correlationSystem.ProcessEvent(ctx, event)
})
```

### 3. Correlation Processing

```go
// SimpleCorrelationSystem processes the event
func (s *SimpleCorrelationSystem) ProcessEvent(ctx context.Context, event *UnifiedEvent) {
    // 1. K8s Native Correlator
    k8sCorrelator.Process(event) 
    // → Finds: mysql-pod-abc is owned by mysql-statefulset
    // → Finds: mysql-service depends on mysql-pod-abc
    
    // 2. Temporal Correlator  
    temporalCorrelator.Process(event)
    // → Groups with other events in 2-minute window
    
    // 3. Sequence Detector
    sequenceDetector.Process(event)
    // → Detects: Start of cascade pattern
    
    // 4. Pattern Matcher
    patterns.Match([]*UnifiedEvent{...})
    // → Matches: CascadingFailurePattern
}
```

### 4. Correlation Results

```go
// When pattern is detected
result := &CorrelationResult{
    Type: "cascading_failure",
    RootCause: "mysql-pod-abc OOM at 10:00:00",
    RelatedEvents: [
        "mysql-pod-abc OOM",
        "api-timeout to mysql-service",
        "frontend-503 from api-service"
    ],
    Impact: {
        Services: ["mysql", "api", "frontend"],
        Pods: 15,
    },
    Remediation: "Increase mysql memory limits",
}

// Publish back to NATS
nats.PublishCorrelationResult(result)
// → Publishes to: correlations.results
```

## Key Integration Points

### 1. NATS Subjects Structure
```
traces.{traceID}.{source}     # Input events
correlations.results          # Correlation outputs
correlations.alerts          # High-priority correlations
```

### 2. Event Grouping by Trace ID
- NATS subscriber automatically groups events by trace ID
- All events with same trace ID are correlated together
- Enables tracking of distributed traces

### 3. Real-time Processing
- Events processed as they arrive from NATS
- Correlation state maintained across events
- Results published back to NATS for consumers

### 4. Integration with Existing System
- Uses existing `SimpleCorrelationSystem`
- Leverages `K8sRelationshipLoader` for context
- Patterns from `correlation_patterns.go`
- State tracking from event tracker

## Benefits of NATS Integration

1. **Decoupled Architecture**: Collectors don't need to know about correlation
2. **Scalable**: Can add more correlation instances
3. **Reliable**: JetStream provides persistence
4. **Real-time**: Events processed as they arrive
5. **Trace-aware**: Natural grouping by trace ID

## Example End-to-End Flow

1. **10:00:00** - MySQL OOM event published to `traces.trace-123.ebpf`
2. **10:00:01** - Correlation system receives event, starts tracking
3. **10:00:15** - API timeout published to `traces.trace-123.api`  
4. **10:00:16** - Correlation system correlates with MySQL event
5. **10:00:30** - Frontend error published to `traces.trace-123.frontend`
6. **10:00:31** - Pattern detected, correlation result published
7. **10:00:32** - Alert consumers receive correlation via NATS

This creates a complete feedback loop where raw events become actionable correlations.