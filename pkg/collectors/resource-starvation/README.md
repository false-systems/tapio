# Resource Starvation Collector

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
```

## Event Types

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