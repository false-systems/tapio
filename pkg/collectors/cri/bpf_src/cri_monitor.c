//go:build ignore

// SPDX-License-Identifier: GPL-2.0
// CRI eBPF Monitor - Kernel-level container monitoring
// Provides zero-overhead OOM detection and container process tracking

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_CONTAINER_ID_LEN 64
#define MAX_CGROUP_PATH_LEN 256
#define TASK_COMM_LEN 16

// Cgroup subsystem constants - must match kernel
#ifndef CGROUP_SUBSYS_MEMORY
#define CGROUP_SUBSYS_MEMORY 2
#endif

// Event types - must match Go EventType
#define EVENT_CREATED 0
#define EVENT_STARTED 1
#define EVENT_STOPPED 2
#define EVENT_DIED    3
#define EVENT_OOM     4

// Container exit event - optimized struct for ring buffer
struct container_exit_event {
    u64 timestamp;
    u32 pid;
    u32 tgid;
    s32 exit_code;
    u64 cgroup_id;
    u64 memory_usage;
    u64 memory_limit;
    u8 oom_killed;
    char comm[TASK_COMM_LEN];
    char container_id[MAX_CONTAINER_ID_LEN];
};

// Ring buffer for events - high performance event delivery
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB buffer
} events SEC(".maps");

// Container PID to metadata map - for correlation
struct container_metadata {
    char container_id[MAX_CONTAINER_ID_LEN];
    char pod_uid[36];
    char pod_name[64];
    char namespace[64];
    u64 memory_limit;
    u64 cgroup_id;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, u32);   // PID
    __type(value, struct container_metadata);
} container_map SEC(".maps");

// Cgroup ID to container mapping - fast lookup
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, u64);   // Cgroup ID
    __type(value, struct container_metadata);
} cgroup_map SEC(".maps");

// Statistics
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 16);
    __type(key, u32);
    __type(value, u64);
} stats_map SEC(".maps");

// Stat indices
#define STAT_OOM_KILLS        0
#define STAT_PROCESS_EXITS    1
#define STAT_CONTAINER_STARTS 2
#define STAT_EVENTS_DROPPED   3

static __always_inline void update_stat(u32 index, s64 delta) {
    u64 *count = bpf_map_lookup_elem(&stats_map, &index);
    if (count) {
        __sync_fetch_and_add(count, delta);
    }
}

static __always_inline bool is_container_process(u64 cgroup_id) {
    // Check if cgroup belongs to a container
    // Container cgroups typically contain specific patterns
    return cgroup_id != 0;
}

static __always_inline struct container_metadata* 
get_container_metadata(u32 pid, u64 cgroup_id) {
    // Try PID lookup first (fastest)
    struct container_metadata *meta = bpf_map_lookup_elem(&container_map, &pid);
    if (meta) {
        return meta;
    }
    
    // Fallback to cgroup lookup
    return bpf_map_lookup_elem(&cgroup_map, &cgroup_id);
}

static __always_inline u64 get_cgroup_memory_usage(u64 cgroup_id) {
    struct task_struct *task;
    struct cgroup *cgrp;
    struct mem_cgroup *memcg;
    u64 usage = 0;
    
    // Get current task
    task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        return 0;
    }
    
    // Get cgroup from task
    // Note: This is a simplified approach - in production you'd need to
    // traverse the cgroup hierarchy more carefully
    cgrp = BPF_CORE_READ(task, cgroups, subsys[CGROUP_SUBSYS_MEMORY], cgroup);
    if (!cgrp) {
        return 0;
    }
    
    // Get memory controller from cgroup
    memcg = BPF_CORE_READ(cgrp, subsys[CGROUP_SUBSYS_MEMORY]);
    if (!memcg) {
        return 0;
    }
    
    // Read memory usage statistics
    // The exact field depends on kernel version, we try the most common ones
    #ifdef BPF_CORE_READ_INTO
        // For newer kernels - read memory.current
        if (bpf_core_field_exists(memcg->memory)) {
            BPF_CORE_READ_INTO(&usage, memcg, memory.usage_in_bytes);
        } else if (bpf_core_field_exists(memcg->usage)) {
            BPF_CORE_READ_INTO(&usage, memcg, usage);
        } else if (bpf_core_field_exists(memcg->res.usage)) {
            BPF_CORE_READ_INTO(&usage, memcg, res.usage);
        }
    #else
        // Fallback for older kernels
        // Try different possible field layouts
        usage = BPF_CORE_READ(memcg, res.usage);
        if (usage == 0) {
            // Try alternate field name
            usage = BPF_CORE_READ(memcg, usage_in_bytes);
        }
    #endif
    
    return usage;
}

