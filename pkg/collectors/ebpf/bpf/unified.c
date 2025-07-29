// SPDX-License-Identifier: GPL-2.0-or-later
// Unified eBPF program for Tapio collector with CO-RE support

#include "headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Event types
#define EVENT_NETWORK  1
#define EVENT_SYSCALL  2
#define EVENT_MEMORY   3
#define EVENT_OOM      4

// Network families
#ifndef AF_INET
#define AF_INET 2
#endif
#ifndef AF_INET6
#define AF_INET6 10
#endif

// Unified event structure - minimal to reduce overhead
struct event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 cpu;
    __u8  type;
    __u8  flags;
    __u16 data_len;
    __u8  data[64]; // Minimal data capture
} __attribute__((packed));

// Ring buffer for events - single buffer for all event types
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 * 1024 * 1024); // 8MB
} events SEC(".maps");

// Helper to check if process is in container
static __always_inline bool is_container_process(struct task_struct *task)
{
    __u32 ns_level = 0;
    
    // Use CO-RE to safely check namespace level
    if (bpf_core_field_exists(task->nsproxy)) {
        struct pid_namespace *pidns = BPF_CORE_READ(task, nsproxy, pid_ns_for_children);
        if (pidns) {
            ns_level = BPF_CORE_READ(pidns, level);
        }
    }
    
    // Level > 0 means we're in a container namespace
    return ns_level > 0;
}

// Memory allocation tracking
SEC("tp_btf/kmem/kmalloc")
int BPF_PROG(trace_kmalloc, unsigned long call_site, const void *ptr,
             size_t bytes_req, size_t bytes_alloc, gfp_t gfp_flags)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
    
    // Only track container processes
    if (!is_container_process(task))
        return 0;
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->cpu = bpf_get_smp_processor_id();
    e->type = EVENT_MEMORY;
    e->flags = 0; // allocation
    e->data_len = 16;
    
    // Store allocation size and call site
    *(__u64 *)&e->data[0] = bytes_alloc;
    *(__u64 *)&e->data[8] = call_site;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Memory free tracking
SEC("tp_btf/kmem/kfree")
int BPF_PROG(trace_kfree, unsigned long call_site, const void *ptr)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
    
    if (!is_container_process(task))
        return 0;
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->cpu = bpf_get_smp_processor_id();
    e->type = EVENT_MEMORY;
    e->flags = 1; // free
    e->data_len = 16;
    
    // Store pointer and call site
    *(__u64 *)&e->data[0] = (__u64)ptr;
    *(__u64 *)&e->data[8] = call_site;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// OOM killer tracking
SEC("tp_btf/oom/oom_score_adj_update")
int BPF_PROG(trace_oom)
{
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->cpu = bpf_get_smp_processor_id();
    e->type = EVENT_OOM;
    e->flags = 0;
    e->data_len = 0;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Network connection tracking with CO-RE
SEC("fentry/tcp_v4_connect")
int BPF_PROG(trace_tcp_v4_connect, struct sock *sk)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
    
    if (!is_container_process(task))
        return 0;
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->cpu = bpf_get_smp_processor_id();
    e->type = EVENT_NETWORK;
    e->flags = AF_INET;
    e->data_len = 12;
    
    // CO-RE: safely read socket info
    __u32 saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    __u32 daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    __u16 sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    __u16 dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    
    // Pack network info
    *(__u32 *)&e->data[0] = saddr;
    *(__u32 *)&e->data[4] = daddr;
    *(__u16 *)&e->data[8] = sport;
    *(__u16 *)&e->data[10] = dport;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Process execution tracking
SEC("tp_btf/sched_process_exec")
int BPF_PROG(trace_exec)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
    
    if (!is_container_process(task))
        return 0;
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->cpu = bpf_get_smp_processor_id();
    e->type = EVENT_SYSCALL;
    e->flags = 0; // exec
    e->data_len = 16;
    
    // Store comm (process name)
    bpf_get_current_comm(&e->data[0], 16);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";