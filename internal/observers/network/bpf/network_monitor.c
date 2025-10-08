// SPDX-License-Identifier: GPL-2.0
// Network observer eBPF program for TCP/UDP connection tracking

#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <linux/socket.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define TASK_COMM_LEN 16
#define AF_INET 2

// Network event structure - matches Go-side NetworkEventBPF
struct network_event {
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol; // 6=TCP, 17=UDP
    __u8  _pad1;    // Padding for alignment
    char  comm[TASK_COMM_LEN];
    __u16 _pad2;    // Padding to align to 4-byte boundary (total 36 bytes)
};

// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4096 * sizeof(struct network_event));
} events SEC(".maps");

char LICENSE[] SEC("license") = "GPL";
