# Container Observer Design Refinements

**Date:** 2025-01-26
**Status:** Design Fixes
**Related:** 006-container-observer-design-v2.md

---

## 🎯 Purpose

Address 5 design concerns raised during architecture review:
1. Exit categorization too ambitious (8 → 3 categories)
2. cgroup monitor polling performance
3. Missing error handling details
4. Kernel compatibility requirements
5. OOM race condition handling

---

## 1. Exit Categorization - Start Simple

### ❌ Original (Too Ambitious)
8 categories: OOMKill, Normal, SIGKILL, SIGTERM, Segfault, Timeout, Resource, Unknown

### ✅ Refined (MVP)

**3 Categories for v1:**

```go
// types.go
type ExitCategory string

const (
    ExitCategoryOOMKill ExitCategory = "oom_kill"    // OOM killed by kernel
    ExitCategoryNormal  ExitCategory = "normal"      // Exit code 0 or clean signal
    ExitCategoryError   ExitCategory = "error"       // Non-zero exit or crash
)

type ExitClassification struct {
    Category    ExitCategory
    ExitCode    int32
    Signal      int32
    Evidence    []string  // Evidence used for classification
}

func ClassifyExit(exitCode int32, signal int32, isOOMKilled bool) ExitClassification {
    var evidence []string

    // Priority 1: OOM (most specific)
    if isOOMKilled {
        evidence = append(evidence, "oom_kill event detected")
        return ExitClassification{
            Category: ExitCategoryOOMKill,
            ExitCode: exitCode,
            Signal:   signal,
            Evidence: evidence,
        }
    }

    // Priority 2: Normal exit
    if exitCode == 0 || signal == 15 { // SIGTERM
        if exitCode == 0 {
            evidence = append(evidence, "exit_code=0")
        }
        if signal == 15 {
            evidence = append(evidence, "SIGTERM (clean shutdown)")
        }
        return ExitClassification{
            Category: ExitCategoryNormal,
            ExitCode: exitCode,
            Signal:   signal,
            Evidence: evidence,
        }
    }

    // Priority 3: Error (everything else)
    if exitCode != 0 {
        evidence = append(evidence, fmt.Sprintf("exit_code=%d", exitCode))
    }
    if signal != 0 {
        evidence = append(evidence, fmt.Sprintf("signal=%d", signal))
    }
    return ExitClassification{
        Category: ExitCategoryError,
        ExitCode: exitCode,
        Signal:   signal,
        Evidence: evidence,
    }
}
```

**Add Later (v2):**
- Segfault detection (signal == 11)
- Timeout detection (signal == 9 + short runtime)
- Resource exhaustion (exit code 137 + no OOM)

---

## 2. cgroup Monitor Optimization

### Problem
Reading cgroupfs on every event = 100s of reads/sec = unnecessary I/O.

### Solution: Event-Driven Cache with TTL

```go
// cgroup_monitor.go
type CgroupMonitor struct {
    cache     *lru.Cache[string, CgroupInfo]
    cacheTTL  time.Duration
    mu        sync.RWMutex

    // Metrics
    cacheHits   metric.Int64Counter
    cacheMisses metric.Int64Counter
    readErrors  metric.Int64Counter
}

type CgroupInfo struct {
    ContainerID   string
    PodName       string
    Namespace     string
    MemoryLimit   int64
    CPULimit      int64
    FetchedAt     time.Time
}

func NewCgroupMonitor(cacheSize int, cacheTTL time.Duration) (*CgroupMonitor, error) {
    cache, err := lru.New[string, CgroupInfo](cacheSize)
    if err != nil {
        return nil, fmt.Errorf("failed to create LRU cache: %w", err)
    }

    return &CgroupMonitor{
        cache:    cache,
        cacheTTL: cacheTTL, // 30s default
    }, nil
}

func (m *CgroupMonitor) GetCgroupInfo(ctx context.Context, cgroupPath string) (CgroupInfo, error) {
    // Try cache first (hot path)
    m.mu.RLock()
    if cached, ok := m.cache.Get(cgroupPath); ok {
        if time.Since(cached.FetchedAt) < m.cacheTTL {
            m.mu.RUnlock()
            m.cacheHits.Add(ctx, 1)
            return cached, nil
        }
    }
    m.mu.RUnlock()

    // Cache miss or expired - read from cgroupfs
    m.cacheMisses.Add(ctx, 1)
    info, err := m.readCgroupfs(cgroupPath)
    if err != nil {
        m.readErrors.Add(ctx, 1)
        return CgroupInfo{}, fmt.Errorf("failed to read cgroup %s: %w", cgroupPath, err)
    }

    info.FetchedAt = time.Now()

    // Update cache
    m.mu.Lock()
    m.cache.Add(cgroupPath, info)
    m.mu.Unlock()

    return info, nil
}

func (m *CgroupMonitor) readCgroupfs(cgroupPath string) (CgroupInfo, error) {
    // Read memory.limit_in_bytes
    memLimit, err := readCgroupInt64(cgroupPath, "memory.limit_in_bytes")
    if err != nil {
        return CgroupInfo{}, fmt.Errorf("failed to read memory limit: %w", err)
    }

    // Read cpu.cfs_quota_us
    cpuQuota, err := readCgroupInt64(cgroupPath, "cpu.cfs_quota_us")
    if err != nil {
        return CgroupInfo{}, fmt.Errorf("failed to read CPU quota: %w", err)
    }

    // Parse container ID from path
    // /sys/fs/cgroup/memory/kubepods/pod.../abc123...
    containerID := parseContainerID(cgroupPath)

    return CgroupInfo{
        ContainerID: containerID,
        MemoryLimit: memLimit,
        CPULimit:    cpuQuota,
    }, nil
}
```

