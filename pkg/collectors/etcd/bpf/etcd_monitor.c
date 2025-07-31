// SPDX-License-Identifier: GPL-2.0
// Minimal etcd eBPF monitor - raw syscall monitoring only

#include "headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

// Event types
#define EVENT_WRITE 1
#define EVENT_FSYNC 2

// Max data capture
#define MAX_DATA_SIZE 256

// Raw event structure - no business logic
struct etcd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u8  event_type;
    __u8  pad[3];
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 data_len;
    __u8  data[MAX_DATA_SIZE];
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB
} events SEC(".maps");

// Helper to check if process might be etcd (basic check)
static __always_inline bool is_etcd_like(struct task_struct *task)
{
    char comm[16];
    bpf_probe_read_kernel_str(&comm, sizeof(comm), &task->comm);
    
    // Very basic check - just see if it contains "etcd"
    return (comm[0] == 'e' && comm[1] == 't' && comm[2] == 'c' && comm[3] == 'd');
}

// Monitor write syscalls
SEC("tracepoint/syscalls/sys_enter_write")
int trace_sys_enter_write(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Basic filter - only track etcd-like processes
    if (!is_etcd_like(task))
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    e->event_type = EVENT_WRITE;
    
    // Get file descriptor and size
    int fd = (int)ctx->args[0];
    size_t count = (size_t)ctx->args[2];
    
    e->data_len = count > MAX_DATA_SIZE ? MAX_DATA_SIZE : count;
    
    // Just store fd and size as raw data
    if (e->data_len >= 8) {
        *(int*)e->data = fd;
        *(size_t*)(e->data + 4) = count;
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor fsync syscalls
SEC("tracepoint/syscalls/sys_enter_fsync")
int trace_sys_enter_fsync(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_etcd_like(task))
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    e->event_type = EVENT_FSYNC;
    
    // Get file descriptor
    int fd = (int)ctx->args[0];
    
    e->data_len = sizeof(int);
    *(int*)e->data = fd;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}