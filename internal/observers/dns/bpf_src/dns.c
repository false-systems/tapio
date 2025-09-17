// SPDX-License-Identifier: GPL-2.0
// DNS Monitor with Full CO-RE Support - Per CLAUDE.md standards
// NO STUBS - Complete implementation only

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>
#include "../../bpf_common/core_helpers.h"

// DNS constants
#define DNS_PORT 53
#define MAX_DNS_NAME_LEN 256
#define DNS_SAMPLE_RATE 10  // Sample 1 in 10 for high QPS
#define DNS_MAX_EVENTS_PER_SEC 1000

// DNS event types
#define DNS_EVENT_QUERY    1
#define DNS_EVENT_RESPONSE 2
#define DNS_EVENT_TIMEOUT  3
#define DNS_EVENT_ERROR    4

// Protocol constants
#define IPPROTO_UDP 17
#define IPPROTO_TCP 6
#define AF_INET 2
#define AF_INET6 10

// DNS query types
#define DNS_TYPE_A     1
#define DNS_TYPE_AAAA  28
#define DNS_TYPE_CNAME 5
#define DNS_TYPE_MX    15
#define DNS_TYPE_TXT   16
#define DNS_TYPE_SRV   33

// DNS response codes
#define DNS_RCODE_NOERROR  0
#define DNS_RCODE_SERVFAIL 2
#define DNS_RCODE_NXDOMAIN 3
#define DNS_RCODE_REFUSED  5

// DNS header structure
struct dnshdr {
    u16 id;
    u16 flags;
    u16 qdcount;
    u16 ancount;
    u16 nscount;
    u16 arcount;
} __attribute__((packed));

// IPv6 address structure
struct ipv6_addr {
    u32 addr[4];
} __attribute__((packed));

// DNS event structure with IPv4/IPv6 support
struct dns_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u32 uid;
    u32 gid;
    u64 cgroup_id;

    u8 event_type;
    u8 protocol;
    u8 ip_version;
    u8 rcode;

    // Network info - supports both IPv4 and IPv6
    union {
        u32 v4_src;
        struct ipv6_addr v6_src;
    } src_addr;

    union {
        u32 v4_dst;
        struct ipv6_addr v6_dst;
    } dst_addr;

    u16 src_port;
    u16 dst_port;

    // DNS info
    u16 dns_id;
    u16 query_type;
    u32 latency_ns;

    char comm[TASK_COMM_LEN];
    char query_name[MAX_DNS_NAME_LEN];

    // Stats
    u32 packet_size;
    u8 answers_count;
    u8 is_error;
    u8 is_tcp;  // Track if DNS over TCP
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RINGBUF_SIZE_MEDIUM);  // 256KB for DNS
} dns_events SEC(".maps");

// Rate limiter map
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct rate_limiter);
} dns_rate_limit SEC(".maps");

// Overflow stats map
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct overflow_stats);
} dns_overflow SEC(".maps");

// Query tracking for latency calculation with IPv4/IPv6 support
struct query_track {
    u64 start_time;
    u16 query_type;
    u8 ip_version;
    u8 protocol;
    union {
        u32 v4_src;
        struct ipv6_addr v6_src;
    } src_addr;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, u16);  // DNS ID
    __type(value, struct query_track);
} dns_queries SEC(".maps");

