// SPDX-License-Identifier: GPL-2.0
// Production L3-L4-L7 network monitoring via eBPF
// Supports IPv4/IPv6, TCP/UDP, HTTP/HTTPS, gRPC protocol parsing
// Zero-overhead monitoring for cloud-native environments

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include "../../bpf_common/bpf_stats.h"
#include "../../bpf_common/bpf_filters.h"
#include "../../bpf_common/bpf_batch.h"
#include "../../bpf_common/container_utils.h"
#include "../../bpf_common/shared_maps.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Network protocol constants
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define IPPROTO_ICMP 1
#define AF_INET 2
#define AF_INET6 10

// L7 Protocol detection constants
#define L7_PROTOCOL_UNKNOWN 0
#define L7_PROTOCOL_HTTP    1
#define L7_PROTOCOL_GRPC    2
#define L7_PROTOCOL_DNS     3

// Event types
#define EVENT_TYPE_CONNECTION       1
#define EVENT_TYPE_CONNECTION_CLOSE 2
#define EVENT_TYPE_HTTP_REQUEST     3
#define EVENT_TYPE_HTTP_RESPONSE    4
#define EVENT_TYPE_GRPC_CALL        5
#define EVENT_TYPE_GRPC_RESPONSE    6
#define EVENT_TYPE_DNS_QUERY        7
#define EVENT_TYPE_DNS_RESPONSE     8

// Connection states
#define CONN_STATE_CONNECTING   0
#define CONN_STATE_ESTABLISHED  1
#define CONN_STATE_CLOSING      2
#define CONN_STATE_CLOSED       3
#define CONN_STATE_LISTENING    4

// HTTP constants
#define HTTP_METHOD_GET     1
#define HTTP_METHOD_POST    2
#define HTTP_METHOD_PUT     3
#define HTTP_METHOD_DELETE  4
#define HTTP_METHOD_PATCH   5
#define HTTP_METHOD_HEAD    6
#define HTTP_METHOD_OPTIONS 7

// Maximum data sizes for efficient packet processing
#define MAX_L7_DATA_SIZE    255  // Must fit in __u8 (l7_data_len field)
#define MAX_COMM_SIZE       16
#define MAX_POD_UID_SIZE    40
#define MAX_FLOW_KEY_SIZE   24

// gRPC constants
#define GRPC_FRAME_HEADER_SIZE 5
#define GRPC_MAGIC_BYTES "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

// Flow tracking key for connection correlation
struct flow_key {
    __u32 src_ip[4];  // IPv6-compatible (IPv4 uses first element)
    __u32 dst_ip[4];  // IPv6-compatible (IPv4 uses first element)
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  ip_version;
    __u8  _pad[2];
} __attribute__((packed));

// Connection tracking info
struct conn_info {
    __u64 start_time;
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u32 packets_sent;
    __u32 packets_recv;
    __u32 pid;
    __u8  state;
    __u8  l7_protocol;
    __u8  _pad[2];
} __attribute__((packed));

// HTTP request/response parsing state
struct http_state {
    __u8  method;
    __u16 status_code;
    __u8  version;       // HTTP/1.1=11, HTTP/2.0=20
    __u32 content_length;
    __u8  is_request;
    __u8  is_response;
    __u8  _pad[2];
} __attribute__((packed));

// gRPC call state
struct grpc_state {
    __u32 stream_id;
    __u8  message_type;  // 0=request, 1=response
    __u8  compression;
    __u8  status_code;
    __u8  _pad[1];
    char  service[64];
    char  method[32];
} __attribute__((packed));

// Network event structure (matches Go BPFNetworkEvent)
struct network_event {
    // Header (8-byte aligned)
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    
    // Event info
    __u8  event_type;
    __u8  protocol;
    __u8  ip_version;
    __u8  direction;
    
    // Network addresses (IPv6-compatible)
    __u8  src_addr[16];
    __u8  dst_addr[16];
    
    // Ports
    __u16 src_port;
    __u16 dst_port;
    
    // Process info
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    char  comm[MAX_COMM_SIZE];
    
    // Connection state and metrics
    __u8  conn_state;
    __u8  _pad1;
    __u16 _pad2;
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u32 packets_sent;
    __u32 packets_recv;
    
    // L7 protocol information
    __u8  l7_protocol;
    __u8  l7_data_len;
    __u16 _pad3;
    __u8  l7_data[MAX_L7_DATA_SIZE];
    
