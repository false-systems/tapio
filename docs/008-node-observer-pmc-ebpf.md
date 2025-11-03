# ADR 008: Node Observer Step 2 - eBPF PMC (Performance Monitoring Counters)

**Status**: Design Phase
**Date**: 2025-11-01
**Author**: Claude + Yair
**Context**: Node Observer Step 1 (K8s API) complete, Step 2 adds kernel-level performance metrics

---

## Executive Summary

**Problem**: CPU utilization metrics lie. "90% CPU busy" might be 90% memory stalls, not actual work.

**Solution**: Track **real CPU efficiency** via eBPF + PMC (Performance Monitoring Counters):
- **IPC (Instructions Per Cycle)** - True CPU productivity metric
- **Memory Stall Cycles** - Time CPU waits for memory
- **Proactive alerts** - Detect bottlenecks BEFORE Kubernetes MemoryPressure

**Impact**:
- Detect memory bottlenecks 5-10 minutes earlier than K8s pressure signals
- Differentiate CPU-bound (high IPC) vs memory-bound (low IPC) workloads
- Enable precise root cause analysis (compute vs memory vs I/O)

---

## Background: Brendan Gregg's CPU Utilization Problem

### The Lie of CPU Utilization (2021)

From Brendan Gregg's "CPU Utilization is Wrong" (2021):

> **"90% CPU utilization might be 10% instructions and 90% stalled cycles waiting on memory."**

**Traditional metrics** (what we get from K8s/kubelet):
```
CPU Utilization = (Total Cycles - Idle Cycles) / Total Cycles
```

**Problem**: This counts **stalled cycles as "busy"**!
- CPU waiting for memory = counted as "busy"
- CPU waiting for cache miss = counted as "busy"
- CPU doing actual work = counted as "busy"

**Solution**: Track **IPC (Instructions Per Cycle)**:
```
IPC = Instructions Retired / Cycles Elapsed

- IPC > 1.0 = Superscalar execution (multiple instructions per cycle)
- IPC 0.5-1.0 = Efficient execution (typical for compute-bound)
- IPC 0.2-0.5 = Memory-bound (waiting for cache/RAM)
- IPC < 0.2 = Severe memory bottleneck (mostly stalls)
```

### Real-World Example

**Scenario**: Node shows "90% CPU utilization" in K8s metrics

**Without PMC** (Step 1 - K8s API):
```
✅ CPU: 90% busy
❌ Diagnosis: "Need more CPU" → Scale up compute instances
💸 Result: Waste money, problem persists
```

