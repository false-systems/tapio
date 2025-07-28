#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

struct http_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u64 duration_ns;
    u16 status_code;
    u8 method;
    u8 version;
    char uri[64];
    char host[32];
    char container_id[64];
    char namespace[32];
    char pod_name[64];
    u64 request_size;
    u64 response_size;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
};

#define HTTP_GET     1
#define HTTP_POST    2
#define HTTP_PUT     3
#define HTTP_DELETE  4
#define HTTP_HEAD    5
#define HTTP_OPTIONS 6
#define HTTP_PATCH   7

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, u64);
    __type(value, struct http_event);
} http_requests SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u32));
} http_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, u32);
    __type(value, char[64]);
} pid_to_container SEC(".maps");

static __always_inline u8 parse_http_method(const char *data)
{
    if (data[0] == 'G' && data[1] == 'E' && data[2] == 'T')
        return HTTP_GET;
    if (data[0] == 'P' && data[1] == 'O' && data[2] == 'S' && data[3] == 'T')
        return HTTP_POST;
    if (data[0] == 'P' && data[1] == 'U' && data[2] == 'T')
        return HTTP_PUT;
    if (data[0] == 'D' && data[1] == 'E' && data[2] == 'L' && data[3] == 'E' && data[4] == 'T' && data[5] == 'E')
        return HTTP_DELETE;
    if (data[0] == 'H' && data[1] == 'E' && data[2] == 'A' && data[3] == 'D')
        return HTTP_HEAD;
    if (data[0] == 'O' && data[1] == 'P' && data[2] == 'T' && data[3] == 'I' && data[4] == 'O' && data[5] == 'N' && data[6] == 'S')
        return HTTP_OPTIONS;
    if (data[0] == 'P' && data[1] == 'A' && data[2] == 'T' && data[3] == 'C' && data[4] == 'H')
        return HTTP_PATCH;
    return 0;
}

static __always_inline bool is_http_request(const char *data, int len)
{
    if (len < 10)
        return false;
    
    u8 method = parse_http_method(data);
    if (method == 0)
        return false;
    
    // Check for space after method
    int pos = 0;
    switch (method) {
        case HTTP_GET:
        case HTTP_PUT:
            pos = 3;
            break;
        case HTTP_POST:
        case HTTP_HEAD:
            pos = 4;
            break;
        case HTTP_PATCH:
            pos = 5;
            break;
        case HTTP_DELETE:
            pos = 6;
            break;
        case HTTP_OPTIONS:
            pos = 7;
            break;
    }
    
    return data[pos] == ' ';
}

static __always_inline bool is_http_response(const char *data, int len)
{
    if (len < 12)
        return false;
    
    // Check for "HTTP/1." or "HTTP/2"
    return (data[0] == 'H' && data[1] == 'T' && data[2] == 'T' && data[3] == 'P' && data[4] == '/');
}

static __always_inline void extract_uri(const char *data, char *uri, int max_len)
{
    int start = 0;
    int end = 0;
    
    // Find start of URI (after method and space)
    for (int i = 0; i < 20 && i < max_len; i++) {
        if (data[i] == ' ') {
            start = i + 1;
            break;
        }
    }
    
    // Find end of URI (next space)
    for (int i = start; i < start + 63 && i < max_len; i++) {
        if (data[i] == ' ' || data[i] == '\r' || data[i] == '\n') {
            end = i;
            break;
        }
    }
    
    // Copy URI
    int len = end - start;
    if (len > 63) len = 63;
    for (int i = 0; i < len; i++) {
        uri[i] = data[start + i];
    }
    uri[len] = '\0';
}

static __always_inline u16 extract_status_code(const char *data)
{
    // Status code starts after "HTTP/x.x "
    int pos = 9;
    if (data[5] == '2') pos = 8; // HTTP/2
    
    u16 code = 0;
    code = (data[pos] - '0') * 100;
    code += (data[pos + 1] - '0') * 10;
    code += (data[pos + 2] - '0');
    
    return code;
}

SEC("kprobe/tcp_sendmsg")
int trace_tcp_sendmsg(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = PT_REGS_PARM3(ctx);
    
    if (size < 10 || size > 65536)
        return 0;
    
    // Read first part of data to check if it's HTTP
    char buf[128];
    struct iov_iter iter;
    bpf_probe_read_kernel(&iter, sizeof(iter), &msg->msg_iter);
    
    if (iter.iov && iter.nr_segs > 0) {
        struct iovec iov;
        bpf_probe_read_kernel(&iov, sizeof(iov), iter.iov);
        
        int len = iov.iov_len < 128 ? iov.iov_len : 128;
        bpf_probe_read_user(buf, len, iov.iov_base);
        
        if (is_http_request(buf, len)) {
            struct http_event event = {};
            event.timestamp = bpf_ktime_get_ns();
            event.pid = bpf_get_current_pid_tgid() >> 32;
            event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
            event.method = parse_http_method(buf);
            event.request_size = size;
            
            extract_uri(buf, event.uri, len);
            
            // Get container ID from map
            char *container_id = bpf_map_lookup_elem(&pid_to_container, &event.pid);
            if (container_id) {
                bpf_probe_read_kernel_str(event.container_id, sizeof(event.container_id), container_id);
            }
            
            // Get socket info
            struct inet_sock *inet = (struct inet_sock *)sk;
            bpf_probe_read_kernel(&event.src_port, sizeof(event.src_port), &inet->inet_sport);
            bpf_probe_read_kernel(&event.dst_port, sizeof(event.dst_port), &inet->inet_dport);
            bpf_probe_read_kernel(&event.src_ip, sizeof(event.src_ip), &inet->inet_saddr);
            bpf_probe_read_kernel(&event.dst_ip, sizeof(event.dst_ip), &inet->inet_daddr);
            
            u64 key = ((u64)event.pid << 32) | event.tid;
            bpf_map_update_elem(&http_requests, &key, &event, BPF_ANY);
        }
    }
    
    return 0;
}

SEC("kprobe/tcp_recvmsg")
int trace_tcp_recvmsg(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = PT_REGS_PARM3(ctx);
    int ret = PT_REGS_RC(ctx);
    
    if (ret <= 0 || ret > 65536)
        return 0;
    
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u32 tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    u64 key = ((u64)pid << 32) | tid;
    
    struct http_event *request = bpf_map_lookup_elem(&http_requests, &key);
    if (!request)
        return 0;
    
    // Read response data
    char buf[128];
    struct iov_iter iter;
    bpf_probe_read_kernel(&iter, sizeof(iter), &msg->msg_iter);
    
    if (iter.iov && iter.nr_segs > 0) {
        struct iovec iov;
        bpf_probe_read_kernel(&iov, sizeof(iov), iter.iov);
        
        int len = iov.iov_len < 128 ? iov.iov_len : 128;
        bpf_probe_read_user(buf, len, iov.iov_base);
        
        if (is_http_response(buf, len)) {
            request->status_code = extract_status_code(buf);
            request->response_size = ret;
            request->duration_ns = bpf_ktime_get_ns() - request->timestamp;
            
            // Submit event
            bpf_perf_event_output(ctx, &http_events, BPF_F_CURRENT_CPU, request, sizeof(*request));
            
            // Clean up
            bpf_map_delete_elem(&http_requests, &key);
        }
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";