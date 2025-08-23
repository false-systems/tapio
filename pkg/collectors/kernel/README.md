# Kernel Collector - Focused ConfigMap/Secret Access Monitoring

## Executive Summary

The Tapio Kernel Collector has been refactored from a general-purpose "god collector" into a focused, specialized collector that monitors **ConfigMap and Secret access patterns** in Kubernetes environments. This focused approach eliminates functional overlap with other specialized collectors while providing unique insights into configuration access security.

## Architecture Overview

### Design Philosophy

After identifying that network, storage I/O, process lifecycle, and memory monitoring are comprehensively covered by dedicated collectors, the kernel collector now focuses exclusively on:

- **ConfigMap Access Tracking**: Monitor when pods access mounted ConfigMaps
- **Secret Access Monitoring**: Track Secret mount access for security auditing  
- **Pod Correlation**: Provide cgroup ID extraction and pod attribution for other collectors
- **Configuration Security**: Detect unusual configuration access patterns

### Core Components

```
┌─────────────────────────────────────────────┐
│           User Space (Go)                   │
│                                              │
│  ┌─────────────┐    ┌──────────────┐       │
│  │  Collector  │───▶│ Ring Buffer  │       │
│  │   (Go)      │    │   Reader     │       │
│  └─────────────┘    └──────────────┘       │
│         ▲                  ▲                │
└─────────┼──────────────────┼────────────────┘
          │                  │
     ┌────┴───────┐    ┌────┴────┐
     │ Control    │    │  Data   │
     │ Maps       │    │  Path   │
     └────────────┘    └─────────┘
          ▲                  ▲
┌─────────┼──────────────────┼────────────────┐
│         │     Kernel Space │                │
│         │                  │                │
│  ┌──────┴──────┐    ┌─────┴──────┐         │
│  │ Config/Pod  │    │   Events   │         │
│  │ Correlation │    │ Ring Buffer│         │
│  │    Maps     │    │ (256KB)    │         │
│  └─────────────┘    └────────────┘         │
│         ▲                  ▲                │
│  ┌──────┴───────────────────┴──────┐       │
│  │     Focused eBPF Programs        │       │
│  │  - openat tracepoint             │       │
│  │  - Pod syscall correlation       │       │
│  └──────────────────────────────────┘       │
└──────────────────────────────────────────────┘
```

## Unique Events Captured

### ConfigMap Access Events
- **File Access**: `openat()` syscalls to ConfigMap mount paths
- **Mount Point Detection**: `/var/lib/kubelet/pods/{pod-uid}/volumes/kubernetes.io~configmap/`
- **ConfigMap Name Extraction**: Parse ConfigMap name from mount path
- **Access Correlation**: Link access to specific pod and namespace

### Secret Access Events  
- **Secure File Access**: `openat()` syscalls to Secret mount paths
- **Secret Mount Detection**: `/var/lib/kubelet/pods/{pod-uid}/volumes/kubernetes.io~secret/`
- **Secret Name Extraction**: Parse Secret name from mount path
- **Security Attribution**: Link access to pod for audit trails

### Pod Correlation Services
- **Cgroup ID Extraction**: Advanced multi-method cgroup ID extraction
- **Container PID Tracking**: Maintain PID-to-container mapping
- **Pod Information Storage**: Map cgroup IDs to pod metadata
- **Service Endpoint Mapping**: Correlation data for other collectors

## What This Collector Does NOT Monitor

To eliminate confusion and functional overlap, this collector **explicitly does not** monitor:

❌ **Network Events** - Handled by [Network Collector](../network/README.md)
❌ **Storage I/O Operations** - Handled by [Storage-IO Collector](../storage-io/README.md) 
❌ **Process Lifecycle** - Handled by [Syscall-Errors Collector](../syscall-errors/)
❌ **Memory Allocation** - Handled by [OOM Collector](../oom/README.md)
❌ **Container Runtime Events** - Handled by [CRI Collectors](../cri/)

## eBPF Programs Analysis

### Main Program: `kernel_monitor.c` (Focused)

**Size**: 506 lines (down from 861)
**Maps**: 8 BPF maps for correlation and state tracking  
**Ring Buffer**: 256KB (reduced from 512KB)
**Overhead**: ~50% reduction in resource usage

#### Key Features:

1. **ConfigMap/Secret Detection**
   - Monitors `sys_enter_openat` tracepoint
   - Filters paths matching Kubernetes volume patterns
   - Extracts ConfigMap/Secret names from mount paths

2. **Pod Correlation Infrastructure**
   - Maintains cgroup ID to pod UID mappings
   - Provides PID-to-container attribution
   - Exports correlation data for other collectors

3. **Security-Focused Monitoring**
   - Tracks who accesses which configurations
   - Provides audit trail for Secret access
   - Detects unusual configuration access patterns

#### Hook Points (Reduced):

