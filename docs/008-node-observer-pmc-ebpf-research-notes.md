# Node Observer PMC Research Notes

**Date**: 2025-11-01
**Related**: ADR 008 (Node Observer PMC eBPF)
**Status**: Research Complete, Ready for TDD Implementation

---

## Research Summary

Researched eBPF PMC (Performance Monitoring Counter) implementation for Node Observer Step 2. Goal: Track IPC (Instructions Per Cycle) and memory stalls to detect CPU efficiency problems that traditional "% busy" metrics miss.

**Key Insight** (Brendan Gregg 2021):
> "90% CPU utilization might be 10% instructions and 90% stalled cycles waiting on memory."

---

## Implementation Patterns Found

### Pattern 1: BPF_MAP_TYPE_PERF_EVENT_ARRAY (BCC)

**Source**: https://github.com/iovisor/bcc/blob/master/tests/cc/test_perf_event.cc

**eBPF Kernel Code**:
```c
// Declare perf event array (one entry per CPU)
BPF_PERF_ARRAY(pmc_counters, NUM_CPUS);

// Read PMC counter from current CPU
int on_event(void *ctx) {
    u64 cycles = pmc_counters.perf_read(CUR_CPU_IDENTIFIER);

    // Check for errors (negative values -1 to -255)
    if (((s64)cycles < 0) && ((s64)cycles > -256)) {
        return 0;  // Error reading counter
    }

    // Use counter value...
    return 0;
}
```

**Userspace Setup** (C++/BCC):
```cpp
ebpf::BPF bpf;

// Initialize BPF program with NUM_CPUS define
bpf.init(BPF_PROGRAM,
    {"-DNUM_CPUS=" + std::to_string(sysconf(_SC_NPROCESSORS_ONLN))},
    {});

// Open perf event for each CPU
// For PMC: Use PERF_TYPE_HARDWARE with specific event
int pid = -1;  // -1 = all processes
bpf.open_perf_event("pmc_counters",
                    PERF_TYPE_HARDWARE,    // Hardware PMC
                    PERF_COUNT_HW_CPU_CYCLES,  // CPU cycles counter
                    pid);  // Target PID (-1 for system-wide)
```

**Key Points**:
- BPF_MAP_TYPE_PERF_EVENT_ARRAY stores file descriptors from perf_event_open()
- Userspace calls perf_event_open() for each CPU
- eBPF reads counters via bpf_perf_event_read() helper
- Counters are cumulative (need delta calculation in userspace)

---

### Pattern 2: Direct perf_event Attachment (Cilium eBPF)

**Source**: https://eunomia.dev/tutorials/12-profile/

**eBPF Kernel Code**:
```c
// Attached to perf_event via BPF_PROG_TYPE_PERF_EVENT
SEC("perf_event")
int on_perf_sample(struct bpf_perf_event_data *ctx)
{
    int cpu = bpf_get_smp_processor_id();
    u64 timestamp = bpf_ktime_get_ns();

    // ctx contains perf event data (can read PMC values)
    // Emit event to ring buffer
    struct pmc_event event = {
        .cpu = cpu,
        .timestamp = timestamp,
        // ... PMC values
    };

    bpf_ringbuf_output(&events, &event, sizeof(event), 0);
    return 0;
}
```

**Userspace Setup** (Conceptual - from tutorial):
```go
// For each online CPU:
for cpu := range onlineCPUs {
    // Open perf_event for PMC
    fd := perf_event_open(
        PERF_TYPE_HARDWARE,
        PERF_COUNT_HW_CPU_CYCLES,
        cpu,                // CPU ID
        -1,                 // Group FD
        PERF_FLAG_FD_CLOEXEC,
    )

    // Attach eBPF program to perf event
    link := bpf_program__attach_perf_event(prog, fd)
}
```

**Key Points**:
- BPF program attached directly to perf_event (timer-based)
- Fires on sample_period (e.g., every 100ms)
- Can read multiple PMCs in single handler
- More flexible than BPF_PERF_ARRAY pattern

