# Memory Observer

A production-ready, CO-RE eBPF-based memory leak detector for Kubernetes environments. This observer tracks memory allocations, detects leaks, captures stack traces, and enriches events with Kubernetes metadata.

## Features

### Core Capabilities
- **CO-RE eBPF**: Compile Once, Run Everywhere - works across kernel versions without recompilation
- **Memory Allocation Tracking**: Monitors malloc/free and mmap/munmap operations via uprobes
- **Leak Detection**: Identifies long-lived allocations that may indicate memory leaks
- **Stack Trace Capture**: Records call stacks for allocation sites to aid in debugging
- **Kubernetes Enrichment**: Automatically adds pod, namespace, and container metadata

### Performance Optimizations
- **Configurable Filtering**: Skip small allocations below threshold
- **Stack Deduplication**: Avoid reporting the same leak multiple times
- **Rate Limiting**: Prevent event storms from overwhelming the system
- **Ring Buffer**: Efficient kernel-to-userspace event transfer

## Architecture

```
┌──────────────────────────────────────┐
│         User Space (Go)              │
├──────────────────────────────────────┤
│  Observer                            │
│  ├── Configuration Management        │
│  ├── Event Processing Pipeline       │
│  ├── K8s Enrichment                 │
│  └── Metrics Collection             │
├──────────────────────────────────────┤
│  eBPF Programs (Kernel Space)        │
│  ├── malloc/free uprobes            │
│  ├── mmap/munmap uprobes            │
│  ├── Stack trace capture            │
│  └── Allocation tracking maps       │
└──────────────────────────────────────┘
```

## Configuration

```go
type Config struct {
    // Basic settings
    Name       string `json:"name"`
    BufferSize int    `json:"buffer_size"`
    EnableEBPF bool   `json:"enable_ebpf"`

    // Operation modes
    Mode OperationMode `json:"mode"`
    // - growth_detection: RSS monitoring only (lowest overhead)
    // - targeted: Track specific PID
    // - debugging: Full tracking with stack traces

    // Filtering thresholds
    MinAllocationSize int64         `json:"min_allocation_size"` // Default: 10KB
    MinUnfreedAge     time.Duration `json:"min_unfreed_age"`     // Default: 30s
    SamplingRate      int           `json:"sampling_rate"`       // 1 in N allocations
    MaxEventsPerSec   int           `json:"max_events_per_sec"`  // Rate limiting

    // Stack deduplication
    StackDedupWindow time.Duration `json:"stack_dedup_window"` // Default: 10s

    // Targeted mode
    TargetPID      int32  `json:"target_pid"`       // 0 = all processes
    TargetCGroupID uint64 `json:"target_cgroup_id"` // Target container

    // Library path
    LibCPath string `json:"libc_path"` // Default: /lib/x86_64-linux-gnu/libc.so.6

    // K8s enrichment
    EnableK8sEnrichment bool `json:"enable_k8s_enrichment"`
}
```

## Usage

### Basic Usage

```go
import "github.com/yairfalse/tapio/internal/observers/memory"

// Create observer with default config
config := memory.DefaultConfig()
observer, err := memory.NewObserver("memory-observer", config, logger)
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
    log.Printf("Memory event: %+v", event)
}

// Stop observer
observer.Stop()
```

### Production Configuration

```go
config := &memory.Config{
    Name:       "production-memory-observer",
    BufferSize: 50000,
    EnableEBPF: true,
    
    // Use growth detection mode for lower overhead
    Mode: memory.ModeGrowthDetection,
    
    // Only track allocations >= 100KB
    MinAllocationSize: 102400,
    
    // Consider unfreed after 1 minute
    MinUnfreedAge: 1 * time.Minute,
    
    // Sample 1 in 100 for medium allocations
    SamplingRate: 100,
    
    // Rate limit to 5000 events/sec
    MaxEventsPerSec: 5000,
    
    // Enable K8s metadata
    EnableK8sEnrichment: true,
}
```

### Debugging Memory Leaks

```go
config := &memory.Config{
    Name: "leak-hunter",
    Mode: memory.ModeDebugging,
    
    // Track all allocations >= 1KB
    MinAllocationSize: 1024,
    
    // Report leaks after 30 seconds
    MinUnfreedAge: 30 * time.Second,
    
    // No sampling - track everything
    SamplingRate: 1,
    
    // Target specific pod
    TargetPID: 12345,
}
```

## Event Types

### Memory Allocation Event
```go
type MemoryEvent struct {
    Timestamp       uint64
    EventType       EventType
    PID            uint32
    TID            uint32
    Address        uint64
    Size           uint64
    StackTrace     *StackTrace
    IsLeak         bool
    AllocationAgeNs uint64
    Kubernetes     *KubernetesMetadata
}
```

