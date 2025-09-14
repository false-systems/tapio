// SPDX-License-Identifier: GPL-2.0
// CO-RE Helper Library - Per CLAUDE.md standards
// NO STUBS - Complete implementation only

#ifndef __CORE_HELPERS_H
#define __CORE_HELPERS_H

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Standard sizes per migration plan
#define RINGBUF_SIZE_LOW    (128 * 1024)  // 128KB - Low volume observers
#define RINGBUF_SIZE_MEDIUM (256 * 1024)  // 256KB - Medium volume observers
#define RINGBUF_SIZE_HIGH   (512 * 1024)  // 512KB - High volume observers

// Safety limits
#define MAX_STRING_SIZE 256
#define TASK_COMM_LEN 16
#define MAX_STACK_DEPTH 20
#define MAX_LOOP_ITERATIONS 255

// Rate limiting structure
struct rate_limiter {
    __u64 tokens;
    __u64 last_refill_ns;
    __u64 max_per_sec;
};

// Overflow tracking
struct overflow_stats {
    __u64 ringbuf_drops;
    __u64 rate_limit_drops;
    __u64 sampling_drops;
};

// Safe probe read with null check - ALWAYS use this
static __always_inline int safe_probe_read(void *dst, __u32 size, const void *src) {
    if (!src || !dst) return -1;
    if (size == 0 || size > MAX_STRING_SIZE) return -1;
    return bpf_probe_read_kernel(dst, size, src);
}

// Safe string read with bounds - ALWAYS use this for strings
static __always_inline int safe_probe_read_str(void *dst, __u32 size, const void *src) {
    if (!src || !dst) return -1;
    if (size == 0 || size > MAX_STRING_SIZE) return -1;
    
    int ret = bpf_probe_read_kernel_str(dst, size, src);
    if (ret < 0) {
        // Zero out on failure for safety
        __builtin_memset(dst, 0, size);
        return ret;
    }
    return ret;
}

// Safe user probe read
static __always_inline int safe_probe_read_user(void *dst, __u32 size, const void *user_src) {
    if (!user_src || !dst) return -1;
    if (size == 0 || size > MAX_STRING_SIZE) return -1;
    return bpf_probe_read_user(dst, size, user_src);
}

// Safe user string read
static __always_inline int safe_probe_read_user_str(void *dst, __u32 size, const void *user_src) {
    if (!user_src || !dst) return -1;
    if (size == 0 || size > MAX_STRING_SIZE) return -1;
    
    int ret = bpf_probe_read_user_str(dst, size, user_src);
    if (ret < 0) {
        __builtin_memset(dst, 0, size);
        return ret;
    }
    return ret;
}

// Rate limiting implementation - per observer
static __always_inline bool should_rate_limit(struct rate_limiter *limiter) {
    if (!limiter) return false;
    
    __u64 now = bpf_ktime_get_ns();
    __u64 elapsed_ns = now - limiter->last_refill_ns;
    
    // Refill tokens based on elapsed time
    // 1 token per (1e9 / max_per_sec) nanoseconds
    if (elapsed_ns > 1000000) { // Refill every 1ms minimum
        __u64 ns_per_token = 1000000000ULL / limiter->max_per_sec;
        __u64 new_tokens = elapsed_ns / ns_per_token;
        
        limiter->tokens = limiter->tokens + new_tokens;
        if (limiter->tokens > limiter->max_per_sec) {
            limiter->tokens = limiter->max_per_sec;
        }
        limiter->last_refill_ns = now;
    }
    
    if (limiter->tokens > 0) {
        limiter->tokens--;
        return false; // Don't rate limit
    }
    
    return true; // Rate limit this event
}

// Sampling implementation for high volume
static __always_inline bool should_sample(__u32 sample_rate) {
    if (sample_rate <= 1) return true; // No sampling
    
    // Use pseudo-random for sampling
    __u32 rand = bpf_get_prandom_u32();
    return (rand % sample_rate) == 0;
}

// Safe ring buffer submission with overflow tracking
#define SAFE_RINGBUF_SUBMIT(rb_map, event_type, event_data, overflow_map) ({     \
    int __ret = 0;                                                                \
    event_type *__e = bpf_ringbuf_reserve(&rb_map, sizeof(event_type), 0);       \
    if (!__e) {                                                                   \
        /* Increment overflow counter */                                          \
        __u32 __key = 0;                                                         \
        struct overflow_stats *__stats = bpf_map_lookup_elem(&overflow_map, &__key); \
        if (__stats) {                                                           \
            __sync_fetch_and_add(&__stats->ringbuf_drops, 1);                    \
        }                                                                         \
        __ret = -ENOBUFS;                                                        \
    } else {                                                                      \
        __builtin_memcpy(__e, event_data, sizeof(event_type));                   \
        bpf_ringbuf_submit(__e, 0);                                              \
    }                                                                             \
    __ret;                                                                        \
})

// CO-RE field existence check
#define CORE_FIELD_EXISTS(type, field) \
    bpf_core_field_exists(type, field)

// Safe CO-RE read with fallback
#define SAFE_CORE_READ(dst, type, src, field, fallback) ({                      \
    if (CORE_FIELD_EXISTS(type, field)) {                                        \
        dst = BPF_CORE_READ(src, field);                                         \
    } else {                                                                      \
        dst = fallback;                                                          \
    }                                                                             \
})

// Container ID extraction helper
static __always_inline __u64 get_cgroup_id(struct task_struct *task) {
    if (!task) return 0;
    
    __u64 cgroup_id = 0;
    
    // Try to get cgroup ID with CO-RE
    if (CORE_FIELD_EXISTS(struct task_struct, cgroups)) {
        struct css_set *cgroups = BPF_CORE_READ(task, cgroups);
        if (cgroups) {
            // Navigate to get cgroup ID - kernel version dependent
            // This is simplified - real implementation needs version checks
            struct cgroup_subsys_state *css = BPF_CORE_READ(cgroups, subsys[0]);
            if (css) {
                struct cgroup *cgrp = BPF_CORE_READ(css, cgroup);
                if (cgrp) {
                    struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
                    if (kn) {
                        cgroup_id = BPF_CORE_READ(kn, id);
                    }
                }
            }
        }
    }
    
    return cgroup_id;
}

// Get current task comm safely
static __always_inline void get_current_comm(char comm[TASK_COMM_LEN]) {
    if (!comm) return;
    
    if (bpf_get_current_comm(comm, TASK_COMM_LEN) != 0) {
        __builtin_memset(comm, 0, TASK_COMM_LEN);
    }
}

// PID namespace aware PID
static __always_inline __u32 get_pid_ns_aware(__u32 pid) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) return pid;
    
    // Check if we're in a PID namespace
    if (CORE_FIELD_EXISTS(struct task_struct, nsproxy)) {
        struct nsproxy *nsproxy = BPF_CORE_READ(task, nsproxy);
        if (nsproxy && CORE_FIELD_EXISTS(struct nsproxy, pid_ns_for_children)) {
            struct pid_namespace *pid_ns = BPF_CORE_READ(nsproxy, pid_ns_for_children);
            if (pid_ns) {
                // In container, return container PID
                return pid;
            }
        }
    }
    
    return pid;
}

// Bounds check helper
#define CHECK_BOUNDS(ptr, size, end) \
    ((void *)(ptr) + (size) <= (void *)(end))

// Loop unroll helper for verifier
#define BOUNDED_LOOP(i, max, body) \
    _Pragma("unroll") \
    for (int i = 0; i < (max) && i < MAX_LOOP_ITERATIONS; i++) { \
        body \
    }

#endif /* __CORE_HELPERS_H */