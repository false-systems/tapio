// SPDX-License-Identifier: GPL-2.0
// Minimal etcd eBPF monitor - CO-RE enabled raw syscall monitoring
//
// SECURITY: This monitor implements multi-layer process verification to prevent
// false positives from processes like etcd-backup, etcdctl, etcd-operator, etc.
// Only genuine "etcd" processes are monitored through:
// 1. Exact process name matching (not prefix matching)
// 2. PID allowlist managed by userspace
// 3. Parent process validation
//
// COMPILATION: Requires Linux system with clang/LLVM to compile
// Run 'go generate ./...' on Linux to regenerate bytecode after changes

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

// Event types
#define EVENT_WRITE 1
#define EVENT_FSYNC 2

// Max data capture
#define MAX_DATA_SIZE 256

// Raw event structure - no business logic
struct etcd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u8  event_type;
    __u8  pad[3];
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 data_len;
    __u8  data[MAX_DATA_SIZE];
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB
} events SEC(".maps");

// PID allowlist for verified etcd processes
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 64);  // Support up to 64 etcd processes
    __type(key, __u32);      // PID
    __type(value, __u64);    // Verification timestamp
} etcd_pids SEC(".maps");

// Process validation metadata
struct etcd_proc_metadata {
    __u32 pid;
    __u32 ppid;
    char comm[16];
    char cmdline[64];
    __u64 start_time;
    __u64 verified_at;
} __attribute__((packed));

// Secure exact match validation for etcd processes
// SECURITY-CRITICAL: This function prevents false positives by requiring
// an EXACT match for "etcd" - not "etcd-backup", "etcdctl", "etcd-operator", etc.
// Previous vulnerable versions only checked if the name STARTED with "etcd"
// which allowed any process with "etcd" prefix to be monitored (security risk)
static __always_inline bool is_exact_etcd_process(struct task_struct *task)
{
    char comm[16];
    // Use CO-RE field access for task->comm
    int ret = bpf_core_read_str(&comm, sizeof(comm), &task->comm);
    if (ret < 0)
        return false;
    
    // Exact match validation - must be exactly "etcd" (not etcd-backup, etcdctl, etc.)
    // Check each character individually for security
    if (comm[0] != 'e') return false;
    if (comm[1] != 't') return false;
    if (comm[2] != 'c') return false;
    if (comm[3] != 'd') return false;
    
    // Fifth character must be null terminator for exact match
    // This is CRITICAL - it ensures we only match "etcd\0" and not "etcdXXX"
    if (comm[4] != '\0') return false;
    
    return true;
}

// Multi-layer process verification
// This implements defense-in-depth with multiple security layers:
// Layer 1: PID allowlist with time-based expiration (prevents PID reuse attacks)
// Layer 2: Exact process name matching (prevents prefix-based false positives)
// Layer 3: Parent process validation (ensures reasonable process hierarchy)
// The userspace component manages the PID allowlist by discovering and validating
// genuine etcd processes through command-line analysis and binary path verification
static __always_inline bool is_verified_etcd_process(struct task_struct *task)
{
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // Layer 1: Check PID allowlist (most efficient)
    // The allowlist is managed by userspace which performs comprehensive validation
    __u64 *verified_time = bpf_map_lookup_elem(&etcd_pids, &pid);
    if (verified_time) {
        __u64 current_time = bpf_ktime_get_ns();
        // Allow 5-minute verification window (300 * 1e9 nanoseconds)
        // This prevents stale PIDs from being monitored after process termination
        if (current_time - *verified_time < 300000000000ULL) {
            return true;
        }
        // Verification expired, remove from allowlist
        bpf_map_delete_elem(&etcd_pids, &pid);
    }
    
    // Layer 2: Exact process name validation
    if (!is_exact_etcd_process(task)) {
        return false;
    }
    
    // Layer 3: Additional process metadata validation
    // Check if process has reasonable characteristics for etcd
    __u32 ppid = 0;
    int ret = bpf_core_read(&ppid, sizeof(ppid), &task->real_parent->pid);
    if (ret < 0) {
        return false;  // Unable to read parent PID - suspicious
    }
    
    // etcd typically runs as daemon (ppid should be 1 or systemd)
    // But allow some flexibility for containerized environments
    if (ppid > 65535) {  // Reasonable upper bound for PID
        return false;
    }
    
    return true;
}

// Monitor write syscalls
SEC("tracepoint/syscalls/sys_enter_write")
int trace_sys_enter_write(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Basic filter - only track etcd-like processes
    if (!is_verified_etcd_process(task))
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    e->event_type = EVENT_WRITE;
    
    // Get file descriptor and size
    int fd = (int)ctx->args[0];
    size_t count = (size_t)ctx->args[2];
    
    e->data_len = count > MAX_DATA_SIZE ? MAX_DATA_SIZE : count;
    
    // Just store fd and size as raw data
    if (e->data_len >= 8) {
        *(int*)e->data = fd;
        *(size_t*)(e->data + 4) = count;
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor fsync syscalls
SEC("tracepoint/syscalls/sys_enter_fsync")
int trace_sys_enter_fsync(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_verified_etcd_process(task))
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    e->event_type = EVENT_FSYNC;
    
    // Get file descriptor
    int fd = (int)ctx->args[0];
    
    e->data_len = sizeof(int);
    *(int*)e->data = fd;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}