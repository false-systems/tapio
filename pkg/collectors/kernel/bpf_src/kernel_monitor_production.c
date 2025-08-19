// SPDX-License-Identifier: GPL-2.0
// Production-optimized eBPF program for kernel monitoring
// Focused on minimal overhead and maximum efficiency

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Reduce event types to only essential ones
#define EVENT_TYPE_PROCESS_EXEC 1
#define EVENT_TYPE_NETWORK_CONN 2
#define EVENT_TYPE_FILE_OPEN    3

// Optimized kernel event structure (64 bytes total - cache line aligned)
struct optimized_kernel_event {
    __u64 timestamp;      // 8 bytes
    __u64 cgroup_id;      // 8 bytes - most important for correlation
    __u32 pid;            // 4 bytes
    __u32 event_type;     // 4 bytes
    char comm[16];        // 16 bytes - process name
    union {
        struct {
            __u32 src_ip;
            __u32 dst_ip;
            __u16 src_port;
            __u16 dst_port;
        } net;               // 12 bytes for network events
        struct {
            char path[24];   // 24 bytes for file paths (truncated)
        } file;
        __u8 raw_data[24];   // 24 bytes for other data
    };
    __u32 flags;            // 4 bytes for additional flags
} __attribute__((packed));

// Single ring buffer - smaller but more efficient
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB - reduced from 512KB
} events SEC(".maps");

// Simplified container tracking - just PID whitelist
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192); // Reduced from 10240
    __type(key, __u32);        // PID
    __type(value, __u64);      // Cgroup ID (cached for performance)
} container_cgroups SEC(".maps");

// Performance counters for monitoring
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 8);
    __type(key, __u32);
    __type(value, __u64);
} perf_counters SEC(".maps");

#define COUNTER_EVENTS_TOTAL     0
#define COUNTER_EVENTS_DROPPED   1
#define COUNTER_CGROUP_LOOKUPS   2
#define COUNTER_CGROUP_HITS      3
#define COUNTER_NET_EVENTS       4
#define COUNTER_FILE_EVENTS      5
#define COUNTER_PROC_EVENTS      6

// Optimized cgroup ID extraction - single method, cached
static __always_inline __u64 get_cgroup_id_fast(struct task_struct *task, __u32 pid)
{
    if (!task)
        return 0;

    // First check cache
    __u64 *cached_cgroup = bpf_map_lookup_elem(&container_cgroups, &pid);
    if (cached_cgroup) {
        // Update counter
        __u32 key = COUNTER_CGROUP_HITS;
        __u64 *counter = bpf_map_lookup_elem(&perf_counters, &key);
        if (counter) {
            __sync_fetch_and_add(counter, 1);
        }
        return *cached_cgroup;
    }

    // Update lookup counter
    __u32 key = COUNTER_CGROUP_LOOKUPS;
    __u64 *counter = bpf_map_lookup_elem(&perf_counters, &key);
    if (counter) {
        __sync_fetch_and_add(counter, 1);
    }

    // Only use kernfs inode method - most reliable and fast
    if (!bpf_core_field_exists(task->cgroups))
        return 0;

    struct css_set *css_set_ptr;
    if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) != 0 || !css_set_ptr)
        return 0;

    struct cgroup_subsys_state *css;
    if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[0]) != 0 || !css)
        return 0;

    struct cgroup *cgroup_ptr;
    if (BPF_CORE_READ_INTO(&cgroup_ptr, css, cgroup) != 0 || !cgroup_ptr)
        return 0;

    // Only use kernfs inode - drop fallbacks for performance
    __u64 cgroup_id = 0;
    if (bpf_core_field_exists(cgroup_ptr->kn)) {
        struct kernfs_node *kn;
        if (BPF_CORE_READ_INTO(&kn, cgroup_ptr, kn) == 0 && kn) {
            __u64 ino;
            if (BPF_CORE_READ_INTO(&ino, kn, ino) == 0 && ino != 0) {
                cgroup_id = ino;
            }
        }
    }

    // Cache the result for future lookups
    if (cgroup_id != 0) {
        bpf_map_update_elem(&container_cgroups, &pid, &cgroup_id, BPF_ANY);
    }

    return cgroup_id;
}

// Efficient container process check
static __always_inline bool is_container_process_fast(__u32 pid)
{
    return bpf_map_lookup_elem(&container_cgroups, &pid) != NULL;
}

