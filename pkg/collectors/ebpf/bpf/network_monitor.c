#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_ENTRIES 10240
#define MAX_CONN_ENTRIES 8192

// Connection state definitions
#define TCP_ESTABLISHED 1
#define TCP_SYN_SENT 2
#define TCP_SYN_RECV 3
#define TCP_FIN_WAIT1 4
#define TCP_FIN_WAIT2 5
#define TCP_TIME_WAIT 6
#define TCP_CLOSE 7
#define TCP_CLOSE_WAIT 8
#define TCP_LAST_ACK 9
#define TCP_LISTEN 10
#define TCP_CLOSING 11

// Network event types
enum network_event_type {
    NET_CONN_ESTABLISHED = 1,
    NET_CONN_CLOSED = 2,
    NET_CONN_FAILED = 3,
    NET_PACKET_DROP = 4,
    NET_HIGH_LATENCY = 5,
    NET_RETRANSMIT = 6,
};

// Network connection tracking
struct connection_key {
    u32 pid;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u8 protocol; // TCP=6, UDP=17
};

struct connection_stats {
    u64 start_time;
    u64 last_seen;
    u64 bytes_sent;
    u64 bytes_recv;
    u64 packets_sent;
    u64 packets_recv;
    u64 retransmits;
    u32 rtt_min;
    u32 rtt_max;
    u32 rtt_avg;
    u8 state;
    u8 failed;
};

// Network event structure
struct network_event {
    u64 timestamp;
    u32 pid;
    u32 tgid;
    u32 uid;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u8 protocol;
    u8 event_type;
    u32 latency_us;
    u32 bytes;
    u16 error_code;
    char comm[16];
    char container_id[64];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CONN_ENTRIES);
    __type(key, struct connection_key);
    __type(value, struct connection_stats);
} connection_tracker SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} network_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, u32);
    __type(value, u64);
} pid_start_time SEC(".maps");

// Helper function to extract container ID from cgroup
static __always_inline int extract_container_id(char *container_id) {
    struct task_struct *task;
    struct css_set *cgroups;
    struct cgroup_subsys_state *css;
    
    task = (struct task_struct *)bpf_get_current_task();
    if (!task)
        return -1;
    
    // Read the cgroups from task
    BPF_CORE_READ_INTO(&cgroups, task, cgroups);
    if (!cgroups) {
        __builtin_memcpy(container_id, "host\0", 5);
        return 0;
    }
    
    // For now, just mark it as unknown - full implementation would parse cgroup path
    __builtin_memcpy(container_id, "container\0", 10);
    
    return 0;
}

// Helper function to get connection key
static __always_inline void get_connection_key(struct connection_key *key, u32 pid,
                              u32 src_ip, u32 dst_ip, 
                              u16 src_port, u16 dst_port, u8 protocol) {
    key->pid = pid;
    key->src_ip = src_ip;
    key->dst_ip = dst_ip;
    key->src_port = src_port;
    key->dst_port = dst_port;
    key->protocol = protocol;
}

// Helper function to emit network event
static __always_inline void emit_network_event(u8 event_type, u32 pid, u32 tgid, u32 uid,
                              u32 src_ip, u32 dst_ip, u16 src_port, u16 dst_port,
                              u8 protocol, u32 latency_us, u32 bytes, u16 error_code) {
    struct network_event *event;
    
    event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event)
        return;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = tgid;
    event->uid = uid;
    event->src_ip = src_ip;
    event->dst_ip = dst_ip;
    event->src_port = src_port;
    event->dst_port = dst_port;
    event->protocol = protocol;
    event->event_type = event_type;
    event->latency_us = latency_us;
    event->bytes = bytes;
    event->error_code = error_code;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_container_id(event->container_id);
    
    bpf_ringbuf_submit(event, 0);
}

// Track TCP connection establishment
SEC("kprobe/tcp_v4_connect")
int BPF_KPROBE(tcp_v4_connect_entry, struct sock *sk) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u64 start_time = bpf_ktime_get_ns();
    
    bpf_map_update_elem(&pid_start_time, &pid, &start_time, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/tcp_v4_connect")
int BPF_KRETPROBE(tcp_v4_connect_exit, int ret) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    u64 *start_time = bpf_map_lookup_elem(&pid_start_time, &pid);
    if (!start_time)
        return 0;
    
    u64 latency_ns = bpf_ktime_get_ns() - *start_time;
    u32 latency_us = latency_ns / 1000;
    
    bpf_map_delete_elem(&pid_start_time, &pid);
    
    if (ret == 0) {
        // Connection successful
        emit_network_event(NET_CONN_ESTABLISHED, pid, tgid, uid,
                          0, 0, 0, 0, 6, latency_us, 0, 0);
    } else {
        // Connection failed
        emit_network_event(NET_CONN_FAILED, pid, tgid, uid,
                          0, 0, 0, 0, 6, latency_us, 0, -ret);
    }
    
    return 0;
}

