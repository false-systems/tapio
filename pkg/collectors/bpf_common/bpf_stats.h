#ifndef __BPF_STATS_H__
#define __BPF_STATS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Statistics event types
#define STATS_EVENT_FILTERED 1
#define STATS_EVENT_SAMPLED  2
#define STATS_EVENT_ERROR    3

// Statistics structure for monitoring probe health
struct bpf_statistics {
    __u64 invocations;      // Total probe invocations
    __u64 events_sent;      // Events successfully sent to userspace
    __u64 events_dropped;   // Events dropped due to buffer full
    __u64 errors;           // Processing errors
    __u64 last_update_ns;   // Last update timestamp in nanoseconds
    __u64 bytes_processed;  // Total bytes processed
    __u64 filter_hits;      // Events that passed filters
    __u64 filter_misses;    // Events filtered out
};

// Per-CPU statistics map for each probe
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct bpf_statistics);
} probe_stats SEC(".maps");

// Macro to define per-probe statistics map
#define DEFINE_BPF_STATS_MAP(name, max_probes) \
struct { \
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY); \
    __uint(max_entries, max_probes); \
    __type(key, __u32); \
    __type(value, struct bpf_statistics); \
} name SEC(".maps")

// Macro to define global statistics map
#define DEFINE_GLOBAL_STATS_MAP(name) \
struct { \
    __uint(type, BPF_MAP_TYPE_ARRAY); \
    __uint(max_entries, 1); \
    __type(key, __u32); \
    __type(value, struct bpf_statistics); \
} name SEC(".maps")

// Macro to enter probe and update invocation count
#define BPF_STATS_ENTER(stats_map, probe_id) \
    do { \
        __u32 __key = probe_id; \
        struct bpf_statistics *__stats = bpf_map_lookup_elem(stats_map, &__key); \
        if (__stats) { \
            __sync_fetch_and_add(&__stats->invocations, 1); \
            __stats->last_update_ns = bpf_ktime_get_ns(); \
        } \
    } while(0)

// Macro to exit probe with error
#define BPF_STATS_EXIT_ERROR(stats_map, probe_id, error_type) \
    do { \
        __u32 __key = probe_id; \
        struct bpf_statistics *__stats = bpf_map_lookup_elem(stats_map, &__key); \
        if (__stats) { \
            if (error_type == STATS_EVENT_FILTERED) { \
                __sync_fetch_and_add(&__stats->filter_misses, 1); \
            } else if (error_type == STATS_EVENT_SAMPLED) { \
                __sync_fetch_and_add(&__stats->filter_misses, 1); \
            } else { \
                __sync_fetch_and_add(&__stats->errors, 1); \
            } \
            __stats->last_update_ns = bpf_ktime_get_ns(); \
        } \
    } while(0)

// Macro to exit probe successfully  
#define BPF_STATS_EXIT_SUCCESS(stats_map, probe_id, bytes) \
    do { \
        __u32 __key = probe_id; \
        struct bpf_statistics *__stats = bpf_map_lookup_elem(stats_map, &__key); \
        if (__stats) { \
            __sync_fetch_and_add(&__stats->events_sent, 1); \
            __sync_fetch_and_add(&__stats->bytes_processed, bytes); \
            __sync_fetch_and_add(&__stats->filter_hits, 1); \
            __stats->last_update_ns = bpf_ktime_get_ns(); \
        } \
    } while(0)

// Helper function to update statistics
static __always_inline void update_probe_stats_invocation()
{
    __u32 key = 0;
    struct bpf_statistics *stats = bpf_map_lookup_elem(&probe_stats, &key);
    if (stats) {
        __sync_fetch_and_add(&stats->invocations, 1);
        stats->last_update_ns = bpf_ktime_get_ns();
    }
}

static __always_inline void update_probe_stats_event_sent(__u64 bytes)
{
    __u32 key = 0;
    struct bpf_statistics *stats = bpf_map_lookup_elem(&probe_stats, &key);
    if (stats) {
        __sync_fetch_and_add(&stats->events_sent, 1);
        __sync_fetch_and_add(&stats->bytes_processed, bytes);
        stats->last_update_ns = bpf_ktime_get_ns();
    }
}

static __always_inline void update_probe_stats_event_dropped()
{
    __u32 key = 0;
    struct bpf_statistics *stats = bpf_map_lookup_elem(&probe_stats, &key);
    if (stats) {
        __sync_fetch_and_add(&stats->events_dropped, 1);
        stats->last_update_ns = bpf_ktime_get_ns();
    }
}

static __always_inline void update_probe_stats_error()
{
    __u32 key = 0;
    struct bpf_statistics *stats = bpf_map_lookup_elem(&probe_stats, &key);
    if (stats) {
        __sync_fetch_and_add(&stats->errors, 1);
        stats->last_update_ns = bpf_ktime_get_ns();
    }
}

static __always_inline void update_probe_stats_filter(__u8 passed)
{
    __u32 key = 0;
    struct bpf_statistics *stats = bpf_map_lookup_elem(&probe_stats, &key);
    if (stats) {
        if (passed) {
            __sync_fetch_and_add(&stats->filter_hits, 1);
        } else {
            __sync_fetch_and_add(&stats->filter_misses, 1);
        }
        stats->last_update_ns = bpf_ktime_get_ns();
    }
}

#endif // __BPF_STATS_H__