---

## PMC Event Types

### Hardware PMC Events (PERF_TYPE_HARDWARE)

| Event Constant | Description | Intel x86_64 | AMD Zen |
|----------------|-------------|--------------|---------|
| PERF_COUNT_HW_CPU_CYCLES | CPU cycles (not halted) | ✅ CPU_CLK_UNHALTED | ✅ CPU_CLOCKS_NOT_HALTED |
| PERF_COUNT_HW_INSTRUCTIONS | Instructions retired | ✅ INST_RETIRED.ANY | ✅ RETIRED_INSTRUCTIONS |
| PERF_COUNT_HW_CACHE_REFERENCES | Cache references | ✅ | ✅ |
| PERF_COUNT_HW_CACHE_MISSES | Cache misses | ✅ | ✅ |
| PERF_COUNT_HW_BRANCH_INSTRUCTIONS | Branch instructions | ✅ | ✅ |
| PERF_COUNT_HW_BRANCH_MISSES | Branch mispredictions | ✅ | ✅ |

### Raw PMC Events (PERF_TYPE_RAW)

For advanced counters (e.g., memory stalls), use raw event codes:

**Intel Skylake+**:
- `0xa3` - CYCLE_ACTIVITY.STALLS_L3_MISS (memory stall cycles)
- `0xd1` - MEM_LOAD_RETIRED.L3_MISS (L3 cache miss loads)

**AMD Zen 2+**:
- `0x76` - LS_NOT_HALTED_CYCS (load/store stall cycles)

**Example**:
```c
perf_event_open(
    PERF_TYPE_RAW,
    0xa3,  // Intel: CYCLE_ACTIVITY.STALLS_L3_MISS
    cpu,
    -1,
    PERF_FLAG_FD_CLOEXEC
)
```

---

## Implementation Strategy for Tapio

### Recommended Pattern: Direct perf_event Attachment

**Why**:
1. **Simpler**: One perf_event per CPU (not separate map + fd management)
2. **Timer-based**: Fire every 100ms automatically (no manual polling)
3. **Multiple counters**: Read CYCLES + INSTRUCTIONS + STALLS in one handler
4. **Ring buffer**: Already using this pattern (matches container/network observers)

### Architecture

```
┌──────────────────────────────────────────────────┐
│ Userspace (Go)                                   │
│ - Load eBPF program (node_pmc_monitor.c)        │
│ - For each CPU:                                  │
│     perf_event_open(PERF_COUNT_HW_CPU_CYCLES)   │
│     perf_event_open(PERF_COUNT_HW_INSTRUCTIONS) │
│     perf_event_open(PERF_TYPE_RAW, 0xa3)        │ <- Memory stalls
│     attach eBPF prog to events                   │
└──────────────────┬───────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────┐
│ eBPF Kernel (node_pmc_monitor.c)                │
│ SEC("perf_event")                                │
│ int sample_pmc(struct bpf_perf_event_data *ctx)  │
│ {                                                │
│     u32 cpu = bpf_get_smp_processor_id();       │
│     u64 cycles = bpf_perf_event_read_value(...);│
│     u64 instructions = ...;                      │
│     u64 stall_cycles = ...;                      │
│                                                  │
│     struct pmc_event event = {...};              │
│     bpf_ringbuf_output(&events, &event, ...);   │
│ }                                                │
└──────────────────┬───────────────────────────────┘
                   │ Ring Buffer
                   ▼
┌──────────────────────────────────────────────────┐
│ Go PMCProcessor                                  │
│ - Read pmc_event from ring buffer                │
│ - Calculate delta (counters are cumulative)      │
│ - IPC = ΔInstructions / ΔCycles                  │
│ - Stall% = ΔStalls / ΔCycles * 100              │
│ - Emit domain.ObserverEvent if degraded          │
└──────────────────────────────────────────────────┘
```

