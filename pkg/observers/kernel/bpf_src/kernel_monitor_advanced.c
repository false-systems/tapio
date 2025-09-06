// SPDX-License-Identifier: GPL-2.0
// Advanced kernel monitoring with per-CPU optimizations and zero-copy patterns
// Demonstrates production-grade eBPF development techniques

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Performance optimizations
#define ENABLE_PER_CPU_CACHE 1
#define USE_BATCH_PROCESSING 1
#define MAX_BATCH_SIZE 32

// Event type definitions with priority levels
#define EVENT_PRIORITY_HIGH   0x80000000
#define EVENT_PRIORITY_MEDIUM 0x40000000
#define EVENT_PRIORITY_LOW    0x20000000

// Sampling rates for different event types (1 in N events)
#define SAMPLE_RATE_MEMORY 100    // Sample 1 in 100 memory events
#define SAMPLE_RATE_NETWORK 10    // Sample 1 in 10 network events
#define SAMPLE_RATE_FILE 50       // Sample 1 in 50 file events

// Advanced event structure with compression
struct advanced_event {
    // Header (8 bytes)
    __u32 timestamp_delta;  // Delta from batch start (saves 4 bytes)
    __u16 event_type;       // Event type and flags
    __u8 cpu;              // CPU where event occurred
    __u8 batch_id;         // Batch identifier
    
    // Process info (12 bytes) 
    __u32 pid;
    __u32 tgid;
    __u32 cgroup_hash;     // Hash of cgroup path (saves space)
    
    // Variable data (up to 236 bytes)
    union {
        struct {
            __u32 saddr_v4;
            __u32 daddr_v4;
            __u16 sport;
            __u16 dport;
            __u8 proto;
            __u8 flags;
            __u16 _pad;
        } net_v4;
        
        struct {
            __u32 saddr_v6[4];
            __u32 daddr_v6[4];
            __u16 sport;
            __u16 dport;
            __u8 proto;
            __u8 flags;
            __u16 _pad;
        } net_v6;
        
        struct {
            __u64 size;
            __u64 latency_ns;
            __u32 numa_node;
            __u32 flags;
        } memory;
        
        __u8 raw[236];
    } data;
} __attribute__((packed));

// Per-CPU batch buffer for event aggregation
struct event_batch {
    __u64 batch_start_time;
    __u32 event_count;
    __u32 dropped_count;
    struct advanced_event events[MAX_BATCH_SIZE];
};

// Maps with optimized configurations
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB - production optimized
} advanced_events SEC(".maps");

// Per-CPU batch buffers for lock-free aggregation
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct event_batch);
} cpu_batches SEC(".maps");

// LRU hash for deduplication
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 100000);
    __type(key, __u64);  // Event hash
    __type(value, __u64); // Last seen timestamp
} event_dedup SEC(".maps");

// Per-CPU statistics for monitoring
struct perf_stats {
    __u64 events_processed;
    __u64 events_sampled;
    __u64 events_dropped;
    __u64 batches_sent;
    __u64 bytes_sent;
    __u64 cache_hits;
    __u64 cache_misses;
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct perf_stats);
} performance_stats SEC(".maps");

// Bloom filter for fast path filtering
struct {
    __uint(type, BPF_MAP_TYPE_BLOOM_FILTER);
    __uint(max_entries, 100000);
    __type(value, __u32); // PID to track
} pid_bloom_filter SEC(".maps");

// Fast hash function for deduplication
static __always_inline __u64 fast_hash(void *data, __u32 len)
{
    __u64 hash = 0xcbf29ce484222325ULL; // FNV-1a offset basis
    __u8 *bytes = data;
    
    #pragma unroll
    for (int i = 0; i < 32 && i < len; i++) {
        hash ^= bytes[i];
        hash *= 0x100000001b3ULL; // FNV-1a prime
    }
    
    return hash;
}

