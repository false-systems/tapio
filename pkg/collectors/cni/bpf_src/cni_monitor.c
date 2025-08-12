// SPDX-License-Identifier: GPL-2.0
// Minimal CNI monitoring - tracks network namespace operations

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Event types
#define EVENT_NETNS_ENTER  1
#define EVENT_NETNS_CREATE 2
#define EVENT_NETNS_EXIT   3

// Flags for clone/unshare
#define CLONE_NEWNET 0x40000000

struct cni_event {
    __u64 timestamp;
    __u32 pid;
    __u32 netns;
    __u32 event_type;
    char comm[16];
    char data[64];
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB
} events SEC(".maps");

// Helper to get current netns inode
static __always_inline __u32 get_netns_ino(struct task_struct *task)
{
    struct nsproxy *nsproxy;
    struct net *net_ns;
    struct ns_common *ns;
    unsigned int inum = 0;

    nsproxy = BPF_CORE_READ(task, nsproxy);
    if (!nsproxy)
        return 0;

    net_ns = BPF_CORE_READ(nsproxy, net_ns);
    if (!net_ns)
        return 0;

    // Read the ns field which is an embedded struct, not a pointer
    BPF_CORE_READ_INTO(&inum, net_ns, ns.inum);
    return inum;
}

// Monitor setns - entering different network namespace
SEC("tracepoint/syscalls/sys_enter_setns")
int trace_sys_enter_setns(struct trace_event_raw_sys_enter *ctx)
{
    struct cni_event *e;
    struct task_struct *task;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;

    task = (struct task_struct *)bpf_get_current_task();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->netns = get_netns_ino(task);
    e->event_type = EVENT_NETNS_ENTER;
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Get the fd and nstype from syscall args
    int fd = (int)ctx->args[0];
    int nstype = (int)ctx->args[1];
    
    BPF_SNPRINTF(e->data, sizeof(e->data), "fd=%d nstype=%d", fd, nstype);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor unshare - creating new network namespace
SEC("tracepoint/syscalls/sys_enter_unshare")
int trace_sys_enter_unshare(struct trace_event_raw_sys_enter *ctx)
{
    unsigned long flags = (unsigned long)ctx->args[0];
    
    // Only interested in network namespace creation
    if (!(flags & CLONE_NEWNET))
        return 0;
    
    struct cni_event *e;
    struct task_struct *task;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;

    task = (struct task_struct *)bpf_get_current_task();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->netns = get_netns_ino(task);
    e->event_type = EVENT_NETNS_CREATE;
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    BPF_SNPRINTF(e->data, sizeof(e->data), "flags=0x%lx", flags);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";