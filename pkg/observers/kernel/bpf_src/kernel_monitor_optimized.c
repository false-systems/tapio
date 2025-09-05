// SPDX-License-Identifier: GPL-2.0
// Optimized kernel monitoring eBPF program with performance improvements
// Features: Per-CPU buffers, sampling, IPv6 support, better verifier compliance

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Sampling rate for memory events (1 in N events)
#define MEMORY_SAMPLE_RATE 100

// Event types
#define EVENT_TYPE_MEMORY_ALLOC 1
#define EVENT_TYPE_MEMORY_FREE  2
#define EVENT_TYPE_PROCESS_EXEC 3
#define EVENT_TYPE_NETWORK_CONN 5
#define EVENT_TYPE_NETWORK_CLOSE 7
#define EVENT_TYPE_FILE_OPEN 8
#define EVENT_TYPE_OOM_KILL 9
#define EVENT_TYPE_IO_URING_OP 10

// Network structures with IPv6 support
struct network_info_v2 {
    union {
        __u32 ipv4_saddr;
        __u32 ipv6_saddr[4];
    };
    union {
        __u32 ipv4_daddr;
        __u32 ipv6_daddr[4];
    };
    __u16 sport;
    __u16 dport;
    __u8 protocol;
    __u8 ip_version; // 4 or 6
    __u8 state;
    __u8 direction;
} __attribute__((packed));

// Optimized event structure with better alignment
struct kernel_event_v2 {
    __u64 timestamp;
    __u64 cgroup_id;
    __u64 size;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u32 flags;
    char comm[16];
    char pod_uid[36];
    union {
        struct network_info_v2 net_info;
        __u8 data[64];
    };
} __attribute__((packed));

// Ring buffer with larger size for production
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB - production optimized
} events SEC(".maps");

// LRU hash for container PIDs - automatic eviction
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 20000);
    __type(key, __u32);
    __type(value, __u8);
} container_pids SEC(".maps");

// Per-CPU array for sampling counters
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 16); // One per event type
    __type(key, __u32);
    __type(value, __u64);
} event_counters SEC(".maps");

// Per-CPU scratch space to avoid stack overflow
struct scratch_space {
    struct kernel_event_v2 event;
    char buffer[256];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct scratch_space);
} scratch_map SEC(".maps");

// Optimized helper to check if we should sample this event
static __always_inline bool should_sample(__u32 event_type)
{
    __u32 key = event_type;
    __u64 *counter = bpf_map_lookup_elem(&event_counters, &key);
    if (!counter) {
        return true; // Sample if counter doesn't exist
    }
    
    __u64 count = __sync_fetch_and_add(counter, 1);
    
    // Different sampling rates for different event types
    switch (event_type) {
        case EVENT_TYPE_MEMORY_ALLOC:
        case EVENT_TYPE_MEMORY_FREE:
            return (count % MEMORY_SAMPLE_RATE) == 0;
        case EVENT_TYPE_FILE_OPEN:
            return (count % 10) == 0; // Sample 1 in 10 file opens
        default:
            return true; // Don't sample critical events
    }
}

// Optimized cgroup ID extraction with caching
static __always_inline __u64 get_cgroup_id_optimized(struct task_struct *task)
{
    if (!task)
        return 0;
    
    // Use BPF helper if available (kernel 4.18+)
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    if (cgroup_id != 0)
        return cgroup_id;
    
    // Fallback to manual extraction
    if (!bpf_core_field_exists(task->cgroups))
        return 0;
    
    struct css_set *css_set_ptr;
    if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) != 0 || !css_set_ptr)
        return 0;
    
    // Try unified hierarchy first (cgroup v2)
    struct cgroup_subsys_state *css;
    if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[0]) != 0 || !css)
        return 0;
    
    struct cgroup *cgroup_ptr;
    if (BPF_CORE_READ_INTO(&cgroup_ptr, css, cgroup) != 0 || !cgroup_ptr)
        return 0;
    
    // Get kernfs inode number
    if (bpf_core_field_exists(cgroup_ptr->kn)) {
        struct kernfs_node *kn;
        if (BPF_CORE_READ_INTO(&kn, cgroup_ptr, kn) == 0 && kn) {
            __u64 ino;
            if (BPF_CORE_READ_INTO(&ino, kn, ino) == 0)
                return ino;
        }
    }
    
    return 0;
}

