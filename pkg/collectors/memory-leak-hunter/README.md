# Memory Leak Hunter

Production-grade memory leak detection using eBPF with zero application instrumentation.

## 🎯 Purpose

Detects memory leaks in production by tracking allocations and deallocations at the kernel level, providing:
- **Unfreed memory detection**: Identifies allocations that haven't been freed
- **RSS growth monitoring**: Tracks process memory growth patterns
- **Stack attribution**: Links leaks to specific code paths (when available)
- **Container correlation**: Maps memory usage to specific containers

## 🏗️ Architecture

### Three Operation Modes

1. **Growth Detection Mode** (Default - Always On)
   - Monitors RSS growth trends
   - Minimal overhead (<0.01% CPU)
   - Identifies which containers are leaking

2. **Targeted Mode** (On-Demand)
   - Activates when leak suspected
   - Tracks specific PID or container
   - Records large allocations (>10KB)

3. **Debug Mode** (Development)
   - Full stack traces
   - All allocations tracked
   - High overhead - not for production

### Lean Pre-Processing

The collector uses smart filtering to reduce noise without making leak decisions:
- **Size filtering**: Ignores allocations < 10KB
- **Age filtering**: Only reports allocations unfreed > 30 seconds
- **Stack deduplication**: Prevents duplicate reports
- **Rate limiting**: Max 1000 events/second

## 📊 Metrics (OpenTelemetry)

### Core Metrics (Required)
- `memory_leak_hunter_events_processed_total`: Total events processed
- `memory_leak_hunter_errors_total`: Total errors
- `memory_leak_hunter_processing_duration_ms`: Processing time
- `memory_leak_hunter_dropped_events_total`: Dropped events
- `memory_leak_hunter_buffer_usage`: Buffer utilization

### Memory-Specific Metrics
- `memory_leak_hunter_allocations_tracked_total`: Allocations tracked
- `memory_leak_hunter_deallocations_tracked_total`: Deallocations tracked
- `memory_leak_hunter_rss_growth_detected_total`: RSS growth events
- `memory_leak_hunter_unfreed_memory_bytes`: Current unfreed memory
- `memory_leak_hunter_largest_allocation_bytes`: Largest allocation seen

## 🚀 Usage

### Basic Configuration

```yaml
memory_leak_hunter:
  enabled: true
  mode: "growth_detection"  # Start with least invasive
  
  # Pre-processing filters
  min_allocation_size: 10240  # 10KB
  min_unfreed_age: 30s
  sampling_rate: 10  # 1 in 10 for medium allocations
  
  # RSS monitoring
  rss_growth_threshold: 256  # 1MB in pages
  rss_check_interval: 30s
```

### Targeted Investigation

When a leak is suspected:

```yaml
memory_leak_hunter:
  mode: "targeted"
  target_pid: 12345  # Or use target_cgroup_id
  target_duration: 5m
  min_allocation_size: 1024  # Lower threshold for investigation
```

## 📈 Event Types

The collector emits standard Tapio events:

- `EventTypeKernelFS`: Memory allocations (mmap/munmap)
- `EventTypeMemoryPressure`: RSS growth detected
- `EventTypeContainerOOM`: Potential OOM situation (unfreed memory)

Each event includes:
- Process information (PID, command)
- Kernel data (cgroup ID for container correlation)
- Custom fields with memory specifics (address, size, caller IP)

## 🔧 Technical Details

### eBPF Hooks

- **mmap/munmap uprobes**: Track large allocations
- **RSS tracepoint**: Monitor memory growth
- **LRU map**: Auto-evicts old allocations (max 10K entries)

### Realistic Limitations

- **Go specifics**: Go uses its own memory allocator, so we track mmap (large allocations) not malloc
- **Stack traces**: Require frame pointers (not default in Go)
- **Volume**: Can't track every allocation - focuses on large/long-lived ones

## 📋 Production Workflow

1. **Continuous Monitoring**: RSS growth detection always on
2. **Alert Triggered**: "Container X growing 10MB/hour"
3. **Activate Targeting**: Enable allocation tracking for that container
4. **Collect Data**: 5 minutes of targeted collection
5. **Analyze**: Intelligence layer identifies patterns
6. **Disable**: Return to growth detection mode

## 🏎️ Performance

- **Growth Detection**: <0.01% CPU overhead
- **Targeted Mode**: ~0.1% CPU overhead
- **Memory Usage**: ~100MB kernel memory for maps
- **Event Rate**: Max 1000/sec (configurable)

## 🔌 Integration

Works with Tapio's intelligence layer for:
- Pattern detection across time
- Leak velocity calculation
- Cross-container correlation
- OOM prediction

## 📊 Example Output

```json
{
  "event_id": "memory-3-1699123456789",
  "type": "kernel.filesystem",
  "source": "memory-leak-hunter",
  "event_data": {
    "kernel": {
      "event_type": "mmap",
      "pid": 12345,
      "command": "api-server",
      "cgroup_id": 98765
    },
    "custom": {
      "operation": "mmap",
      "address": "0x7f1234567890",
      "size_bytes": "1048576",
      "caller_ip": "0x400123",
      "rss_pages": "1024",
      "rss_growth": "256"
    }
  },
  "metadata": {
    "tags": ["memory", "allocation"],
    "labels": {
      "event_type": "mmap",
      "size_bytes": "1048576"
    }
  }
}
```

## 🏗️ Platform Architecture

### Cross-Platform Development Support

The collector uses a clean separation between platform-specific and platform-agnostic code:

```
memory-leak-hunter/
├── collector.go           # Core logic (platform-agnostic)
├── collector_ebpf.go      # Linux eBPF implementation (//go:build linux)
├── collector_fallback.go  # Non-Linux fallback (//go:build !linux)
└── bpf_src/              # eBPF C programs
```

- **Production**: Runs on Linux with full eBPF capabilities in Kubernetes
- **Development**: Compiles on Mac/Windows with graceful fallback
- **Testing**: Unit tests run on any platform, eBPF tests require Linux

This architecture ensures:
1. Clean compilation on all platforms for development
2. Full functionality on Linux production environments
3. Clear separation of concerns without complex abstractions
4. Easy local development and testing on Mac/Windows

## 🚨 Important Notes

1. **Not a Memory Profiler**: Detects leaks, doesn't profile all memory usage
2. **Large Allocations Only**: Focuses on allocations >10KB by default
3. **Requires Linux**: eBPF is Linux-specific for production
4. **Go Limitations**: Can't track small Go allocations directly

## 🎯 Success Metrics

- Detection accuracy: >95% for leaks >1MB
- False positive rate: <5%
- Time to detection: <5 minutes for significant leaks
- Performance overhead: <0.1% in targeted mode

This collector provides production-grade memory leak detection without application changes, focusing on actionable intelligence rather than overwhelming data.