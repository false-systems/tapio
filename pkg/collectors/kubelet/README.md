# Kubelet Collector

High-performance Kubernetes observability collector that monitors node and pod metrics through the Kubelet API, providing comprehensive cluster visibility with zero business logic.

## Architecture

```
Kubelet API → Kubelet Collector → CollectorEvents → Pipeline
     ↓              ↓                     ↓
/stats/summary    Node CPU/Memory      Typed Events  
/pods endpoint    Container Stats      OTEL Metrics
                  Pod Lifecycle        Trace Context
```

## Features

- **Node Metrics**: CPU usage, memory consumption, and capacity monitoring
- **Container Metrics**: Per-container resource usage, throttling, and memory pressure detection
- **Pod Lifecycle**: Container states, crash loops, restart counts, and readiness
- **Storage Events**: Ephemeral storage usage and pressure alerts
- **Real-time Monitoring**: Configurable intervals for stats and lifecycle collection
- **OTEL Integration**: Full OpenTelemetry instrumentation with 8 core metrics
- **Production Ready**: 80%+ test coverage with robust error handling
- **Type Safety**: Uses strongly-typed CollectorEvent instead of map[string]interface{}

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/kubelet"

// Create collector with default config
config := kubelet.DefaultConfig()
config.Address = "https://node-ip:10250"

collector, err := kubelet.NewCollector("kubelet", config)
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process typed events
for event := range collector.Events() {
    switch event.Type {
    case domain.EventTypeKubeletNodeCPU:
        nodeMetrics := event.EventData.Kubelet.NodeMetrics
        fmt.Printf("Node %s CPU: %d nanocores\n", 
            nodeMetrics.NodeName, nodeMetrics.CPUUsageNano)
    
    case domain.EventTypeKubeletMemoryPressure:
        containerMetrics := event.EventData.Kubelet.ContainerMetrics
        fmt.Printf("Container %s/%s memory pressure: %d bytes\n",
            containerMetrics.Namespace, containerMetrics.Container, 
            containerMetrics.MemoryUsage)
    
    case domain.EventTypeKubeletCrashLoop:
        pod := event.EventData.Kubelet.PodLifecycle
        fmt.Printf("Crash loop detected: %s/%s (restarts: %d)\n",
            pod.Namespace, pod.Pod, pod.RestartCount)
    }
}

// Check health
healthy, status := collector.Health()
fmt.Printf("Healthy: %v, Events: %d, Errors: %d\n",
    healthy, status.EventsCollected, status.ErrorsCount)

// Stop collection
collector.Stop()
```

## Configuration

### Default Configuration
```go
config := kubelet.DefaultConfig()
// Address: "localhost:10250"
// MetricsInterval: 30s
// StatsInterval: 10s  
// Insecure: false
// RequestTimeout: 10s
// MaxRetries: 3
```

### Production Configuration
```go
config := kubelet.ProductionConfig()
// Optimized for production with less frequent polling
// MetricsInterval: 60s
// RequestTimeout: 5s
// MaxRetries: 2
```

### Development Configuration  
```go
config := kubelet.DevelopmentConfig()
// More frequent polling and longer timeouts for debugging
// MetricsInterval: 10s
// StatsInterval: 5s
// Insecure: true (for local testing)
// RequestTimeout: 30s
```

### Custom Configuration
```go
config := &kubelet.Config{
    NodeName:        "",                    // Auto-detect or specify
    Address:         "https://node:10250",  // Kubelet address
    Insecure:        false,                 // Use TLS verification
    ClientCert:      "/path/to/cert.pem",   // mTLS authentication
    ClientKey:       "/path/to/key.pem",
    MetricsInterval: 30 * time.Second,      // Pod lifecycle polling
    StatsInterval:   10 * time.Second,      // Resource stats polling
    RequestTimeout:  10 * time.Second,      // API request timeout
    MaxRetries:      3,                     // Request retry limit
    Logger:          logger,                // Custom logger
}
```

## Event Types

The collector generates 9 different event types:

### Node Events
- **EventTypeKubeletNodeCPU**: Node CPU usage and capacity
- **EventTypeKubeletNodeMemory**: Node memory usage, available, and working set

### Container Events  
- **EventTypeKubeletCPUThrottling**: CPU throttling detection
- **EventTypeKubeletMemoryPressure**: Memory pressure and RSS usage
- **EventTypeKubeletEphemeralStorage**: Storage usage > 50%

### Pod Lifecycle Events
- **EventTypeKubeletContainerWaiting**: Containers in waiting state
- **EventTypeKubeletContainerTerminated**: Failed container terminations
- **EventTypeKubeletCrashLoop**: Crash loop detection (restarts > 3)
- **EventTypeKubeletPodNotReady**: Pod readiness failures

## Event Structure

All events use the strongly-typed `CollectorEvent` format:

```json
{
  "event_id": "kubelet-a1b2c3d4e5f67890",
  "timestamp": "2025-08-24T10:30:00Z",
  "source": "kubelet",
  "type": "kubelet.node.cpu",
  "severity": "info",
  "event_data": {
    "kubelet": {
      "event_type": "node_cpu",
      "node_metrics": {
        "node_name": "worker-1",
        "cpu_usage_nano": 1500000000,
        "cpu_usage_milli": 1500,
        "timestamp": "2025-08-24T10:30:00Z"
      }
    }
  },
  "metadata": {
    "trace_id": "a1b2c3d4e5f6789012345678",
    "span_id": "1234567890abcdef",
    "attributes": {
      "collector": "kubelet",
      "api_endpoint": "/stats/summary"
    }
  }
}
```

## OpenTelemetry Integration

The collector implements **mandatory OTEL instrumentation** with 8 core metrics:

### Core Metrics (Required)
- `kubelet_events_processed_total`: Total events processed
- `kubelet_errors_total`: Total errors encountered  
- `kubelet_processing_duration_ms`: Processing time histogram
- `kubelet_dropped_events_total`: Dropped events counter
- `kubelet_buffer_usage`: Current event buffer utilization

### Kubelet-Specific Metrics
- `kubelet_api_latency_ms`: Kubelet API call latency
- `kubelet_active_polls`: Active polling operations gauge
- `kubelet_api_failures_total`: API request failures

### OTEL Usage Pattern
```go
// Direct OpenTelemetry usage (MANDATORY pattern)
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