// Check if we should sample this event
static __always_inline bool should_sample(__u32 event_type, __u32 pid)
{
    // Use PID as seed for deterministic sampling
    __u32 sample_rate = SAMPLE_RATE_NETWORK;
    
    switch (event_type & 0xFF) {
        case 1: // Memory events
        case 2:
            sample_rate = SAMPLE_RATE_MEMORY;
            break;
        case 5: // Network events
        case 6:
        case 7:
            sample_rate = SAMPLE_RATE_NETWORK;
            break;
        case 8: // File events
        case 9:
        case 10:
            sample_rate = SAMPLE_RATE_FILE;
            break;
        default:
            sample_rate = 1; // Always sample unknown events
    }
    
    // Simple but effective sampling
    return (pid % sample_rate) == 0;
}

// Get or create batch buffer for current CPU
static __always_inline struct event_batch *get_cpu_batch(void)
{
    __u32 zero = 0;
    struct event_batch *batch = bpf_map_lookup_elem(&cpu_batches, &zero);
    
    if (!batch)
        return NULL;
    
    // Initialize if needed
    if (batch->event_count == 0) {
        batch->batch_start_time = bpf_ktime_get_ns();
    }
    
    return batch;
}

// Flush batch to ring buffer
static __always_inline void flush_batch(struct event_batch *batch)
{
    if (!batch || batch->event_count == 0)
        return;
    
    // Reserve space for entire batch
    __u32 batch_size = sizeof(__u64) + sizeof(__u32) + 
                      (batch->event_count * sizeof(struct advanced_event));
    
    void *rb_space = bpf_ringbuf_reserve(&advanced_events, batch_size, 0);
    if (!rb_space) {
        // Update dropped counter
        __u32 zero = 0;
        struct perf_stats *stats = bpf_map_lookup_elem(&performance_stats, &zero);
        if (stats) {
            __sync_fetch_and_add(&stats->events_dropped, batch->event_count);
        }
        goto cleanup;
    }
    
    // Write batch header
    *(__u64 *)rb_space = batch->batch_start_time;
    rb_space += sizeof(__u64);
    *(__u32 *)rb_space = batch->event_count;
    rb_space += sizeof(__u32);
    
    // Copy events (compiler will optimize this)
    #pragma unroll
    for (int i = 0; i < MAX_BATCH_SIZE && i < batch->event_count; i++) {
        __builtin_memcpy(rb_space, &batch->events[i], sizeof(struct advanced_event));
        rb_space += sizeof(struct advanced_event);
    }
    
    bpf_ringbuf_submit(rb_space - batch_size, 0);
    
    // Update statistics
    __u32 zero = 0;
    struct perf_stats *stats = bpf_map_lookup_elem(&performance_stats, &zero);
    if (stats) {
        __sync_fetch_and_add(&stats->batches_sent, 1);
        __sync_fetch_and_add(&stats->bytes_sent, batch_size);
    }

cleanup:
    // Reset batch
    batch->event_count = 0;
    batch->dropped_count = 0;
}

// Add event to batch with automatic flushing
static __always_inline int add_to_batch(struct advanced_event *event)
{
    struct event_batch *batch = get_cpu_batch();
    if (!batch)
        return -1;
    
    // Check if batch is full
    if (batch->event_count >= MAX_BATCH_SIZE) {
        flush_batch(batch);
        batch->batch_start_time = bpf_ktime_get_ns();
    }
    
    // Add event to batch
    if (batch->event_count < MAX_BATCH_SIZE) {
        __builtin_memcpy(&batch->events[batch->event_count], event, 
                        sizeof(struct advanced_event));
        batch->event_count++;
        return 0;
    }
    
    return -1;
}

