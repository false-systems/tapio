//go:build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_tracing.h>

// Connection event structure
struct connection_event {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  event_type; // 0=new, 1=close
    __u64 timestamp;
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u32 pid;
    __u32 uid;
    char  comm[16];
};

// Connection state tracking
struct connection_state {
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u64 start_time;
    __u32 pid;
    __u32 uid;
};

// Connection key for map lookups
struct connection_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  pad[3];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 100000);
    __type(key, struct connection_key);
    __type(value, struct connection_state);
} connections SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Helper to get current timestamp
static __always_inline __u64 get_timestamp() {
    return bpf_ktime_get_ns();
}

// Helper to extract connection info from socket
static __always_inline int extract_connection_info(struct sock *sk, struct connection_key *key) {
    // Get IP addresses and ports
    BPF_CORE_READ_INTO(&key->src_ip, sk, __sk_common.skc_rcv_saddr);
    BPF_CORE_READ_INTO(&key->dst_ip, sk, __sk_common.skc_daddr);
    BPF_CORE_READ_INTO(&key->src_port, sk, __sk_common.skc_num);
    BPF_CORE_READ_INTO(&key->dst_port, sk, __sk_common.skc_dport);
    
    // Convert port to host byte order
    key->dst_port = bpf_ntohs(key->dst_port);
    
    // Get protocol (6=TCP, 17=UDP)
    __u16 family = 0;
    BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family);
    
    if (family == AF_INET) {
        // Check if TCP or UDP
        __u8 protocol = 0;
        BPF_CORE_READ_INTO(&protocol, sk, sk_protocol);
        key->protocol = protocol;
        return 0;
    }
    
    return -1;
}

// Track TCP connection establishment
SEC("kprobe/tcp_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    
    // Only track TCP
    key.protocol = IPPROTO_TCP;
    
    // Get process info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    
    // Create connection state
    struct connection_state state = {
        .bytes_sent = 0,
        .bytes_recv = 0,
        .start_time = get_timestamp(),
        .pid = pid,
        .uid = uid,
    };
    
    // Store in map
    bpf_map_update_elem(&connections, &key, &state, BPF_ANY);
    
    // Send event
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        event->src_ip = key.src_ip;
        event->dst_ip = key.dst_ip;
        event->src_port = key.src_port;
        event->dst_port = key.dst_port;
        event->protocol = key.protocol;
        event->event_type = 0; // New connection
        event->timestamp = state.start_time;
        event->bytes_sent = 0;
        event->bytes_recv = 0;
        event->pid = pid;
        event->uid = uid;
        bpf_get_current_comm(event->comm, sizeof(event->comm));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

// Track TCP accept (server side)
SEC("kprobe/inet_csk_accept")
int trace_tcp_accept(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    
    // Swap source and destination for server perspective
    __u32 tmp_ip = key.src_ip;
    key.src_ip = key.dst_ip;
    key.dst_ip = tmp_ip;
    
    __u16 tmp_port = key.src_port;
    key.src_port = key.dst_port;
    key.dst_port = tmp_port;
    
    key.protocol = IPPROTO_TCP;
    
    // Get process info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    
    // Create connection state
    struct connection_state state = {
        .bytes_sent = 0,
        .bytes_recv = 0,
        .start_time = get_timestamp(),
        .pid = pid,
        .uid = uid,
    };
    
    // Store in map
    bpf_map_update_elem(&connections, &key, &state, BPF_ANY);
    
    // Send event
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        event->src_ip = key.src_ip;
        event->dst_ip = key.dst_ip;
        event->src_port = key.src_port;
        event->dst_port = key.dst_port;
        event->protocol = key.protocol;
        event->event_type = 0; // New connection
        event->timestamp = state.start_time;
        event->bytes_sent = 0;
        event->bytes_recv = 0;
        event->pid = pid;
        event->uid = uid;
        bpf_get_current_comm(event->comm, sizeof(event->comm));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

// Track data transfer
SEC("kprobe/tcp_sendmsg")
int trace_tcp_sendmsg(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    if (!sk || size == 0) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    key.protocol = IPPROTO_TCP;
    
    // Update bytes sent
    struct connection_state *state = bpf_map_lookup_elem(&connections, &key);
    if (state) {
        __sync_fetch_and_add(&state->bytes_sent, size);
    }
    
    return 0;
}

// Track received data
SEC("kprobe/tcp_cleanup_rbuf")
int trace_tcp_cleanup_rbuf(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    int copied = (int)PT_REGS_PARM2(ctx);
    
    if (!sk || copied <= 0) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    key.protocol = IPPROTO_TCP;
    
    // Update bytes received
    struct connection_state *state = bpf_map_lookup_elem(&connections, &key);
    if (state) {
        __sync_fetch_and_add(&state->bytes_recv, copied);
    }
    
    return 0;
}

// Track connection close
SEC("kprobe/tcp_close")
int trace_tcp_close(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    key.protocol = IPPROTO_TCP;
    
    // Get final state
    struct connection_state *state = bpf_map_lookup_elem(&connections, &key);
    if (!state) return 0;
    
    // Send close event
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        event->src_ip = key.src_ip;
        event->dst_ip = key.dst_ip;
        event->src_port = key.src_port;
        event->dst_port = key.dst_port;
        event->protocol = key.protocol;
        event->event_type = 1; // Close connection
        event->timestamp = get_timestamp();
        event->bytes_sent = state->bytes_sent;
        event->bytes_recv = state->bytes_recv;
        event->pid = state->pid;
        event->uid = state->uid;
        bpf_get_current_comm(event->comm, sizeof(event->comm));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    // Remove from map
    bpf_map_delete_elem(&connections, &key);
    
    return 0;
}

// Track UDP send
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    if (!sk || size == 0) return 0;
    
    struct connection_key key = {};
    if (extract_connection_info(sk, &key) < 0) {
        return 0;
    }
    key.protocol = IPPROTO_UDP;
    
    // For UDP, we track each send as a separate event
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    
    struct connection_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (event) {
        event->src_ip = key.src_ip;
        event->dst_ip = key.dst_ip;
        event->src_port = key.src_port;
        event->dst_port = key.dst_port;
        event->protocol = key.protocol;
        event->event_type = 0; // UDP packet
        event->timestamp = get_timestamp();
        event->bytes_sent = size;
        event->bytes_recv = 0;
        event->pid = pid;
        event->uid = uid;
        bpf_get_current_comm(event->comm, sizeof(event->comm));
        
        bpf_ringbuf_submit(event, 0);
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";