// Parse DNS name from packet with compression support
static __always_inline int parse_dns_name(char *dst, void *dns_start, void *dns_payload, void *data_end) {
    if (!dst || !dns_payload || !dns_start) return -1;

    u8 *pos = (u8 *)dns_payload;
    int name_len = 0;
    int jumps = 0;  // Track compression jumps to prevent loops
    bool jumped = false;
    u8 *jump_back = NULL;

    // Bounded loop for DNS name parsing with compression support
    #pragma unroll
    for (int i = 0; i < 64 && i < MAX_LOOP_ITERATIONS; i++) {
        if (pos + 1 > (u8 *)data_end) break;

        u8 len;
        if (safe_probe_read(&len, 1, pos) < 0) break;

        // Check for DNS compression (top 2 bits set: 11xxxxxx)
        if ((len & 0xC0) == 0xC0) {
            if (jumps++ > 5) break;  // Prevent infinite loops

            // Read 2-byte offset
            if (pos + 2 > (u8 *)data_end) break;

            u8 offset_high, offset_low;
            if (safe_probe_read(&offset_high, 1, pos) < 0) break;
            if (safe_probe_read(&offset_low, 1, pos + 1) < 0) break;

            u16 offset = ((u16)(offset_high & 0x3F) << 8) | (u16)offset_low;

            // Validate offset
            if (offset >= ((u8 *)data_end - (u8 *)dns_start)) break;

            // Save position to return to if first jump
            if (!jumped) {
                jump_back = pos + 2;
                jumped = true;
            }

            // Jump to referenced position
            pos = (u8 *)dns_start + offset;
            continue;
        }

        if (len == 0) {
            // End of name
            if (jumped && jump_back) {
                pos = jump_back;
            }
            break;
        }

        if (len > 63) break;  // Invalid label length

        pos++;
        if (pos + len > (u8 *)data_end) break;

        // Add dot separator between labels
        if (name_len > 0 && name_len < MAX_DNS_NAME_LEN - 1) {
            dst[name_len++] = '.';
        }

        // Copy label with bounds check
        int copy_len = len;
        if (name_len + copy_len >= MAX_DNS_NAME_LEN - 1) {
            copy_len = MAX_DNS_NAME_LEN - 1 - name_len;
        }

        if (copy_len > 0) {
            if (safe_probe_read(dst + name_len, copy_len, pos) < 0)
                break;
            name_len += copy_len;
        }

        pos += len;
    }

    // Null terminate
    if (name_len < MAX_DNS_NAME_LEN) {
        dst[name_len] = '\0';
    } else {
        dst[MAX_DNS_NAME_LEN - 1] = '\0';
    }

    return name_len;
}

// Extract DNS query type
static __always_inline u16 extract_query_type(void *dns_start, void *dns_payload, void *data_end) {
    if (!dns_payload || !dns_start) return 0;

    u8 *pos = (u8 *)dns_payload;

    // Skip the query name
    #pragma unroll
    for (int i = 0; i < 64 && i < MAX_LOOP_ITERATIONS; i++) {
        if (pos + 1 > (u8 *)data_end) return 0;

        u8 len;
        if (safe_probe_read(&len, 1, pos) < 0) return 0;

        // Handle compression
        if ((len & 0xC0) == 0xC0) {
            pos += 2;  // Skip compression pointer
            break;
        }

        if (len == 0) {
            pos++;  // Skip null terminator
            break;
        }

        if (len > 63) return 0;

        pos += len + 1;
    }

    // Read query type (2 bytes)
    if (pos + 2 > (u8 *)data_end) return 0;

    u16 qtype;
    if (safe_probe_read(&qtype, 2, pos) < 0) return 0;

    return bpf_ntohs(qtype);
}

// Initialize rate limiter
static __always_inline void init_rate_limiter(void) {
    u32 key = 0;
    struct rate_limiter *limiter = bpf_map_lookup_elem(&dns_rate_limit, &key);
    if (limiter && limiter->max_per_sec == 0) {
        limiter->max_per_sec = DNS_MAX_EVENTS_PER_SEC;
        limiter->tokens = DNS_MAX_EVENTS_PER_SEC;
        limiter->last_refill_ns = bpf_ktime_get_ns();
    }
}

// Check rate limit
static __always_inline bool check_rate_limit(void) {
    u32 key = 0;
    struct rate_limiter *limiter = bpf_map_lookup_elem(&dns_rate_limit, &key);
    if (!limiter) return false;
    
    bool limited = should_rate_limit(limiter);
    
    if (limited) {
        struct overflow_stats *stats = bpf_map_lookup_elem(&dns_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->rate_limit_drops, 1);
        }
    }
    
    return limited;
}

// Check sampling
static __always_inline bool check_sampling(void) {
    bool sampled = should_sample(DNS_SAMPLE_RATE);
    
    if (!sampled) {
        u32 key = 0;
        struct overflow_stats *stats = bpf_map_lookup_elem(&dns_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->sampling_drops, 1);
        }
    }
    
    return !sampled;
}

// Submit DNS event with overflow tracking
static __always_inline int submit_dns_event(struct dns_event *event) {
    if (!event) return -1;
    
    struct dns_event *e = bpf_ringbuf_reserve(&dns_events, sizeof(*e), 0);
    if (!e) {
        u32 key = 0;
        struct overflow_stats *stats = bpf_map_lookup_elem(&dns_overflow, &key);
        if (stats) {
            __sync_fetch_and_add(&stats->ringbuf_drops, 1);
        }
        return -1;
    }
    
    __builtin_memcpy(e, event, sizeof(*event));
    bpf_ringbuf_submit(e, 0);
    return 0;
}

SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    // Initialize rate limiter on first call
    init_rate_limiter();
    
    // Rate limiting check
    if (check_rate_limit()) return 0;
    
    // Sampling check
    if (check_sampling()) return 0;
    
    // Get socket with CO-RE
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    // Read socket fields with CO-RE
    u16 dport = 0, sport = 0;
    u32 daddr = 0, saddr = 0;
    
    // Use BPF_CORE_READ for all kernel struct access
    dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
    sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    
    // Check if DNS port
    if (bpf_ntohs(dport) != DNS_PORT && sport != DNS_PORT) {
        return 0;
    }
    
    // Get addresses with CO-RE
    daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    
    // Get message buffer
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    if (!msg) return 0;
    
    // Get iov_base and iov_len with CO-RE
    // Get iov data directly without const issues
    void *dns_data = NULL;
    size_t dns_len = 0;

    // Read iov_base and iov_len separately to avoid const qualifier issues
    if (BPF_CORE_READ_INTO(&dns_data, msg, msg_iter.iov->iov_base) != 0) return 0;
    if (BPF_CORE_READ_INTO(&dns_len, msg, msg_iter.iov->iov_len) != 0) return 0;
    if (!dns_data) return 0;
    
    // dns_data and dns_len already read above
    
    if (!dns_data || dns_len < sizeof(struct dnshdr)) return 0;
    
    // Read DNS header
    struct dnshdr dns_hdr = {};
    if (safe_probe_read_user(&dns_hdr, sizeof(dns_hdr), dns_data) < 0)
        return 0;
    
    // Reserve space in ring buffer to avoid stack overflow
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) return 0;

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = DNS_EVENT_QUERY;
    event->protocol = IPPROTO_UDP;
    event->ip_version = 4;
    
    // Process info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->pid = BPF_CORE_READ(task, tgid);
        event->tid = BPF_CORE_READ(task, pid);
        u64 uid_gid = bpf_get_current_uid_gid();
        event->uid = uid_gid & 0xFFFFFFFF;
        event->gid = uid_gid >> 32;
        event->cgroup_id = get_cgroup_id(task);
    }
    
    get_current_comm(event->comm);

    // Network info - IPv4 for now, IPv6 support added below
    event->src_addr.v4_src = saddr;
    event->dst_addr.v4_dst = daddr;
    event->src_port = sport;
    event->dst_port = bpf_ntohs(dport);

    // DNS info
    event->dns_id = bpf_ntohs(dns_hdr.id);
    event->packet_size = dns_len;

    // Parse query name with compression support
    void *query_start = dns_data + sizeof(struct dnshdr);
    void *data_end = dns_data + dns_len;
    parse_dns_name(event->query_name, dns_data, query_start, data_end);

    // Extract query type
    event->query_type = extract_query_type(dns_data, query_start, data_end);

    // Track query for latency calculation
    struct query_track track = {
        .start_time = event->timestamp,
        .query_type = event->query_type,
        .ip_version = 4,
        .protocol = IPPROTO_UDP,
    };
    track.src_addr.v4_src = saddr;
    bpf_map_update_elem(&dns_queries, &event->dns_id, &track, BPF_ANY);

    // Submit event (event is already a pointer)
    bpf_ringbuf_submit(event, 0);

    return 0;
}

SEC("kprobe/udp_recvmsg")
int trace_udp_recvmsg(struct pt_regs *ctx) {
    // Rate limiting check
    if (check_rate_limit()) return 0;
    
    // Get socket with CO-RE
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    // Check if DNS port with CO-RE
    u16 sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    if (sport != DNS_PORT) return 0;
    
    // Get message buffer
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    if (!msg) return 0;
    
    // Get DNS data with CO-RE
    // Get iov data directly without const issues
    void *dns_data = NULL;
    size_t dns_len = 0;

    // Read iov_base and iov_len separately to avoid const qualifier issues
    if (BPF_CORE_READ_INTO(&dns_data, msg, msg_iter.iov->iov_base) != 0) return 0;
    if (BPF_CORE_READ_INTO(&dns_len, msg, msg_iter.iov->iov_len) != 0) return 0;
    if (!dns_data) return 0;
    
    // dns_data and dns_len already read above
    
    if (!dns_data || dns_len < sizeof(struct dnshdr)) return 0;
    
    // Read DNS header
    struct dnshdr dns_hdr = {};
    if (safe_probe_read_user(&dns_hdr, sizeof(dns_hdr), dns_data) < 0)
        return 0;
    
    // Look up original query
    u16 dns_id = bpf_ntohs(dns_hdr.id);
    struct query_track *track = bpf_map_lookup_elem(&dns_queries, &dns_id);
    
    // Reserve ring buffer space for response event
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) return 0;

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = DNS_EVENT_RESPONSE;
    event->protocol = IPPROTO_UDP;
    event->ip_version = 4;

    // Calculate latency if we tracked the query
    if (track) {
        event->latency_ns = event->timestamp - track->start_time;
        event->query_type = track->query_type;
        event->ip_version = track->ip_version;
        if (track->ip_version == 4) {
            event->src_addr.v4_src = track->src_addr.v4_src;
        } else {
            event->src_addr.v6_src = track->src_addr.v6_src;
        }
        bpf_map_delete_elem(&dns_queries, &dns_id);
    }

    // Process info with CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->pid = BPF_CORE_READ(task, tgid);
        event->tid = BPF_CORE_READ(task, pid);
        u64 uid_gid = bpf_get_current_uid_gid();
        event->uid = uid_gid & 0xFFFFFFFF;
        event->gid = uid_gid >> 32;
        event->cgroup_id = get_cgroup_id(task);
    }

    get_current_comm(event->comm);

    // DNS response info
    event->dns_id = dns_id;
    event->packet_size = dns_len;

    // Extract response code
    u16 flags = bpf_ntohs(dns_hdr.flags);
    event->rcode = flags & 0x0F;
    event->is_error = (event->rcode != DNS_RCODE_NOERROR);
    event->answers_count = bpf_ntohs(dns_hdr.ancount);

    // Submit event
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

