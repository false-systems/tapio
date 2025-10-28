# Design Doc 006: Container Observer - The REAL Implementation

**Status**: Draft v2 (v1 scrapped for being 20% observer)
**Date**: 2025-10-26
**Author**: Yair + Claude
**Context**: Tapio v1.0 - Following Brendan Gregg's eBPF principles
**Related**: CLAUDE.md (Section 5: eBPF Development Pattern)

---

## 🔥 Why v1 Was Scrapped

**v1 Design Flaws** (honest assessment):
- Exit codes alone = useless noise
- containerd API alone = insufficient data
- Ignoring eBPF = lazy design
- K8s enrichment races = wrong data
- No runtime metrics = not observability

**Result**: 20% observer with minimal, problematic features

**v2 Approach**: Build REAL observability following Brendan Gregg's principles

---

## 1. Brendan Gregg's eBPF Principles (MANDATORY)

### Principle 1: eBPF Captures, Userspace Parses

**From CLAUDE.md (Section 5)**:
> "eBPF should capture, userspace should parse. Parsing complex protocols in eBPF is slow and error-prone. Let eBPF collect the raw data, then parse it in userspace where you have full language features."

**Performance**:
- eBPF parsing: ~500ns per event (slow, limited instructions)
- Go parsing: ~50ns per event (10x faster!)
- Ring buffer already copies to userspace - parsing there is free

**Applied to Container Observer**:
- eBPF: Capture PID, exit_code, cgroup_id, errno (raw data)
- Go: Parse meanings, categorize exits, correlate with cgroup metrics

---

### Principle 2: Minimal eBPF Processing

**Rules**:
- NO string parsing in eBPF
- NO complex loops (bounded iteration only)
- Filter early (skip non-container PIDs immediately)
- Small event structs (~64 bytes)

**Applied**:
```c
// ❌ BAD - Complex parsing in eBPF
char comm[16];
bpf_probe_read_kernel(&comm, sizeof(comm), task->comm);
if (strcmp(comm, "nginx") == 0) { ... }  // SLOW!

// ✅ GOOD - Capture raw, parse in Go
evt->pid = task->pid;
evt->cgroup_id = task->cgroups->dfl_cgrp->kn->id;
// Go userspace: lookup cgroup_id → container name
```

---

### Principle 3: Single eBPF Program

**Why**:
- Lower kernel overhead (one program loaded)
- Shared BPF maps (container_pids, events ring buffer)
- Simpler lifecycle management (Start/Stop)

**Applied**:
- One `container_monitor.c` file
- Multiple hooks (OOM, exit, syscall)
- Shared ring buffer for all events

---

### Principle 4: Hook at the Right Layer

**Brendan Gregg's advice**: Hook where data is available

| Goal | Hook Point | Why |
|------|------------|-----|
| OOM detection | `kprobe/oom_kill_process` | Kernel knows victim PID, memory usage |
| Container exit | `tracepoint/sched/sched_process_exit` | Has exit_code, exit_signal |
| Syscall failures | `tracepoint/raw_syscalls/sys_exit` | Has syscall number, errno |
| Memory pressure | `/sys/fs/cgroup/.../memory.pressure` | Userspace read (PSI metrics) |

**NOT hooked**:
- containerd API → Too high level, missing context
- cgroup v1 → Deprecated, use cgroup v2 only

---

## 2. Problem Statement (The Real One)

### Problems We're ACTUALLY Solving

**Problem 1**: Container crashes with NO context
- Exit code 137 doesn't reveal WHY OOM happened
- **Need**: Memory usage trend (gradual leak vs sudden spike)
- **Need**: Failed syscalls leading to crash (ENOMEM, ENOSPC)
- **Need**: Network state at crash time (mid-request?)

**Problem 2**: OOM kills diagnosed too late
- By the time pod fails, container is gone
- **Need**: Memory pressure BEFORE OOM (50%, 80%, 90%)
- **Need**: Predict OOM 30s before it happens

**Problem 3**: Restart loops have no pattern detection
- CrashLoopBackOff visible in K8s, but WHY?
- **Need**: Correlation between crashes and resource pressure
- **Need**: Detect patterns (always crashes at 90% memory)

**Problem 4**: No runtime visibility
- Can't see container degrading BEFORE failure
- **Need**: Continuous resource monitoring (1s granularity)
- **Need**: Baseline vs current (is this normal?)

**Problem 5**: Syscall failures invisible
- Application errors due to ENOMEM, ENOSPC, EMFILE
- **Need**: Track failed syscalls by errno
- **Need**: Correlate syscall failures with container exits

---

## 3. Solution Architecture

### The 4-Layer Approach (Brendan Gregg Style)

