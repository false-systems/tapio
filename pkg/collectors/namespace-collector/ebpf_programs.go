//go:build linux

package namespace_collector

// Minimal eBPF programs for CNI syscall tracing
// These are the actual eBPF C programs as strings

const cniTraceProgramSource = `
#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <linux/sched.h>

// Event structure matching what we send to userspace
struct cni_event {
    __u32 pid;
    __u32 tgid;
    __u64 timestamp;
    __u8  type; // 0=exec, 1=netns, 2=veth, 3=route
    char  comm[16];
    char  data[64]; // CNI binary name or network operation details
};

// Map for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} events SEC(".maps");

// Trace CNI binary execution
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_cni_exec(struct trace_event_raw_sys_enter* ctx) {
    struct cni_event event = {};
    
    // Get filename being executed
    const char *filename = (const char *)ctx->args[0];
    
    // Check if it's a CNI binary (contains "/cni/bin/")
    if (!filename) return 0;
    
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tgid = bpf_get_current_pid_tgid();
    event.timestamp = bpf_ktime_get_ns();
    event.type = 0; // exec type
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    bpf_probe_read_user_str(&event.data, sizeof(event.data), filename);
    
    // Only send event if it contains "cni" in path
    if (event.data[0] != 0) {
        bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, 
                              &event, sizeof(event));
    }
    
    return 0;
}

// Trace network namespace creation (CLONE_NEWNET)
SEC("tracepoint/syscalls/sys_enter_clone")
int trace_netns_create(struct trace_event_raw_sys_enter* ctx) {
    unsigned long flags = ctx->args[0];
    
    // Check for CLONE_NEWNET flag (0x40000000)
    if (!(flags & 0x40000000)) return 0;
    
    struct cni_event event = {};
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tgid = bpf_get_current_pid_tgid();
    event.timestamp = bpf_ktime_get_ns();
    event.type = 1; // netns type
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    __builtin_snprintf(event.data, sizeof(event.data), "netns_create");
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, 
                          &event, sizeof(event));
    return 0;
}

// Trace network namespace switching
SEC("tracepoint/syscalls/sys_enter_setns")
int trace_netns_enter(struct trace_event_raw_sys_enter* ctx) {
    int nstype = ctx->args[1];
    
    // Check for CLONE_NEWNET (0x40000000)
    if (nstype != 0 && nstype != 0x40000000) return 0;
    
    struct cni_event event = {};
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tgid = bpf_get_current_pid_tgid();
    event.timestamp = bpf_ktime_get_ns();
    event.type = 1; // netns type
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    __builtin_snprintf(event.data, sizeof(event.data), "netns_enter_fd_%d", (int)ctx->args[0]);
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, 
                          &event, sizeof(event));
    return 0;
}

char _license[] SEC("license") = "GPL";
`
