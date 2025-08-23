// SPDX-License-Identifier: GPL-2.0
/* Premium Storage I/O Monitor eBPF Program
 * Enterprise-grade monitoring for Kubernetes storage performance
 * Designed for paying customers who need deep insights and root cause analysis
 */

/* Basic type definitions for eBPF */
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

/* Boolean type */
typedef _Bool bool;
#define true 1
#define false 0

/* Network byte order types */
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u32 __wsum;

/* Size types */
typedef __u64 size_t;
typedef __s64 loff_t;
typedef __u32 dev_t;
typedef __s64 ssize_t;

/* BPF map types */
#define BPF_MAP_TYPE_HASH 1
#define BPF_MAP_TYPE_ARRAY 2
#define BPF_MAP_TYPE_RINGBUF 27
#define BPF_MAP_TYPE_LRU_HASH 9
#define BPF_MAP_TYPE_PERCPU_ARRAY 6

/* BPF update flags */
#define BPF_ANY 0
#define BPF_NOEXIST 1
#define BPF_EXIST 2

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

char LICENSE[] SEC("license") = "GPL";

/* Configuration constants */
#define MAX_PATH_LEN 256
#define MAX_COMM_LEN 16
#define MAX_ACTIVE_EVENTS 10240
#define MAX_PATTERNS 4096
#define MAX_CONTAINER_ID_LEN 64

/* Event types - Premium feature set */
#define IO_TYPE_READ 1
#define IO_TYPE_WRITE 2
#define IO_TYPE_FSYNC 3
#define IO_TYPE_OPEN 4
#define IO_TYPE_CLOSE 5
#define IO_TYPE_STAT 6
#define IO_TYPE_MMAP 7

/* I/O patterns - Advanced pattern recognition */
#define PATTERN_UNKNOWN 0
#define PATTERN_SEQUENTIAL 1
#define PATTERN_RANDOM 2
#define PATTERN_STRIDED 3
#define PATTERN_BURST 4
#define PATTERN_CACHE_FRIENDLY 5

/* Premium event structure with rich telemetry */
struct premium_storage_event {
    /* Core identifiers */
    __u8 event_type;
    __u32 pid;
    __u32 tid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    
    /* Premium timing metrics */
    __u64 start_time_ns;
    __u64 end_time_ns;
    __u64 queue_enter_ns;
    __u64 syscall_enter_ns;
    __u64 vfs_enter_ns;
    __u64 block_enter_ns;
    
    /* File operation intelligence */
    __u64 inode;
    __s64 size;
    __s64 offset;
    __s64 prev_offset;      // For pattern detection
    __u32 flags;
    __u32 mode;
    
    /* Advanced pattern analytics */
    __u8 io_pattern;
    __u8 confidence_score;   // 0-100
    __u16 burst_count;       // Operations in burst
    __u32 stride_size;       // For strided patterns
    __u32 cache_hits;        // Page cache hits
    __u32 cache_misses;      // Page cache misses
    
    /* Container and Kubernetes context */
    char container_id[MAX_CONTAINER_ID_LEN];
    char namespace[32];
    char pod_name[64];
    
    /* Path and process info */
    char path[MAX_PATH_LEN];
    char comm[MAX_COMM_LEN];
    
    /* Device and filesystem details */
    __u32 dev_major;
    __u32 dev_minor;
    char fs_type[16];        // ext4, xfs, nfs, etc.
    
    /* Error tracking */
    __s32 error_code;
    __u32 retry_count;       // Number of retries
    
    /* Performance indicators */
    __u16 cpu_id;            // CPU where operation ran
    __u32 queue_depth;       // Approximated queue depth
    __u64 memory_pressure;   // Memory pressure indicator
} __attribute__((packed));

/* Active operation tracking */
struct active_operation {
    __u64 start_time;
    __u64 queue_time;
    __u64 syscall_time;
    __u64 vfs_time;
    __u64 inode;
    __s64 size;
    __s64 offset;
    __s64 prev_offset;
    __u32 flags;
    __u32 mode;
    __u32 fd;
    __u16 cpu_id;
    char path[MAX_PATH_LEN];
};

/* Pattern tracking with machine learning hints */
struct pattern_tracker {
    __s64 last_offsets[8];   // Last 8 access offsets
    __u32 access_times[8];    // Timestamps of accesses
    __u32 sequential_score;
    __u32 random_score;
    __u32 strided_score;
    __u32 burst_score;
    __s64 detected_stride;
    __u64 last_update;
    __u32 total_ops;
    __u64 total_bytes;
};

