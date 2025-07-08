//go:build ignore

#include "headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "common.h"

char LICENSE[] SEC("license") = "GPL";

// BPF maps for storing data
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);               // PID
    __type(value, __u64);             // Total allocated memory
} process_memory SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);               // PID
    __type(value, __u64);             // Last allocation timestamp
} last_alloc_time SEC(".maps");

// Helper to check if process is in container
static __always_inline int is_in_container(struct task_struct *task) {
    struct pid_namespace *pidns;
    pidns = BPF_CORE_READ(task, nsproxy, pid_ns_for_children);
    
    // If PID namespace is not the init namespace, it's in a container
    return pidns != NULL && BPF_CORE_READ(pidns, level) > 0;
}

// Helper to get container PID (PID as seen inside container)
static __always_inline __u32 get_container_pid(struct task_struct *task) {
    struct pid *pid_struct;
    pid_struct = BPF_CORE_READ(task, thread_pid);
    return BPF_CORE_READ(pid_struct, numbers[1].nr); // Container namespace PID
}

// Track memory allocations
SEC("tp/kmem/mm_page_alloc")
int track_memory_alloc(struct trace_event_raw_mm_page_alloc *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u64 size = (1ULL << ctx->order) * 4096; // Convert pages to bytes
    __u64 timestamp = bpf_ktime_get_ns();
    
    // Skip kernel threads
    if (pid == 0 || pid == 1) {
        return 0;
    }
    
    // Update total memory for this process
    __u64 *current_memory = bpf_map_lookup_elem(&process_memory, &pid);
    __u64 new_total = size;
    if (current_memory) {
        new_total = *current_memory + size;
    }
    bpf_map_update_elem(&process_memory, &pid, &new_total, BPF_ANY);
    
    // Update last allocation timestamp
    bpf_map_update_elem(&last_alloc_time, &pid, &timestamp, BPF_ANY);
    
    // Send event to userspace (only for significant allocations)
    if (size > 4096) { // Only track allocations > 4KB
        struct memory_event *event;
        event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
        if (!event) {
            return 0;
        }
        
        event->timestamp = timestamp;
        event->pid = pid;
        event->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
        event->size = size;
        event->total_memory = new_total;
        event->event_type = EVENT_MEMORY_ALLOC;
        event->in_container = is_in_container(task);
        event->container_pid = event->in_container ? get_container_pid(task) : 0;
        
        bpf_get_current_comm(&event->comm, sizeof(event->comm));
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

// Track memory frees
SEC("tp/kmem/mm_page_free")
int track_memory_free(struct trace_event_raw_mm_page_free *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u64 size = (1ULL << ctx->order) * 4096;
    __u64 timestamp = bpf_ktime_get_ns();
    
    if (pid == 0 || pid == 1) {
        return 0;
    }
    
    // Update total memory for this process
    __u64 *current_memory = bpf_map_lookup_elem(&process_memory, &pid);
    if (current_memory && *current_memory >= size) {
        __u64 new_total = *current_memory - size;
        bpf_map_update_elem(&process_memory, &pid, &new_total, BPF_ANY);
        
        // Send free event for significant frees
        if (size > 4096) {
            struct memory_event *event;
            event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
            if (!event) {
                return 0;
            }
            
            event->timestamp = timestamp;
            event->pid = pid;
            event->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
            event->size = size;
            event->total_memory = new_total;
            event->event_type = EVENT_MEMORY_FREE;
            
            struct task_struct *task = (struct task_struct *)bpf_get_current_task();
            event->in_container = is_in_container(task);
            event->container_pid = event->in_container ? get_container_pid(task) : 0;
            
            bpf_get_current_comm(&event->comm, sizeof(event->comm));
            bpf_ringbuf_submit(event, 0);
        }
    }
    
    return 0;
}

// Track OOM kills
SEC("tp/oom/oom_score_adj_update")
int track_oom_kill(void *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u64 timestamp = bpf_ktime_get_ns();
    
    struct memory_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = timestamp;
    event->pid = pid;
    event->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event->size = 0;
    event->total_memory = 0;
    event->event_type = EVENT_OOM_KILL;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->in_container = is_in_container(task);
    event->container_pid = event->in_container ? get_container_pid(task) : 0;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

// Track process exits
SEC("tp/sched/sched_process_exit")
int track_process_exit(struct trace_event_raw_sched_process_template *ctx) {
    __u32 pid = ctx->pid;
    __u64 timestamp = bpf_ktime_get_ns();
    
    // Clean up memory tracking for this PID
    bpf_map_delete_elem(&process_memory, &pid);
    bpf_map_delete_elem(&last_alloc_time, &pid);
    
    // Send exit event
    struct memory_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = timestamp;
    event->pid = pid;
    event->tid = 0;
    event->size = 0;
    event->total_memory = 0;
    event->event_type = EVENT_PROCESS_EXIT;
    event->in_container = 0;
    event->container_pid = 0;
    
    __builtin_memcpy(event->comm, ctx->comm, TASK_COMM_LEN);
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}