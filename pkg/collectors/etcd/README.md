# Etcd eBPF Collector

Zero-overhead etcd monitoring at the kernel level using eBPF.

## Overview

This collector observes etcd without any API calls or instrumentation by:
- Capturing network packets on ports 2379/2380
- Tracing syscalls from etcd processes
- Monitoring file operations on /var/lib/etcd

## Architecture

```
┌─────────────────┐
│   etcd process  │
└────────┬────────┘
         │
    ╔════╧════╗
    ║ KERNEL  ║     ← eBPF programs attached here
    ╚════╤════╝
         │
┌────────┴────────┐
│ eBPF Collector  │
└────────┬────────┘
         │
┌────────┴────────┐
│    RawEvent     │
└─────────────────┘
```

## What We Capture

### Network Level (TC/XDP)
- All gRPC operations on port 2379 (client)
- Peer communication on port 2380
- Operation type, key, latency

### Syscall Level
- write() calls showing actual persistence
- fsync() for WAL operations
- Correlates with network operations for full picture

### Example Event
```json
{
  "timestamp": 1234567890,
  "type": "etcd.put",
  "data": {
    "operation": "put",
    "key": "/registry/pods/default/nginx",
    "value_size": 2048,
    "latency_ms": 15,
    "capture_point": "network",
    "src_ip": "10.0.1.5",
    "dst_ip": "10.0.1.10",
    "pid": 12345
  }
}
```

## Implementation Details

### eBPF Programs
1. **TC program**: Captures network packets, parses gRPC frames
2. **Tracepoint programs**: Monitor syscalls from etcd processes
3. **Ring buffer**: Efficient event passing to userspace

### Performance
- Zero overhead to etcd
- Ring buffer for high-throughput events
- CO-RE for portability across kernels

## Future Enhancements
1. Full gRPC/HTTP2 frame parsing
2. Raft protocol visibility
3. Snapshot operations tracking
4. Leader election detection
5. Split-brain detection at kernel level