// SPDX-License-Identifier: GPL-2.0
// Minimal eBPF program for kernel monitoring - focused on containers

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event types
#define EVENT_TYPE_MEMORY_ALLOC 1
#define EVENT_TYPE_MEMORY_FREE  2
#define EVENT_TYPE_PROCESS_EXEC 3
#define EVENT_TYPE_POD_SYSCALL  4
#define EVENT_TYPE_NETWORK_CONN 5

// Kernel event structure (must match Go struct)
struct kernel_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u64 size;
    char comm[16];
    __u64 cgroup_id;  // Add cgroup ID for pod correlation
    char pod_uid[36]; // Add pod UID
    __u8 data[64];
} __attribute__((packed));

// Pod information structure
struct pod_info {
    char pod_uid[36];
    char namespace[64];
    char pod_name[128];
    __u64 created_at;
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4 * 1024 * 1024); // 4MB buffer
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u8);  // Flag
} container_pids SEC(".maps");

// Map cgroup ID to pod information
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);          // cgroup ID
    __type(value, struct pod_info); // pod info
} pod_info_map SEC(".maps");

// Helper to check if process is in a container
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != NULL;
}

// Helper to extract cgroup ID from task struct
static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    // This is a simplified version - in practice, we'd traverse the cgroup hierarchy
    // For now, use a hash of the PID as a pseudo-cgroup ID
    __u32 pid = BPF_CORE_READ(task, pid);
    return (__u64)pid; // Simplified - would need proper cgroup traversal
}

// Helper to get pod information for a cgroup ID
static __always_inline struct pod_info *get_pod_info(__u64 cgroup_id)
{
    return bpf_map_lookup_elem(&pod_info_map, &cgroup_id);
}

// Memory allocation tracing - using generic tracepoint
SEC("tracepoint/kmem/kmalloc")
int trace_malloc(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_ALLOC;
    event->size = 0; // Can't easily get size from generic tracepoint
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod information
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod) {
        __builtin_memcpy(event->pod_uid, pod->pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Memory free tracing
SEC("tracepoint/kmem/kfree")
int trace_free(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_FREE;
    event->size = 0;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process execution tracing
SEC("tracepoint/sched/sched_process_exec")  
int trace_exec(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_EXEC;
    event->size = 0;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Network connection tracing - track connect syscalls for pod-to-pod communication
SEC("tracepoint/syscalls/sys_enter_connect")
int trace_connect(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CONN;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod information
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod) {
        __builtin_memcpy(event->pod_uid, pod->pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    // Store socket info in data field (simplified)
    int sockfd = (int)ctx->args[0];
    __builtin_memcpy(event->data, &sockfd, sizeof(sockfd));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";