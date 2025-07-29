//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

// Memory event structure
struct memory_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u32 uid;
    u32 gid;
    u64 size;
    u64 addr;
    u32 flags;
    u8 type; // 0: alloc, 1: free, 2: mmap, 3: munmap
    char comm[16];
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Per-process memory tracking with persistence
struct memory_stats {
    u64 total_allocated;
    u64 total_freed;
    u64 current_usage;
    u32 alloc_count;
    u32 free_count;
    u64 peak_usage;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, u32); // pid
    __type(value, struct memory_stats);
    __uint(max_entries, 10240);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} process_memory SEC(".maps");

// CO-RE helper to read mm_struct fields
static __always_inline u64 get_mm_counter(struct mm_struct *mm, int member) {
    // Handle different kernel versions using CO-RE
    if (bpf_core_field_exists(mm->rss_stat)) {
        // Newer kernels have rss_stat
        switch (member) {
        case 0: // MM_FILEPAGES
            return BPF_CORE_READ(mm, rss_stat.count[MM_FILEPAGES]);
        case 1: // MM_ANONPAGES  
            return BPF_CORE_READ(mm, rss_stat.count[MM_ANONPAGES]);
        }
    }
    return 0;
}

// Trace memory allocations
SEC("tp_btf/kmem/mm_page_alloc")
int BPF_PROG(trace_mm_page_alloc, struct page *page, unsigned int order, gfp_t gfp_flags, int migratetype) {
    struct memory_event *e;
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tid = (u32)pid_tgid;
    e->uid = bpf_get_current_uid_gid() >> 32;
    e->gid = (u32)bpf_get_current_uid_gid();
    e->size = (1ULL << order) * 4096; // Convert order to bytes
    e->flags = gfp_flags;
    e->type = 0; // allocation
    
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    
    // Update per-process stats
    struct memory_stats *stats = bpf_map_lookup_elem(&process_memory, &pid);
    if (!stats) {
        struct memory_stats new_stats = {0};
        new_stats.total_allocated = e->size;
        new_stats.current_usage = e->size;
        new_stats.peak_usage = e->size;
        new_stats.alloc_count = 1;
        bpf_map_update_elem(&process_memory, &pid, &new_stats, BPF_NOEXIST);
    } else {
        __sync_fetch_and_add(&stats->total_allocated, e->size);
        __sync_fetch_and_add(&stats->current_usage, e->size);
        __sync_fetch_and_add(&stats->alloc_count, 1);
        
        // Update peak usage atomically
        u64 current = stats->current_usage;
        u64 peak = stats->peak_usage;
        while (current > peak) {
            if (__sync_bool_compare_and_swap(&stats->peak_usage, peak, current)) {
                break;
            }
            peak = stats->peak_usage;
        }
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Trace memory frees
SEC("tp_btf/kmem/mm_page_free")  
int BPF_PROG(trace_mm_page_free, struct page *page, unsigned int order) {
    struct memory_event *e;
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tid = (u32)pid_tgid;
    e->uid = bpf_get_current_uid_gid() >> 32;
    e->gid = (u32)bpf_get_current_uid_gid();
    e->size = (1ULL << order) * 4096;
    e->type = 1; // free
    
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    
    // Update per-process stats
    struct memory_stats *stats = bpf_map_lookup_elem(&process_memory, &pid);
    if (stats) {
        __sync_fetch_and_add(&stats->total_freed, e->size);
        __sync_fetch_and_sub(&stats->current_usage, e->size);
        __sync_fetch_and_add(&stats->free_count, 1);
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Trace mmap calls for user space memory tracking
SEC("tracepoint/syscalls/sys_enter_mmap")
int trace_sys_enter_mmap(struct trace_event_raw_sys_enter *ctx) {
    struct memory_event *e;
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    
    // Extract mmap arguments
    u64 length = (u64)ctx->args[1];
    if (length == 0) {
        return 0;
    }
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tid = (u32)pid_tgid;
    e->uid = bpf_get_current_uid_gid() >> 32;
    e->gid = (u32)bpf_get_current_uid_gid();
    e->size = length;
    e->flags = (u32)ctx->args[3]; // mmap flags
    e->type = 2; // mmap
    
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Get current memory usage for a process using CO-RE
SEC("kprobe/get_mm_rss")
int BPF_KPROBE(get_process_memory_usage) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    struct mm_struct *mm = BPF_CORE_READ(task, mm);
    
    if (!mm) {
        return 0;
    }
    
    // Read various memory counters using CO-RE
    u64 file_pages = get_mm_counter(mm, 0);
    u64 anon_pages = get_mm_counter(mm, 1);
    u64 total_pages = file_pages + anon_pages;
    u64 total_bytes = total_pages * 4096;
    
    // You can store or use this information as needed
    return 0;
}

// OOM killer tracking
SEC("tp_btf/oom/mark_victim")
int BPF_PROG(trace_oom_mark_victim, struct task_struct *task) {
    struct memory_event *e;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = BPF_CORE_READ(task, pid);
    e->tid = BPF_CORE_READ(task, pid);
    e->uid = BPF_CORE_READ(task, real_cred, uid.val);
    e->gid = BPF_CORE_READ(task, real_cred, gid.val);
    e->type = 0xFF; // OOM kill
    
    // Read comm directly from task_struct
    bpf_core_read_str(&e->comm, sizeof(e->comm), &task->comm);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}