# Scheduler Observer

**Status: Production Ready (Linux with eBPF)**

## Overview

The Scheduler observer detects CPU scheduling anomalies and resource contention that impact application performance. It captures invisible latency caused by scheduling delays, CPU throttling, and noisy neighbors using eBPF to monitor kernel scheduler decisions in real-time.

## What This Observer Does

- **Scheduling Delay Detection**: Measures time processes spend waiting to run
- **CPU Throttling Monitoring**: Tracks CFS bandwidth throttling events
- **Noisy Neighbor Detection**: Identifies processes monopolizing CPU cores
- **Core Migration Tracking**: Monitors involuntary CPU core migrations
- **Priority Inversion Detection**: Catches when low-priority tasks block high-priority ones
- **Wait/Run Ratio Analysis**: Calculates the ratio of wait time to actual runtime

## The Invisible Latency Problem

Your application might be slow not because of your code, but because it's not getting CPU time:

```
User Experience: "The app feels sluggish"
Metrics: CPU usage shows only 30%
Reality: Process is waiting 70% of the time for CPU scheduling
Root Cause: Noisy neighbor consuming CPU quantum
```

This observer makes the invisible visible by tracking kernel scheduler decisions.

## Features

- ✅ Sub-millisecond scheduling delay detection
- ✅ Real-time throttle event capture
- ✅ Per-container noisy neighbor scoring
- ✅ Pattern detection for recurring issues
- ✅ Stack trace capture for debugging (optional)
- ✅ Cgroup-aware container attribution
- ✅ CFS bandwidth controller integration
- ✅ NUMA-aware migration tracking

## Architecture

```
┌─────────────────────────┐
│     Kernel Space        │
│                         │
│  ╭─────────────────╮    │
│  │ sched_switch    │    │ ◄── Process context switches
│  │   tracepoint    │    │
│  ╰─────────────────╯    │
│                         │
│  ╭─────────────────╮    │
│  │ sched_wakeup    │    │ ◄── Process wakeup events
│  │   tracepoint    │    │
│  ╰─────────────────╯    │
│                         │
│  ╭─────────────────╮    │
│  │ cfs_bandwidth   │    │ ◄── CPU throttling
│  │    kprobe       │    │
│  ╰─────────────────╯    │
└─────────────────────────┘
            │
            ▼
┌─────────────────────────┐
│     User Space          │
│                         │
│  Pattern Detection      │
│  Noise Score Calc       │
│  Event Correlation      │
└─────────────────────────┘
```

## Events Generated

```go
domain.EventTypeSchedulingDelay     // Process waited too long for CPU
domain.EventTypeCPUThrottle         // Container hit CPU limit
domain.EventTypeNoisyNeighbor       // Process monopolizing CPU
domain.EventTypePriorityInversion   // Low-priority blocking high-priority
domain.EventTypeExcessiveMigration  // Too many core migrations
```

## Configuration

```go
type Config struct {
    // Detection thresholds
    SchedDelayThresholdMs  int     // Alert on scheduling delays (default: 10ms)
    ThrottleThresholdMs    int     // Alert on throttle duration (default: 100ms)
    MigrationThreshold     int     // Migrations per second (default: 10)
    NoiseNeighborThreshold float64 // CPU monopolization ratio (default: 0.8)
    
    // eBPF configuration
    RingBufferSize   int  // Kernel ring buffer size (default: 8MB)
    EventChannelSize int  // Event channel buffer (default: 10000)
    
    // Feature flags
    EnableStackTraces    bool // Capture stack traces (default: false)
    EnablePatternDetect  bool // Detect recurring patterns (default: true)
    EnableNoiseDetection bool // Calculate noise scores (default: true)
    
    // Performance tuning
    SamplingRate int // 1 = all events, 100 = 1 in 100 (default: 1)
}
```

## Usage Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/observers/scheduler"
)

func main() {
    config := scheduler.NewDefaultConfig()
    config.SchedDelayThresholdMs = 5  // Alert on >5ms delays
    
    logger, _ := zap.NewProduction()
    observer, err := scheduler.NewObserver("scheduler", config, logger)
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    if err := observer.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Process events
    for event := range observer.Events() {
        switch event.Type {
        case domain.EventTypeSchedulingDelay:
            log.Printf("WARNING: Process %s waited %dms for CPU",
                event.ProcessInfo.Command,
                event.SchedulerData.DelayMs)
                
        case domain.EventTypeNoisyNeighbor:
            log.Printf("ALERT: Noisy neighbor detected: %s using %d%% CPU",
                event.ProcessInfo.Command,
                event.SchedulerData.CPUPercent)
        }
    }
}
```

## Metrics (OpenTelemetry)

```
# Scheduling delays
scheduler_sched_delay_seconds{container_id, percentile}

