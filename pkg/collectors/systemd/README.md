# systemd Collector

The systemd collector provides comprehensive monitoring of systemd services and units, tracking state changes, failures, and system events through D-Bus integration.

## Architecture

This module follows the Tapio 5-level dependency hierarchy:

```
pkg/collectors/systemd/
├── go.mod                    # Independent module
├── core/                     # Public interfaces and types
│   ├── interfaces.go         # Collector contracts
│   ├── types.go             # systemd-specific types
│   └── errors.go            # Error definitions
├── internal/                # Internal implementation
│   ├── collector.go         # Main collector logic
│   ├── processor.go         # Event processing
│   ├── platform_linux.go   # Linux platform factory
│   └── platform_other.go   # Non-Linux platform factory
├── linux/                   # Linux-specific D-Bus implementation
│   └── implementation.go    # Real systemd/D-Bus functionality
├── stub/                    # Stub for non-Linux platforms
│   └── implementation.go    # Returns appropriate errors
├── cmd/                     # Standalone executables
│   └── collector/           # Test collector binary
└── collector.go             # Public API exports
```

## Features

- **Service Monitoring**: Track start, stop, restart, and failure events
- **State Tracking**: Monitor active, inactive, failed, and transitional states
- **D-Bus Integration**: Real-time event streaming via systemd D-Bus API
- **Service Discovery**: Automatic discovery and filtering of services
- **Failure Analysis**: Detailed exit codes, signals, and failure reasons
- **Platform Abstraction**: Works on Linux, graceful degradation elsewhere
- **Flexible Filtering**: Watch specific services or all services with exclusions

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/systemd"

// Create collector with default config
config := systemd.DefaultConfig()
collector, err := systemd.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    // Events are domain.Event types with ServiceEventPayload
    fmt.Printf("systemd Event: %+v\n", event)
}

// Check health
health := collector.Health()
fmt.Printf("D-Bus connected: %v\n", health.DBusConnected)
fmt.Printf("systemd version: %s\n", health.SystemdVersion)

// Stop collection
collector.Stop()
```

## Configuration

### Default Configuration
```go
config := systemd.DefaultConfig()
// Monitors services with basic filtering
```

### Critical Services Only
```go
config := systemd.CriticalServicesConfig()
// Monitors only critical system services like sshd, dbus, kubelet, etc.
```

### All Services
```go
config := systemd.AllServicesConfig()
// Monitors all services, sockets, and timers
```

### Custom Configuration
```go
config := systemd.Config{
    Name:            "my-systemd-collector",
    Enabled:         true,
    EventBufferSize: 2000,
    
    // Service selection
    WatchAllServices: false,
    ServiceFilter:    []string{"nginx", "mysql", "redis"},
    ServiceExclude:   []string{"getty@", "user@"},
    UnitTypes:        []string{"service", "socket"},
    
    // Event types
    WatchServiceStates:   true,
    WatchServiceFailures: true,
    WatchServiceReloads:  true,
    WatchJobQueue:        false,
    
    // Performance
    PollInterval:       30 * time.Second,
    EventRateLimit:     1000,
    DBusTimeout:        30 * time.Second,
    MaxConcurrentWatch: 100,
}
```

## Platform Support

- **Linux**: Full systemd/D-Bus functionality
- **Other platforms**: Graceful error with clear messaging

## Building

This module can be built independently:

```bash
cd pkg/collectors/systemd
go build ./...
go test ./...
```

## Running the Standalone Collector

```bash
# Build the collector
cd cmd/collector
go build -o systemd-collector

# Run with default config
./systemd-collector

# Monitor critical services only
./systemd-collector -config=critical

# Monitor all services
./systemd-collector -config=all

# Monitor specific services
./systemd-collector -services="nginx,mysql,redis"

# Monitor with custom unit types
./systemd-collector -unit-types="service,socket,timer"

# Exclude noisy services
./systemd-collector -exclude="getty@,user@,session-"
```

### Command Line Options

- `-config`: Configuration type (default, critical, all)
- `-services`: Comma-separated list of services to monitor
- `-exclude`: Comma-separated list of services to exclude
- `-unit-types`: Comma-separated list of unit types (service, socket, timer, etc.)
- `-poll-interval`: Polling interval for service scanning
- `-buffer-size`: Event buffer size
- `-watch-all`: Watch all services

## Event Types

The collector generates events for:

- **Service Start**: When a service becomes active
- **Service Stop**: When a service becomes inactive
- **Service Restart**: When a service is restarting
- **Service Failure**: When a service enters failed state
- **State Changes**: Other state transitions
- **Property Changes**: Service property updates

## Event Processing

Each event includes:

- Service name and type
- State transition (old → new)
- Exit codes and signals for failures
- Main PID and resource usage
- systemd-specific metadata
- Computed severity based on service criticality

## Dependencies

- **D-Bus**: System D-Bus connection required on Linux
- **systemd**: systemd init system (Linux only)
- **Permissions**: May require elevated permissions for full D-Bus access

## Security Considerations

- Requires read access to system D-Bus
- No sensitive service data exposed in events
- Service filtering helps reduce data volume
- Rate limiting prevents event flooding

## Performance Notes

- Uses systemd's native D-Bus signals for real-time events
- Periodic scanning for state synchronization
- Configurable rate limiting and buffering
- Efficient service filtering at the source

## Troubleshooting

### Common Issues

1. **Permission Denied**: Run with sufficient privileges or configure D-Bus policies
2. **D-Bus Not Available**: Ensure D-Bus system service is running
3. **systemd Not Found**: Only works on systemd-based Linux systems
4. **High Event Volume**: Use service filtering or increase rate limits

### Debug Commands

```bash
# Check D-Bus connectivity
dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
    /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager.GetUnit string:dbus.service

# List systemd units
systemctl list-units --type=service

# Check systemd version
systemctl --version
```