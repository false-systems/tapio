# OpenTelemetry (OTEL) Observer

Cross-platform observer for receiving OpenTelemetry traces and metrics via OTLP protocol.

## Features

- **OTLP Protocol Support**: Receives traces and metrics over gRPC (:4317) and HTTP (:4318)
- **Service Dependency Mapping**: Extracts service relationships from trace parent-child spans
- **Intelligent Sampling**: Probabilistic sampling with always-sample-errors option
- **Cross-Platform**: Works on all platforms (no eBPF or Linux-specific requirements)
- **Production Ready**: 80%+ test coverage with comprehensive error handling

## Configuration

```go
config := &otel.Config{
    Name:               "otel-observer",
    GRPCEndpoint:       ":4317",  // Standard OTLP gRPC port
    HTTPEndpoint:       ":4318",  // Standard OTLP HTTP port
    BufferSize:         10000,    // Event buffer size
    SamplingRate:       1.0,      // 0.0-1.0 (1.0 = sample all)
    AlwaysSampleErrors: true,     // Always sample error spans
    EnableDependencies: true,     // Extract service dependencies
    ServiceMapInterval: 30 * time.Second,
    MaxTracesPerSecond: 1000,
}
```

## Usage

```go
observer, err := otel.NewObserver("otel-observer", config)
if err != nil {
    return fmt.Errorf("failed to create OTEL observer: %w", err)
}

// Start collecting
ctx := context.Background()
if err := observer.Start(ctx); err != nil {
    return fmt.Errorf("failed to start observer: %w", err)
}

// Process events
for event := range observer.Events() {
    // Handle OTEL span/metric events
    switch event.Type {
    case domain.EventTypeOTELSpan:
        span := event.EventData.OTELSpan
        fmt.Printf("Received span: %s from service %s\n", span.Name, span.ServiceName)
    case domain.EventTypeOTELMetric:
        // Handle metrics
    }
}
```

## Event Types

### OTEL Span Events (`EventTypeOTELSpan`)
Contains distributed tracing data with:
- Trace ID and Span ID for correlation
- Service name and operation name
- HTTP method, URL, status codes
- Kubernetes pod/namespace metadata
- Parent-child relationships for service dependencies

### OTEL Metric Events (`EventTypeOTELMetric`)
Contains application metrics and service dependency data

## Service Dependencies

The collector automatically extracts service dependencies from trace spans:
- Parent spans calling child spans create service relationships
- Aggregated over configurable time windows
- Emitted as service dependency events for correlation

## Integration with Tapio

- Events contain trace IDs for correlation with other collectors
- Kubernetes metadata extracted for pod/namespace correlation
- Integrates with eBPF collectors for application ↔ kernel correlation
- Service dependencies feed into Tapio's correlation engine

## Testing

Run tests with:
```bash
cd pkg/collectors/otel
go test -v -race -cover
```

Achieves 80.2% test coverage including:
- Collector lifecycle
- Service dependency extraction
- Sampling logic
- Event emission
- Configuration validation

## Production Considerations

- **Buffer Sizing**: Set `BufferSize` based on trace volume
- **Sampling**: Use probabilistic sampling for high-volume services
- **Dependencies**: Enable service mapping for observability
- **Endpoints**: Standard OTLP ports 4317 (gRPC) and 4318 (HTTP)

## Correlation Capabilities

The OTEL collector enables powerful correlations:
- **App → Kernel**: Trace spans correlated with eBPF kernel events
- **Service → Infrastructure**: Service calls correlated with pod/node metrics  
- **Distributed Traces**: End-to-end request flow across microservices
- **Error Analysis**: Application errors correlated with system-level issues