/* Per-CPU performance metrics */
struct cpu_metrics {
    __u64 read_ops;
    __u64 write_ops;
    __u64 read_bytes;
    __u64 write_bytes;
    __u64 slow_io_count;
    __u64 total_latency_ns;
    __u64 max_latency_ns;
    __u64 cache_hits;
    __u64 cache_misses;
};

/* Container metadata cache */
struct container_meta {
    char container_id[MAX_CONTAINER_ID_LEN];
    char namespace[32];
    char pod_name[64];
    __u64 last_seen;
};

/* Maps - Premium configuration */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 * 1024 * 1024);  // 8MB for premium events
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ACTIVE_EVENTS);
    __type(key, __u64);  // pid_tgid
    __type(value, struct active_operation);
} active_ops SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_PATTERNS);
    __type(key, __u64);  // inode
    __type(value, struct pattern_tracker);
} pattern_cache SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct cpu_metrics);
} cpu_stats SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 1024);
    __type(key, __u64);  // cgroup_id
    __type(value, struct container_meta);
} container_cache SEC(".maps");

/* Use percpu array for queue depth to avoid atomics */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);  // Per-CPU queue depth
} queue_depth SEC(".maps");

/* Helper functions */
static __always_inline __u64 get_pid_tgid(void)
{
    return bpf_get_current_pid_tgid();
}

static __always_inline __u32 get_pid(void)
{
    return bpf_get_current_pid_tgid() >> 32;
}

static __always_inline __u32 get_tid(void)
{
    return bpf_get_current_pid_tgid() & 0xFFFFFFFF;
}

/* Safe queue depth update without atomics */
static __always_inline void adjust_queue_depth(int delta)
{
    __u32 key = 0;
    __u32 *depth = bpf_map_lookup_elem(&queue_depth, &key);
    if (depth) {
        if (delta > 0) {
            *depth = *depth + 1;
        } else if (*depth > 0) {
            *depth = *depth - 1;
        }
    }
}

static __always_inline __u32 get_current_queue_depth(void)
{
    __u32 key = 0;
    __u32 *depth = bpf_map_lookup_elem(&queue_depth, &key);
    return depth ? *depth : 0;
}

/* Advanced pattern detection algorithm */
static __always_inline void detect_pattern(struct pattern_tracker *tracker, 
                                          __s64 offset, __s64 size,
                                          __u8 *pattern, __u8 *confidence)
{
    if (!tracker) {
        *pattern = PATTERN_UNKNOWN;
        *confidence = 0;
        return;
    }
    
    // Shift history
    for (int i = 7; i > 0; i--) {
        tracker->last_offsets[i] = tracker->last_offsets[i-1];
        tracker->access_times[i] = tracker->access_times[i-1];
    }
    tracker->last_offsets[0] = offset;
    tracker->access_times[0] = bpf_ktime_get_ns() / 1000000;  // ms
    
    // Analyze pattern
    __u32 sequential = 0, random = 0, strided = 0, burst = 0;
    __s64 potential_stride = 0;
    
    // Check for sequential
    if (tracker->last_offsets[1] != 0) {
        __s64 delta = offset - tracker->last_offsets[1];
        if (delta >= 0 && delta <= size + 4096) {
            sequential++;
        }
    }
    
    // Check for burst (multiple ops within 10ms)
    if (tracker->access_times[1] != 0) {
        __u32 time_delta = tracker->access_times[0] - tracker->access_times[1];
        if (time_delta < 10) {
            burst++;
        }
    }
    
    // Check for strided pattern
    bool consistent_stride = true;
    if (tracker->last_offsets[2] != 0) {
        potential_stride = tracker->last_offsets[0] - tracker->last_offsets[1];
        __s64 prev_stride = tracker->last_offsets[1] - tracker->last_offsets[2];
        if (potential_stride != prev_stride) {
            consistent_stride = false;
        }
    }
    
    if (consistent_stride && potential_stride > 4096) {
        strided++;
        tracker->detected_stride = potential_stride;
    }
    
    // Update scores
    tracker->sequential_score = (tracker->sequential_score * 7 + sequential * 10) / 8;
    tracker->random_score = (tracker->random_score * 7 + (!sequential && !strided) * 10) / 8;
    tracker->strided_score = (tracker->strided_score * 7 + strided * 10) / 8;
    tracker->burst_score = (tracker->burst_score * 7 + burst * 10) / 8;
    
    // Determine pattern
    __u32 max_score = tracker->sequential_score;
    *pattern = PATTERN_SEQUENTIAL;
    
    if (tracker->burst_score > max_score) {
        max_score = tracker->burst_score;
        *pattern = PATTERN_BURST;
    }
    if (tracker->strided_score > max_score) {
        max_score = tracker->strided_score;
        *pattern = PATTERN_STRIDED;
    }
    if (tracker->random_score > max_score) {
        max_score = tracker->random_score;
        *pattern = PATTERN_RANDOM;
    }
    
    // Calculate confidence
    __u32 total = tracker->sequential_score + tracker->random_score + 
                  tracker->strided_score + tracker->burst_score;
    if (total > 0) {
        *confidence = (max_score * 100) / total;
    } else {
        *confidence = 0;
    }
    
    tracker->total_ops++;
    tracker->total_bytes += size;
    tracker->last_update = bpf_ktime_get_ns();
}