```c
// ConfigMap/Secret access tracking
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_config_access(void *ctx)

// Pod correlation (sampled 1:100)  
SEC("tracepoint/raw_syscalls/sys_enter")
int trace_pod_syscalls(void *ctx)
```

## Performance Characteristics

### CPU Overhead (Improved)
- **Baseline**: <0.2% CPU usage (down from 0.5%)
- **Peak**: <1% CPU during high config access (down from 2%)
- **Sampling**: 1:100 syscalls for correlation only

### Memory Usage (Reduced)
- **Ring Buffer**: 256KB (down from 512KB)
- **BPF Maps**: ~1MB total (down from 2MB)
- **User Space**: ~5MB Go runtime (down from 10MB)

### Event Throughput (Focused)
- **Primary Events**: ConfigMap/Secret access only
- **Correlation Events**: Sampled syscalls for attribution
- **Latency**: <50μs from kernel to userspace
- **Drop Rate**: Near zero for focused event set

## Security & Audit Capabilities

### ConfigMap Access Monitoring
```json
{
  "event_type": "configmap_access",
  "timestamp": "2024-01-21T10:30:45Z",
  "pod_uid": "abc-123-def",
  "namespace": "production",
  "config_name": "app-config",
  "mount_path": "/var/lib/kubelet/pods/abc-123-def/volumes/kubernetes.io~configmap/app-config",
  "process": {
    "pid": 1234,
    "command": "nginx",
    "cgroup_id": 567890
  }
}
```

### Secret Access Monitoring
```json
{
  "event_type": "secret_access", 
  "timestamp": "2024-01-21T10:30:45Z",
  "pod_uid": "xyz-789-def",
  "namespace": "production",
  "config_name": "db-credentials",
  "mount_path": "/var/lib/kubelet/pods/xyz-789-def/volumes/kubernetes.io~secret/db-credentials",
  "process": {
    "pid": 5678,
    "command": "app-server",
    "cgroup_id": 123456
  }
}
```

## Integration with Other Collectors

### Pod Correlation Services

The kernel collector provides correlation infrastructure that other collectors can leverage:

```go
// Add container PID for tracking
err := kernelCollector.AddContainerPID(1234)

// Add pod information for correlation
podInfo := kernel.PodInfo{
    PodUID:    "abc-123-def",
    Namespace: "production", 
    PodName:   "nginx-deployment-xyz",
    CreatedAt: time.Now().Unix(),
}
err := kernelCollector.AddPodInfo(cgroupID, podInfo)

// Add ConfigMap/Secret mount info
mountInfo := kernel.MountInfo{
    Name:      "app-config",
    Namespace: "production",
    MountPath: "/var/lib/kubelet/pods/.../app-config",
    IsSecret:  false,
}
pathHash := hashPath(mountInfo.MountPath)
err := kernelCollector.AddMountInfo(pathHash, mountInfo)
```

### Event Enrichment

Other collectors can use kernel collector data for enrichment:

- **Network Collector**: Gets pod attribution via cgroup correlation
- **Storage-IO Collector**: Uses mount path correlation for volume identification
- **Syscall-Errors Collector**: Leverages PID-to-container mapping

## Deployment Instructions

### Kubernetes DaemonSet (Simplified)

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-kernel-collector
  namespace: tapio-system
spec:
  selector:
    matchLabels:
      app: tapio-kernel-collector
  template:
    metadata:
      labels:
        app: tapio-kernel-collector
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: kernel-collector
        image: tapio/kernel-collector:v2.0.0
        securityContext:
          privileged: true
          capabilities:
            add:
            - SYS_ADMIN  # Required for eBPF
        resources:
          requests:
            memory: "64Mi"   # Reduced from 128Mi
            cpu: "50m"       # Reduced from 100m
          limits:
            memory: "256Mi"  # Reduced from 512Mi  
            cpu: "200m"      # Reduced from 500m
        volumeMounts:
        - name: debugfs
          mountPath: /sys/kernel/debug
        - name: bpffs
          mountPath: /sys/fs/bpf
      volumes:
      - name: debugfs
        hostPath:
          path: /sys/kernel/debug
      - name: bpffs
        hostPath:
          path: /sys/fs/bpf
          type: DirectoryOrCreate
```

### Configuration Options

```yaml
# config.yaml
kernel_collector:
  name: "kernel-config-monitor"
  buffer_size: 1000            # Reduced from 10000
  enable_ebpf: true
  
  # Focus areas
  monitor_configmaps: true
  monitor_secrets: true
  enable_pod_correlation: true
  
  # Sampling (correlation only)
  syscall_sampling_rate: 100   # 1 in 100 syscalls
  
  # Paths to monitor
  kubelet_pods_path: "/var/lib/kubelet/pods"
  configmap_volume_pattern: "kubernetes.io~configmap"
  secret_volume_pattern: "kubernetes.io~secret"
