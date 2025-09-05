// SPDX-License-Identifier: GPL-2.0
// Kernel definitions for resource starvation monitoring
// Using REAL kernel structures with CO-RE for proper access

#ifndef __KERNEL_DEFS_H
#define __KERNEL_DEFS_H

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Map types that might be missing
#ifndef BPF_MAP_TYPE_STACK_TRACE
#define BPF_MAP_TYPE_STACK_TRACE 7
#endif

// Stack trace flags
#ifndef BPF_F_FAST_STACK_CMP
#define BPF_F_FAST_STACK_CMP (1ULL << 9)
#endif

#ifndef BPF_F_REUSE_STACKID
#define BPF_F_REUSE_STACKID  (1ULL << 10)
#endif

#ifndef BPF_F_USER_STACK
#define BPF_F_USER_STACK     (1ULL << 8)
#endif

// Helper to check if task is throttled using CO-RE
// Now we can access the real sched_entity!
static __always_inline bool 
is_task_throttled(struct task_struct *task) {
    // Use CO-RE to check if throttled field exists (CONFIG_CFS_BANDWIDTH)
    if (bpf_core_field_exists(task->se.throttled)) {
        int throttled = BPF_CORE_READ(task, se.throttled);
        return throttled != 0;
    }
    return false;
}

// Helper to get throttle count
static __always_inline __u32
get_throttle_count(struct task_struct *task) {
    if (bpf_core_field_exists(task->se.throttle_count)) {
        return BPF_CORE_READ(task, se.throttle_count);
    }
    return 0;
}

// Helper to get throttled time
static __always_inline __u64 
get_throttled_time(struct task_struct *task) {
    if (bpf_core_field_exists(task->se.throttled_clock) && 
        bpf_core_field_exists(task->se.throttled_clock_task)) {
        __u64 throttled_clock = BPF_CORE_READ(task, se.throttled_clock);
        __u64 throttled_clock_task = BPF_CORE_READ(task, se.throttled_clock_task);
        return throttled_clock > throttled_clock_task ? 
               throttled_clock - throttled_clock_task : 0;
    }
    return 0;
}

// Helper to get task runtime
static __always_inline __u64
get_task_runtime(struct task_struct *task) {
    if (bpf_core_field_exists(task->se.sum_exec_runtime)) {
        return BPF_CORE_READ(task, se.sum_exec_runtime);
    }
    return 0;
}

// Helper to get vruntime (virtual runtime for CFS fairness)
static __always_inline __u64
get_task_vruntime(struct task_struct *task) {
    if (bpf_core_field_exists(task->se.vruntime)) {
        return BPF_CORE_READ(task, se.vruntime);
    }
    return 0;
}

// Helper to detect wait time from statistics
static __always_inline __u64
get_task_wait_sum(struct task_struct *task) {
    if (bpf_core_field_exists(task->se.statistics.wait_sum)) {
        return BPF_CORE_READ(task, se.statistics.wait_sum);
    }
    return 0;
}

// Fallback: Behavioral detection for kernels without CONFIG_CFS_BANDWIDTH
static __always_inline bool 
is_likely_throttled(__u64 wait_time_ns, __u64 runtime_ns) {
    // If wait time is > 100ms and runtime < 10ms, likely throttled
    if (wait_time_ns > 100000000 && runtime_ns < 10000000) {
        return true;
    }
    
    // If wait/runtime ratio > 10, likely throttled
    if (runtime_ns > 0 && (wait_time_ns / runtime_ns) > 10) {
        return true;
    }
    
    return false;
}

#endif /* __KERNEL_DEFS_H */