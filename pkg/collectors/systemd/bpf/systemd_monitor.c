//go:build ignore

#include "../../ebpf/bpf/headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_COMM_LEN 16
#define MAX_FILENAME_LEN 256

// Event types
#define EVENT_EXEC 1
#define EVENT_EXIT 2
#define EVENT_KILL 3

// Event structure
struct systemd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 ppid;
    __u32 event_type;
    __u32 exit_code;
    char comm[MAX_COMM_LEN];
    char filename[MAX_FILENAME_LEN];
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Map to track systemd PIDs
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, __u8);
} systemd_pids SEC(".maps");

// Helper to check if PID is systemd-related
static inline int is_systemd_process(__u32 pid) {
    return bpf_map_lookup_elem(&systemd_pids, &pid) != NULL;
}

// Track process execution
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec(void *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u32 ppid = BPF_CORE_READ(task, real_parent, tgid);

    // Only track if parent is systemd or child of systemd
    if (!is_systemd_process(pid) && !is_systemd_process(ppid)) {
        return 0;
    }

    struct systemd_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->ppid = ppid;
    event->event_type = EVENT_EXEC;
    event->exit_code = 0;

    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get filename from execve args (simplified)
    __builtin_memset(event->filename, 0, sizeof(event->filename));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track process exit
SEC("tracepoint/syscalls/sys_enter_exit")
int trace_exit(void *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    if (!is_systemd_process(pid)) {
        return 0;
    }

    struct systemd_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->ppid = 0;
    event->event_type = EVENT_EXIT;
    event->exit_code = 0; // Could extract from context

    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    __builtin_memset(event->filename, 0, sizeof(event->filename));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char _license[] SEC("license") = "GPL";