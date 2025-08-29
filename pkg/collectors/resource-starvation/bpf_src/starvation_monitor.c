// SPDX-License-Identifier: GPL-2.0
// Resource Starvation Monitor - Reveals the invisible latency
// This program captures CPU scheduling delays, throttling, and noisy neighbors

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Event types - The different ways your app gets starved
#define EVENT_SCHED_WAIT      1  // Waiting for CPU (invisible in metrics)
#define EVENT_CFS_THROTTLE    2  // Hit CPU quota, forced to wait
#define EVENT_PRIORITY_INVERT 3  // Low-pri blocking high-pri
#define EVENT_CORE_MIGRATE    4  // Bounced between CPUs (cache killer)
#define EVENT_NOISY_NEIGHBOR  5  // Someone else hogging CPU

// Thresholds for detection (in nanoseconds)
#define STARVATION_THRESHOLD_NS   100000000  // 100ms wait = starvation
#define SEVERE_THRESHOLD_NS       500000000  // 500ms wait = severe
#define THROTTLE_THRESHOLD_NS     10000000   // 10ms throttle = problem

// Maximum tracked processes
#define MAX_TRACKED_PIDS 10000

struct starvation_event {
    // When and what
    __u64 timestamp;
    __u32 event_type;
    __u32 cpu_core;

    // The victim (who got starved)
    __u32 victim_pid;
    __u32 victim_tgid;
    __u64 wait_time_ns;     // How long they waited (THE INVISIBLE METRIC!)
    __u64 run_time_ns;      // How long they ran after waiting

    // The culprit (who caused starvation)
    __u32 culprit_pid;
    __u32 culprit_tgid;
    __u64 culprit_runtime;  // How much CPU they consumed

    // Throttling data (container hit limits)
    __u64 throttled_ns;     // Total throttle time
    __u32 nr_periods;       // Number of periods
    __u32 nr_throttled;     // Times throttled

    // Context for correlation
    __u64 victim_cgroup_id;
    __u64 culprit_cgroup_id;
    char victim_comm[16];
    char culprit_comm[16];

    // Scheduling info
    __s32 victim_prio;      // Process priority
    __s32 culprit_prio;
    __u32 victim_policy;    // SCHED_NORMAL, SCHED_FIFO, etc.
} __attribute__((packed));

// Ring buffer for events - never lose a starvation event
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);  // 1MB buffer
} events SEC(".maps");

// Track per-task CPU consumption for culprit detection
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

// Track scheduling delays - THE INVISIBLE LATENCY
SEC("tracepoint/sched/sched_stat_wait")
int trace_sched_wait(struct trace_event_raw_sched_stat_wait *ctx)
{
    __u64 delay = ctx->delay;

    // Ignore small delays (normal context switching)
    if (delay < STARVATION_THRESHOLD_NS) {
        return 0;
    }

    struct starvation_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }

    __builtin_memset(e, 0, sizeof(*e));

    e->timestamp = bpf_ktime_get_ns();
    e->event_type = EVENT_SCHED_WAIT;
    e->cpu_core = bpf_get_smp_processor_id();

    // Victim info (who waited)
    e->victim_pid = ctx->pid;
    e->victim_tgid = ctx->tgid;
    e->wait_time_ns = delay;

    // Get victim process name
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        bpf_probe_read_kernel_str(e->victim_comm, sizeof(e->victim_comm),
                                  BPF_CORE_READ(task, comm));
        e->victim_cgroup_id = bpf_get_current_cgroup_id();
        e->victim_prio = BPF_CORE_READ(task, prio);
        e->victim_policy = BPF_CORE_READ(task, policy);
    }

    // Find potential culprit (highest CPU consumer on same core)
    // This is simplified - real implementation would track per-core leaders
    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &e->victim_pid);
    if (stats) {
        e->run_time_ns = stats->total_runtime;
    }

    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Track CFS bandwidth throttling - containers hitting CPU limits
SEC("tracepoint/sched/sched_stat_runtime")
int trace_throttle(struct trace_event_raw_sched_stat_runtime *ctx)
{
    __u32 pid = ctx->pid;
    __u64 runtime = ctx->runtime;

    // Update task runtime tracking
    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &pid);
    if (!stats) {
        struct task_stats new_stats = {
            .total_runtime = runtime,
            .last_seen = bpf_ktime_get_ns(),
            .throttle_count = 0,
            .migrations = 0
        };
        bpf_map_update_elem(&task_tracking, &pid, &new_stats, BPF_ANY);
    } else {
        stats->total_runtime += runtime;
        stats->last_seen = bpf_ktime_get_ns();
    }

    return 0;
}

// Track CPU migrations (cache thrashing)
SEC("tracepoint/sched/sched_migrate_task")
int trace_migrate(struct trace_event_raw_sched_migrate_task *ctx)
{
    __u32 pid = ctx->pid;
    __u32 orig_cpu = ctx->orig_cpu;
    __u32 dest_cpu = ctx->dest_cpu;

    // Migrations destroy CPU cache, causing hidden latency
    struct task_stats *stats = bpf_map_lookup_elem(&task_tracking, &pid);
    if (stats) {
        stats->migrations++;

        // Frequent migrations = potential starvation symptom
        if (stats->migrations > 10) {
            struct starvation_event *e;
            e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (!e) {
                return 0;
            }

            __builtin_memset(e, 0, sizeof(*e));

            e->timestamp = bpf_ktime_get_ns();
            e->event_type = EVENT_CORE_MIGRATE;
            e->victim_pid = pid;
            e->cpu_core = orig_cpu;

            bpf_get_current_comm(e->victim_comm, sizeof(e->victim_comm));

            bpf_ringbuf_submit(e, 0);
        }
    }

    return 0;
}

// Track context switches to identify CPU hogs
SEC("tracepoint/sched/sched_switch")
int trace_sched_switch(struct trace_event_raw_sched_switch *ctx)
{
    __u32 prev_pid = ctx->prev_pid;
    __u32 next_pid = ctx->next_pid;
    __u64 now = bpf_ktime_get_ns();

    // Track how long the previous task ran
    struct task_stats *prev_stats = bpf_map_lookup_elem(&task_tracking, &prev_pid);
    if (prev_stats && prev_stats->last_seen > 0) {
        __u64 runtime = now - prev_stats->last_seen;

        // Detect CPU hogs (running for >100ms continuously)
        if (runtime > STARVATION_THRESHOLD_NS) {
            // This process is hogging CPU, potentially starving others
            struct starvation_event *e;
            e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (!e) {
                return 0;
            }

            __builtin_memset(e, 0, sizeof(*e));

            e->timestamp = now;
            e->event_type = EVENT_NOISY_NEIGHBOR;
            e->culprit_pid = prev_pid;
            e->culprit_runtime = runtime;
            e->cpu_core = bpf_get_smp_processor_id();

            // Get culprit name
            bpf_probe_read_kernel_str(e->culprit_comm, sizeof(e->culprit_comm),
                                     ctx->prev_comm);
            e->culprit_cgroup_id = bpf_get_current_cgroup_id();

            bpf_ringbuf_submit(e, 0);
        }
    }

    // Update next task's last seen time
    struct task_stats *next_stats = bpf_map_lookup_elem(&task_tracking, &next_pid);
    if (!next_stats) {
        struct task_stats new_stats = {
            .total_runtime = 0,
            .last_seen = now,
            .throttle_count = 0,
            .migrations = 0
        };
        bpf_map_update_elem(&task_tracking, &next_pid, &new_stats, BPF_ANY);
    } else {
        next_stats->last_seen = now;
    }

    return 0;
}

char _license[] SEC("license") = "GPL";