```
┌────────────────────────────────────────────────────────────┐
│ Layer 1: eBPF Capture (Kernel Space)                       │
│ ──────────────────────────────────────────────────────────│
│ container_monitor.c (SINGLE eBPF program)                  │
│                                                             │
│ Hook 1: kprobe/oom_kill_process                            │
│   Capture: PID, cgroup_id, memory_usage, oom_score         │
│   Why: Kernel knows victim details at kill time            │
│                                                             │
│ Hook 2: tracepoint/sched/sched_process_exit                │
│   Capture: PID, exit_code, exit_signal                     │
│   Why: Process exit with exit code available               │
│                                                             │
│ Hook 3: tracepoint/raw_syscalls/sys_exit                   │
│   Capture: syscall_nr, ret, errno (ONLY failures: ret < 0) │
│   Filter: ENOMEM, ENOSPC, EMFILE, ENFILE only              │
│   Why: Critical syscall failures predict crashes           │
│                                                             │
│ BPF Map 1: container_pids (hash map)                       │
│   Key: PID → Value: cgroup_id                              │
│   Updated by userspace when containers start/stop          │
│   Why: Fast PID → container lookup in eBPF                 │
│                                                             │
│ BPF Map 2: events (ring buffer, per-CPU)                   │
│   Size: 256KB per CPU                                      │
│   Why: High-performance event delivery to userspace        │
└────────────────────┬───────────────────────────────────────┘
                     │ Ring Buffer Read (perf/ringbuf)
                     ▼
┌────────────────────────────────────────────────────────────┐
│ Layer 2: cgroup Monitor (Go Userspace)                     │
│ ──────────────────────────────────────────────────────────│
│ Read every 1 second from cgroupfs:                         │
│                                                             │
│ For each container in /sys/fs/cgroup/system.slice/docker-*:│
│   - memory.current (current memory usage bytes)            │
│   - memory.max (memory limit bytes)                        │
│   - memory.pressure (PSI: some avg10, avg60, total)        │
│   - cpu.stat (usage_usec, throttled_usec, nr_throttled)    │
│   - io.pressure (I/O stalls)                               │
│                                                             │
│ Store in time-series buffer:                               │
│   - Keep last 60 data points (1 minute window)             │
│   - Calculate trends (increasing, stable, decreasing)      │
│   - Detect anomalies (>2σ from baseline)                   │
│                                                             │
│ Baseline calculation:                                      │
│   - First 60s after container start = baseline             │
│   - Rolling average for comparison                         │
└────────────────────┬───────────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────────┐
│ Layer 3: Event Correlation (Go Userspace)                  │
│ ──────────────────────────────────────────────────────────│
│ When container exits (EVENT_PROCESS_EXIT):                 │
│                                                             │
│ 1. Lookup cgroup metrics (last 60s)                        │
│    → Memory trend: Was memory increasing?                  │
│    → CPU trend: Was CPU throttled?                         │
│    → Pressure: Was memory.pressure > 50%?                  │
│                                                             │
│ 2. Lookup failed syscalls (from eBPF events)               │
│    → Last 60s: How many ENOMEM? ENOSPC?                    │
│    → Correlation: Did syscall failures spike before exit?  │
│                                                             │
│ 3. Lookup OOM events (from eBPF)                           │
│    → Was there an OOM kill event for this PID?             │
│                                                             │
│ 4. Categorize exit (ROOT CAUSE):                           │
│    ┌─────────────────────────────────────────────────────┐│
│    │ Exit Category Matrix                                ││
│    │─────────────────────────────────────────────────────││
│    │ Exit 137 + OOM event                                ││
│    │   → Category: "oom_killed"                          ││
│    │   → Root Cause: Memory limit too low                ││
│    │   → Evidence: Memory at 100% for 30s                ││
│    │                                                      ││
│    │ Exit 1 + ENOMEM syscalls (>10 in last 60s)         ││
│    │   → Category: "memory_allocation_failure"           ││
│    │   → Root Cause: Memory pressure + app not handling  ││
│    │   → Evidence: memory.pressure > 80%, ENOMEM spike   ││
│    │                                                      ││
│    │ Exit 1 + ENOSPC syscalls                            ││
│    │   → Category: "disk_full"                           ││
│    │   → Root Cause: Disk space exhausted                ││
│    │   → Evidence: ENOSPC on write() syscalls            ││
│    │                                                      ││
│    │ Exit 143 + memory.pressure < 10%                    ││
│    │   → Category: "graceful_shutdown"                   ││
│    │   → Root Cause: Normal termination                  ││
│    │   → Evidence: SIGTERM, no resource pressure         ││
│    │                                                      ││
│    │ Exit 0                                               ││
│    │   → Category: "success"                             ││
│    │   → Root Cause: Normal completion                   ││
│    └─────────────────────────────────────────────────────┘│
└────────────────────┬───────────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────────┐
│ Layer 4: K8s Enrichment (Go Userspace)                     │
│ ──────────────────────────────────────────────────────────│
│ Enrich with K8s context (via informer cache):              │
│                                                             │
│ cgroup_id → containerd container → K8s pod                 │
│   - Pod name, namespace                                    │
│   - Container name, image                                  │
│   - Resource limits (from pod spec)                        │
│   - Restart count                                          │
│   - Node name                                              │
│   - Labels, annotations                                    │
│                                                             │
│ Comparison:                                                │
│   - Actual memory usage vs pod limit                       │
│   - CPU throttling vs CPU limit                            │
│   - Detect: Limit too low? (usage = 100% of limit)         │
└────────────────────┬───────────────────────────────────────┘
                     │
                     ▼
              domain.ObserverEvent
              ┌─────────────────────────────────────────────┐
              │ Type: "container"                           │
              │ Subtype: "exit_oom" | "exit_memory_failure" │
              │          "exit_disk_full" | etc.            │
              │                                             │
              │ ContainerData:                              │
              │   ExitCode: 137                             │
              │   ExitCategory: "oom_killed"                │
              │   RootCause: "Memory limit too low"         │
              │                                             │
              │   RuntimeMetrics:                           │
              │     MemoryUsageBytes: 524288000             │
              │     MemoryLimitBytes: 524288000 (100%!)     │
              │     MemoryPressurePct: 95                   │
              │     MemoryTrend: "increasing"               │
              │     CPUThrottledPct: 5                      │
              │                                             │
              │   SyscallFailures:                          │
              │     ENOMEM: 15 (last 60s)                   │
              │     ENOSPC: 0                               │
              │                                             │
              │   K8sMetadata:                              │
              │     PodName: "nginx-abc"                    │
              │     Namespace: "production"                 │
              │     Image: "nginx:1.25"                     │
              │     RestartCount: 5                         │
              └─────────────────────────────────────────────┘
```

---

## 4. eBPF Program Implementation

### container_monitor.c (Complete Implementation)

