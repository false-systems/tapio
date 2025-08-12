# Etcd Collector

Minimal etcd collector using eBPF to monitor etcd syscalls with zero business logic.

## Architecture

```
pkg/collectors/etcd/
├── collector.go              # Minimal collector implementation
├── ebpf_collector.go         # Linux eBPF implementation
├── ebpf_collector_nolinux.go # Non-Linux stub
├── collector_test.go         # Unit tests
├── generate.go              # bpf2go generation
├── init.go                  # Registry integration
├── bpf/                     # eBPF programs
│   ├── etcd_monitor.c       # Syscall monitoring
│   └── headers/
│       └── vmlinux.h        # Kernel headers
└── etcdmonitor_*.go/o       # Generated eBPF objects
```

## Features

- **Minimal Design**: Zero business logic, just raw event collection
- **eBPF-Based**: Monitors write/fsync syscalls for WAL operations
- **No Parsing**: No operation type detection or enrichment
- **Low Overhead**: Efficient kernel-level monitoring
- **Raw Data**: Streams raw syscall data without interpretation

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/etcd"

// Create minimal collector
collector, err := etcd.NewCollector("etcd")
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}
defer collector.Stop()

// Process raw events
for event := range collector.Events() {
    // Raw syscall events in event.Data
    fmt.Printf("Etcd Event: %s\n", event.Type)
}
```

## Event Types

The collector monitors:
- Write syscalls (potential WAL writes)
- Fsync syscalls (WAL persistence)
- Network events (if detected in syscalls)

All events include:
- Process information (PID, TID)
- Timestamp
- Raw event type (numeric, no interpretation)
- Raw data bytes (no parsing)

## eBPF Details

The collector uses tracepoints to monitor:
- `syscalls:sys_enter_write` - Write operations
- `syscalls:sys_enter_fsync` - Sync operations

Events are delivered via a ring buffer for efficiency. No business logic or operation parsing is performed - that's the pipeline's job.