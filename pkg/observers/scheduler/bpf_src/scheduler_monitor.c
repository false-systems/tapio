// SPDX-License-Identifier: GPL-2.0
// Scheduler Monitor - Reveals the invisible latency
// This program captures CPU scheduling delays, throttling, and noisy neighbors

#include "kernel_defs.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Event types
#define EVENT_SCHED_WAIT      1
#define EVENT_CFS_THROTTLE    2
#define EVENT_PRIORITY_INVERT 3
#define EVENT_CORE_MIGRATE    4
#define EVENT_NOISY_NEIGHBOR  5

// Default thresholds (in nanoseconds) - configurable via map
#define DEFAULT_SCHED_DELAY_THRESHOLD_NS   100000000  // 100ms
#define DEFAULT_THROTTLE_THRESHOLD_NS     10000000   // 10ms
#define DEFAULT_MIGRATION_THRESHOLD       10

// Maximum tracked processes
#define MAX_TRACKED_PIDS 10000
#define MAX_STACK_DEPTH   8

struct scheduler_event {
    __u64 timestamp;
    __u32 event_type;
    __u32 cpu_core;

    __u32 victim_pid;
    __u32 victim_tgid;
    __u64 wait_time_ns;
    __u64 run_time_ns;

    __u32 culprit_pid;
    __u32 culprit_tgid;
    __u64 culprit_runtime;

    __u64 throttled_ns;
    __u32 nr_periods;
    __u32 nr_throttled;

    __u64 victim_cgroup_id;
    __u64 culprit_cgroup_id;
    char victim_comm[16];
    char culprit_comm[16];

    __s32 victim_prio;
    __s32 culprit_prio;
    __u32 victim_policy;

    __s64 stack_id;  // Stack trace ID
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);  // 1MB
} events SEC(".maps");

// Track per-task stats
struct task_stats {
    __u64 total_runtime;
    __u64 last_seen;
    __u32 throttle_count;
    __u32 migrations;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_TRACKED_PIDS);
    __type(key, __u32);  // PID
    __type(value, struct task_stats);
} task_tracking SEC(".maps");

// Config map for thresholds (user-space updatable)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4);
    __type(key, __u32);  // Key: 0=sched_delay_ns, 1=throttle_ns, 2=severe_ns, 3=migration_count
    __type(value, __u64);
} config_map SEC(".maps");

// Filter map for PIDs (key: PID, value: 1 to track)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);
    __type(value, __u8);
} filter_map SEC(".maps");

// Per-CPU top hog tracker (for culprit detection)
struct hog_info {
    __u32 pid;
    __u64 runtime;
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);  // One per CPU
    __type(key, __u32);      // Key 0
    __type(value, struct hog_info);
} per_cpu_hogs SEC(".maps");

// Dropped events counter
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);  // Key 0
    __type(value, __u64);
} dropped_events SEC(".maps");

// Stack traces map
struct {
    __uint(type, BPF_MAP_TYPE_STACK_TRACE);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, __u64[MAX_STACK_DEPTH]);
} stack_traces SEC(".maps");

// Helper to get config value
static __always_inline __u64 get_config(__u32 key, __u64 default_val) {
    __u64 *val = bpf_map_lookup_elem(&config_map, &key);
    return val ? *val : default_val;
}

// Helper to check filter - if filter map is empty, track everything
static __always_inline bool is_filtered(__u32 pid) {
    // First check if this PID is in the filter
    __u8 *val = bpf_map_lookup_elem(&filter_map, &pid);
    if (val && *val == 1) {
        return true;  // Explicitly included
    }
    
    // Check if filter is being used at all by looking for a special marker (PID 0)
    __u32 marker_pid = 0;
    __u8 *marker = bpf_map_lookup_elem(&filter_map, &marker_pid);
    if (!marker) {
        // No marker means filter is not active, track everything
        return true;
    }
    
    // Filter is active but this PID is not in it
    return false;
}

