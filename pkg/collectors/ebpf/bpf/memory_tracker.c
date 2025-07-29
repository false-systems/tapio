//go:build ignore

#include "headers/vmlinux.h"
#include "common.h"

// Map definition macros
#define __uint(name, val) int(*name)[val]
#define __type(name, val) typeof(val) *name
#define __array(name, val) typeof(val) *name[]

// Section attributes
#define SEC(name) __attribute__((section(name), used))

#define BPF_MAP_TYPE_PERF_EVENT_ARRAY 4
#define BPF_F_CURRENT_CPU 0xffffffffULL

// BPF helper prototypes
static long (*bpf_ktime_get_ns)(void) = (void *) 5;
static long (*bpf_get_current_pid_tgid)(void) = (void *) 14;
static long (*bpf_get_current_comm)(void *buf, __u32 size_of_buf) = (void *) 16;
static long (*bpf_perf_event_output)(void *ctx, void *map, __u64 flags, void *data, __u64 size) = (void *) 25;

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
    event.event_type = EVENT_MEMORY_ALLOC;
    
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
    event.event_type = EVENT_MEMORY_FREE;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Trace OOM kills
SEC("tracepoint/oom/oom_kill_process")
int trace_oom_kill_process(void *ctx) {
    struct memory_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.size = 0;
    event.event_type = EVENT_OOM_KILL;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

char _license[] SEC("license") = "GPL";