### Event Types
- `EventTypeMalloc`: malloc() allocation
- `EventTypeFree`: free() deallocation  
- `EventTypeMmap`: Large allocation via mmap
- `EventTypeMunmap`: Memory freed via munmap
- `EventTypeUnfreed`: Long-lived allocation (potential leak)
- `EventTypeLeakDetected`: Confirmed memory leak
- `EventTypeRSSGrowth`: RSS increase detected

## Metrics

The observer exposes the following metrics:

### Memory Metrics
- `memory_allocations_tracked_total`: Total allocations tracked
- `memory_deallocations_tracked_total`: Total deallocations tracked
- `memory_unfreed_memory_bytes`: Current unfreed memory
- `memory_largest_allocation_bytes`: Largest single allocation
- `memory_leaks_detected_total`: Total leaks detected
- `memory_allocated_bytes`: Total bytes allocated
- `memory_freed_bytes`: Total bytes freed

### Performance Metrics
- `memory_events_processed_total`: Total events processed
- `memory_events_dropped_total`: Events dropped due to buffer full
- `memory_filtered_events_total`: Events filtered by pre-processing
- `memory_processing_time_seconds`: Event processing time histogram

## Requirements

### Kernel Requirements
- Linux kernel 4.15+ (for CO-RE support)
- BTF support enabled
- eBPF support enabled

### Kubernetes Requirements
- When running in Kubernetes, requires privileged container for eBPF
- K8s enrichment requires in-cluster RBAC permissions

### Build Requirements
- Go 1.21+
- Clang/LLVM for BPF compilation
- libbpf headers

## Building

```bash
# Generate BPF code
cd internal/observers/memory
go generate ./bpf

# Build observer
go build .

# Run tests
go test ./...
```

## Troubleshooting

### High Memory Usage
- Increase `MinAllocationSize` to track fewer allocations
- Increase `SamplingRate` to sample less frequently
- Use `ModeGrowthDetection` instead of `ModeDebugging`

### Missing Events
- Check if buffer is full (see `events_dropped_total` metric)
- Increase `BufferSize` configuration
- Reduce `SamplingRate` if tracking too many allocations

### No Stack Traces
- Ensure target binary has frame pointers (`-fno-omit-frame-pointer`)
- Check if stack unwinding is supported for the target language
- Verify BTF information is available

### K8s Metadata Missing
- Verify `EnableK8sEnrichment` is true
- Check RBAC permissions for pod/namespace access
- Ensure cgroup paths are correctly parsed

## Performance Impact

| Mode | CPU Overhead | Memory Overhead | Use Case |
|------|--------------|-----------------|----------|
| growth_detection | < 1% | Minimal | Production monitoring |
| targeted | 1-3% | Low | Debugging specific process |
| debugging | 3-5% | Moderate | Development/debugging |

## Limitations

- Requires libc with symbols for uprobe attachment
- Stack traces limited to 20 frames depth
- Maximum 10,000 concurrent allocations tracked
- Does not track kernel memory allocations
- Requires root/CAP_BPF capability

## Integration with Tapio

The Memory Observer follows Tapio's 5-level architecture:

- **Level 0 (Domain)**: Uses `domain.CollectorEvent` for event representation
- **Level 1 (Observers)**: Implements the Observer interface
- **Level 2 (Intelligence)**: Events can be correlated for leak pattern detection
- **Level 3 (Integrations)**: Supports K8s metadata enrichment
- **Level 4 (Interfaces)**: Exposes events via gRPC/REST APIs

## Files Structure

```
memory/
├── README.md           # This file
├── observer.go         # Main observer implementation
├── observer_ebpf.go    # Linux eBPF implementation
├── observer_fallback.go # Non-Linux fallback
├── config.go           # Configuration structures
├── types.go            # Event and data types
├── k8s_enricher.go     # Kubernetes metadata enrichment
├── bpf/
│   ├── generate.go     # BPF code generation
│   └── memory_*.go     # Generated BPF bindings
└── bpf_src/
    └── memory.c        # eBPF C source code
```

## Contributing

When modifying the Memory Observer:

1. **No TODOs or Stubs**: Complete implementations only (per CLAUDE.md)
2. **Test Coverage**: Maintain >80% test coverage
3. **Strong Typing**: No `map[string]interface{}` - use typed structs
4. **Error Handling**: Always wrap errors with context
5. **BPF Changes**: Regenerate bindings with `go generate ./bpf`

## License

Part of the Tapio Observability Platform.