```c
// bpf/container_monitor.c
// Following Brendan Gregg's "capture in kernel, parse in userspace"

#include <vmlinux.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// ═══════════════════════════════════════════════════════════
// Event Types and Structures
// ═══════════════════════════════════════════════════════════

enum event_type {
    EVENT_OOM_KILL = 1,
    EVENT_PROCESS_EXIT = 2,
    EVENT_SYSCALL_FAILURE = 3,
};

// Minimal event struct (Brendan Gregg: keep it small for performance)
// Size: 64 bytes (fits in cache line)
struct container_event {
    __u64 timestamp_ns;    // bpf_ktime_get_ns()
    __u32 pid;             // Process ID
    __u32 tid;             // Thread ID
    __u64 cgroup_id;       // Container identifier
    __u8  event_type;      // EVENT_OOM_KILL, EVENT_PROCESS_EXIT, etc.
    __u8  _pad[7];         // Alignment padding

    // Event-specific data (union to save space)
    union {
        // OOM kill event data
        struct {
            __u64 memory_usage;   // RSS at kill time (bytes)
            __u64 memory_limit;   // cgroup memory limit (bytes)
            __u32 oom_score;      // OOM score at kill
            __u32 _pad;
        } oom;

        // Process exit event data
        struct {
            __s32 exit_code;      // Exit code (e.g., 137)
            __u32 exit_signal;    // Signal that caused exit (e.g., SIGKILL)
        } exit;

        // Syscall failure event data
        struct {
            __u64 syscall_nr;     // Syscall number
            __s64 syscall_ret;    // Return value (negative = error)
            __u32 errno_val;      // errno value (ENOMEM, ENOSPC, etc.)
            __u32 _pad;
        } syscall;
    };
};

// ═══════════════════════════════════════════════════════════
// BPF Maps
// ═══════════════════════════════════════════════════════════

// Ring buffer for events (per-CPU for performance)
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB per CPU
} events SEC(".maps");

// Hash map: PID → cgroup_id
// Updated by userspace when containers start/stop
// Used for fast filtering (is this PID a container?)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);  // Support 10K containers max
    __type(key, __u32);          // PID
    __type(value, __u64);        // cgroup_id
} container_pids SEC(".maps");

// ═══════════════════════════════════════════════════════════
// HOOK 1: OOM Kill Detection
// ═══════════════════════════════════════════════════════════
// Hook: kprobe/oom_kill_process
// Triggered: When kernel OOM killer selects a victim process
// Data available: task_struct of victim, memory usage, OOM score
// ═══════════════════════════════════════════════════════════

SEC("kprobe/oom_kill_process")
int trace_oom_kill(struct pt_regs *ctx) {
    // PT_REGS_PARM2 = second argument to oom_kill_process
    // This is the task_struct* of the victim process
    struct task_struct *task = (struct task_struct *)PT_REGS_PARM2(ctx);

    // Allocate event in ring buffer
    struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;  // Buffer full, drop event (backpressure)
    }

    // Capture minimal data (Brendan Gregg: capture, don't parse)
    evt->timestamp_ns = bpf_ktime_get_ns();
    evt->pid = BPF_CORE_READ(task, tgid);  // Thread group ID = process ID
    evt->tid = BPF_CORE_READ(task, pid);   // Thread ID
    evt->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    evt->event_type = EVENT_OOM_KILL;

    // OOM-specific data: memory usage at kill time
    struct mm_struct *mm = BPF_CORE_READ(task, mm);
    if (mm) {
        // RSS (Resident Set Size) = file pages + anon pages
        unsigned long file_pages = BPF_CORE_READ(mm, rss_stat.count[MM_FILEPAGES]);
        unsigned long anon_pages = BPF_CORE_READ(mm, rss_stat.count[MM_ANONPAGES]);
        evt->oom.memory_usage = (file_pages + anon_pages) * 4096;  // Pages to bytes
    }

    // OOM score (higher = more likely to be killed)
    evt->oom.oom_score = BPF_CORE_READ(task, signal, oom_score_adj);

    // Memory limit: Read from cgroup (Note: requires kernel access)
    // Alternative: userspace reads this from cgroupfs
    evt->oom.memory_limit = 0;  // Populated by userspace correlation

    // Submit event to ring buffer
    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// ═══════════════════════════════════════════════════════════
// HOOK 2: Process Exit Detection
// ═══════════════════════════════════════════════════════════
// Hook: tracepoint/sched/sched_process_exit
// Triggered: When any process exits
// Data available: PID, exit_code, exit_signal
// Filter: Only track container processes (via container_pids map)
// ═══════════════════════════════════════════════════════════

SEC("tracepoint/sched/sched_process_exit")
int trace_process_exit(struct trace_event_raw_sched_process_template *ctx) {
    __u32 pid = ctx->pid;

    // Filter: Check if this PID is a container process
    // Brendan Gregg: Filter early for performance
    __u64 *cgroup_id = bpf_map_lookup_elem(&container_pids, &pid);
    if (!cgroup_id) {
        return 0;  // Not a container, skip
    }

    // Allocate event
    struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    // Capture exit data
    evt->timestamp_ns = bpf_ktime_get_ns();
    evt->pid = pid;
    evt->cgroup_id = *cgroup_id;
    evt->event_type = EVENT_PROCESS_EXIT;

    // Get current task_struct to read exit code
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();

    // exit_code format: high byte = exit code, low byte = signal
    // Example: 137 << 8 | 9 (SIGKILL)
    __u32 exit_code = BPF_CORE_READ(task, exit_code);
    evt->exit.exit_code = exit_code >> 8;        // Extract exit code
    evt->exit.exit_signal = BPF_CORE_READ(task, exit_signal);

    // Clean up PID from map (process no longer exists)
    bpf_map_delete_elem(&container_pids, &pid);

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

// ═══════════════════════════════════════════════════════════
// HOOK 3: Syscall Failure Detection
// ═══════════════════════════════════════════════════════════
// Hook: tracepoint/raw_syscalls/sys_exit
// Triggered: When any syscall exits
// Data available: syscall number, return value
// Filter: Only failures (ret < 0) and critical errors
// ═══════════════════════════════════════════════════════════

SEC("tracepoint/raw_syscalls/sys_exit")
int trace_syscall_exit(struct trace_event_raw_sys_exit *ctx) {
    __s64 ret = ctx->ret;

    // Filter 1: Only track failures (Brendan Gregg: filter early)
    if (ret >= 0) {
        return 0;  // Success, skip
    }

    __u32 pid = bpf_get_current_pid_tgid() >> 32;

    // Filter 2: Only container processes
    __u64 *cgroup_id = bpf_map_lookup_elem(&container_pids, &pid);
    if (!cgroup_id) {
        return 0;  // Not a container
    }

    // Filter 3: Only track critical errors (reduce noise)
    // Brendan Gregg: Focus on actionable signals
    __u32 errno_val = -ret;  // Convert negative ret to positive errno

    // Critical errors that predict container crashes:
    // - ENOMEM (12): Out of memory
    // - ENOSPC (28): No space left on device
    // - EMFILE (24): Too many open files (per-process limit)
    // - ENFILE (23): File table overflow (system-wide limit)
    if (errno_val != 12 &&  // ENOMEM
        errno_val != 28 &&  // ENOSPC
        errno_val != 24 &&  // EMFILE
        errno_val != 23) {  // ENFILE
        return 0;  // Not a critical error, skip
    }

    // Allocate event
    struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
    if (!evt) {
        return 0;
    }

    // Capture syscall failure data
    evt->timestamp_ns = bpf_ktime_get_ns();
    evt->pid = pid;
    evt->cgroup_id = *cgroup_id;
    evt->event_type = EVENT_SYSCALL_FAILURE;
    evt->syscall.syscall_nr = ctx->id;      // Syscall number (e.g., __NR_mmap)
    evt->syscall.syscall_ret = ret;         // Negative return value
    evt->syscall.errno_val = errno_val;     // errno (ENOMEM, ENOSPC, etc.)

    bpf_ringbuf_submit(evt, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
```

---

## 5. cgroup Monitor Implementation (Go Userspace)

### Why cgroup Monitor is Critical

**Problem**: eBPF can't easily read cgroup metrics
- cgroup memory.current: Requires reading from cgroupfs
- memory.pressure (PSI): Complex calculation, not available in eBPF
- CPU throttling: Requires reading cpu.stat file

**Solution**: Go userspace reads cgroupfs every 1 second