**Configuration:**

```go
// Config
type Config struct {
    CgroupCacheSize int           `json:"cgroup_cache_size"` // Default: 1000
    CgroupCacheTTL  time.Duration `json:"cgroup_cache_ttl"`  // Default: 30s
}
```

**Why This Works:**
- ✅ Cache hit rate >90% for steady-state workloads
- ✅ TTL prevents stale data (pods can be deleted)
- ✅ LRU eviction handles large clusters (1000 entries = ~100KB)
- ✅ Metrics expose cache efficiency

**Alternative (inotify) Rejected:**
- inotify on `/sys/fs/cgroup` = 1000s of watch descriptors
- Kernel limit = 8192 watches per user (ulimit -n)
- Not scalable for large clusters

---

## 3. Error Handling Specification

### Ring Buffer Full

**eBPF Side:**
```c
// container_monitor.c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB
} events SEC(".maps");

SEC("tracepoint/oom/mark_victim")
int handle_oom(struct trace_event_raw_mark_victim *ctx) {
    struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        // Ring buffer full - eBPF cannot do anything
        // Userspace will see dropped events
        return 0;
    }

    // Fill event
    evt->type = EVENT_OOM_KILL;
    // ...

    bpf_ringbuf_submit(evt, 0);
    return 0;
}
```

**Userspace Side:**
```go
// observer_ebpf.go
func (c *ContainerObserver) loadAndAttachStage(ctx context.Context, eventCh chan ContainerEventBPF) error {
    rd, err := ringbuf.NewReader(c.objs.Events)
    if err != nil {
        return fmt.Errorf("failed to open ring buffer: %w", err)
    }
    defer rd.Close()

    for {
        record, err := rd.Read()
        if err != nil {
            if errors.Is(err, ringbuf.ErrClosed) {
                return nil // Graceful shutdown
            }
            c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
                attribute.String("error_type", "ringbuf_read"),
            ))
            continue // Don't crash on transient errors
        }

        var evt ContainerEventBPF
        if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
            c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
                attribute.String("error_type", "parse_event"),
            ))
            continue
        }

        select {
        case eventCh <- evt:
            c.eventsTotal.Add(ctx, 1)
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

**Monitoring:**
```bash
# Alert on dropped events
container_events_total{error_type="ringbuf_read"} > 10/min
```

---

### cgroup Disappeared Mid-Read

```go
// cgroup_monitor.go
func (m *CgroupMonitor) readCgroupfs(cgroupPath string) (CgroupInfo, error) {
    memLimit, err := readCgroupInt64(cgroupPath, "memory.limit_in_bytes")
    if err != nil {
        if os.IsNotExist(err) {
            // cgroup deleted (pod terminated) - not an error
            return CgroupInfo{ContainerID: parseContainerID(cgroupPath)}, ErrCgroupNotFound
        }
        return CgroupInfo{}, fmt.Errorf("failed to read memory limit: %w", err)
    }

    // Continue reading other fields...
}

// processor_exit.go
func (p *ExitProcessor) Process(ctx context.Context, evt ContainerEventBPF) *domain.ContainerEvent {
    cgroupInfo, err := p.cgroupMonitor.GetCgroupInfo(ctx, evt.CgroupPath)
    if err != nil {
        if errors.Is(err, ErrCgroupNotFound) {
            // Expected - container already gone
            // Fallback to minimal event using only eBPF data
            p.logger.Debug("cgroup not found (container terminated)",
                "cgroup_path", evt.CgroupPath,
                "container_id", parseContainerID(evt.CgroupPath))
        } else {
            // Unexpected error
            p.logger.Warn("failed to read cgroup",
                "error", err,
                "cgroup_path", evt.CgroupPath)
            p.readErrorsTotal.Add(ctx, 1)
        }

        // Emit event with partial data
        cgroupInfo = CgroupInfo{
            ContainerID: parseContainerID(evt.CgroupPath),
        }
    }

    return &domain.ContainerEvent{
        Type:        domain.EventTypeContainerExit,
        ContainerID: cgroupInfo.ContainerID,
        ExitCode:    evt.ExitCode,
        Signal:      evt.Signal,
        // ... rest of event
    }
}
```

---

### K8s API Timeout

```go
// observer.go
type Config struct {
    K8sAPITimeout time.Duration `json:"k8s_api_timeout"` // Default: 5s
}

