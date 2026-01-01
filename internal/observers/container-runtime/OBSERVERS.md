# Container Observers - Dual Architecture

This package contains **two complementary container observers**, each solving different monitoring needs.

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                   Container Monitoring                        │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────────────┐       ┌──────────────────────────┐ │
│  │   Runtime Observer     │       │  API Observer     │ │
│  │  (observer.go)      │       │  (observer_k8s.go)       │ │
│  │                     │       │                          │ │
│  │  • Runtime forensics│       │  • Cluster monitoring    │ │
│  │  • Kernel-level     │       │  • K8s API integration   │ │
│  │  • PIDs, cgroups    │       │  • Pod context           │ │
│  │  • Linux-only       │       │  • Cross-platform ready  │ │
│  └─────────────────────┘       └──────────────────────────┘ │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

## 1. Runtime Observer (`observer.go`)

**Purpose**: Runtime forensics and kernel-level container monitoring

**Constructor**: `New(cfg Config, deps *base.Deps) (*RuntimeObserver, error)`

**Capabilities**:
- **Kernel-level monitoring** - eBPF tracepoints capture container exits at the source
- **Forensic details** - PID, cgroup paths, memory usage at exit time
- **OOM detection** - Direct kernel event capture (oom_kill tracepoint)
- **Exit classification** - Categorizes exits (OOM, normal, error, signal)

**Use Cases**:
- Root cause analysis for container failures
- Memory forensics (actual vs limit at OOM time)
- Low-level debugging (PIDs, signals, cgroup paths)
- Performance analysis (exit event frequency)

**Limitations**:
- Linux-only (requires eBPF support)
- Requires BPF object file (`container_monitor.o`)
- No K8s context (pod names, namespaces, labels)

**Example**:
```go
cfg := Config{BPFPath: "/path/to/container_monitor.o"}
observer, err := New(cfg, deps)
// Run blocks until context is cancelled
err = observer.Run(ctx)
```

**Event Structure** (eBPF):
```go
domain.ContainerEventData{
    PID:          12345,
    Category:     "oom_kill",
    Evidence:     []string{"memory_limit_exceeded"},
    MemoryLimit:  536870912,  // 512MB
    MemoryUsage:  537001984,  // 512.1MB (over limit!)
    CgroupPath:   "/kubepods/pod-abc/container-xyz",
}
```

## 2. API Observer (`observer_k8s.go`)

**Purpose**: Cluster-level container monitoring via K8s API

**Constructor**: `New(cfg Config, deps *base.Deps) (*APIObserver, error)`

**Capabilities**:
- **K8s API integration** - Uses Informers to watch Pod updates
- **Pod context** - Namespace, labels, annotations, node assignment
- **Container types** - Distinguishes init, main, ephemeral containers
- **Failure reasons** - ImagePullBackOff, CrashLoopBackOff, OOMKilled
- **Cross-node visibility** - Monitors entire cluster from single location

**Use Cases**:
- Cluster-wide container health monitoring
- Image pull failure detection
- Pod scheduling and placement context
- Multi-container pod failures (init, sidecar, main)

**Limitations**:
- K8s API latency (eventual consistency)
- No forensic details (PID, cgroup path)
- No memory usage at failure time

**Example**:
```go
cfg := Config{
    Clientset: k8sClient,
    Namespace: "production",  // or "" for all namespaces
}
observer, err := New(cfg, deps)
// Run blocks until context is cancelled
err = observer.Run(ctx)
```

**Event Structure** (K8s):
```go
domain.ContainerEventData{
    ContainerName: "nginx",
    ContainerType: "main",
    PodName:       "web-7d4b5",
    PodNamespace:  "production",
    NodeName:      "node-1",
    Image:         "nginx:1.21",
    State:         "Terminated",
    Reason:        "OOMKilled",
    Message:       "Container exceeded memory limit",
    RestartCount:  3,
    ExitCode:      137,
    Signal:        9,  // SIGKILL
}
```

## Comparison Matrix

| Feature                   | Runtime Observer      | API Observer  |
|---------------------------|-------------------|---------------------|
| **Monitoring Level**      | Kernel (eBPF)     | K8s API (Informer)  |
| **Pod Context**           | ❌ No             | ✅ Yes              |
| **PIDs & Cgroups**        | ✅ Yes            | ❌ No               |
| **Memory at Exit**        | ✅ Yes            | ❌ No               |
| **Image Pull Failures**   | ❌ No             | ✅ Yes              |
| **Container Types**       | ❌ No             | ✅ Yes (init/main)  |
| **Cross-Node Visibility** | ❌ Per-node only  | ✅ Cluster-wide     |
| **Platform Support**      | Linux-only        | Cross-platform      |
| **Latency**               | Low (kernel)      | Higher (API)        |
| **Deployment**            | DaemonSet         | Deployment          |

## When to Use Which?

### Use Runtime Observer When:
- Root cause analysis is critical
- Need forensic details (PID, cgroup, memory)
- Performance debugging (low latency required)
- Already running DaemonSets on every node

### Use API Observer When:
- Cluster-wide monitoring is needed
- Image pull failures are important
- Pod context (namespace, labels) is required
- Multi-container pod failures matter
- Cross-platform support is desired

### Use Both When:
- Complete observability is required
- Forensics + cluster context complement each other
- Different teams consume different event types
- Production-grade monitoring setup

## Event Deduplication

When running both observers simultaneously, you'll receive **complementary events**, not duplicates:

**eBPF Event** (from kernel):
```json
{
  "type": "container",
  "subtype": "container_oom_killed",
  "source": "container-observer",
  "containerData": {
    "pid": 12345,
    "memoryLimit": 536870912,
    "memoryUsage": 537001984,
    "cgroupPath": "/kubepods/pod-abc/container-xyz"
  }
}
```

**K8s Event** (from API):
```json
{
  "type": "container",
  "subtype": "container_oom_killed",
  "source": "container-observer-k8s",
  "containerData": {
    "containerName": "nginx",
    "podName": "web-7d4b5",
    "podNamespace": "production",
    "nodeName": "node-1",
    "image": "nginx:1.21",
    "reason": "OOMKilled",
    "exitCode": 137
  }
}
```

**Correlation**: Link by `exitCode=137` + timestamp proximity + node name

## Testing

Each observer has dedicated test files:

**Runtime Observer Tests**:
- `observer_e2e_test.go` - End-to-end workflow
- `observer_integration_test.go` - BPF loading and ring buffer
- `observer_system_test.go` - Linux-specific system tests
- `processor_oom_test.go` - OOM detection logic
- `processor_exit_test.go` - Exit classification logic

**API Observer Tests**:
- `observer_k8s_test.go` - K8s API integration tests

**Shared Tests**:
- `helpers_test.go` - Shared test utilities
- `types_test.go` - BPF type validation

## Metrics

Both observers emit OpenTelemetry metrics:

**Runtime Observer**:
```
tapio.observer.container.events_processed_total{source="ebpf"}
tapio.observer.container.errors_total{source="ebpf"}
```

**API Observer**:
```
tapio.observer.container.k8s.events_processed_total
tapio.observer.container.k8s.errors_total
tapio.observer.container.k8s.processing_time_ms
```

## References

- **Design Doc**: `/docs/002-tapio-observer-consolidation.md` (eBPF architecture)
- **Merge Resolution**: This dual-observer pattern resolved merge conflict between main (eBPF) and feat/container-observer (K8s API)
- **CLAUDE.md**: TDD workflow, production standards