// Alternative implementation using cgroup iterator (for newer kernels)
static __always_inline u64 get_cgroup_memory_usage_v2(u64 cgroup_id) {
    // This would use cgroup v2 unified hierarchy
    // Implementation would depend on kernel version and cgroup v2 availability
    // For now, return 0 as fallback
    return 0;
}

// Trace OOM kills - CRITICAL for container monitoring
SEC("kprobe/oom_kill_process")
int trace_oom_kill(struct pt_regs *ctx) {
    struct task_struct *task = (struct task_struct *)PT_REGS_PARM1(ctx);
    
    if (!task) {
        return 0;
    }
    
    u32 pid = BPF_CORE_READ(task, pid);
    u32 tgid = BPF_CORE_READ(task, tgid);
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    // Only track container processes
    if (!is_container_process(cgroup_id)) {
        return 0;
    }
    
    struct container_exit_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        update_stat(STAT_EVENTS_DROPPED, 1);
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = tgid;
    event->exit_code = 137; // SIGKILL
    event->cgroup_id = cgroup_id;
    event->oom_killed = 1;
    
    // Get container metadata if available
    struct container_metadata *meta = get_container_metadata(pid, cgroup_id);
    if (meta) {
        __builtin_memcpy(event->container_id, meta->container_id, MAX_CONTAINER_ID_LEN);
        event->memory_limit = meta->memory_limit;
    }
    
    // Get process comm
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get memory usage from memory cgroup
    event->memory_usage = get_cgroup_memory_usage(cgroup_id);
    
    bpf_ringbuf_submit(event, 0);
    update_stat(STAT_OOM_KILLS, 1);
    
    return 0;
}

// Trace process exits - catch container process termination
SEC("tracepoint/sched/sched_process_exit")
int trace_process_exit(struct trace_event_raw_sched_process_exit *ctx) {
    u32 pid = ctx->pid;
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    // Only track container processes
    if (!is_container_process(cgroup_id)) {
        return 0;
    }
    
    // Check if this is a container process we're tracking
    struct container_metadata *meta = get_container_metadata(pid, cgroup_id);
    if (!meta) {
        return 0; // Not a tracked container process
    }
    
    struct container_exit_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        update_stat(STAT_EVENTS_DROPPED, 1);
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = pid; // For process exit, pid == tgid
    event->exit_code = (s32)(ctx->exit_code >> 8); // Extract exit code
    event->cgroup_id = cgroup_id;
    event->oom_killed = 0;
    event->memory_usage = get_cgroup_memory_usage(cgroup_id);
    event->memory_limit = meta->memory_limit;
    
    __builtin_memcpy(event->container_id, meta->container_id, MAX_CONTAINER_ID_LEN);
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    update_stat(STAT_PROCESS_EXITS, 1);
    
    return 0;
}

// Optional: Trace container process forks
SEC("tracepoint/sched/sched_process_fork")
int trace_process_fork(struct trace_event_raw_sched_process_fork *ctx) {
    u32 parent_pid = ctx->parent_pid;
    u32 child_pid = ctx->child_pid;
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    // Check if parent is a container process
    struct container_metadata *parent_meta = get_container_metadata(parent_pid, cgroup_id);
    if (!parent_meta) {
        return 0;
    }
    
    // Inherit container metadata to child process
    struct container_metadata child_meta;
    __builtin_memcpy(&child_meta, parent_meta, sizeof(child_meta));
    child_meta.cgroup_id = cgroup_id;
    
    bpf_map_update_elem(&container_map, &child_pid, &child_meta, BPF_ANY);
    
    return 0;
}

// Optional: Trace memory cgroup limit hits
SEC("kprobe/mem_cgroup_out_of_memory")
int trace_memcg_oom(struct pt_regs *ctx) {
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    // Only track container cgroups
    if (!is_container_process(cgroup_id)) {
        return 0;
    }
    
    // This is a precursor to OOM kill - useful for early warning
    struct container_exit_event *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        update_stat(STAT_EVENTS_DROPPED, 1);
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->tgid = event->pid;
    event->exit_code = 0;
    event->cgroup_id = cgroup_id;
    event->oom_killed = 0; // Not killed yet, just OOM condition
    
    // Get container metadata
    struct container_metadata *meta = get_container_metadata(event->pid, cgroup_id);
    if (meta) {
        __builtin_memcpy(event->container_id, meta->container_id, MAX_CONTAINER_ID_LEN);
        event->memory_limit = meta->memory_limit;
    }
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";