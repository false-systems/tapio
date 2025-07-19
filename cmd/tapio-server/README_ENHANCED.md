# Tapio Server - Enhanced with OTEL Semantic Correlation

The Tapio server now includes enhanced capabilities for receiving and processing events enriched with OTEL semantic correlation data.

## New Features

### 1. gRPC Streaming with OTEL Context

The server now supports bidirectional gRPC streaming that preserves OTEL trace context:

```proto
// Bidirectional streaming for events
rpc StreamEvents(stream Event) returns (stream EventAck);
```

Events received include:
- OTEL trace and span IDs
- Semantic correlation metadata
- Impact assessments
- Predictive analytics

### 2. Real-time Event Subscriptions

Clients can subscribe to filtered event streams:

```proto
rpc SubscribeToEvents(SubscribeRequest) returns (stream Event);
```

Filter options:
- Event types
- Severity levels
- Namespaces
- Services

### 3. Semantic Correlation API

Query semantic correlation findings:

```proto
rpc GetCorrelations(GetCorrelationsRequest) returns (GetCorrelationsResponse);
rpc GetSemanticGroups(GetSemanticGroupsRequest) returns (GetSemanticGroupsResponse);
```

### 4. Built-in Correlation Processing

The server includes an embedded correlation manager that:
- Processes incoming events in real-time
- Generates insights and correlations
- Maintains semantic groups
- Provides root cause analysis

## Architecture

```
┌─────────────────┐
│ Tapio Collector │
│  (with OTEL)    │
└────────┬────────┘
         │ gRPC Stream
         │ (with trace context)
┌────────▼────────┐
│  Enhanced       │
│  Tapio Server   │
├─────────────────┤
│ • gRPC Handler  │
│ • OTEL Context  │
│ • Correlation   │
│   Manager       │
│ • Event Buffer  │
│ • Subscribers   │
└─────────────────┘
```

## Usage

### Starting the Enhanced Server

```bash
# Default configuration
tapio-server

# Custom configuration
tapio-server \
  --grpc-port 9090 \
  --rest-port 8080 \
  --grpc-enabled \
  --log-level debug
```

### Client Examples

#### Python Client

```python
import grpc
from tapio.v1 import tapio_pb2, tapio_pb2_grpc

# Connect to server
channel = grpc.insecure_channel('localhost:9090')
stub = tapio_pb2_grpc.TapioServiceStub(channel)

# Subscribe to events
request = tapio_pb2.SubscribeRequest(
    event_types=['system', 'kubernetes'],
    severity_filter='high',
    include_correlations=True
)

for event in stub.SubscribeToEvents(request):
    print(f"Event: {event.id} - {event.message}")
    if event.semantic_data:
        print(f"  Correlation: {event.semantic_data.correlation_id}")
        print(f"  Intent: {event.semantic_data.intent}")
```

#### Go Client

```go
conn, err := grpc.Dial("localhost:9090", grpc.WithInsecure())
client := pb.NewTapioServiceClient(conn)

// Stream events
stream, err := client.StreamEvents(ctx)

// Send event
event := &pb.Event{
    Id:       "evt-123",
    Type:     "system",
    Source:   "ebpf",
    TraceId:  span.SpanContext().TraceID().String(),
    SpanId:   span.SpanContext().SpanID().String(),
}
stream.Send(event)

// Receive acknowledgment
ack, err := stream.Recv()
```

## API Endpoints

### StreamEvents

Bidirectional streaming for event processing:
- Send: Events with OTEL context
- Receive: Acknowledgments with correlation insights

### GetCorrelations

Query correlation findings:
- Filter by time range
- Filter by correlation type
- Pagination support

### SubscribeToEvents

Real-time event subscription:
- Multiple filter options
- Automatic correlation enrichment
- Low-latency streaming

### GetSemanticGroups

Query semantic correlation groups:
- Filter by confidence score
- Filter by group type
- Include full causal chains

## Performance

- **Throughput**: 165k+ events/sec
- **Latency**: <1ms for event acknowledgment
- **Correlation**: Real-time semantic analysis
- **Memory**: Configurable event buffer (default 10k events)

## Monitoring

The server logs key metrics every 10 seconds:

```
📊 Stats: Events=15234, Correlations=127, ActiveStreams=3
```

## Integration with OTEL

The server preserves OTEL trace context throughout:

1. Extracts trace context from gRPC metadata
2. Links server spans to collector traces
3. Propagates context to correlation processing
4. Returns trace IDs in correlation findings

This enables end-to-end tracing from:
- Original system event (eBPF, K8s, etc.)
- Through collector enrichment
- To server correlation processing
- To client consumption

## Future Enhancements

1. **REST API**: HTTP/JSON endpoints for web clients
2. **WebSocket**: Real-time browser subscriptions
3. **Metrics Export**: Prometheus/OTEL metrics
4. **Storage Backend**: Persistent event and correlation storage
5. **ML Integration**: Advanced pattern recognition