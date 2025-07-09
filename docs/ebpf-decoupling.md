# eBPF Decoupling in Tapio

Tapio now supports building with or without eBPF functionality, making it flexible for different deployment scenarios.

## Overview

The eBPF functionality has been decoupled from the main utility through:
- A pluggable interface design
- Build tags for conditional compilation
- Platform-specific implementations

## Building Tapio

### Without eBPF (Default)
```bash
# Standard build - works on all platforms
make build

# Or directly with go
go build ./cmd/tapio
```

### With eBPF Support (Linux Only)
```bash
# Build with eBPF support
make build-ebpf

# Or directly with go
go build -tags ebpf ./cmd/tapio
```

## Architecture

### Interface Design
The eBPF functionality is abstracted behind the `ebpf.Monitor` interface:

```go
type Monitor interface {
    Start(ctx context.Context) error
    Stop() error
    GetMemoryStats() (map[uint32]*ProcessMemoryStats, error)
    GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)
    IsAvailable() bool
    GetLastError() error
}
```

### Implementations

1. **Linux with eBPF** (`monitor_linux.go`)
   - Full eBPF functionality
   - Requires root or CAP_BPF capability
   - Provides kernel-level memory tracking

2. **Stub Implementation** (`stub.go`)
   - Used on non-Linux platforms or when eBPF is disabled
   - Returns `ErrNotSupported` for all operations
   - Allows Tapio to run without eBPF dependencies

## Using eBPF in Tapio

### Prometheus Exporter
Enable eBPF monitoring in the Prometheus exporter:

```bash
# With eBPF support (requires root)
sudo tapio prometheus --enable-ebpf

# Without eBPF (default)
tapio prometheus
```

### Configuration
Create a checker with eBPF configuration:

```go
ebpfConfig := &ebpf.Config{
    Enabled:         true,
    EventBufferSize: 1000,
    RetentionPeriod: "5m",
}

checker, err := simple.NewCheckerWithConfig(ebpfConfig)
```

## Benefits of Decoupling

1. **Portability**: Tapio can run on any platform without eBPF dependencies
2. **Flexibility**: Choose whether to use eBPF based on your needs
3. **Security**: Run without elevated privileges when eBPF is not needed
4. **Simplicity**: Easier installation and deployment for basic use cases

## When to Use eBPF

Enable eBPF when you need:
- Kernel-level memory tracking
- Accurate OOM predictions
- Real-time process monitoring
- Enhanced metrics for Prometheus

## Requirements for eBPF

- Linux kernel 4.14+ (5.4+ recommended)
- Root access or CAP_BPF capability
- libbpf and kernel headers installed

## Troubleshooting

If eBPF fails to start:
1. Check permissions: `sudo tapio prometheus --enable-ebpf`
2. Verify kernel support: `uname -r` (should be 4.14+)
3. Install dependencies: `sudo apt-get install libbpf-dev linux-headers-$(uname -r)`
4. Check error message: Tapio will show why eBPF couldn't start