// processor_exit.go
func (p *ExitProcessor) enrichWithK8s(ctx context.Context, evt *domain.ContainerEvent) {
    // Create timeout context
    k8sCtx, cancel := context.WithTimeout(ctx, p.config.K8sAPITimeout)
    defer cancel()

    pod, err := p.k8sClient.GetPodByContainerID(k8sCtx, evt.ContainerID)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            p.k8sTimeoutsTotal.Add(ctx, 1)
            p.logger.Warn("K8s API timeout - emitting event without pod metadata",
                "container_id", evt.ContainerID,
                "timeout", p.config.K8sAPITimeout)
        } else if !errors.Is(err, k8s.ErrPodNotFound) {
            p.k8sErrorsTotal.Add(ctx, 1)
            p.logger.Warn("K8s API error",
                "error", err,
                "container_id", evt.ContainerID)
        }

        // Fallback: emit event without K8s metadata
        evt.PodName = ""
        evt.Namespace = ""
        return
    }

    // Success - enrich event
    evt.PodName = pod.Name
    evt.Namespace = pod.Namespace
    evt.Labels = pod.Labels
}
```

**Monitoring:**
```bash
# Alert on high K8s API timeout rate
rate(container_k8s_timeouts_total[5m]) > 0.1
```

---

## 4. Kernel Compatibility Matrix

### Minimum Requirements

| Feature | Minimum Kernel | Why |
|---------|----------------|-----|
| **CO-RE (BTF)** | 5.2 | Type info in /sys/kernel/btf/vmlinux |
| **cgroup v2** | 4.5 | unified hierarchy |
| **tracepoint/oom/mark_victim** | 4.6 | OOM tracepoint added |
| **tracepoint/sched/sched_process_exit** | 2.6.23 | Ancient - always available |
| **BPF ring buffer** | 5.8 | Preferred over perf buffer |

**Conservative Choice: Kernel 5.8+**

### Runtime Detection

```go
// observer.go
func checkKernelCompatibility() error {
    var uname unix.Utsname
    if err := unix.Uname(&uname); err != nil {
        return fmt.Errorf("failed to get kernel version: %w", err)
    }

    release := unix.ByteSliceToString(uname.Release[:])
    major, minor, err := parseKernelVersion(release)
    if err != nil {
        return fmt.Errorf("failed to parse kernel version %s: %w", release, err)
    }

    if major < 5 || (major == 5 && minor < 8) {
        return fmt.Errorf("kernel %d.%d too old - require 5.8+ (for BPF ring buffer)", major, minor)
    }

    return nil
}

func NewObserver(name string, cfg Config) (*Observer, error) {
    if err := checkKernelCompatibility(); err != nil {
        return nil, fmt.Errorf("kernel compatibility check failed: %w", err)
    }

    // Continue with observer creation
}
```

### Fallback for Old Kernels (Optional v2)

```go
// Use perf buffer instead of ring buffer for kernel 5.2-5.7
func (c *ContainerObserver) selectEventBuffer() (string, error) {
    major, minor, _ := getKernelVersion()

    if major > 5 || (major == 5 && minor >= 8) {
        return "ringbuf", nil
    }

    if major == 5 && minor >= 2 {
        c.logger.Warn("kernel 5.2-5.7 detected - using perf buffer (slower)",
            "kernel_version", fmt.Sprintf("%d.%d", major, minor))
        return "perfbuf", nil
    }

    return "", fmt.Errorf("kernel %d.%d too old - require 5.2+", major, minor)
}
```

---

## 5. OOM Race Condition Handling

### Problem
Timeline:
1. Container hits OOM → kernel kills process
2. eBPF hook fires → event captured
3. Userspace reads event (200µs later)
4. Try to read cgroupfs → **cgroup already deleted** ❌

### Solution: Capture Everything in eBPF

```c
// bpf/container_monitor.c
struct container_event {
    u32 type;
    u32 pid;
    u32 tid;
    s32 exit_code;
    s32 signal;

    // Capture cgroup info AT EVENT TIME (before deletion)
    char cgroup_path[256];
    u64 memory_limit;     // From cgroup (if available)
    u64 memory_usage;     // From cgroup (if available)

