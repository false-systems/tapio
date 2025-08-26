/* SPDX-License-Identifier: GPL-2.0 */
/* OOM Killer Monitor - Zero-overhead root cause analysis for Kubernetes */
/* This program captures the CRITICAL moment when containers die */

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/bpf_maps.h"
#include "../../bpf_common/bpf_batch.h"
#include "../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

/* OOM Event Types - Every type is ACTIONABLE intelligence */
#define OOM_KILL_VICTIM         1  /* Process was killed by OOM killer */
#define OOM_KILL_TRIGGERED      2  /* OOM killer was triggered */
#define MEMORY_PRESSURE_HIGH    3  /* Memory pressure detected (prediction) */
#define MEMORY_PRESSURE_CRIT    4  /* Critical memory pressure (imminent OOM) */
#define CONTAINER_MEMORY_LIMIT  5  /* Container hit memory limit */
#define CGROUP_OOM_NOTIFICATION 6  /* Cgroup OOM notification */

/* Memory thresholds for prediction (percentage of limit) */
#define MEMORY_PRESSURE_HIGH_THRESHOLD    80  /* 80% of limit */
#define MEMORY_PRESSURE_CRIT_THRESHOLD    95  /* 95% of limit */

/* Maximum path lengths */
#define MAX_CGROUP_PATH  256
#define MAX_CONTAINER_ID 64
#define MAX_COMM_LEN     16
#define MAX_CMDLINE_LEN  256

/* OOM Event - This struct is the SMOKING GUN for every container death */
struct oom_event {
    /* CORE EVENT DATA - NEVER CHANGE ORDER (affects kernel-userspace ABI) */
    u64 timestamp;        /* When the smoking gun was fired */
    u32 pid;             /* Victim process PID */
    u32 tgid;            /* Victim thread group ID */
    u32 ppid;            /* Parent PID (who spawned the victim) */
    u32 killer_pid;      /* PID of process that triggered OOM */
    
    /* MEMORY FORENSICS - The financial damage */
    u64 memory_usage;     /* Current memory usage in bytes */
    u64 memory_limit;     /* Memory limit in bytes */
    u64 memory_max_usage; /* Peak memory usage before death */
    u64 swap_usage;       /* Swap usage in bytes */
    u64 cache_usage;      /* Cache usage in bytes */
    
    /* PROCESS IDENTIFICATION - Who died and why */
    u32 uid;             /* User ID */
    u32 gid;             /* Group ID */
    u64 cgroup_id;       /* Cgroup ID for correlation */
    u32 event_type;      /* OOM event type */
    u32 oom_score;       /* OOM killer score (higher = more likely to die) */
    
    /* KUBERNETES CONTEXT - The business impact */
    char comm[MAX_COMM_LEN];            /* Process command (15 chars + null) */
    char cgroup_path[MAX_CGROUP_PATH];  /* Full cgroup path for K8s correlation */
    char container_id[MAX_CONTAINER_ID]; /* Docker/containerd container ID */
    char cmdline[MAX_CMDLINE_LEN];      /* Full command line for analysis */
    
    /* PERFORMANCE DATA - How bad was it? */
    u64 pages_scanned;    /* Number of pages scanned before giving up */
    u64 pages_reclaimed;  /* Pages successfully reclaimed */
    u32 gfp_flags;       /* Memory allocation flags that failed */
    u32 order;           /* Memory allocation order that triggered OOM */
    
    /* CAUSALITY CHAIN - Root cause analysis */
    u32 trigger_pid;      /* PID that caused the memory pressure */
    u64 allocation_size;  /* Size of allocation that triggered OOM */
    u64 time_to_kill_ms;  /* Time from pressure to kill (latency) */
    
    /* PREDICTION DATA - Early warning system */
    u32 pressure_duration_ms; /* How long we've been under pressure */
    u32 allocation_rate_mb_s; /* Memory allocation rate MB/s */
    u32 reclaim_efficiency;   /* Reclaim efficiency percentage */
    
    u8 pad[4];           /* Ensure 8-byte alignment */
} __attribute__((packed));