```

## Use Cases & Benefits

### Security Audit Trail
- **Who accessed which Secrets?** - Complete audit trail with pod attribution
- **Unusual configuration access** - Detect processes accessing configs they shouldn't
- **Secret access patterns** - Identify which secrets are accessed most frequently

### Configuration Management
- **ConfigMap usage tracking** - Understand which configs are actually used
- **Dead configuration detection** - Find unused ConfigMaps/Secrets
- **Configuration blast radius** - See which pods are affected by config changes

### Compliance & Governance
- **Access logging** - Maintain records of configuration access for compliance
- **Principle of least privilege** - Identify over-privileged pods accessing unnecessary secrets
- **Configuration drift** - Detect when pods access configs outside their expected patterns

## Monitoring & Observability

### OpenTelemetry Metrics

```go
// Focused metrics
kernel_monitor_configmap_access_total{namespace="prod",configmap="app-config"}
kernel_monitor_secret_access_total{namespace="prod",secret="db-creds"}
kernel_monitor_pod_correlation_total{operation="cgroup_lookup"}
kernel_monitor_processing_duration_ms{event_type="configmap_access"}
```

### Health Checks

```go
GET /healthz
{
  "status": "healthy",
  "ebpf_loaded": true,
  "focus": "config_access",
  "ring_buffer_active": true,
  "events_processed": 1234
}
```

## Troubleshooting

### No ConfigMap/Secret Events

```bash
# Verify mount paths exist
ls -la /var/lib/kubelet/pods/*/volumes/kubernetes.io~*

# Check eBPF program attachment
sudo bpftool prog list | grep trace_config_access

# Monitor trace output
sudo cat /sys/kernel/debug/tracing/trace_pipe | grep openat
```

### Missing Pod Correlation

```bash
# Check container PID tracking
sudo bpftool map dump name container_pids

# Verify pod info mapping
sudo bpftool map dump name pod_info_map
```

## Migration from Previous Version

The kernel collector has been significantly refactored. If upgrading from a previous version:

### Breaking Changes

1. **Event Types Changed**: 
   - Old: `process_exec`, `network_connect`, `file_open`
   - New: `configmap_access`, `secret_access`, `pod_syscall`

2. **Reduced Event Volume**: ~90% fewer events due to focused monitoring

3. **New Configuration Schema**: Updated config structure for focused functionality

### Migration Steps

1. **Deploy Specialized Collectors First**:
   ```bash
   kubectl apply -f network-collector.yaml
   kubectl apply -f storage-io-collector.yaml  
   kubectl apply -f syscall-errors-collector.yaml
   ```

2. **Update Configuration**:
   ```bash
   kubectl apply -f kernel-collector-v2-config.yaml
   ```

3. **Deploy Updated Kernel Collector**:
   ```bash
   kubectl apply -f kernel-collector-v2.yaml
   ```

4. **Verify Focused Operation**:
   ```bash
   kubectl logs -f daemonset/tapio-kernel-collector | grep "configmap_access\|secret_access"
   ```

## Future Roadmap

### Phase 1: Enhanced ConfigMap/Secret Analysis
- [ ] Pattern-based anomaly detection for unusual access
- [ ] ConfigMap/Secret dependency mapping
- [ ] Access frequency analytics
- [ ] Dead configuration identification

### Phase 2: Security Enhancements  
- [ ] Secret access policy enforcement
- [ ] Real-time security alerts for policy violations
- [ ] Integration with Kubernetes RBAC analysis
- [ ] Compliance reporting for SOC2/PCI-DSS

### Phase 3: Advanced Correlation
- [ ] Cross-namespace secret sharing detection
- [ ] Configuration blast radius analysis  
- [ ] Automated least-privilege recommendations
- [ ] GitOps integration for config change correlation

## Architecture Compliance

The focused kernel collector maintains strict architectural compliance:

- **Level 1**: Collectors layer (depends only on domain)
- **Type Safety**: No `map[string]interface{}` usage  
- **OpenTelemetry**: Direct OTEL integration, no custom wrappers
- **Error Handling**: Comprehensive error propagation with context
- **Resource Management**: Proper cleanup and lifecycle management
- **Testing**: 80%+ test coverage maintained

## Contributing

The kernel collector follows strict production standards:

1. **Focus Maintained** - Only accept ConfigMap/Secret related features
2. **No Feature Creep** - Reject functionality covered by other collectors
3. **Security First** - All code must consider security implications
4. **Performance** - Maintain <0.2% CPU overhead baseline
5. **Documentation** - Update this README for any changes

## License

GPL-2.0 (required for eBPF kernel programs)

## Support

For support with the focused kernel collector:
- Slack: #tapio-kernel-config
- Issues: github.com/yairfalse/tapio/issues (label: kernel-collector)
- Docs: docs.tapio.io/kernel-collector-v2

---

*Last Updated: 2024-01-21*  
*Version: 2.0.0 (Focused ConfigMap/Secret Monitoring)*  
*Migration Required: Yes (from v1.x)*
*Maintainer: Tapio Kernel Team*