SEC("tracepoint/sched/sched_stat_wait")
int trace_sched_wait(struct trace_event_raw_sched_stat_wait *ctx) {
    __u32 pid = ctx->pid;
    if (!is_filtered(pid)) return 0;

    __u64 delay = ctx->delay;
    __u64 threshold = get_config(0, DEFAULT_SCHED_DELAY_THRESHOLD_NS);
    if (delay < threshold) return 0;

    struct scheduler_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        __u32 key = 0;
        __u64 *count = bpf_map_lookup_elem(&dropped_events, &key);
        if (count) __sync_fetch_and_add(count, 1);
        return 0;
    }

    __builtin_memset(e, 0, sizeof(*e));

    e->timestamp = bpf_ktime_get_ns();
    e->event_type = EVENT_SCHED_WAIT;
    e->cpu_core = bpf_get_smp_processor_id();
    e->victim_pid = pid;
    e->victim_tgid = bpf_get_current_pid_tgid() >> 32;
    e->wait_time_ns = delay;

    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        bpf_probe_read_kernel_str(e->victim_comm, sizeof(e->victim_comm), BPF_CORE_READ(task, comm));
        e->victim_cgroup_id = bpf_get_current_cgroup_id();
        e->victim_prio = BPF_CORE_READ(task, prio);
        e->victim_policy = BPF_CORE_READ(task, policy);
    }

    // Get run time from stats
    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &pid);
    if (stats) {
        e->run_time_ns = stats->total_runtime;
    }

    // Culprit: Get top hog from per_cpu_hogs
    __u32 key = 0;
    struct hog_info *hog = bpf_map_lookup_elem(&per_cpu_hogs, &key);
    if (hog) {
        e->culprit_pid = hog->pid;
        e->culprit_runtime = hog->runtime;
    }

    // Capture stack trace
    e->stack_id = bpf_get_stackid(ctx, &stack_traces, BPF_F_FAST_STACK_CMP);

    bpf_ringbuf_submit(e, 0);
    return 0;
}

SEC("tracepoint/sched/sched_stat_runtime")
int trace_throttle(struct trace_event_raw_sched_stat_runtime *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    if (!is_filtered(pid)) return 0;

    __u64 runtime = ctx->runtime;
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();

    // Update stats
    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &pid);
    if (!stats) {
        struct task_stats new_stats = { .total_runtime = runtime, .last_seen = bpf_ktime_get_ns() };
        bpf_map_update_elem(&task_tracking, &pid, &new_stats, BPF_ANY);
        stats = &new_stats;
    } else {
        stats->total_runtime += runtime;
        stats->last_seen = bpf_ktime_get_ns();
    }

    // Check for REAL CFS throttling using CO-RE access to task->se
    if (is_task_throttled(task)) {
        __u64 throttled_time = get_throttled_time(task);
        __u64 threshold = get_config(1, DEFAULT_THROTTLE_THRESHOLD_NS);
        
        // Also check behavioral pattern as fallback
        if (throttled_time > threshold || 
            (throttled_time == 0 && is_likely_throttled(stats->last_seen, runtime))) {
            
            struct scheduler_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (!e) {
                __u32 key = 0;
                __u64 *count = bpf_map_lookup_elem(&dropped_events, &key);
                if (count) __sync_fetch_and_add(count, 1);
                return 0;
            }

            __builtin_memset(e, 0, sizeof(*e));

            e->timestamp = bpf_ktime_get_ns();
            e->event_type = EVENT_CFS_THROTTLE;
            e->cpu_core = bpf_get_smp_processor_id();
            e->victim_pid = pid;
            e->victim_tgid = bpf_get_current_pid_tgid() >> 32;
            e->throttled_ns = throttled_time > 0 ? throttled_time : stats->last_seen;
            e->run_time_ns = get_task_runtime(task);
            e->nr_throttled = get_throttle_count(task);
            
            bpf_get_current_comm(e->victim_comm, sizeof(e->victim_comm));
            e->victim_cgroup_id = bpf_get_current_cgroup_id();
            
            // Get scheduling priority info
            e->victim_prio = BPF_CORE_READ(task, prio);
            e->victim_policy = BPF_CORE_READ(task, policy);

            // Capture stack trace
            e->stack_id = bpf_get_stackid(ctx, &stack_traces, BPF_F_FAST_STACK_CMP);

            bpf_ringbuf_submit(e, 0);
            
            // Update our throttle count
            if (stats) {
                stats->throttle_count++;
            }
        }
    }

    return 0;
}

SEC("tracepoint/sched/sched_migrate_task")
int trace_migrate(struct trace_event_raw_sched_migrate_task *ctx) {
    __u32 pid = ctx->pid;
    if (!is_filtered(pid)) return 0;

    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &pid);
    if (stats) {
        stats->migrations++;
        __u64 threshold = get_config(3, DEFAULT_MIGRATION_THRESHOLD);
        if (stats->migrations > threshold) {
            struct scheduler_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (!e) {
                __u32 key = 0;
                __u64 *count = bpf_map_lookup_elem(&dropped_events, &key);
                if (count) __sync_fetch_and_add(count, 1);
                return 0;
            }

            __builtin_memset(e, 0, sizeof(*e));

            e->timestamp = bpf_ktime_get_ns();
            e->event_type = EVENT_CORE_MIGRATE;
            e->victim_pid = pid;
            e->cpu_core = ctx->orig_cpu;
            bpf_get_current_comm(e->victim_comm, sizeof(e->victim_comm));
            e->victim_cgroup_id = bpf_get_current_cgroup_id();

            // Capture stack trace
            e->stack_id = bpf_get_stackid(ctx, &stack_traces, BPF_F_FAST_STACK_CMP);

            bpf_ringbuf_submit(e, 0);
        }
    }
    return 0;
}

