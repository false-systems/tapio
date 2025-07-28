#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

#define MAX_ENTRIES 4096
#define MAX_PAYLOAD_SIZE 256
#define HTTP_PORT 80
#define HTTPS_PORT 443
#define GRPC_PORT 50051
#define MYSQL_PORT 3306
#define POSTGRES_PORT 5432
#define REDIS_PORT 6379
#define IPPROTO_TCP 6

// Protocol types
enum protocol_type {
    PROTO_HTTP = 1,
    PROTO_HTTPS = 2,
    PROTO_GRPC = 3,
    PROTO_MYSQL = 4,
    PROTO_POSTGRES = 5,
    PROTO_REDIS = 6,
    PROTO_UNKNOWN = 7,
};

// Protocol event types
enum protocol_event_type {
    PROTO_REQUEST = 1,
    PROTO_RESPONSE = 2,
    PROTO_ERROR = 3,
    PROTO_TIMEOUT = 4,
    PROTO_SLOW = 5,
};

// HTTP status code categories
enum http_status_category {
    HTTP_1XX = 1,  // Informational
    HTTP_2XX = 2,  // Success
    HTTP_3XX = 3,  // Redirection
    HTTP_4XX = 4,  // Client Error
    HTTP_5XX = 5,  // Server Error
};

// Protocol event structure
struct protocol_event {
    u64 timestamp;
    u32 pid;
    u32 tgid;
    u32 uid;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u8 protocol_type;
    u8 event_type;
    u16 status_code;
    u32 latency_us;
    u32 payload_size;
    u32 request_id;
    char method[16];        // HTTP method, SQL command, etc.
    char path[128];         // HTTP path, DB table, etc.
    char user_agent[64];    // HTTP User-Agent
    char error_msg[128];    // Error message if any
    char comm[16];
    char container_id[64];
};

// Request tracking
struct request_key {
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u32 seq_num;
};

struct request_info {
    u64 start_time;
    u8 protocol_type;
    u32 request_id;
    char method[16];
    char path[128];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct request_key);
    __type(value, struct request_info);
} active_requests SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024);
} protocol_events SEC(".maps");

// Helper function to extract container ID
static __always_inline int extract_container_id(char *container_id) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        __builtin_memcpy(container_id, "host\0", 5);
        return 0;
    }
    
    // For now, simplified container detection
    __builtin_memcpy(container_id, "container\0", 10);
    return 0;
}

// Helper function to emit protocol event
static __always_inline void emit_protocol_event(u8 protocol_type, u8 event_type, u32 pid, u32 tgid, u32 uid,
                               u32 src_ip, u32 dst_ip, u16 src_port, u16 dst_port,
                               u16 status_code, u32 latency_us, u32 payload_size,
                               u32 request_id, const char *method, const char *path,
                               const char *user_agent, const char *error_msg) {
    struct protocol_event *event;
    
    event = bpf_ringbuf_reserve(&protocol_events, sizeof(*event), 0);
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
    event->protocol_type = protocol_type;
    event->event_type = event_type;
    event->status_code = status_code;
    event->latency_us = latency_us;
    event->payload_size = payload_size;
    event->request_id = request_id;
    
    if (method)
        bpf_probe_read_kernel_str(event->method, sizeof(event->method), method);
    else
        __builtin_memset(event->method, 0, sizeof(event->method));
    
    if (path)
        bpf_probe_read_kernel_str(event->path, sizeof(event->path), path);
    else
        __builtin_memset(event->path, 0, sizeof(event->path));
    
    if (user_agent)
        bpf_probe_read_kernel_str(event->user_agent, sizeof(event->user_agent), user_agent);
    else
        __builtin_memset(event->user_agent, 0, sizeof(event->user_agent));
    
    if (error_msg)
        bpf_probe_read_kernel_str(event->error_msg, sizeof(event->error_msg), error_msg);
    else
        __builtin_memset(event->error_msg, 0, sizeof(event->error_msg));
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_container_id(event->container_id);
    
    bpf_ringbuf_submit(event, 0);
}

// Determine protocol type based on port
static __always_inline u8 get_protocol_type(u16 port) {
    switch (port) {
        case HTTP_PORT:
            return PROTO_HTTP;
        case HTTPS_PORT:
            return PROTO_HTTPS;
        case GRPC_PORT:
            return PROTO_GRPC;
        case MYSQL_PORT:
            return PROTO_MYSQL;
        case POSTGRES_PORT:
            return PROTO_POSTGRES;
        case REDIS_PORT:
            return PROTO_REDIS;
        default:
            return PROTO_UNKNOWN;
    }
}

