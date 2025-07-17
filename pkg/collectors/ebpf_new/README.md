# eBPF Collector for Tapio

The eBPF collector provides deep kernel-level observability for Linux systems, capturing events from syscalls, network operations, process lifecycle, memory allocations, and file I/O operations.

## Features

- **Kernel-level event collection** using eBPF technology
- **Multiple event types**: syscalls, network, process, memory, file I/O
- **High-performance** ring buffer-based event transport
- **Flexible filtering** by event type, process ID, container, namespace
- **Rate limiting** to prevent event storms
- **Platform-aware** with graceful degradation on non-Linux systems
- **Production-ready** with comprehensive error handling and health monitoring

## Architecture

The eBPF collector follows Tapio's strict architectural constraints:

```
pkg/collectors/ebpf_new/
├── core/                  # Public interfaces and types
├── internal/              # Core implementation
├── linux/                 # Linux-specific eBPF implementation
├── stub/                  # Stub for non-Linux platforms
├── cmd/                   # Standalone executables
│   ├── collector/         # Main collector executable
│   └── debug/            # Debug utilities
├── testdata/             # Test fixtures
└── examples/             # Usage examples
```

## Usage

### Basic Usage

```go
import (
    ebpf "github.com/yairfalse/tapio/pkg/collectors/ebpf_new"
    "github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
)

// Create collector with default configuration
config := core.DefaultConfig()
collector, err := ebpf.NewCollector(config)
if err != nil {
    log.Fatal(err)
}
defer collector.Close()

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}
defer collector.Stop()

// Subscribe to events
criteria := domain.QueryCriteria{
    TimeWindow: domain.TimeWindow{
        Start: time.Now(),
        End:   time.Now().Add(time.Hour),
    },
}

eventChan, err := collector.Subscribe(ctx, criteria, domain.SubscriptionOptions{
    BufferSize: 1000,
})

// Process events
for event := range eventChan {
    fmt.Printf("Event: %+v\n", event)
}
```

### Configuration Profiles

The collector provides pre-configured profiles for common use cases:

```go
// Minimal configuration for testing
config := core.MinimalConfig()

// Syscall monitoring
config := core.SyscallMonitorConfig()

// Network monitoring
config := core.NetworkMonitorConfig()

// Process lifecycle monitoring
config := core.ProcessMonitorConfig()

// Memory allocation monitoring
config := core.MemoryMonitorConfig()

// File I/O monitoring
config := core.FileIOMonitorConfig()
```

### Custom Configuration

```go
config := core.Config{
    Name:               "custom-monitor",
    Enabled:            true,
    EventBufferSize:    10000,
    RingBufferSize:     65536,  // Must be power of 2
    BatchSize:          100,
    CollectionInterval: 50 * time.Millisecond,
    MaxEventsPerSecond: 10000,
    Timeout:            30 * time.Second,
    Programs: []core.ProgramSpec{
        {
            Name:         "tcp_monitor",
            Type:         core.ProgramTypeKprobe,
            AttachTarget: "tcp_v4_connect",
            Code:         tcpMonitorBytecode, // Your eBPF bytecode
            Maps: []core.MapSpec{
                {
                    Name:       "events",
                    Type:       core.MapTypeRingBuf,
                    KeySize:    0,
                    ValueSize:  0,
                    MaxEntries: 32768,
                },
            },
        },
    },
    Filter: core.Filter{
        EventTypes: []core.EventType{
            core.EventTypeNetworkOut,
        },
        ExcludeSystemProcesses: true,
    },
}
```

### Filtering Events

```go
// Set filter to monitor specific processes
filter := core.Filter{
    EventTypes: []core.EventType{
        core.EventTypeSyscall,
        core.EventTypeProcessExec,
    },
    ProcessIDs:   []uint32{1234, 5678},
    ContainerIDs: []string{"container-1", "container-2"},
    Namespaces:   []string{"default"},
    MinSeverity:  domain.SeverityMedium,
}

if err := collector.SetFilter(filter); err != nil {
    log.Fatal(err)
}
```

## Standalone Executables

### Collector

Run the eBPF collector as a standalone process:

```bash
# Build
cd pkg/collectors/ebpf_new
go build ./cmd/collector

# Run with minimal profile
sudo ./collector -profile minimal

# Run with custom config
sudo ./collector -config config.json

# Run in test mode (10 seconds)
sudo ./collector -profile syscall -test -verbose

# Output formats
sudo ./collector -profile network -output json
sudo ./collector -profile process -output text
```

### Debug Tool

Check system compatibility and debug issues:

```bash
# Build
go build ./cmd/debug

# Check eBPF support
sudo ./debug -check

# Test minimal program load
sudo ./debug -test-load

# List loaded programs
sudo ./debug -list

# Show collector statistics
sudo ./debug -stats
```

## Event Types

### System Events

```go
type SystemEventPayload struct {
    Component   string                 // "syscall", "process", "memory", "filesystem"
    Operation   string                 // Specific operation name
    Status      string                 // "success", "failed", etc.
    Message     string                 // Human-readable message
    ErrorCode   int64                  // System error code if applicable
    Details     map[string]interface{} // Additional event-specific details
}
```