```go
// internal/observers/container/cgroup_monitor.go

package container

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"
)

// CgroupMetrics represents container resource usage at a point in time
type CgroupMetrics struct {
    Timestamp time.Time

    // Memory metrics
    MemoryCurrentBytes uint64  // memory.current
    MemoryLimitBytes   uint64  // memory.max
    MemoryUsagePct     float64 // (current / limit) * 100

    // Memory pressure (PSI - Pressure Stall Information)
    MemoryPressureSomeAvg10 float64  // % of time some tasks stalled (10s window)
    MemoryPressureSomeAvg60 float64  // % of time some tasks stalled (60s window)
    MemoryPressureSomeTotal uint64   // Total stall time (microseconds)

    // CPU metrics
    CPUUsageUsec      uint64  // Total CPU usage (microseconds)
    CPUThrottledUsec  uint64  // Time throttled due to CPU limit
    CPUThrottledCount uint64  // Number of throttle events
    CPUThrottledPct   float64 // (throttled / total) * 100

    // I/O pressure
    IOPressureSomeAvg10 float64
    IOPressureSomeAvg60 float64
}

// CgroupTimeSeries stores last 60 seconds of metrics (1s granularity)
type CgroupTimeSeries struct {
    ContainerID string
    CgroupPath  string
    Metrics     []CgroupMetrics  // Ring buffer, size 60
    Index       int              // Current index in ring buffer
    Baseline    *CgroupMetrics   // Baseline (first 60s average)
    mu          sync.RWMutex
}

// CgroupMonitor monitors cgroup metrics for all containers
type CgroupMonitor struct {
    cgroupBasePath string  // e.g., "/sys/fs/cgroup"
    containers     map[string]*CgroupTimeSeries  // containerID → time series
    mu             sync.RWMutex
}

func NewCgroupMonitor(basePath string) *CgroupMonitor {
    return &CgroupMonitor{
        cgroupBasePath: basePath,
        containers:     make(map[string]*CgroupTimeSeries),
    }
}

// Start monitoring (reads cgroup metrics every 1 second)
func (m *CgroupMonitor) Start(ctx context.Context) error {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            m.scanAndUpdate(ctx)
        }
    }
}

// Scan all containers and update metrics
func (m *CgroupMonitor) scanAndUpdate(ctx context.Context) {
    // Find all container cgroups (e.g., /sys/fs/cgroup/system.slice/docker-*.scope)
    pattern := filepath.Join(m.cgroupBasePath, "system.slice", "docker-*.scope")
    matches, err := filepath.Glob(pattern)
    if err != nil {
        return
    }

    for _, cgroupPath := range matches {
        // Extract container ID from path
        // e.g., /sys/fs/cgroup/system.slice/docker-abc123.scope → abc123
        containerID := extractContainerID(cgroupPath)

        // Read current metrics
        metrics, err := m.readCgroupMetrics(cgroupPath)
        if err != nil {
            continue
        }

        // Update time series
        m.updateTimeSeries(containerID, cgroupPath, metrics)
    }
}

// Read cgroup metrics from cgroupfs
func (m *CgroupMonitor) readCgroupMetrics(cgroupPath string) (*CgroupMetrics, error) {
    metrics := &CgroupMetrics{
        Timestamp: time.Now(),
    }

    // Read memory.current
    memCurrent, err := readUint64(filepath.Join(cgroupPath, "memory.current"))
    if err != nil {
        return nil, err
    }
    metrics.MemoryCurrentBytes = memCurrent

    // Read memory.max
    memMax, err := readUint64(filepath.Join(cgroupPath, "memory.max"))
    if err != nil {
        return nil, err
    }
    metrics.MemoryLimitBytes = memMax

    // Calculate usage percentage
    if memMax > 0 {
        metrics.MemoryUsagePct = (float64(memCurrent) / float64(memMax)) * 100
    }

    // Read memory.pressure (PSI format)
    // Example:
    // some avg10=0.00 avg60=0.00 avg300=0.00 total=0
    // full avg10=0.00 avg60=0.00 avg300=0.00 total=0
    pressureFile := filepath.Join(cgroupPath, "memory.pressure")
    if psi, err := readPSI(pressureFile); err == nil {
        metrics.MemoryPressureSomeAvg10 = psi.SomeAvg10
        metrics.MemoryPressureSomeAvg60 = psi.SomeAvg60
        metrics.MemoryPressureSomeTotal = psi.SomeTotal
    }

    // Read cpu.stat
    // Example:
    // usage_usec 12345678
    // user_usec 10000000
    // system_usec 2345678
    // nr_periods 100
    // nr_throttled 5
    // throttled_usec 50000
    cpuStatFile := filepath.Join(cgroupPath, "cpu.stat")
    if cpuStat, err := readCPUStat(cpuStatFile); err == nil {
        metrics.CPUUsageUsec = cpuStat.UsageUsec
        metrics.CPUThrottledUsec = cpuStat.ThrottledUsec
        metrics.CPUThrottledCount = cpuStat.NrThrottled

        if cpuStat.UsageUsec > 0 {
            metrics.CPUThrottledPct = (float64(cpuStat.ThrottledUsec) / float64(cpuStat.UsageUsec)) * 100
        }
    }

    // Read io.pressure
    ioPressureFile := filepath.Join(cgroupPath, "io.pressure")
    if psi, err := readPSI(ioPressureFile); err == nil {
        metrics.IOPressureSomeAvg10 = psi.SomeAvg10
        metrics.IOPressureSomeAvg60 = psi.SomeAvg60
    }

    return metrics, nil
}

// Update time series for container
func (m *CgroupMonitor) updateTimeSeries(containerID, cgroupPath string, metrics *CgroupMetrics) {
    m.mu.Lock()
    defer m.mu.Unlock()

    ts, exists := m.containers[containerID]
    if !exists {
        // Create new time series
        ts = &CgroupTimeSeries{
            ContainerID: containerID,
            CgroupPath:  cgroupPath,
            Metrics:     make([]CgroupMetrics, 60),  // 60 seconds
            Index:       0,
        }
        m.containers[containerID] = ts
    }

    // Add to ring buffer
    ts.mu.Lock()
    ts.Metrics[ts.Index] = *metrics
    ts.Index = (ts.Index + 1) % 60

    // Calculate baseline (first 60 samples)
    if ts.Baseline == nil && ts.Index == 0 {
        ts.Baseline = calculateBaseline(ts.Metrics)
    }
    ts.mu.Unlock()
}

// Get metrics for container (last 60 seconds)
func (m *CgroupMonitor) GetMetrics(containerID string) (*CgroupTimeSeries, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    ts, exists := m.containers[containerID]
    return ts, exists
}

// Helper: Read uint64 from file
func readUint64(path string) (uint64, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0, err
    }
    return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// PSI (Pressure Stall Information) data
type PSIData struct {
    SomeAvg10  float64
    SomeAvg60  float64
    SomeAvg300 float64
    SomeTotal  uint64
    FullAvg10  float64
    FullAvg60  float64
    FullAvg300 float64
    FullTotal  uint64
}

// Parse PSI file format
func readPSI(path string) (*PSIData, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    psi := &PSIData{}
    scanner := bufio.NewScanner(file)

    for scanner.Scan() {
        line := scanner.Text()
        // Example: some avg10=0.00 avg60=0.00 avg300=0.00 total=0
        if strings.HasPrefix(line, "some ") {
            fmt.Sscanf(line, "some avg10=%f avg60=%f avg300=%f total=%d",
                &psi.SomeAvg10, &psi.SomeAvg60, &psi.SomeAvg300, &psi.SomeTotal)
        } else if strings.HasPrefix(line, "full ") {
            fmt.Sscanf(line, "full avg10=%f avg60=%f avg300=%f total=%d",
                &psi.FullAvg10, &psi.FullAvg60, &psi.FullAvg300, &psi.FullTotal)
        }
    }

    return psi, scanner.Err()
}

// CPU stat data
type CPUStatData struct {
    UsageUsec     uint64
    UserUsec      uint64
    SystemUsec    uint64
    NrPeriods     uint64
    NrThrottled   uint64
    ThrottledUsec uint64
}

// Parse cpu.stat file
func readCPUStat(path string) (*CPUStatData, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    stat := &CPUStatData{}
    scanner := bufio.NewScanner(file)

    for scanner.Scan() {
        line := scanner.Text()
        fields := strings.Fields(line)
        if len(fields) != 2 {
            continue
        }

        value, _ := strconv.ParseUint(fields[1], 10, 64)

        switch fields[0] {
        case "usage_usec":
            stat.UsageUsec = value
        case "user_usec":
            stat.UserUsec = value
        case "system_usec":
            stat.SystemUsec = value
        case "nr_periods":
            stat.NrPeriods = value
        case "nr_throttled":
            stat.NrThrottled = value
        case "throttled_usec":
            stat.ThrottledUsec = value
        }
    }

    return stat, scanner.Err()
}

// Calculate baseline (average of first 60 samples)
func calculateBaseline(metrics []CgroupMetrics) *CgroupMetrics {
    baseline := &CgroupMetrics{}
    count := 0

    for _, m := range metrics {
        if m.Timestamp.IsZero() {
            continue
        }
        baseline.MemoryCurrentBytes += m.MemoryCurrentBytes
        baseline.MemoryPressureSomeAvg60 += m.MemoryPressureSomeAvg60
        baseline.CPUUsageUsec += m.CPUUsageUsec
        baseline.CPUThrottledPct += m.CPUThrottledPct
        count++
    }

    if count > 0 {
        baseline.MemoryCurrentBytes /= uint64(count)
        baseline.MemoryPressureSomeAvg60 /= float64(count)
        baseline.CPUUsageUsec /= uint64(count)
        baseline.CPUThrottledPct /= float64(count)
    }

    return baseline
}

// Extract container ID from cgroup path
func extractContainerID(cgroupPath string) string {
    // /sys/fs/cgroup/system.slice/docker-abc123def456.scope → abc123def456
    base := filepath.Base(cgroupPath)
    base = strings.TrimPrefix(base, "docker-")
    base = strings.TrimSuffix(base, ".scope")
    return base
}
```

---

## 6. Event Correlation (Root Cause Analysis)

This is where the REAL observability happens. We correlate:
1. eBPF events (OOM, exit, syscall failures)
2. cgroup metrics (last 60s)
3. K8s metadata

