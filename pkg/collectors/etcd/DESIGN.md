# etcd Collector Design

## Philosophy: "Control the Means of Production"

We observe etcd at the kernel level, not through its API. This gives us:
- Zero overhead on etcd itself
- Complete visibility into all operations
- Early detection of issues
- Understanding of actual behavior vs reported behavior

## Architecture

### Phase 1: Network Observation (Current)
- TC/XDP programs capture packets on ports 2379/2380
- Parse gRPC/HTTP2 frames in kernel
- Extract etcd protocol messages
- Minimal overhead, maximum visibility

### Phase 2: Syscall Interception (Next)
- Trace read/write/fsync syscalls from etcd process
- Monitor file operations on /var/lib/etcd
- Capture WAL writes and snapshot operations
- Correlate with network operations

### Phase 3: Full Context (Future)
- Join network + syscall data
- Build complete operation timeline
- Detect split-brain scenarios
- Identify performance bottlenecks

## Implementation Details

### eBPF Programs

1. **TC Ingress/Egress**
   - Attached to network interfaces
   - Filters etcd ports (2379, 2380)
   - Extracts first 256 bytes of payload
   - Minimal CPU overhead

2. **Syscall Tracepoints**
   - sys_enter_write/read/fsync
   - Filtered by etcd PID
   - Captures file descriptors and sizes
   - Tracks I/O patterns

3. **Kprobes**
   - vfs_write/vfs_read
   - Extracts file paths
   - Monitors /var/lib/etcd operations
   - Tracks WAL and snapshot files

### Data Flow

```
Network Packet → TC eBPF → Parse → Event
                    ↓
                Perf Buffer
                    ↓
              Go Collector → RawEvent → Pipeline
                    ↑
                Perf Buffer
                    ↑
Syscall → Tracepoint eBPF → Event
```

### Event Correlation

Events are correlated by:
- Timestamp (nanosecond precision)
- Client IP/Port
- Operation key
- Request ID (if available in payload)

## Performance Considerations

1. **Ring Buffer vs Perf Buffer**
   - Using perf buffer for compatibility
   - Consider ring buffer for newer kernels
   - Batching for efficiency

2. **Payload Parsing**
   - Only first 256 bytes captured
   - Full parsing done in userspace
   - Enough for key and operation type

3. **CPU Overhead**
   - TC: ~50ns per packet
   - Tracepoint: ~100ns per syscall
   - Negligible impact on etcd

## Security Benefits

1. **Unauthorized Access Detection**
   - See all connection attempts
   - Track unusual access patterns
   - Identify potential attacks

2. **Data Exfiltration**
   - Monitor large read operations
   - Track key access patterns
   - Detect bulk exports

3. **Performance Attacks**
   - Identify expensive queries
   - Detect intentional slowdowns
   - Track resource exhaustion

## Future Enhancements

1. **In-Kernel Protocol Parsing**
   - Parse etcd/gRPC protocol in eBPF
   - Extract keys and values directly
   - Reduce userspace processing

2. **Raft Protocol Monitoring**
   - Track leader elections
   - Monitor consensus operations
   - Detect split-brain early

3. **Anomaly Detection**
   - Learn normal access patterns
   - Detect deviations in kernel
   - Real-time alerting

4. **Performance Histograms**
   - Latency distribution in kernel
   - No sampling needed
   - Accurate percentiles

## Configuration

```yaml
collectors:
  etcd:
    enabled: true
    mode: ebpf  # or "api" for fallback
    ebpf:
      capture_payload: true
      payload_size: 256
      trace_syscalls: true
      monitor_files: true
    ports:
      - 2379  # Client
      - 2380  # Peer
    pids: []  # Auto-detect or specify
    data_dir: /var/lib/etcd
```

## Comparison with API-based Collection

| Aspect | eBPF Collection | API Collection |
|--------|----------------|----------------|
| Overhead | Near zero | API calls add load |
| Visibility | Everything | Only what's logged |
| Latency | Nanoseconds | Milliseconds |
| Completeness | 100% | Sampled/filtered |
| Setup | Requires privileges | Simple |
| Maintenance | Kernel-dependent | API-dependent |

## Testing Strategy

1. **Unit Tests**
   - Mock eBPF maps
   - Test event parsing
   - Verify correlation logic

2. **Integration Tests**
   - Test against real etcd
   - Verify packet capture
   - Check syscall tracing

3. **Load Tests**
   - High packet rate
   - Many concurrent operations
   - Measure overhead

4. **Chaos Tests**
   - Network failures
   - Process crashes
   - Kernel module issues