---

## Helper Functions

### bpf_perf_event_read() vs bpf_perf_event_read_value()

**bpf_perf_event_read()**:
- Returns u64 (counter value only)
- Error codes: -EINVAL, -ENOENT, -E2BIG
- Simpler, for single counter reads

**bpf_perf_event_read_value()**:
- Returns struct { u64 counter; u64 enabled; u64 running; }
- Provides timing info (useful for multiplexing)
- Preferred for accurate measurements

**Example**:
```c
struct bpf_perf_event_value value;
int ret = bpf_perf_event_read_value(ctx, map, index, &value, sizeof(value));
if (ret == 0) {
    u64 counter = value.counter;
    u64 enabled = value.enabled;  // Time counter was enabled
    u64 running = value.running;  // Time counter was actually counting
}
```

---

## Error Handling

### Common Errors

| Error Code | Meaning | Mitigation |
|-----------|---------|------------|
| -EINVAL | Invalid PMC type/event | Check CPU supports event (cpuid) |
| -EACCES | Permission denied | Require CAP_PERFMON or CAP_SYS_ADMIN |
| -ENOENT | PMC not available | Fallback to K8s API only |
| -E2BIG | Buffer too small | Use correct struct size for bpf_perf_event_read_value |

### Detection Pattern

```c
s64 cycles = (s64)bpf_perf_event_read(ctx, &pmc_map, cpu);

// Error codes are negative values in range -1 to -255
if (cycles < 0 && cycles > -256) {
    // Error reading PMC
    return 0;
}

// Valid counter value
process_counter(cycles);
```

---

## Userspace Setup (Go + Cilium eBPF)

### Load eBPF Program

```go
import (
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/link"
    "golang.org/x/sys/unix"
)

// Load eBPF collection
spec, err := ebpf.LoadCollectionSpec("node_pmc_monitor.o")
coll, err := ebpf.NewCollection(spec)

// Get eBPF program
prog := coll.Programs["sample_pmc"]
```

### Attach to perf_event (Per CPU)

```go
func attachPMCPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
    // Configure perf event
    attr := &unix.PerfEventAttr{
        Type:        unix.PERF_TYPE_HARDWARE,
        Config:      unix.PERF_COUNT_HW_CPU_CYCLES,
        Sample_type: unix.PERF_SAMPLE_RAW,
        Sample:      100, // Sample every 100ms (100 * 1ms)
        Wakeup:      1,
    }

    // Open perf event for specific CPU
    fd, err := unix.PerfEventOpen(attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
    if err != nil {
        return nil, fmt.Errorf("perf_event_open failed: %w", err)
    }

    // Attach eBPF program to perf event
    link, err := link.AttachRawLink(link.RawLinkOptions{
        Target:  fd,
        Program: prog,
        Attach:  ebpf.AttachPerfEvent,
    })
    if err != nil {
        unix.Close(fd)
        return nil, fmt.Errorf("attach failed: %w", err)
    }

    return link, nil
}
```

---

## Counter Delta Calculation

PMC counters are **cumulative** (monotonically increasing), so userspace must calculate deltas:

```go
type PMCSample struct {
    Cycles       uint64
    Instructions uint64
    StallCycles  uint64
    Timestamp    uint64
}

func (p *PMCProcessor) calculateIPC(current, previous *PMCSample) float64 {
    deltaCycles := current.Cycles - previous.Cycles
    deltaInstructions := current.Instructions - previous.Instructions

    if deltaCycles == 0 {
        return 0.0
    }

    return float64(deltaInstructions) / float64(deltaCycles)
}
```

**Overflow Handling** (48-bit counters):
```go
func calculateDelta(current, previous uint64) uint64 {
    if current >= previous {
        return current - previous
    }

    // Counter wrapped around (48-bit hardware counter)
    const maxCounter48bit uint64 = (1 << 48) - 1
    return (maxCounter48bit - previous) + current
}
```

