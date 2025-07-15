# Tapio OTEL Connection Test

This test verifies that Tapio's native OTEL output actually connects and sends traces to collectors.

## Quick Start

### 1. Start Jaeger (simplest option)

```bash
docker-compose up -d
```

Wait a few seconds for Jaeger to start, then open http://localhost:16686

### 2. Run the connection test

```bash
# From the tapio root directory
cd examples/otel-connection-test
go run test_otel_connection.go
```

### 3. Verify traces in Jaeger UI

1. Open http://localhost:16686
2. Select service: `tapio-otel-test`
3. Click "Find Traces"
4. You should see traces with:
   - Root span: `tapio.check`
   - Child spans for issues
   - Rich attributes including human explanations
   - Business impact metrics
   - Prediction spans

## What This Tests

- ✅ OTEL exporter creates successfully
- ✅ Connection to OTLP endpoint works
- ✅ Traces are properly formatted
- ✅ ForceFlush ensures traces are sent
- ✅ Rich Tapio metadata is included
- ✅ Span hierarchy is correct

## Testing with Different Collectors

### Grafana Tempo
```bash
OTEL_ENDPOINT=tempo:4317 go run test_otel_connection.go
```

### Remote Collector
```bash
OTEL_ENDPOINT=my-collector.example.com:4317 go run test_otel_connection.go
```

### OTEL Collector (full setup)
```bash
docker-compose --profile full up -d
```

## Troubleshooting

### No traces appearing?

1. Check Jaeger is running: `docker ps`
2. Check Jaeger logs: `docker logs tapio-test-jaeger`
3. Try the HTTP endpoint: Change to `otlptracehttp` in the code
4. Check firewall/network settings

### Connection refused?

Make sure Jaeger has OTLP enabled:
```bash
docker logs tapio-test-jaeger | grep OTLP
# Should see: "OTLP receiver enabled"
```

## Example Trace Structure

```
tapio.check (root span)
├── issue.OOMKiller
│   ├── human.what_happened: "Your API service is running out of memory..."
│   ├── business.impact_score: 0.9
│   ├── correlation.group_id: "corr-memory-pressure"
│   └── evidence events (metric, log)
├── issue.HighCPU
│   └── human.what_happened: "Worker service is being CPU throttled..."
├── tapio.predictions
│   └── prediction.ServiceOutage
│       ├── prediction.probability: 0.78
│       └── prevention_action events
└── tapio.recommendations
    └── recommendation events
```

## Integration with Tapio CLI

This test uses the same code as the real Tapio CLI:

```bash
# Real usage (sends to OTEL by default)
tapio check my-deployment

# With custom endpoint
tapio check --otel-endpoint=collector:4317
```