### Network Events

```go
// Captured for EventTypeNetworkIn and EventTypeNetworkOut
Details: {
    "protocol":     "TCP",
    "source_addr":  "192.168.1.100",
    "source_port":  45678,
    "dest_addr":    "10.0.0.1",
    "dest_port":    443,
    "bytes_sent":   1024,
    "bytes_recv":   2048,
    "packet_count": 15,
}
```

### Process Events

```go
// Process execution (EventTypeProcessExec)
Details: {
    "filename":    "/usr/bin/ls",
    "args":        "-la /tmp",
    "ppid":        1234,
    "uid":         1000,
    "gid":         1000,
    "return_code": 0,
}

// Process exit (EventTypeProcessExit)
Details: {
    "exit_code":   0,
    "signal":      0,
    "core_dumped": false,
    "ppid":        1234,
}
```

### Memory Events

```go
// Memory allocation (EventTypeMemoryAlloc)
Details: {
    "size":       4096,
    "address":    "0x7f1234567890",
    "call_site":  "0x555555554000",
    "gfp_flags":  0x14000c0,
    "alloc_type": "kmalloc",
    "node_id":    0,
}
```

### File I/O Events

```go
// File operations (EventTypeFileIO)
Details: {
    "filename": "/var/log/app.log",
    "offset":   1024,
    "count":    512,
    "flags":    0x8002,
    "mode":     0644,
}
```

## Health Monitoring

```go
// Get collector health status
health := collector.Health()
fmt.Printf("Status: %s\n", health.Status)
fmt.Printf("Message: %s\n", health.Message)
fmt.Printf("Programs: %d loaded, %d healthy\n", 
    health.ProgramsLoaded, health.ProgramsHealthy)

// Check for issues
for _, issue := range health.Issues {
    fmt.Printf("Issue: [%s] %s - %s (since %s)\n",
        issue.Severity, issue.Component, issue.Issue, issue.Since)
}

// Get statistics
stats, _ := collector.GetStats()
fmt.Printf("Events collected: %d\n", stats.EventsCollected)
fmt.Printf("Events dropped: %d\n", stats.EventsDropped)
fmt.Printf("Ring buffer lost: %d\n", stats.RingBufferStats.Lost)
```

## Requirements

### Linux

- **Kernel version**: 4.15 or later (5.8+ recommended for full features)
- **Permissions**: Root or CAP_BPF capability
- **Dependencies**: BPF filesystem mounted at `/sys/fs/bpf`

### Build Requirements

- Go 1.21 or later
- CGO enabled for cilium/ebpf library

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires root on Linux)
sudo go test -tags=integration ./...

# Benchmarks
go test -bench=. ./...

# Coverage
go test -cover ./...
```

## Performance

The eBPF collector is designed for high-performance event collection:

- **Event rate**: Up to 100,000+ events/second (configurable)
- **Memory usage**: ~10MB base + ring buffer size
- **CPU overhead**: <1% for typical workloads
- **Latency**: Sub-millisecond event delivery

### Tuning

```go
// High-frequency event collection
config.BatchSize = 500
config.CollectionInterval = 10 * time.Millisecond
config.RingBufferSize = 1048576  // 1MB
config.MaxEventsPerSecond = 100000

// Low-overhead monitoring
config.BatchSize = 50
config.CollectionInterval = 500 * time.Millisecond
config.RingBufferSize = 16384    // 16KB
config.MaxEventsPerSecond = 1000
```

## Troubleshooting

### Common Issues

1. **"insufficient privileges"**
   - Run with sudo or grant CAP_BPF capability
   - Check user has access to /sys/fs/bpf

2. **"BPF filesystem not mounted"**
   - Mount BPF filesystem: `sudo mount -t bpf none /sys/fs/bpf`
   - Add to /etc/fstab for persistence

3. **"program load failed"**
   - Check kernel version supports required eBPF features
   - Verify kernel has CONFIG_BPF enabled
   - Check dmesg for verifier errors

4. **"ring buffer full"**
   - Increase RingBufferSize in configuration
   - Reduce CollectionInterval for more frequent reads
   - Enable rate limiting with MaxEventsPerSecond

### Debug Commands

```bash
# Check kernel config
zcat /proc/config.gz | grep CONFIG_BPF

# View loaded eBPF programs
sudo bpftool prog list

# View eBPF maps
sudo bpftool map list

# Monitor eBPF events
sudo cat /sys/kernel/debug/tracing/trace_pipe
```

## Examples

See the `examples/` directory for complete examples:

- `examples/basic/` - Basic event collection
- `examples/filtering/` - Advanced filtering
- `examples/multi-program/` - Multiple eBPF programs
- `examples/custom-parser/` - Custom event parsing

## Contributing

When contributing to the eBPF collector:

1. Follow Tapio's architectural constraints (see CLAUDE.md)
2. Ensure platform compatibility (Linux-specific code in `linux/`)
3. Add comprehensive tests (unit, integration, benchmarks)
4. Update documentation and examples
5. Verify no cross-level imports

## License

Part of the Tapio system monitoring platform.