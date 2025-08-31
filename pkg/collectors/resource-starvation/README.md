# Resource Starvation Collector

eBPF-based detection of CPU scheduling delays and throttling events in containerized environments.

## Purpose

Tracks CPU starvation events that traditional metrics miss:
- **Scheduling delays**: Time spent waiting for CPU (the "invisible" latency)
- **CFS throttling**: When containers hit their CPU quota limits
- **Priority inversions**: Lower priority tasks blocking higher priority ones
- **Core migrations**: Excessive CPU hopping that impacts cache performance
A high-performance eBPF-based collector for detecting and monitoring CPU resource starvation in Linux systems. This collector provides deep insights into scheduling delays, CFS throttling, priority inversions, and noisy neighbor effects.

## Features

- **Real-time CPU Starvation Detection**: Monitors scheduling delays exceeding configurable thresholds
- **CFS Throttling Monitoring**: Detects when cgroups hit their CPU quota limits
- **Priority Inversion Detection**: Identifies when lower-priority tasks block higher-priority ones
- **Core Migration Tracking**: Monitors excessive CPU core migrations that impact cache performance
- **Noisy Neighbor Detection**: Identifies processes monopolizing CPU resources
- **Pattern Detection**: Automatically identifies recurring starvation patterns
- **Stack Trace Capture**: Provides kernel stack traces for root cause analysis
- **Zero Overhead**: Efficient eBPF programs with minimal performance impact


## Architecture

```
┌─────────────────────────────────────────────┐
│           User Space (Go)                    │
├─────────────────────────────────────────────┤
│  - Event enrichment                          │
│  - Pattern detection                         │
│  - Metrics export                            │
└────────────────┬────────────────────────────┘
                 │ Ring Buffer
┌────────────────┴────────────────────────────┐
│         Kernel Space (eBPF)                  │
├─────────────────────────────────────────────┤
│  Tracepoints:                                │
│  - sched_stat_wait                           │
│  - sched_stat_runtime                        │
│  - sched_migrate_task                        │
│  - sched_switch                              │
=======
│           User Space (Go Collector)          │
├─────────────────────────────────────────────┤
│  - Event Processing & Enrichment             │
│  - Pattern Detection                         │
│  - Metrics Export (OpenTelemetry)           │
│  - Configuration Management                  │
└────────────────┬────────────────────────────┘
                 │ Ring Buffer
┌────────────────┴────────────────────────────┐
│         Kernel Space (eBPF Programs)         │
├─────────────────────────────────────────────┤
│  Tracepoints:                               │
│  - sched/sched_stat_wait                    │
│  - sched/sched_stat_runtime                 │
│  - sched/sched_migrate_task                 │
│  - sched/sched_switch                       │
└─────────────────────────────────────────────┘


## Event Types


1. **Scheduling Delay**: Process waited > threshold for CPU time
2. **CFS Throttling**: Container exceeded its CPU quota
3. **Priority Inversion**: High-priority task blocked by lower-priority one
4. **Core Migration**: Process moved between CPUs excessively
5. **Noisy Neighbor**: Process consuming excessive CPU time

## Configuration

```yaml
resource_starvation:
  enabled: true
  
  # Thresholds (milliseconds)
  starvation_threshold_ms: 100    # Consider it starvation after 100ms wait
  severe_threshold_ms: 500        # Severe starvation
  critical_threshold_ms: 2000     # Critical starvation
  
  # Performance controls
  sample_rate: 0.1                # Sample 10% of events
  max_events_per_sec: 1000        # Rate limiting
  
  # Pattern detection (optional)
  enable_pattern_detection: true
  pattern_window_sec: 60          # Look for patterns in 60s windows
