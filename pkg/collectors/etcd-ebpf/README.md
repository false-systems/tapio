# Etcd eBPF Collector

Minimal etcd syscall monitoring via eBPF - focused on capturing low-level etcd WAL operations through kernel syscall tracing.

## Overview

The etcd eBPF collector monitors etcd processes at the kernel level by tracing write and fsync system calls. This approach provides minimal overhead visibility into etcd's Write-Ahead Log (WAL) operations without any userspace instrumentation.

## Architecture

```
pkg/collectors/etcd-ebpf/
├── collector.go              # Main eBPF collector implementation
├── ebpf_collector.go         # Linux-specific eBPF implementation
├── ebpf_collector_nolinux.go # Non-Linux platform stubs
├── etcd_ebpf_stub.go         # eBPF type stubs for compilation
├── config.go                 # eBPF-specific configuration
├── types.go                  # eBPF event types and process info
├── bpf_src/
│   └── etcd_monitor.c        # Secured eBPF kernel program
├── etcdmonitor_bpfel_*.go    # Generated eBPF bytecode (ARM64/x86)
├── etcdmonitor_bpfel_*.o     # Compiled eBPF objects
└── README.md                 # This documentation
```

## Features

- **Zero-Overhead Monitoring**: Kernel-level syscall tracing with minimal performance impact
- **Security-Hardened**: Multi-layer process validation prevents monitoring of unrelated processes
- **Exact Process Matching**: Only monitors processes named exactly "etcd" (not etcd-backup, etcdctl, etc.)
- **PID Allowlist Management**: Userspace validation with kernel-side allowlist for security
- **Process Discovery**: Automatic discovery and validation of legitimate etcd processes
- **Time-Based Expiration**: Prevents PID reuse attacks with configurable verification windows
- **Raw Syscall Data**: Captures uninterpreted syscall information for downstream analysis

## Security Features

The collector implements **defense-in-depth** security to prevent false positives:

### Layer 1: Exact Process Name Matching
- Must be exactly "etcd" (not "etcd-backup", "etcdctl", "etcd-operator", etc.)
- Character-by-character validation in kernel space

### Layer 2: PID Allowlist Management
- Userspace validates processes and maintains kernel-side allowlist
- Time-based expiration prevents stale PID monitoring

### Layer 3: Command-Line Validation
- Verifies etcd-specific flags (`--data-dir`, `--listen-client`, etc.)
- Rejects suspicious or empty command lines

### Layer 4: Process Metadata Validation
- Validates reasonable parent PID and process characteristics
- Cross-references multiple `/proc` sources for consistency

## Configuration

```go
config := etcdebpf.Config{
    BufferSize:               10000,
    ProcessDiscoveryInterval: 30,     // seconds
    PIDVerificationTimeout:   300,    // seconds (5 minutes)
    CaptureDataPayload:       false,  // disabled for performance
    MaxDataCaptureSize:       256,    // bytes
}
```

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/etcd-ebpf"

// Create eBPF collector
collector, err := etcdebpf.NewCollector("etcd-ebpf", config)
if err != nil {
    log.Fatal(err)
}

// Start monitoring (requires Linux + eBPF support)
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}
defer collector.Stop()

