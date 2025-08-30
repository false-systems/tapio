# Resource Starvation Collector

eBPF-based detection of CPU scheduling delays and throttling events in containerized environments.

## Purpose

Tracks CPU starvation events that traditional metrics miss:
- **Scheduling delays**: Time spent waiting for CPU (the "invisible" latency)
- **CFS throttling**: When containers hit their CPU quota limits
- **Priority inversions**: Lower priority tasks blocking higher priority ones
- **Core migrations**: Excessive CPU hopping that impacts cache performance

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
└─────────────────────────────────────────────┘
```

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
}

collector, err := NewCollector(config, logger)
if err != nil {
    log.Fatal(err)
}

if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
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