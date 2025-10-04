// SPDX-License-Identifier: GPL-2.0
// CO-RE Status monitoring - tracks L7 failures, latency, and error patterns

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

// Error types for L7 status tracking
#define STATUS_OK               0
#define STATUS_ERROR_TIMEOUT    1
#define STATUS_ERROR_REFUSED    2
#define STATUS_ERROR_RESET      3
#define STATUS_ERROR_5XX        4
#define STATUS_ERROR_4XX        5
#define STATUS_ERROR_SLOW       6
#define STATUS_ERROR_PARTIAL    7

// Protocol types
#define PROTO_HTTP    1
#define PROTO_GRPC    2
#define PROTO_TCP     3

struct status_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 service_hash;
    __u32 endpoint_hash;
    __u32 latency_us;        // Latency in microseconds
    __u16 status_code;       // HTTP/gRPC status code
    __u16 error_type;        // Error classification
    __u16 protocol;          // Protocol type
    __u16 port;              // Service port
    __u32 src_ip;            // Source IP (client)
    __u32 dst_ip;            // Destination IP (server)
    char comm[16];           // Process name
} __attribute__((packed));

// Connection tracking for latency measurement
struct conn_info {
    __u64 start_time;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u16 protocol;
    __u8 state;
    __u8 _pad;
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} status_events SEC(".maps");

// Connection tracking map
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, __u64);  // socket pointer or connection ID
    __type(value, struct conn_info);
} conn_tracker SEC(".maps");

// Hash function for service/endpoint identification
static __always_inline __u32 djb2_hash(const char *str, int len)
{
    __u32 hash = 5381;

    #pragma unroll
    for (int i = 0; i < 64 && i < len; i++) {
        if (i >= len || str[i] == 0) break;
        hash = ((hash << 5) + hash) + str[i];
    }

    return hash;
}

// Parse HTTP response status code
static __always_inline int parse_http_status(const char *data, int len)
{
    // Look for "HTTP/1.x NNN" or "HTTP/2 NNN" pattern
    if (len < 12) return 0;

    // Check for HTTP prefix
    if (data[0] != 'H' || data[1] != 'T' || data[2] != 'T' || data[3] != 'P')
        return 0;

    // Find status code (after "HTTP/1.1 " or similar)
    int status = 0;
    int found_space = 0;

    #pragma unroll
    for (int i = 8; i < len && i < 16; i++) {
        if (data[i] == ' ') {
            found_space = 1;
            continue;
        }
        if (found_space && data[i] >= '0' && data[i] <= '9') {
            status = status * 10 + (data[i] - '0');
            if (status > 999) break; // Prevent overflow
        } else if (found_space) {
            break; // End of status code
        }
    }

    return status;
}

// Classify error type based on status code or other indicators
static __always_inline __u16 classify_error(int status_code, __u64 latency_us)
{
    if (status_code == 0 && latency_us > 30000000) { // > 30 seconds
        return STATUS_ERROR_TIMEOUT;
    }

    if (status_code >= 500) return STATUS_ERROR_5XX;
    if (status_code >= 400) return STATUS_ERROR_4XX;
    if (latency_us > 5000000) return STATUS_ERROR_SLOW; // > 5 seconds

    return STATUS_OK;
}

// Emit status event to ring buffer
static __always_inline void emit_status_event(__u32 service_hash, __u32 endpoint_hash,
                                              int status_code, __u64 latency_us,
                                              __u16 protocol, __u32 src_ip, __u32 dst_ip, __u16 port)
{
    struct status_event *event;

    event = bpf_ringbuf_reserve(&status_events, sizeof(*event), 0);
    if (!event) return;

    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event->service_hash = service_hash;
    event->endpoint_hash = endpoint_hash;
    event->latency_us = (__u32)(latency_us / 1000); // Convert ns to us
    event->status_code = (__u16)status_code;
    event->error_type = classify_error(status_code, latency_us);
    event->protocol = protocol;
    event->port = port;
    event->src_ip = src_ip;
    event->dst_ip = dst_ip;

    bpf_get_current_comm(event->comm, sizeof(event->comm));

    bpf_ringbuf_submit(event, 0);
}

// Track TCP connection establishment for latency measurement
SEC("kprobe/tcp_v4_connect")
int trace_tcp_connect(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;

    struct conn_info conn = {};
    conn.start_time = bpf_ktime_get_ns();
    conn.protocol = PROTO_TCP;
    conn.state = 1; // Connecting

    // Extract connection details using CO-RE
    struct inet_sock *inet = (struct inet_sock *)sk;
    if (bpf_core_field_exists(inet->inet_daddr)) {
        BPF_CORE_READ_INTO(&conn.dst_ip, inet, inet_daddr);
    }
    if (bpf_core_field_exists(inet->inet_saddr)) {
        BPF_CORE_READ_INTO(&conn.src_ip, inet, inet_saddr);
    }
    if (bpf_core_field_exists(inet->inet_dport)) {
        BPF_CORE_READ_INTO(&conn.dst_port, inet, inet_dport);
        conn.dst_port = bpf_ntohs(conn.dst_port);
    }
    if (bpf_core_field_exists(inet->inet_sport)) {
        BPF_CORE_READ_INTO(&conn.src_port, inet, inet_sport);
        conn.src_port = bpf_ntohs(conn.src_port);
    }

    __u64 sk_key = (__u64)sk;
    bpf_map_update_elem(&conn_tracker, &sk_key, &conn, BPF_ANY);

    return 0;
}

// Track connection close to detect resets and calculate latency
SEC("kprobe/tcp_close")
int trace_tcp_close(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;

    __u64 sk_key = (__u64)sk;
    struct conn_info *conn = bpf_map_lookup_elem(&conn_tracker, &sk_key);
    if (!conn) return 0;

    __u64 now = bpf_ktime_get_ns();
    __u64 duration = now - conn->start_time;

    // Check for suspicious quick closes (potential resets)
    if (duration < 100000000) { // Less than 100ms
        __u32 service_hash = conn->dst_ip; // Simple hash for now
        emit_status_event(service_hash, 0, 0, duration,
                         PROTO_TCP, conn->src_ip, conn->dst_ip, conn->dst_port);
    }

    bpf_map_delete_elem(&conn_tracker, &sk_key);
    return 0;
}

// Parse HTTP responses for status codes (simplified uprobe approach)
SEC("uprobe/http_response")
int trace_http_response(struct pt_regs *ctx)
{
    // This would attach to HTTP library functions like curl_easy_perform
    // or Go's http.Client.Do for more accurate L7 monitoring

    void *response_ptr = (void *)PT_REGS_PARM1(ctx);
    if (!response_ptr) return 0;

    // For demonstration - in practice, this would read from the HTTP response
    char http_data[128];
    long ret = bpf_probe_read_user(http_data, sizeof(http_data), response_ptr);
    if (ret < 0) return 0;

    int status_code = parse_http_status(http_data, sizeof(http_data));
    if (status_code > 0) {
        __u32 service_hash = djb2_hash("example.com", 11); // Placeholder
        __u32 endpoint_hash = djb2_hash("/api/v1/status", 15); // Placeholder

        emit_status_event(service_hash, endpoint_hash, status_code, 0,
                         PROTO_HTTP, 0, 0, 80);
    }

    return 0;
}

// Socket filter for HTTP traffic analysis (DISABLED - verifier issues)
// SEC("socket/http_filter")
// int socket_http_monitor(struct __sk_buff *skb)
// {
//     // DISABLED: Kernel verifier issues with __sk_buff field access
//     return 0;
// }

char _license[] SEC("license") = "GPL";