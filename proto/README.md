# Tapio Protocol Buffers & gRPC-Gateway

This directory contains the Protocol Buffer definitions for the Tapio observability platform.

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌──────────────┐
│   Web GUI   │  REST   │   gRPC      │  gRPC   │  Collectors  │
│  (Browser)  │ ──────► │  Gateway    │ ──────► │   (eBPF,     │
└─────────────┘         │  (Proxy)    │         │   K8s, etc)  │
                        └─────────────┘         └──────────────┘
                               │
┌─────────────┐                │                 ┌──────────────┐
│   CLI/API   │ ───────────────┘                 │    Tapio     │
│   Clients   │  REST or gRPC                    │    Server    │
└─────────────┘                                  └──────────────┘
```

## Services

### 1. TapioService (tapio.proto)
Main API for observability platform:
- `StreamEvents` - Bidirectional streaming for real-time events
- `GetCorrelations` - Query correlation findings
- `SubscribeToEvents` - Real-time filtered subscriptions
- `GetSemanticGroups` - Semantic correlation queries
- `AnalyzeEvents` - On-demand correlation analysis
- `GetInsights` - AI-generated insights
- `GetMetrics` - System metrics
- `HealthCheck` - Service health monitoring

### 2. CollectorService (collector.proto)
Collector management and data ingestion:
- `StreamEvents` - High-performance event streaming
- `RegisterCollector` - Collector registration
- `Heartbeat` - Health monitoring
- `GetConfig` - Dynamic configuration
- `UpdateConfig` - Configuration updates
- `GetMetrics` - Performance metrics
- `ListCollectors` - Fleet management

### 3. EventService (events.proto)
Event management:
- `StreamEvents` - Bidirectional event streaming
- `Subscribe` - Real-time event subscriptions
- `GetEvents` - Historical event queries
- `GetStatistics` - Event analytics
- `SubmitEventBatch` - Batch submission

### 4. CorrelationService (correlations.proto)
Correlation and intelligence:
- `GetCorrelations` - Query correlations
- `GetSemanticGroups` - Semantic groups
- `AnalyzeEvents` - On-demand analysis
- `SubscribeToCorrelations` - Real-time updates
- `GetRecommendedActions` - Action recommendations

### 5. ObservabilityService (observability.proto)
Metrics, traces, logs, and profiles:
- `GetMetrics` - Query metrics
- `StreamMetrics` - Real-time metrics
- `GetTraces` - Distributed traces
- `GetLogs` - Log aggregation
- `GetProfiles` - Continuous profiling

## REST API

All gRPC services are exposed via REST using gRPC-Gateway:

### Base URL
```
https://api.tapio.io/v1
```

### Authentication
```bash
# API Key
curl -H "X-API-Key: your-api-key" https://api.tapio.io/v1/events

# Bearer Token
curl -H "Authorization: Bearer your-token" https://api.tapio.io/v1/events
```

### Example Endpoints

#### Get Events
```bash
GET /v1/events?limit=100&severity=error
```

#### Stream Events (Server-Sent Events)
```bash
GET /v1/events/subscribe?event_types=error,warning
```

#### Get Correlations
```bash
GET /v1/correlations?start_time=2024-01-01T00:00:00Z&limit=50
```

#### Analyze Events
```bash
POST /v1/analyze
{
  "event_ids": ["evt1", "evt2"],
  "enable_root_cause": true,
  "enable_predictions": true
}
```

#### List Collectors
```bash
GET /v1/collectors?states=running,degraded
```

#### Get Collector Metrics
```bash
GET /v1/collector/{collector_id}/metrics?time_window=5m
```

## OpenAPI/Swagger

OpenAPI documentation is automatically generated:

- Swagger JSON: `https://api.tapio.io/swagger.json`
- Swagger UI: `https://api.tapio.io/swagger/`

## Development

### Generate Proto Code
```bash
make proto
```

This generates:
- Go code for Protocol Buffers
- gRPC service stubs
- gRPC-Gateway reverse proxy
- OpenAPI v2 specification

### Running the Gateway
```bash
# Start gRPC server (port 50051)
go run cmd/tapio-server/main.go

# Start gRPC-Gateway (port 8080)
go run cmd/tapio-gateway/main.go \
  --grpc-server-endpoint=localhost:50051 \
  --http-port=8080 \
  --enable-cors=true \
  --enable-swagger=true
```

### Testing

#### gRPC (using grpcurl)
```bash
# List services
grpcurl -plaintext localhost:50051 list

# Get events
grpcurl -plaintext localhost:50051 tapio.v1.TapioService/GetEvents

# Stream events
grpcurl -plaintext -d '{}' localhost:50051 tapio.v1.TapioService/StreamEvents
```

#### REST
```bash
# Get events
curl http://localhost:8080/v1/events

# Health check
curl http://localhost:8080/health

# Swagger UI
open http://localhost:8080/swagger/
```

## CORS Configuration

CORS is enabled by default for development. For production, configure allowed origins in `gateway.yaml` or via environment variables.

## Rate Limiting

Default limits:
- 10,000 requests/minute per API key
- 165,000 events/second for streaming

Configure in the gateway or use external rate limiters (nginx, envoy).

## Security

1. **Authentication**: API keys or JWT tokens
2. **Authorization**: Role-based access control (RBAC)
3. **TLS**: Always use HTTPS in production
4. **Rate Limiting**: Prevent abuse
5. **Input Validation**: Automatic via protobuf

## Performance

- Binary protocol (protobuf) for efficiency
- HTTP/2 for multiplexing
- Streaming for real-time data
- Connection pooling
- Client-side load balancing

## Monitoring

The gateway exposes metrics at `/metrics` (Prometheus format):
- Request count
- Request duration
- Error rate
- Active connections
- gRPC status codes