#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

struct grpc_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u64 duration_ns;
    u16 status_code;
    u8 type; // 0=unary, 1=client_stream, 2=server_stream, 3=bidi_stream
    u8 flags;
    char method[96];
    char service[64];
    char container_id[64];
    char namespace[32];
    char pod_name[64];
    u64 request_size;
    u64 response_size;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u32 stream_id;
};

// gRPC frame types
#define GRPC_FRAME_DATA    0x00
#define GRPC_FRAME_HEADERS 0x01
#define GRPC_FRAME_RST_STREAM 0x03
#define GRPC_FRAME_SETTINGS 0x04
#define GRPC_FRAME_WINDOW_UPDATE 0x08

// gRPC status codes
#define GRPC_STATUS_OK 0
#define GRPC_STATUS_CANCELLED 1
#define GRPC_STATUS_UNKNOWN 2
#define GRPC_STATUS_INVALID_ARGUMENT 3
#define GRPC_STATUS_DEADLINE_EXCEEDED 4
#define GRPC_STATUS_NOT_FOUND 5
#define GRPC_STATUS_ALREADY_EXISTS 6
#define GRPC_STATUS_PERMISSION_DENIED 7
#define GRPC_STATUS_RESOURCE_EXHAUSTED 8
#define GRPC_STATUS_FAILED_PRECONDITION 9
#define GRPC_STATUS_ABORTED 10
#define GRPC_STATUS_OUT_OF_RANGE 11
#define GRPC_STATUS_UNIMPLEMENTED 12
#define GRPC_STATUS_INTERNAL 13
#define GRPC_STATUS_UNAVAILABLE 14
#define GRPC_STATUS_DATA_LOSS 15
#define GRPC_STATUS_UNAUTHENTICATED 16

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, u64);
    __type(value, struct grpc_event);
} grpc_requests SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u32));
} grpc_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, u32);
    __type(value, char[64]);
} pid_to_container SEC(".maps");

static __always_inline bool is_http2_magic(const char *data)
{
    // HTTP/2 connection preface: "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
    return (data[0] == 'P' && data[1] == 'R' && data[2] == 'I' && 
            data[3] == ' ' && data[4] == '*' && data[5] == ' ' &&
            data[6] == 'H' && data[7] == 'T' && data[8] == 'T' &&
            data[9] == 'P' && data[10] == '/' && data[11] == '2');
}

static __always_inline bool is_http2_frame(const char *data, int len)
{
    if (len < 9)
        return false;
    
    // HTTP/2 frame structure:
    // 3 bytes: length
    // 1 byte: type
    // 1 byte: flags
    // 4 bytes: stream ID
    u32 length = (data[0] << 16) | (data[1] << 8) | data[2];
    u8 type = data[3];
    
    // Reasonable frame length and valid type
    return (length > 0 && length < 16384 && type <= 0x0A);
}

static __always_inline void parse_grpc_headers(const char *data, int len, struct grpc_event *event)
{
    // gRPC uses HTTP/2 headers
    // Look for :path header which contains the gRPC method
    // Format: /package.Service/Method
    
    int pos = 9; // Skip HTTP/2 frame header
    while (pos < len - 10) {
        // Simplified HPACK parsing
        if (data[pos] == ':' && data[pos+1] == 'p' && data[pos+2] == 'a' && 
            data[pos+3] == 't' && data[pos+4] == 'h') {
            pos += 6; // Skip ":path" and length byte
            
            int path_start = pos;
            int path_len = 0;
            
            // Find end of path
            while (pos < len && data[pos] != '\0' && path_len < 95) {
                pos++;
                path_len++;
            }
            
            // Copy and parse path
            char path[96];
            bpf_probe_read_user_str(path, path_len + 1, &data[path_start]);
            
            // Extract service and method
            int slash_count = 0;
            int service_start = 0;
            int method_start = 0;
            
            for (int i = 0; i < path_len && i < 95; i++) {
                if (path[i] == '/') {
                    slash_count++;
                    if (slash_count == 1) {
                        service_start = i + 1;
                    } else if (slash_count == 2) {
                        method_start = i + 1;
                        // Copy service name
                        int service_len = i - service_start;
                        if (service_len > 63) service_len = 63;
                        for (int j = 0; j < service_len; j++) {
                            event->service[j] = path[service_start + j];
                        }
                        event->service[service_len] = '\0';
                    }
                }
            }
            
            // Copy method name
            if (method_start > 0) {
                bpf_probe_read_user_str(event->method, sizeof(event->method), &path[method_start]);
            }
            
            break;
        }
        pos++;
    }
}

