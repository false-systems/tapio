//go:build ignore

#include "headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

struct memory_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u64 addr;
    __u64 size;
    __u8 event_type; // 0 = alloc, 1 = free
    __u8 pad[3];
    char comm[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} events SEC(".maps");

// Trace memory allocations
SEC("tracepoint/kmem/mm_page_alloc")
int trace_mm_page_alloc(struct trace_event_raw_mm_page_alloc *ctx) {
    struct memory_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.size = (1 << ctx->order) << 12; // Convert order to pages to bytes
    event.event_type = 0; // allocation
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Trace memory frees
SEC("tracepoint/kmem/mm_page_free")
int trace_mm_page_free(struct trace_event_raw_mm_page_free *ctx) {
    struct memory_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.size = (1 << ctx->order) << 12; // Convert order to pages to bytes
    event.event_type = 1; // free
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

char _license[] SEC("license") = "GPL";