/* Extract container metadata from cgroup */
static __always_inline void get_container_metadata(struct premium_storage_event *event)
{
    struct container_meta *meta = bpf_map_lookup_elem(&container_cache, &event->cgroup_id);
    if (meta) {
        // Use cached metadata
        __builtin_memcpy(event->container_id, meta->container_id, MAX_CONTAINER_ID_LEN);
        __builtin_memcpy(event->namespace, meta->namespace, 32);
        __builtin_memcpy(event->pod_name, meta->pod_name, 64);
    } else {
        // Generate simple container ID from cgroup
        event->container_id[0] = 'c';
        event->container_id[1] = 'g';
        event->container_id[2] = ':';
        for (int i = 0; i < 12; i++) {
            __u8 nibble = (event->cgroup_id >> (48 - i * 4)) & 0xF;
            if (nibble < 10) {
                event->container_id[3 + i] = '0' + nibble;
            } else {
                event->container_id[3 + i] = 'a' + (nibble - 10);
            }
        }
        event->container_id[15] = '\0';
    }
}

/* Update CPU metrics */
static __always_inline void update_cpu_metrics(__u8 event_type, __s64 size, __u64 latency, bool slow)
{
    __u32 key = 0;
    struct cpu_metrics *metrics = bpf_map_lookup_elem(&cpu_stats, &key);
    if (!metrics) return;
    
    switch (event_type) {
    case IO_TYPE_READ:
        metrics->read_ops++;
        if (size > 0) metrics->read_bytes += size;
        break;
    case IO_TYPE_WRITE:
        metrics->write_ops++;
        if (size > 0) metrics->write_bytes += size;
        break;
    }
    
    metrics->total_latency_ns += latency;
    if (latency > metrics->max_latency_ns) {
        metrics->max_latency_ns = latency;
    }
    
    if (slow) {
        metrics->slow_io_count++;
    }
}

/* Premium read tracking */
SEC("tracepoint/syscalls/sys_enter_read")
int premium_trace_read_enter(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation op = {};
    
    op.start_time = bpf_ktime_get_ns();
    op.queue_time = op.start_time;
    op.syscall_time = op.start_time;
    op.cpu_id = bpf_get_smp_processor_id();
    
    adjust_queue_depth(1);
    
    bpf_map_update_elem(&active_ops, &pid_tgid, &op, BPF_ANY);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int premium_trace_read_exit(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation *op = bpf_map_lookup_elem(&active_ops, &pid_tgid);
    if (!op) return 0;
    
    adjust_queue_depth(-1);
    
    struct premium_storage_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_ops, &pid_tgid);
        return 0;
    }
    
    // Fill premium event data
    event->event_type = IO_TYPE_READ;
    event->pid = get_pid();
    event->tid = get_tid();
    event->ppid = 0;  // Would need task_struct
    
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    // Premium timing
    event->start_time_ns = op->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    event->queue_enter_ns = op->queue_time;
    event->syscall_enter_ns = op->syscall_time;
    event->vfs_enter_ns = op->vfs_time;
    
    // File details
    event->inode = op->inode;
    event->size = op->size;
    event->offset = op->offset;
    event->prev_offset = op->prev_offset;
    event->flags = op->flags;
    event->mode = op->mode;
    
    // Pattern detection
    struct pattern_tracker *tracker = bpf_map_lookup_elem(&pattern_cache, &op->inode);
    if (!tracker) {
        struct pattern_tracker new_tracker = {};
        bpf_map_update_elem(&pattern_cache, &op->inode, &new_tracker, BPF_ANY);
        tracker = bpf_map_lookup_elem(&pattern_cache, &op->inode);
    }
    
    if (tracker) {
        detect_pattern(tracker, op->offset, op->size, 
                      &event->io_pattern, &event->confidence_score);
        event->stride_size = tracker->detected_stride;
    }
    
    event->queue_depth = get_current_queue_depth();
    event->cpu_id = op->cpu_id;
    
    // Get process info
    bpf_get_current_comm(event->comm, sizeof(event->comm));
    
    // Copy path
    __builtin_memcpy(event->path, op->path, MAX_PATH_LEN);
    
    // Get container metadata
    get_container_metadata(event);
    
    // Update metrics
    __u64 latency = event->end_time_ns - event->start_time_ns;
    bool is_slow = latency > 10000000;  // 10ms
    update_cpu_metrics(IO_TYPE_READ, op->size, latency, is_slow);
    
    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_ops, &pid_tgid);
    
    return 0;
}

