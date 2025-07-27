# NATS Integration for Tapio Architecture

## Why NATS Makes Sense

NATS is a perfect match for Tapio's event-driven, multi-dimensional correlation architecture:

1. **High Performance**: Millions of msgs/sec matches our 165k events/sec target
2. **Low Latency**: Sub-millisecond delivery for real-time correlation
3. **Cloud Native**: Built for Kubernetes, supports multi-cluster
4. **Simple**: No complex configuration - aligns with "zero-config" philosophy

## Proposed Architecture with NATS

```
┌─────────────────┐
│   Collectors    │
│  (eBPF, K8s,    │
│   Network, App) │
└────────┬────────┘
         │ Publish UnifiedEvents
         ▼
┌─────────────────────────────────────────────┐
│                NATS JetStream               │
│                                             │
│  Subjects:                                  │
│  • events.kernel.syscall                    │
│  • events.k8s.pod.{namespace}.{name}       │
│  • events.network.{protocol}.{service}     │
│  • events.app.{service}.{level}            │
│                                             │
│  Streams:                                   │
│  • UNIFIED_EVENTS (all events)             │
│  • HIGH_IMPACT (filtered by impact > 0.7)  │
│  • TRACES (grouped by trace_id)            │
└─────────────────┬───────────────────────────┘
                  │
     ┌────────────┴────────────┬────────────┐
     ▼                         ▼            ▼
┌─────────────┐  ┌──────────────────┐  ┌─────────────┐
│  Analytics  │  │   Correlation    │  │ Persistence │
│   Engine    │  │     System       │  │   Service   │
│             │  │                  │  │             │
│ Subscribe:  │  │ Subscribe:       │  │ Subscribe:  │
│ events.*    │  │ events.k8s.*     │  │ events.*    │
│             │  │ events.*.error   │  │ (sample)    │
└─────────────┘  └──────────────────┘  └─────────────┘
```

## Key Integration Points

### 1. Event Publishing (Collectors → NATS)

```go
// UnifiedEvent publisher with subject routing
type NATSEventPublisher struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func (p *NATSEventPublisher) PublishEvent(event *domain.UnifiedEvent) error {
    // Multi-dimensional subject routing
    subjects := p.generateSubjects(event)
    
    // Publish to primary subject
    primarySubject := subjects[0]
    data, _ := json.Marshal(event)
    
    // Use JetStream for persistence
    _, err := p.js.Publish(primarySubject, data,
        nats.MsgId(event.ID),
        nats.Header("Trace-ID", event.TraceContext.TraceID),
        nats.Header("Impact", fmt.Sprintf("%.2f", event.Impact.BusinessImpact)),
    )
    
    return err
}

func (p *NATSEventPublisher) generateSubjects(event *domain.UnifiedEvent) []string {
    subjects := []string{}
    
    // Layer-based routing
    if event.IsKubernetesEvent() {
        subjects = append(subjects, fmt.Sprintf("events.k8s.%s.%s.%s",
            event.Kubernetes.EventType,
            event.Entity.Namespace,
            event.Entity.Name))
    }
    
    // Severity-based routing
    if event.GetSeverity() == "critical" {
        subjects = append(subjects, "events.critical")
    }
    
    // Trace-based routing for correlation
    if event.HasTraceContext() {
        subjects = append(subjects, fmt.Sprintf("traces.%s", event.TraceContext.TraceID))
    }
    
    return subjects
}
```

### 2. Correlation System Integration

```go
// NATS-based correlation event source
type NATSCorrelationSource struct {
    nc     *nats.Conn
    js     nats.JetStreamContext
    system *SimpleCorrelationSystem
}

func (s *NATSCorrelationSource) Start() error {
    // Subscribe to relevant events for correlation
    
    // 1. K8s events for native correlation
    s.js.Subscribe("events.k8s.>", s.handleK8sEvent,
        nats.Durable("correlation-k8s"),
        nats.DeliverAll(),
    )
    
    // 2. High-impact events for priority correlation
    s.js.Subscribe("events.critical", s.handleCriticalEvent,
        nats.Durable("correlation-critical"),
    )
    
    // 3. Trace-grouped events for causal correlation
    s.js.QueueSubscribe("traces.>", "correlation-workers", s.handleTraceEvent,
        nats.Durable("correlation-traces"),
    )
    
    return nil
}

func (s *NATSCorrelationSource) handleK8sEvent(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    json.Unmarshal(msg.Data, event)
    
    // Process for correlation
    ctx := context.Background()
    s.system.ProcessEvent(ctx, event)
    
    // Publish correlations back to NATS
    insights := s.system.Insights()
    for insight := range insights {
        s.publishInsight(insight)
    }
}
```

