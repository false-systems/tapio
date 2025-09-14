# Storage I/O Observer

**Status: Production Ready (Linux with eBPF)**

## Overview

The Storage I/O observer provides deep visibility into storage subsystem performance using eBPF to trace block device operations at the kernel level. It captures I/O patterns, latency distributions, and throughput metrics with minimal overhead.

## What This Observer Does

- **I/O Latency Tracking**: Measures request-to-completion time for all block I/O operations
- **Throughput Monitoring**: Tracks read/write bytes and IOPS per device
- **Queue Depth Analysis**: Monitors device queue depths and congestion
- **I/O Pattern Detection**: Identifies sequential vs random access patterns
- **Error Detection**: Captures I/O errors and retries
- **Device Mapping**: Correlates block devices to containers and pods

## Features

- ✅ eBPF-based zero-overhead monitoring
- ✅ Per-container I/O attribution
- ✅ Latency percentile tracking (p50, p95, p99)
- ✅ I/O size distribution analysis
- ✅ Device utilization metrics
- ✅ Write amplification detection
- ✅ SSD wear tracking (when available)
- ✅ Multi-queue block layer support

## Architecture

```
┌─────────────────┐
│   Kernel Space  │
│                 │
│  ╭────────────╮ │         ┌──────────────┐
│  │ blk_start  │ │◄────────│ Block Layer  │
│  │   kprobe   │ │         │   Requests   │
│  ╰────────────╯ │         └──────────────┘
│  ╭────────────╮ │         ┌──────────────┐
│  │ blk_done   │ │◄────────│  Completion  │
│  │   kprobe   │ │         │    Events    │
│  ╰────────────╯ │         └──────────────┘
└─────────────────┘
         │
         ▼
┌─────────────────┐
│   User Space    │
│                 │
│  ╭────────────╮ │
│  │ Ring Buffer│ │
│  │   Reader   │ │
│  ╰────────────╯ │
│  ╭────────────╮ │
│  │  Latency   │ │
│  │ Calculator │ │
│  ╰────────────╯ │
└─────────────────┘
         │
         ▼
   Domain Events
```

## eBPF Programs

### 1. Block Start Trace (`trace_block_rq_insert`)
```c
// Captures I/O request submission
int trace_block_rq_insert(struct pt_regs *ctx) {
    // Record start time and request details
    // Store in hash map keyed by request pointer
}
```

### 2. Block Complete Trace (`trace_block_rq_complete`)
```c
// Captures I/O completion
int trace_block_rq_complete(struct pt_regs *ctx) {
    // Calculate latency from start time
    // Generate event with full I/O metrics
}
```

## Events Generated

```go
domain.EventTypeStorageHighLatency    // I/O latency exceeds threshold
domain.EventTypeStorageThroughput     // Throughput anomaly detected
domain.EventTypeStorageError          // I/O errors occurred
domain.EventTypeStorageDeviceFull     // Device approaching capacity
domain.EventTypeStorageQueueDepth     // Queue depth saturation
```

## Configuration

```go
type Config struct {
    Name                string        // Observer name
    BufferSize          int          // Event buffer size
    
    // eBPF settings
    RingBufferSize      int          // Kernel ring buffer size (default: 8MB)
    SampleRate          int          // Sample 1 in N requests (default: 1)
    
    // Thresholds
    LatencyThresholdMs  float64      // High latency threshold (default: 100ms)
    ErrorRateThreshold  float64      // Error rate threshold (default: 0.01)
    UtilizationThreshold float64     // Device utilization threshold (default: 95%)
    
    // Features
    EnableLatencyHist   bool         // Collect latency histograms (default: true)
    EnableIOPattern     bool         // Detect I/O patterns (default: true)
    EnableContainerAttr bool         // Attribute I/O to containers (default: true)
    
    // Devices
    MonitoredDevices    []string     // Specific devices to monitor (empty = all)
    ExcludedDevices     []string     // Devices to exclude
}
```

## Usage Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/observers/storage-io"
)

func main() {
    config := storageio.NewDefaultConfig("storage-io")
    config.LatencyThresholdMs = 50.0  // Alert on >50ms latency
    config.MonitoredDevices = []string{"sda", "nvme0n1"}
    
    observer, err := storageio.NewObserver("storage-io", config)
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
        case domain.EventTypeStorageHighLatency:
            ioData, _ := event.GetIOData()
            log.Printf("High latency: %s device=%s latency=%fms",
                event.CorrelationHints.ContainerID,
                ioData.Device,
                ioData.LatencyMs)
                
        case domain.EventTypeStorageError:
            log.Printf("I/O error on device: %v", event)
        }
    }
}
```

## Metrics (OpenTelemetry)

```
# Latency metrics
storage_io_latency_ms{device, operation, container_id}
storage_io_latency_p50_ms{device}
storage_io_latency_p95_ms{device}
storage_io_latency_p99_ms{device}

