# Network Observer

## Overview

The Network Observer tracks TCP connections, UDP traffic, and DNS queries using eBPF kernel probes. It demonstrates the consolidated observer pattern built on Tapio's base observer infrastructure.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     Network Observer                          │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐     │
│  │ eBPF Probes │───▶│ Ring Buffer │───▶│  Pipeline   │     │
│  └─────────────┘    └─────────────┘    └─────────────┘     │
│         │                                       │            │
│         │ tcp_connect (kprobe)                 │            │
│         │ udp_sendmsg (kprobe)                 │            │
│         │ DNS filter (TC - optional)            │            │
│         │                                       │            │
│  ┌──────▼─────────────────────────────────────▼─────┐       │
│  │           BaseObserver Infrastructure             │       │
│  │  - Lifecycle (Start/Stop/IsHealthy)              │       │
│  │  - Metrics (OTEL counters/histograms)            │       │
│  │  - Pipeline (errgroup stages)                     │       │
│  └──────────────────────────────────────────────────┘       │
│                          │                                    │
│                          ▼                                    │
│  ┌──────────────────────────────────────────────────┐       │
│  │              Multi-Output Emitters                │       │
│  │  - OTEL (Grafana/Prometheus/Jaeger)              │       │
│  │  - Tapio (UKKO ecosystem)                         │       │
│  │  - Stdout (debugging)                             │       │
│  └──────────────────────────────────────────────────┘       │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

## Features

### Protocol Support
- **TCP**: Connection tracking via `tcp_connect` kprobe
- **UDP**: Datagram tracking via `udp_sendmsg` kprobe
- **DNS**: Query/response tracking (optional TC filter on port 53)

### Event Data
Each network event captures:
- Source/destination IP addresses
- Source/destination ports
- Protocol type (TCP/UDP)
- Process ID and name
- Timestamp (nanosecond precision)
- Network namespace

### Output Modes
Configure via `OutputConfig`:
- **OTEL**: Export as OpenTelemetry spans (production observability)
- **Tapio**: Send to UKKO ecosystem via channel (correlation engine)
- **Stdout**: JSON lines for debugging

## Usage

### Basic Usage
```go
import (
    "context"
    "github.com/yairfalse/tapio/internal/base"
    "github.com/yairfalse/tapio/internal/observers/network"
)

// Create observer with OTEL output
config := network.Config{
    Output: base.OutputConfig{
        OTEL:   true,
        Tapio:  false,
        Stdout: false,
    },
}

observer, err := network.NewNetworkObserver("network-observer", config)
if err != nil {
    log.Fatalf("failed to create observer: %v", err)
}

ctx := context.Background()
if err := observer.Start(ctx); err != nil {
    log.Fatalf("failed to start observer: %v", err)
}
defer observer.Stop()
```

### Multi-Output Configuration
```go
// Enable all outputs for development
config := network.Config{
    Output: base.OutputConfig{
        OTEL:   true,  // Production metrics
        Tapio:  true,  // Correlation engine
        Stdout: true,  // Debug logs
    },
}
```

### Standalone Mode (No UKKO Dependency)
```go
// Minimal standalone observer
config := network.Config{
    Output: base.OutputConfig{
        OTEL: true,
    },
}
// Observer works independently, exports only to OTEL backend
```

## Requirements

### Linux Kernel
- **Minimum**: Linux 5.8+ (eBPF features)
- **Recommended**: Linux 5.15+ (improved eBPF stability)
- **Capabilities**: `CAP_BPF` and `CAP_SYS_ADMIN`

### Development Environment
```bash
# Ubuntu 24.04 (recommended)
sudo apt-get install -y \
    clang-18 \
    llvm-18 \
    libbpf-dev \
    linux-headers-generic

# Or use Docker dev container
make docker-dev
```

### Mac Development
```bash
# Use Colima for local eBPF testing
colima start --mount $HOME/tapio:w
colima ssh
cd /tapio && sudo go run ./cmd/observers
```

## Event Schema

### ObserverEvent Structure
```go
type ObserverEvent struct {
    ID        string    `json:"id"`
    Type      string    `json:"type"`      // "tcp_connect", "udp_send", "dns_query"
    Source    string    `json:"source"`    // "network-observer"
    Timestamp time.Time `json:"timestamp"`

    NetworkData *NetworkEventData `json:"network_data"`
    ProcessData *ProcessEventData `json:"process_data"`
}

type NetworkEventData struct {
    Protocol string `json:"protocol"`      // "TCP", "UDP"
    SrcIP    string `json:"src_ip"`        // "10.0.1.5"
    DstIP    string `json:"dst_ip"`        // "10.0.2.10"
    SrcPort  uint16 `json:"src_port"`      // 45678
    DstPort  uint16 `json:"dst_port"`      // 443
}

type ProcessEventData struct {
    PID         uint32 `json:"pid"`
    ProcessName string `json:"process_name"`
    CommandLine string `json:"command_line,omitempty"`
}
```