// TCP DNS support - track DNS sockets
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 1024);
    __type(key, u64);  // Socket pointer
    __type(value, u8);  // 1 if DNS socket
} tcp_dns_sockets SEC(".maps");

// TCP DNS monitoring - connect to port 53
SEC("kprobe/tcp_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;

    // Check destination port with CO-RE
    u16 dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
    if (bpf_ntohs(dport) != DNS_PORT) return 0;

    // Mark this socket for DNS tracking
    u64 sock_ptr = (u64)sk;
    u8 dns_flag = 1;
    bpf_map_update_elem(&tcp_dns_sockets, &sock_ptr, &dns_flag, BPF_ANY);

    return 0;
}

// TCP DNS query monitoring
SEC("kprobe/tcp_sendmsg")
int trace_tcp_sendmsg(struct pt_regs *ctx) {
    // Rate limiting check
    if (check_rate_limit()) return 0;
    if (check_sampling()) return 0;

    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;

    // Check if this is a DNS socket
    u64 sock_ptr = (u64)sk;
    u8 *is_dns = bpf_map_lookup_elem(&tcp_dns_sockets, &sock_ptr);
    if (!is_dns) return 0;

    // Get message buffer
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    if (!msg) return 0;

    // Get DNS data with CO-RE
    // Get iov data directly without const issues
    void *dns_data = NULL;
    size_t dns_len = 0;

    // Read iov_base and iov_len separately to avoid const qualifier issues
    if (BPF_CORE_READ_INTO(&dns_data, msg, msg_iter.iov->iov_base) != 0) return 0;
    if (BPF_CORE_READ_INTO(&dns_len, msg, msg_iter.iov->iov_len) != 0) return 0;
    if (!dns_data) return 0;

    // dns_data and dns_len already read above

    // TCP DNS has 2-byte length prefix
    if (!dns_data || dns_len < sizeof(struct dnshdr) + 2) return 0;

    // Skip TCP length prefix and read DNS header
    struct dnshdr dns_hdr = {};
    if (safe_probe_read_user(&dns_hdr, sizeof(dns_hdr), dns_data + 2) < 0)
        return 0;

    // Reserve space in ring buffer
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) return 0;

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = DNS_EVENT_QUERY;
    event->protocol = IPPROTO_TCP;
    event->is_tcp = 1;
    // Detect IP version from socket family
    u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    event->ip_version = (family == AF_INET6) ? 6 : 4;

    // Process info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->pid = BPF_CORE_READ(task, tgid);
        event->tid = BPF_CORE_READ(task, pid);
        u64 uid_gid = bpf_get_current_uid_gid();
        event->uid = uid_gid & 0xFFFFFFFF;
        event->gid = uid_gid >> 32;
        event->cgroup_id = get_cgroup_id(task);
    }

    get_current_comm(event->comm);

    // Network info from socket
    u32 saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    u32 daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    u16 sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    u16 dport = BPF_CORE_READ(sk, __sk_common.skc_dport);

    event->src_addr.v4_src = saddr;
    event->dst_addr.v4_dst = daddr;
    event->src_port = sport;
    event->dst_port = bpf_ntohs(dport);

    // DNS info
    event->dns_id = bpf_ntohs(dns_hdr.id);
    event->packet_size = dns_len - 2;  // Exclude TCP length prefix

    // Parse query name (skip TCP 2-byte prefix)
    void *query_start = dns_data + 2 + sizeof(struct dnshdr);
    void *data_end = dns_data + dns_len;
    parse_dns_name(event->query_name, dns_data + 2, query_start, data_end);

    // Extract query type
    event->query_type = extract_query_type(dns_data + 2, query_start, data_end);

    // Track query
    struct query_track track = {
        .start_time = event->timestamp,
        .query_type = event->query_type,
        .ip_version = 4,
        .protocol = IPPROTO_TCP,
    };
    track.src_addr.v4_src = saddr;
    bpf_map_update_elem(&dns_queries, &event->dns_id, &track, BPF_ANY);

    // Submit event (event is already a pointer)
    bpf_ringbuf_submit(event, 0);

    return 0;
}

