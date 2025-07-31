# systemd Collector

Minimal systemd collector using eBPF to monitor systemd-managed processes with zero business logic.

## Architecture

```
pkg/collectors/systemd/
├── collector.go              # Minimal collector implementation
├── collector_test.go         # Unit tests
├── generate.go              # bpf2go generation
├── register.go              # Registry integration
├── bpf/                     # eBPF programs
│   ├── systemd_monitor.c    # Main eBPF program
│   ├── k8s_service_syscalls.c  # K8s service syscall monitoring
│   └── common.h             # Shared headers
└── systemdmonitor_*.go/o    # Generated eBPF objects
```

## Features

- **Minimal Design**: Zero business logic, just raw event collection
- **eBPF-Based**: Uses tracepoints for execve/exit events
- **K8s Focus**: Specifically monitors systemd units relevant to K8s
- **Low Overhead**: Efficient kernel-level monitoring
- **Container-Aware**: Detects container processes via cgroup analysis

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/systemd"

// Create minimal collector
collector, err := systemd.NewCollector("systemd")
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