### Example Event (Stdout Output)
```json
{
  "id": "net-1728392847-001",
  "type": "tcp_connect",
  "source": "network-observer",
  "timestamp": "2025-10-08T12:34:07.123456789Z",
  "network_data": {
    "protocol": "TCP",
    "src_ip": "10.0.1.5",
    "dst_ip": "142.250.80.46",
    "src_port": 54321,
    "dst_port": 443
  },
  "process_data": {
    "pid": 12345,
    "process_name": "curl"
  }
}
```

## Metrics

### OTEL Metrics Exported
All metrics follow OpenTelemetry naming conventions:

```
observer_events_processed_total{observer="network-observer", type="tcp_connect"}
observer_events_processed_total{observer="network-observer", type="udp_send"}
observer_events_dropped_total{observer="network-observer"}
observer_errors_total{observer="network-observer"}
observer_processing_duration_ms{observer="network-observer"}
```

### Prometheus Scrape Example
```promql
# Connection rate
rate(observer_events_processed_total{type="tcp_connect"}[5m])

# Drop rate
rate(observer_events_dropped_total[5m])

# Processing latency (p95)
histogram_quantile(0.95, observer_processing_duration_ms)
```

## Testing

### Test Coverage
The observer includes 6 test types per CLAUDE.md standards:

1. **observer_unit_test.go** - Unit tests for individual methods
2. **observer_e2e_test.go** - End-to-end workflow tests
3. **observer_integration_test.go** - Real eBPF program tests
4. **observer_system_test.go** - Linux system-level tests
5. **observer_performance_test.go** - Benchmarks and load tests
6. **observer_negative_test.go** - Error handling tests

### Running Tests
```bash
# Unit tests (Mac compatible)
go test ./internal/observers/network -v

# Integration tests (requires Linux)
make docker-test

# Performance benchmarks
go test ./internal/observers/network -bench=. -benchmem

# With race detector
go test ./internal/observers/network -race
```

## Performance

### Benchmarks (Target)
- **Event processing**: < 50μs per event
- **Ring buffer overhead**: < 5% CPU
- **Memory usage**: < 10MB baseline
- **Throughput**: 10,000+ events/sec on modern hardware

### Optimization Notes
- Ring buffer sized for 4096 events (configurable)
- Zero-copy event reading from eBPF
- Atomic counters for lock-free statistics
- Preallocated event pool for hot path

## Troubleshooting

### Permission Denied
```
Error: failed to load eBPF program: operation not permitted
```
**Solution**: Run with required capabilities:
```bash
sudo setcap cap_bpf,cap_sys_admin+ep ./observer
# Or run as root
sudo go run ./cmd/observers
```

### eBPF Program Load Failed
```
Error: invalid eBPF program: verifier rejected
```
**Solution**: Check kernel version and eBPF features:
```bash
# Kernel version should be 5.8+
uname -r
# /sys/kernel/btf/vmlinux should exist for CO-RE
ls /sys/kernel/btf/vmlinux
```

### Ring Buffer Full (Events Dropped)
```
observer_events_dropped_total increasing
```
**Solution**: Increase ring buffer size or reduce event generation:
```go
config := network.Config{
    RingBufferSize: 8192, // Default: 4096
}
```

### No Events Appearing
1. Check observer is running: `observer.IsHealthy()` returns true
2. Verify eBPF probes attached: `sudo bpftool prog list`
3. Generate test traffic: `curl https://example.com`
4. Check emitter configuration: `OutputConfig` has at least one output enabled

## Architecture Compliance

### CLAUDE.md Standards
- ✅ NO `map[string]interface{}` - All typed structs
- ✅ NO TODOs or stubs - Complete implementation
- ✅ Direct OTEL imports - No wrapper abstractions
- ✅ 80%+ test coverage - All 6 test types
- ✅ Proper error handling - Context wrapping
- ✅ Resource cleanup - defer and error checks

### Dependency Hierarchy
```
Level 0: pkg/domain/           # Event schemas (ZERO deps)
Level 1: internal/base/         # Observer infrastructure
Level 2: internal/observers/network/  # This package (depends on base + domain)
```

## Integration with Tapio Ecosystem

### Standalone Mode
Network observer works independently with just OTEL backend:
```go
config.Output.OTEL = true  // Only OTEL enabled
// No UKKO/NATS/correlation dependencies
```

### UKKO Ecosystem Mode
Enable Tapio emitter for full ecosystem integration:
```go
config.Output.OTEL = true   // Metrics/traces to Grafana
config.Output.Tapio = true  // Events to correlation engine

// Events flow to UKKO via TapioEmitter channel
tapioEmitter := emitter.(*base.MultiEmitter).GetTapioEmitter()
go func() {
    for event := range tapioEmitter.Events() {
        // UKKO processing: correlation, enrichment, etc.
    }
}()
```

## Future Enhancements

### Planned Features
- [ ] DNS query/response correlation
- [ ] HTTP/2 and gRPC tracking
- [ ] Connection duration tracking
- [ ] Retransmission detection
- [ ] Network namespace filtering

### Experimental
- [ ] XDP (eXpress Data Path) support for line-rate processing
- [ ] TCP state machine tracking
- [ ] TLS handshake inspection

## References

- [eBPF Documentation](https://ebpf.io/)
- [cilium/ebpf Library](https://github.com/cilium/ebpf)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Tapio Architecture](../../CLAUDE.md)
