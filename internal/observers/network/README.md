# Network Observer

## Overview

The Network Observer is a high-performance, eBPF-based network monitoring component that captures and analyzes network traffic at the kernel level. It provides deep visibility into network connections, protocol analysis, and application-layer insights with minimal overhead.

## Features

### Core Capabilities
- **L3-L4 Protocol Monitoring**: TCP, UDP, ICMP with full connection tracking
- **L7 Protocol Analysis**: HTTP/HTTPS, DNS, gRPC parsing and analysis
- **Connection Tracking**: Stateful tracking of all network connections
- **Kubernetes Integration**: Automatic pod and service enrichment
- **Performance Optimization**: Rate limiting, sampling, and efficient buffering
- **Zero-Copy Architecture**: eBPF ring buffer for high-throughput event streaming

### Protocol Support

#### Network Layer (L3)
- IPv4 and IPv6
- ICMP/ICMPv6

#### Transport Layer (L4)
- TCP with connection state tracking
- UDP with flow tracking

#### Application Layer (L7)
- HTTP/1.x request/response parsing
- DNS query/response correlation
- gRPC call tracking (HTTP/2)
- TLS metadata extraction (without decryption)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Network Observer                         │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ eBPF Manager │  │  L7 Parser   │  │ K8s Enricher │      │
│  │              │  │              │  │              │      │
│  │ - CO-RE      │  │ - HTTP       │  │ - Pod Meta   │      │
│  │ - Tracepoint │  │ - DNS        │  │ - Service    │      │
│  │ - Ring Buf   │  │ - gRPC       │  │ - Container  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Conn Tracker │  │ Rate Limiter │  │   Metrics    │      │
│  │              │  │              │  │              │      │
│  │ - Flow State │  │ - Token Buck │  │ - OTEL       │      │
│  │ - Lifecycle  │  │ - Sampling   │  │ - Stats      │      │
│  │ - Cleanup    │  │ - Throttle   │  │ - Export     │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

### Basic Configuration

```go
config := &Config{
    // General settings
    Name:          "network-observer",
    BufferSize:    10000,
    FlushInterval: 10 * time.Second,

    // Protocol enablement
    EnableIPv4: true,
    EnableIPv6: true,
    EnableTCP:  true,
    EnableUDP:  true,

    // L7 parsing
    EnableL7Parse: true,
    HTTPPorts:    []int{80, 8080, 3000},
    DNSPort:      53,

    // Performance tuning
    MaxEventsPerSecond: 10000,
    SamplingRate:       1.0,
}
```

### Advanced Configuration

```go
config := &Config{
    // Connection tracking
    MaxConnections:            65536,
    ConnectionTimeout:         5 * time.Minute,
    ConnectionCleanupInterval: 30 * time.Second,

    // eBPF settings
    EnableCORE:      true,
    RingBufferSize:  8 * 1024 * 1024, // 8MB
    VerifierLogSize: 64 * 1024 * 1024, // 64MB

    // Features
    EnableK8sEnrichment: true,
    EnableFlowTracking:  true,

    // Mock mode for development
    MockMode: os.Getenv("TAPIO_MOCK_MODE") == "true",
}
```

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "log"

    "github.com/yairfalse/tapio/internal/observers/network"
    "go.uber.org/zap"
)

func main() {
    // Create logger
    logger, _ := zap.NewProduction()

    // Create observer
    observer, err := network.NewObserver(
        "network-observer",
        network.DefaultConfig(),
        logger,
    )
    if err != nil {
        log.Fatal(err)
    }

    // Start observing
    ctx := context.Background()
    if err := observer.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer observer.Stop()

    // Process events
    for event := range observer.Events() {
        // Handle network event
        if event.EventData.Network != nil {
            log.Printf("Network event: %s %s:%d -> %s:%d",
                event.EventData.Network.Protocol,
                event.EventData.Network.SrcIP,
                event.EventData.Network.SrcPort,
                event.EventData.Network.DstIP,
                event.EventData.Network.DstPort,
            )
        }
    }
}
```

### With Custom Configuration

```go
config := network.DefaultConfig()
config.EnableHTTPS = true
config.HTTPSPorts = append(config.HTTPSPorts, 8443)
config.SamplingRate = 0.1 // Sample 10% of events
config.MaxEventsPerSecond = 50000

