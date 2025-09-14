// SPDX-License-Identifier: GPL-2.0
// Memory Observer with CO-RE Support (Simplified) - Per CLAUDE.md standards

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "../../bpf_common/core_helpers.h"

// Memory allocation thresholds
#define MIN_ALLOCATION_SIZE 10240  // 10KB minimum
#define MAX_TRACKED_ALLOCS  10000  // Reduced from 1M per migration plan
#define MEMORY_SAMPLE_RATE  5      // Sample 1 in 5 allocations
#define MEMORY_MAX_EVENTS_PER_SEC 500  // Rate limit per migration plan

// Event types
#define EVENT_MMAP          1
#define EVENT_MUNMAP        2
#define EVENT_RSS_GROWTH    3
#define EVENT_UNFREED       4

// Memory event for userspace
struct memory_event {
    u64 timestamp;
    u32 event_type;
    u32 pid;
    u32 tid;
    u32 uid;
    u32 gid;
    u64 address;
    u64 size;
    u64 cgroup_id;
    char comm[TASK_COMM_LEN];
    u8 pad[4];
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RINGBUF_SIZE_MEDIUM);  // 256KB
} memory_events SEC(".maps");

// Rate limiter
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct rate_limiter);
} memory_rate_limit SEC(".maps");

// Overflow statistics
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct overflow_stats);
} memory_overflow SEC(".maps");

// Initialize rate limiter
static __always_inline void init_memory_rate_limiter(void) {
    u32 key = 0;
    struct rate_limiter *limiter = bpf_map_lookup_elem(&memory_rate_limit, &key);
    if (limiter && limiter->max_per_sec == 0) {
        limiter->max_per_sec = MEMORY_MAX_EVENTS_PER_SEC;
        limiter->tokens = MEMORY_MAX_EVENTS_PER_SEC;
        limiter->last_refill_ns = bpf_ktime_get_ns();
    }
}

// Check rate limit
static __always_inline bool check_memory_rate_limit(void) {
    u32 key = 0;
    struct rate_limiter *limiter = bpf_map_lookup_elem(&memory_rate_limit, &key);
    if (!limiter) return false;
    
    bool limited = should_rate_limit(limiter);
    
    if (limited) {
        struct overflow_stats *stats = bpf_map_lookup_elem(&memory_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->rate_limit_drops, 1);
        }
    }
    
    return limited;
}

// Submit memory event with overflow tracking
static __always_inline int submit_memory_event(struct memory_event *event) {
    if (!event) return -1;
    
    struct memory_event *e = bpf_ringbuf_reserve(&memory_events, sizeof(*e), 0);
    if (!e) {
        u32 key = 0;
        struct overflow_stats *stats = bpf_map_lookup_elem(&memory_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->ringbuf_drops, 1);
        }
        return -1;
    }
    
    __builtin_memcpy(e, event, sizeof(*event));
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Simplified RSS tracking via tracepoint
SEC("tracepoint/mm/rss_stat")
int trace_rss_change(void *ctx) {
    init_memory_rate_limiter();
    
    // Rate limiting
    if (check_memory_rate_limit()) return 0;
    
    // Sampling
    if (should_sample(MEMORY_SAMPLE_RATE)) return 0;
    
    // Create event
    struct memory_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.event_type = EVENT_RSS_GROWTH;
    
    // Get current task info
    u64 pid_tgid = bpf_get_current_pid_tgid();
    event.pid = pid_tgid >> 32;
    event.tid = pid_tgid & 0xFFFFFFFF;
    event.uid = bpf_get_current_uid_gid() >> 32;
    event.gid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    
    get_current_comm(event.comm);
    
    // Submit event
    submit_memory_event(&event);
    
    return 0;
}

// Simplified periodic scanner (called from userspace)
SEC("perf_event")
int scan_memory_periodic(void *ctx) {
    init_memory_rate_limiter();
    
    // Create unfreed allocation event
    struct memory_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.event_type = EVENT_UNFREED;
    
    // Get current task info
    u64 pid_tgid = bpf_get_current_pid_tgid();
    event.pid = pid_tgid >> 32;
    event.tid = pid_tgid & 0xFFFFFFFF;
    event.uid = bpf_get_current_uid_gid() >> 32;
    event.gid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    
    get_current_comm(event.comm);
    
    // Submit event (this would be enhanced with actual leak detection logic)
    submit_memory_event(&event);
    
    return 0;
}

char _license[] SEC("license") = "GPL";