// Helper to check container process with LRU optimization
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != NULL && *flag == 1;
}

// Optimized memory allocation tracing with sampling
SEC("tracepoint/kmem/kmalloc")
int trace_kmalloc_optimized(struct trace_event_raw_kmalloc *ctx)
{
    __u32 event_type = EVENT_TYPE_MEMORY_ALLOC;
    
    // Apply sampling early to reduce overhead
    if (!should_sample(event_type))
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    // Use per-CPU scratch space
    __u32 zero = 0;
    struct scratch_space *scratch = bpf_map_lookup_elem(&scratch_map, &zero);
    if (!scratch)
        return 0;
    
    struct kernel_event_v2 *event = &scratch->event;
    __builtin_memset(event, 0, sizeof(*event));
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = event_type;
    
    // Read size from tracepoint context
    if (bpf_core_field_exists(ctx->bytes_alloc))
        event->size = BPF_CORE_READ(ctx, bytes_alloc);
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->cgroup_id = get_cgroup_id_optimized(task);
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Reserve and submit to ring buffer
    struct kernel_event_v2 *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memcpy(e, event, sizeof(*e));
    bpf_ringbuf_submit(e, 0);
    
    return 0;
}

// Optimized TCP connection tracking with IPv6 support
SEC("kprobe/tcp_connect")
int trace_tcp_connect_v2(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct sock *sk = read_sock_from_kprobe(ctx);
    if (!sk)
        return 0;
    
    // Use per-CPU scratch space
    __u32 zero = 0;
    struct scratch_space *scratch = bpf_map_lookup_elem(&scratch_map, &zero);
    if (!scratch)
        return 0;
    
    struct kernel_event_v2 *event = &scratch->event;
    __builtin_memset(event, 0, sizeof(*event));
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CONN;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->cgroup_id = get_cgroup_id_optimized(task);
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Read socket info with IPv4/IPv6 support
    __u16 family;
    BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family);
    
    if (family == AF_INET) {
        event->net_info.ip_version = 4;
        BPF_CORE_READ_INTO(&event->net_info.ipv4_saddr, sk, __sk_common.skc_rcv_saddr);
        BPF_CORE_READ_INTO(&event->net_info.ipv4_daddr, sk, __sk_common.skc_daddr);
    } else if (family == AF_INET6) {
        event->net_info.ip_version = 6;
        BPF_CORE_READ_INTO(&event->net_info.ipv6_saddr, sk, 
                          __sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32);
        BPF_CORE_READ_INTO(&event->net_info.ipv6_daddr, sk,
                          __sk_common.skc_v6_daddr.in6_u.u6_addr32);
    } else {
        return 0; // Unsupported family
    }
    
    BPF_CORE_READ_INTO(&event->net_info.sport, sk, __sk_common.skc_num);
    BPF_CORE_READ_INTO(&event->net_info.dport, sk, __sk_common.skc_dport);
    event->net_info.dport = __builtin_bswap16(event->net_info.dport);
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 0; // Outgoing
    
    struct kernel_event_v2 *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memcpy(e, event, sizeof(*e));
    bpf_ringbuf_submit(e, 0);
    
    return 0;
}

// New: OOM Kill tracking
SEC("tracepoint/oom/mark_victim")
int trace_oom_kill(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct kernel_event_v2 *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_OOM_KILL;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->cgroup_id = get_cgroup_id_optimized(task);
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Mark this as critical event
    event->flags = 0x1; // CRITICAL flag
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// New: io_uring operation tracking
SEC("kprobe/io_submit_sqes")
int trace_io_uring_submit(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    // Sample io_uring operations
    if (!should_sample(EVENT_TYPE_IO_URING_OP))
        return 0;
    
    struct kernel_event_v2 *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_IO_URING_OP;
    
    // Get number of SQEs being submitted
    event->size = BPF_KPROBE_READ_ARG(unsigned, ctx, 1);
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->cgroup_id = get_cgroup_id_optimized(task);
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";