/* Premium write tracking */
SEC("tracepoint/syscalls/sys_enter_write")
int premium_trace_write_enter(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation op = {};
    
    op.start_time = bpf_ktime_get_ns();
    op.queue_time = op.start_time;
    op.syscall_time = op.start_time;
    op.cpu_id = bpf_get_smp_processor_id();
    
    adjust_queue_depth(1);
    
    bpf_map_update_elem(&active_ops, &pid_tgid, &op, BPF_ANY);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int premium_trace_write_exit(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation *op = bpf_map_lookup_elem(&active_ops, &pid_tgid);
    if (!op) return 0;
    
    adjust_queue_depth(-1);
    
    struct premium_storage_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_ops, &pid_tgid);
        return 0;
    }
    
    // Fill premium event data
    event->event_type = IO_TYPE_WRITE;
    event->pid = get_pid();
    event->tid = get_tid();
    
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    // Timing
    event->start_time_ns = op->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    event->queue_enter_ns = op->queue_time;
    event->syscall_enter_ns = op->syscall_time;
    
    // Pattern detection for writes
    struct pattern_tracker *tracker = bpf_map_lookup_elem(&pattern_cache, &op->inode);
    if (!tracker) {
        struct pattern_tracker new_tracker = {};
        bpf_map_update_elem(&pattern_cache, &op->inode, &new_tracker, BPF_ANY);
        tracker = bpf_map_lookup_elem(&pattern_cache, &op->inode);
    }
    
    if (tracker) {
        detect_pattern(tracker, op->offset, op->size,
                      &event->io_pattern, &event->confidence_score);
    }
    
    event->queue_depth = get_current_queue_depth();
    event->cpu_id = op->cpu_id;
    
    bpf_get_current_comm(event->comm, sizeof(event->comm));
    get_container_metadata(event);
    
    // Update metrics
    __u64 latency = event->end_time_ns - event->start_time_ns;
    bool is_slow = latency > 10000000;  // 10ms
    update_cpu_metrics(IO_TYPE_WRITE, op->size, latency, is_slow);
    
    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_ops, &pid_tgid);
    
    return 0;
}

/* Premium fsync tracking for data durability */
SEC("tracepoint/syscalls/sys_enter_fsync")
int premium_trace_fsync_enter(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation op = {};
    
    op.start_time = bpf_ktime_get_ns();
    op.cpu_id = bpf_get_smp_processor_id();
    
    adjust_queue_depth(1);
    bpf_map_update_elem(&active_ops, &pid_tgid, &op, BPF_ANY);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_fsync")
int premium_trace_fsync_exit(void *ctx)
{
    __u64 pid_tgid = get_pid_tgid();
    struct active_operation *op = bpf_map_lookup_elem(&active_ops, &pid_tgid);
    if (!op) return 0;
    
    adjust_queue_depth(-1);
    
    struct premium_storage_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_ops, &pid_tgid);
        return 0;
    }
    
    event->event_type = IO_TYPE_FSYNC;
    event->pid = get_pid();
    event->tid = get_tid();
    
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    event->cgroup_id = bpf_get_current_cgroup_id();
    event->start_time_ns = op->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    event->cpu_id = op->cpu_id;
    event->queue_depth = get_current_queue_depth();
    
    bpf_get_current_comm(event->comm, sizeof(event->comm));
    get_container_metadata(event);
    
    // Fsync is critical for durability - always mark slow if > 50ms
    __u64 latency = event->end_time_ns - event->start_time_ns;
    bool is_slow = latency > 50000000;
    update_cpu_metrics(IO_TYPE_FSYNC, 0, latency, is_slow);
    
    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_ops, &pid_tgid);
    
    return 0;
}