### 3. Stream Processing for Analytics

```go
// JetStream for event replay and time windows
func setupJetStreamAnalytics(js nats.JetStreamContext) error {
    // Main event stream with retention
    _, err := js.AddStream(&nats.StreamConfig{
        Name:     "UNIFIED_EVENTS",
        Subjects: []string{"events.>"},
        Storage:  nats.FileStorage,
        Retention: nats.WorkQueuePolicy,
        MaxAge:   24 * time.Hour,
        Replicas: 3,
    })
    
    // High-impact filtered stream
    _, err = js.AddStream(&nats.StreamConfig{
        Name:     "HIGH_IMPACT",
        Sources: []*nats.StreamSource{{
            Name: "UNIFIED_EVENTS",
            FilterSubject: "events.>",
        }},
        Storage: nats.MemoryStorage,
    })
    
    // Consumer for time-window analytics
    _, err = js.AddConsumer("UNIFIED_EVENTS", &nats.ConsumerConfig{
        Name:          "analytics-5min-window",
        DeliverPolicy: nats.DeliverLastPerSubjectPolicy,
        AckWait:       30 * time.Second,
        MaxAckPending: 10000,
    })
    
    return err
}
```

### 4. Real-time Correlation Mesh

```go
// Distributed correlation using NATS Request-Reply
type DistributedCorrelation struct {
    nc *nats.Conn
}

func (d *DistributedCorrelation) RequestCorrelation(event *domain.UnifiedEvent) ([]*Correlation, error) {
    data, _ := json.Marshal(event)
    
    // Request correlation from all correlation nodes
    msg, err := d.nc.Request("correlation.request", data, 100*time.Millisecond)
    if err != nil {
        return nil, err
    }
    
    var correlations []*Correlation
    json.Unmarshal(msg.Data, &correlations)
    return correlations, nil
}

// Each correlation node subscribes
func (d *DistributedCorrelation) ServeCorrelations() {
    d.nc.Subscribe("correlation.request", func(msg *nats.Msg) {
        event := &domain.UnifiedEvent{}
        json.Unmarshal(msg.Data, event)
        
        // Quick correlation check
        correlations := d.findQuickCorrelations(event)
        data, _ := json.Marshal(correlations)
        
        msg.Respond(data)
    })
}
```

## Benefits for Tapio

### 1. **Scalability**
- Horizontal scaling of correlation workers
- Fan-out to multiple analytics pipelines
- Backpressure handling for event storms

### 2. **Reliability**
- JetStream persistence for event replay
- Exactly-once delivery for critical events
- Automatic failover with clustering

### 3. **Flexibility**
- Dynamic subscription patterns
- Subject-based routing for multi-tenancy
- Easy integration of new components

### 4. **Performance**
- Memory-based streams for hot paths
- Parallel processing with queue groups
- Native support for millions of msgs/sec

### 5. **Observability**
- Built-in monitoring endpoints
- Message tracing capabilities
- Stream/consumer metrics

## Implementation Strategy

### Phase 1: Event Bus (Week 1)
- Replace channels with NATS pub/sub
- Basic subject hierarchy
- Memory-based operation

### Phase 2: Persistence (Week 2)
- Enable JetStream
- Configure retention policies
- Implement replay capabilities

### Phase 3: Distributed Correlation (Week 3)
- Queue groups for correlation workers
- Request-reply for real-time correlation
- Cross-cluster event federation

### Phase 4: Advanced Features (Week 4)
- Key-value store for correlation state
- Object store for large events
- Leaf nodes for edge correlation

## Configuration Example

```yaml
# nats-server.conf
jetstream {
  store_dir: "/data/nats"
  max_memory_store: 10GB
  max_file_store: 100GB
}

cluster {
  name: "tapio-cluster"
  routes: [
    "nats://nats-1:6222"
    "nats://nats-2:6222"
    "nats://nats-3:6222"
  ]
}

# Subject hierarchy
accounts {
  TAPIO {
    jetstream: enabled
    users: [
      {user: collector, permissions: {publish: ["events.>"]}}
      {user: analytics, permissions: {subscribe: ["events.>", "insights.>"]}}
      {user: correlation, permissions: {publish: ["insights.>"], subscribe: ["events.>"]}}
    ]
  }
}
```

## Conclusion

NATS provides the perfect event backbone for Tapio:
- **Zero-config philosophy**: Simple subjects, auto-discovery
- **Performance**: Exceeds 165k events/sec requirement
- **Kubernetes-native**: StatefulSets, operators available
- **Multi-dimensional routing**: Subject hierarchy matches event dimensions

The integration would make Tapio more scalable, resilient, and easier to extend while maintaining the simplicity goal.