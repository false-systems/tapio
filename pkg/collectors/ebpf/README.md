# eBPF Collector

The eBPF collector provides deep system observability using eBPF (Extended Berkeley Packet Filter) technology on Linux systems.

## Architecture

This module follows the Tapio 5-level dependency hierarchy:

```
pkg/collectors/ebpf/
├── go.mod                    # Independent module
├── core/                     # Public interfaces and types
│   ├── interfaces.go         # Collector contracts
│   ├── types.go             # eBPF-specific types
│   └── errors.go            # Error definitions
├── internal/                # Internal implementation
│   ├── collector.go         # Main collector logic
│   ├── processor.go         # Event processing
│   └── platform_*.go        # Platform-specific factories
├── linux/                   # Linux-specific eBPF implementation
│   └── implementation.go    # Actual eBPF functionality
├── stub/                    # Stub for non-Linux platforms
│   └── implementation.go    # Returns appropriate errors
└── collector.go             # Public API exports
```

## Features

- **Memory Monitoring**: Track memory allocations, OOM events, and pressure
- **Process Tracking**: Monitor process lifecycle, syscalls, and behavior
- **Network Observability**: Connection tracking, packet analysis, and latency
- **File System Events**: File operations, access patterns, and I/O

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/ebpf"

// Create collector with default config
config := ebpf.DefaultConfig()
collector, err := ebpf.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    // Events are domain.Event types
    fmt.Printf("Event: %+v\n", event)
}

// Check health
health := collector.Health()
fmt.Printf("Collector health: %s\n", health.Status)

// Stop collection
collector.Stop()
```

## Platform Support

- **Linux**: Full eBPF functionality (requires kernel 4.9+)
- **Other platforms**: Returns appropriate error indicating eBPF is not supported

## Requirements

- Linux kernel 4.9+ with eBPF support
- CAP_SYS_ADMIN capability or root privileges
- Sufficient locked memory limit (handled automatically)

## Configuration

```go
config := ebpf.Config{
    Name:            "my-ebpf-collector",
    Enabled:         true,
    EventBufferSize: 2000,
    
    // Feature toggles
    EnableNetwork: true,
    EnableMemory:  true,
    EnableProcess: true,
    EnableFile:    false,
    
    // Performance tuning
    RingBufferSize:   16384,
    EventRateLimit:   5000,
    SamplingInterval: time.Second,
}
```

## Building

This module can be built independently:

```bash
cd pkg/collectors/ebpf
go build ./...
go test ./...
```

## Testing

Run tests with:

```bash
go test -v ./...
```

For Linux-specific tests (requires root):

```bash
sudo go test -v ./linux/...
```