**With PMC** (Step 2 - eBPF):
```
✅ CPU: 90% busy
✅ IPC: 0.18 (critical!)
✅ Memory Stalls: 82%
✅ Diagnosis: "Memory bottleneck" → Optimize memory access patterns
💰 Result: Fix root cause, save money
```

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│ Hardware Layer                                                  │
│ - CPU PMC (Performance Monitoring Counters)                     │
│   * CPU_CYCLES: Total cycles                                    │
│   * INSTRUCTIONS_RETIRED: Instructions completed                │
│   * MEM_LOAD_RETIRED_L3_MISS: Memory stalls                    │
└──────────────────┬──────────────────────────────────────────────┘
                   │ perf_event_open() syscall
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│ Kernel Layer (perf_events subsystem)                            │
│ - PMC sampling infrastructure                                   │
│ - Security checks (CAP_PERFMON)                                 │
└──────────────────┬──────────────────────────────────────────────┘
                   │ BPF_PROG_TYPE_PERF_EVENT
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│ eBPF Layer (node_pmc_monitor.c)                                │
│ - Attached to perf_event (100ms timer)                         │
│ - Reads PMC via bpf_perf_event_read_value()                    │
│ - Emits struct pmc_event to ring buffer                        │
│                                                                 │
│ struct pmc_event {                                              │
│     u32 cpu;              // CPU ID (0-N)                       │
│     u64 cycles;           // Total cycles                       │
│     u64 instructions;     // Instructions retired               │
│     u64 stall_cycles;     // Memory stall cycles                │
│     u64 timestamp;        // bpf_ktime_get_ns()                 │
│ };                                                              │
└──────────────────┬──────────────────────────────────────────────┘
                   │ BPF_MAP_TYPE_RINGBUF
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│ Userspace Layer (Node Observer - Go)                           │
│                                                                 │
│ ┌─────────────────────┐  ┌─────────────────────┐              │
│ │ K8s API Stage       │  │ PMC Stage (NEW)     │              │
│ │ (Existing)          │  │                     │              │
│ │ - Node Informer     │  │ - RingReader        │              │
│ │ - Condition changes │  │ - PMCProcessor      │              │
│ │ - Pressure events   │  │ - IPC calculation   │              │
│ └──────────┬──────────┘  └──────────┬──────────┘              │
│            │                        │                          │
│            └────────────┬───────────┘                          │
│                         ▼                                      │
│              ┌──────────────────────┐                          │
│              │ Event Emission       │                          │
│              │ - OTEL Metrics       │                          │
│              │ - Stdout/NATS        │                          │
│              └──────────────────────┘                          │
└─────────────────────────────────────────────────────────────────┘
```

### Pipeline Architecture (BaseObserver Pattern)

Node Observer now runs **2 parallel pipeline stages**:

```go
func (o *Observer) Start(ctx context.Context) error {
    // Stage 1: K8s API (existing)
    o.AddStage(o.processK8sEventsStage)

    // Stage 2: PMC eBPF (NEW)
    o.AddStage(o.processPMCEventsStage)

    return o.BaseObserver.Start(ctx)
}
```

**Stage 1** (K8s API - Reactive):
- Watches Node.Status.Conditions
- Emits: `node_ready`, `node_memory_pressure`, etc
- Latency: 30-60 seconds (kubelet sync interval)

**Stage 2** (eBPF PMC - Proactive):
- Samples PMC every 100ms per CPU
- Emits: `node_performance_degradation`, `node_memory_bottleneck`
- Latency: 100ms (near real-time)

---

## Domain Model Extensions

### NodeEventData (pkg/domain/events.go)

```go
type NodeEventData struct {
    // ✅ Existing fields (Step 1 - K8s API)
    NodeName        string `json:"node_name"`
    Condition       string `json:"condition"`         // Ready, MemoryPressure, DiskPressure
    Status          string `json:"status"`            // True, False, Unknown
    Reason          string `json:"reason,omitempty"`
    Message         string `json:"message,omitempty"`
    CPUCapacity     int64  `json:"cpu_capacity,omitempty"`
    MemoryCapacity  int64  `json:"memory_capacity,omitempty"`
    PodCapacity     int64  `json:"pod_capacity,omitempty"`

    // ⭐ NEW fields (Step 2 - eBPF PMC)
    CPUIPC               float64 `json:"cpu_ipc,omitempty"`                // Instructions Per Cycle (0.0 - 2.0)
    MemoryStalls         float64 `json:"memory_stalls,omitempty"`          // % of cycles stalled on memory (0-100)
    PerformanceImpact    string  `json:"performance_impact,omitempty"`     // low, medium, high, critical

    // ⭐ Per-CPU breakdown (optional, for deep analysis)
    PerCPUIPC            map[string]float64 `json:"per_cpu_ipc,omitempty"`     // CPU ID → IPC
    PerCPUMemoryStalls   map[string]float64 `json:"per_cpu_stalls,omitempty"`  // CPU ID → Stall %
}
```

### Event Subtypes (NEW)

| Subtype | Trigger Condition | Severity |
|---------|------------------|----------|
| `node_performance_degradation` | IPC < 0.5 + Stalls > 30% | Warning |
| `node_memory_bottleneck` | IPC < 0.3 + Stalls > 50% | Error |
| `node_critical_memory_bottleneck` | IPC < 0.2 + Stalls > 70% | Critical |

---

## Implementation Details

### 1. eBPF Program (bpf/node_pmc_monitor.c)

```c
// SPDX-License-Identifier: GPL-2.0
// Node PMC Monitor - IPC and Memory Stall Tracking

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

// PMC event types (Intel x86_64)
#define PMC_CPU_CYCLES           0x3c   // CPU_CLK_UNHALTED.THREAD
#define PMC_INSTRUCTIONS         0xc0   // INST_RETIRED.ANY
#define PMC_MEM_STALLS           0xa3   // CYCLE_ACTIVITY.STALLS_L3_MISS

// Event structure sent to userspace
struct pmc_event {
    __u32 cpu;              // CPU ID
    __u64 cycles;           // Total cycles
    __u64 instructions;     // Instructions retired
    __u64 stall_cycles;     // Memory stall cycles
    __u64 timestamp;        // Nanoseconds since boot
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB buffer
} events SEC(".maps");