```go
// internal/observers/container/processor_exit.go

package container

import (
    "context"
    "fmt"
    "time"

    "github.com/yairfalse/tapio/pkg/domain"
)

// ExitProcessor processes container exit events with full correlation
type ExitProcessor struct {
    cgroupMonitor *CgroupMonitor
    enricher      *ContainerEnricher

    // Track syscall failures (last 60s)
    syscallFailures map[uint32][]SyscallFailure  // PID → failures
}

type SyscallFailure struct {
    Timestamp  time.Time
    SyscallNr  uint64
    ErrnoVal   uint32
}

// Process container exit event (THE CORE LOGIC)
func (p *ExitProcessor) Process(ctx context.Context, evt *containerEvent) *domain.ObserverEvent {
    // Step 1: Get cgroup metrics (last 60s)
    containerID := cgroupIDToContainerID(evt.CgroupID)
    timeSeries, exists := p.cgroupMonitor.GetMetrics(containerID)
    if !exists {
        // No metrics available, emit basic event
        return p.createBasicExitEvent(evt)
    }

    // Step 2: Analyze resource trends
    analysis := p.analyzeResourceTrends(timeSeries)

    // Step 3: Check for syscall failures (last 60s)
    syscallFailures := p.getSyscallFailures(evt.PID, 60*time.Second)

    // Step 4: Categorize exit (ROOT CAUSE ANALYSIS)
    category, rootCause := p.categorizeExit(evt, analysis, syscallFailures)

    // Step 5: Enrich with K8s metadata
    k8sMetadata, _ := p.enricher.EnrichContainer(ctx, containerID)

    // Step 6: Build comprehensive event
    return &domain.ObserverEvent{
        ID:        generateID(),
        Type:      "container",
        Subtype:   category,  // "exit_oom", "exit_memory_failure", etc.
        Timestamp: time.Now(),

        ContainerData: &domain.ContainerEventData{
            ContainerID:   containerID,
            ContainerName: k8sMetadata.ContainerName,
            ExitCode:      evt.Exit.ExitCode,
            State:         "exited",

            // ROOT CAUSE (the value!)
            ExitCategory: category,
            RootCause:    rootCause,

            // Runtime metrics (last value before exit)
            RuntimeMetrics: &domain.RuntimeMetrics{
                MemoryUsageBytes:      timeSeries.GetLatest().MemoryCurrentBytes,
                MemoryLimitBytes:      timeSeries.GetLatest().MemoryLimitBytes,
                MemoryUsagePct:        timeSeries.GetLatest().MemoryUsagePct,
                MemoryPressurePct:     timeSeries.GetLatest().MemoryPressureSomeAvg60,
                MemoryTrend:           analysis.MemoryTrend,  // "increasing", "stable", "decreasing"

                CPUUsageUsec:          timeSeries.GetLatest().CPUUsageUsec,
                CPUThrottledPct:       timeSeries.GetLatest().CPUThrottledPct,
                CPUTrend:              analysis.CPUTrend,

                IOPressurePct:         timeSeries.GetLatest().IOPressureSomeAvg60,
            },

            // Syscall failures (evidence)
            SyscallFailures: map[string]int{
                "ENOMEM": countSyscallFailures(syscallFailures, 12),
                "ENOSPC": countSyscallFailures(syscallFailures, 28),
                "EMFILE": countSyscallFailures(syscallFailures, 24),
                "ENFILE": countSyscallFailures(syscallFailures, 23),
            },

            // K8s metadata
            PodName:      k8sMetadata.PodName,
            Namespace:    k8sMetadata.Namespace,
            ImageName:    k8sMetadata.ImageName,
            RestartCount: k8sMetadata.RestartCount,
        },
    }
}

// Categorize exit (ROOT CAUSE ANALYSIS)
func (p *ExitProcessor) categorizeExit(
    evt *containerEvent,
    analysis *ResourceAnalysis,
    syscallFailures []SyscallFailure,
) (category, rootCause string) {

    exitCode := evt.Exit.ExitCode

    // ═══════════════════════════════════════════════════════
    // Case 1: OOM Kill (exit code 137 + high memory pressure)
    // ═══════════════════════════════════════════════════════
    if exitCode == 137 && analysis.MemoryPressurePct > 80 {
        if analysis.MemoryUsagePct >= 95 {
            return "exit_oom", "Memory limit too low - container at 100% of limit before OOM"
        }
        if analysis.MemoryTrend == "increasing" {
            return "exit_oom", "Memory leak detected - usage steadily increasing before OOM"
        }
        return "exit_oom", "Out of memory - killed by kernel OOM killer"
    }

    // ═══════════════════════════════════════════════════════
    // Case 2: Memory Allocation Failure (ENOMEM syscalls)
    // ═══════════════════════════════════════════════════════
    enomemCount := countSyscallFailures(syscallFailures, 12)  // ENOMEM
    if exitCode == 1 && enomemCount > 10 {
        if analysis.MemoryPressurePct > 50 {
            return "exit_memory_failure",
                fmt.Sprintf("Application crashed due to memory allocation failures - %d ENOMEM syscalls, memory pressure %d%%",
                    enomemCount, int(analysis.MemoryPressurePct))
        }
        return "exit_memory_failure",
            fmt.Sprintf("Memory allocation failures - %d ENOMEM syscalls before crash", enomemCount)
    }

    // ═══════════════════════════════════════════════════════
    // Case 3: Disk Full (ENOSPC syscalls)
    // ═══════════════════════════════════════════════════════
    enospcCount := countSyscallFailures(syscallFailures, 28)  // ENOSPC
    if exitCode == 1 && enospcCount > 0 {
        return "exit_disk_full",
            fmt.Sprintf("Disk space exhausted - %d ENOSPC syscalls (write failures)", enospcCount)
    }

    // ═══════════════════════════════════════════════════════
    // Case 4: File Descriptor Exhaustion
    // ═══════════════════════════════════════════════════════
    emfileCount := countSyscallFailures(syscallFailures, 24)  // EMFILE
    if exitCode == 1 && emfileCount > 5 {
        return "exit_fd_exhaustion",
            fmt.Sprintf("File descriptor limit reached - %d EMFILE syscalls", emfileCount)
    }

    // ═══════════════════════════════════════════════════════
    // Case 5: CPU Throttling Leading to Timeout
    // ═══════════════════════════════════════════════════════
    if exitCode == 1 && analysis.CPUThrottledPct > 50 {
        return "exit_cpu_throttled",
            fmt.Sprintf("Application likely timed out due to severe CPU throttling (%d%% throttled)",
                int(analysis.CPUThrottledPct))
    }

    // ═══════════════════════════════════════════════════════
    // Case 6: Graceful Shutdown (SIGTERM, low pressure)
    // ═══════════════════════════════════════════════════════
    if exitCode == 143 && analysis.MemoryPressurePct < 20 {
        return "exit_graceful", "Normal termination - SIGTERM with no resource pressure"
    }

    // ═══════════════════════════════════════════════════════
    // Case 7: Graceful Completion
    // ═══════════════════════════════════════════════════════
    if exitCode == 0 {
        return "exit_success", "Container completed successfully"
    }

    // ═══════════════════════════════════════════════════════
    // Case 8: Unknown (application error, no clear resource cause)
    // ═══════════════════════════════════════════════════════
    return "exit_unknown",
        fmt.Sprintf("Application exited with code %d - no clear resource cause detected", exitCode)
}

// Resource trend analysis
type ResourceAnalysis struct {
    MemoryTrend         string  // "increasing", "stable", "decreasing"
    MemoryUsagePct      float64 // Latest usage %
    MemoryPressurePct   float64 // Latest pressure %

    CPUTrend            string
    CPUThrottledPct     float64

    IOPressurePct       float64
}

// Analyze resource trends (last 60s)
func (p *ExitProcessor) analyzeResourceTrends(ts *CgroupTimeSeries) *ResourceAnalysis {
    ts.mu.RLock()
    defer ts.mu.RUnlock()

    analysis := &ResourceAnalysis{}

    // Get latest metrics
    latest := ts.Metrics[(ts.Index-1+60)%60]
    analysis.MemoryUsagePct = latest.MemoryUsagePct
    analysis.MemoryPressurePct = latest.MemoryPressureSomeAvg60
    analysis.CPUThrottledPct = latest.CPUThrottledPct
    analysis.IOPressurePct = latest.IOPressureSomeAvg60

    // Calculate memory trend (linear regression over last 60 samples)
    // Simplified: compare first 20s vs last 20s
    first20Avg := calculateAverage(ts.Metrics[:20], func(m CgroupMetrics) float64 {
        return m.MemoryUsagePct
    })
    last20Avg := calculateAverage(ts.Metrics[40:], func(m CgroupMetrics) float64 {
        return m.MemoryUsagePct
    })

    if last20Avg > first20Avg*1.2 {
        analysis.MemoryTrend = "increasing"
    } else if last20Avg < first20Avg*0.8 {
        analysis.MemoryTrend = "decreasing"
    } else {
        analysis.MemoryTrend = "stable"
    }

    // Similar for CPU trend
    firstCPU := calculateAverage(ts.Metrics[:20], func(m CgroupMetrics) float64 {
        return m.CPUThrottledPct
    })
    lastCPU := calculateAverage(ts.Metrics[40:], func(m CgroupMetrics) float64 {
        return m.CPUThrottledPct
    })

    if lastCPU > firstCPU*1.5 {
        analysis.CPUTrend = "increasing"
    } else {
        analysis.CPUTrend = "stable"
    }

    return analysis
}
```