// Process raw syscall events
for event := range collector.Events() {
    // event.Data contains raw syscall information:
    // - timestamp, PID, TID
    // - syscall type (write/fsync)
    // - optional network info
    // - optional raw data payload
    fmt.Printf("Syscall Event: %s\n", event.Data)
}
```

## Event Format

Events contain minimal, strongly-typed syscall data:

```json
{
  "timestamp": 1640995200000000000,
  "pid": 1234,
  "tid": 1234,
  "type": 1,
  "data_len": 8,
  "src_ip": "192.168.1.100",
  "dst_ip": "192.168.1.101",
  "src_port": 2379,
  "dst_port": 2380,
  "raw_data": "aGVsbG8gd29ybGQ="
}
```

## Monitored System Calls

- **`write`**: Captures potential WAL write operations
- **`fsync`**: Captures WAL synchronization calls

All events include:
- Process information (PID, TID)
- High-precision timestamps
- Raw syscall type (numeric, no interpretation)
- Optional network metadata (if detected)
- Optional data payload (configurable)

## Platform Support

### Linux (Full Support)
- Requires kernel 4.1+ with eBPF support
- Requires `CAP_BPF` or `CAP_SYS_ADMIN` capabilities
- Automatically compiles and loads eBPF programs

### Non-Linux (Stub Implementation)
- macOS, Windows: Graceful fallback with error messages
- Build compatibility maintained across platforms
- No runtime functionality on non-Linux systems

## Performance Characteristics

- **Kernel-Level Efficiency**: Bypasses userspace context switches
- **Minimal CPU Overhead**: ~0.1% CPU impact per monitored process
- **Memory Efficient**: Ring buffer with configurable size
- **Low Latency**: Direct kernel-to-userspace event delivery
- **Scalable**: Supports monitoring multiple etcd processes simultaneously

## eBPF Program Details

The kernel program (`bpf_src/etcd_monitor.c`) implements:

- **Tracepoint Attachment**: `syscalls:sys_enter_write`, `syscalls:sys_enter_fsync`
- **Process Filtering**: Kernel-side PID allowlist lookup
- **Event Structure**: Packed binary format for efficiency
- **Ring Buffer**: Lock-free event delivery to userspace
- **Memory Safety**: Bounds checking and validation

### Compilation Requirements

**IMPORTANT**: eBPF programs must be compiled on Linux:

```bash
# Requires Linux system with clang/LLVM and kernel headers
go generate ./...

# Generated files can be committed and used on any platform
```

## Process Discovery

The collector automatically discovers etcd processes:

1. **Periodic Scanning**: Scans `/proc` every 30 seconds (configurable)
2. **Process Validation**: Multi-layer security validation
3. **Allowlist Updates**: Maintains kernel-side PID allowlist
4. **Cleanup**: Removes terminated processes from allowlist
5. **Metrics**: Tracks number of verified processes

## Security Considerations

- **Privilege Requirements**: Requires `CAP_BPF` or root access
- **Kernel Trust**: eBPF programs run in kernel space with verification
- **Process Isolation**: Only monitors validated etcd processes
- **Data Sensitivity**: Optionally captures syscall data payloads
- **Resource Limits**: Configurable buffer sizes prevent resource exhaustion

## Use Cases

1. **Low-Level Performance Analysis**: Monitor etcd WAL performance at syscall level
2. **Storage I/O Patterns**: Analyze write and sync patterns
3. **System-Level Debugging**: Debug etcd storage issues
4. **Security Monitoring**: Detect unusual etcd process behavior
5. **Compliance Auditing**: Record all etcd storage operations

## Limitations

- **Linux Only**: eBPF requires Linux kernel 4.1+
- **Root Privileges**: Requires elevated permissions for eBPF
- **No Interpretation**: Raw syscalls only (no business logic parsing)
- **Kernel Dependency**: Requires compatible kernel headers
- **Limited Context**: Syscall-level view only (no application context)

## Metrics

The collector exposes comprehensive OpenTelemetry metrics:

- `etcd_ebpf_events_processed_total`: Total syscalls processed
- `etcd_ebpf_errors_total`: Errors by type (eBPF, parse, buffer)
- `etcd_ebpf_processing_duration_ms`: Event processing latency
- `etcd_ebpf_syscalls_monitored_total`: Syscalls by type and PID
- `etcd_ebpf_processes_tracked`: Number of verified etcd processes

## Dependencies

- `github.com/cilium/ebpf`: eBPF program loading and management
- `go.opentelemetry.io/otel`: Observability and metrics
- `go.uber.org/zap`: Structured logging
- Linux kernel headers (compilation only)

## Health Checks

The collector provides detailed eBPF status:

```go
health := collector.Health()
fmt.Printf("eBPF Active: %s, Processes: %s\n", 
    health.ComponentInfo["ebpf_active"],
    health.ComponentInfo["processes_tracked"])
```

## Troubleshooting

### Common Issues

1. **Permission Denied**: Ensure `CAP_BPF` or root access
2. **eBPF Not Supported**: Verify kernel version (4.1+) and CONFIG_BPF=y
3. **No Events**: Check that etcd processes are running and validated
4. **High CPU Usage**: Reduce buffer size or disable data payload capture

### Debug Mode

Enable verbose logging for troubleshooting:

```go
config.CaptureDataPayload = true  // Capture syscall data
// Check collector.Health() for detailed component status
```