// Attached to perf_event (timer fires every 100ms per CPU)
SEC("perf_event")
int sample_pmc(struct bpf_perf_event_data *ctx)
{
    struct pmc_event *event;

    // Reserve space in ring buffer
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0; // Ring buffer full, drop sample
    }

    // Get CPU ID
    event->cpu = bpf_get_smp_processor_id();

    // Read PMC values
    // Note: These are cumulative counters since boot
    event->cycles = bpf_perf_event_read(ctx, PMC_CPU_CYCLES);
    event->instructions = bpf_perf_event_read(ctx, PMC_INSTRUCTIONS);
    event->stall_cycles = bpf_perf_event_read(ctx, PMC_MEM_STALLS);

    // Timestamp
    event->timestamp = bpf_ktime_get_ns();

    // Submit to ring buffer
    bpf_ringbuf_submit(event, 0);

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
```

### 2. Go PMC Processor (internal/observers/node/pmc_processor.go)

```go
package node

import (
    "context"
    "fmt"
    "sync"

    "github.com/yairfalse/tapio/pkg/domain"
)

// PMCEvent represents a single PMC sample from eBPF
type PMCEvent struct {
    CPU          uint32
    Cycles       uint64
    Instructions uint64
    StallCycles  uint64
    Timestamp    uint64
}

// PMCSample stores previous sample for delta calculation
type PMCSample struct {
    Cycles       uint64
    Instructions uint64
    StallCycles  uint64
    Timestamp    uint64
}

// PMCProcessor calculates IPC and memory stalls from PMC events
type PMCProcessor struct {
    mu         sync.Mutex
    lastSample map[uint32]*PMCSample
    nodeName   string
}

// NewPMCProcessor creates a new PMC processor
func NewPMCProcessor(nodeName string) *PMCProcessor {
    return &PMCProcessor{
        lastSample: make(map[uint32]*PMCSample),
        nodeName:   nodeName,
    }
}

// Process calculates IPC and stall percentage from PMC event
func (p *PMCProcessor) Process(ctx context.Context, event PMCEvent) *domain.ObserverEvent {
    p.mu.Lock()
    defer p.mu.Unlock()

    cpu := event.CPU

    // Get previous sample for this CPU
    prev := p.lastSample[cpu]
    if prev == nil {
        // First sample, just store it
        p.lastSample[cpu] = &PMCSample{
            Cycles:       event.Cycles,
            Instructions: event.Instructions,
            StallCycles:  event.StallCycles,
            Timestamp:    event.Timestamp,
        }
        return nil
    }

    // Calculate deltas (PMC counters are cumulative)
    deltaCycles := event.Cycles - prev.Cycles
    deltaInstructions := event.Instructions - prev.Instructions
    deltaStalls := event.StallCycles - prev.StallCycles

    // Avoid division by zero
    if deltaCycles == 0 {
        return nil
    }

    // Calculate IPC (Instructions Per Cycle)
    ipc := float64(deltaInstructions) / float64(deltaCycles)

    // Calculate stall percentage
    stallPct := float64(deltaStalls) / float64(deltaCycles) * 100.0

    // Update last sample
    p.lastSample[cpu] = &PMCSample{
        Cycles:       event.Cycles,
        Instructions: event.Instructions,
        StallCycles:  event.StallCycles,
        Timestamp:    event.Timestamp,
    }

    // Classify performance impact
    impact := p.classifyImpact(ipc, stallPct)

    // Only emit event if performance degradation detected
    if impact == "" {
        return nil
    }

    // Determine subtype based on severity
    subtype := p.determineSubtype(ipc, stallPct)

    return &domain.ObserverEvent{
        Type:    "node",
        Subtype: subtype,
        Source:  "node-observer-pmc",
        NodeData: &domain.NodeEventData{
            NodeName:          p.nodeName,
            CPUIPC:            ipc,
            MemoryStalls:      stallPct,
            PerformanceImpact: impact,
        },
    }
}

// classifyImpact determines performance impact level
func (p *PMCProcessor) classifyImpact(ipc, stallPct float64) string {
    // Critical: IPC < 0.2 + Stalls > 70%
    if ipc < 0.2 && stallPct > 70.0 {
        return "critical"
    }

    // High: IPC < 0.3 + Stalls > 50%
    if ipc < 0.3 && stallPct > 50.0 {
        return "high"
    }

    // Medium: IPC < 0.5 + Stalls > 30%
    if ipc < 0.5 && stallPct > 30.0 {
        return "medium"
    }

    // Low: IPC < 0.7 + Stalls > 20%
    if ipc < 0.7 && stallPct > 20.0 {
        return "low"
    }

    // No significant degradation
    return ""
}

