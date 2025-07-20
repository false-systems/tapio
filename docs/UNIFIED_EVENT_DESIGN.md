# UnifiedEvent Design: The Heart of Cross-Layer Correlation

## Design Philosophy

The UnifiedEvent is designed with three core principles:

1. **Flexibility**: Only populate what's relevant
2. **Traceability**: OTEL context links everything
3. **Semantics**: Understand meaning, not just data

## Why We Need UnifiedEvent

### The Problem with Current Observability

Today's observability tools work in silos:
- **Prometheus**: Sees metrics but not traces
- **Jaeger**: Sees traces but not logs
- **ELK**: Sees logs but not kernel events
- **eBPF tools**: See kernel but not business context

**Result**: When production breaks, you need 5+ dashboards to understand why.

### Our Solution: One Event to Rule Them All

UnifiedEvent can represent:
- A kernel syscall from eBPF
- An HTTP request from Envoy
- A pod crash from Kubernetes
- A database query from your app
- A metric spike from Prometheus

**All with the same trace context linking them together.**

## Detailed Structure Breakdown

### Core Identity
```go
ID        string    // Globally unique identifier
Timestamp time.Time // When it happened
Type      EventType // What kind of event
Source    string    // Which collector generated it
```

Every event MUST have these fields.

### OTEL Trace Context (The Magic)
```go
TraceContext *TraceContext {
    TraceID      string // THE KEY - links all related events
    SpanID       string // This event's span
    ParentSpanID string // Parent in the trace tree
    TraceState   string // W3C trace state
    Baggage      map[string]string // Propagated context
    Sampled      bool   // Is this trace being sampled?
}
```

This is what makes correlation possible. A user request gets a TraceID, and that ID follows through:
- Frontend JavaScript
- API Gateway
- Microservices
- Database queries
- Kernel syscalls
- Network packets

### Semantic Context (Understanding)
```go
Semantic *SemanticContext {
    Intent     string   // "user-checkout", "cache-refresh", "health-check"
    Category   string   // "business-critical", "maintenance", "debugging"
    Tags       []string // ["payment", "high-priority", "customer-visible"]
    Narrative  string   // Human-readable: "User attempting to process payment"
    Confidence float64  // How sure are we about this interpretation?
}
```

This transforms raw events into business understanding.

### Layer-Specific Data

Only one of these is typically populated per event:

#### Kernel Data (eBPF Events)
```go
Kernel *KernelData {
    Syscall    string            // "open", "mmap", "connect"
    PID        uint32            // Process ID
    ReturnCode int32             // Success/error code
    StackTrace []string          // Kernel stack trace
    CPUCore    int               // Which CPU core
}
```

#### Network Data
```go
Network *NetworkData {
    Protocol   string // "HTTP", "gRPC", "TCP"
    StatusCode int    // HTTP 500, gRPC UNAVAILABLE
    Latency    int64  // Nanoseconds
    Path       string // "/api/v1/checkout"
}
```

#### Application Data
```go
Application *ApplicationData {
    Level      string // "error", "warn", "info"
    Message    string // "Payment processing failed"
    ErrorType  string // "DatabaseTimeout"
    StackTrace string // Application stack trace
    UserID     string // Affected user
    RequestID  string // Correlate with requests
}
```

#### Kubernetes Data
```go
Kubernetes *KubernetesData {
    EventType  string // "Warning", "Normal"
    Reason     string // "OOMKilled", "BackOff"
    Object     string // "pod/payment-service-abc123"
    Message    string // "Container exceeded memory limit"
}
```

### Impact & Correlation

#### Business Impact
```go
Impact *ImpactContext {
    Severity         string   // "critical", "high", "medium", "low"
    BusinessImpact   float64  // 0.0-1.0 score
    AffectedServices []string // ["payment-api", "checkout-ui"]
    CustomerFacing   bool     // Visible to customers?
    RevenueImpacting bool     // Affects revenue?
    SLOImpact        bool     // Violates SLOs?
}
```

#### Correlation Context
```go
Correlation *CorrelationContext {
    CorrelationID string   // Groups related events
    GroupID       string   // Semantic group
    CausalChain   []string // [event1_id, event2_id, ...] in causal order
    Pattern       string   // "cascading-timeout-pattern"
}
```

## Real-World Examples

### Example 1: Payment Failure