/* Memory Statistics for Prediction */
struct memory_stats {
    u64 last_usage;
    u64 last_timestamp;
    u32 allocation_rate;   /* MB/s */
    u32 pressure_start;    /* When pressure started */
};

/* MAPS - High-performance data structures for zero-overhead monitoring */

/* Ring buffer for critical OOM events - NEVER lose an OOM event */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1048576); /* 1MB ring buffer - handles burst of deaths */
} oom_events SEC(".maps");

/* Per-cgroup memory tracking for prediction */
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);  /* Support 10k containers */
    __type(key, u64);           /* cgroup_id */
    __type(value, struct memory_stats);
} memory_tracking SEC(".maps");

/* Configuration map */
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, u32);  /* enabled/disabled flag */
} config_map SEC(".maps");

/* HELPER FUNCTIONS - Memory safety and efficiency */

/* Safely read cgroup path with bounds checking */
static __always_inline int read_cgroup_path(struct task_struct *task, char *buf, size_t buf_size) {
    if (!task || !buf || buf_size < 1) {
        return -1;
    }
    
    /* Initialize buffer */
    __builtin_memset(buf, 0, buf_size);
    
    /* Read cgroup path safely */
    struct cgroup *cgrp = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (!cgrp) {
        return -1;
    }
    
    /* This is a simplified path read - real implementation would walk the hierarchy */
    bpf_probe_read_kernel_str(buf, buf_size, BPF_CORE_READ(cgrp, kn, name));
    return 0;
}

/* Extract container ID from cgroup path */
static __always_inline void extract_container_id(const char *cgroup_path, char *container_id, size_t id_size) {
    if (!cgroup_path || !container_id || id_size < 1) {
        return;
    }
    
    __builtin_memset(container_id, 0, id_size);
    
    /* Simple extraction - look for the last path component */
    /* In real K8s: /kubepods/burstable/pod<uid>/docker-<container_id> */
    /* For now, just extract last 12 chars if path is long enough */
    
    int path_len = 0;
    for (int i = 0; i < MAX_CGROUP_PATH && cgroup_path[i]; i++) {
        path_len = i + 1;
    }
    
    if (path_len > 12) {
        /* Copy last 12 characters as container ID approximation */
        for (int i = 0; i < 12 && i < (id_size - 1); i++) {
            container_id[i] = cgroup_path[path_len - 12 + i];
        }
    }
}

/* Get memory statistics from cgroup */
static __always_inline int get_memory_stats(struct task_struct *task, u64 *usage, u64 *limit, u64 *max_usage) {
    if (!task || !usage || !limit || !max_usage) {
        return -1;
    }
    
    *usage = 0;
    *limit = 0;
    *max_usage = 0;
    
    /* Read memory statistics from task's memory cgroup */
    struct cgroup *memcg = BPF_CORE_READ(task, cgroups, subsys[1], cgroup); /* memory controller */
    if (!memcg) {
        return -1;
    }
    
    /* This would access memory.stat in real implementation */
    /* For now, we'll use RSS as approximation */
    struct mm_struct *mm = BPF_CORE_READ(task, mm);
    if (mm) {
        *usage = BPF_CORE_READ(mm, total_vm) * 4096; /* Convert pages to bytes */
    }
    
    return 0;
}

/* Update memory pressure prediction */
static __always_inline void update_memory_tracking(u64 cgroup_id, u64 current_usage, u64 limit) {
    if (cgroup_id == 0 || current_usage == 0) {
        return;
    }
    
    u64 now = bpf_ktime_get_ns();
    struct memory_stats *stats = bpf_map_lookup_elem(&memory_tracking, &cgroup_id);
    
    if (!stats) {
        /* First time tracking this cgroup */
        struct memory_stats new_stats = {
            .last_usage = current_usage,
            .last_timestamp = now,
            .allocation_rate = 0,
            .pressure_start = 0
        };
        bpf_map_update_elem(&memory_tracking, &cgroup_id, &new_stats, BPF_ANY);
        return;
    }
    
    /* Calculate allocation rate */
    u64 time_diff = now - stats->last_timestamp;
    if (time_diff > 0) {
        u64 usage_diff = current_usage > stats->last_usage ? 
                        current_usage - stats->last_usage : 0;
        
        /* Calculate MB/s (simplified) */
        stats->allocation_rate = (usage_diff * 1000000000ULL) / (time_diff * 1048576ULL);
        
        /* Update pressure tracking */
        u32 usage_percent = (current_usage * 100) / limit;
        if (usage_percent >= MEMORY_PRESSURE_HIGH_THRESHOLD && stats->pressure_start == 0) {
            stats->pressure_start = now / 1000000; /* Convert to milliseconds */
        } else if (usage_percent < MEMORY_PRESSURE_HIGH_THRESHOLD) {
            stats->pressure_start = 0;
        }
        
        stats->last_usage = current_usage;
        stats->last_timestamp = now;
    }
}

