//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event types
#define LINK_EVENT_SYN_TIMEOUT    1
#define LINK_EVENT_CONNECTION_RST 2
#define LINK_EVENT_ARP_TIMEOUT    3

// Link failure event - keep it simple and lean
struct link_event {
    __u64 timestamp;
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  event_type;
    __u8  protocol;  // TCP=6, UDP=17
    __u16 _padding;
    char  comm[16];
} __attribute__((packed));

// Ring buffer for events (256KB - same as status observer)
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} link_events SEC(".maps");

// Connection tracking for SYN timeouts
struct conn_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
} __attribute__((packed));

struct syn_info {
    __u64 timestamp;
    __u32 pid;
    char  comm[16];
} __attribute__((packed));

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10240);
    __type(key, struct conn_key);
    __type(value, struct syn_info);
} syn_tracker SEC(".maps");

// Helper to emit link failure event
static __always_inline int emit_link_event(__u8 event_type, __u32 src_ip, __u32 dst_ip,
                                          __u16 src_port, __u16 dst_port, __u8 protocol) {
    struct link_event *event;

    event = bpf_ringbuf_reserve(&link_events, sizeof(*event), 0);
    if (!event) {
        return -1;
    }

    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->src_ip = src_ip;
    event->dst_ip = dst_ip;
    event->src_port = src_port;
    event->dst_port = dst_port;
    event->event_type = event_type;
    event->protocol = protocol;
    event->_padding = 0;

    bpf_get_current_comm(event->comm, sizeof(event->comm));

    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track TCP SYN attempts
SEC("kprobe/tcp_v4_connect")
int trace_tcp_syn(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct inet_sock *inet;
    struct conn_key key = {};
    struct syn_info info = {};

    if (!sk) {
        return 0;
    }

    // Get inet_sock from sock using CO-RE
    inet = (struct inet_sock *)sk;
    if (!inet) {
        return 0;
    }

    // Read connection details with CO-RE
    BPF_CORE_READ_INTO(&key.src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&key.dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&key.src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&key.dst_port, inet, inet_dport);

    // Store SYN attempt with timestamp
    info.timestamp = bpf_ktime_get_ns();
    info.pid = bpf_get_current_pid_tgid() >> 32;
    bpf_get_current_comm(info.comm, sizeof(info.comm));

    bpf_map_update_elem(&syn_tracker, &key, &info, BPF_ANY);

    return 0;
}

// Detect connection establishment (completes 3-way handshake)
SEC("kprobe/tcp_finish_connect")
int trace_tcp_established(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct inet_sock *inet;
    struct conn_key key = {};

    if (!sk) {
        return 0;
    }

    inet = (struct inet_sock *)sk;
    if (!inet) {
        return 0;
    }

    // Read connection details
    BPF_CORE_READ_INTO(&key.src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&key.dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&key.src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&key.dst_port, inet, inet_dport);

    // Remove from SYN tracker (connection established successfully)
    bpf_map_delete_elem(&syn_tracker, &key);

    return 0;
}

// Detect connection resets (when RST is sent)
SEC("kprobe/tcp_send_active_reset")
int trace_tcp_reset(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct inet_sock *inet;

    if (!sk) {
        return 0;
    }

    inet = (struct inet_sock *)sk;
    if (!inet) {
        return 0;
    }

    __u32 src_ip, dst_ip;
    __u16 src_port, dst_port;

    BPF_CORE_READ_INTO(&src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&dst_port, inet, inet_dport);

    // Emit connection reset event
    emit_link_event(LINK_EVENT_CONNECTION_RST, src_ip, dst_ip, src_port, dst_port, 6);

    return 0;
}

// Detect retransmission timeouts (indicates network issues)
SEC("kprobe/tcp_retransmit_timer")
int trace_tcp_timeout(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct inet_sock *inet;
    struct conn_key key = {};

    if (!sk) {
        return 0;
    }

    inet = (struct inet_sock *)sk;
    if (!inet) {
        return 0;
    }

    BPF_CORE_READ_INTO(&key.src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&key.dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&key.src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&key.dst_port, inet, inet_dport);

    // Check if this was a tracked SYN attempt
    struct syn_info *info = bpf_map_lookup_elem(&syn_tracker, &key);
    if (info) {
        // Emit SYN timeout event
        emit_link_event(LINK_EVENT_SYN_TIMEOUT, key.src_ip, key.dst_ip,
                       key.src_port, key.dst_port, 6);

        // Remove from tracker
        bpf_map_delete_elem(&syn_tracker, &key);
    }

    return 0;
}

// Periodic cleanup via tcp_close (happens frequently enough)
SEC("kprobe/tcp_close")
int cleanup_stale_syns(struct pt_regs *ctx) {
    __u64 now = bpf_ktime_get_ns();
    __u64 timeout_ns = 5000000000; // 5 seconds timeout for SYN

    // Note: We can't iterate maps in kernel, but we can check
    // the current connection being closed
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) {
        return 0;
    }

    struct inet_sock *inet = (struct inet_sock *)sk;
    if (!inet) {
        return 0;
    }

    struct conn_key key = {};
    BPF_CORE_READ_INTO(&key.src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&key.dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&key.src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&key.dst_port, inet, inet_dport);

    // Check if this connection has a stale SYN entry
    struct syn_info *info = bpf_map_lookup_elem(&syn_tracker, &key);
    if (info && (now - info->timestamp) > timeout_ns) {
        // This SYN timed out - emit event
        emit_link_event(LINK_EVENT_SYN_TIMEOUT, key.src_ip, key.dst_ip,
                       key.src_port, key.dst_port, 6);
        bpf_map_delete_elem(&syn_tracker, &key);
    }

    return 0;
}

char LICENSE[] SEC("license") = "GPL";