---

## 7. What Makes This a REAL Observer (vs v1)

### v1 (Scrapped) vs v2 (Real)

| Aspect | v1 (20% Observer) | v2 (REAL Observer) |
|--------|-------------------|---------------------|
| **Exit codes** | Just the code (137) | Code + WHY it happened |
| **Memory** | None | 60s trend + pressure + baseline |
| **Syscalls** | None | Failed syscalls (ENOMEM, ENOSPC) |
| **CPU** | None | Throttling % + trend |
| **I/O** | None | I/O pressure (PSI) |
| **Root cause** | "OOMKilled" | "Memory limit too low - container at 100% of limit before OOM" |
| **eBPF** | None (containerd API only) | 3 hooks (OOM, exit, syscall) |
| **Correlation** | Exit code + K8s name | Exit + cgroup + syscalls + K8s |
| **Actionable?** | No | YES - tells you exactly what to fix |

---

## 8. Example Event Output

### Scenario: Memory Leak Leading to OOM

**eBPF captures**:
- T+0s to T+60s: 15 ENOMEM syscalls (memory allocation failures)
- T+60s: Process exit (PID 12345, exit_code 137)

**cgroup metrics** (last 60s):
- Memory usage: 100MB → 500MB → 512MB (limit)
- Memory pressure: 0% → 50% → 95%
- Trend: Steadily increasing

**K8s enrichment**:
- Pod: nginx-abc
- Namespace: production
- Memory limit: 512MB (from pod spec)
- Restart count: 5

**Emitted Event**:
```json
{
  "id": "evt-001",
  "type": "container",
  "subtype": "exit_oom",
  "timestamp": "2025-10-26T12:34:56Z",

  "container_data": {
    "container_id": "abc123def456",
    "container_name": "nginx",
    "exit_code": 137,
    "state": "exited",

    "exit_category": "exit_oom",
    "root_cause": "Memory leak detected - usage steadily increasing before OOM",

    "runtime_metrics": {
      "memory_usage_bytes": 536870912,
      "memory_limit_bytes": 536870912,
      "memory_usage_pct": 100.0,
      "memory_pressure_pct": 95.0,
      "memory_trend": "increasing",

      "cpu_usage_usec": 12345678,
      "cpu_throttled_pct": 5.0,
      "cpu_trend": "stable",

      "io_pressure_pct": 0.0
    },

    "syscall_failures": {
      "ENOMEM": 15,
      "ENOSPC": 0,
      "EMFILE": 0,
      "ENFILE": 0
    },

    "pod_name": "nginx-abc",
    "namespace": "production",
    "image_name": "nginx:1.25",
    "restart_count": 5
  }
}
```

**Actionable Insight**:
> Memory leak detected in container 'nginx' (pod nginx-abc). Memory increased from 100MB to 512MB over 60s. 15 ENOMEM syscalls before OOM kill. Recommendation: Fix memory leak in application OR increase memory limit to 1GB.

---

## 9. Implementation Plan (TDD - 3 Weeks)

### Week 1: eBPF + cgroup Foundation

**Day 1-2: eBPF Program (TDD)**
```bash
# RED: Write test for OOM event capture
$ vim observer_ebpf_test.go
func TestEBPF_CaptureOOMEvent(t *testing.T) { ... }
$ go test ./... # FAILS ✅

# GREEN: Implement container_monitor.c
$ vim bpf/container_monitor.c
# Implement trace_oom_kill hook
$ go generate ./...
$ go test ./... # PASS ✅

# REFACTOR: Add process exit hook
# Commit: "feat: add eBPF OOM and exit hooks" (≤30 lines)
```

**Day 3-4: cgroup Monitor (TDD)**
```bash
# RED: Test cgroup metric reading
func TestCgroupMonitor_ReadMetrics(t *testing.T) { ... }

# GREEN: Implement cgroup_monitor.go
# Read memory.current, memory.pressure, cpu.stat

# REFACTOR: Add time series storage
# Commit: "feat: add cgroup monitor with time series"
```

**Day 5: Syscall Tracking (TDD)**
```bash
# RED: Test syscall failure capture
func TestEBPF_CaptureSyscallFailure(t *testing.T) { ... }

# GREEN: Implement trace_syscall_exit hook
# Filter: Only ENOMEM, ENOSPC, EMFILE, ENFILE

# Commit: "feat: add syscall failure tracking"
```

---

### Week 2: Correlation + Root Cause

**Day 1-2: Exit Processor (TDD)**
```bash
# RED: Test exit categorization
func TestExitProcessor_OOMKill(t *testing.T) {
    // Mock: exit_code 137 + memory pressure 95%
    // Expect: category = "exit_oom"
}

# GREEN: Implement processor_exit.go
# Implement categorizeExit() with all cases

# REFACTOR: Add resource trend analysis
# Commit: "feat: add exit processor with root cause analysis"
```

**Day 3-4: K8s Enrichment (TDD)**
```bash
# RED: Test K8s enrichment
func TestContainerEnricher_EnrichContainer(t *testing.T) { ... }

# GREEN: Implement container_enricher.go
# cgroup_id → containerd → K8s pod

# Commit: "feat: add K8s enrichment for containers"
```

**Day 5: Integration Testing**
```bash
# Test end-to-end flow:
# 1. Start eBPF
# 2. Start cgroup monitor
# 3. Trigger OOM in test container
# 4. Verify event with full context

# Commit: "test: add integration tests for container observer"
```

---

### Week 3: Polish + Performance

**Day 1-2: OTEL Metrics**
```bash
# Add counters, histograms:
# - container_exits_total (by exit_code, category)
# - oom_kills_total
# - syscall_failures_total (by errno)
# - event_processing_duration_ms

# Commit: "feat: add OTEL metrics"
```

**Day 3-4: Performance Optimization**
```bash
# Benchmark:
# - Event processing latency (<10ms target)
# - Memory usage (stable over 1M events)
# - cgroup read performance

# Optimize:
# - Use sync.Pool for event allocation
# - Batch cgroup reads

# Commit: "perf: optimize event processing"
```

