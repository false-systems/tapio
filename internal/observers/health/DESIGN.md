# Health Observer eBPF Design Session

## Problem Statement
We need to detect system health issues in real-time by monitoring failed syscalls at the kernel level. Currently the health observer is FAKE - it just generates mock events. We need REAL eBPF implementation that will impress an eBPF expert mom in Petach Tikva.

## What's the Simplest Solution?
Hook into syscall exit points, check return codes for errors (negative values), track patterns of failures, and report health issues. Focus on the most critical syscalls that indicate system problems.

## Component Breakdown

### 1. eBPF Programs (Kernel Space)
```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│ Syscall Exit│ ───> │Check Return │ ───> │Track Error  │
│   Tracepoint│      │   Code < 0  │      │  Patterns   │
└─────────────┘      └─────────────┘      └─────────────┘
       ↓                                          ↓
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│Categorize   │ ───> │Rate Limit   │ ───> │Send Event   │
│Error Type   │      │   Events    │      │to Userspace │
└─────────────┘      └─────────────┘      └─────────────┘
```

### 2. Syscalls to Monitor

Priority syscalls that indicate health issues:

| Syscall | Error Codes | Health Category | Why Monitor |
|---------|------------|-----------------|-------------|
| open/openat | ENOSPC, EMFILE, EACCES | File | Disk space, FD exhaustion |
| write | ENOSPC, EIO | File | Disk full, I/O errors |
| mmap | ENOMEM | Memory | Memory exhaustion |
| connect | ECONNREFUSED, ETIMEDOUT | Network | Service availability |
| bind | EADDRINUSE, EACCES | Network | Port conflicts |
| fork/clone | EAGAIN, ENOMEM | Process | Resource limits |

### 3. Data Structures

#### Health Event (Kernel)
```c
struct health_event {
    u64 timestamp_ns;
    u32 pid;
    u32 ppid;
    u32 uid;
    u32 gid;
    u64 cgroup_id;

    // Syscall info
    u32 syscall_nr;
    s32 error_code;    // Negative errno

    // Context
    u8 comm[16];       // Process name
    u8 path[256];      // File path (if applicable)

    // Network context (if applicable)
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;

    // Tracking
    u32 error_count;   // How many times this error happened
    u8 category;       // 1=file, 2=network, 3=memory, 4=process
};
```

#### Error Tracking Map
```c
// Track error patterns per process
struct error_stats {
    u32 count;
    u64 first_seen_ns;
    u64 last_seen_ns;
};

// BPF Maps
BPF_HASH(error_tracking, struct error_key, struct error_stats, 10240);
BPF_RINGBUF_OUTPUT(health_events, 8192);
BPF_ARRAY(config, struct health_config, 1);
```

## eBPF Implementation Strategy

### 1. Tracepoint Hooks
```c
// Hook into syscall exit to catch all errors
SEC("tracepoint/syscalls/sys_exit_open")
int trace_exit_open(struct trace_event_raw_sys_exit *ctx) {
    // Check if return value is negative (error)
    if (ctx->ret >= 0) {
        return 0;  // Success, ignore
    }

    // Track this error
    track_health_event(ctx, CATEGORY_FILE);
    return 0;
}
```

### 2. Error Pattern Detection
```c
static __always_inline void track_health_event(void *ctx, u8 category) {
    struct health_event *event;

    // Rate limiting
    if (should_rate_limit()) {
        return;
    }

    // Get event from ringbuf
    event = bpf_ringbuf_reserve(&health_events, sizeof(*event), 0);
    if (!event) {
        return;
    }

    // Fill event data
    event->timestamp_ns = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->category = category;

    // Send to userspace
    bpf_ringbuf_submit(event, 0);
}
```

## Test Plan (TDD)

### Phase 1: Unit Tests (No eBPF)
```go
func TestHealthEventParsing(t *testing.T)
func TestErrorCodeMapping(t *testing.T)
func TestSyscallNameMapping(t *testing.T)
func TestErrorSeverityCalculation(t *testing.T)
func TestRateLimiting(t *testing.T)
```

### Phase 2: eBPF Load Tests
```go
func TestLoadHealthProgram(t *testing.T)
func TestAttachTracepoints(t *testing.T)
func TestRingBufferCreation(t *testing.T)
func TestConfigMapUpdate(t *testing.T)
```

### Phase 3: System Tests (Linux only)
```go
//go:build linux

func TestDetectENOSPC(t *testing.T)     // Fill disk, catch ENOSPC
func TestDetectEMFILE(t *testing.T)     // Open too many files
func TestDetectENOMEM(t *testing.T)     // Memory allocation failure
func TestDetectECONNREFUSED(t *testing.T) // Connection refused
```

### Phase 4: Performance Tests
```go
func BenchmarkEventProcessing(b *testing.B)
func TestHighErrorRate(t *testing.T)
func TestMemoryUnderLoad(t *testing.T)
```

## Failure Modes & Handling

| Failure Mode | Detection | Recovery |
|-------------|-----------|----------|
| eBPF load fails | Error on Load() | Fall back to mock mode WITH WARNING |
| Verifier rejects | VerifierError | Simplify program |
| Ring buffer full | Drop counter | Increase buffer size |
| Too many errors | Rate limit triggered | Exponential backoff |
| Permission denied | EPERM | Check CAP_BPF |

## Implementation Steps

1. **Write tests first** (TDD)
2. **Create BPF C program** with syscall hooks
3. **Generate Go bindings** using bpf2go
4. **Implement loader** with proper error handling
5. **Test on Linux** with real syscalls
6. **Performance optimize** if needed

## Success Criteria
- [ ] Detects real syscall failures
- [ ] <1% CPU overhead
- [ ] <10MB memory usage
- [ ] Handles 10K errors/sec
- [ ] NO FAKE EVENTS
- [ ] NO STUBS
- [ ] Mom approved ✓

## Code Structure
```
internal/observers/health/
├── DESIGN.md           (this file)
├── health_monitor.c    (eBPF C code)
├── health_monitor.go   (Go bindings)
├── loader.go          (eBPF loader)
├── loader_test.go
├── Makefile           (BPF compilation)
└── testdata/
    └── health_trace.json
```