// Parse HTTP request
static __always_inline int parse_http_request(void *data, void *data_end, char *method, char *path, char *user_agent) {
    char *payload = (char *)data;
    
    if (data + 16 > data_end)
        return -1;
    
    // Check for HTTP methods
    if (data + 7 <= data_end && __builtin_memcmp(payload, "GET ", 4) == 0) {
        __builtin_memcpy(method, "GET", 4);
    } else if (data + 8 <= data_end && __builtin_memcmp(payload, "POST ", 5) == 0) {
        __builtin_memcpy(method, "POST", 5);
    } else if (data + 7 <= data_end && __builtin_memcmp(payload, "PUT ", 4) == 0) {
        __builtin_memcpy(method, "PUT", 4);
    } else if (data + 10 <= data_end && __builtin_memcmp(payload, "DELETE ", 7) == 0) {
        __builtin_memcpy(method, "DELETE", 7);
    } else if (data + 9 <= data_end && __builtin_memcmp(payload, "PATCH ", 6) == 0) {
        __builtin_memcpy(method, "PATCH", 6);
    } else if (data + 8 <= data_end && __builtin_memcmp(payload, "HEAD ", 5) == 0) {
        __builtin_memcpy(method, "HEAD", 5);
    } else {
        return -1; // Not HTTP
    }
    
    // Extract path (simplified)
    int method_len = __builtin_strlen(method) + 1; // +1 for space
    char *path_start = payload + method_len;
    
    if (path_start >= (char *)data_end)
        return -1;
    
    #pragma unroll
    for (int i = 0; i < 127 && path_start + i < (char *)data_end; i++) {
        char c = path_start[i];
        if (c == ' ' || c == '\r' || c == '\n' || c == '?') {
            path[i] = '\0';
            break;
        }
        path[i] = c;
    }
    
    // Extract User-Agent (simplified - just mark as present)
    __builtin_memcpy(user_agent, "browser", 8);
    
    return 0;
}

// Parse HTTP response
static __always_inline int parse_http_response(void *data, void *data_end, u16 *status_code) {
    char *payload = (char *)data;
    
    if (data + 12 > data_end)
        return -1;
    
    // Check for HTTP response
    if (__builtin_memcmp(payload, "HTTP/1.0 ", 9) != 0 &&
        __builtin_memcmp(payload, "HTTP/1.1 ", 9) != 0 &&
        __builtin_memcmp(payload, "HTTP/2 ", 7) != 0)
        return -1;
    
    // Find status code position
    char *status_start = NULL;
    if (__builtin_memcmp(payload, "HTTP/2 ", 7) == 0) {
        status_start = payload + 7;
    } else {
        status_start = payload + 9;
    }
    
    if (status_start + 3 > (char *)data_end)
        return -1;
    
    // Convert to number
    *status_code = (status_start[0] - '0') * 100 +
                   (status_start[1] - '0') * 10 +
                   (status_start[2] - '0');
    
    return 0;
}

// Parse MySQL packet
static __always_inline int parse_mysql_packet(void *data, void *data_end, char *command) {
    if (data + 5 > data_end)
        return -1;
    
    // MySQL packet header: 3 bytes length + 1 byte sequence + 1 byte command
    u8 cmd = *((u8 *)data + 4);
    
    switch (cmd) {
        case 0x03:
            __builtin_memcpy(command, "QUERY", 6);
            break;
        case 0x01:
            __builtin_memcpy(command, "QUIT", 5);
            break;
        case 0x02:
            __builtin_memcpy(command, "USE_DB", 7);
            break;
        case 0x04:
            __builtin_memcpy(command, "FIELD_LIST", 11);
            break;
        case 0x05:
            __builtin_memcpy(command, "CREATE_DB", 10);
            break;
        case 0x06:
            __builtin_memcpy(command, "DROP_DB", 8);
            break;
        default:
            __builtin_memcpy(command, "UNKNOWN", 8);
            break;
    }
    
    return 0;
}

// Parse Redis command
static __always_inline int parse_redis_command(void *data, void *data_end, char *command) {
    if (data + 3 > data_end)
        return -1;
    
    char *payload = (char *)data;
    
    // Redis protocol: *<number of args>\r\n$<length>\r\n<command>\r\n...
    if (payload[0] == '*') {
        // Find first command after protocol headers
        char *cmd_start = payload;
        
        // Skip to first $
        #pragma unroll
        for (int i = 0; i < 20 && cmd_start + i < (char *)data_end; i++) {
            if (cmd_start[i] == '$') {
                // Skip $ and length until \r\n
                cmd_start = cmd_start + i + 1;
                #pragma unroll
                for (int j = 0; j < 10 && cmd_start + j < (char *)data_end; j++) {
                    if (cmd_start[j] == '\n') {
                        cmd_start = cmd_start + j + 1;
                        // Copy command
                        #pragma unroll
                        for (int k = 0; k < 15 && cmd_start + k < (char *)data_end; k++) {
                            if (cmd_start[k] == '\r' || cmd_start[k] == '\n') {
                                command[k] = '\0';
                                return 0;
                            }
                            command[k] = cmd_start[k];
                        }
                        return 0;
                    }
                }
                break;
            }
        }
    }
    
    return -1;
}