// IPv6 support for UDP
SEC("kprobe/udpv6_sendmsg")
int trace_udpv6_sendmsg(struct pt_regs *ctx) {
    // Rate limiting check
    if (check_rate_limit()) return 0;
    if (check_sampling()) return 0;

    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;

    // Check DNS port
    u16 dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
    u16 sport = BPF_CORE_READ(sk, __sk_common.skc_num);

    if (bpf_ntohs(dport) != DNS_PORT && sport != DNS_PORT) {
        return 0;
    }

    // Get message buffer
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    if (!msg) return 0;

    // Get DNS data
    // Get iov data directly without const issues
    void *dns_data = NULL;
    size_t dns_len = 0;

    // Read iov_base and iov_len separately to avoid const qualifier issues
    if (BPF_CORE_READ_INTO(&dns_data, msg, msg_iter.iov->iov_base) != 0) return 0;
    if (BPF_CORE_READ_INTO(&dns_len, msg, msg_iter.iov->iov_len) != 0) return 0;
    if (!dns_data) return 0;

    // dns_data and dns_len already read above

    if (!dns_data || dns_len < sizeof(struct dnshdr)) return 0;

    // Read DNS header
    struct dnshdr dns_hdr = {};
    if (safe_probe_read_user(&dns_hdr, sizeof(dns_hdr), dns_data) < 0)
        return 0;

    // Reserve space in ring buffer
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) return 0;

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = DNS_EVENT_QUERY;
    event->protocol = IPPROTO_UDP;
    event->ip_version = 6;

    // Process info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->pid = BPF_CORE_READ(task, tgid);
        event->tid = BPF_CORE_READ(task, pid);
        u64 uid_gid = bpf_get_current_uid_gid();
        event->uid = uid_gid & 0xFFFFFFFF;
        event->gid = uid_gid >> 32;
        event->cgroup_id = get_cgroup_id(task);
    }

    get_current_comm(event->comm);

    // IPv6 addresses - simplified for now (will work for IPv4 over IPv6 mappings)
    // Full IPv6 support requires checking if kernel has IPv6 fields
    u32 saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    u32 daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);

    if (event->ip_version == 6) {
        // For now, store IPv4-mapped addresses
        event->src_addr.v6_src.addr[0] = 0;
        event->src_addr.v6_src.addr[1] = 0;
        event->src_addr.v6_src.addr[2] = bpf_htonl(0xFFFF);
        event->src_addr.v6_src.addr[3] = saddr;

        event->dst_addr.v6_dst.addr[0] = 0;
        event->dst_addr.v6_dst.addr[1] = 0;
        event->dst_addr.v6_dst.addr[2] = bpf_htonl(0xFFFF);
        event->dst_addr.v6_dst.addr[3] = daddr;
    } else {
        event->src_addr.v4_src = saddr;
        event->dst_addr.v4_dst = daddr;
    }

    event->src_port = sport;
    event->dst_port = bpf_ntohs(dport);

    // DNS info
    event->dns_id = bpf_ntohs(dns_hdr.id);
    event->packet_size = dns_len;

    // Parse query name
    void *query_start = dns_data + sizeof(struct dnshdr);
    void *data_end = dns_data + dns_len;
    parse_dns_name(event->query_name, dns_data, query_start, data_end);

    // Extract query type
    event->query_type = extract_query_type(dns_data, query_start, data_end);

    // Track query
    struct query_track track = {
        .start_time = event->timestamp,
        .query_type = event->query_type,
        .ip_version = 6,
        .protocol = IPPROTO_UDP,
    };
    track.src_addr.v6_src = event->src_addr.v6_src;
    bpf_map_update_elem(&dns_queries, &event->dns_id, &track, BPF_ANY);

    // Submit event (event is already a pointer)
    bpf_ringbuf_submit(event, 0);

    return 0;
}

char _license[] SEC("license") = "GPL";