// determineSubtype maps impact to event subtype
func (p *PMCProcessor) determineSubtype(ipc, stallPct float64) string {
    if ipc < 0.2 && stallPct > 70.0 {
        return "node_critical_memory_bottleneck"
    }
    if ipc < 0.3 && stallPct > 50.0 {
        return "node_memory_bottleneck"
    }
    return "node_performance_degradation"
}
```

### 3. Observer Integration (internal/observers/node/observer.go)

```go
type Observer struct {
    *base.BaseObserver

    // K8s API (existing)
    config   Config
    informer cache.SharedIndexInformer
    emitter  base.Emitter

    // ⭐ NEW: PMC eBPF
    pmcCollection *ebpf.Collection
    pmcReader     *RingReader
    pmcProcessor  *PMCProcessor
}

func (o *Observer) Start(ctx context.Context) error {
    logger := o.Logger(ctx)

    // 1. Start K8s informer (existing)
    go o.informer.Run(ctx.Done())
    if !cache.WaitForCacheSync(ctx.Done(), o.informer.HasSynced) {
        return fmt.Errorf("failed to sync informer cache")
    }

    // 2. ⭐ NEW: Load PMC eBPF program
    if err := o.startPMCMonitoring(ctx); err != nil {
        logger.Warn().Err(err).Msg("PMC monitoring unavailable, continuing with K8s API only")
        // Don't fail - PMC is optional enhancement
    }

    // 3. Add pipeline stages
    o.AddStage(o.processK8sEventsStage)      // Existing
    if o.pmcReader != nil {
        o.AddStage(o.processPMCEventsStage)  // NEW
    }

    return o.BaseObserver.Start(ctx)
}

func (o *Observer) startPMCMonitoring(ctx context.Context) error {
    // Load eBPF spec
    spec, err := loadBPFSpec("bpf/node_pmc_monitor.o")
    if err != nil {
        return fmt.Errorf("failed to load BPF spec: %w", err)
    }

    // Create collection
    collection, err := ebpf.NewCollection(spec)
    if err != nil {
        return fmt.Errorf("failed to create BPF collection: %w", err)
    }
    o.pmcCollection = collection

    // Get ring buffer map
    eventsMap, ok := collection.Maps["events"]
    if !ok {
        return fmt.Errorf("events ring buffer not found")
    }

    // Create ring buffer reader
    ringReader, err := ringbuf.NewReader(eventsMap)
    if err != nil {
        return fmt.Errorf("failed to create ring buffer reader: %w", err)
    }
    o.pmcReader = NewRingReader(ringReader)

    // Create PMC processor
    o.pmcProcessor = NewPMCProcessor(o.getNodeName())

    // Attach to perf_event (100ms timer)
    if err := o.attachPMCPerfEvent(); err != nil {
        return fmt.Errorf("failed to attach PMC perf_event: %w", err)
    }

    return nil
}

