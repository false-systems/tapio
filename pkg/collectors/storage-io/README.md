# Storage I/O Collector

The storage-io collector monitors VFS-level storage I/O operations in Kubernetes environments using eBPF. It focuses on detecting storage performance issues that can impact application performance.

## Features

### Phase 1 Implementation (Current)

- **VFS Layer Monitoring**: Tracks critical I/O operations at the VFS layer
  - `vfs_read` - File read operations with latency tracking
  - `vfs_write` - File write operations with latency tracking  
  - `vfs_fsync` - File synchronization operations (critical for performance)
  - `iterate_dir` - Directory listing operations (ConfigMap/Secret access)

- **Kubernetes Focus**: Monitors K8s-relevant mount points only
  - `/var/lib/kubelet/pods/` - Pod volumes
  - `/var/lib/kubelet/plugins/` - CSI plugins
  - `/var/lib/docker/containers/` - Docker containers
  - `/var/lib/containerd/` - Containerd containers
  - `/var/log/containers/` - Container logs
  - `/etc/kubernetes/` - K8s configuration
  - `/var/lib/etcd/` - etcd data

- **Performance Classification**:
  - Slow I/O detection (>10ms threshold by default)
  - I/O size classification (small/medium/large/huge)
  - Critical path detection for K8s components
  - Latency classification (fast/normal/slow/critical)

- **Rich Event Context**:
  - Process information (PID, command, cgroup)
  - File system details (device, inode, mount point)
  - Kubernetes correlation (pod UID, volume type, container ID)
  - Performance impact metrics (CPU time, queue time, block time)

## Configuration

```go
config := &Config{
    BufferSize:        10000,           // Event buffer size
    SlowIOThresholdMs: 10,              // Slow I/O threshold in milliseconds
    SamplingRate:      0.1,             // 10% sampling for non-K8s paths
    
    // VFS probe configuration
    EnableVFSRead:       true,
    EnableVFSWrite:      true,
    EnableVFSFsync:      true,
    EnableVFSIterateDir: true,
    
    // K8s volume type monitoring
    MonitorPVCs:       true,
    MonitorConfigMaps: true,
    MonitorSecrets:    true,
    MonitorHostPaths:  true,
    MonitorEmptyDirs:  true,
    
    // Correlation settings
    EnableCgroupCorrelation: true,
    EnableContainerCorrelation: true,
}
```

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/storage-io"

// Create collector
collector, err := storageio.NewCollector("storage-io", config)
if err != nil {
    log.Fatal(err)
}

// Start monitoring
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    storageData, ok := event.GetStorageIOData()
    if !ok {
        continue
    }
    
    if storageData.SlowIO {
        log.Printf("Slow I/O detected: %s on %s (%dms)",
            storageData.Operation,
            storageData.Path,
            storageData.Duration.Milliseconds())
    }
}
```

## Event Output

The collector emits `domain.CollectorEvent` with `StorageIOData` containing:

```go
type StorageIOData struct {
    Operation    string        // read, write, fsync, iterate_dir
    Path         string        // file/directory path
    Size         int64         // bytes read/written
    Duration     time.Duration // operation latency
    SlowIO       bool          // >10ms threshold
    BlockedIO    bool          // blocked operation
    
    // File system details
    Device       string        // block device
    Inode        uint64        // inode number
    FileSystem   string        // fs type (ext4, xfs, etc.)
    MountPoint   string        // mount point
    
    // Kubernetes correlation
    VolumeType   string        // pvc, configmap, secret, hostpath
    ContainerID  string        // container correlation
    PodUID       string        // pod correlation
    
    // Process context
    PID          int32         // process ID
    Command      string        // process command
    CgroupID     uint64        // cgroup correlation
    
    // Performance metrics
    CPUTime      time.Duration // CPU time consumed
    QueueTime    time.Duration // time in I/O queue
    BlockTime    time.Duration // time blocked
}
```

## Performance

- **Low Overhead**: Uses eBPF ring buffer for efficient event collection
- **Smart Sampling**: 100% sampling for K8s volumes, 10% for other paths
- **Filtered Processing**: Only processes relevant paths and slow operations
- **Memory Efficient**: Bounded event buffers with overflow protection

## Requirements

- **Linux kernel 5.8+**: For BPF ring buffer support
- **CO-RE support**: Kernel must have BTF (Build-Time Format) enabled
- **Root privileges**: Required for eBPF program loading
- **Kubernetes environment**: Designed for K8s clusters

## Monitoring Integration

The collector provides OpenTelemetry metrics:

- `storage_io_events_processed_total` - Total events processed
- `storage_io_slow_operations_total` - Slow I/O operations detected
- `storage_io_latency_ms` - I/O operation latency distribution
- `storage_io_vfs_operations_total` - VFS operations by type
- `storage_io_k8s_volume_operations_total` - K8s volume operations

## Architecture Compliance

- **Level 1**: Depends only on `pkg/domain` (follows 5-level hierarchy)
- **Type Safety**: No `map[string]interface{}` usage
- **Error Handling**: Comprehensive error handling with context
- **Resource Management**: Proper cleanup with defer patterns
- **Concurrency**: Race-free with proper synchronization

## Future Phases

- **Phase 2**: Container correlation via CRI integration
- **Phase 3**: Performance anomaly detection with ML
- **Phase 4**: Storage QoS recommendations
- **Phase 5**: Integration with storage orchestrators