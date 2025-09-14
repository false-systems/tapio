# eBPF CO-RE Migration Plan - Tapio Observers

## Executive Summary
Complete migration plan to make ALL observers CO-RE compliant with proper safety, following Tapio standards (CLAUDE.md).

## Core Standards (Applied to ALL Observers)

### 1. CO-RE Requirements
```bash
# Standard bpf2go command for ALL observers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpf -cc clang-14 ${OBSERVER_NAME} ../bpf_src/${OBSERVER_NAME}.c -- -I../../bpf_common -g -O2 -Wall -Wextra -D__TARGET_ARCH_x86
```

### 2. Standard BPF Header Structure
```c
// EVERY BPF program MUST start with:
#include "vmlinux.h"  // BTF-based kernel types
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Standard safety macros
#define MAX_STACK_DEPTH 20
#define MAX_STRING_SIZE 256
#define TASK_COMM_LEN 16
```

### 3. Standard Safety Template
```c
// EVERY probe read MUST use this pattern:
static __always_inline int safe_probe_read(void *dst, int size, const void *src) {
    if (!src) return -1;
    return bpf_probe_read_kernel(dst, size, src);
}

// EVERY string read MUST use:
static __always_inline int safe_probe_read_str(void *dst, int size, const void *src) {
    if (!src) return -1;
    return bpf_probe_read_kernel_str(dst, size, src);
}
```

### 4. Standard Rate Limiting
```c
// EVERY observer MUST have rate limiting
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct rate_limit);
} rate_limiter SEC(".maps");

struct rate_limit {
    u64 tokens;
    u64 last_refill;
};
```

### 5. Standard Ring Buffer Size
```c
// Standardized sizes for ALL observers
#define RINGBUF_SIZE_LOW    (128 * 1024)  // 128KB - Low volume
#define RINGBUF_SIZE_MEDIUM (256 * 1024)  // 256KB - Medium volume  
#define RINGBUF_SIZE_HIGH   (512 * 1024)  // 512KB - High volume
```

---

## Observer-by-Observer Migration Plan

### 1. DNS Observer
**Priority: P0 (High volume, critical)**
**Current State**: No CO-RE, complete functionality
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] Generate vmlinux.h from BTF
- [ ] Replace all struct accesses with BPF_CORE_READ()
- [ ] Add rate limiting (1000 events/sec)
- [ ] Add sampling (1:10 for high QPS)
- [ ] Replace bpf_probe_read_user with safe wrappers
- [ ] Update bpf2go to use `-target bpf`
- [ ] Add overflow counters
- [ ] Test on kernels 5.4, 5.10, 5.15, 6.0

#### Specific Changes:
```c
// OLD (non-CO-RE)
struct sk_buff *skb = (struct sk_buff *)ctx->skb;
u16 sport = skb->sport;

// NEW (CO-RE)
struct sk_buff *skb = (struct sk_buff *)ctx->skb;
u16 sport = BPF_CORE_READ(skb, sport);
```

---

### 2. Memory Observer
**Priority: P0 (Memory leaks critical)**
**Current State**: Partial CO-RE headers, no usage
**Ring Buffer Size**: HIGH (512KB)

#### Tasks:
- [ ] Convert all allocations tracking to CO-RE
- [ ] Reduce allocation map from 1M to 10K entries
- [ ] Add PID namespace filtering
- [ ] Implement stack dedup with BTF
- [ ] Add OOM prediction metrics
- [ ] Rate limit to 500 events/sec
- [ ] Add memory pressure detection
- [ ] Test with memory stress scenarios

#### Specific Changes:
```c
// Must track allocations with CO-RE
struct alloc_info {
    u64 size;
    u64 timestamp;
    u32 pid;
    u32 tid;
    int stack_id;
};

// Use BPF_CORE_READ for task_struct access
u32 pid = BPF_CORE_READ(task, tgid);
```

---

### 3. Network Observer
**Priority: P0 (High volume)**
**Current State**: No CO-RE, partial L7
**Ring Buffer Size**: HIGH (512KB)