---

## Testing Strategy

### Unit Tests (Mock eBPF)

```go
func TestPMCProcessor_CalculateIPC(t *testing.T) {
    proc := NewPMCProcessor("test-node")

    // Baseline sample
    sample1 := PMCSample{
        Cycles:       1000000,
        Instructions: 500000,
        StallCycles:  300000,
    }
    proc.Process(sample1) // Store baseline

    // Second sample
    sample2 := PMCSample{
        Cycles:       2000000,
        Instructions: 1000000,
        StallCycles:  800000,
    }
    event := proc.Process(sample2)

    // IPC = (1000000 - 500000) / (2000000 - 1000000) = 0.5
    assert.Equal(t, 0.5, event.NodeData.CPUIPC)

    // Stall% = (800000 - 300000) / (2000000 - 1000000) * 100 = 50%
    assert.Equal(t, 50.0, event.NodeData.MemoryStalls)
}
```

### Integration Tests (Real Hardware)

```go
// +build linux,integration

func TestPMC_RealHardware(t *testing.T) {
    if !isPMCAvailable() {
        t.Skip("PMC not available")
    }

    // Load eBPF program
    observer, _ := NewObserver("test", cfg)
    observer.Start(ctx)

    // Wait for samples
    time.Sleep(500 * time.Millisecond)

    // Verify metrics recorded
    stats := observer.Stats()
    assert.Greater(t, stats.EventsProcessed, int64(0))
}
```

---

## Deployment Checklist

### Prerequisites

- [ ] Linux kernel 4.15+ (eBPF ring buffers)
- [ ] Linux kernel 5.8+ (CAP_PERFMON capability)
- [ ] CPU with PMC support (Intel/AMD/ARM)
- [ ] Kubernetes 1.19+ (securityContext.capabilities)

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-node-observer
spec:
  template:
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: observer
        securityContext:
          capabilities:
            add:
            - PERFMON  # Required for PMC (Linux 5.8+)
            - SYS_ADMIN  # Fallback (Linux <5.8)
            - BPF
```

### Verification

```bash
# Check PMC availability
perf list | grep -E "cycles|instructions"

# Test perf_event_open
perf stat -e cycles,instructions sleep 1

# Verify CAP_PERFMON
cat /proc/self/status | grep CapEff
```

---

## References

1. **BCC Test Examples**
   - https://github.com/iovisor/bcc/blob/master/tests/cc/test_perf_event.cc
   - Pattern: BPF_MAP_TYPE_PERF_EVENT_ARRAY

2. **Eunomia eBPF Tutorial**
   - https://eunomia.dev/tutorials/12-profile/
   - Pattern: Direct perf_event attachment

3. **Brendan Gregg PMC Tools**
   - https://github.com/brendangregg/pmc-cloud-tools/blob/master/pmcipc
   - Bash wrapper around `perf stat`

4. **Linux Kernel Documentation**
   - perf_event_open(2) man page
   - Documentation/admin-guide/perf/index.rst

5. **Tapio Architecture**
   - ADR 002: Observer Consolidation
   - ADR 008: Node Observer PMC eBPF (this design)

---

## Next Steps

1. ✅ Research complete
2. 📝 Update ADR 008 with implementation details
3. 🧪 TDD Cycle 1: PMCProcessor unit tests (IPC calculation)
4. 🧪 TDD Cycle 2: Counter overflow handling tests
5. 🔨 Implement PMCProcessor (minimal, make tests pass)
6. 🔨 Write eBPF program (node_pmc_monitor.c)
7. 🔨 Integrate with Node Observer (pipeline stage)
8. 🧪 Integration tests (real hardware)
9. 🚀 Deploy to staging cluster

---

**Status**: Ready for TDD Implementation (Phase 2)
**Confidence**: High - Clear patterns found, multiple working examples
**Risk**: Low - PMC availability detection + graceful fallback planned
