#include <linux/bpf.h>
#include <linux/in.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/ptrace.h>
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
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol; // TCP=6, UDP=17
};

struct connection_stats {
    __u64 start_time;
    __u64 last_seen;
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u64 packets_sent;
    __u64 packets_recv;
    __u64 retransmits;
    __u32 rtt_min;
    __u32 rtt_max;
    __u32 rtt_avg;
    __u8 state;
    __u8 failed;
};

// Network event structure
struct network_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 uid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 event_type;
    __u32 latency_us;
    __u32 bytes;
    __u16 error_code;
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
    __type(key, __u32);
    __type(value, __u64);
} pid_start_time SEC(".maps");

// Helper function to extract container ID from cgroup
static int extract_container_id(char *container_id) {
    struct task_struct *task;
    struct css_set *cgroups;
    char *cgroup_path;
    
    task = (struct task_struct *)bpf_get_current_task();
    if (!task)
        return -1;
    
    // This is a simplified version - in reality we'd need to traverse
    // the cgroup hierarchy and extract the container ID from the path
    __builtin_memset(container_id, 0, 64);
    
    // For now, just mark it as unknown
    bpf_probe_read_str(container_id, 8, "unknown");
    
    return 0;
}

// Helper function to get connection key
static void get_connection_key(struct connection_key *key, __u32 pid,
                              __u32 src_ip, __u32 dst_ip, 
                              __u16 src_port, __u16 dst_port, __u8 protocol) {
    key->pid = pid;
    key->src_ip = src_ip;
    key->dst_ip = dst_ip;
    key->src_port = src_port;
    key->dst_port = dst_port;
    key->protocol = protocol;
}

// Helper function to emit network event
static void emit_network_event(__u8 event_type, __u32 pid, __u32 tgid, __u32 uid,
                              __u32 src_ip, __u32 dst_ip, __u16 src_port, __u16 dst_port,
                              __u8 protocol, __u32 latency_us, __u32 bytes, __u16 error_code) {
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
int tcp_v4_connect_entry(struct pt_regs *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u64 start_time = bpf_ktime_get_ns();
    
    bpf_map_update_elem(&pid_start_time, &pid, &start_time, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/tcp_v4_connect")
int tcp_v4_connect_exit(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    int ret = PT_REGS_RC(ctx);
    
    __u64 *start_time = bpf_map_lookup_elem(&pid_start_time, &pid);
    if (!start_time)
        return 0;
    
    __u64 latency_ns = bpf_ktime_get_ns() - *start_time;
    __u32 latency_us = latency_ns / 1000;
    
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
int tcp_close(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    emit_network_event(NET_CONN_CLOSED, pid, tgid, uid,
                      0, 0, 0, 0, 6, 0, 0, 0);
    
    return 0;
}

// Track TCP retransmissions
SEC("kprobe/tcp_retransmit_skb")
int tcp_retransmit_skb(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    emit_network_event(NET_RETRANSMIT, pid, tgid, uid,
                      0, 0, 0, 0, 6, 0, 0, 0);
    
    return 0;
}

// Track packet drops
SEC("tracepoint/skb/kfree_skb")
int trace_kfree_skb(struct trace_event_raw_kfree_skb *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    // Check if this is a network-related drop
    void *location = (void *)ctx->location;
    
    emit_network_event(NET_PACKET_DROP, pid, tgid, uid,
                      0, 0, 0, 0, 0, 0, 0, 0);
    
    return 0;
}

// Track high network latency
SEC("kprobe/tcp_rcv_established")
int tcp_rcv_established(struct pt_regs *ctx) {
    // This would track when packets are received and calculate latency
    // Implementation would involve tracking packet timestamps
    
    return 0;
}

// Track UDP operations
SEC("kprobe/udp_sendmsg")
int udp_sendmsg(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    // Track UDP send operations
    emit_network_event(NET_CONN_ESTABLISHED, pid, tgid, uid,
                      0, 0, 0, 0, 17, 0, 0, 0);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";