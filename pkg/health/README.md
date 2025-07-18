# Health Check Package

This package provides standardized health check functionality for all Tapio services.

## Features

- Standardized health check response format
- Support for liveness and readiness probes
- Concurrent health check execution
- Configurable timeout for checks
- Built-in checkers for common scenarios
- Easy integration with HTTP servers

## Usage

### Basic Setup

```go
import "github.com/yairfalse/tapio/pkg/health"

// Create health handler
healthHandler := health.NewHandler("my-service", "v1.0.0")

// Add health checkers
healthHandler.AddChecker(health.NewDatabaseChecker("postgres", db))
healthHandler.AddChecker(health.NewHTTPChecker("upstream-api", "http://api:8080/health"))

// Set up HTTP routes
mux := http.NewServeMux()
mux.Handle("/health", healthHandler)
mux.HandleFunc("/health/live", healthHandler.LivenessHandler())
mux.HandleFunc("/health/ready", healthHandler.ReadinessHandler())
```

### Custom Health Checkers

```go
healthHandler.AddChecker(health.NewCustomChecker("cache", func(ctx context.Context) health.Check {
    if err := cache.Ping(ctx); err != nil {
        return health.Check{
            Status:  health.StatusUnhealthy,
            Message: fmt.Sprintf("Cache error: %v", err),
        }
    }
    
    return health.Check{
        Status:  health.StatusHealthy,
        Message: "Cache is accessible",
        Metadata: map[string]interface{}{
            "entries": cache.Count(),
            "size_mb": cache.SizeMB(),
        },
    }
}))
```

## Response Format

### Health Check Response

```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "service": "tapio-api",
  "version": "v1.0.0",
  "uptime": "2h30m15s",
  "checks": {
    "database": {
      "status": "healthy",
      "message": "Database is accessible",
      "latency": "5ms",
      "metadata": {
        "open_connections": 10,
        "in_use": 2,
        "idle": 8
      }
    },
    "cache": {
      "status": "healthy",
      "message": "Cache is accessible",
      "latency": "1ms"
    }
  }
}
```

### Status Codes

- `200 OK` - Service is healthy or degraded
- `503 Service Unavailable` - Service is unhealthy

### Health Status Values

- `healthy` - All checks passed
- `degraded` - Some checks failed but service is operational
- `unhealthy` - Critical checks failed, service is not operational

## Kubernetes Integration

### Liveness Probe

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 8081
  initialDelaySeconds: 10
  periodSeconds: 10
```

### Readiness Probe

```yaml
readinessProbe:
  httpGet:
    path: /health/ready
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Best Practices

1. **Separate Port**: Run health checks on a different port (e.g., 8081) from your main API
2. **Timeout**: All checks have a 5-second timeout by default
3. **Concurrency**: Checks run concurrently to minimize response time
4. **Metadata**: Include relevant debugging information in metadata
5. **Graceful Degradation**: Use "degraded" status for non-critical failures

## Migration Guide

To migrate from existing health endpoints:

1. Replace existing health handlers with the standardized handler
2. Convert existing checks to implement the `Checker` interface
3. Update Kubernetes manifests to use new endpoints
4. Update monitoring/alerting to parse new response format

## Example Services

See the following services for implementation examples:
- `cmd/tapio-server` - Main API server
- `cmd/tapio-engine` - Processing engine
- `pkg/telemetry/enterprise.go` - Enterprise telemetry server