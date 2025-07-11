#include <linux/bpf.h>
#include <linux/in.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/ptrace.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_ENTRIES 4096
#define MAX_PAYLOAD_SIZE 256
#define HTTP_PORT 80
#define HTTPS_PORT 443
#define GRPC_PORT 50051
#define MYSQL_PORT 3306
#define POSTGRES_PORT 5432
#define REDIS_PORT 6379

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
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 uid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol_type;
    __u8 event_type;
    __u16 status_code;
    __u32 latency_us;
    __u32 payload_size;
    __u32 request_id;
    char method[16];        // HTTP method, SQL command, etc.
    char path[128];         // HTTP path, DB table, etc.
    char user_agent[64];    // HTTP User-Agent
    char error_msg[128];    // Error message if any
    char comm[16];
    char container_id[64];
};

// Request tracking
struct request_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 seq_num;
};

struct request_info {
    __u64 start_time;
    __u8 protocol_type;
    __u32 request_id;
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
static int extract_container_id(char *container_id) {
    __builtin_memset(container_id, 0, 64);
    bpf_probe_read_str(container_id, 8, "unknown");
    return 0;
}

// Helper function to emit protocol event
static void emit_protocol_event(__u8 protocol_type, __u8 event_type, __u32 pid, __u32 tgid, __u32 uid,
                               __u32 src_ip, __u32 dst_ip, __u16 src_port, __u16 dst_port,
                               __u16 status_code, __u32 latency_us, __u32 payload_size,
                               __u32 request_id, const char *method, const char *path,
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
        bpf_probe_read_str(event->method, sizeof(event->method), method);
    else
        __builtin_memset(event->method, 0, sizeof(event->method));
    
    if (path)
        bpf_probe_read_str(event->path, sizeof(event->path), path);
    else
        __builtin_memset(event->path, 0, sizeof(event->path));
    
    if (user_agent)
        bpf_probe_read_str(event->user_agent, sizeof(event->user_agent), user_agent);
    else
        __builtin_memset(event->user_agent, 0, sizeof(event->user_agent));
    
    if (error_msg)
        bpf_probe_read_str(event->error_msg, sizeof(event->error_msg), error_msg);
    else
        __builtin_memset(event->error_msg, 0, sizeof(event->error_msg));
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_container_id(event->container_id);
    
    bpf_ringbuf_submit(event, 0);
}

// Determine protocol type based on port
static __u8 get_protocol_type(__u16 port) {
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
static int parse_http_request(void *data, __u32 data_len, char *method, char *path, char *user_agent) {
    char *payload = (char *)data;
    
    if (data_len < 16)
        return -1;
    
    // Check for HTTP methods
    if (bpf_probe_read(method, 7, payload) < 0)
        return -1;
    
    if (__builtin_memcmp(method, "GET ", 4) == 0) {
        bpf_probe_read_str(method, 4, "GET");
    } else if (__builtin_memcmp(method, "POST ", 5) == 0) {
        bpf_probe_read_str(method, 5, "POST");
    } else if (__builtin_memcmp(method, "PUT ", 4) == 0) {
        bpf_probe_read_str(method, 4, "PUT");
    } else if (__builtin_memcmp(method, "DELETE ", 7) == 0) {
        bpf_probe_read_str(method, 7, "DELETE");
    } else {
        return -1; // Not HTTP
    }
    
    // Extract path (simplified)
    for (int i = 0; i < data_len - 10 && i < 120; i++) {
        char c;
        if (bpf_probe_read(&c, 1, payload + i) < 0)
            break;
        
        if (c == ' ') {
            // Found space after method, next is path
            int path_start = i + 1;
            for (int j = 0; j < 120 && path_start + j < data_len; j++) {
                if (bpf_probe_read(&c, 1, payload + path_start + j) < 0)
                    break;
                if (c == ' ' || c == '\r' || c == '\n') {
                    path[j] = '\0';
                    break;
                }
                path[j] = c;
            }
            break;
        }
    }
    
    // Extract User-Agent (simplified)
    bpf_probe_read_str(user_agent, 16, "unknown");
    
    return 0;
}

// Parse HTTP response
static int parse_http_response(void *data, __u32 data_len, __u16 *status_code) {
    char *payload = (char *)data;
    
    if (data_len < 12)
        return -1;
    
    // Check for HTTP response
    char http_version[9];
    if (bpf_probe_read(http_version, 8, payload) < 0)
        return -1;
    
    if (__builtin_memcmp(http_version, "HTTP/1.", 7) != 0)
        return -1;
    
    // Extract status code (simplified)
    if (data_len >= 12) {
        char status_str[4];
        if (bpf_probe_read(status_str, 3, payload + 9) == 0) {
            // Convert to number (simplified)
            *status_code = (status_str[0] - '0') * 100 +
                          (status_str[1] - '0') * 10 +
                          (status_str[2] - '0');
        }
    }
    
    return 0;
}

// Parse MySQL packet
static int parse_mysql_packet(void *data, __u32 data_len, char *command) {
    if (data_len < 5)
        return -1;
    
    // MySQL packet header: 3 bytes length + 1 byte sequence + 1 byte command
    __u8 cmd;
    if (bpf_probe_read(&cmd, 1, (char *)data + 4) < 0)
        return -1;
    
    switch (cmd) {
        case 0x03:
            bpf_probe_read_str(command, 6, "QUERY");
            break;
        case 0x01:
            bpf_probe_read_str(command, 5, "QUIT");
            break;
        case 0x02:
            bpf_probe_read_str(command, 8, "USE_DB");
            break;
        default:
            bpf_probe_read_str(command, 8, "UNKNOWN");
            break;
    }
    
    return 0;
}

// Track protocol traffic
SEC("tc")
int tc_protocol_monitor(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)eth + sizeof(*eth) > data_end)
        return TC_ACT_OK;
    
