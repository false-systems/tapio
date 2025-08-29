// SPDX-License-Identifier: GPL-2.0
// Memory leak detection - Track large allocations and RSS growth

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Focus on REALISTIC detection - large allocations only
#define MIN_ALLOCATION_SIZE 10240  // 10KB - ignore small allocations
#define MAX_TRACKED_ALLOCS  10000   // Limit kernel memory usage

// Event types
#define EVENT_MMAP          1  // Large allocation via mmap
#define EVENT_MUNMAP        2  // Memory freed
#define EVENT_RSS_GROWTH    3  // RSS increase detected
#define EVENT_UNFREED       4  // Long-lived allocation

// Allocation tracking
struct allocation_info {
    __u64 size;
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u64 cgroup_id;
    char comm[16];
    // Simple stack trace - just return address
    __u64 caller_ip;
};

// Memory event for userspace
struct memory_event {
    __u64 timestamp;
    __u32 event_type;
    __u32 pid;
    __u64 address;
    __u64 size;
    __u64 cgroup_id;
    char comm[16];
    __u64 caller_ip;
    // RSS tracking
    __u64 rss_pages;
    __u64 rss_growth;
} __attribute__((packed));

// Track active allocations - LRU map auto-evicts old entries
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_TRACKED_ALLOCS);
    __type(key, __u64);   // Memory address
    __type(value, struct allocation_info);
} active_allocations SEC(".maps");

// Track RSS per process
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);   // PID
    __type(value, __u64); // Last RSS value
} process_rss SEC(".maps");

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024); // 1MB buffer
} events SEC(".maps");

// Simple sampling counter
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} sample_counter SEC(".maps");

// Helper: Should we sample this allocation?
static __always_inline bool should_sample(__u64 size)
{
    // Always track very large allocations
    if (size >= 1048576) { // >= 1MB
        return true;
    }
    
    // Sample smaller allocations
    if (size >= MIN_ALLOCATION_SIZE) {
        __u32 key = 0;
        __u64 *counter = bpf_map_lookup_elem(&sample_counter, &key);
        if (counter) {
            __u64 count = *counter;
            *counter = count + 1;
            // Sample 1 in 10 for 10KB-1MB allocations
            return (count % 10) == 0;
        }
    }
    
    return false;
}

// Track mmap (large allocations)
SEC("uprobe/mmap")
int trace_mmap_entry(struct pt_regs *ctx)
{
    __u64 size = PT_REGS_PARM2(ctx);
    
    // Filter small allocations
    if (size < MIN_ALLOCATION_SIZE) {
        return 0;
    }
    
    // Sampling logic
    if (!should_sample(size)) {
        return 0;
    }
    
    // Store pending allocation info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct allocation_info info = {};
    info.size = size;
    info.timestamp = bpf_ktime_get_ns();
    info.pid = pid;
    info.tid = pid_tgid & 0xFFFFFFFF;
    info.cgroup_id = bpf_get_current_cgroup_id();
    info.caller_ip = PT_REGS_IP(ctx);
    
    bpf_get_current_comm(&info.comm, sizeof(info.comm));
    
    // We'll get the actual address in return probe
    // For now, use PID+timestamp as temporary key
    __u64 temp_key = ((__u64)pid << 32) | (info.timestamp & 0xFFFFFFFF);
    bpf_map_update_elem(&active_allocations, &temp_key, &info, BPF_ANY);
    
    return 0;
}

// Track mmap return (get allocated address)
SEC("uretprobe/mmap")
int trace_mmap_return(struct pt_regs *ctx)
{
    void *addr = (void *)PT_REGS_RC(ctx);
    
    // Check for allocation failure
    if ((long)addr < 0) {
        return 0;
    }
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Find our pending allocation
    __u64 temp_key = ((__u64)pid << 32) | (bpf_ktime_get_ns() & 0xFFFFFFFF);
    struct allocation_info *info = bpf_map_lookup_elem(&active_allocations, &temp_key);
    if (!info) {
        return 0;
    }
    
    // Move to real address
    __u64 real_addr = (__u64)addr;
    bpf_map_update_elem(&active_allocations, &real_addr, info, BPF_ANY);
    bpf_map_delete_elem(&active_allocations, &temp_key);
    
    // Emit allocation event
    struct memory_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = bpf_ktime_get_ns();
    e->event_type = EVENT_MMAP;
    e->pid = pid;
    e->address = real_addr;
    e->size = info->size;
    e->cgroup_id = info->cgroup_id;
    e->caller_ip = info->caller_ip;
    __builtin_memcpy(e->comm, info->comm, sizeof(e->comm));
    
    bpf_ringbuf_submit(e, 0);
    
    return 0;
}

// Track munmap (memory free)
SEC("uprobe/munmap")
int trace_munmap(struct pt_regs *ctx)
{
    void *addr = (void *)PT_REGS_PARM1(ctx);
    __u64 address = (__u64)addr;
    
    // Check if we were tracking this allocation
    struct allocation_info *info = bpf_map_lookup_elem(&active_allocations, &address);
    if (!info) {
        return 0;  // Not tracking this one
    }
    
    // Emit free event
    struct memory_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (e) {
        e->timestamp = bpf_ktime_get_ns();
        e->event_type = EVENT_MUNMAP;
        e->pid = bpf_get_current_pid_tgid() >> 32;
        e->address = address;
        e->size = info->size;
        e->cgroup_id = info->cgroup_id;
        bpf_get_current_comm(&e->comm, sizeof(e->comm));
        
        bpf_ringbuf_submit(e, 0);
    }
    
    // Remove from tracking
    bpf_map_delete_elem(&active_allocations, &address);
    
    return 0;
}

// Periodic check for long-lived allocations
SEC("perf_event")
int check_unfreed_allocations(struct bpf_perf_event_data *ctx)
{
    __u64 now = bpf_ktime_get_ns();
    __u64 age_threshold = 30 * 1000000000ULL; // 30 seconds in nanoseconds
    
    // Note: In real implementation, we'd iterate through map
    // For now, this is a placeholder showing the concept
    // Real iteration requires BPF_MAP_TYPE_HASH_OF_MAPS or similar
    
    return 0;
}

// Track RSS growth (page allocations)
SEC("tracepoint/mm/rss_stat")
int trace_rss_change(struct trace_event_raw_rss_stat *ctx)
{
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __s64 size = ctx->size;
    
    // Only track increases
    if (size <= 0) {
        return 0;
    }
    
    // Only track significant growth (> 1MB)
    if (size < 256) { // 256 pages = 1MB
        return 0;
    }
    
    // Update RSS tracking
    __u64 *last_rss = bpf_map_lookup_elem(&process_rss, &pid);
    __u64 current_rss = size;
    
    if (last_rss) {
        __u64 growth = current_rss - *last_rss;
        
        // Report significant growth
        if (growth > 256) { // 1MB growth
            struct memory_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
            if (e) {
                e->timestamp = bpf_ktime_get_ns();
                e->event_type = EVENT_RSS_GROWTH;
                e->pid = pid;
                e->rss_pages = current_rss;
                e->rss_growth = growth;
                e->cgroup_id = bpf_get_current_cgroup_id();
                bpf_get_current_comm(&e->comm, sizeof(e->comm));
                
                bpf_ringbuf_submit(e, 0);
            }
        }
    }
    
    bpf_map_update_elem(&process_rss, &pid, &current_rss, BPF_ANY);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";