// Initialize OTEL components
tracer := otel.Tracer(name)
meter := otel.Meter(name)

// Record metrics with attributes
eventsProcessed.Add(ctx, 1, metric.WithAttributes(
    attribute.String("event_type", "kubelet_node_cpu"),
    attribute.String("node_name", nodeName),
))
```

## Authentication

The collector supports multiple authentication methods:

### TLS Client Certificates (Recommended)
```go
config.ClientCert = "/var/lib/kubelet/pki/kubelet-client.crt"
config.ClientKey = "/var/lib/kubelet/pki/kubelet-client.key"
config.Insecure = false
```

### Insecure Mode (Development Only)
```go
config.Insecure = true  // Skips TLS verification
config.Address = "http://localhost:10250"  // HTTP instead of HTTPS
```

### Service Account (In-Cluster)
When running inside Kubernetes, the collector can use the pod's service account for authentication.

## Building

The collector can be built independently:

```bash
cd pkg/collectors/kubelet
go build ./...
go test ./... -race -cover
```

## Integration with Tapio

### Pipeline Integration
```go
// Register with collector orchestrator
orchestrator.RegisterCollector("kubelet", kubeletCollector)

// Events flow through pipeline
CollectorEvent → Pipeline → NATS → Intelligence Layer
```

### Correlation Capabilities
- **Trace Context**: All events include trace/span IDs for correlation
- **Pod Correlation**: Container metrics correlated with K8s API events
- **Node Correlation**: Node metrics correlated with system-level events
- **Temporal Correlation**: Time-based event correlation across collectors

## Performance Considerations

### Resource Usage
- **Memory**: ~50MB baseline + event buffer
- **CPU**: <1% on typical nodes
- **Network**: 2-3 KB/s per node average

### Scaling Guidelines
- **Small clusters** (< 50 nodes): Default intervals
- **Medium clusters** (50-200 nodes): 30-60s intervals  
- **Large clusters** (200+ nodes): 60-120s intervals

### Buffer Sizing
```go
// Event buffer recommendations
config.BufferSize = 10000  // Default for most deployments
config.BufferSize = 25000  // High-volume environments
config.BufferSize = 5000   // Resource-constrained nodes
```

## Security Considerations

- **Kubelet API Access**: Requires read access to kubelet stats
- **Certificate Management**: Store client certificates securely
- **Network Security**: Uses HTTPS by default with certificate verification
- **Least Privilege**: Only requires metrics API access, not full kubelet admin

## Troubleshooting

### Common Issues

1. **Permission Denied (403)**
   - Check client certificate validity
   - Verify RBAC permissions for kubelet API access
   - Ensure service account has required permissions

2. **Connection Refused**
   - Verify kubelet address and port (default: 10250)
   - Check if kubelet API server is enabled
   - Validate network connectivity to kubelet

3. **TLS Certificate Errors**
   - Use `config.Insecure = true` for testing only
   - Verify client certificate matches kubelet CA
   - Check certificate expiration dates

4. **High Memory Usage**
   - Reduce event buffer size
   - Increase collection intervals
   - Monitor for event processing bottlenecks

### Debug Commands
```bash
# Test kubelet connectivity
curl -k https://node-ip:10250/healthz

# Check stats endpoint  
curl -k --cert client.crt --key client.key \
    https://node-ip:10250/stats/summary

# Verify certificate
openssl x509 -in client.crt -text -noout

# Check kubelet logs
journalctl -u kubelet -f
```

### Health Monitoring
```go
healthy, status := collector.Health()
fmt.Printf(`
Collector Health: %v
Events Collected: %d  
Errors Count: %d
Last Event: %v
Kubelet Address: %s
`, healthy, status.EventsCollected, status.ErrorsCount, 
   status.LastEventTime, status.KubeletAddress)
```

## Testing

Comprehensive test suite with 80%+ coverage:

```bash
# Run all tests
go test -v -race -cover ./...

# Run specific test categories
go test -run TestCollectorLifecycle
go test -run TestEventGeneration  
go test -run TestOTELMetrics
go test -run TestErrorHandling

# Integration tests (requires kubelet)
go test -tags=integration ./...
```

## Architecture Compliance

The kubelet collector follows Tapio's architectural standards:

- ✅ **Type Safety**: Uses `CollectorEvent` instead of `map[string]interface{}`
- ✅ **Zero Business Logic**: Raw collection only, intelligence in pipeline
- ✅ **OTEL Integration**: Direct OpenTelemetry usage (no wrappers)
- ✅ **Error Handling**: Contextual errors with proper wrapping
- ✅ **Resource Cleanup**: Proper goroutine and resource management
- ✅ **Test Coverage**: >80% coverage requirement met
- ✅ **Dependency Management**: Minimal dependencies, clear boundaries

## Contributing

When modifying the kubelet collector:

1. **Maintain Type Safety**: Never use `map[string]interface{}`
2. **Follow OTEL Standards**: Use direct OpenTelemetry imports
3. **Preserve Zero Logic**: Keep business logic in intelligence layer
4. **Test Coverage**: Maintain >80% test coverage
5. **Architecture Compliance**: Follow 5-level dependency hierarchy