# eBPF Collector

The eBPF collector provides deep kernel-level observability using eBPF (Extended Berkeley Packet Filter) technology, capturing system events with minimal overhead and unprecedented visibility into system behavior.

## Quick Start

```go
import "github.com/yairfalse/tapio/pkg/collectors/ebpf"

// Create and start collector
config := ebpf.DefaultConfig()
collector, err := ebpf.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    fmt.Printf("Event: %+v\n", event)
}
```

## Key Features

- **Dual-Path Architecture**: Preserves raw kernel events while enabling semantic correlation
- **Kernel-Level Visibility**: Direct observation of syscalls, network packets, and kernel events  
- **Zero-Copy Performance**: Ring buffer maps for efficient event transfer
- **Adaptive Sampling**: Intelligent rate limiting based on event importance
- **Real-Time Streaming**: Bidirectional gRPC streaming to Tapio server
- **Multi-Category Events**: Network, Process, File, Security, Container, and Memory events

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    eBPF Collector                        │
├─────────────────────────────────────────────────────────┤
│  Kernel Space: BPF Programs → Ring Buffers              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─    │
│  User Space:                                             │
│    ┌────────────┐     ┌─────────────┐                  │
│    │Event Filter│────▶│Event Enricher│                  │
│    └────────────┘     └─────────────┘                  │
│           │                   │                          │
│           ▼                   ▼                          │
│    ┌─────────────┐    ┌──────────────┐   ┌────────────┐│
│    │ Raw Storage │    │Semantic Layer│───▶│Correlation ││
│    └─────────────┘    └──────────────┘   └────────────┘│
└─────────────────────────────────────────────────────────┘
```

## Module Structure

```
pkg/collectors/ebpf/
├── core/                  # Public interfaces
├── internal/              # Internal implementation  
├── linux/                 # Linux eBPF programs
├── stub/                  # Non-Linux stubs
├── types.go              # Event type definitions
├── filter.go             # Event filtering logic
├── enricher.go           # Context enrichment
├── processor.go          # Dual-path processor
├── tapio_client.go       # gRPC integration
└── raw_event_formatter.go # Human-readable formatting
```

## Requirements

- Linux kernel 4.18+ (5.8+ recommended)
- BPF/BTF support enabled
- CAP_SYS_ADMIN capability
- Go 1.24+ for building

## Documentation

For comprehensive documentation including:
- Detailed architecture and design
- Installation and deployment guides
- Configuration reference
- Security best practices
- Performance tuning
- Troubleshooting guides
- API reference

See: [📚 **eBPF Collector Documentation**](/docs/collectors/ebpf.md)

## Quick Examples

### High-Performance Configuration
```go
config := ebpf.HighPerformanceConfig()
collector, _ := ebpf.NewCollector(config)
```

### Security-Focused Monitoring
```go
config := ebpf.SecurityConfig()
collector, _ := ebpf.NewCollector(config)
```

### Dual-Path Processing
```go
processor := ebpf.NewDualPathProcessor(
    ebpf.WithRawStorage("/var/lib/tapio/raw"),
    ebpf.WithSemanticFiltering(0.8),
    ebpf.WithCorrelation(tapioClient),
)
```

## Building

```bash
cd pkg/collectors/ebpf
go build ./...
go test ./...
```

## License

See [LICENSE](../../../LICENSE) for details.