```

## Metrics

OpenTelemetry metrics exported:

- `resource_starvation_sched_delay_seconds`: Scheduling delay histogram
- `resource_starvation_events_total`: Counter by event type and severity
- `resource_starvation_throttle_duration_seconds`: CFS throttle duration
- `resource_starvation_patterns_detected_total`: Recurring patterns found

## Usage

### Basic Setup

```go
config := &Config{
    StarvationThresholdMS: 100,
    SampleRate: 0.1,  // Start with 10% sampling

### 1. Scheduling Delay (EVENT_SCHED_WAIT)
Detects when tasks wait excessively in the runqueue before getting CPU time.

### 2. CFS Throttling (EVENT_CFS_THROTTLE)
Identifies when cgroup CPU bandwidth limits are hit, causing throttling.

### 3. Priority Inversion (EVENT_PRIORITY_INVERT)
Catches situations where high-priority tasks are blocked by lower-priority ones.

### 4. Core Migration (EVENT_CORE_MIGRATE)
Tracks excessive CPU core migrations that can cause cache thrashing.

### 5. Noisy Neighbor (EVENT_NOISY_NEIGHBOR)
Identifies processes consuming excessive CPU time and starving others.

## Installation

### Prerequisites

- Linux kernel 5.4+ with BTF support
- Go 1.19+
- libbpf headers
- clang/llvm for eBPF compilation

### Building

```bash
# Generate eBPF bytecode
make generate

# Run tests
go test ./...

# Build the collector
go build ./...
```

## Configuration

```go
config := &Config{
    Name:                  "resource-starvation",
    StarvationThresholdMS: 100,  // Minimum delay to consider starvation
    SevereThresholdMS:     500,  // Severe starvation threshold
    CriticalThresholdMS:   1000, // Critical starvation threshold
    EnablePatternDetection: true,
    SampleRate:            1.0,  // Sample 100% of events
    MaxEventsPerSecond:    1000,
    FilterPIDs:            []uint32{}, // Empty = monitor all PIDs

}

collector, err := NewCollector(config, logger)
if err != nil {
    log.Fatal(err)
}

if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
```

## Usage

### Basic Example

```go
package main

import (
    "context"
    "log"
    
    resourcestarvation "github.com/yourorg/tapio/pkg/collectors/resource-starvation"
    "go.uber.org/zap"
)

func main() {
    logger, _ := zap.NewProduction()
    config := resourcestarvation.NewDefaultConfig()
    
    collector, err := resourcestarvation.NewCollector(config, logger)
    if err != nil {
        log.Fatal(err)
    }
    defer collector.Stop()
    
    ctx := context.Background()
    if err := collector.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Collector is now running and emitting metrics
    select {}
}
```

### Filtering Specific PIDs

Monitor only specific processes:

```go
config.FilterPIDs = []uint32{1234, 5678}
```

## Pattern Detection

The collector can identify recurring patterns:

- **Periodic Throttling**: Regular CFS bandwidth hits
- **Migration Storm**: Rapid CPU core changes
- **Sustained Starvation**: Continuous scheduling delays

## Requirements

- Linux kernel 5.4+ with BTF support
- `CONFIG_DEBUG_INFO_BTF=y`
- `CONFIG_BPF_SYSCALL=y`
- `CONFIG_FTRACE=y`

Optional but helpful:
- `CONFIG_CFS_BANDWIDTH=y` for accurate throttling detection

## Troubleshooting

### No Events Captured

1. Check thresholds aren't too high
2. Verify kernel has required configs: `zcat /proc/config.gz | grep BPF`
3. Check for dropped events in metrics
4. Try increasing sample rate temporarily

### High Overhead

1. Reduce sample_rate (e.g., 0.01 for 1% sampling)
2. Increase thresholds to reduce event volume
3. Use PID filtering to focus on specific processes

### eBPF Load Failures

1. Check kernel version: `uname -r` (needs 5.4+)
2. Verify BTF: `ls /sys/kernel/btf/vmlinux`
3. Check permissions (needs CAP_SYS_ADMIN)
4. Review dmesg for BPF errors: `dmesg | grep -i bpf`

## Limitations

- Stack traces require additional symbols for full resolution
- CFS throttling detection requires CONFIG_CFS_BANDWIDTH
- Some patterns may only be visible with higher sampling rates
- Per-container tracking requires cgroup v2

## Performance Impact

Typical overhead with default settings:
- CPU: < 0.1% with 10% sampling
- Memory: ~50MB including ring buffers
- Network: No impact
- Disk: No impact

## See Also

- [Kernel Collector](../kernel/) - For system-wide CPU metrics
- [CRI Collector](../cri/) - For container runtime metrics
- [Orchestrator Collector](../orchestrator/) - For scheduling decisions
=======
```go
config.FilterPIDs = []uint32{1234, 5678, 9012}
```

### Adjusting Thresholds

```go
// Only detect severe starvation (>500ms delays)
config.StarvationThresholdMS = 500
config.SevereThresholdMS = 1000
config.CriticalThresholdMS = 2000
```

## Metrics

The collector exports the following OpenTelemetry metrics:

- `starvation_events_total`: Counter of starvation events by type and severity
- `starvation_duration_seconds`: Histogram of starvation durations
- `throttle_duration_seconds`: Histogram of CFS throttle durations
- `cpu_hogs_detected_total`: Counter of CPU hog detections
- `priority_inversions_total`: Counter of priority inversion events
- `core_migrations_total`: Counter of excessive core migrations
- `patterns_detected_total`: Counter of recurring patterns identified
- `pattern_duration_seconds`: Duration of detected patterns
- `dropped_events_total`: Events dropped due to buffer overflow

## Pattern Detection

The collector automatically identifies recurring starvation patterns:

- **Periodic Throttling**: Regular CFS bandwidth hits
- **Sustained Throttling**: Continuous throttling
- **Severe Starvation**: Long scheduling delays
- **Migration Storm**: Rapid core migrations
- **Noisy Neighbor Pattern**: Consistent CPU monopolization
- **High Frequency Starvation**: Rapid, repeated delays

## Performance Impact

The collector is designed for production use with minimal overhead:

- eBPF programs execute in kernel space with nanosecond latency
- Ring buffer for lock-free event transmission
- Configurable sampling rates for high-traffic systems
- Per-CPU data structures to avoid contention
- LRU maps with automatic cleanup of old entries

## Troubleshooting

### eBPF Load Failures

If the eBPF programs fail to load:

1. Check kernel version: `uname -r` (needs 5.4+)
2. Verify BTF support: `ls /sys/kernel/btf/vmlinux`
3. Check permissions (needs CAP_SYS_ADMIN or root)
4. Review kernel logs: `dmesg | grep -i bpf`

### Missing Events

If events are not being captured:

1. Check dropped events counter in metrics
2. Increase ring buffer size if drops are occurring
3. Verify PID filter configuration
4. Check threshold settings aren't too high

### High CPU Usage

If the collector itself uses too much CPU:

1. Reduce sampling rate (e.g., 0.1 for 10% sampling)
2. Increase thresholds to reduce event volume
3. Use PID filtering to focus on specific processes
4. Lower MaxEventsPerSecond limit

## Kernel Requirements

### Required Kernel Configs
- `CONFIG_BPF=y`
- `CONFIG_BPF_SYSCALL=y`
- `CONFIG_DEBUG_INFO_BTF=y`
- `CONFIG_FTRACE=y`
- `CONFIG_FUNCTION_TRACER=y`

### Optional but Recommended
- `CONFIG_CFS_BANDWIDTH=y` (for accurate CFS throttling detection)
- `CONFIG_CGROUPS=y` (for cgroup-aware monitoring)

## Contributing

See the main project documentation for contribution guidelines.

## License

See LICENSE file in the project root.