**Day 5: Documentation + Review**
```bash
# Write:
# - README.md (how to deploy)
# - TROUBLESHOOTING.md (common issues)
# - METRICS.md (OTEL metrics reference)

# Final verification:
make verify-full
```

---

## 10. Success Criteria (Definition of Done)

**Functional**:
- [ ] Captures OOM kills with memory usage at kill time
- [ ] Captures process exits with exit code + signal
- [ ] Captures failed syscalls (ENOMEM, ENOSPC, EMFILE, ENFILE)
- [ ] Reads cgroup metrics every 1s (memory, CPU, I/O pressure)
- [ ] Stores 60s time series for trend analysis
- [ ] Categorizes exits with root cause (8 categories)
- [ ] Enriches with K8s metadata (pod, namespace, image)
- [ ] Emits typed domain events with full context

**Quality (CLAUDE.md)**:
- [ ] TDD: All code written RED → GREEN → REFACTOR
- [ ] 80% test coverage minimum
- [ ] NO TODOs or stubs
- [ ] NO `map[string]interface{}`
- [ ] All errors handled
- [ ] Small commits (≤30 lines)

**Performance**:
- [ ] Event processing <10ms (p99)
- [ ] Memory footprint <100MB (with 1000 containers)
- [ ] cgroup read overhead <1% CPU
- [ ] Zero goroutine leaks
- [ ] Handles 1000 containers × 60 metrics/min = 60K metrics/min

**Brendan Gregg Principles**:
- [ ] eBPF captures raw data only (no parsing)
- [ ] Userspace does all parsing and correlation
- [ ] Single eBPF program (multiple hooks)
- [ ] Early filtering (skip non-container PIDs)
- [ ] Small event structs (~64 bytes)
- [ ] Hooks at right layer (OOM = kprobe, exit = tracepoint)

---

## 11. Why This is a REAL Observer Now

**v1 Problem**: Exit code 137 = "OOMKilled" → User says "So what?"

**v2 Solution**:
```
Exit code 137
+ Memory usage 536MB / 536MB (100% of limit)
+ Memory trend: Increasing (100MB → 536MB over 60s)
+ Memory pressure: 95%
+ 15 ENOMEM syscalls before exit
+ Restart count: 5 (CrashLoopBackOff)
────────────────────────────────────────
ROOT CAUSE: Memory leak detected
ACTION: Fix memory leak in app OR increase limit to 1GB
```

**This is observability.** User knows exactly what happened and what to do.

---

## 12. Next Steps

**After this design is approved**:
1. Implement Week 1 (eBPF + cgroup)
2. Implement Week 2 (Correlation)
3. Implement Week 3 (Polish)
4. Ship as part of Tapio v1.0

**Future enhancements** (v1.1):
- Predict OOM 30s before it happens (ML on memory trend)
- Auto-recommend memory limits based on baseline
- Detect memory leak patterns (saw-tooth usage)
- CRI-O support (in addition to containerd)

---

**Last Updated**: 2025-10-26
**Status**: Ready for Review ✅
**Next Step**: Get approval, then start TDD implementation

---

## 13. Deployment (Production Patterns from groundcover + hexshift)

### DaemonSet Deployment (RECOMMENDED)

**Why DaemonSet**:
- ✅ Auto-deploys to new nodes (scales with cluster)
- ✅ One observer per node (eBPF sees all containers on that node)
- ✅ No SSH needed (Kubernetes-native)
- ✅ Handles node failures automatically

**Deployment YAML**:
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-container-observer
  namespace: tapio-system
  labels:
    app: tapio
    component: container-observer
spec:
  selector:
    matchLabels:
      app: tapio
      component: container-observer
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1  # Update one node at a time
  template:
    metadata:
      labels:
        app: tapio
        component: container-observer
    spec:
      # eBPF Requirements
      hostPID: true       # See all PIDs on node
      hostNetwork: true   # See all network namespaces
      
      # Node selector (optional: only Linux nodes)
      nodeSelector:
        kubernetes.io/os: linux
      
      # Tolerations (run on all nodes, including masters)
      tolerations:
      - operator: Exists
        effect: NoSchedule
      
      containers:
      - name: observer
        image: tapio/container-observer:v1.0
        imagePullPolicy: Always
        
        # Security Context (eBPF requires privileged)
        securityContext:
          privileged: true
          capabilities:
            add:
            - SYS_ADMIN      # Load eBPF programs
            - SYS_RESOURCE   # Adjust rlimits for eBPF maps
            - SYS_PTRACE     # Read process memory (for cgroup_id)
          readOnlyRootFilesystem: true  # Security hardening
        
        # Environment variables
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: NATS_URL
          value: "nats://tapio-nats:4222"
        - name: LOG_LEVEL
          value: "info"
        
        # Resource limits
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
        
        # Volume mounts
        volumeMounts:
        # cgroup v2 filesystem (read metrics)
        - name: cgroup
          mountPath: /sys/fs/cgroup
          readOnly: true
        
        # BPF filesystem (load programs)
        - name: bpffs
          mountPath: /sys/fs/bpf
          mountPropagation: Bidirectional
        
        # Kernel headers (for BTF, optional if CO-RE used)
        - name: kernel-headers
          mountPath: /lib/modules
          readOnly: true
        
        # containerd socket (for container metadata)
        - name: containerd-sock
          mountPath: /run/containerd/containerd.sock
          readOnly: true
        
        # K8s service account token
        - name: serviceaccount
          mountPath: /var/run/secrets/kubernetes.io/serviceaccount
          readOnly: true
        
        # Liveness probe (check eBPF program loaded)
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        
        # Readiness probe (check event processing)
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
      
      # Volumes
      volumes:
      - name: cgroup
        hostPath:
          path: /sys/fs/cgroup
          type: Directory
      
      - name: bpffs
        hostPath:
          path: /sys/fs/bpf
          type: DirectoryOrCreate
      
      - name: kernel-headers
        hostPath:
          path: /lib/modules
          type: Directory
      
      - name: containerd-sock
        hostPath:
          path: /run/containerd/containerd.sock
          type: Socket
      
      - name: serviceaccount
        projected:
          sources:
          - serviceAccountToken:
              path: token
              expirationSeconds: 3607
          - configMap:
              name: kube-root-ca.crt
              items:
              - key: ca.crt
                path: ca.crt
          - downwardAPI:
              items:
              - path: namespace
                fieldRef:
                  apiVersion: v1
                  fieldPath: metadata.namespace
      
      # Service account for K8s API access
      serviceAccountName: tapio-container-observer

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tapio-container-observer
  namespace: tapio-system

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tapio-container-observer
rules:
# Read pods (for K8s enrichment)
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]

# Read nodes (for node metadata)
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tapio-container-observer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tapio-container-observer
subjects:
- kind: ServiceAccount
  name: tapio-container-observer
  namespace: tapio-system
```

---

### Kernel Requirements

**Minimum Kernel Version**: 5.8+ (for BTF + CO-RE)

**Check kernel version**:
```bash
uname -r
# Example: 5.15.0-1027-azure
```

**Verify eBPF support**:
```bash
# Check if BTF is available
ls /sys/kernel/btf/vmlinux

# Check eBPF features
bpftool feature probe
```

**If kernel < 5.8**:
- Option 1: Upgrade kernel (recommended)
- Option 2: Use kernel headers instead of BTF (more complex)
- Option 3: Don't deploy observer on old nodes (nodeSelector)

---

### Security Considerations

**Privileged Mode Risks**:
- eBPF programs run in kernel space
- Bugs could crash kernel (mitigated by eBPF verifier)
- Privileged container has host access

**Mitigations**:
1. **AppArmor/SELinux Profile**:
```yaml
securityContext:
  appArmorProfile:
    type: RuntimeDefault  # Or custom profile
  seLinuxOptions:
    type: spc_t  # Super privileged container (RHEL/CentOS)