// Optimized TCP connection tracking with batching
SEC("fentry/tcp_v4_connect")
int BPF_PROG(trace_tcp_connect_optimized, struct sock *sk)
{
    if (!sk)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Fast path: check bloom filter first
    if (bpf_map_peek_elem(&pid_bloom_filter, &pid) != 0) {
        // PID not in bloom filter, skip
        return 0;
    }
    
    // Sampling decision
    if (!should_sample(5, pid)) {
        return 0;
    }
    
    // Create compact event
    struct advanced_event event = {};
    __u64 now = bpf_ktime_get_ns();
    
    // Get batch for timestamp delta calculation
    struct event_batch *batch = get_cpu_batch();
    if (batch) {
        event.timestamp_delta = (__u32)(now - batch->batch_start_time);
    }
    
    event.event_type = 5 | EVENT_PRIORITY_MEDIUM;
    event.cpu = bpf_get_smp_processor_id();
    event.pid = pid;
    event.tgid = pid_tgid & 0xFFFFFFFF;
    
    // Extract network info using CO-RE
    __u16 family = 0;
    BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family);
    
    if (family == 2) { // AF_INET
        BPF_CORE_READ_INTO(&event.data.net_v4.sport, sk, __sk_common.skc_num);
        BPF_CORE_READ_INTO(&event.data.net_v4.dport, sk, __sk_common.skc_dport);
        BPF_CORE_READ_INTO(&event.data.net_v4.saddr_v4, sk, __sk_common.skc_rcv_saddr);
        BPF_CORE_READ_INTO(&event.data.net_v4.daddr_v4, sk, __sk_common.skc_daddr);
        event.data.net_v4.proto = 6; // TCP
        event.data.net_v4.dport = __builtin_bswap16(event.data.net_v4.dport);
    }
    
    // Deduplication check
    __u64 event_hash = fast_hash(&event.data, sizeof(event.data.net_v4));
    __u64 *last_seen = bpf_map_lookup_elem(&event_dedup, &event_hash);
    if (last_seen && (now - *last_seen) < 1000000000) { // 1 second dedup window
        // Duplicate event, skip
        __u32 zero = 0;
        struct perf_stats *stats = bpf_map_lookup_elem(&performance_stats, &zero);
        if (stats) {
            __sync_fetch_and_add(&stats->cache_hits, 1);
        }
        return 0;
    }
    
    // Update dedup cache
    bpf_map_update_elem(&event_dedup, &event_hash, &now, BPF_ANY);
    
    // Add to batch
    add_to_batch(&event);
    
    // Update stats
    __u32 zero = 0;
    struct perf_stats *stats = bpf_map_lookup_elem(&performance_stats, &zero);
    if (stats) {
        __sync_fetch_and_add(&stats->events_processed, 1);
        __sync_fetch_and_add(&stats->events_sampled, 1);
    }
    
    return 0;
}

// Memory allocation with NUMA awareness
SEC("fentry/kmem_cache_alloc_node")
int BPF_PROG(trace_memory_alloc_numa, struct kmem_cache *cachep, gfp_t flags, int node)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Fast path filtering
    if (bpf_map_peek_elem(&pid_bloom_filter, &pid) != 0) {
        return 0;
    }
    
    if (!should_sample(1, pid)) {
        return 0;
    }
    
    struct advanced_event event = {};
    __u64 now = bpf_ktime_get_ns();
    
    struct event_batch *batch = get_cpu_batch();
    if (batch) {
        event.timestamp_delta = (__u32)(now - batch->batch_start_time);
    }
    
    event.event_type = 1 | EVENT_PRIORITY_LOW;
    event.cpu = bpf_get_smp_processor_id();
    event.pid = pid;
    event.tgid = pid_tgid & 0xFFFFFFFF;
    
    // Memory specific data
    if (cachep) {
        BPF_CORE_READ_INTO(&event.data.memory.size, cachep, size);
    }
    event.data.memory.numa_node = node;
    event.data.memory.flags = flags;
    
    add_to_batch(&event);
    
    return 0;
}

// Periodic batch flusher (called from timer or other trigger)
SEC("perf_event")
int flush_all_batches(struct bpf_perf_event_data *ctx)
{
    __u32 zero = 0;
    struct event_batch *batch = bpf_map_lookup_elem(&cpu_batches, &zero);
    
    if (batch && batch->event_count > 0) {
        flush_batch(batch);
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";