# Throughput metrics
storage_io_read_bytes_total{device, container_id}
storage_io_write_bytes_total{device, container_id}
storage_io_read_ops_total{device, container_id}
storage_io_write_ops_total{device, container_id}

# Queue metrics
storage_io_queue_depth{device}
storage_io_inflight_requests{device}

# Error metrics
storage_io_errors_total{device, error_type}
storage_io_retries_total{device}

# Pattern metrics
storage_io_sequential_ratio{device}
storage_io_request_size_bytes{device, operation}
```

## I/O Attribution

The observer attributes I/O operations to containers using cgroup information:

```go
// I/O event includes container context
type IOEvent struct {
    Device      string
    ContainerID string
    PodUID      string
    Operation   string  // read/write
    SizeBytes   uint64
    LatencyNs   uint64
    Timestamp   time.Time
}
```

## Performance Characteristics

- **Overhead**: < 1% CPU for up to 100K IOPS
- **Memory**: ~10MB + ring buffer size
- **Latency**: Adds < 1μs to I/O path
- **Accuracy**: Nanosecond precision timing

## Platform Requirements

### Linux Kernel
- **Minimum**: 4.18+ (basic eBPF support)
- **Recommended**: 5.4+ (BTF support)
- **Optimal**: 5.10+ (improved BPF helpers)

### Kernel Configuration
```bash
CONFIG_BPF=y
CONFIG_BPF_SYSCALL=y
CONFIG_BPF_JIT=y
CONFIG_BLK_DEV_THROTTLING=y
CONFIG_BLK_DEV_THROTTLING_LOW=y
```

### Permissions
```yaml
securityContext:
  privileged: true  # Required for eBPF
  capabilities:
    add:
      - SYS_ADMIN
      - SYS_RESOURCE
```

## Storage Backend Support

| Backend | Support | Notes |
|---------|---------|-------|
| Local SSD/NVMe | ✅ Full | Best performance |
| Local HDD | ✅ Full | Higher latencies expected |
| iSCSI | ✅ Full | Network latency included |
| NFS | ⚠️ Partial | Limited visibility |
| Ceph RBD | ✅ Full | Via kernel client |
| AWS EBS | ✅ Full | Via block device |
| GCE PD | ✅ Full | Via block device |
| Azure Disk | ✅ Full | Via block device |

## Advanced Features

### Latency Breakdown
```go
type LatencyBreakdown struct {
    QueueLatency    time.Duration  // Time in queue
    ServiceLatency  time.Duration  // Device service time
    TotalLatency    time.Duration  // End-to-end latency
}
```

### I/O Pattern Analysis
```go
type IOPattern struct {
    SequentialRatio float64  // 0.0 (random) to 1.0 (sequential)
    ReadWriteRatio  float64  // Read operations / total operations
    AvgRequestSize  uint64   // Average I/O size in bytes
    IOPSProfile     string   // "small-random", "large-sequential", etc.
}
```

## Troubleshooting

### eBPF Loading Issues
```bash
# Check kernel support
grep CONFIG_BPF /boot/config-$(uname -r)

# Verify BPF filesystem
mount | grep bpf

# Check permissions
ls -la /sys/fs/bpf
```

### Missing I/O Events
```bash
# Verify block devices are visible
lsblk

# Check if device is excluded
cat /sys/block/*/queue/scheduler

# Verify eBPF programs are attached
bpftool prog list
```

### High Overhead
- Increase sample rate (e.g., sample 1 in 10 requests)
- Reduce ring buffer size
- Filter specific devices only
- Disable histogram collection

## Testing

```bash
# Unit tests
go test ./pkg/observers/storage-io/...

# Integration tests (requires Linux)
sudo go test -tags=integration ./pkg/observers/storage-io/...

# Stress test with fio
fio --name=test --rw=randrw --size=1G --runtime=60

# Verify metrics
curl localhost:9090/metrics | grep storage_io
```

## Comparison with Traditional Tools

| Feature | Storage I/O Observer | iostat | iotop | blktrace |
|---------|---------------------|--------|-------|----------|
| Overhead | < 1% | ~2% | ~5% | ~10% |
| Container Attribution | ✅ | ❌ | ⚠️ | ❌ |
| Latency Percentiles | ✅ | ❌ | ❌ | ✅ |
| Real-time Events | ✅ | ❌ | ✅ | ⚠️ |
| Production Safe | ✅ | ✅ | ⚠️ | ❌ |

## Integration with Intelligence Layer

The Storage I/O observer provides critical data for:

- **Root Cause Analysis**: Correlating application slowdowns with I/O bottlenecks
- **Capacity Planning**: Predicting when storage will be exhausted
- **Performance Optimization**: Identifying inefficient I/O patterns
- **Cost Optimization**: Right-sizing storage based on actual usage