    u64 timestamp_ns;
};

SEC("tracepoint/oom/mark_victim")
int handle_oom(struct trace_event_raw_mark_victim *ctx) {
    struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    evt->type = EVENT_OOM_KILL;
    evt->pid = bpf_get_current_pid_tgid() >> 32;
    evt->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    evt->timestamp_ns = bpf_ktime_get_ns();

    // Capture cgroup path NOW (before cgroup is deleted)
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    bpf_probe_read_kernel_str(evt->cgroup_path, sizeof(evt->cgroup_path),
                               BPF_CORE_READ(task, cgroups, subsys[0], cgroup, kn, name));

    // Try to capture memory info NOW (best effort)
    // If this fails, memory_limit will be 0
    struct mem_cgroup *memcg = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (memcg) {
        evt->memory_limit = BPF_CORE_READ(memcg, memory, max);
        evt->memory_usage = BPF_CORE_READ(memcg, memory, usage);
    }

    bpf_ringbuf_submit(evt, 0);
    return 0;
}
```

**Userspace Processing:**

```go
// processor_oom.go
func (p *OOMProcessor) Process(ctx context.Context, evt ContainerEventBPF) *domain.ContainerEvent {
    // Use data captured by eBPF (BEFORE cgroup deletion)
    containerID := parseContainerID(evt.CgroupPath)

    domainEvt := &domain.ContainerEvent{
        Type:        domain.EventTypeOOMKill,
        ContainerID: containerID,
        PID:         evt.PID,
        Timestamp:   time.Unix(0, int64(evt.TimestampNs)),
    }

    // If eBPF captured memory info, use it
    if evt.MemoryLimit > 0 {
        domainEvt.MemoryLimit = evt.MemoryLimit
        domainEvt.MemoryUsage = evt.MemoryUsage
    } else {
        // Fallback: try cgroup monitor (likely will fail, but try anyway)
        cgroupInfo, err := p.cgroupMonitor.GetCgroupInfo(ctx, evt.CgroupPath)
        if err == nil {
            domainEvt.MemoryLimit = cgroupInfo.MemoryLimit
        } else {
            p.logger.Debug("cgroup already deleted (expected for OOM)",
                "container_id", containerID,
                "cgroup_path", evt.CgroupPath)
        }
    }

    // Enrich with K8s metadata (independent of cgroup)
    p.enrichWithK8s(ctx, domainEvt)

    return domainEvt
}
```

**Why This Works:**
- ✅ eBPF captures data **before** cgroup deletion
- ✅ Userspace has all critical data even if cgroupfs gone
- ✅ K8s enrichment works (uses container ID, not cgroup path)
- ✅ Graceful degradation if eBPF memory read fails

---

## Summary of Changes

| Issue | Original | Refined |
|-------|----------|---------|
| **Exit categories** | 8 categories | 3 categories (v1), add more in v2 |
| **cgroup polling** | No caching mentioned | LRU cache with 30s TTL, 90%+ hit rate |
| **Error handling** | Not specified | Ring buffer: log + continue<br>cgroup gone: fallback to container ID<br>K8s timeout: emit partial event |
| **Kernel compat** | Not specified | Require 5.8+ (ring buffer)<br>Runtime check on startup |
| **OOM race** | Assume cgroup available | Capture memory data in eBPF<br>Fallback to eBPF data only |

---

## Updated Implementation Order

### Week 1: Foundation
1. ✅ `observer.go` - Config, constructor, kernel check
2. ✅ `types.go` - 3 exit categories only
3. ✅ `cgroup_monitor.go` - With LRU cache + TTL
4. ✅ `bpf/container_monitor.c` - OOM hook with memory capture
5. ✅ Tests

### Week 2: Core Processing
6. ✅ `processor_oom.go` - Use eBPF-captured data first
7. ✅ `processor_exit.go` - 3-category classification
8. ✅ `observer_ebpf.go` - Error handling per spec
9. ✅ Integration tests

### Week 3: Production Ready
10. ✅ OTEL metrics (cache hits/misses, errors, timeouts)
11. ✅ E2E tests (actual K8s pods)
12. ✅ Performance tests (1000 events/sec)
13. ✅ Negative tests (all error paths)

---

## Definition of Done (Refined)

- [ ] Kernel 5.8+ check on startup
- [ ] 3 exit categories working (OOM, Normal, Error)
- [ ] cgroup cache hit rate >90% (metric exposed)
- [ ] OOM events work even if cgroup deleted
- [ ] All error paths tested (ring buffer full, cgroup gone, K8s timeout)
- [ ] 6 test types passing
- [ ] Coverage >80%
- [ ] No map[string]interface{}
- [ ] No TODOs

**Ready to implement.**
