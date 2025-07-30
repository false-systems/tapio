//go:build linux
// +build linux

package cni

// eBPF programs as C code strings
// In production, these would be compiled from .c files

const cniExecProgram = `
#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

struct cni_event {
    __u64 timestamp;
    __u32 pid;
    __u32 event_type;
    char data[256];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(int));
    __uint(value_size, sizeof(int));
} cni_events SEC(".maps");

SEC("tracepoint/syscalls/sys_enter_execve")
int trace_cni_exec(struct trace_event_raw_sys_enter* ctx)
{
    struct cni_event event = {};
    char comm[256];
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.event_type = 0; // CNI_EXEC
    
    // Get the filename being executed
    char* filename_ptr = (char*)ctx->args[0];
    bpf_probe_read_str(&event.data, sizeof(event.data), filename_ptr);
    
    // Check if it's a CNI binary
    if (!__builtin_strstr(event.data, "/opt/cni/bin/") && 
        !__builtin_strstr(event.data, "/usr/libexec/cni/")) {
        return 0;
    }
    
    // Submit event
    bpf_perf_event_output(ctx, &cni_events, BPF_F_CURRENT_CPU, 
                          &event, sizeof(event));
    
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_clone")
int trace_clone(struct trace_event_raw_sys_enter* ctx)
{
    struct cni_event event = {};
    unsigned long flags = ctx->args[0];
    
    // Check for CLONE_NEWNET flag (0x40000000)
    if (!(flags & 0x40000000)) {
        return 0;
    }
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.event_type = 1; // NETNS_CREATE
    
    // Get process name
    bpf_get_current_comm(&event.data, sizeof(event.data));
    
    // Submit event
    bpf_perf_event_output(ctx, &cni_events, BPF_F_CURRENT_CPU,
                          &event, sizeof(event));
    
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_setns")
int trace_setns(struct trace_event_raw_sys_enter* ctx)
{
    struct cni_event event = {};
    int nstype = ctx->args[1];
    
    // Check for CLONE_NEWNET (0x40000000)
    if (nstype != 0x40000000) {
        return 0;
    }
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.event_type = 2; // NETNS_CHANGE
    
    // Get process name
    bpf_get_current_comm(&event.data, sizeof(event.data));
    
    // Submit event
    bpf_perf_event_output(ctx, &cni_events, BPF_F_CURRENT_CPU,
                          &event, sizeof(event));
    
    return 0;
}

char _license[] SEC("license") = "GPL";
`