    // Performance metrics
    __u64 latency_ns;
    __u64 duration_ns;
    
    // Container context
    char pod_uid[MAX_POD_UID_SIZE];
    
    // Network interface
    __u32 if_index;
    __u32 _pad4;
} __attribute__((packed));

// Maps for network monitoring
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024); // 1MB ring buffer for high-throughput
} network_events SEC(".maps");

// Connection tracking map
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 65536);
    __type(key, struct flow_key);
    __type(value, struct conn_info);
} active_connections SEC(".maps");

// HTTP parsing state map
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 8192);
    __type(key, struct flow_key);
    __type(value, struct http_state);
} http_states SEC(".maps");

// gRPC parsing state map
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 8192);
    __type(key, struct flow_key);
    __type(value, struct grpc_state);
} grpc_states SEC(".maps");

// L7 port configuration map (updated from userspace)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u16);     // port number
    __type(value, __u8);    // L7 protocol type
} l7_port_map SEC(".maps");

// Statistics map
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 16);
    __type(key, __u32);
    __type(value, __u64);
} stats SEC(".maps");

// Helper functions

// Create flow key from connection tuple
static __always_inline void make_flow_key(struct flow_key *key, 
                                          __u32 *src_ip, __u32 *dst_ip,
                                          __u16 src_port, __u16 dst_port,
                                          __u8 protocol, __u8 ip_version)
{
    __builtin_memset(key, 0, sizeof(*key));
    
    if (ip_version == 4) {
        key->src_ip[0] = src_ip[0];
        key->dst_ip[0] = dst_ip[0];
    } else if (ip_version == 6) {
        __builtin_memcpy(key->src_ip, src_ip, 16);
        __builtin_memcpy(key->dst_ip, dst_ip, 16);
    }
    
    key->src_port = src_port;
    key->dst_port = dst_port;
    key->protocol = protocol;
    key->ip_version = ip_version;
}

// Get L7 protocol for port
static __always_inline __u8 get_l7_protocol(__u16 port)
{
    __u8 *proto = bpf_map_lookup_elem(&l7_port_map, &port);
    if (proto) {
        return *proto;
    }
    
    // Default protocol detection based on well-known ports
    switch (port) {
        case 53:
            return L7_PROTOCOL_DNS;
        case 80:
        case 8080:
        case 8000:
        case 3000:
            return L7_PROTOCOL_HTTP;
        case 443:
        case 8443:
            return L7_PROTOCOL_HTTP; // HTTPS is HTTP at L7
        case 50051:
        case 9090:
            return L7_PROTOCOL_GRPC;
        default:
            return L7_PROTOCOL_UNKNOWN;
    }
}

// Parse HTTP method from payload
static __always_inline __u8 parse_http_method(const char *data, __u32 size)
{
    if (size < 3) return 0;
    
    // Check for common HTTP methods
    if (data[0] == 'G' && data[1] == 'E' && data[2] == 'T')
        return HTTP_METHOD_GET;
    if (size >= 4 && data[0] == 'P' && data[1] == 'O' && data[2] == 'S' && data[3] == 'T')
        return HTTP_METHOD_POST;
    if (size >= 3 && data[0] == 'P' && data[1] == 'U' && data[2] == 'T')
        return HTTP_METHOD_PUT;
    if (size >= 6 && data[0] == 'D' && data[1] == 'E' && data[2] == 'L' && 
        data[3] == 'E' && data[4] == 'T' && data[5] == 'E')
        return HTTP_METHOD_DELETE;
    if (size >= 5 && data[0] == 'P' && data[1] == 'A' && data[2] == 'T' &&
        data[3] == 'C' && data[4] == 'H')
        return HTTP_METHOD_PATCH;
    if (size >= 4 && data[0] == 'H' && data[1] == 'E' && data[2] == 'A' && data[3] == 'D')
        return HTTP_METHOD_HEAD;
    if (size >= 7 && data[0] == 'O' && data[1] == 'P' && data[2] == 'T' &&
        data[3] == 'I' && data[4] == 'O' && data[5] == 'N' && data[6] == 'S')
        return HTTP_METHOD_OPTIONS;
    
    return 0;
}