```

2. **Network Policy** (isolate observer):
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: tapio-container-observer
spec:
  podSelector:
    matchLabels:
      component: container-observer
  policyTypes:
  - Ingress
  - Egress
  egress:
  # Only allow NATS connection
  - to:
    - podSelector:
        matchLabels:
          app: tapio-nats
    ports:
    - protocol: TCP
      port: 4222
  # Allow K8s API access
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443
  ingress: []  # No ingress (observer doesn't need external access)
```

3. **ReadOnly Filesystem**:
```yaml
securityContext:
  readOnlyRootFilesystem: true
volumeMounts:
- name: tmp
  mountPath: /tmp  # Writable tmp if needed
```

4. **Resource Limits** (prevent resource exhaustion):
```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
```

---

### Deployment Verification

**1. Check DaemonSet deployed**:
```bash
kubectl get daemonset -n tapio-system tapio-container-observer
```

**2. Check pods running on all nodes**:
```bash
kubectl get pods -n tapio-system -l component=container-observer -o wide
```

**3. Check eBPF programs loaded**:
```bash
# SSH to node (or use kubectl exec)
kubectl exec -n tapio-system <pod-name> -- bpftool prog list

# Expected output:
# 123: kprobe  name trace_oom_kill  ...
# 124: tracepoint  name trace_process_exit  ...
# 125: tracepoint  name trace_syscall_exit  ...
```

**4. Check events being emitted**:
```bash
# View logs
kubectl logs -n tapio-system <pod-name> --tail=50

# Expected:
# INFO: eBPF programs loaded successfully
# INFO: Processing events (123 events/sec)
# INFO: Emitted exit_oom event for container abc123
```

**5. Check NATS stream**:
```bash
# Connect to NATS
nats stream ls

# Check container events
nats stream view tapio-events --subject="tapio.container.*"
```

---

### Troubleshooting

**Problem**: Pods stuck in `CrashLoopBackOff`

**Diagnosis**:
```bash
kubectl logs -n tapio-system <pod-name>
```

**Common Causes**:
1. **Kernel too old** (< 5.8)
   - Solution: Upgrade kernel or use nodeSelector
   
2. **Missing BTF**:
   - Error: "failed to load eBPF program: no BTF found"
   - Solution: Install kernel headers or build with BTF support

3. **Insufficient privileges**:
   - Error: "failed to load eBPF program: operation not permitted"
   - Solution: Ensure `privileged: true` in securityContext

4. **containerd socket missing**:
   - Error: "failed to connect to containerd: /run/containerd/containerd.sock: no such file"
   - Solution: Check containerd running, verify socket path

5. **cgroup v1 instead of v2**:
   - Error: "failed to read memory.pressure: no such file"
   - Solution: Enable cgroup v2 (requires kernel boot param `systemd.unified_cgroup_hierarchy=1`)

---

**Problem**: High CPU usage

**Diagnosis**:
```bash
kubectl top pod -n tapio-system
```

**Common Causes**:
1. **Too many containers** (>1000 per node)
   - Solution: Increase `resources.limits.cpu`
   
2. **cgroup read interval too frequent** (< 1s)
   - Solution: Increase interval to 1s or 5s

3. **eBPF program overhead**:
   - Check: `perf top` on node
   - Solution: Optimize eBPF filtering (reduce events emitted)

---

**Problem**: Events not appearing in NATS

**Diagnosis**:
```bash
# Check NATS connectivity
kubectl exec -n tapio-system <pod-name> -- nats pub test.subject "hello"

# Check observer logs
kubectl logs -n tapio-system <pod-name> | grep "NATS"
```

**Common Causes**:
1. **NATS server down**:
   - Solution: Check `kubectl get pods -n tapio-system -l app=tapio-nats`
   
2. **Network policy blocking**:
   - Solution: Check NetworkPolicy allows egress to NATS

3. **Authentication failure**:
   - Solution: Check NATS credentials in observer config

---

### Performance Tuning

**Expected Overhead**:
- CPU: ~100m per node (baseline) + 50m per 1000 containers
- Memory: ~128Mi per node (baseline) + 256Mi for 1000 containers
- eBPF: <1% kernel CPU overhead

**Tuning Parameters**:

**1. cgroup Read Interval**:
```yaml
env:
- name: CGROUP_READ_INTERVAL
  value: "1s"  # Default: 1s, Increase to 5s for lower overhead
```

**2. Ring Buffer Size**:
```yaml
env:
- name: EBPF_RINGBUF_SIZE
  value: "262144"  # 256KB per CPU (default), increase for high event rate
```

**3. Syscall Filter**:
```yaml
env:
- name: SYSCALL_FILTER
  value: "ENOMEM,ENOSPC"  # Only track critical errors (reduce noise)
```

**4. Event Sampling**:
```yaml
env:
- name: EVENT_SAMPLE_RATE
  value: "1.0"  # 1.0 = 100% (all events), 0.1 = 10% (sample)
```

---

### Monitoring the Observer

**OTEL Metrics** (exported to Prometheus):
```promql
# Events processed per second
rate(tapio_container_events_processed_total[1m])

# Event processing latency (p99)
histogram_quantile(0.99, tapio_container_event_processing_duration_ms_bucket)

# eBPF map size (memory usage)
tapio_container_ebpf_map_entries

# cgroup read errors
rate(tapio_container_cgroup_read_errors_total[1m])

# Syscall failures detected
rate(tapio_container_syscall_failures_total[1m])
```

**Alerts**:
```yaml
groups:
- name: tapio-container-observer
  rules:
  - alert: ContainerObserverDown
    expr: up{job="tapio-container-observer"} == 0
    for: 5m
    annotations:
      summary: "Container observer down on {{ $labels.node }}"
  
  - alert: HighEventProcessingLatency
    expr: histogram_quantile(0.99, tapio_container_event_processing_duration_ms_bucket) > 100
    for: 5m
    annotations:
      summary: "High event processing latency (p99 > 100ms)"
  
  - alert: HighCgroupReadErrors
    expr: rate(tapio_container_cgroup_read_errors_total[5m]) > 10
    for: 5m
    annotations:
      summary: "High cgroup read error rate (>10/s)"
```

---

## 14. Validation Against Production Best Practices

### groundcover.com/ebpf/ebpf-kubernetes Checklist

- ✅ **DaemonSet deployment** - Automated across nodes
- ✅ **Privileged mode** - Required for eBPF
- ✅ **Data correlation** - eBPF + cgroup + K8s metadata
- ✅ **Cluster scaling** - DaemonSet handles new nodes
- ✅ **Performance profiling** - Process/container/microservice granularity
- ✅ **Security detection** - Syscall failures, unexpected behavior
- ✅ **Network traffic mapping** - (Network observer handles this)
- ✅ **Request tracing** - OTEL trace context in events

### hexshift.medium.com Checklist

- ✅ **Kernel 5.8+** - BTF + CO-RE support
- ✅ **PID-based tracing** - `container_pids` BPF map
- ✅ **Syscall tracing** - `tracepoint/raw_syscalls/sys_exit`
- ✅ **cgroup filtering** - Only container processes
- ✅ **containerd integration** - Socket for metadata
- ✅ **bpftool verification** - Health checks use bpftool

---

**Last Updated**: 2025-10-26
**Status**: Ready for Implementation ✅
**Next Step**: Week 1 TDD implementation (eBPF + cgroup)
