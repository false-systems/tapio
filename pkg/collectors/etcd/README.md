# Etcd Collector

The etcd collector monitors etcd clusters for comprehensive operational insights using both high-level client monitoring and low-level eBPF observability.

## Architecture

The collector uses a two-tier approach:

1. **Basic Collector**: Uses etcd client library to monitor cluster operations
2. **eBPF Collector** (Linux only): Kernel-level monitoring for deep insights

## Features

### Basic Monitoring
- **Watch Operations**: Real-time monitoring of key-value changes
- **Cluster Status**: Health, leader election, member status
- **Performance Metrics**: Database size, raft index/term tracking
- **Event Correlation**: Links operations to keys and revisions

### eBPF Enhancement (Linux)
- **Syscall Monitoring**: Track write/fsync operations for WAL persistence
- **Network Analysis**: Monitor client-server communication patterns
- **Zero Configuration**: Automatically attaches to etcd processes
- **High Performance**: Ring buffer for efficient event streaming

## Configuration

```go
config := collectors.CollectorConfig{
    BufferSize: 1000,
    Labels: map[string]string{
        "etcd_endpoints": "localhost:2379,localhost:2380,localhost:2381",
        "etcd_username":  "monitoring_user",
        "etcd_password":  "secure_password",
        "cluster_name":   "production",
        "environment":    "prod",
    },
}
```

### Configuration Options

- `etcd_endpoints`: Comma-separated list of etcd endpoints (default: "localhost:2379")
- `etcd_username`: Authentication username (optional)
- `etcd_password`: Authentication password (optional)
- Additional labels are included in event metadata

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors"
    "github.com/yairfalse/tapio/pkg/collectors/etcd"
)

func main() {
    config := collectors.CollectorConfig{
        BufferSize: 1000,
        Labels: map[string]string{
            "etcd_endpoints": "localhost:2379",
            "cluster_name":   "my-cluster",
        },
    }
    
    collector, err := etcd.NewCollector(config)
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    if err := collector.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer collector.Stop()
    
    log.Printf("Monitoring etcd with %s collector", collector.Name())
    
    for event := range collector.Events() {
        fmt.Printf("Event: %s at %s\n", event.Type, event.Timestamp)
        fmt.Printf("  Key: %s\n", event.Metadata["key"])
        fmt.Printf("  Source: %s\n", event.Metadata["source"])
    }
}
```

## Event Types

### Watch Events
- `etcd.PUT`: Key creation/update operations
- `etcd.DELETE`: Key deletion operations

### Status Events  
- `etcd.status`: Periodic cluster health and metrics

### eBPF Events (Linux only)
- `etcd.get`: Key retrieval operations (from eBPF)
- `etcd.put`: Key write operations (from eBPF)
- `etcd.delete`: Key deletion operations (from eBPF)
- `etcd.watch`: Watch operations (from eBPF)
- `etcd.lease`: Lease operations (from eBPF)
- `etcd.txn`: Transaction operations (from eBPF)
- `etcd.wal_sync`: WAL sync operations (from eBPF)

## Event Data Structure

### Watch Events
```json
{
  "type": "PUT",
  "key": "/config/database/host",
  "value": "db.example.com",
  "create_revision": 12345,
  "mod_revision": 12346,
  "version": 2,
  "lease": 0,
  "prev_value": "old-db.example.com",
  "prev_mod_revision": 12340
}
```

### Status Events
```json
{
  "version": "3.5.9",
  "db_size": 2097152,
  "leader": 8234567890123456789,
  "raft_index": 12346,
  "raft_term": 5,
  "raft_applied_index": 12346
}
```

### eBPF Events
```json
{
  "timestamp": 1642678901234567890,
  "pid": 1234,
  "tid": 1234,
  "type": "syscall",
  "operation": "put",
  "latency_ms": 5,
  "key_size": 64,
  "value_size": 256,
  "key": "/config/database/host"
}
```

## Deployment Considerations

### Basic Collector
- Requires network access to etcd cluster
- Uses minimal resources
- Works on all platforms

### eBPF Collector  
- Linux only (kernel 4.18+)
- Requires CAP_BPF or root privileges
- Automatic fallback to basic collector if eBPF fails
- Provides deeper insights with minimal overhead

### Monitoring Production Clusters
- Use dedicated monitoring credentials with read-only access
- Configure appropriate buffer sizes for high-throughput clusters
- Consider network latency for multi-region deployments
- Monitor collector health and restart policies

## Troubleshooting

### Common Issues

1. **Connection Failed**: Check etcd endpoints and authentication
2. **eBPF Load Failed**: Verify Linux kernel version and permissions
3. **High Memory Usage**: Reduce buffer size or increase event processing speed
4. **Missing Events**: Check etcd watch permissions and network connectivity

### Debug Mode
Enable verbose logging by setting appropriate log levels in your application.

## Architecture Diagram

```
┌─────────────────┐
│   etcd cluster  │
│  (client:2379)  │
│  (peer:2380)    │
└────────┬────────┘
         │
    ╭────┴────╮
    │ Kernel  │     ← eBPF programs (Linux only)
    ╰────┬────╯
         │
┌────────┴────────┐
│ Etcd Collector  │
│  ┌───────────┐  │
│  │   Basic   │  │ ← etcd client watch + status
│  └───────────┘  │
│  ┌───────────┐  │
│  │   eBPF    │  │ ← syscall + network monitoring
│  └───────────┘  │
└────────┬────────┘
         │
┌────────┴────────┐
│   RawEvents     │
│ - etcd.PUT      │
│ - etcd.DELETE   │
│ - etcd.status   │
│ - etcd.wal_sync │
└─────────────────┘
```