// Track protocol traffic
SEC("tc")
int tc_protocol_monitor(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)eth + sizeof(*eth) > data_end)
        return TC_ACT_OK;
    
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;
    
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    if ((void *)tcp + sizeof(*tcp) > data_end)
        return TC_ACT_OK;
    
    u16 src_port = bpf_ntohs(tcp->source);
    u16 dst_port = bpf_ntohs(tcp->dest);
    u32 seq_num = bpf_ntohl(tcp->seq);
    
    u8 protocol_type = get_protocol_type(dst_port);
    if (protocol_type == PROTO_UNKNOWN) {
        protocol_type = get_protocol_type(src_port);
    }
    
    if (protocol_type == PROTO_UNKNOWN)
        return TC_ACT_OK;
    
    void *payload = (void *)tcp + (tcp->doff * 4);
    u32 payload_size = data_end - payload;
    
    if (payload_size == 0 || payload >= data_end)
        return TC_ACT_OK;
    
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    // Check if this is a request or response based on port
    bool is_request = (dst_port == HTTP_PORT || dst_port == HTTPS_PORT ||
                      dst_port == GRPC_PORT || dst_port == MYSQL_PORT ||
                      dst_port == POSTGRES_PORT || dst_port == REDIS_PORT);
    
    if (is_request) {
        // Handle request
        struct request_key req_key = {
            .src_ip = ip->saddr,
            .dst_ip = ip->daddr,
            .src_port = src_port,
            .dst_port = dst_port,
            .seq_num = seq_num,
        };
        
        struct request_info req_info = {
            .start_time = bpf_ktime_get_ns(),
            .protocol_type = protocol_type,
            .request_id = seq_num,
        };
        
        char method[16] = {0};
        char path[128] = {0};
        char user_agent[64] = {0};
        
        if (protocol_type == PROTO_HTTP) {
            if (parse_http_request(payload, data_end, method, path, user_agent) == 0) {
                __builtin_memcpy(req_info.method, method, sizeof(req_info.method));
                __builtin_memcpy(req_info.path, path, sizeof(req_info.path));
            }
        } else if (protocol_type == PROTO_MYSQL) {
            if (parse_mysql_packet(payload, data_end, method) == 0) {
                __builtin_memcpy(req_info.method, method, sizeof(req_info.method));
            }
        } else if (protocol_type == PROTO_REDIS) {
            if (parse_redis_command(payload, data_end, method) == 0) {
                __builtin_memcpy(req_info.method, method, sizeof(req_info.method));
            }
        }
        
        bpf_map_update_elem(&active_requests, &req_key, &req_info, BPF_ANY);
        
        emit_protocol_event(protocol_type, PROTO_REQUEST, pid, tgid, uid,
                           ip->saddr, ip->daddr, src_port, dst_port,
                           0, 0, payload_size, seq_num, method, path, user_agent, NULL);
    } else {
        // Handle response
        struct request_key req_key = {
            .src_ip = ip->daddr,  // Reversed for response lookup
            .dst_ip = ip->saddr,
            .src_port = dst_port,
            .dst_port = src_port,
        };
        
        // Find matching request
        struct request_info *req_info = bpf_map_lookup_elem(&active_requests, &req_key);
        
        u32 latency_us = 0;
        if (req_info) {
            u64 latency_ns = bpf_ktime_get_ns() - req_info->start_time;
            latency_us = latency_ns / 1000;
        }
        
        u16 status_code = 0;
        char error_msg[128] = {0};
        
        if (protocol_type == PROTO_HTTP) {
            if (parse_http_response(payload, data_end, &status_code) < 0) {
                __builtin_memcpy(error_msg, "Parse error", 12);
            }
        }
        
        u8 event_type = PROTO_RESPONSE;
        if (status_code >= 400) {
            event_type = PROTO_ERROR;
        } else if (latency_us > 1000000) { // > 1 second
            event_type = PROTO_SLOW;
        }
        
        emit_protocol_event(protocol_type, event_type, pid, tgid, uid,
                           ip->saddr, ip->daddr, src_port, dst_port,
                           status_code, latency_us, payload_size, 0,
                           req_info ? req_info->method : NULL,
                           req_info ? req_info->path : NULL,
                           NULL, error_msg[0] ? error_msg : NULL);
        
        if (req_info) {
            bpf_map_delete_elem(&active_requests, &req_key);
        }
    }
    
    return TC_ACT_OK;
}

// Track SSL/TLS operations
SEC("uprobe/libssl:SSL_write")
int BPF_KPROBE(ssl_write_entry, void *ssl, const void *buf, int num) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    if (num > 0) {
        // Track SSL write for HTTPS monitoring
        emit_protocol_event(PROTO_HTTPS, PROTO_REQUEST, pid, tgid, uid,
                           0, 0, 0, 443, 0, 0, num, 0, "SSL_WRITE", NULL, NULL, NULL);
    }
    
    return 0;
}

SEC("uprobe/libssl:SSL_read")
int BPF_KPROBE(ssl_read_entry, void *ssl, void *buf, int num) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    // Track SSL read for HTTPS monitoring
    emit_protocol_event(PROTO_HTTPS, PROTO_RESPONSE, pid, tgid, uid,
                       0, 0, 443, 0, 0, 0, num, 0, "SSL_READ", NULL, NULL, NULL);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";