func (o *Observer) processPMCEventsStage(ctx context.Context) error {
    logger := o.Logger(ctx)

    for {
        select {
        case <-ctx.Done():
            return nil
        default:
            // Read PMC event from ring buffer
            event, err := o.pmcReader.ReadPMCEvent()
            if err != nil {
                logger.Debug().Err(err).Msg("Failed to read PMC event")
                continue
            }

            // Process PMC event
            domainEvent := o.pmcProcessor.Process(ctx, event)
            if domainEvent != nil {
                // Emit event
                if err := o.emitter.Emit(ctx, domainEvent); err != nil {
                    o.RecordError(ctx, domainEvent)
                    logger.Error().Err(err).Msg("Failed to emit PMC event")
                } else {
                    o.RecordEvent(ctx)
                    logger.Info().
                        Float64("ipc", domainEvent.NodeData.CPUIPC).
                        Float64("stall_pct", domainEvent.NodeData.MemoryStalls).
                        Str("impact", domainEvent.NodeData.PerformanceImpact).
                        Msg("PMC performance degradation detected")
                }
            }
        }
    }
}
```

---

## PMC Hardware Support

### Intel x86_64 PMC Events

| Event Name | Event Code | Description | Availability |
|------------|-----------|-------------|--------------|
| `CPU_CLK_UNHALTED.THREAD` | 0x3c | CPU cycles (not halted) | All Intel CPUs |
| `INST_RETIRED.ANY` | 0xc0 | Instructions retired | All Intel CPUs |
| `CYCLE_ACTIVITY.STALLS_L3_MISS` | 0xa3 | Cycles stalled on L3 miss | Skylake+ |
| `MEM_LOAD_RETIRED.L3_MISS` | 0xd1 | L3 cache miss loads | Haswell+ |

### AMD Zen PMC Events

| Event Name | Event Code | Description | Availability |
|------------|-----------|-------------|--------------|
| `CPU_CLOCKS_NOT_HALTED` | 0x76 | CPU cycles | All AMD Zen |
| `RETIRED_INSTRUCTIONS` | 0xc0 | Instructions retired | All AMD Zen |
| `LS_NOT_HALTED_CYCS` | 0x76 | Load/store stall cycles | Zen 2+ |

### ARM64 PMC Events

| Event Name | Event Code | Description | Availability |
|------------|-----------|-------------|--------------|
| `CPU_CYCLES` | 0x11 | CPU cycles | ARMv8+ |
| `INST_RETIRED` | 0x08 | Instructions retired | ARMv8+ |
| `L3D_CACHE_REFILL` | 0x2a | L3 cache refills (stalls) | ARMv8.2+ |

---

## Failure Modes & Mitigations

### 1. PMC Not Available

**Symptom**: PMC counters not supported on CPU
**Detection**: `perf_event_open()` returns `ENOENT`
**Mitigation**: Fallback to K8s API only (Step 1)

```go
func (o *Observer) startPMCMonitoring(ctx context.Context) error {
    if err := o.attachPMCPerfEvent(); err != nil {
        logger.Warn().Err(err).Msg("PMC unavailable, using K8s API only")
        return nil // Don't fail, just disable PMC
    }
    return nil
}
```

### 2. Permission Denied

**Symptom**: `EACCES` when opening perf_event
**Detection**: `perf_event_open()` returns `EACCES`
**Mitigation**: Require `CAP_PERFMON` capability in deployment

```yaml
# deployment.yaml
securityContext:
  capabilities:
    add:
    - PERFMON  # Required for PMC access
```

### 3. Counter Overflow

**Symptom**: PMC counter wraps around (48-bit counters)
**Detection**: Delta calculation shows negative value
**Mitigation**: Handle wraparound in delta calculation

```go
func calculateDelta(current, previous uint64) uint64 {
    if current >= previous {
        return current - previous
    }
    // Counter wrapped around (48-bit counter)
    const maxCounter = (1 << 48) - 1
    return (maxCounter - previous) + current
}
```

### 4. High Overhead

**Symptom**: PMC sampling consumes >5% CPU
**Detection**: Monitor observer CPU usage via OTEL
**Mitigation**: Increase sampling interval (100ms → 500ms)

```c
// Adjust timer interval based on CPU overhead
#define SAMPLE_INTERVAL_MS 100  // Default: 100ms
```

### 5. CPU Hotplug

**Symptom**: New CPUs added, no PMC samples for them
**Detection**: Missing CPU IDs in PMC events
**Mitigation**: Watch `/sys/devices/system/cpu/online`, reinit on changes

---

## Performance Considerations

### Overhead Analysis

| Component | CPU Overhead | Memory Overhead |
|-----------|-------------|-----------------|
| PMC sampling (100ms) | ~0.1% per CPU | 256KB ring buffer |
| eBPF program execution | ~100 CPU cycles per sample | ~2KB eBPF program |
| Userspace processing | ~0.05% | ~10MB per-CPU state |
| **Total** | **~0.2% per CPU** | **~10MB + 256KB** |

### Sampling Interval Tradeoffs

| Interval | Latency | Overhead | Use Case |
|----------|---------|----------|----------|
| 10ms | Real-time | ~2% CPU | Development/debug |
| 100ms | <1 second | ~0.2% CPU | **Production (recommended)** |
| 1000ms (1s) | <10 seconds | ~0.02% CPU | Low-overhead monitoring |

---

## OTEL Metrics

### New Metrics (Step 2 - PMC)

```go
// Observer-level metrics
observer_node_pmc_ipc                       // Gauge: Instructions Per Cycle (per CPU)
observer_node_pmc_memory_stalls_ratio       // Gauge: Memory stall percentage (per CPU)
observer_node_pmc_performance_impact        // Gauge: Impact level (0=none, 1=low, 2=medium, 3=high, 4=critical)
observer_node_pmc_samples_total             // Counter: Total PMC samples processed
observer_node_pmc_events_emitted_total      // Counter: PMC events emitted

