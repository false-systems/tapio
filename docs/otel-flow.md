# OTEL Trace Flow in Tapio

## 1. Collection Phase
```
kubeapi collector → RawEvent {
    Type: "kubeapi",
    Data: {...},
    Metadata: {
        "trace_id": "abc-123-def",
        "span_id": "span-456",
        "parent_span_id": "span-parent",
        "namespace": "production",
        "cluster": "prod-west"
    }
}
```

## 2. NATS Publishing
The event is published to multiple subjects:
- `events.raw.kubeapi.production` (primary)
- `traces.abc-123-def` (trace routing)

## 3. Event Transformation
```go
RawEvent → UnifiedEvent {
    TraceContext: {
        TraceID: "abc-123-def",
        SpanID: "span-456",
        ParentSpanID: "span-parent",
    },
    // Full event data...
}
```

## 4. Correlation Magic 🎯
The correlation engine subscribes to `traces.>` and automatically:
- Groups all events with same trace_id
- Builds the request flow graph
- Identifies bottlenecks and failures

## Example Trace Flow
```
User Request (trace: abc-123-def)
    ↓
API Gateway (span: gw-1)
    ↓
Auth Service (span: auth-2, parent: gw-1)
    ↓
API Server (span: api-3, parent: gw-1)
    ↓
etcd Write (span: etcd-4, parent: api-3)
    ↓
Webhook (span: webhook-5, parent: api-3)
```

When ANY component fails, we can:
1. Find all related events via trace_id
2. Build the causality chain
3. Identify root cause
4. Show full impact analysis