# REST API Integration with Correlation Adapter

This document describes the REST API implementation that integrates with the Tapio correlation adapter, providing a clean interface for the CLI and external tools to access correlation functionality.

## Architecture Overview

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────────┐
│   Tapio CLI     │────▶│   REST API       │────▶│ Correlation Adapter │
└─────────────────┘     │  (Port 8888)     │     │                     │
                        └──────────────────┘     └─────────────────────┘
                                │                          │
                                ▼                          ▼
                        ┌──────────────────┐     ┌─────────────────────┐
                        │  HTTP Handlers   │     │ Correlation Engine  │
                        │  - Insights      │     │ - Pattern Detection │
                        │  - Predictions   │     │ - Event Processing  │
                        │  - Correlations  │     │ - AI Analysis       │
                        └──────────────────┘     └─────────────────────┘
```

## Implementation Details

### Server with Adapter (`internal/api/server_with_adapter.go`)

The `ServerWithAdapter` struct provides the REST API server that uses the correlation adapter:

```go
type ServerWithAdapter struct {
    router              *gin.Engine
    correlationAdapter  *correlationAdapter.CorrelationAdapter
    logger              *zap.Logger
    config              *Config
}
```

### Key Features

1. **Correlation Adapter Integration**
   - Direct integration with `pkg/server/adapters/correlation`
   - All correlation operations go through the adapter
   - Maintains adapter's abstraction layer

2. **RESTful Endpoints**
   - Resource-based URLs
   - Standard HTTP methods
   - JSON request/response format

3. **Error Handling**
   - Proper HTTP status codes
   - Detailed error messages
   - Graceful degradation when adapter is disabled

## API Endpoints

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/ready` | Readiness check |
| GET | `/api/v1/insights/:namespace/:resource` | Get insights for a resource |
| GET | `/api/v1/predictions/:namespace/:resource` | Get predictions for a resource |
| GET | `/api/v1/fixes/:namespace/:resource` | Get actionable items |
| POST | `/api/v1/events` | Process a single event |
| POST | `/api/v1/correlate` | Correlate multiple events |
| GET | `/api/v1/patterns` | List correlation patterns |
| GET | `/api/v1/patterns/:patternId/matches` | Get pattern matches |
| GET | `/api/v1/stats` | Get correlation statistics |

### Admin Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/status` | Get adapter status |
| POST | `/admin/correlation/enable` | Enable correlation |
| POST | `/admin/correlation/disable` | Disable correlation |

## Integration with Main Server

### Configuration (`cmd/tapio-server/main_enhanced.go`)

```go
func initializeAPIServer(cfg *ServerConfig, engine correlation.CorrelationEngine, insightStore correlation.InsightStore) (*api.ServerWithAdapter, error) {
    // Create correlation adapter
    logger := logging.NewZapLogger(logging.Config{
        Level:  logLevel,
        Format: "json",
    })
    adapter := correlationAdapter.NewCorrelationAdapter(logger)
    
    // Enable the adapter
    adapter.Enable()

    // Create API server with correlation adapter
    server := api.NewServerWithAdapter(adapter, apiConfig)
    
    return server, nil
}
```

### Startup Process

1. Initialize correlation engine
2. Create correlation adapter with logger
3. Enable adapter
4. Create REST API server with adapter
5. Start server on configured port (default: 8888)

## Usage Examples

### Getting Insights

```bash
# Get insights for a specific resource
curl http://localhost:8888/api/v1/insights/production/api-deployment

# Response
{
  "resource": "api-deployment",
  "namespace": "production",
  "insights": [...],
  "count": 5,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

### Processing Events

```bash
# Send an event for processing
curl -X POST http://localhost:8888/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "memory_warning",
    "severity": "warning",
    "source": "kubelet",
    "message": "High memory usage",
    "entity": {
      "type": "pod",
      "name": "api-pod",
      "namespace": "production"
    }
  }'
```

### Correlating Events

```bash
# Correlate multiple events
curl -X POST http://localhost:8888/api/v1/correlate \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"id": "event-1", "type": "memory_warning", ...},
      {"id": "event-2", "type": "pod_restart", ...}
    ]
  }'
```

## Testing

### Unit Tests (`internal/api/server_with_adapter_test.go`)

The implementation includes comprehensive tests:

- Health check endpoints
- Resource insights retrieval
- Event processing
- Pattern management
- Statistics retrieval
- Admin operations
- Middleware functionality

### Running Tests

```bash
cd internal/api
go test -v ./...
```

### Benchmark Tests

Performance benchmarks are included for:
- Event processing throughput
- Insights retrieval latency

```bash
go test -bench=. ./...
```

## CLI Integration

### Example CLI Tool (`examples/cli_correlation_integration.go`)

A sample CLI implementation demonstrates:
- Getting insights for resources
- Retrieving predictions
- Sending events
- Correlating events
- Listing patterns
- Viewing statistics

### Usage

```bash
# Build the CLI
go build -o tapio-cli examples/cli_correlation_integration.go

# Use the CLI
./tapio-cli insights production api-deployment
./tapio-cli predictions production api-deployment
./tapio-cli event memory_warning
./tapio-cli correlate
./tapio-cli patterns
./tapio-cli stats
```

## Benefits of This Architecture

1. **Clean Separation**
   - REST API layer is independent of correlation implementation
   - Adapter provides abstraction from correlation engine details

2. **Extensibility**
   - Easy to add new endpoints
   - Can swap correlation implementations without changing API

3. **Testability**
   - Mock adapter for testing
   - Isolated components for unit tests

4. **Performance**
   - Lightweight HTTP handlers
   - Adapter handles complex correlation logic
   - Asynchronous event processing

5. **Maintainability**
   - Clear interfaces between components
   - Well-documented API endpoints
   - Consistent error handling

## Future Enhancements

1. **WebSocket Support**
   - Real-time insight streaming
   - Live correlation updates

2. **Batch Operations**
   - Bulk event processing
   - Multiple resource queries

3. **Advanced Queries**
   - Time-based filtering
   - Complex correlation queries

4. **Caching Layer**
   - Redis integration for performance
   - Configurable TTL for different data types

5. **Authentication & Authorization**
   - OAuth2/JWT support
   - Role-based access control

## Conclusion

The REST API integration with the correlation adapter provides a clean, extensible interface for the Tapio CLI and external tools. It maintains the separation of concerns while exposing the full power of the correlation engine through simple HTTP endpoints.