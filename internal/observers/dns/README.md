# DNS Problem Observer

A DNS problem detector that tracks DNS failures and performance issues using eBPF with **actual DNS packet parsing**.

## What It Does

This observer detects DNS problems:
- ✅ **Slow queries** - Queries exceeding latency threshold with domain names
- ✅ **Timeouts** - DNS queries with no response (5 second scan)
- ⚠️ **NXDOMAIN** - Domain not found errors (detection stubbed)
- ⚠️ **SERVFAIL** - DNS server failures (detection stubbed)
- ⚠️ **REFUSED** - Query refused by server (detection stubbed)

## Architecture

**Negative Observer Pattern**: Only tracks problems, ignores normal operations.

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│ eBPF Probes │───▶│ DNS Problems │───▶│ Events Out  │
│ UDP traffic │    │   Detector   │    │ (failures)  │
└─────────────┘    └──────────────┘    └─────────────┘
```

## Configuration

```go
type Config struct {
    SlowQueryThresholdMs int  // default: 100ms
    TimeoutMs            int  // default: 5000ms
    OnlyProblems         bool // We're a negative observer
    EnableEBPF           bool // Linux: true, other: false
}
```

## Event Output

```go
{
    "event_id": "dns-problem-12345-1634567890",
    "type": "dns",
    "severity": "warning",
    "data": {
        "dns": {
            "query_name": "slow.service.local",
            "query_type": "A",
            "duration": "250ms",
            "error": true,
            "error_message": "Query took 250.00ms"
        },
        "process": {
            "pid": 12345,
            "command": "curl"
        }
    },
    "metadata": {
        "labels": {
            "observer": "dns",
            "problem_type": "slow"
        }
    }
}
```

## Implementation

- **Linux**: eBPF kprobes on `udp_sendmsg`/`udp_recvmsg`
- **Other platforms**: Mock problems for testing
- **Zero ML**: Simple threshold-based detection
- **Ring buffer**: 4MB for eBPF events
- **Active tracking**: Maps DNS queries to responses

## Files

- `observer.go` - Main observer with base functionality
- `observer_linux.go` - eBPF implementation for Linux
- `observer_fallback.go` - Mock implementation for testing
- `types.go` - DNS event types and problem definitions
- `config.go` - Configuration with sane defaults
- `bpf_src/dns.c` - eBPF C program for kernel-level monitoring

## No Fake Intelligence

This observer does **not** include:
- ❌ Machine learning baselines
- ❌ Complex statistics
- ❌ "Anomaly detection"
- ❌ Circuit breakers
- ❌ Learning engines

Just clean, simple DNS problem detection.