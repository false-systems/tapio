# Systemd Service Monitoring Collector

High-performance systemd service monitoring via D-Bus with advanced pattern detection and dependency tracking.

## Features

### 🚀 High-Performance Monitoring
- **D-Bus Integration**: Direct systemd communication via D-Bus
- **Event Streaming**: Real-time service state change detection
- **Efficient Batching**: Process up to 1000+ state changes/minute
- **Memory Efficient**: <20MB memory footprint

### 🔍 Advanced Pattern Detection
- **Crash Loop Detection**: Identifies services stuck in restart loops
- **Anomaly Detection**: ML-based detection of unusual restart patterns
- **Time-Based Patterns**: Detects time-correlated failures
- **Memory Leak Detection**: Identifies services with increasing memory usage

### 🔗 Service Dependency Tracking
- **Dependency Graph**: Tracks service relationships
- **Impact Analysis**: Identifies affected services when failures occur
- **Container Runtime Focus**: Special handling for Docker, containerd, CRI-O
- **Transitive Dependencies**: Multi-level dependency analysis

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Systemd Collector                     │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────┐  │
│  │   D-Bus     │───▶│   Service    │───▶│  Event   │  │
│  │ Connection  │    │   Monitor    │    │ Channel  │  │
│  └─────────────┘    └──────────────┘    └──────────┘  │
│         │                   │                    ▲      │
│         │                   │                    │      │
│         ▼                   ▼                    │      │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────┐  │
│  │   Signal    │    │   Pattern    │    │  Event   │  │
│  │  Processor  │    │  Detector    │    │ Filter   │  │
│  └─────────────┘    └──────────────┘    └──────────┘  │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Usage

### Basic Configuration

```go
config := collectors.DefaultCollectorConfig("systemd-monitor", "systemd")
config.Extra = map[string]interface{}{
    "monitor_all_services": false,
    "service_whitelist": []string{"docker.service", "kubelet.service"},
    "track_dependencies": true,
    "restart_threshold": 3,
    "restart_window": "5m",
}

collector, err := systemd.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

// Start monitoring
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    log.Printf("Service event: %s - %s", event.Data["service"], event.Data["event_type"])
}
```

### Advanced Configuration

```go
config.Extra = map[string]interface{}{
    // Service filtering
    "monitor_all_services": false,
    "service_whitelist": []string{"docker", "containerd", "kubelet"},
    "service_blacklist": []string{"systemd-", "user@"},
    
    // Container runtime detection
    "container_runtime_filter": []string{"docker", "containerd", "cri-o"},
    
    // Pattern detection
    "restart_threshold": 3,
    "restart_window": "5m",
    "anomaly_detection": true,
    "baseline_period": "24h",
    
    // Dependency tracking
    "track_dependencies": true,
    "dependency_depth": 3,
    
    // Performance tuning
    "signal_buffer_size": 10000,
    "max_events_per_second": 1000,
    "poll_interval": "1s",
}
```

## Event Types

### Service State Events
- `started`: Service successfully started
- `stopped`: Service stopped normally
- `failed`: Service failed
- `restarting`: Service is restarting
- `restarted`: Service restarted after failure

### Pattern Events
- `crash_loop`: Service stuck in restart loop
- `periodic_crash`: Regular crash pattern detected
- `memory_leak`: Increasing memory usage pattern
- `anomaly`: Unusual behavior detected

## Pattern Detection

The collector includes sophisticated pattern detection:

### Crash Loop Detection
Identifies services that restart repeatedly within a short time window:
- Rapid restarts (< 30 seconds between restarts)
- Consecutive failures
- Escalating restart intervals

### Anomaly Detection
Uses statistical analysis to detect unusual patterns:
- Deviation from baseline restart rate
- Unusual time-of-day patterns
- Abnormal resource usage at restart

### Time-Based Patterns
Detects failures correlated with time:
- Hour-of-day clustering
- Day-of-week patterns
- Scheduled job correlations

## Performance

### Throughput
- **Signal Processing**: 10,000+ D-Bus signals/second
- **Event Generation**: 1,000+ events/second
- **State Tracking**: 1,000+ services simultaneously

### Resource Usage
- **Memory**: <20MB base + ~10KB per monitored service
- **CPU**: <1% overhead on host system
- **Network**: Minimal (local D-Bus only)

### Optimization Techniques
- Event batching for efficient processing
- Signal filtering at D-Bus level
- Concurrent service state polling
- Memory-efficient data structures

## Integration

### With Kubernetes
The collector automatically detects and prioritizes container runtime services:
- Docker daemon monitoring
- Containerd health tracking
- CRI-O state monitoring
- Kubelet service tracking

### Event Correlation
Events include correlation data for cross-collector analysis:
- Service dependency chains
- Process IDs for eBPF correlation
- Timestamps for timeline reconstruction
- Impact assessment for failures

## Troubleshooting

### D-Bus Connection Issues
```bash
# Check D-Bus system bus
systemctl status dbus

# Verify systemd is accessible
busctl tree org.freedesktop.systemd1

# Test connection
dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
    /org/freedesktop/systemd1 \
    org.freedesktop.DBus.Properties.Get \
    string:org.freedesktop.systemd1.Manager string:Version
```

### Permission Issues
The collector requires access to the system D-Bus:
```bash
# Add user to systemd-journal group
sudo usermod -a -G systemd-journal $USER

# Or run with appropriate capabilities
sudo setcap cap_dac_read_search+ep /path/to/tapio-collector
```

### High Memory Usage
- Reduce `service_whitelist` to monitor fewer services
- Decrease `event_buffer_size`
- Lower `signal_buffer_size`
- Increase `poll_interval`

## Metrics

The collector exposes metrics via the stats interface:

- `services_monitored`: Number of services being tracked
- `container_services`: Number of container runtime services
- `events_generated`: Total events generated
- `events_dropped`: Events dropped due to buffer overflow
- `dbus_signals_received`: D-Bus signals processed
- `patterns_detected`: Patterns identified by type
- `anomalies_detected`: Number of anomalies found

## Development

### Running Tests
```bash
# Unit tests
go test ./pkg/collectors/systemd/...

# Integration tests (requires D-Bus)
sudo go test -tags=integration ./pkg/collectors/systemd/...

# Benchmarks
go test -bench=. ./pkg/collectors/systemd/...
```

### Adding New Patterns
1. Define pattern type in `patterns.go`
2. Implement detection logic in `classifyPattern()`
3. Add tests for the new pattern
4. Update documentation

### Extending Service Monitoring
1. Add new service properties to `ServiceInfo`
2. Update `updateServiceFromProperties()`
3. Add event generation logic if needed
4. Update tests and documentation