// Increment performance counter
static __always_inline void inc_counter(__u32 counter_id)
{
    __u64 *counter = bpf_map_lookup_elem(&perf_counters, &counter_id);
    if (counter) {
        __sync_fetch_and_add(counter, 1);
    }
}

// Common event initialization
static __always_inline struct optimized_kernel_event *
init_event(__u32 event_type, __u32 pid, __u64 cgroup_id)
{
    struct optimized_kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        inc_counter(COUNTER_EVENTS_DROPPED);
        return NULL;
    }

    event->timestamp = bpf_ktime_get_ns();
    event->cgroup_id = cgroup_id;
    event->pid = pid;
    event->event_type = event_type;
    event->flags = 0;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));

    inc_counter(COUNTER_EVENTS_TOTAL);
    return event;
}

// Process execution tracing - minimal overhead
SEC("tracepoint/sched/sched_process_exec")
int trace_exec_optimized(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;

    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id_fast(task, pid);
    
    // Only process container processes
    if (cgroup_id == 0)
        return 0;

    struct optimized_kernel_event *event = init_event(EVENT_TYPE_PROCESS_EXEC, pid, cgroup_id);
    if (!event)
        return 0;

    // For exec events, no additional data needed
    __builtin_memset(&event->raw_data, 0, sizeof(event->raw_data));
    
    inc_counter(COUNTER_PROC_EVENTS);
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Network connection tracing - optimized for TCP only
SEC("kprobe/tcp_v4_connect")
int trace_tcp_connect_optimized(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;

    // Fast container check first
    if (!is_container_process_fast(pid))
        return 0;

    // Get cgroup ID from cache
    __u64 *cached_cgroup = bpf_map_lookup_elem(&container_cgroups, &pid);
    if (!cached_cgroup)
        return 0;

    struct sock *sk = read_sock_from_kprobe(ctx);
    if (!sk)
        return 0;

    struct optimized_kernel_event *event = init_event(EVENT_TYPE_NETWORK_CONN, pid, *cached_cgroup);
    if (!event)
        return 0;

    // Read socket info efficiently
    __u16 family = 0;
    BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family);
    
    if (family == 2) { // AF_INET
        BPF_CORE_READ_INTO(&event->net.src_port, sk, __sk_common.skc_num);
        BPF_CORE_READ_INTO(&event->net.dst_port, sk, __sk_common.skc_dport);
        BPF_CORE_READ_INTO(&event->net.src_ip, sk, __sk_common.skc_rcv_saddr);
        BPF_CORE_READ_INTO(&event->net.dst_ip, sk, __sk_common.skc_daddr);
        
        // Convert port from network byte order
        event->net.dst_port = bpf_ntohs(event->net.dst_port);
        event->flags = 4; // IPv4 flag
    }

    inc_counter(COUNTER_NET_EVENTS);
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// File open tracing - only for interesting paths
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat_optimized(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;

    // Fast container check first  
    if (!is_container_process_fast(pid))
        return 0;

    // Get cgroup ID from cache
    __u64 *cached_cgroup = bpf_map_lookup_elem(&container_cgroups, &pid);
    if (!cached_cgroup)
        return 0;

    // Get filename from syscall args
    const char __user *filename = (const char __user *)ctx->args[1];
    if (!filename)
        return 0;

    struct optimized_kernel_event *event = init_event(EVENT_TYPE_FILE_OPEN, pid, *cached_cgroup);
    if (!event)
        return 0;

    // Read filename with bounds checking
    long ret = bpf_probe_read_user_str(event->file.path, sizeof(event->file.path), filename);
    if (ret <= 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }

    // Simple filter - only track /etc, /proc, /sys paths for K8s correlation
    char first_char = event->file.path[0];
    if (first_char != '/') {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }

    // Check for interesting prefixes
    bool interesting = false;
    if (event->file.path[1] == 'e' && event->file.path[2] == 't' && event->file.path[3] == 'c') // /etc
        interesting = true;
    else if (event->file.path[1] == 'p' && event->file.path[2] == 'r' && event->file.path[3] == 'o') // /proc
        interesting = true;
    else if (event->file.path[1] == 's' && event->file.path[2] == 'y' && event->file.path[3] == 's') // /sys
        interesting = true;
    else if (event->file.path[1] == 'v' && event->file.path[2] == 'a' && event->file.path[3] == 'r') // /var
        interesting = true;

    if (!interesting) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }

    inc_counter(COUNTER_FILE_EVENTS);
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";