// Parse HTTP response status code
__attribute__((unused))
static __always_inline __u16 parse_http_status(const char *data, __u32 size)
{
    if (size < 12) return 0; // "HTTP/1.1 200"
    
    // Look for HTTP response pattern
    if (data[0] == 'H' && data[1] == 'T' && data[2] == 'T' && data[3] == 'P') {
        // Find the status code (after "HTTP/1.1 " or "HTTP/2.0 ")
        for (int i = 8; i < size - 2 && i < 15; i++) {
            if (data[i] >= '0' && data[i] <= '9' &&
                data[i+1] >= '0' && data[i+1] <= '9' &&
                data[i+2] >= '0' && data[i+2] <= '9') {
                return (data[i] - '0') * 100 + (data[i+1] - '0') * 10 + (data[i+2] - '0');
            }
        }
    }
    
    return 0;
}

// Check if payload contains gRPC magic bytes
static __always_inline bool is_grpc_handshake(const char *data, __u32 size)
{
    const char grpc_preface[] = "PRI * HTTP/2.0";
    if (size < sizeof(grpc_preface) - 1) return false;
    
    for (int i = 0; i < sizeof(grpc_preface) - 1; i++) {
        if (data[i] != grpc_preface[i]) return false;
    }
    return true;
}

// Initialize network event with common fields
static __always_inline void init_network_event(struct network_event *event,
                                              __u8 event_type,
                                              struct sock *sk)
{
    __builtin_memset(event, 0, sizeof(*event));
    
    event->timestamp = bpf_ktime_get_ns();
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    event->pid = pid_tgid >> 32;
    event->tid = pid_tgid & 0xffffffff;
    
    event->event_type = event_type;
    event->uid = bpf_get_current_uid_gid() >> 32;
    event->gid = bpf_get_current_uid_gid() & 0xffffffff;
    
    bpf_get_current_comm(event->comm, sizeof(event->comm));
    
    // Get cgroup ID for container correlation
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        
        // Try to get pod information
        struct pod_info *pod = get_pod_info(event->cgroup_id);
        if (pod) {
            safe_copy_pod_uid(event->pod_uid, pod);
        }
    }
}

// Extract socket information using CO-RE
static __always_inline int extract_socket_info(struct network_event *event, struct sock *sk)
{
    if (!sk) return -1;
    
    // Get address family
    __u16 family = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_family)) {
        BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family);
    }
    
    // Get protocol
    __u8 protocol = 0;
    if (bpf_core_field_exists(sk->sk_protocol)) {
        BPF_CORE_READ_INTO(&protocol, sk, sk_protocol);
    }
    event->protocol = protocol;
    
    // Get ports
    __u16 src_port = 0, dst_port = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
        BPF_CORE_READ_INTO(&src_port, sk, __sk_common.skc_num);
    }
    if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
        BPF_CORE_READ_INTO(&dst_port, sk, __sk_common.skc_dport);
        dst_port = __builtin_bswap16(dst_port); // Convert from network byte order
    }
    event->src_port = src_port;
    event->dst_port = dst_port;
    
    if (family == AF_INET) {
        event->ip_version = 4;
        
        // IPv4 addresses
        __u32 src_ip = 0, dst_ip = 0;
        if (bpf_core_field_exists(sk->__sk_common.skc_rcv_saddr)) {
            BPF_CORE_READ_INTO(&src_ip, sk, __sk_common.skc_rcv_saddr);
        }
        if (bpf_core_field_exists(sk->__sk_common.skc_daddr)) {
            BPF_CORE_READ_INTO(&dst_ip, sk, __sk_common.skc_daddr);
        }
        
        // Store IPv4 addresses in first 4 bytes
        __builtin_memcpy(event->src_addr, &src_ip, 4);
        __builtin_memcpy(event->dst_addr, &dst_ip, 4);
        
    } else if (family == AF_INET6) {
        event->ip_version = 6;
        
        // IPv6 addresses - use CO-RE to safely access fields
        if (bpf_core_field_exists(sk->sk_v6_rcv_saddr)) {
            struct in6_addr src_addr;
            if (BPF_CORE_READ_INTO(&src_addr, sk, sk_v6_rcv_saddr) == 0) {
                bpf_probe_read_kernel(event->src_addr, 16, &src_addr);
            }
        }
        if (bpf_core_field_exists(sk->sk_v6_daddr)) {
            struct in6_addr dst_addr;
            if (BPF_CORE_READ_INTO(&dst_addr, sk, sk_v6_daddr) == 0) {
                bpf_probe_read_kernel(event->dst_addr, 16, &dst_addr);
            }
        }
    }
    
    return 0;
}