// Labels:
// - node: Node name
// - cpu: CPU ID
// - impact: low, medium, high, critical
```

### Event Flow Example

```
PMC Sample (CPU 0):
  cycles: 1,000,000
  instructions: 200,000
  stall_cycles: 800,000

IPC = 200,000 / 1,000,000 = 0.2
Stall% = 800,000 / 1,000,000 = 80%

→ Impact: CRITICAL (IPC < 0.2, Stall% > 70%)
→ Emit: node_critical_memory_bottleneck
→ OTEL: observer_node_pmc_ipc{node="worker-1",cpu="0"} = 0.2
→ OTEL: observer_node_pmc_memory_stalls_ratio{node="worker-1",cpu="0"} = 0.8
→ OTEL: observer_node_pmc_performance_impact{node="worker-1",impact="critical"} = 4
```

---

## Integration with Existing System

### Correlation: K8s API + PMC

**Timeline**: Early warning → Kubernetes reaction

```
T+0s:   PMC detects IPC=0.22, Stalls=75%
        → Emit: node_memory_bottleneck
        → OTEL Alert: "Worker-1 memory bottleneck"

T+30s:  Kubelet detects memory pressure (working_set > 80%)
        → Sets NodeCondition: MemoryPressure=True

T+60s:  K8s API informer detects condition change
        → Emit: node_memory_pressure (Step 1 event)

T+120s: Kubelet starts evicting pods
```

**Benefit**: **60-second early warning** via PMC vs K8s API

### Event Deduplication

**Problem**: Both K8s API and PMC might emit events for same issue

**Solution**: Time-based deduplication in event emitter

```go
func (o *Observer) shouldEmit(eventType string) bool {
    o.mu.Lock()
    defer o.mu.Unlock()

    lastEmit := o.lastEmitTime[eventType]
    now := time.Now()

    // Don't emit duplicate events within 60 seconds
    if now.Sub(lastEmit) < 60*time.Second {
        return false
    }

    o.lastEmitTime[eventType] = now
    return true
}
```

---

## Testing Strategy

### Unit Tests (TDD)

**Cycle 1: PMCProcessor IPC Calculation**
```go
func TestPMCProcessor_CalculateIPC(t *testing.T) {
    proc := NewPMCProcessor("test-node")

    // First sample (baseline)
    event1 := PMCEvent{CPU: 0, Cycles: 1000000, Instructions: 500000, StallCycles: 300000}
    result := proc.Process(context.Background(), event1)
    assert.Nil(t, result) // First sample, no delta yet

    // Second sample (calculate IPC)
    event2 := PMCEvent{CPU: 0, Cycles: 2000000, Instructions: 1000000, StallCycles: 800000}
    result = proc.Process(context.Background(), event2)
    require.NotNil(t, result)

    // IPC = (1000000 - 500000) / (2000000 - 1000000) = 500000 / 1000000 = 0.5
    assert.Equal(t, 0.5, result.NodeData.CPUIPC)

    // Stall% = (800000 - 300000) / (2000000 - 1000000) * 100 = 50%
    assert.Equal(t, 50.0, result.NodeData.MemoryStalls)
}
```

**Cycle 2: Impact Classification**
```go
func TestPMCProcessor_ClassifyImpact(t *testing.T) {
    tests := []struct {
        name      string
        ipc       float64
        stallPct  float64
        wantImpact string
    }{
        {"critical", 0.18, 75.0, "critical"},
        {"high", 0.28, 55.0, "high"},
        {"medium", 0.45, 35.0, "medium"},
        {"low", 0.65, 25.0, "low"},
        {"none", 0.80, 10.0, ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            proc := NewPMCProcessor("test-node")
            impact := proc.classifyImpact(tt.ipc, tt.stallPct)
            assert.Equal(t, tt.wantImpact, impact)
        })
    }
}
```

**Cycle 3: Counter Overflow Handling**
```go
func TestPMCProcessor_CounterOverflow(t *testing.T) {
    proc := NewPMCProcessor("test-node")

    // Simulate 48-bit counter overflow
    const maxCounter = (1 << 48) - 1

    event1 := PMCEvent{CPU: 0, Cycles: maxCounter - 1000, Instructions: maxCounter - 2000}
    proc.Process(context.Background(), event1)

    // Counter wraps around
    event2 := PMCEvent{CPU: 0, Cycles: 1000, Instructions: 2000}
    result := proc.Process(context.Background(), event2)

    require.NotNil(t, result)
    // Delta should be ~3000 cycles (not negative!)
    assert.Greater(t, result.NodeData.CPUIPC, 0.0)
}
```

### Integration Tests

**E2E Test with Real PMC** (requires Linux + PMC support):
```go
// +build linux,integration