static __always_inline u16 parse_grpc_status(const char *data, int len)
{
    // Look for grpc-status trailer
    int pos = 9; // Skip HTTP/2 frame header
    
    while (pos < len - 15) {
        if (data[pos] == 'g' && data[pos+1] == 'r' && data[pos+2] == 'p' && 
            data[pos+3] == 'c' && data[pos+4] == '-' && data[pos+5] == 's' &&
            data[pos+6] == 't' && data[pos+7] == 'a' && data[pos+8] == 't' &&
            data[pos+9] == 'u' && data[pos+10] == 's') {
            pos += 12; // Skip "grpc-status:"
            
            // Parse status code
            u16 status = 0;
            if (data[pos] >= '0' && data[pos] <= '9') {
                status = data[pos] - '0';
                if (data[pos+1] >= '0' && data[pos+1] <= '9') {
                    status = status * 10 + (data[pos+1] - '0');
                }
            }
            return status;
        }
        pos++;
    }
    
    return GRPC_STATUS_UNKNOWN;
}

SEC("kprobe/tcp_sendmsg")
int trace_grpc_sendmsg(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = PT_REGS_PARM3(ctx);
    
    if (size < 9 || size > 65536)
        return 0;
    
    // Read first part of data to check if it's HTTP/2
    char buf[256];
    struct iov_iter iter;
    bpf_probe_read_kernel(&iter, sizeof(iter), &msg->msg_iter);
    
    if (iter.iov && iter.nr_segs > 0) {
        struct iovec iov;
        bpf_probe_read_kernel(&iov, sizeof(iov), iter.iov);
        
        int len = iov.iov_len < 256 ? iov.iov_len : 256;
        bpf_probe_read_user(buf, len, iov.iov_base);
        
        bool is_grpc = false;
        u8 frame_type = 0;
        u32 stream_id = 0;
        
        if (is_http2_magic(buf)) {
            is_grpc = true;
        } else if (is_http2_frame(buf, len)) {
            frame_type = buf[3];
            stream_id = (buf[5] << 24) | (buf[6] << 16) | (buf[7] << 8) | buf[8];
            stream_id &= 0x7FFFFFFF; // Clear reserved bit
            
            if (frame_type == GRPC_FRAME_HEADERS && stream_id > 0) {
                is_grpc = true;
            }
        }
        
        if (is_grpc && frame_type == GRPC_FRAME_HEADERS) {
            struct grpc_event event = {};
            event.timestamp = bpf_ktime_get_ns();
            event.pid = bpf_get_current_pid_tgid() >> 32;
            event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
            event.stream_id = stream_id;
            event.request_size = size;
            
            parse_grpc_headers(buf, len, &event);
            
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
            
            u64 key = ((u64)event.pid << 32) | stream_id;
            bpf_map_update_elem(&grpc_requests, &key, &event, BPF_ANY);
        }
    }
    
    return 0;
}

SEC("kprobe/tcp_recvmsg")
int trace_grpc_recvmsg(struct pt_regs *ctx)
{
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = PT_REGS_PARM3(ctx);
    int ret = PT_REGS_RC(ctx);
    
    if (ret <= 0 || ret > 65536)
        return 0;
    
    // Read response data
    char buf[256];
    struct iov_iter iter;
    bpf_probe_read_kernel(&iter, sizeof(iter), &msg->msg_iter);
    
    if (iter.iov && iter.nr_segs > 0) {
        struct iovec iov;
        bpf_probe_read_kernel(&iov, sizeof(iov), iter.iov);
        
        int len = iov.iov_len < 256 ? iov.iov_len : 256;
        bpf_probe_read_user(buf, len, iov.iov_base);
        
        if (is_http2_frame(buf, len)) {
            u8 frame_type = buf[3];
            u32 stream_id = (buf[5] << 24) | (buf[6] << 16) | (buf[7] << 8) | buf[8];
            stream_id &= 0x7FFFFFFF;
            
            if (frame_type == GRPC_FRAME_HEADERS && stream_id > 0) {
                u32 pid = bpf_get_current_pid_tgid() >> 32;
                u64 key = ((u64)pid << 32) | stream_id;
                
                struct grpc_event *request = bpf_map_lookup_elem(&grpc_requests, &key);
                if (request) {
                    request->status_code = parse_grpc_status(buf, len);
                    request->response_size = ret;
                    request->duration_ns = bpf_ktime_get_ns() - request->timestamp;
                    
                    // Submit event
                    bpf_perf_event_output(ctx, &grpc_events, BPF_F_CURRENT_CPU, request, sizeof(*request));
                    
                    // Clean up
                    bpf_map_delete_elem(&grpc_requests, &key);
                }
            }
        }
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";