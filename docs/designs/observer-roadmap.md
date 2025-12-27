# TAPIO Observer Roadmap

**Date**: 2025-12-27
**Status**: Planning

---

## Current State

### Implemented Observers

| Observer | Type | Status | Destination | What it does |
|----------|------|--------|-------------|--------------|
| **network/** | eBPF | ✅ Done | TAPIO | TCP/UDP/DNS, RTT, connection failures |
| **container/** | eBPF | ✅ Done | TAPIO | OOM kills, exits via cgroup |
| **container-runtime/** | eBPF | ✅ Done | TAPIO | Container lifecycle via tracepoints |
| **container-api/** | K8s API | ✅ Done | → PORTTI | Pod/container via informers |
| **deployments/** | K8s API | ✅ Done | → PORTTI | Deployment changes |
| **scheduler/** | K8s API | ✅ Done | → PORTTI | Scheduling events + Prometheus |
| **node/** | K8s API | ✅ Done | → PORTTI | Node conditions via informers |

### Empty Placeholders (Not Implemented)

| Observer | Status | Notes |
|----------|--------|-------|
| **k8s/** | ❌ Empty | Utilities only, not an observer |
| **kubelet/** | ❌ Empty | → Move to PORTTI |
| **storage/** | ❌ Empty | Needs implementation |
| **topology/** | ❌ Empty | → Move to PORTTI |

---

## Observer Split (TAPIO vs PORTTI)

After ADR 009, observers are split:

```
TAPIO (eBPF - per node)          PORTTI (K8s API - central)
├── network/                      ├── deployments/
├── container/                    ├── scheduler/
├── container-runtime/            ├── node/
├── storage-io/ (NEW)             ├── container-api/
├── memory/ (NEW)                 ├── kubelet/ (NEW)
├── cpu/ (NEW)                    ├── topology/ (NEW)
└── k8scontext/ (lookup only)     └── services/ (NEW)
```

---

## Missing Observers - Priority Order

### Priority 1: Storage I/O (eBPF)

**Why**: Disk is a major failure cause. Slow disk = slow pods = timeouts.

```
What to capture:
├── Block I/O latency (read/write)
├── I/O queue depth
├── Disk throughput (MB/s)
├── I/O errors
└── Per-container I/O attribution

eBPF hooks:
├── blk_account_io_start
├── blk_account_io_done
└── block_rq_issue / block_rq_complete
```

**Events emitted**:
- `storage.io_latency_spike` - I/O latency > threshold
- `storage.io_error` - I/O errors
- `storage.throughput_drop` - Sudden throughput decrease

### Priority 2: Memory Pressure (eBPF)

**Why**: Detect pressure BEFORE OOM. OOM is too late.

```
What to capture:
├── Memory pressure events (PSI)
├── Page faults (major/minor)
├── Swap usage
├── cgroup memory.high events
└── Reclaim activity

eBPF hooks:
├── cgroup_memory_pressure
├── mm_vmscan_direct_reclaim_begin
├── oom_score_adj_update
└── handle_mm_fault
```

**Events emitted**:
- `memory.pressure` - Memory pressure detected
- `memory.reclaim` - Aggressive reclaim activity
- `memory.approaching_limit` - Near cgroup limit (before OOM)

### Priority 3: CPU Throttling (eBPF)

**Why**: CPU throttling causes latency but is invisible to apps.

```
What to capture:
├── CFS throttle events
├── CPU cgroup limit hits
├── Scheduler latency (runqueue wait)
├── Context switches
└── Per-container CPU attribution

eBPF hooks:
├── sched_stat_runtime
├── sched_switch
├── cgroup_throttle
└── sched_wakeup_new
```

**Events emitted**:
- `cpu.throttled` - Container CPU throttled
- `cpu.scheduler_delay` - High runqueue latency
- `cpu.contention` - CPU contention detected

### Priority 4: Kubelet (K8s API) → PORTTI

**Why**: Kubelet health affects entire node.

```
What to capture:
├── Kubelet health status
├── Eviction events
├── Image pull events
├── Volume mount/unmount
└── Pod lifecycle from kubelet perspective

Source: Kubelet API (/pods, /stats, /healthz)
```

**Events emitted**:
- `kubelet.eviction` - Pod evicted
- `kubelet.image_pull_failed` - Image pull failure
- `kubelet.volume_mount_failed` - Volume mount failure

### Priority 5: Services/Endpoints (K8s API) → PORTTI

**Why**: Service discovery changes cause traffic shifts.

```
What to capture:
├── Endpoint changes
├── Service creation/deletion
├── Load balancer events
└── Ingress changes

Source: K8s API informers
```

**Events emitted**:
- `service.endpoints_changed` - Backend pods changed
- `service.created` / `service.deleted`

---

## Implementation Order

### Phase 1: Complete Core (Q1)

1. **storage-io/** (eBPF) - Biggest blind spot
2. **memory/** (eBPF) - Before-OOM detection
3. Extract K8s observers to PORTTI

### Phase 2: Performance (Q2)

4. **cpu/** (eBPF) - Throttling detection
5. **kubelet/** (K8s API) - Node health

### Phase 3: Network Deep Dive (Q3)

6. **dns/** (eBPF) - Split from network if needed
7. **topology/** (K8s API) - Service mesh

---

## Observer Template

New observers should follow this pattern:

```go
//go:build linux

package newobserver

import (
    "github.com/yairfalse/tapio/internal/base"
    "github.com/yairfalse/tapio/pkg/intelligence"
)

type Config struct {
    // Observer-specific config
    Emitter intelligence.Service
}

type Observer struct {
    *base.BaseObserver
    config  Config
    emitter intelligence.Service
    // eBPF maps, processors, etc.
}

func NewObserver(name string, cfg Config) (*Observer, error) {
    baseObs, err := base.NewBaseObserver(name)
    if err != nil {
        return nil, err
    }
    // Initialize eBPF, processors, etc.
    return &Observer{
        BaseObserver: baseObs,
        config:       cfg,
        emitter:      cfg.Emitter,
    }, nil
}

func (o *Observer) Start(ctx context.Context) error {
    // Start eBPF, ring buffer reader, processors
}

func (o *Observer) Stop() error {
    // Cleanup
}
```

---

## Raw Event Format

After refactor, TAPIO sends `raw.ebpf` format:

```go
// tapio/pkg/raw/event.go

type RawEbpfEvent struct {
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    Type      EbpfType  `json:"type"`       // network, container, storage, memory, cpu
    Subtype   string    `json:"subtype"`    // connection_reset, oom_kill, io_spike

    // Context (enriched by K8sContextService)
    ClusterID string `json:"cluster_id"`
    Namespace string `json:"namespace"`
    PodName   string `json:"pod_name"`
    Container string `json:"container"`

    // Type-specific data (one of these set)
    Network   *NetworkData   `json:"network,omitempty"`
    Container *ContainerData `json:"container,omitempty"`
    Storage   *StorageData   `json:"storage,omitempty"`
    Memory    *MemoryData    `json:"memory,omitempty"`
    CPU       *CPUData       `json:"cpu,omitempty"`
}

type EbpfType string
const (
    EbpfTypeNetwork   EbpfType = "network"
    EbpfTypeContainer EbpfType = "container"
    EbpfTypeStorage   EbpfType = "storage"
    EbpfTypeMemory    EbpfType = "memory"
    EbpfTypeCPU       EbpfType = "cpu"
)
```

---

## Success Metrics

| Observer | Key Metric | Target |
|----------|------------|--------|
| storage-io | Detects I/O spike before timeout | 90% |
| memory | Detects pressure before OOM | 95% |
| cpu | Detects throttling causing latency | 90% |

---

**Next Step**: Implement storage-io observer (Priority 1)
