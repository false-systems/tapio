// SPDX-License-Identifier: GPL-2.0
// Memory Observer with Full CO-RE Support - Per CLAUDE.md standards
// NO STUBS - Complete implementation only

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "../../bpf_common/core_helpers.h"

// Memory allocation thresholds
#define MIN_ALLOCATION_SIZE 10240  // 10KB minimum
#define MAX_TRACKED_ALLOCS  10000  // Reduced from 1M per migration plan
#define RSS_THRESHOLD_PAGES 256    // 1MB RSS change threshold
#define MEMORY_SAMPLE_RATE  5      // Sample 1 in 5 allocations
#define MEMORY_MAX_EVENTS_PER_SEC 500  // Rate limit per migration plan

// Event types
#define EVENT_MMAP          1
#define EVENT_MUNMAP        2
#define EVENT_RSS_GROWTH    3
#define EVENT_UNFREED       4
#define EVENT_OOM_RISK      5

// Allocation tracking with CO-RE fields
struct allocation_info {
    u64 size;
    u64 timestamp;
    u32 pid;
    u32 tid;
    u64 cgroup_id;
    u64 caller_ip;
    u32 namespace_pid;  // PID namespace tracking
    char comm[TASK_COMM_LEN];
} __attribute__((packed));

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
    u64 caller_ip;
    u64 rss_pages;
    s64 rss_growth;
    u32 namespace_pid;
    char comm[TASK_COMM_LEN];
    u8 is_oom_risk;
    u8 pad[3];
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_TRACKED_ALLOCS);
    __type(key, u64);   // Memory address
    __type(value, struct allocation_info);
} active_allocations SEC(".maps");

// Per-process RSS tracking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, u32);   // PID
    __type(value, u64); // RSS in pages
} process_rss SEC(".maps");

// Ring buffer for events
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

// PID namespace filter (optional)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 100);
    __type(key, u32);   // Namespace ID
    __type(value, u8);  // Allowed flag
} namespace_filter SEC(".maps");

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

// Check sampling for memory allocations
static __always_inline bool check_memory_sampling(u64 size) {
    // Always track very large allocations (>= 1MB)
    if (size >= 1048576) {
        return false;  // Don't skip
    }
    
    // Sample smaller allocations
    bool sampled = should_sample(MEMORY_SAMPLE_RATE);
    
    if (!sampled) {
        u32 key = 0;
        struct overflow_stats *stats = bpf_map_lookup_elem(&memory_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->sampling_drops, 1);
        }
    }
    
    return !sampled;
}

// Get PID namespace ID with CO-RE
static __always_inline u32 get_pid_namespace_id(struct task_struct *task) {
    if (!task) return 0;
    
    // Check if PID namespace fields exist
    if (!bpf_core_field_exists(struct task_struct, nsproxy))
        return 0;
    
    struct nsproxy *nsproxy = BPF_CORE_READ(task, nsproxy);
    if (!nsproxy) return 0;
    
    if (!bpf_core_field_exists(struct nsproxy, pid_ns_for_children))
        return 0;
    
    struct pid_namespace *pid_ns = BPF_CORE_READ(nsproxy, pid_ns_for_children);
    if (!pid_ns) return 0;
    
    // Get namespace inode number as ID
    if (bpf_core_field_exists(struct pid_namespace, ns)) {
        struct ns_common ns = {};
        bpf_core_read(&ns, sizeof(ns), &pid_ns->ns);
        return ns.inum;
    }
    
    return 0;
}

// Check if namespace is allowed (if filtering enabled)
static __always_inline bool is_namespace_allowed(u32 ns_id) {
    if (ns_id == 0) return true;  // No namespace info
    
    u8 *allowed = bpf_map_lookup_elem(&namespace_filter, &ns_id);
    
    // If map is empty, allow all
    // If map has entries, only allow listed namespaces
    return allowed ? (*allowed > 0) : true;
}