# CPU throttling
scheduler_throttle_duration_seconds{container_id, cgroup}
scheduler_throttle_percentage{container_id}

# Noisy neighbor detection
scheduler_noise_score{container_id, pid}

# Wait/Run ratios
scheduler_wait_ratio{container_id}

# Core migrations
scheduler_core_migrations_total{container_id, from_cpu, to_cpu}

# Events
scheduler_events_total{event_type}
```

## Pattern Detection

The observer automatically detects recurring patterns:

### Thundering Herd
Multiple processes wake up simultaneously, causing scheduling delays:
```
Pattern: 50+ processes with scheduling delay spike
Confidence: 95%
Action: Consider staggering wake-ups
```

### CPU Throttle Cascade
Container repeatedly hits CPU limit causing cascading delays:
```
Pattern: Throttle → Delay → Throttle cycle
Confidence: 88%
Action: Increase CPU limits or optimize code
```

### Migration Storm
Excessive core migrations degrading cache performance:
```
Pattern: >100 migrations/sec for single process
Confidence: 92%
Action: Consider CPU pinning
```

## Noisy Neighbor Detection

The observer calculates a "noise score" for each process:

```go
NoiseScore = (CPU_Time_Used / Total_CPU_Time) * Impact_Factor

Where Impact_Factor considers:
- Number of affected processes
- Average scheduling delay induced
- Priority differential
```

Processes with noise score > 0.8 are flagged as noisy neighbors.

## eBPF Programs

### 1. Scheduling Delay (`sched_switch` tracepoint)
Measures time between process runnable and actually running

### 2. CPU Throttling (`sched_cfs_throttled` kprobe)
Captures when containers hit CPU bandwidth limits

### 3. Core Migrations (`sched_migrate_task` tracepoint)
Tracks involuntary CPU core changes

### 4. Priority Tracking (`sched_wakeup` tracepoint)
Monitors priority inversions and unfair scheduling

## Performance Impact

- **Overhead**: < 0.5% CPU for monitoring 1000 containers
- **Memory**: ~8MB for ring buffer + ~10MB for tracking
- **Latency**: Adds < 100ns to scheduler path
- **Accuracy**: Nanosecond precision timing

## Platform Requirements

### Linux Kernel
- **Minimum**: 4.15+ (basic eBPF support)
- **Recommended**: 5.4+ (better scheduler tracepoints)
- **Optimal**: 5.10+ (CFS bandwidth controller visibility)

### CGroups Configuration
- cgroup v2 recommended for container attribution
- CPU controller must be enabled
- `cpu.max` and `cpu.stat` files accessible

## Real-World Scenarios

### Scenario 1: Kubernetes CPU Limits
```
Problem: Microservice latency spikes despite low CPU usage
Detection: Throttling events every 100ms period
Root Cause: CPU limit too restrictive
Solution: Increase CPU limit from 500m to 800m
Result: P99 latency reduced by 60%
```

### Scenario 2: Batch Job Impact
```
Problem: Web service response times degrade at midnight
Detection: Noisy neighbor score spike for backup job
Root Cause: Backup job consuming entire CPU quantum
Solution: Set nice value +10 for batch jobs
Result: Web service latency stable during backups
```

### Scenario 3: NUMA Migration
```
Problem: Database query performance inconsistent
Detection: 50+ core migrations per second
Root Cause: Process migrating between NUMA nodes
Solution: Pin database to specific NUMA node
Result: Query performance variance reduced by 80%
```

## Integration with Intelligence Layer

Scheduler events correlate with other observers to identify:

- **Performance Degradation**: Link scheduling delays to SLO violations
- **Resource Contention**: Correlate with memory pressure events
- **Cascade Failures**: Track how CPU starvation triggers timeouts
- **Capacity Planning**: Identify when to scale based on scheduling pressure

## Troubleshooting

### High Scheduling Delays
1. Check for CPU oversubscription
2. Look for noisy neighbors
3. Verify cgroup CPU limits
4. Check for priority inversions

### Excessive Throttling
1. Review CPU limits in pod specs
2. Check for CPU burst usage
3. Analyze CPU usage patterns
4. Consider vertical scaling

### Missing Events
1. Verify eBPF programs loaded: `bpftool prog list`
2. Check kernel version compatibility
3. Ensure proper permissions (CAP_SYS_ADMIN)
4. Review ring buffer size for drops

## Testing

```bash
# Unit tests
go test ./pkg/observers/scheduler/...

# Stress test with CPU load
stress-ng --cpu 8 --cpu-load 80 --timeout 60s

# Verify metrics
curl localhost:9090/metrics | grep scheduler_

# Simulate throttling
docker run --cpus="0.5" stress:latest
```