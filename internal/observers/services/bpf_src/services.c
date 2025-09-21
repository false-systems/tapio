//go:build ignore

#include <linux/bpf.h>
#include <linux/types.h>
#include <linux/socket.h>
#include <linux/tcp.h>
#include <linux/sched.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

char LICENSE[] SEC("license") = "GPL";

// Connection event types
enum {
    CONN_CONNECT = 1,
    CONN_ACCEPT = 2,
    CONN_CLOSE = 3,
};

// Connection event structure - must match Go struct
struct connection_event {
    __u64 timestamp;
    __u32 event_type;
    __u8  direction;    // 0=outbound, 1=inbound
    __u8  pad1[3];

    // Connection details
    __u8  src_ip[16];
    __u8  dst_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u16 family;
    __u16 pad2;

    // Process context
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    char  comm[16];

    // Network namespace
    __u32 netns;
    __u32 pad3;
};

// Perf event map for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
} events SEC(".maps");

// Helper to get current task info
static __always_inline void get_task_info(struct connection_event *evt) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();

    evt->timestamp = bpf_ktime_get_ns();
    evt->pid = bpf_get_current_pid_tgid() >> 32;
    evt->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    evt->uid = bpf_get_current_uid_gid() >> 32;
    evt->gid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    evt->cgroup_id = bpf_get_current_cgroup_id();
    bpf_get_current_comm(&evt->comm, sizeof(evt->comm));
}

// Helper to extract IP addresses from sock
static __always_inline void get_sock_addrs(struct sock *sk, struct connection_event *evt) {
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    evt->family = family;

    if (family == AF_INET) {
        // IPv4 addresses
        __be32 saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
        __be32 daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);

        // Store IPv4 in first 4 bytes of the array
        __builtin_memcpy(evt->src_ip, &saddr, 4);
        __builtin_memcpy(evt->dst_ip, &daddr, 4);
    } else if (family == AF_INET6) {
        // IPv6 addresses
        BPF_CORE_READ_INTO(&evt->src_ip, sk, __sk_common.skc_v6_rcv_saddr.in6_u.u6_addr8);
        BPF_CORE_READ_INTO(&evt->dst_ip, sk, __sk_common.skc_v6_daddr.in6_u.u6_addr8);
    }

    // Get ports
    evt->src_port = BPF_CORE_READ(sk, __sk_common.skc_num);
    evt->dst_port = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));

    // Get network namespace
    evt->netns = BPF_CORE_READ(sk, __sk_common.skc_net.net, ns.inum);
}

// Tracepoint for TCP connect (outbound connections)
SEC("tracepoint/tcp/tcp_connect")
int tcp_connect(struct trace_event_raw_tcp_event_sk *ctx) {
    struct connection_event evt = {};
    struct sock *sk = (struct sock *)ctx->skaddr;

    if (!sk) {
        return 0;
    }

    evt.event_type = CONN_CONNECT;
    evt.direction = 0; // Outbound

    get_task_info(&evt);
    get_sock_addrs(sk, &evt);

    // Send event to userspace
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &evt, sizeof(evt));

    return 0;
}

// Kprobe for TCP accept (inbound connections)
SEC("kprobe/tcp_accept")
int tcp_accept(struct pt_regs *ctx) {
    struct connection_event evt = {};
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

    if (!sk) {
        return 0;
    }

    evt.event_type = CONN_ACCEPT;
    evt.direction = 1; // Inbound

    get_task_info(&evt);
    get_sock_addrs(sk, &evt);

    // Send event to userspace
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &evt, sizeof(evt));

    return 0;
}

// Tracepoint for TCP close
SEC("tracepoint/tcp/tcp_close")
int tcp_close(struct trace_event_raw_tcp_event_sk *ctx) {
    struct connection_event evt = {};
    struct sock *sk = (struct sock *)ctx->skaddr;

    if (!sk) {
        return 0;
    }

    evt.event_type = CONN_CLOSE;

    get_task_info(&evt);
    get_sock_addrs(sk, &evt);

    // Send event to userspace
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &evt, sizeof(evt));

    return 0;
}