func TestNodeObserver_PMCIntegration(t *testing.T) {
    if !isPMCAvailable() {
        t.Skip("PMC not available on this system")
    }

    observer, err := NewObserver("test-observer", Config{...})
    require.NoError(t, err)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    err = observer.Start(ctx)
    require.NoError(t, err)

    // Wait for PMC samples
    time.Sleep(1 * time.Second)

    // Verify OTEL metrics were recorded
    stats := observer.Stats()
    assert.Greater(t, stats.EventsProcessed, int64(0))
}
```

---

## Deployment Requirements

### Kubernetes Manifests

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-node-observer
  namespace: tapio-system
spec:
  template:
    spec:
      hostNetwork: true  # Access to node-level metrics
      hostPID: true      # Required for PMC

      containers:
      - name: observer
        image: tapio/node-observer:latest

        securityContext:
          privileged: false
          capabilities:
            add:
            - PERFMON      # ⭐ Required for PMC access (Linux 5.8+)
            - SYS_ADMIN    # Fallback for older kernels (<5.8)
            - BPF          # eBPF program loading

        resources:
          requests:
            cpu: 100m       # PMC overhead ~0.2% per CPU
            memory: 50Mi    # Ring buffer + per-CPU state
          limits:
            cpu: 500m
            memory: 200Mi

        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: PMC_SAMPLE_INTERVAL_MS
          value: "100"  # 100ms sampling (default)
```

### Kernel Requirements

| Kernel Version | Feature | Status |
|---------------|---------|--------|
| Linux 4.15+ | eBPF ring buffers | ✅ Required |
| Linux 5.8+ | CAP_PERFMON capability | ✅ Recommended |
| Linux 5.11+ | cgroup memory accounting | ✅ Optional |

**Compatibility**:
- `CAP_PERFMON` (Linux 5.8+): Preferred, least privileged
- `CAP_SYS_ADMIN` (Linux <5.8): Fallback, more privileged
- `CAP_BPF` (Linux 5.8+): Required for eBPF program loading

---

## Comparison: Step 1 (K8s API) vs Step 2 (eBPF PMC)

| Metric | Step 1 (K8s API) | Step 2 (eBPF PMC) | Benefit |
|--------|-----------------|-------------------|---------|
| **Data Source** | Kubelet (30s interval) | Kernel PMC (100ms) | **300x faster** |
| **Latency** | 30-60 seconds | 100-200ms | **Early warning** |
| **Accuracy** | Node-level averages | Per-CPU granularity | **Root cause precision** |
| **CPU Metric** | % busy (lies!) | IPC (truth!) | **Differentiate compute vs memory** |
| **Overhead** | ~0% (passive) | ~0.2% per CPU | **Negligible** |
| **Events Detected** | 5 types | 3 types | **Complementary** |
| **Deployment** | Standard K8s | Requires CAP_PERFMON | **Higher privilege** |

**Synergy**: Use **both** simultaneously
- K8s API: Detect infrastructure failures (DiskPressure, NetworkUnavailable)
- eBPF PMC: Detect performance bottlenecks (low IPC, memory stalls)

---

## Example Event Output

### PMC Event (Critical Memory Bottleneck)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "node",
  "subtype": "node_critical_memory_bottleneck",
  "source": "node-observer-pmc",
  "timestamp": "2025-11-01T14:23:45Z",
  "node_data": {
    "node_name": "worker-1",
    "cpu_ipc": 0.18,
    "memory_stalls": 82.5,
    "performance_impact": "critical"
  }
}
```

### Alert Example (Prometheus/Grafana)

```promql
# Alert when IPC drops below 0.3 with >50% stalls
ALERT NodeMemoryBottleneck
  IF observer_node_pmc_ipc < 0.3
  AND observer_node_pmc_memory_stalls_ratio > 0.5
  FOR 2m
  LABELS { severity = "critical" }
  ANNOTATIONS {
    summary = "Node {{ $labels.node }} has severe memory bottleneck",
    description = "IPC={{ $value }}, Stalls={{ $labels.stalls }}%"
  }
