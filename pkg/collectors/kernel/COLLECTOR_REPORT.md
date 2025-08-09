# eBPF Kernel Collector Report

## Status: ✅ FUNCTIONAL (MINIMAL)

**Location**: `pkg/collectors/ebpf/`  
**Type**: Minimal eBPF Collector  
**Created**: From scratch after deleting complex implementation  
**Lines of Code**: ~260 (collector.go: 260 lines)

## Architecture

### Minimal Design Principles
- **Zero Business Logic**: Only raw event emission
- **Container-Focused**: Tracks processes in K8s/Docker containers
- **CO-RE Compatible**: Works across different kernel versions
- **Single Ring Buffer**: Unified event collection

### Core Components

#### 1. Go Collector (`collector.go`)
```go
type Collector struct {
    name    string
    objs    *kernelmonitorObjects  // Generated eBPF objects
    links   []link.Link           // eBPF program links
    reader  *ringbuf.Reader       // Ring buffer reader
    events  chan collectors.RawEvent
    // ... minimal state management
}
```

#### 2. eBPF Program (`bpf/kernel_monitor.c`)
```c
struct kernel_event {
    __u64 timestamp;
    __u32 pid, tid, event_type;
    __u64 size;
    char comm[16];
    __u8 data[64];
} __attribute__((packed));
```

#### 3. Event Types
- `memory_alloc` (1): Memory allocation events
- `memory_free` (2): Memory deallocation events  
- `process_exec` (3): Process execution events

## Implementation Details

### Container Detection
- Scans `/proc/*/cgroup` for container indicators
- Looks for: `docker`, `containerd`, `kubepods`
- Populates eBPF map with container PIDs
- Kernel-side filtering reduces overhead

### eBPF Tracepoints
- `tracepoint/kmem/kmalloc`: Memory allocations
- `tracepoint/kmem/kfree`: Memory deallocations  
- `tracepoint/sched/sched_process_exec`: Process execution

### Event Processing
1. eBPF programs emit events to ring buffer
2. Go collector reads from ring buffer
3. Raw bytes converted to `RawEvent`
4. Metadata extracted (PID, TID, comm, size)
5. Events sent to pipeline via channel

## Dependencies

### Required
- `github.com/cilium/ebpf` - eBPF program loading and management
- Linux kernel with eBPF support (4.4+)
- CAP_BPF or CAP_SYS_ADMIN capabilities

### Build Dependencies  
- `clang` - eBPF program compilation
- Linux kernel headers
- `bpf2go` - Go code generation

## Testing

### Unit Tests (`collector_test.go`)
- ✅ Collector creation and lifecycle
- ✅ Event type string conversion
- ✅ Null-terminated string parsing
- ✅ Health status management
- ✅ Event channel functionality

### Integration Testing
- Requires privileged environment for eBPF loading
- Container detection tested in K8s environments
- Event generation verified with syscall activity

## Performance Characteristics

### Memory Usage
- Ring buffer: 4MB (configurable)
- Event channel: 1000 events buffered
- Minimal heap allocation per event

### CPU Overhead
- Kernel-side filtering reduces userspace processing
- Ring buffer provides efficient event transport
- No JSON parsing or complex transformations

### Event Throughput  
- Designed for high-frequency syscall events
- Ring buffer prevents event loss under load
- Configurable buffer sizes for tuning

## Comparison with Previous Implementation

| Aspect | Previous (Complex) | New (Minimal) |
|---|---|---|
| **Lines of Code** | 2000+ (multiple files) | ~260 (single file) |
| **Business Logic** | ❌ Complex processing | ✅ Zero business logic |
| **Compilation** | ❌ Broken API mismatches | ✅ Clean compilation |
| **Dependencies** | ❌ Multiple eBPF programs | ✅ Single unified program |
| **Maintainability** | ❌ Complex structure | ✅ Simple, focused |
| **Event Types** | Multiple managers | 3 core event types |

## Future Enhancements

### Potential Additions (Pipeline Layer)
- Network connection tracking
- File I/O monitoring  
- Security event detection
- Performance metric correlation

### Configuration Options
- Adjustable ring buffer size
- Event type filtering
- Container namespace selection
- Sampling rates for high-volume events

## Deployment

### DaemonSet Configuration
```yaml
spec:
  securityContext:
    privileged: true  # Required for eBPF
  volumes:
  - name: sys
    hostPath: {path: /sys}
  - name: proc  
    hostPath: {path: /proc}
```

### Health Checks
- Collector reports healthy when eBPF programs loaded
- Ring buffer reader active
- Event processing goroutine running

## Troubleshooting

### Common Issues
1. **Permission Denied**: Requires CAP_BPF/CAP_SYS_ADMIN
2. **eBPF Not Supported**: Kernel version < 4.4
3. **Compilation Errors**: Missing clang or kernel headers
4. **No Events**: Container PID detection issues

### Debugging
- Enable eBPF program logs via `/sys/kernel/debug/tracing/trace_pipe`
- Check container PID map population
- Verify tracepoint availability in `/sys/kernel/debug/tracing/events`

## Conclusion

The minimal eBPF collector successfully replaces the previous complex implementation with:
- ✅ Clean, maintainable codebase
- ✅ Zero business logic (raw events only)
- ✅ Successful compilation and testing
- ✅ Container-aware kernel monitoring
- ✅ Follows the minimal collector blueprint

This implementation provides a solid foundation for kernel-level observability while maintaining simplicity and reliability.