// Track TCP connection closure
SEC("kprobe/tcp_close")
int BPF_KPROBE(tcp_close_entry, struct sock *sk) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    struct inet_sock *inet = (struct inet_sock *)sk;
    u32 src_ip = 0, dst_ip = 0;
    u16 src_port = 0, dst_port = 0;
    
    // Read socket information
    BPF_CORE_READ_INTO(&src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&dst_port, inet, inet_dport);
    
    // Convert from network to host byte order
    src_port = bpf_ntohs(src_port);
    dst_port = bpf_ntohs(dst_port);
    
    emit_network_event(NET_CONN_CLOSED, pid, tgid, uid,
                      src_ip, dst_ip, src_port, dst_port, 6, 0, 0, 0);
    
    return 0;
}

// Track TCP retransmissions
SEC("kprobe/tcp_retransmit_skb")
int BPF_KPROBE(tcp_retransmit_entry, struct sock *sk, struct sk_buff *skb) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    struct inet_sock *inet = (struct inet_sock *)sk;
    u32 src_ip = 0, dst_ip = 0;
    u16 src_port = 0, dst_port = 0;
    
    // Read socket information
    BPF_CORE_READ_INTO(&src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&dst_port, inet, inet_dport);
    
    src_port = bpf_ntohs(src_port);
    dst_port = bpf_ntohs(dst_port);
    
    emit_network_event(NET_RETRANSMIT, pid, tgid, uid,
                      src_ip, dst_ip, src_port, dst_port, 6, 0, 0, 0);
    
    return 0;
}

// Track packet drops
SEC("tracepoint/skb/kfree_skb")
int trace_kfree_skb(struct trace_event_raw_kfree_skb *ctx) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    // Get drop reason from context
    enum skb_drop_reason reason = ctx->reason;
    
    // Only track network-related drops
    if (reason != SKB_DROP_REASON_NOT_SPECIFIED) {
        emit_network_event(NET_PACKET_DROP, pid, tgid, uid,
                          0, 0, 0, 0, 0, 0, 0, reason);
    }
    
    return 0;
}

// Track high network latency via TCP RTT
SEC("kprobe/tcp_rcv_established")
int BPF_KPROBE(tcp_rcv_established_entry, struct sock *sk, struct sk_buff *skb) {
    struct tcp_sock *tp = (struct tcp_sock *)sk;
    struct inet_sock *inet = (struct inet_sock *)sk;
    u32 srtt = 0;
    
    // Read smoothed RTT (in jiffies shifted left 3)
    BPF_CORE_READ_INTO(&srtt, tp, srtt_us);
    
    // If RTT is high (> 100ms), emit event
    if (srtt > 100000) { // 100ms in microseconds
        u64 id = bpf_get_current_pid_tgid();
        u32 pid = id >> 32;
        u32 tgid = id;
        u32 uid = bpf_get_current_uid_gid();
        
        u32 src_ip = 0, dst_ip = 0;
        u16 src_port = 0, dst_port = 0;
        
        BPF_CORE_READ_INTO(&src_ip, inet, inet_saddr);
        BPF_CORE_READ_INTO(&dst_ip, inet, inet_daddr);
        BPF_CORE_READ_INTO(&src_port, inet, inet_sport);
        BPF_CORE_READ_INTO(&dst_port, inet, inet_dport);
        
        src_port = bpf_ntohs(src_port);
        dst_port = bpf_ntohs(dst_port);
        
        emit_network_event(NET_HIGH_LATENCY, pid, tgid, uid,
                          src_ip, dst_ip, src_port, dst_port, 6, srtt, 0, 0);
    }
    
    return 0;
}

// Track UDP operations
SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(udp_sendmsg_entry, struct sock *sk, struct msghdr *msg, size_t len) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    struct inet_sock *inet = (struct inet_sock *)sk;
    u32 src_ip = 0, dst_ip = 0;
    u16 src_port = 0, dst_port = 0;
    
    BPF_CORE_READ_INTO(&src_ip, inet, inet_saddr);
    BPF_CORE_READ_INTO(&dst_ip, inet, inet_daddr);
    BPF_CORE_READ_INTO(&src_port, inet, inet_sport);
    BPF_CORE_READ_INTO(&dst_port, inet, inet_dport);
    
    src_port = bpf_ntohs(src_port);
    dst_port = bpf_ntohs(dst_port);
    
    // For UDP, we emit a connection event for each send
    emit_network_event(NET_CONN_ESTABLISHED, pid, tgid, uid,
                      src_ip, dst_ip, src_port, dst_port, 17, 0, len, 0);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";