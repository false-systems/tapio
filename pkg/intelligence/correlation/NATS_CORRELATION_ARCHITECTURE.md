# NATS Correlation Architecture

## Design Principles

1. **K8s-First**: Every design decision optimizes for Kubernetes observability
2. **Context Over Count**: One meaningful story beats 100 raw alerts
3. **Flexible Transport**: NATS is an implementation detail, not a core dependency

## System Architecture

### High-Level Flow

```
Data Collection Layer
└─── Kernel Events (eBPF)
└─── K8s API Events  
└─── Network Events
└─── Application Logs
     │
     ▼
Event Enrichment Layer
└─── Add OTEL Context
└─── Add K8s Metadata
└─── Add Relationships
     │
     ▼
Distribution Layer (NATS)
└─── Type-based Topics
└─── Trace-based Topics
└─── Priority Queues
     │
     ▼
Correlation Layer
└─── Temporal Correlation
└─── Spatial Correlation  
└─── Causal Analysis
└─── Semantic Grouping
     │
     ▼
Presentation Layer
└─── Single Root Cause
└─── Clear Narrative
└─── Actionable Insight
```

### Component Details

#### 1. NATS Subscriber

**Purpose**: Groups related events for correlation processing

**Key Features**:
- Subscribes to trace-specific subjects
- Maintains sliding time windows
- Handles backpressure
- Supports parallel processing

**Implementation**:
```go
type NATSSubscriber struct {
    nc                *nats.Conn
    js                JetStreamContext
    correlationEngine CorrelationEngine
    traceEvents       map[string][]*UnifiedEvent
}
```

#### 2. Event Flow Patterns

**Pattern 1: Trace-Based Routing**
```
Event with TraceID "abc123"
    │
    ├──▶ Subject: "events.ebpf.syscall"     (type-based)
    └──▶ Subject: "traces.abc123"           (trace-based)
```

**Pattern 2: Correlation Windows**
```
Timeline: ──────────────────────────────────▶
          
Window 1: [████████████]
          ↑   ↑   ↑
          E1  E2  E3  → Correlate
          
Window 2:     [████████████]
              ↑   ↑   ↑
              E2  E3  E4  → Correlate
```

**Pattern 3: Multi-Dimensional Grouping**
```
TraceID: xyz789
├── Temporal: Events within 30s window
├── Spatial: Same namespace/node/pod
├── Causal: Direct cause-effect chain
└── Semantic: Similar error patterns
```

### Data Structures

#### Raw Event (from Collectors)
```go
type RawEvent struct {
    Type      string              // "ebpf", "kubeapi", "systemd"
    TraceID   string              // OTEL trace identifier
    SpanID    string              // OTEL span identifier
    Timestamp time.Time           
    Data      []byte              // Collector-specific payload
    Metadata  map[string]string   // K8s enrichment
}
```

#### Unified Event (after Transformation)
```go
type UnifiedEvent struct {
    ID           string
    Timestamp    time.Time
    Type         string
    
    // K8s Contexts
    K8sContext   *K8sContext
    Entity       *EntityContext
    
    // OTEL Context
    TraceContext *TraceContext
    
    // Correlation Hints
    Semantic     *SemanticContext
    Impact       *ImpactContext
}
```

#### Correlation Result
```go
type CorrelationResult struct {
    ID               string
    TraceID          string
    Events           []string        // Event IDs
    RootCauseEventID string
    Confidence       float64
    
    // Human-readable output
    Summary          string
    RootCause        string
    Recommendation   string
}
```

### Processing Pipeline

#### Stage 1: Event Reception
```
NATS Message
    │
    ▼
Parse & Validate
    │
    ▼
Extract TraceID
    │
    ▼
Add to Trace Group
```

#### Stage 2: Window Processing
```
Every CorrelationWindow/2:
    │
    ▼
Find Mature Traces
    │
    ▼
Extract Event Groups
    │
    ▼
Send to Correlation
```

#### Stage 3: Correlation
```
Event Group
    │
    ▼
Transform to UnifiedEvents
    │
    ▼
Multi-Dimensional Analysis
    │
    ▼
Generate Results
```

### Scalability Considerations

#### Horizontal Scaling
- Queue groups for parallel processing
- Partitioned by trace ID prefix
- Stateless workers

#### Performance Optimization
- Batch processing
- Async publishing
- Memory-bounded caches
- Configurable timeouts

#### Failure Handling
- At-least-once delivery
- Idempotent processing
- Dead letter queues
- Circuit breakers

### Integration Points

#### With Collectors
```
Collectors → RawEvent → NATS Publisher → Subjects
                ↑
                └── Standardized format
```

#### With Correlation Engine
```
NATS Subscriber → Event Groups → Correlation Engine
                                          ↓
                                  Correlation Results
```

#### With Storage
```
Results → Time Series DB (metrics)
        → Document Store (events)
        → Graph DB (relationships)
```

### Configuration Management

#### Environment-Based
```yaml
nats:
  url: ${NATS_URL:-nats://localhost:4222}
  stream: ${NATS_STREAM:-TAPIO_EVENTS}
  
correlation:
  window: ${CORRELATION_WINDOW:-30s}
  min_events: ${MIN_EVENTS:-2}
```

#### Dynamic Updates
- Config hot-reload
- Graceful transitions
- Zero-downtime updates

### Observability

#### Metrics
- Events processed/sec
- Correlation latency
- Window sizes
- Memory usage

#### Tracing
- Correlation operations
- Event flow paths
- Processing bottlenecks

#### Logging
- Structured logs
- Correlation IDs
- Debug levels

### Security Considerations

- TLS for NATS connections
- Authentication/authorization
- Event data encryption
- PII handling

### Future Considerations

This architecture is designed to be transport-agnostic. NATS could be replaced with:
- Direct memory channels
- Kafka
- Redis Streams
- Custom K8s operators

The core correlation logic remains independent of the transport mechanism.

---

## Appendix: Why This Architecture?

### Problem Space
- K8s generates massive event volumes
- Operators suffer from alert fatigue
- Root causes are obscured by noise
- Context is lost across components

### Solution Approach
- Collect comprehensive telemetry
- Group by operational context (traces)
- Apply intelligent correlation
- Deliver single, meaningful narrative

### Key Innovation
Moving from "alert on everything" to "understand what matters" through context-aware correlation.