```go
// 1. User clicks "Pay Now"
event1 := &UnifiedEvent{
    ID:        "evt-001",
    Type:      "application.request",
    Timestamp: time.Now(),
    TraceContext: &TraceContext{
        TraceID: "trace-payment-123",
        SpanID:  "span-frontend",
    },
    Application: &ApplicationData{
        Level:     "info",
        Message:   "Payment initiated",
        UserID:    "user-456",
        RequestID: "req-789",
    },
    Semantic: &SemanticContext{
        Intent:   "payment-processing",
        Category: "business-critical",
        Tags:     []string{"revenue", "checkout"},
    },
}

// 2. Database query timeout
event2 := &UnifiedEvent{
    ID:        "evt-002",
    Type:      "network.timeout",
    Timestamp: time.Now().Add(5*time.Second),
    TraceContext: &TraceContext{
        TraceID:      "trace-payment-123", // SAME TRACE!
        SpanID:       "span-database",
        ParentSpanID: "span-api",
    },
    Network: &NetworkData{
        Protocol:   "postgresql",
        Latency:    30_000_000_000, // 30 seconds
        StatusCode: 0, // timeout
    },
}

// 3. Kernel OOM Kill
event3 := &UnifiedEvent{
    ID:        "evt-003",
    Type:      "kernel.oom",
    Timestamp: time.Now().Add(5*time.Second),
    TraceContext: &TraceContext{
        TraceID: "trace-payment-123", // SAME TRACE!
        SpanID:  "span-kernel",
    },
    Kernel: &KernelData{
        Syscall:    "oom_kill",
        PID:        12345,
        ReturnCode: -9,
    },
    Impact: &ImpactContext{
        Severity:         "critical",
        BusinessImpact:   0.9,
        CustomerFacing:   true,
        RevenueImpacting: true,
    },
}
```

### Example 2: Performance Degradation

```go
// Gradual memory leak leading to cascading failures
events := []*UnifiedEvent{
    {
        Type: "kernel.syscall",
        Kernel: &KernelData{
            Syscall:    "mmap",
            ReturnCode: -12, // ENOMEM
        },
        Semantic: &SemanticContext{
            Intent: "memory-allocation-failure",
        },
    },
    {
        Type: "application.error",
        Application: &ApplicationData{
            Level:   "warn",
            Message: "Cache allocation failed, using disk",
        },
    },
    {
        Type: "network.latency",
        Network: &NetworkData{
            Latency: 5_000_000_000, // 5 seconds
        },
        Impact: &ImpactContext{
            BusinessImpact: 0.6,
            CustomerFacing: true,
        },
    },
}
```

## Conversion Examples

### From eBPF Event
```go
func ConvertEBPFToUnified(ebpfEvent *BPFEvent) *UnifiedEvent {
    return &UnifiedEvent{
        ID:        GenerateEventID(),
        Type:      "kernel.syscall",
        Timestamp: time.Unix(0, ebpfEvent.Timestamp),
        Source:    "ebpf-collector",
        TraceContext: extractTraceContext(ebpfEvent), // Extract from thread local storage
        Kernel: &KernelData{
            Syscall:    ebpfEvent.Syscall,
            PID:        ebpfEvent.PID,
            ReturnCode: ebpfEvent.RetCode,
            CPUCore:    ebpfEvent.CPU,
        },
        Semantic: inferSyscallSemantics(ebpfEvent),
    }
}
```

### From Kubernetes Event
```go
func ConvertK8sToUnified(k8sEvent *v1.Event) *UnifiedEvent {
    return &UnifiedEvent{
        ID:        GenerateEventID(),
        Type:      "kubernetes.event",
        Timestamp: k8sEvent.FirstTimestamp.Time,
        Source:    "k8s-collector",
        TraceContext: extractFromAnnotations(k8sEvent), // From pod annotations
        Kubernetes: &KubernetesData{
            EventType: k8sEvent.Type,
            Reason:    k8sEvent.Reason,
            Object:    k8sEvent.InvolvedObject.Name,
            Message:   k8sEvent.Message,
        },
        Impact: assessK8sImpact(k8sEvent),
    }
}
```

### From OTEL Span
```go
func ConvertOTELToUnified(span *trace.Span) *UnifiedEvent {
    return &UnifiedEvent{
        ID:        GenerateEventID(),
        Type:      "otel.span",
        Timestamp: span.StartTime(),
        Source:    "otel-collector",
        TraceContext: &TraceContext{
            TraceID:      span.TraceID().String(),
            SpanID:       span.SpanID().String(),
            ParentSpanID: span.ParentSpanID().String(),
        },
        Application: extractAppData(span.Attributes()),
        Semantic:    extractSemantics(span.Name(), span.Attributes()),
    }
}
```

## Performance Considerations

### Memory Efficiency
- Pointers for optional fields (nil = not present)
- Fixed-size metadata arrays
- String interning for repeated values

### Processing Speed
- 165k+ events/second throughput
- Zero-copy where possible
- Lock-free ring buffers

### Storage
- Compress unused fields
- Time-series optimization for metrics
- Columnar storage for analytics

## Future Extensions

### Planned Fields
- `Security`: Authentication/authorization context
- `Cost`: Cloud resource costs
- `Geography`: Region/zone information
- `Compliance`: Regulatory metadata

### Extensibility
- `CustomData map[string]interface{}` for future needs
- Version field for schema evolution
- Plugin system for custom extractors

## Conclusion

UnifiedEvent is more than a data structure - it's a philosophy:

1. **Every observable signal** can be represented
2. **Trace context** links everything
3. **Semantic understanding** drives value
4. **Business impact** guides priority

This design enables Tapio to provide what no other observability tool can: **true cross-layer correlation with business understanding**.