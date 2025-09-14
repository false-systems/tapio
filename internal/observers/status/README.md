# Status Observer

The Status Observer monitors L7 protocol status codes and failures to detect application-level issues before they cascade into system-wide problems.

## Overview

Status acts as an early warning system by tracking:
- HTTP status codes (4xx, 5xx errors)
- gRPC status codes
- Connection timeouts and resets
- Protocol-level failures
- Request latency degradation

## Architecture

```
┌─────────────────────────────────────────┐
│           Status Observer                │
├─────────────────────────────────────────┤
│  ┌─────────────┐    ┌────────────────┐  │
│  │  eBPF Hooks │───▶│  Event Parser  │  │
│  └─────────────┘    └────────────────┘  │
│         │                    │           │
│         ▼                    ▼           │
│  ┌─────────────┐    ┌────────────────┐  │
│  │ Conn Track  │    │  Aggregator    │  │
│  └─────────────┘    └────────────────┘  │
│                             │            │
│                             ▼            │
│                    ┌────────────────┐   │
│                    │ Pattern Detect │   │
│                    └────────────────┘   │
└─────────────────────────────────────────┘
```

## Features

### Protocol Support
- HTTP/1.x and HTTP/2
- gRPC
- MySQL
- Redis
- PostgreSQL (planned)

### Failure Detection
- **Hard Failures**: Connection refused, 500 errors
- **Partial Failures**: Some endpoints failing
- **Degraded Performance**: Slow but functioning
- **Byzantine Failures**: Inconsistent responses

### Pattern Recognition
- Cascading timeouts
- Retry storms
- Service down detection
- Canary deployment failures

## Configuration

```yaml
observers:
  status:
    enabled: true
    sample_rate: 0.01         # Sample 1% of requests
    max_events_per_sec: 1000  # Rate limiting
    max_memory_mb: 100        # Memory cap
    flush_interval: 10s       # Aggregation interval
    redact_headers:           # Privacy
      - Authorization
      - Cookie
      - X-API-Key
```

## Metrics

The observer exports OpenTelemetry metrics:

- `status.http.errors` - HTTP errors by status code
- `status.grpc.errors` - gRPC errors by status
- `status.timeouts` - Connection timeout count
- `status.latency` - L7 request latency histogram
- `status.error_rate` - Error rate by service

## Events

Status events include:

```json
{
  "type": "status",
  "timestamp": "2024-01-15T10:30:00Z",
  "service": "api-gateway",
  "data": {
    "error_count": 42,
    "total_count": 1000,
    "error_rate": 0.042,
    "avg_latency": 250.5,
    "error_types": {
      "5xx": 30,
      "timeout": 10,
      "reset": 2
    }
  }
}
```

## Integration

Status Observer integrates with:
- **Correlation Engine**: Links L7 failures to root causes
- **Resource Observers**: Correlates with CPU/memory pressure
- **Service Map**: Tracks failure propagation paths

## Performance

- <1% CPU overhead with sampling
- ~50MB memory footprint
- Zero-copy eBPF parsing
- Per-CPU aggregation for scalability

## Development

### Building

```bash
# Generate eBPF code
go generate ./pkg/observers/status/...

# Run tests
go test ./pkg/observers/status/...
```

### Testing

Integration tests simulate various failure scenarios:
- HTTP server returning errors
- Connection timeouts
- Retry storms
- Cascading failures

## Future Enhancements

- [ ] TLS/SSL failure tracking
- [ ] WebSocket status monitoring
- [ ] GraphQL error tracking
- [ ] Machine learning for anomaly detection
- [ ] Predictive failure alerts