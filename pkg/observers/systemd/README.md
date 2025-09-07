# Systemd Observer

The Systemd Observer monitors systemd service state changes, failures, and lifecycle events to provide visibility into system service health.

## What It Monitors

The observer tracks:
- **Service Start Events** - When services are started
- **Service Stop Events** - When services are stopped
- **Service Restart Events** - Service restart operations
- **Service Failures** - When services fail or crash
- **Exit Codes** - Service exit codes for failure analysis
- **Cgroup Events** - Service cgroup creation/destruction

## Architecture

```
┌─────────────────────────────────────────┐
│         Systemd Services                │
│    (docker, nginx, postgres, etc.)      │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│        eBPF Tracepoints                 │
│   sched_process_exec/exit               │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Systemd Observer                  │
│   - Tracks service state changes        │
│   - Monitors service failures           │
│   - Rate limits events                  │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Systemd Events                    │
│   EventType: "systemd_service"          │
│   ServiceName, State, ExitCode          │
└─────────────────────────────────────────┘
```

## Configuration

```go
config := &systemd.Config{
    BufferSize:           10000,  // Event buffer size
    EnableEBPF:           true,   // Use eBPF monitoring
    EnableJournal:        false,  // Use journal monitoring
    MonitorServiceStates: true,   // Track state changes
    MonitorCgroups:       true,   // Track cgroup events
    RateLimitPerSecond:   1000,   // Rate limiting
    ServicePatterns:      []string{}, // Empty = all services
}
```

## Metrics

The observer exposes the following metrics:

- `systemd_events_processed_total` - Total events processed
- `systemd_errors_total` - Total errors encountered
- `systemd_processing_duration_ms` - Event processing duration
- `systemd_services_monitored` - Number of services being monitored
- `systemd_service_starts_total` - Total service start events
- `systemd_service_stops_total` - Total service stop events
- `systemd_service_failures_total` - Total service failures

## Service States

The observer tracks these service states:
- `active` - Service is running
- `inactive` - Service is stopped
- `failed` - Service has failed
- `activating` - Service is starting
- `deactivating` - Service is stopping

## Event Data Structure

Each event contains:
```go
type SystemdServiceEvent struct {
    ServiceName string    // Name of the service
    EventType   string    // Type of event (start/stop/failed)
    PID         uint32    // Process ID
    UID         uint32    // User ID
    GID         uint32    // Group ID
    ExitCode    int32     // Exit code (for failures)
    Signal      int32     // Signal that caused exit
    CgroupID    uint64    // Cgroup identifier
    CgroupPath  string    // Full cgroup path
    Severity    string    // Event severity
    Comm        string    // Command name
}
```

## Usage Example

```go
// Create observer
config := systemd.DefaultConfig()
observer, err := systemd.NewObserver("systemd-monitor", config)
if err != nil {
    log.Fatal(err)
}

// Start monitoring
ctx := context.Background()
if err := observer.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range observer.Events() {
    if data, ok := event.EventData.(domain.SystemdServiceEvent); ok {
        log.Printf("Service %s: %s (exit_code=%d)",
            data.ServiceName,
            data.EventType,
            data.ExitCode)
    }
}

// Get service states
states := observer.GetServiceStates()
for name, state := range states {
    log.Printf("Service %s: %s", name, state.State)
}

// Stop monitoring
observer.Stop()
```

## Platform Support

- **Linux**: Full eBPF-based monitoring with kernel-level visibility
- **Non-Linux**: Simulation mode for testing and development

## Performance Considerations

1. **Rate Limiting**: Configurable rate limiting prevents event storms
2. **Buffer Management**: Tunable buffer size for high-volume environments
3. **Selective Monitoring**: Can filter services by patterns to reduce overhead
4. **Efficient State Tracking**: In-memory service state cache

## Integration Points

- Correlates with container runtime events for containerized services
- Links with process signals for crash analysis
- Connects with health observer for resource exhaustion correlation
- Integrates with scheduler observer for performance impact analysis