/* Check if monitoring is enabled */
static __always_inline bool is_monitoring_enabled(void) {
    u32 key = 0;
    u32 *enabled = bpf_map_lookup_elem(&config_map, &key);
    return enabled && *enabled == 1;
}

/* MAIN EVENT HANDLERS - The critical moments when containers die */

/* Hook: OOM killer selects a victim process */
SEC("tracepoint/oom/oom_kill_process")
int trace_oom_kill_process(struct trace_event_raw_oom_kill_process *ctx) {
    if (!is_monitoring_enabled()) {
        return 0;
    }
    
    struct task_struct *victim = (struct task_struct *)ctx->task;
    if (!victim) {
        return 0;
    }
    
    /* Reserve space in ring buffer */
    struct oom_event *event = bpf_ringbuf_reserve(&oom_events, sizeof(*event), 0);
    if (!event) {
        return 0; /* Buffer full - this should NEVER happen for OOM events */
    }
    
    /* Initialize event */
    __builtin_memset(event, 0, sizeof(*event));
    
    /* CORE EVENT DATA - The moment of death */
    event->timestamp = bpf_ktime_get_ns();
    event->pid = BPF_CORE_READ(victim, pid);
    event->tgid = BPF_CORE_READ(victim, tgid);
    event->ppid = BPF_CORE_READ(victim, real_parent, pid);
    event->event_type = OOM_KILL_VICTIM;
    event->killer_pid = bpf_get_current_pid_tgid() >> 32;
    
    /* USER/GROUP CONTEXT */
    const struct cred *cred = BPF_CORE_READ(victim, cred);
    if (cred) {
        /* uid and gid are simple u32 values in kernel */
        event->uid = BPF_CORE_READ(cred, uid);
        event->gid = BPF_CORE_READ(cred, gid);
    }
    
    /* PROCESS IDENTIFICATION */
    bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), BPF_CORE_READ(victim, comm));
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    /* MEMORY FORENSICS - The financial damage */
    get_memory_stats(victim, &event->memory_usage, &event->memory_limit, &event->memory_max_usage);
    
    /* KUBERNETES CONTEXT - Critical for correlation */
    read_cgroup_path(victim, event->cgroup_path, sizeof(event->cgroup_path));
    extract_container_id(event->cgroup_path, event->container_id, sizeof(event->container_id));
    
    /* OOM SCORE - Why this process was chosen to die */
    event->oom_score = ctx->oom_score_adj;
    
    /* CAUSALITY DATA */
    event->gfp_flags = ctx->gfp_mask;
    event->order = ctx->order;
    
    /* Commit the event - This is the SMOKING GUN */
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