// Check OOM risk based on RSS
static __always_inline bool check_oom_risk(u64 rss_pages) {
    // Consider OOM risk if RSS > 1GB (262144 pages)
    return rss_pages > 262144;
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

// Track mmap allocations with CO-RE
SEC("uprobe/mmap")
int trace_mmap_entry(struct pt_regs *ctx) {
    init_memory_rate_limiter();
    
    // Get allocation size
    u64 size = PT_REGS_PARM2(ctx);
    
    // Filter small allocations
    if (size < MIN_ALLOCATION_SIZE) {
        return 0;
    }
    
    // Rate limiting
    if (check_memory_rate_limit()) return 0;
    
    // Sampling
    if (check_memory_sampling(size)) return 0;
    
    // Get task info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) return 0;
    
    u32 pid = BPF_CORE_READ(task, tgid);
    u32 tid = BPF_CORE_READ(task, pid);
    
    // Check namespace filter
    u32 ns_id = get_pid_namespace_id(task);
    if (!is_namespace_allowed(ns_id)) {
        return 0;
    }
    
    // Create allocation info
    struct allocation_info info = {};
    info.size = size;
    info.timestamp = bpf_ktime_get_ns();
    info.pid = pid;
    info.tid = tid;
    info.cgroup_id = get_cgroup_id(task);
    info.caller_ip = PT_REGS_IP(ctx);
    info.namespace_pid = ns_id;
    
    get_current_comm(info.comm);
    
    // Use pid_tgid as temporary key to avoid collisions
    u64 pid_tgid = bpf_get_current_pid_tgid();
    bpf_map_update_elem(&active_allocations, &pid_tgid, &info, BPF_ANY);
    
    return 0;
}

// Track mmap return with CO-RE
SEC("uretprobe/mmap")
int trace_mmap_return(struct pt_regs *ctx) {
    void *addr = (void *)PT_REGS_RC(ctx);
    
    // Check for allocation failure
    if ((long)addr < 0) {
        return 0;
    }
    
    u64 pid_tgid = bpf_get_current_pid_tgid();
    
    // Look up pending allocation
    struct allocation_info *info = bpf_map_lookup_elem(&active_allocations, &pid_tgid);
    if (!info) {
        return 0;
    }
    
    // Move to real address
    u64 real_addr = (u64)addr;
    bpf_map_update_elem(&active_allocations, &real_addr, info, BPF_ANY);
    bpf_map_delete_elem(&active_allocations, &pid_tgid);
    
    // Get current task info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) return 0;
    
    // Create event
    struct memory_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.event_type = EVENT_MMAP;
    event.pid = info->pid;
    event.tid = info->tid;
    event.uid = BPF_CORE_READ(task, cred, uid.val);
    event.gid = BPF_CORE_READ(task, cred, gid.val);
    event.address = real_addr;
    event.size = info->size;
    event.cgroup_id = info->cgroup_id;
    event.caller_ip = info->caller_ip;
    event.namespace_pid = info->namespace_pid;
    __builtin_memcpy(event.comm, info->comm, sizeof(event.comm));
    
    // Submit event
    submit_memory_event(&event);
    
    return 0;
}

// Track munmap with CO-RE
SEC("uprobe/munmap")
int trace_munmap(struct pt_regs *ctx) {
    void *addr = (void *)PT_REGS_PARM1(ctx);
    u64 address = (u64)addr;
    
    // Check if we were tracking this allocation
    struct allocation_info *info = bpf_map_lookup_elem(&active_allocations, &address);
    if (!info) {
        return 0;
    }
    
    // Rate limiting
    if (check_memory_rate_limit()) return 0;
    
    // Get task info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) return 0;
    
    // Create event
    struct memory_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.event_type = EVENT_MUNMAP;
    event.pid = BPF_CORE_READ(task, tgid);
    event.tid = BPF_CORE_READ(task, pid);
    event.uid = BPF_CORE_READ(task, cred, uid.val);
    event.gid = BPF_CORE_READ(task, cred, gid.val);
    event.address = address;
    event.size = info->size;
    event.cgroup_id = info->cgroup_id;
    event.namespace_pid = info->namespace_pid;
    get_current_comm(event.comm);
    
    // Submit event
    submit_memory_event(&event);
    
    // Remove from tracking
    bpf_map_delete_elem(&active_allocations, &address);
    
    return 0;
}

// RSS stat tracepoint structure with CO-RE
struct trace_event_raw_rss_stat {
    struct trace_entry ent;
    u32 mm_id;
    u32 curr;
    s32 member;
    s64 size;
    char __data[];
};

// Track RSS changes with CO-RE
SEC("tracepoint/mm/rss_stat")
int trace_rss_change(struct trace_event_raw_rss_stat *ctx) {
    // Rate limiting
    if (check_memory_rate_limit()) return 0;
    
    s64 size = ctx->size;
    
    // Only track significant changes (> 1MB)
    if (size < RSS_THRESHOLD_PAGES && size > -RSS_THRESHOLD_PAGES) {
        return 0;
    }
    
    // Get task info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) return 0;
    
    u32 pid = BPF_CORE_READ(task, tgid);
    
    // Check namespace filter
    u32 ns_id = get_pid_namespace_id(task);
    if (!is_namespace_allowed(ns_id)) {
        return 0;
    }
    
    // Update RSS tracking
    u64 *last_rss = bpf_map_lookup_elem(&process_rss, &pid);
    u64 current_rss = size > 0 ? (u64)size : 0;
    
    if (last_rss) {
        s64 growth = (s64)current_rss - (s64)*last_rss;
        
        // Report significant changes
        if (growth > RSS_THRESHOLD_PAGES || growth < -RSS_THRESHOLD_PAGES) {
            struct memory_event event = {};
            event.timestamp = bpf_ktime_get_ns();
            event.event_type = EVENT_RSS_GROWTH;
            event.pid = pid;
            event.tid = BPF_CORE_READ(task, pid);
            event.uid = BPF_CORE_READ(task, cred, uid.val);
            event.gid = BPF_CORE_READ(task, cred, gid.val);
            event.rss_pages = current_rss;
            event.rss_growth = growth;
            event.cgroup_id = get_cgroup_id(task);
            event.namespace_pid = ns_id;
            event.is_oom_risk = check_oom_risk(current_rss);
            get_current_comm(event.comm);
            
            submit_memory_event(&event);
        }
    }
    
    bpf_map_update_elem(&process_rss, &pid, &current_rss, BPF_ANY);
    
    return 0;
}

// OOM killer invocation tracking with CO-RE
SEC("kprobe/oom_kill_process")
int trace_oom_kill(struct pt_regs *ctx) {
    // Get victim task with CO-RE
    struct task_struct *victim = (struct task_struct *)PT_REGS_PARM1(ctx);
    if (!victim) return 0;
    
    // Create OOM event
    struct memory_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.event_type = EVENT_OOM_RISK;
    event.pid = BPF_CORE_READ(victim, tgid);
    event.tid = BPF_CORE_READ(victim, pid);
    event.uid = BPF_CORE_READ(victim, cred, uid.val);
    event.gid = BPF_CORE_READ(victim, cred, gid.val);
    event.cgroup_id = get_cgroup_id(victim);
    event.namespace_pid = get_pid_namespace_id(victim);
    event.is_oom_risk = 1;
    
    // Get victim's RSS if available
    u64 *rss = bpf_map_lookup_elem(&process_rss, &event.pid);
    if (rss) {
        event.rss_pages = *rss;
    }
    
    // Get comm with CO-RE
    bpf_core_read_str(event.comm, sizeof(event.comm), &victim->comm);
    
    submit_memory_event(&event);
    
    return 0;
}

char _license[] SEC("license") = "GPL";