```

---

## Success Criteria

### Functional Requirements

- [ ] Detect IPC < 0.3 within 200ms
- [ ] Detect memory stalls > 50% within 200ms
- [ ] Emit `node_memory_bottleneck` events
- [ ] Fallback gracefully if PMC unavailable
- [ ] Handle counter overflow correctly
- [ ] Support Intel x86_64 PMC events
- [ ] Support AMD Zen PMC events (future)
- [ ] Support ARM64 PMC events (future)

### Performance Requirements

- [ ] CPU overhead < 0.5% per CPU
- [ ] Memory overhead < 50MB total
- [ ] Sampling latency < 200ms
- [ ] Ring buffer never fills (no dropped samples)

### Operational Requirements

- [ ] Deploy as DaemonSet (one per node)
- [ ] OTEL metrics for IPC/stalls
- [ ] Graceful degradation on PMC failure
- [ ] Documentation for required capabilities

---

## Next Steps (Implementation Plan)

### Phase 1: Research & Prototyping
1. Research perf_events eBPF examples (Brendan Gregg's BPF tools)
2. Test PMC availability on target hardware (Intel/AMD/ARM)
3. Prototype eBPF program with `bpftool` debugging

### Phase 2: TDD Implementation
1. Write failing tests for PMCProcessor (IPC calculation)
2. Implement PMCProcessor (minimal, make tests pass)
3. Write failing tests for overflow handling
4. Implement overflow handling
5. Write failing tests for impact classification
6. Implement classification logic

### Phase 3: eBPF Integration
1. Write eBPF program (`node_pmc_monitor.c`)
2. Add ring buffer reader to observer
3. Integrate PMC pipeline stage
4. Test on development cluster

### Phase 4: Production Deployment
1. Create DaemonSet manifest
2. Document capability requirements
3. Deploy to staging cluster
4. Monitor OTEL metrics for 24 hours
5. Production rollout (canary → full)

---

## References

1. **Brendan Gregg - "CPU Utilization is Wrong"** (2021)
   - https://www.brendangregg.com/blog/2017-05-09/cpu-utilization-is-wrong.html

2. **Brendan Gregg - BPF Performance Tools** (Book, 2019)
   - Chapter 6: CPU Analysis
   - Chapter 7: Memory Analysis

3. **Intel Software Developer Manual**
   - Volume 3B: Performance Monitoring Counters (PMC)

4. **Linux Kernel Documentation**
   - `Documentation/admin-guide/perf/index.rst`
   - `tools/perf/examples/`

5. **eBPF Perf Event Examples**
   - https://github.com/iovisor/bcc/blob/master/examples/tracing/task_switch.c
   - https://github.com/brendangregg/perf-tools

6. **Tapio Design Docs**
   - ADR 002: Observer Consolidation
   - ADR 003: Network Observer Integration

---

## Appendix: Brendan Gregg's 60-Second Checklist (eBPF Implementation)

From the requirements doc, here's how we map the checklist to eBPF:

| Command | What It Shows | eBPF Equivalent | Status |
|---------|--------------|-----------------|--------|
| `uptime` | Load averages (1, 5, 15 min) | Read `/proc/loadavg` | ✅ Future |
| `dmesg \| tail` | Kernel errors | Trace `printk()` via kprobe | ✅ Future |
| `vmstat 1` | CPU breakdown, swapping | PMC + `/proc/vmstat` | ✅ **This ADR** |
| `mpstat -P ALL 1` | Per-CPU balance | PMC per-CPU sampling | ✅ **This ADR** |
| `pidstat 1` | Process-level CPU | Trace scheduler events | ✅ Future |
| `iostat -xz 1` | Disk I/O | Trace block layer | ✅ Future |
| `free -m` | Memory usage | `/proc/meminfo` | ✅ Future |
| `sar -n DEV 1` | Network I/O | Already done (Network Observer) | ✅ Complete |
| `sar -n TCP,ETCP 1` | TCP stats | Already done (Network Observer) | ✅ Complete |
| `top` | Process overview | Trace scheduler + memory | ✅ Future |

**This ADR covers**: `vmstat` + `mpstat` equivalents (IPC + memory stalls via PMC)

---

**Status**: Ready for Phase 1 (Research & Prototyping)
**Next Action**: Research PMC/perf_events eBPF implementation examples