SEC("tracepoint/sched/sched_switch")
int trace_sched_switch(struct trace_event_raw_sched_switch *ctx) {
    __u32 prev_pid = ctx->prev_pid;
    __u32 next_pid = ctx->next_pid;
    __u64 now = bpf_ktime_get_ns();
    __u32 cpu = bpf_get_smp_processor_id();

    if (!is_filtered(prev_pid) && !is_filtered(next_pid)) return 0;

    // Update prev runtime and check for hog
    struct task_stats *prev_stats = bpf_map_lookup_elem(&task_tracking, &prev_pid);
    if (prev_stats && prev_stats->last_seen > 0) {
        __u64 runtime = now - prev_stats->last_seen;
        __u64 threshold = get_config(0, DEFAULT_SCHED_DELAY_THRESHOLD_NS);
        if (runtime > threshold) {
            // Update per-CPU hog if this is longer
            __u32 key = 0;
            struct hog_info *hog = bpf_map_lookup_elem(&per_cpu_hogs, &key);
            if (!hog) {
                struct hog_info new_hog = {0};
                bpf_map_update_elem(&per_cpu_hogs, &key, &new_hog, BPF_ANY);
                hog = bpf_map_lookup_elem(&per_cpu_hogs, &key);
            }
            if (hog && runtime > hog->runtime) {
                hog->pid = prev_pid;
                hog->runtime = runtime;
            }

            // Generate noisy neighbor event
            struct scheduler_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (!e) {
                __u64 *count = bpf_map_lookup_elem(&dropped_events, &key);
                if (count) __sync_fetch_and_add(count, 1);
                return 0;
            }

            __builtin_memset(e, 0, sizeof(*e));

            e->timestamp = now;
            e->event_type = EVENT_NOISY_NEIGHBOR;
            e->culprit_pid = prev_pid;
            e->culprit_runtime = runtime;
            e->cpu_core = cpu;
            bpf_probe_read_kernel_str(e->culprit_comm, sizeof(e->culprit_comm), ctx->prev_comm);
            e->culprit_cgroup_id = bpf_get_current_cgroup_id();

            // Capture stack trace
            e->stack_id = bpf_get_stackid(ctx, &stack_traces, BPF_F_FAST_STACK_CMP);

            bpf_ringbuf_submit(e, 0);
        }
    }

    // Check for priority inversion
    // In sched_switch, we have prev_prio and next_prio directly
    if (ctx->prev_prio < ctx->next_prio) {  // Higher priority task (lower number) being preempted
        struct scheduler_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
        if (!e) {
            __u32 key = 0;
            __u64 *count = bpf_map_lookup_elem(&dropped_events, &key);
            if (count) __sync_fetch_and_add(count, 1);
            return 0;
        }

        __builtin_memset(e, 0, sizeof(*e));

        e->timestamp = now;
        e->event_type = EVENT_PRIORITY_INVERT;
        e->victim_pid = prev_pid;
        e->culprit_pid = next_pid;
        e->cpu_core = cpu;
        e->victim_prio = ctx->prev_prio;
        e->culprit_prio = ctx->next_prio;
        bpf_probe_read_kernel_str(e->victim_comm, sizeof(e->victim_comm), ctx->prev_comm);
        bpf_probe_read_kernel_str(e->culprit_comm, sizeof(e->culprit_comm), ctx->next_comm);

        // Get cgroup IDs
        e->victim_cgroup_id = bpf_get_current_cgroup_id();
        e->culprit_cgroup_id = e->victim_cgroup_id; // Same CPU, likely same cgroup

        // Capture stack trace
        e->stack_id = bpf_get_stackid(ctx, &stack_traces, BPF_F_FAST_STACK_CMP);

        bpf_ringbuf_submit(e, 0);
    }

    // Update next last_seen
    struct task_stats *next_stats = bpf_map_lookup_elem(&task_tracking, &next_pid);
    if (!next_stats) {
        struct task_stats new_stats = { .last_seen = now };
        bpf_map_update_elem(&task_tracking, &next_pid, &new_stats, BPF_ANY);
    } else {
        next_stats->last_seen = now;
    }

    return 0;
}

char _license[] SEC("license") = "GPL";