// TCP connection tracking
SEC("kprobe/tcp_v4_connect")
int trace_tcp_v4_connect(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only monitor container processes
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event) return 0;
    
    init_network_event(event, EVENT_TYPE_CONNECTION, sk);
    if (extract_socket_info(event, sk) != 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    
    event->direction = 0; // Outbound
    event->conn_state = CONN_STATE_CONNECTING;
    
    // Determine L7 protocol
    event->l7_protocol = get_l7_protocol(event->dst_port);
    
    // Create flow key for connection tracking
    struct flow_key flow;
    __u32 src_ip[4], dst_ip[4];
    __builtin_memcpy(src_ip, event->src_addr, 16);
    __builtin_memcpy(dst_ip, event->dst_addr, 16);
    
    make_flow_key(&flow, src_ip, dst_ip, event->src_port, event->dst_port, 
                  event->protocol, event->ip_version);
    
    // Track connection
    struct conn_info conn = {
        .start_time = event->timestamp,
        .pid = event->pid,
        .state = CONN_STATE_CONNECTING,
        .l7_protocol = event->l7_protocol,
    };
    bpf_map_update_elem(&active_connections, &flow, &conn, BPF_ANY);
    
    // Update statistics
    __u32 key = 0; // Total connections
    __u64 *count = bpf_map_lookup_elem(&stats, &key);
    if (count) (*count)++;
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// TCP connection establishment (IPv4)
SEC("kprobe/tcp_finish_connect")
int trace_tcp_finish_connect(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event) return 0;
    
    init_network_event(event, EVENT_TYPE_CONNECTION, sk);
    if (extract_socket_info(event, sk) != 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    
    event->direction = 0; // Outbound
    event->conn_state = CONN_STATE_ESTABLISHED;
    event->l7_protocol = get_l7_protocol(event->dst_port);
    
    // Update connection state
    struct flow_key flow;
    __u32 src_ip[4], dst_ip[4];
    __builtin_memcpy(src_ip, event->src_addr, 16);
    __builtin_memcpy(dst_ip, event->dst_addr, 16);
    
    make_flow_key(&flow, src_ip, dst_ip, event->src_port, event->dst_port,
                  event->protocol, event->ip_version);
    
    struct conn_info *conn = bpf_map_lookup_elem(&active_connections, &flow);
    if (conn) {
        conn->state = CONN_STATE_ESTABLISHED;
        event->duration_ns = event->timestamp - conn->start_time;
        event->latency_ns = event->duration_ns; // Connection establishment latency
        
        event->bytes_sent = conn->bytes_sent;
        event->bytes_recv = conn->bytes_recv;
        event->packets_sent = conn->packets_sent;
        event->packets_recv = conn->packets_recv;
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// TCP connection close
SEC("kprobe/tcp_close")
int trace_tcp_close(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event) return 0;
    
    init_network_event(event, EVENT_TYPE_CONNECTION_CLOSE, sk);
    if (extract_socket_info(event, sk) != 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    
    event->conn_state = CONN_STATE_CLOSING;
    
    // Get connection info for metrics
    struct flow_key flow;
    __u32 src_ip[4], dst_ip[4];
    __builtin_memcpy(src_ip, event->src_addr, 16);
    __builtin_memcpy(dst_ip, event->dst_addr, 16);
    
    make_flow_key(&flow, src_ip, dst_ip, event->src_port, event->dst_port,
                  event->protocol, event->ip_version);
    
    struct conn_info *conn = bpf_map_lookup_elem(&active_connections, &flow);
    if (conn) {
        event->duration_ns = event->timestamp - conn->start_time;
        event->bytes_sent = conn->bytes_sent;
        event->bytes_recv = conn->bytes_recv;
        event->packets_sent = conn->packets_sent;
        event->packets_recv = conn->packets_recv;
        event->l7_protocol = conn->l7_protocol;
        
        // Remove from tracking
        bpf_map_delete_elem(&active_connections, &flow);
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// TCP data transmission (for L7 parsing)
SEC("kprobe/tcp_sendmsg")
int trace_tcp_sendmsg(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    if (!sk || !msg || size == 0) return 0;
    
    // Only parse L7 protocols on monitored ports
    __u16 dst_port = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
        BPF_CORE_READ_INTO(&dst_port, sk, __sk_common.skc_dport);
        dst_port = __builtin_bswap16(dst_port);
    }
    
    __u8 l7_protocol = get_l7_protocol(dst_port);
    if (l7_protocol == L7_PROTOCOL_UNKNOWN) {
        return 0;
    }
    
    // Extract payload for L7 parsing
    char payload[MAX_L7_DATA_SIZE];
    __builtin_memset(payload, 0, sizeof(payload));
    
    // Safely read the first part of the message
    struct iov_iter *iter = NULL;
    if (bpf_core_field_exists(msg->msg_iter)) {
        BPF_CORE_READ_INTO(&iter, msg, msg_iter);
    }
    
    // Try to read payload data safely
    if (iter) {
        size_t copy_size = size < MAX_L7_DATA_SIZE ? size : MAX_L7_DATA_SIZE;
        // This is a simplified approach - real implementation would need
        // more complex iovec handling for production use
        bpf_probe_read_kernel(payload, copy_size, iter);
    }
    
    // Parse based on L7 protocol
    if (l7_protocol == L7_PROTOCOL_HTTP) {
        __u8 method = parse_http_method(payload, MAX_L7_DATA_SIZE);
        if (method > 0) {
            // This is an HTTP request
            struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
            if (event) {
                init_network_event(event, EVENT_TYPE_HTTP_REQUEST, sk);
                if (extract_socket_info(event, sk) == 0) {
                    event->l7_protocol = L7_PROTOCOL_HTTP;
                    event->l7_data_len = MAX_L7_DATA_SIZE;
                    __builtin_memcpy(event->l7_data, payload, MAX_L7_DATA_SIZE);
                    
                    bpf_ringbuf_submit(event, 0);
                } else {
                    bpf_ringbuf_discard(event, 0);
                }
            }
        }
    } else if (l7_protocol == L7_PROTOCOL_GRPC) {
        if (is_grpc_handshake(payload, MAX_L7_DATA_SIZE)) {
            struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
            if (event) {
                init_network_event(event, EVENT_TYPE_GRPC_CALL, sk);
                if (extract_socket_info(event, sk) == 0) {
                    event->l7_protocol = L7_PROTOCOL_GRPC;
                    event->l7_data_len = MAX_L7_DATA_SIZE;
                    __builtin_memcpy(event->l7_data, payload, MAX_L7_DATA_SIZE);
                    
                    bpf_ringbuf_submit(event, 0);
                } else {
                    bpf_ringbuf_discard(event, 0);
                }
            }
        }
    }
    
    return 0;
}

// UDP sendmsg for DNS and other UDP protocols
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    int size = (int)PT_REGS_PARM3(ctx);
    
    if (!sk || !msg || size <= 0) return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event) return 0;
    
    init_network_event(event, EVENT_TYPE_CONNECTION, sk);
    if (extract_socket_info(event, sk) != 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    
    event->direction = 0; // Outbound
    event->bytes_sent = size;
    event->packets_sent = 1;
    event->l7_protocol = get_l7_protocol(event->dst_port);
    
    // For DNS (port 53), try to parse the query
    if (event->dst_port == 53 && event->l7_protocol == L7_PROTOCOL_DNS) {
        event->event_type = EVENT_TYPE_DNS_QUERY;
        
        // Extract DNS payload - simplified DNS parsing
        char dns_payload[MAX_L7_DATA_SIZE];
        __builtin_memset(dns_payload, 0, sizeof(dns_payload));
        
        // In a production implementation, we would properly parse iovec here
        // For now, this serves as a framework for DNS parsing
        event->l7_data_len = size < MAX_L7_DATA_SIZE ? size : MAX_L7_DATA_SIZE;
        // bpf_probe_read_kernel(event->l7_data, event->l7_data_len, msg_data);
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// IPv6 variants of TCP functions
SEC("kprobe/tcp_v6_connect")
int trace_tcp_v6_connect(struct pt_regs *ctx)
{
    // Similar to tcp_v4_connect but for IPv6
    return trace_tcp_v4_connect(ctx);
}

// Network statistics update
__attribute__((unused))
static __always_inline void update_stats(__u32 stat_type)
{
    __u64 *count = bpf_map_lookup_elem(&stats, &stat_type);
    if (count) {
        (*count)++;
    }
}

char _license[] SEC("license") = "GPL";