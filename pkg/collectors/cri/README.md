# CRI Collector - Container Runtime Interface Monitoring

**Status: Production Ready (Pure CRI API)**

## What This Actually Does

The CRI collector monitors container lifecycle events through the Container Runtime Interface (CRI) API. It polls your container runtime (containerd, CRI-O, Docker) every 5 seconds and generates events when containers change state.

This collector is **PURE CRI API** - focused exclusively on container runtime interface monitoring. For real-time eBPF events, see the separate `cri-ebpf` collector.

## Features That Work

- ✅ Container lifecycle events (create, start, stop)
- ✅ Kubernetes pod context extraction from labels
- ✅ Container metadata (images, mounts, annotations)
- ✅ Exit code tracking for debugging
- ✅ Automatic CRI socket detection
- ✅ Memory leak prevention (old state cleanup)
- ✅ OpenTelemetry metrics integration

## What This Collector Doesn't Do

- Real-time OOM kill detection (use `cri-ebpf` collector)
- Kernel-level process tracking (use `kernel` collector)
- Sub-second event detection (CRI API polling limitation)
- Memory pressure alerts (use `cri-ebpf` collector)

## Architecture

```
┌─────────────────┐    ┌──────────────┐    ┌─────────────────┐
│   CRI Socket    │◄───│ CRI Collector │───►│ CollectorEvent  │
│ (5s polling)    │    │   (Go API)    │    │   (Structured)  │
└─────────────────┘    └──────────────┘    └─────────────────┘
        │                       │                      │
        ▼                       ▼                      ▼
┌─────────────────┐    ┌──────────────┐    ┌─────────────────┐
│ containerd/     │    │ State Change │    │ Intelligence    │
│ CRI-O/Docker    │    │  Detection   │    │   Pipeline      │
└─────────────────┘    └──────────────┘    └─────────────────┘
```

## Supported Runtimes

- **containerd** (`/run/containerd/containerd.sock`) ✅ Most common
- **CRI-O** (`/run/crio/crio.sock`) ✅ OpenShift default
- **cri-dockerd** (`/var/run/cri-dockerd.sock`) ✅ Docker Engine
- **k3s** (`/run/k3s/containerd/containerd.sock`) ✅ Edge clusters

## Event Types Generated

```go
// Container created (scheduled but not started)
domain.EventTypeContainerCreate

// Container started (running)
domain.EventTypeContainerStart  

// Container stopped (exited)
domain.EventTypeContainerStop
```

## Sample Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/cri"

config := cri.NewDefaultConfig("cri-collector")
collector, err := cri.NewCollector("cri", config)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process events
for event := range collector.Events() {
    containerData, ok := event.GetContainerData()
    if ok {
        log.Printf("Container %s: %s -> %s", 
            containerData.ContainerID,
            containerData.Action,
            containerData.State)
    }
}
```

## Configuration

```go
type Config struct {
    Name         string        // Collector instance name
    SocketPath   string        // CRI socket (auto-detected if empty)
    BufferSize   int           // Event buffer size (default: 10000)
    PollInterval time.Duration // Polling frequency (default: 5s)
}
```

## Performance Characteristics

- **Latency:** 0-5 seconds (polling interval)
- **Overhead:** Low (single API call every 5s)
- **Memory:** ~2MB + (100 bytes × active containers)
- **CPU:** Minimal (<1% on modern systems)

## Kubernetes Integration

Automatically extracts context from container labels:

```yaml
# These labels become K8sContext in events
io.kubernetes.pod.name: "nginx-pod"
io.kubernetes.pod.namespace: "default"
io.kubernetes.pod.uid: "12345-abcd"
io.kubernetes.container.name: "nginx"
```

## Metrics (OpenTelemetry)

```
cri_events_processed_total{event_type, container_id}
cri_errors_total{error_type}
cri_processing_duration_ms{operation}
cri_dropped_events_total{reason}
cri_buffer_usage{collector}
```

## Complementary Collectors

For complete container monitoring, consider using alongside:

- **`cri-ebpf`** - Real-time eBPF-based OOM kills, memory pressure
- **`kernel`** - System-wide process and syscall monitoring  
- **`kubeapi`** - Kubernetes API events and resource changes

## Should You Use This Collector?

**Use CRI collector when:**
- You need rich container metadata
- Kubernetes label extraction is important
- 5-second latency is acceptable
- You want runtime-agnostic monitoring

**Use cri-ebpf collector when:**
- You need real-time OOM kill detection
- Memory pressure monitoring is critical
- Sub-second latency required
- Process exit tracking needed

**Use both cri + cri-ebpf when:**
- You want complete coverage (metadata + real-time)
- Different teams need different data granularity
- Cost of running both is acceptable

## Limitations

1. **Polling Delay:** Events delayed by up to 5 seconds
2. **No Real-time Events:** CRI API is polled, not event-driven
3. **CRI API Dependency:** Requires CRI socket access
4. **Container Lifecycle Only:** No process-level or kernel events
5. **No Memory Pressure:** For real-time memory monitoring, use `cri-ebpf`

## Dependencies

- Go 1.21+
- CRI socket access (usually requires privileged mode)
- Kubernetes RBAC for production deployments
- OpenTelemetry for metrics collection

## Test Coverage

Current: **47.9%** - needs improvement for production use.

Run tests: `go test ./...`

## Related Documentation

- [CRI-eBPF Collector](../cri-ebpf/README.md) - Real-time container events via eBPF
- [Kernel Collector](../kernel/README.md) - System-wide eBPF monitoring
- [Container Architecture](../../ARCHITECTURE.md) - Overall system design