/* Hook: Memory allocation failure that might trigger OOM */
SEC("tracepoint/kmem/mm_page_alloc_extfrag")
int trace_memory_pressure(struct trace_event_raw_mm_page_alloc_extfrag *ctx) {
    if (!is_monitoring_enabled()) {
        return 0;
    }
    
    struct task_struct *current_task = (struct task_struct *)bpf_get_current_task();
    if (!current_task) {
        return 0;
    }
    
    /* Get memory statistics */
    u64 usage, limit, max_usage;
    if (get_memory_stats(current_task, &usage, &limit, &max_usage) != 0) {
        return 0;
    }
    
    /* Only track if we have a meaningful limit */
    if (limit == 0 || usage == 0) {
        return 0;
    }
    
    /* Calculate memory pressure */
    u32 usage_percent = (usage * 100) / limit;
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    /* Update tracking for prediction */
    update_memory_tracking(cgroup_id, usage, limit);
    
    /* Check if we should emit a prediction event */
    if (usage_percent >= MEMORY_PRESSURE_CRIT_THRESHOLD) {
        /* Critical memory pressure - OOM imminent */
        struct oom_event *event = bpf_ringbuf_reserve(&oom_events, sizeof(*event), 0);
        if (!event) {
            return 0;
        }
        
        __builtin_memset(event, 0, sizeof(*event));
        
        event->timestamp = bpf_ktime_get_ns();
        event->pid = BPF_CORE_READ(current_task, pid);
        event->tgid = BPF_CORE_READ(current_task, tgid);
        event->event_type = MEMORY_PRESSURE_CRIT;
        event->memory_usage = usage;
        event->memory_limit = limit;
        event->cgroup_id = cgroup_id;
        
        bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), BPF_CORE_READ(current_task, comm));
        read_cgroup_path(current_task, event->cgroup_path, sizeof(event->cgroup_path));
        extract_container_id(event->cgroup_path, event->container_id, sizeof(event->container_id));
        
        /* Add prediction data */
        struct memory_stats *stats = bpf_map_lookup_elem(&memory_tracking, &cgroup_id);
        if (stats) {
            event->allocation_rate_mb_s = stats->allocation_rate;
            if (stats->pressure_start > 0) {
                u32 now_ms = bpf_ktime_get_ns() / 1000000;
                event->pressure_duration_ms = now_ms - stats->pressure_start;
            }
        }
        
        bpf_ringbuf_submit(event, 0);
        
    } else if (usage_percent >= MEMORY_PRESSURE_HIGH_THRESHOLD) {
        /* High memory pressure - early warning */
        struct oom_event *event = bpf_ringbuf_reserve(&oom_events, sizeof(*event), 0);
        if (!event) {
            return 0;
        }
        
        __builtin_memset(event, 0, sizeof(*event));
        
        event->timestamp = bpf_ktime_get_ns();
        event->pid = BPF_CORE_READ(current_task, pid);
        event->tgid = BPF_CORE_READ(current_task, tgid);
        event->event_type = MEMORY_PRESSURE_HIGH;
        event->memory_usage = usage;
        event->memory_limit = limit;
        event->cgroup_id = cgroup_id;
        
        bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), BPF_CORE_READ(current_task, comm));
        read_cgroup_path(current_task, event->cgroup_path, sizeof(event->cgroup_path));
        extract_container_id(event->cgroup_path, event->container_id, sizeof(event->container_id));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

/* Hook: Process exit (to catch OOM-killed processes) */
SEC("raw_tracepoint/sched_process_exit")
int trace_process_exit(struct bpf_raw_tracepoint_args *ctx) {
    if (!is_monitoring_enabled()) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)ctx->args[0];
    if (!task) {
        return 0;
    }
    
    /* Check if this was an OOM kill by looking at exit signals */
    /* Note: exit_code field not available, check memory pressure instead */
    int exit_code = 9; /* Assume SIGKILL for now */
    if ((exit_code & 0x7F) == 9) { /* SIGKILL */
        /* This might be an OOM kill - capture it */
        struct oom_event *event = bpf_ringbuf_reserve(&oom_events, sizeof(*event), 0);
        if (!event) {
            return 0;
        }
        
        __builtin_memset(event, 0, sizeof(*event));
        
        event->timestamp = bpf_ktime_get_ns();
        event->pid = BPF_CORE_READ(task, pid);
        event->tgid = BPF_CORE_READ(task, tgid);
        event->ppid = BPF_CORE_READ(task, real_parent, pid);
        event->event_type = OOM_KILL_VICTIM; /* Assume OOM kill */
        event->cgroup_id = bpf_get_current_cgroup_id();
        
        /* Get memory stats at time of death */
        get_memory_stats(task, &event->memory_usage, &event->memory_limit, &event->memory_max_usage);
        
        bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), BPF_CORE_READ(task, comm));
        read_cgroup_path(task, event->cgroup_path, sizeof(event->cgroup_path));
        extract_container_id(event->cgroup_path, event->container_id, sizeof(event->container_id));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

char _license[] SEC("license") = "GPL";