    if (eth->h_proto != __builtin_bswap16(ETH_P_IP))
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;
    
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    if ((void *)tcp + sizeof(*tcp) > data_end)
        return TC_ACT_OK;
    
    __u16 src_port = __builtin_bswap16(tcp->source);
    __u16 dst_port = __builtin_bswap16(tcp->dest);
    __u32 seq_num = __builtin_bswap32(tcp->seq);
    
    __u8 protocol_type = get_protocol_type(dst_port);
    if (protocol_type == PROTO_UNKNOWN) {
        protocol_type = get_protocol_type(src_port);
    }
    
    if (protocol_type == PROTO_UNKNOWN)
        return TC_ACT_OK;
    
    void *payload = (void *)tcp + (tcp->doff * 4);
    __u32 payload_size = data_end - payload;
    
    if (payload_size == 0)
        return TC_ACT_OK;
    
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
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
            if (parse_http_request(payload, payload_size, method, path, user_agent) == 0) {
                bpf_probe_read_str(req_info.method, sizeof(req_info.method), method);
                bpf_probe_read_str(req_info.path, sizeof(req_info.path), path);
            }
        } else if (protocol_type == PROTO_MYSQL) {
            if (parse_mysql_packet(payload, payload_size, method) == 0) {
                bpf_probe_read_str(req_info.method, sizeof(req_info.method), method);
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
        
        // Find matching request (simplified - in practice we'd need better matching)
        struct request_info *req_info = bpf_map_lookup_elem(&active_requests, &req_key);
        
        __u32 latency_us = 0;
        if (req_info) {
            __u64 latency_ns = bpf_ktime_get_ns() - req_info->start_time;
            latency_us = latency_ns / 1000;
        }
        
        __u16 status_code = 0;
        char error_msg[128] = {0};
        
        if (protocol_type == PROTO_HTTP) {
            if (parse_http_response(payload, payload_size, &status_code) < 0) {
                bpf_probe_read_str(error_msg, sizeof(error_msg), "Parse error");
            }
        }
        
        __u8 event_type = PROTO_RESPONSE;
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

// Track socket system calls for higher-level protocol monitoring
SEC("kprobe/sys_sendto")
int sys_sendto(struct pt_regs *ctx) {
    // Track send operations that might be protocol-related
    return 0;
}

SEC("kprobe/sys_recvfrom")
int sys_recvfrom(struct pt_regs *ctx) {
    // Track receive operations that might be protocol-related
    return 0;
}

// Track SSL/TLS handshakes for HTTPS monitoring
SEC("uretprobe/SSL_connect")
int ssl_connect_ret(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    int ret = PT_REGS_RC(ctx);
    
    if (ret <= 0) {
        // SSL connection failed
        emit_protocol_event(PROTO_HTTPS, PROTO_ERROR, pid, tgid, uid,
                           0, 0, 0, 0, 0, 0, 0, 0, "SSL_connect", NULL, NULL, "SSL handshake failed");
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";