#### Tasks:
- [ ] Full CO-RE conversion for socket operations
- [ ] Complete L7 protocol parsing (HTTP/gRPC)
- [ ] Add connection tracking with limits (10K max)
- [ ] Implement TCP retransmit detection
- [ ] Add packet drop monitoring
- [ ] Rate limit to 2000 events/sec
- [ ] Add per-protocol sampling
- [ ] SSL/TLS visibility via uprobe

#### Specific Changes:
```c
// Track connections with CO-RE
struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
u16 dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
u32 saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
```

---

### 4. Container Runtime Observer
**Priority: P1 (Container boundaries critical)**
**Current State**: Headers included, minimal CO-RE
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] Full CO-RE for cgroup operations
- [ ] Add container ID extraction via CO-RE
- [ ] Implement OOM kill prediction
- [ ] Add fork bomb detection
- [ ] Rate limit fork/exec to 100/sec
- [ ] Add memory pressure correlation
- [ ] Test with container stress scenarios
- [ ] Add pod-to-container mapping

#### Specific Changes:
```c
// Extract container ID with CO-RE
u64 cgroup_id = BPF_CORE_READ(task, cgroups, subsys[0], cgroup, kn, id);

// Get container boundaries
struct css_set *cgroups = BPF_CORE_READ(task, cgroups);
```

---

### 5. Process Signals Observer
**Priority: P1 (Security critical)**
**Current State**: No CO-RE
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] CO-RE conversion for signal handling
- [ ] Add signal storm detection
- [ ] Implement kill chain tracking
- [ ] Add process tree correlation
- [ ] Rate limit to 200 events/sec
- [ ] Add SIGKILL/SIGTERM analysis
- [ ] Test with signal floods
- [ ] Add container signal isolation

#### Specific Changes:
```c
// Track signals with CO-RE
struct kernel_siginfo *info = (struct kernel_siginfo *)PT_REGS_PARM2(ctx);
int sig = BPF_CORE_READ(info, si_signo);
pid_t sender = BPF_CORE_READ(info, si_pid);
```

---

### 6. Systemd Observer
**Priority: P2 (Already has some CO-RE)**
**Current State**: Best CO-RE usage
**Ring Buffer Size**: LOW (128KB)

#### Tasks:
- [ ] Complete CO-RE migration
- [ ] Add service dependency tracking
- [ ] Implement restart loop detection
- [ ] Add journal correlation
- [ ] Rate limit to 100 events/sec
- [ ] Add systemd timer monitoring
- [ ] Test with service failures
- [ ] Add cgroup v2 support

#### Specific Changes:
```c
// Already using BPF_CORE_READ correctly
// Just need to complete migration and add safety
```

---

### 7. Storage I/O Observer
**Priority: P2 (Performance impact)**
**Current State**: Multiple versions, no CO-RE
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] Consolidate 3 versions into one
- [ ] Full CO-RE for bio operations
- [ ] Add I/O pattern detection
- [ ] Implement latency histograms
- [ ] Rate limit to 500 events/sec
- [ ] Add device mapper support
- [ ] Test with I/O stress
- [ ] Add filesystem-specific tracking

#### Specific Changes:
```c
// Track I/O with CO-RE
struct bio *bio = (struct bio *)PT_REGS_PARM1(ctx);
sector_t sector = BPF_CORE_READ(bio, bi_iter.bi_sector);
u32 size = BPF_CORE_READ(bio, bi_iter.bi_size);
```

---

### 8. Kernel Observer
**Priority: P1 (Security)**
**Current State**: No CO-RE, limited scope
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] Expand beyond ConfigMap/Secret
- [ ] Full CO-RE for syscall tracking
- [ ] Add audit trail capability
- [ ] Implement syscall filtering
- [ ] Rate limit to 1000 events/sec
- [ ] Add security policy engine
- [ ] Test with syscall floods
- [ ] Add eBPF LSM hooks

