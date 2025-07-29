//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

char LICENSE[] SEC("license") = "GPL";

// CO-RE enabled network event structure
struct network_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u32 uid;
    u32 gid;
    u32 saddr;
    u32 daddr;
    u16 sport;
    u16 dport;
    u16 family;
    u16 proto;
    u32 netns;
    u32 bytes_sent;
    u32 bytes_received;
    u8 direction; // 0: egress, 1: ingress
    char comm[16];
} __attribute__((packed));

// Ring buffer map for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Per-CPU array for connection tracking
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, u32);
    __type(value, struct network_event);
    __uint(max_entries, 1);
} heap SEC(".maps");

// Hash map for connection state tracking with persistence
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, u64);  // pid_tgid
    __type(value, struct network_event);
    __uint(max_entries, 10240);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} connections SEC(".maps");

// Program array for tail calls
struct {
    __uint(type, BPF_MAP_TYPE_PROG_ARRAY);
    __uint(max_entries, 3);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u32));
} progs SEC(".maps");

// Define tail call indices
enum {
    TAIL_CALL_PROCESS_TCP = 0,
    TAIL_CALL_PROCESS_UDP = 1,
    TAIL_CALL_PROCESS_METRICS = 2,
};

// Helper to read socket fields using CO-RE
static __always_inline int read_sock_fields(struct sock *sk, struct network_event *event) {
    // Use CO-RE to read socket family
    event->family = BPF_CORE_READ(sk, __sk_common.skc_family);
    
    if (event->family == AF_INET) {
        // Read IPv4 addresses using CO-RE
        event->saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
        event->daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    } else if (event->family == AF_INET6) {
        // For IPv6, we'll just mark it differently for now
        event->saddr = 0;
        event->daddr = 0;
    }
    
    // Read ports using CO-RE field access
    event->sport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_num));
    event->dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    
    // Read protocol
    event->proto = BPF_CORE_READ(sk, sk_protocol);
    
    // Try to read network namespace if available
    struct net *net = BPF_CORE_READ(sk, __sk_common.skc_net.net);
    if (net) {
        // Use CO-RE to handle different kernel versions
        if (bpf_core_field_exists(net->ns.inum)) {
            event->netns = BPF_CORE_READ(net, ns.inum);
        } else {
            event->netns = 0;
        }
    }
    
    return 0;
}

// Main entry point for TCP connect
SEC("kprobe/tcp_v4_connect")
int BPF_KPROBE(trace_tcp_v4_connect, struct sock *sk) {
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    u32 tid = (u32)pid_tgid;
    
    // Get per-CPU storage
    u32 zero = 0;
    struct network_event *event = bpf_map_lookup_elem(&heap, &zero);
    if (!event) {
        return 0;
    }
    
    // Initialize event
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = tid;
    event->uid = bpf_get_current_uid_gid() >> 32;
    event->gid = (u32)bpf_get_current_uid_gid();
    event->direction = 0; // egress
    
    // Get process name
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Read socket fields using CO-RE
    if (read_sock_fields(sk, event) < 0) {
        return 0;
    }
    
    // Store in connection tracking map
    bpf_map_update_elem(&connections, &pid_tgid, event, BPF_ANY);
    
    // Tail call to protocol-specific handler
    if (event->proto == IPPROTO_TCP) {
        bpf_tail_call(ctx, &progs, TAIL_CALL_PROCESS_TCP);
    } else if (event->proto == IPPROTO_UDP) {
        bpf_tail_call(ctx, &progs, TAIL_CALL_PROCESS_UDP);
    }
    
    // If tail call fails, submit event directly
    submit_event(event);
    return 0;
}

// TCP-specific processing (tail call target)
SEC("kprobe/tcp_process")
int BPF_KPROBE(process_tcp) {
    u64 pid_tgid = bpf_get_current_pid_tgid();
    
    struct network_event *event = bpf_map_lookup_elem(&connections, &pid_tgid);
    if (!event) {
        return 0;
    }
    
    // TCP-specific processing
    event->proto = IPPROTO_TCP;
    
    // Submit event
    submit_event(event);
    
    // Chain to metrics processing
    bpf_tail_call(ctx, &progs, TAIL_CALL_PROCESS_METRICS);
    
    return 0;
}

// UDP-specific processing (tail call target)
SEC("kprobe/udp_process")
int BPF_KPROBE(process_udp) {
    u64 pid_tgid = bpf_get_current_pid_tgid();
    
    struct network_event *event = bpf_map_lookup_elem(&connections, &pid_tgid);
    if (!event) {
        return 0;
    }
    
    // UDP-specific processing
    event->proto = IPPROTO_UDP;
    
    // Submit event
    submit_event(event);
    
    // Chain to metrics processing
    bpf_tail_call(ctx, &progs, TAIL_CALL_PROCESS_METRICS);
    
    return 0;
}

// Metrics processing (tail call target)
SEC("kprobe/metrics_process")
int BPF_KPROBE(process_metrics) {
    // Update metrics maps (not shown for brevity)
    return 0;
}

// Helper function to submit events
static __always_inline void submit_event(struct network_event *event) {
    struct network_event *e;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) {
        return;
    }
    
    __builtin_memcpy(e, event, sizeof(*e));
    bpf_ringbuf_submit(e, 0);
}

// XDP program for packet-level processing
SEC("xdp")
int xdp_packet_filter(struct xdp_md *ctx) {
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    
    // Basic packet parsing using CO-RE
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end) {
        return XDP_PASS;
    }
    
    // Check if IPv4
    if (eth->h_proto != bpf_htons(ETH_P_IP)) {
        return XDP_PASS;
    }
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end) {
        return XDP_PASS;
    }
    
    // Example: Drop packets from specific source
    // This would be configurable via maps in production
    if (ip->saddr == 0x0100007f) { // 127.0.0.1 in network byte order
        return XDP_DROP;
    }
    
    return XDP_PASS;
}

// Kretprobe for capturing return values
SEC("kretprobe/tcp_v4_connect")
int BPF_KRETPROBE(trace_tcp_v4_connect_ret, int ret) {
    u64 pid_tgid = bpf_get_current_pid_tgid();
    
    struct network_event *event = bpf_map_lookup_elem(&connections, &pid_tgid);
    if (!event) {
        return 0;
    }
    
    // Check if connection failed
    if (ret != 0) {
        // Mark as failed connection
        event->direction = 0xFF;
    }
    
    // Clean up connection tracking
    bpf_map_delete_elem(&connections, &pid_tgid);
    
    return 0;
}

// BTF-enabled tracepoint for more efficient access
SEC("tp_btf/sock/inet_sock_set_state")
int BPF_PROG(trace_inet_sock_set_state, struct sock *sk, int oldstate, int newstate) {
    // This is a BTF-enabled tracepoint that gives direct access to kernel structures
    if (newstate != TCP_ESTABLISHED) {
        return 0;
    }
    
    u32 zero = 0;
    struct network_event *event = bpf_map_lookup_elem(&heap, &zero);
    if (!event) {
        return 0;
    }
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    
    // Direct access to kernel structures via BTF
    event->family = sk->__sk_common.skc_family;
    event->saddr = sk->__sk_common.skc_rcv_saddr;
    event->daddr = sk->__sk_common.skc_daddr;
    event->sport = sk->__sk_common.skc_num;
    event->dport = bpf_ntohs(sk->__sk_common.skc_dport);
    
    submit_event(event);
    return 0;
}