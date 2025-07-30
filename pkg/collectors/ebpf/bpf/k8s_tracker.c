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
static long (*bpf_get_current_cgroup_id)(void) = (void *) 80;
static long (*bpf_probe_read_kernel)(void *dst, __u32 size, const void *unsafe_ptr) = (void *) 113;

// K8s event types
#define EVENT_K8S_CONTAINER_CREATE 10
#define EVENT_K8S_CONTAINER_DELETE 11
#define EVENT_K8S_NETNS_CREATE    12
#define EVENT_K8S_NETNS_DELETE    13
#define EVENT_K8S_CGROUP_CREATE   14
#define EVENT_K8S_CGROUP_DELETE   15
#define EVENT_K8S_EXEC_IN_POD     16

// K8s event structure
struct k8s_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u64 cgroup_id;         // cgroup ID (can correlate to container)
    u32 namespace_pid;     // PID in namespace
    u32 event_type;
    char comm[TASK_COMM_LEN];
    char container_id[64]; // Container ID from cgroup path
    u32 netns_ino;        // Network namespace inode
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} k8s_events SEC(".maps");

// Helper to extract container ID from cgroup path
// Cgroup paths look like: /kubepods/burstable/pod<pod-uid>/<container-id>
static __always_inline void extract_container_id(struct k8s_event *event) {
    // For now, just use cgroup_id as a proxy
    // In production, would parse the actual cgroup path
    event->container_id[0] = 'c';
    event->container_id[1] = 'g';
    event->container_id[2] = ':';
    // Convert cgroup_id to hex string (simplified)
    for (int i = 0; i < 8; i++) {
        u8 nibble = (event->cgroup_id >> (i * 8)) & 0xFF;
        event->container_id[3 + i * 2] = "0123456789abcdef"[(nibble >> 4) & 0xF];
        event->container_id[3 + i * 2 + 1] = "0123456789abcdef"[nibble & 0xF];
    }
    event->container_id[19] = '\0';
}

// Track container creation via clone syscall with CLONE_NEWPID
SEC("raw_tracepoint/sys_enter")
int trace_container_create(struct bpf_raw_tracepoint_args *ctx) {
    // Get syscall number from pt_regs
    struct pt_regs *regs = (struct pt_regs *)ctx->args[0];
    long syscall_nr;
    bpf_probe_read_kernel(&syscall_nr, sizeof(syscall_nr), &regs->orig_ax);
    
    // Check if this is clone syscall (56 on x86_64)
    if (syscall_nr != 56)
        return 0;
    
    // Check clone flags for container indicators
    unsigned long flags;
    bpf_probe_read_kernel(&flags, sizeof(flags), &regs->di);
    
    // CLONE_NEWPID = 0x20000000, CLONE_NEWNS = 0x00020000, CLONE_NEWNET = 0x40000000
    if (!(flags & 0x20000000))
        return 0;
    
    struct k8s_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    event.event_type = EVENT_K8S_CONTAINER_CREATE;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    extract_container_id(&event);
    
    bpf_perf_event_output(ctx, &k8s_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Track cgroup creation (containers are placed in cgroups)
SEC("kprobe/cgroup_mkdir")
int trace_cgroup_mkdir(struct pt_regs *ctx) {
    struct k8s_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    event.event_type = EVENT_K8S_CGROUP_CREATE;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    extract_container_id(&event);
    
    bpf_perf_event_output(ctx, &k8s_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Track cgroup deletion  
SEC("kprobe/cgroup_rmdir")
int trace_cgroup_rmdir(struct pt_regs *ctx) {
    struct k8s_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    event.event_type = EVENT_K8S_CGROUP_DELETE;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    extract_container_id(&event);
    
    bpf_perf_event_output(ctx, &k8s_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Track exec calls in containers (kubectl exec, debugging)
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec_in_container(struct trace_event_raw_sys_enter *ctx) {
    u64 cgroup_id = bpf_get_current_cgroup_id();
    
    // Simple heuristic: high cgroup IDs are likely containers
    // In production, would check against a map of known container cgroups
    if (cgroup_id < 1000000)
        return 0;
    
    struct k8s_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.cgroup_id = cgroup_id;
    event.event_type = EVENT_K8S_EXEC_IN_POD;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    extract_container_id(&event);
    
    bpf_perf_event_output(ctx, &k8s_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

// Track network namespace operations
SEC("kprobe/create_new_namespaces")  
int trace_netns_create(struct pt_regs *ctx) {
    unsigned long flags;
    bpf_probe_read_kernel(&flags, sizeof(flags), (void *)ctx->di);
    
    // CLONE_NEWNET = 0x40000000
    if (!(flags & 0x40000000))
        return 0;
    
    struct k8s_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event.cgroup_id = bpf_get_current_cgroup_id();
    event.event_type = EVENT_K8S_NETNS_CREATE;
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    extract_container_id(&event);
    
    bpf_perf_event_output(ctx, &k8s_events, BPF_F_CURRENT_CPU, &event, sizeof(event));
    return 0;
}

char _license[] SEC("license") = "GPL";