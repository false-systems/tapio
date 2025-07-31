# CNI Collector

Minimal Container Network Interface (CNI) collector using eBPF to monitor network namespace operations with zero business logic.

## Architecture

```
pkg/collectors/cni/
├── collector.go              # Minimal collector implementation
├── collector_ebpf.go         # Linux eBPF implementation
├── collector_noebpf.go       # Non-Linux stub
├── collector_test.go         # Unit tests
├── generate.go              # bpf2go generation
├── init.go                  # Registry integration
├── bpf/                     # eBPF programs
│   ├── cni_monitor.c        # Network namespace monitoring
│   └── vmlinux.h            # Kernel headers
└── cnimonitor_*.go/o        # Generated eBPF objects
```

## Features

- **Minimal Design**: Zero business logic, just raw event collection
- **eBPF-Based**: Monitors network namespace operations (setns, unshare)
- **Network Focus**: Tracks container network creation and changes
- **Low Overhead**: Efficient kernel-level monitoring
- **K8s Aware**: Designed for Kubernetes CNI monitoring

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/cni"

// Create minimal collector
collector, err := cni.NewCollector("cni")
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
    // Raw network namespace events in event.Data
    fmt.Printf("CNI Event: %s\n", event.Type)
}
```

## Event Types

The collector monitors:
- Network namespace creation (unshare with CLONE_NEWNET)
- Network namespace entry (setns)
- Network namespace exit

All events include:
- Process information (PID, comm)
- Network namespace ID
- Timestamp
- Event type

## eBPF Details

The collector uses tracepoints to monitor:
- `syscalls:sys_enter_setns` - Entering network namespaces
- `syscalls:sys_enter_unshare` - Creating new namespaces

Events are delivered via a ring buffer for efficiency.