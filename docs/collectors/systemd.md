# Systemd Collector

The Systemd Collector provides real-time monitoring of systemd services on Linux systems, capturing service state changes, failures, and performance metrics through D-Bus integration.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Installation](#installation)
- [Configuration](#configuration)
- [Event Types](#event-types)
- [Service Filtering](#service-filtering)
- [Health Monitoring](#health-monitoring)
- [Performance](#performance)
- [Troubleshooting](#troubleshooting)
- [Examples](#examples)

## Overview

### Features

- **Real-time Service Monitoring**: Captures systemd service events as they occur
- **D-Bus Integration**: Uses native systemd D-Bus interface for efficient monitoring
- **Service Filtering**: Flexible filtering by service names, patterns, and states
- **Critical Service Detection**: Automatic severity escalation for critical services
- **State Change Tracking**: Monitors all service state transitions
- **Performance Metrics**: Captures CPU, memory, and process information
- **Automatic Reconnection**: Handles D-Bus connection failures gracefully

### Supported Platforms

- Linux with systemd (recommended: systemd 240+)
- Stub implementation for non-Linux platforms (for development)

## Architecture

### Component Structure

```
pkg/collectors/systemd/
├── core/           # Public interfaces and types
├── internal/       # Core collector implementation
├── linux/          # Linux-specific D-Bus implementation
├── stub/           # Stub implementation for non-Linux
└── cmd/collector/  # Standalone collector executable
```

### Key Components

1. **Collector**: Main orchestrator managing service watchers and event processing
2. **ServiceWatcher**: Monitors specific systemd services via D-Bus
3. **EventProcessor**: Transforms raw systemd events into domain events
4. **DBusConnection**: Abstracted D-Bus interface for systemd communication

### Event Flow

```
systemd D-Bus → ServiceWatcher → RawEvent → EventProcessor → domain.Event → Output
```

## Installation

### As Library

```go
import "github.com/yairfalse/tapio/pkg/collectors/systemd"
```

### Standalone Collector

```bash
# Build the collector
cd pkg/collectors/systemd/cmd/collector
go build -o systemd-collector

# Run with default configuration
./systemd-collector

# Run with custom config
./systemd-collector -config /path/to/config.json
```

### Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN cd pkg/collectors/systemd/cmd/collector && go build -o systemd-collector

FROM alpine:latest
RUN apk --no-cache add ca-certificates dbus
WORKDIR /root/
COPY --from=builder /app/pkg/collectors/systemd/cmd/collector/systemd-collector .
CMD ["./systemd-collector"]
```

## Configuration

### Quick Start Configurations

```go
import "github.com/yairfalse/tapio/pkg/collectors/systemd"

// Default configuration - monitors common services
config := systemd.DefaultConfig()

// Critical services only
config := systemd.CriticalConfig()

// Monitor all services
config := systemd.AllConfig()
```

### Configuration Options

```go
type Config struct {
    // Basic settings
    Name            string `json:"name"`
    Enabled         bool   `json:"enabled"`
    EventBufferSize int    `json:"event_buffer_size"`
    
    // D-Bus connection
    DBusType       string        `json:"dbus_type"`        // "system" or "session"
    ReconnectDelay time.Duration `json:"reconnect_delay"`
    
    // Service filtering
    WatchedServices        []string `json:"watched_services"`
    ServiceIncludePatterns []string `json:"service_include_patterns"`
    ServiceExcludePatterns []string `json:"service_exclude_patterns"`
    
    // State filtering
    IncludeOnlyFailed bool     `json:"include_only_failed"`
    IncludeStates     []string `json:"include_states"`
    ExcludeStates     []string `json:"exclude_states"`
    
    // Event type filtering
    IncludeEventTypes []string `json:"include_event_types"`
    ExcludeEventTypes []string `json:"exclude_event_types"`
    
    // Performance tuning
    EnableDBusCache     bool          `json:"enable_dbus_cache"`
    PropertySyncPeriod  time.Duration `json:"property_sync_period"`
    EventRateLimit      int           `json:"event_rate_limit"`
    
    // gRPC client settings
    GRPCEndpoint    string        `json:"grpc_endpoint"`
    GRPCTimeout     time.Duration `json:"grpc_timeout"`
    GRPCRetryDelay  time.Duration `json:"grpc_retry_delay"`
    GRPCMaxRetries  int           `json:"grpc_max_retries"`
}
```

### Example Configuration

```json
{
  "name": "systemd-collector",
  "enabled": true,
  "event_buffer_size": 5000,
  "dbus_type": "system",
  "reconnect_delay": "5s",
  "watched_services": [
    "sshd.service",
    "docker.service",
    "kubelet.service",
    "nginx.service"
  ],
  "service_include_patterns": ["web-*", "api-*"],
  "service_exclude_patterns": ["*-test", "*-debug"],
  "include_only_failed": false,
  "include_states": ["active", "failed", "activating"],
  "exclude_states": ["inactive"],
  "include_event_types": ["start", "stop", "failure", "restart"],
  "enable_dbus_cache": true,
  "property_sync_period": "30s",
  "event_rate_limit": 1000,
  "grpc_endpoint": "localhost:50051",
  "grpc_timeout": "30s",
  "grpc_retry_delay": "2s",
  "grpc_max_retries": 3
}
```

## Event Types

### Core Event Types

| Type | Description | Typical Triggers |
|------|-------------|------------------|
| `start` | Service started | `systemctl start service` |
| `stop` | Service stopped | `systemctl stop service` |
| `restart` | Service restarted | `systemctl restart service` |
| `reload` | Service reloaded | `systemctl reload service` |
| `failure` | Service failed | Process crash, exit code != 0 |
| `state_change` | State transition | Any state change |

### Event Severity Mapping

| Condition | Severity | Examples |
|-----------|----------|----------|
| Critical service failure | `critical` | sshd, kubelet, docker failure |
| Service failure | `high` | Application service failure |
| Service restart | `warning` | Unexpected restart |
| State change to inactive | `warning` | Active → Inactive transition |
| Non-zero exit code | `warning` | Process exit with error |
| Normal operations | `low` | Start, stop, reload |

### Critical Services

Default critical services (failures escalated to `critical` severity):

- `sshd.service` - SSH daemon
- `systemd-networkd.service` - Network management
- `systemd-resolved.service` - DNS resolution
- `dbus.service` - D-Bus system message bus
- `systemd-journald.service` - Journal service
- `kubelet.service` - Kubernetes node agent
- `docker.service` - Docker daemon
- `containerd.service` - Container runtime

## Service Filtering

### Basic Service Lists

```go
config := core.Config{
    // Monitor specific services
    WatchedServices: []string{
        "nginx.service",
        "mysql.service",
        "redis.service",
    },
}
```

### Pattern-Based Filtering

```go
config := core.Config{
    // Include patterns (glob-style)
    ServiceIncludePatterns: []string{
        "web-*",      // All services starting with "web-"
        "api-*",      // All services starting with "api-"
        "*-prod",     // All services ending with "-prod"
    },
    
    // Exclude patterns
    ServiceExcludePatterns: []string{
        "*-test",     // Exclude test services
        "*-debug",    // Exclude debug services
        "temp-*",     // Exclude temporary services
    },
}
```

### State-Based Filtering

```go
config := core.Config{
    // Only monitor failed services
    IncludeOnlyFailed: true,
    
    // Or specify states explicitly
    IncludeStates: []string{"active", "failed", "activating"},
    ExcludeStates: []string{"inactive", "dead"},
}
```

### Event Type Filtering

```go
config := core.Config{
    // Only monitor specific event types
    IncludeEventTypes: []string{"failure", "start", "stop"},
    
    // Exclude noisy event types
    ExcludeEventTypes: []string{"reload", "state_change"},
}
```

## Health Monitoring

### Health Status

The collector provides detailed health information:

```go
health := collector.Health()

type Health struct {
    Status             HealthStatus       `json:"status"`
    Message            string             `json:"message"`
    Connected          bool               `json:"connected"`
    LastEventTime      time.Time          `json:"last_event_time"`
    EventsProcessed    uint64             `json:"events_processed"`
    EventsDropped      uint64             `json:"events_dropped"`
    ErrorCount         uint64             `json:"error_count"`
    ServicesWatched    int                `json:"services_watched"`
    ReconnectCount     uint64             `json:"reconnect_count"`
    DBusMethodCalls    uint64             `json:"dbus_method_calls"`
    ProcessingErrors   uint64             `json:"processing_errors"`
    Metrics            map[string]float64 `json:"metrics"`
}
```

### Health Status Levels

- **`healthy`**: All systems operational, D-Bus connected, processing events
- **`degraded`**: Minor issues, some errors but still functional
- **`unhealthy`**: Major issues, D-Bus disconnected or high error rate
- **`unknown`**: Status cannot be determined

### Monitoring Best Practices

1. **Check D-Bus Connection**: Monitor `Connected` field
2. **Watch Error Rates**: Alert on high `ErrorCount` or `ProcessingErrors`
3. **Monitor Event Flow**: Ensure `LastEventTime` is recent
4. **Track Reconnections**: High `ReconnectCount` may indicate D-Bus issues

## Performance

### Memory Usage

- **Base Memory**: ~10-20 MB for basic operation
- **Per Service**: ~100-500 KB additional memory per watched service
- **Event Buffer**: Configurable, default 1000 events (~1-5 MB)

### CPU Usage

- **Idle**: <1% CPU when no events
- **Active**: 1-5% CPU during normal event processing
- **High Load**: Up to 10-15% during service restart storms

### Network Usage

- **D-Bus**: Minimal, only system bus communication
- **gRPC**: Depends on event volume, typically <1 MB/hour

### Tuning Parameters

```go
config := core.Config{
    // Increase buffer for high-volume environments
    EventBufferSize: 10000,
    
    // Rate limiting to prevent overload
    EventRateLimit: 1000,
    
    // Reduce property sync frequency
    PropertySyncPeriod: time.Minute,
    
    // Enable D-Bus caching
    EnableDBusCache: true,
}
```

## Troubleshooting

### Common Issues

#### D-Bus Connection Failed

**Symptoms**: Collector shows "unhealthy" status, `Connected: false`

**Solutions**:
1. Check D-Bus service is running: `systemctl status dbus`
2. Verify permissions: User must be in `systemd-journal` group
3. Check D-Bus policy restrictions
4. Restart D-Bus service: `sudo systemctl restart dbus`

#### High Memory Usage

**Symptoms**: Collector consuming excessive memory

**Solutions**:
1. Reduce `EventBufferSize` in configuration
2. Enable more aggressive service filtering
3. Reduce `PropertySyncPeriod`
4. Monitor for memory leaks in logs

#### Missing Events

**Symptoms**: Expected service events not appearing

**Solutions**:
1. Verify service is in `WatchedServices` or matches include patterns
2. Check state and event type filters
3. Verify service actually generates systemd events
4. Check collector logs for processing errors

#### High CPU Usage

**Symptoms**: Collector consuming excessive CPU

**Solutions**:
1. Enable `EventRateLimit` to throttle processing
2. Reduce number of watched services
3. Increase `ReconnectDelay` to reduce connection attempts
4. Check for D-Bus connection issues causing retry loops

### Debug Configuration

```json
{
  "name": "debug-systemd-collector",
  "enabled": true,
  "event_buffer_size": 100,
  "watched_services": ["test.service"],
  "include_event_types": ["start", "stop", "failure"],
  "enable_dbus_cache": false,
  "property_sync_period": "5s",
  "grpc_timeout": "5s"
}
```

### Logging

Enable debug logging to troubleshoot issues:

```bash
export LOG_LEVEL=debug
./systemd-collector
```

Key log patterns to watch:
- `D-Bus connection established` - Successful connection
- `Service watcher started` - Service monitoring active
- `Event processed` - Successful event processing
- `Reconnecting to D-Bus` - Connection recovery

## Examples

### Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors/systemd"
)

func main() {
    // Create collector with default config
    collector, err := systemd.NewCollector(systemd.DefaultConfig())
    if err != nil {
        log.Fatal(err)
    }
    
    // Start collecting
    ctx := context.Background()
    if err := collector.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer collector.Stop()
    
    // Process events
    for event := range collector.Events() {
        log.Printf("Service event: %s - %s", 
            event.Context.Component, event.Data["event_type"])
    }
}
```

### Custom Service Monitoring

```go
config := systemd.DefaultConfig()
config.WatchedServices = []string{
    "nginx.service",
    "mysql.service",
    "redis.service",
}
config.IncludeOnlyFailed = true

collector, err := systemd.NewCollector(config)
if err != nil {
    log.Fatal(err)
}
```

### Integration with Tapio Server

```go
config := systemd.DefaultConfig()
config.GRPCEndpoint = "tapio-server:50051"
config.GRPCTimeout = 30 * time.Second

collector, err := systemd.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

// Collector will automatically stream events to Tapio server
```

### Container Deployment

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: systemd-collector
spec:
  selector:
    matchLabels:
      app: systemd-collector
  template:
    metadata:
      labels:
        app: systemd-collector
    spec:
      hostPID: true
      hostNetwork: true
      containers:
      - name: systemd-collector
        image: tapio/systemd-collector:latest
        securityContext:
          privileged: true
        volumeMounts:
        - name: dbus
          mountPath: /var/run/dbus
          readOnly: true
        - name: systemd
          mountPath: /run/systemd
          readOnly: true
        env:
        - name: GRPC_ENDPOINT
          value: "tapio-server:50051"
      volumes:
      - name: dbus
        hostPath:
          path: /var/run/dbus
      - name: systemd
        hostPath:
          path: /run/systemd
```

## API Reference

### Constructor Functions

```go
// Create collector with custom config
func NewCollector(config core.Config) (core.Collector, error)

// Preset configurations
func DefaultConfig() core.Config
func CriticalConfig() core.Config  
func AllConfig() core.Config
```

### Collector Interface

```go
type Collector interface {
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan domain.Event
    Health() Health
    Statistics() Statistics
    Configure(config Config) error
}
```

### Event Structure

```go
type Event struct {
    ID         EventID              `json:"id"`
    Type       EventType            `json:"type"`
    Source     SourceType           `json:"source"`
    Timestamp  time.Time            `json:"timestamp"`
    Data       map[string]interface{} `json:"data"`
    Context    EventContext         `json:"context"`
    Severity   EventSeverity        `json:"severity"`
    Confidence float64              `json:"confidence"`
    Attributes map[string]interface{} `json:"attributes"`
}
```

For complete API documentation, see the [Go package documentation](https://pkg.go.dev/github.com/yairfalse/tapio/pkg/collectors/systemd).