observer, err := network.NewObserver("custom-network", config, logger)
```

## Event Structure

### Network Event

```go
type NetworkEvent struct {
    EventID   string
    Timestamp time.Time
    EventType string // "connection", "close", "http_request", etc.

    // Process info
    PID     uint32
    Command string

    // Network info
    Protocol  string // "TCP", "UDP", "ICMP"
    SrcIP     net.IP
    DstIP     net.IP
    SrcPort   uint16
    DstPort   uint16
    Direction string // "inbound", "outbound"

    // Connection metrics
    BytesSent   uint64
    BytesRecv   uint64
    Latency     time.Duration

    // L7 data
    L7Protocol string
    HTTPData   *HTTPData
    DNSData    *DNSData

    // Kubernetes
    PodUID     string
    Kubernetes *KubernetesMetadata
}
```

## eBPF Programs

### Network Monitoring Programs

The observer uses CO-RE (Compile Once, Run Everywhere) eBPF programs:

1. **TCP Connection Tracking**
   - `tcp_connect`: Traces outbound TCP connections
   - `tcp_accept`: Traces inbound TCP connections
   - `tcp_close`: Traces connection closure

2. **UDP Flow Tracking**
   - `udp_send`: Traces UDP packet transmission
   - `udp_recv`: Traces UDP packet reception

3. **L7 Protocol Detection**
   - HTTP method detection
   - DNS query/response correlation
   - gRPC call tracking

### BPF Maps

- `active_connections`: LRU hash map for connection tracking
- `http_states`: HTTP parsing state per connection
- `l7_ports`: Port to protocol mapping configuration
- `network_events`: Ring buffer for event streaming

## Performance Considerations

### Resource Usage

- **Memory**: ~50-100MB for typical workloads
- **CPU**: <1% overhead with sampling
- **Kernel Memory**: 8MB ring buffer + map allocations

### Optimization Techniques

1. **Sampling**: Configurable event sampling (0.0-1.0)
2. **Rate Limiting**: Token bucket algorithm with configurable rate
3. **Connection Limits**: LRU eviction for connection tracking
4. **Batch Processing**: Event batching for efficiency

### Benchmarks

```
Network Events Processing:
- Events/sec: 100,000+
- Latency p50: <100µs
- Latency p99: <1ms
- Memory usage: 50MB base + 1KB per connection
```

## Kubernetes Integration

### Automatic Enrichment

The observer automatically enriches events with:
- Pod name and namespace
- Service name and type
- Container ID
- Workload information (Deployment, StatefulSet, etc.)
- Pod labels and annotations

### Requirements

- Running inside Kubernetes cluster
- Access to pod metadata via downward API
- Optional: RBAC permissions for service discovery

## Development

### Mock Mode

For development without eBPF support:

```bash
export TAPIO_MOCK_MODE=true
go run cmd/network-observer/main.go
```

### Testing

```bash
# Run unit tests
go test ./internal/observers/network/...

# Run with coverage
go test -cover ./internal/observers/network/...

# Run eBPF tests (requires root)
sudo go test -tags=ebpf ./internal/observers/network/...
```

### Building eBPF Programs

```bash
# Generate eBPF bytecode
cd internal/observers/network/bpf_src
make generate
```

## Troubleshooting

### Common Issues

1. **Permission Denied**
   - Solution: Run with `CAP_BPF` and `CAP_PERFMON` capabilities
   - Alternative: Run as root (not recommended for production)

2. **Verifier Errors**
   - Check kernel version (5.8+ recommended)
   - Increase verifier log size in configuration
   - Check for BTF support: `ls /sys/kernel/btf/vmlinux`

3. **High Memory Usage**
   - Reduce `MaxConnections` limit
   - Decrease `RingBufferSize`
   - Enable sampling with `SamplingRate < 1.0`

4. **Missing Events**
   - Check rate limiting configuration
   - Verify eBPF programs are attached: `bpftool prog list`
   - Check ring buffer for overflows

### Debug Mode

Enable debug logging:

```go
logger, _ := zap.NewDevelopment()
observer, _ := network.NewObserver("debug", config, logger)
```

## Security Considerations

1. **No TLS Decryption**: The observer does not decrypt TLS traffic
2. **Kernel Access**: Requires elevated privileges for eBPF
3. **Data Sensitivity**: May capture sensitive connection metadata
4. **Rate Limiting**: Prevents resource exhaustion attacks

## License

This component is part of the Tapio observability platform and follows the project's licensing terms.

## Contributing

See the main Tapio contributing guidelines. Key requirements:
- No `map[string]interface{}` - use typed structs
- Complete implementations only - no TODOs or stubs
- Minimum 80% test coverage
- All errors must be wrapped with context
- Follow the 5-level architecture hierarchy