#### Specific Changes:
```c
// Track syscalls with CO-RE
int syscall_nr = BPF_CORE_READ(task, thread.trap_nr);
unsigned long args[6];
BPF_CORE_READ_INTO(&args, task, thread.regs, di); // arg1
```

---

### 9. Health Observer
**Priority: P3 (Currently stub)**
**Current State**: Stub implementation
**Ring Buffer Size**: LOW (128KB)

#### Tasks:
- [ ] Complete implementation from scratch
- [ ] Design health metrics collection
- [ ] Implement with CO-RE from start
- [ ] Add system resource monitoring
- [ ] Rate limit to 50 events/sec
- [ ] Add anomaly detection
- [ ] Test health scenarios
- [ ] Add predictive alerts

---

### 10. Scheduler Observer
**Priority: P3 (Incomplete)**
**Current State**: BPF source exists, no loader
**Ring Buffer Size**: MEDIUM (256KB)

#### Tasks:
- [ ] Complete eBPF loader implementation
- [ ] Full CO-RE for scheduler tracking
- [ ] Add CFS monitoring
- [ ] Implement latency tracking
- [ ] Rate limit to 200 events/sec
- [ ] Add CPU contention detection
- [ ] Test with CPU stress
- [ ] Add priority inversion detection

---

## Global Migration Steps

### Phase 1: Foundation (Week 1)
1. Generate vmlinux.h from BTF for target kernels
2. Create common CO-RE header library
3. Implement standard safety wrappers
4. Set up multi-kernel test environment

### Phase 2: Critical Observers (Week 2-3)
1. Migrate DNS Observer (P0)
2. Migrate Memory Observer (P0)
3. Migrate Network Observer (P0)
4. Test on kernels 5.4, 5.10, 5.15, 6.0

### Phase 3: Security Observers (Week 4)
1. Migrate Container Runtime (P1)
2. Migrate Process Signals (P1)
3. Migrate Kernel Observer (P1)
4. Security validation and stress testing

### Phase 4: Remaining Observers (Week 5)
1. Complete Systemd Observer (P2)
2. Migrate Storage I/O (P2)
3. Implement Health Observer (P3)
4. Complete Scheduler Observer (P3)

### Phase 5: Validation (Week 6)
1. Full integration testing
2. Performance benchmarking
3. Memory leak detection
4. Production readiness audit

## Success Criteria

Each observer is complete when:
- [ ] Uses `-target bpf` for CO-RE
- [ ] All kernel access via BPF_CORE_READ
- [ ] Rate limiting implemented
- [ ] Safe probe reads everywhere
- [ ] Ring buffer overflow handling
- [ ] Tests pass on 4+ kernel versions
- [ ] No memory leaks
- [ ] No verifier errors
- [ ] Performance within 5% of non-CO-RE

## Testing Matrix

| Observer | Kernel 5.4 | Kernel 5.10 | Kernel 5.15 | Kernel 6.0 |
|----------|------------|-------------|-------------|------------|
| DNS      | [ ]        | [ ]         | [ ]         | [ ]        |
| Memory   | [ ]        | [ ]         | [ ]         | [ ]        |
| Network  | [ ]        | [ ]         | [ ]         | [ ]        |
| Container| [ ]        | [ ]         | [ ]         | [ ]        |
| Signals  | [ ]        | [ ]         | [ ]         | [ ]        |
| Systemd  | [ ]        | [ ]         | [ ]         | [ ]        |
| Storage  | [ ]        | [ ]         | [ ]         | [ ]        |
| Kernel   | [ ]        | [ ]         | [ ]         | [ ]        |
| Health   | [ ]        | [ ]         | [ ]         | [ ]        |
| Scheduler| [ ]        | [ ]         | [ ]         | [ ]        |

## NO COMPROMISE
Following CLAUDE.md standards:
- NO STUBS - Complete implementations only
- NO TODOs - Finish or don't start
- Test coverage >= 80%
- Complete CO-RE or don't deploy
- All safety checks or reject PR

**WE FIX ALL OBSERVERS TO CO-RE STANDARD OR WE DON'T SHIP.**