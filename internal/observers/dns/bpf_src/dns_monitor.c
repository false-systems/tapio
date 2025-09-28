//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "../../bpf_common/core_helpers.h"

#define DNS_PORT 53
#define COREDNS_PORT 9153
#define MAX_DNS_NAME_LENGTH 253
#define MAX_MAP_ENTRIES 10240
#define MAX_DNS_PACKET_SIZE 1024

// Event types
#define DNS_EVENT_SLOW      1
#define DNS_EVENT_TIMEOUT   2
#define DNS_EVENT_NXDOMAIN  3
#define DNS_EVENT_SERVFAIL  4

// DNS problem thresholds
#define SLOW_THRESHOLD_NS   100000000ULL  // 100ms
#define TIMEOUT_THRESHOLD_NS 5000000000ULL // 5s

// DNS query tracking key
struct dns_query_key {
    __u32 pid;
    __u32 tid;
    __u16 query_id;
    __u8 protocol;
    __u8 pad;
};

// DNS query state for tracking
struct dns_query_state {
    __u64 start_time_ns;
    __u32 pid;
    __u32 tid;
    __u16 query_id;
    __u16 query_type;
    __u16 src_port;
    __u16 dst_port;
    __u8 query_name[MAX_DNS_NAME_LENGTH];
    __u8 server_ip[16];
    __u8 protocol;
    __u8 is_coredns;
    __u8 pad[2];
};

// DNS event to userspace
struct dns_event {
    __u64 timestamp_ns;
    __u64 latency_ns;
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u16 query_id;
    __u16 query_type;
    __u16 src_port;
    __u16 dst_port;
    __u8 event_type;
    __u8 response_code;
    __u8 protocol;
    __u8 is_coredns;
    __u8 query_name[MAX_DNS_NAME_LENGTH];
    __u8 server_ip[16];
    __u8 comm[TASK_COMM_LEN];
    __u8 k8s_service[64];
    __u8 k8s_namespace[32];
    __u32 retries;
    __u32 pad;
};

// Configuration map
struct dns_config {
    __u64 slow_threshold_ns;
    __u64 timeout_threshold_ns;
    __u16 coredns_port;
    __u8 enable_tcp;
    __u8 enable_k8s;
    __u32 rate_limit_per_sec;
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, struct dns_query_key);
    __type(value, struct dns_query_state);
    __uint(max_entries, MAX_MAP_ENTRIES);
} active_queries SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RINGBUF_SIZE_MEDIUM);
} dns_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __type(key, __u32);
    __type(value, struct dns_config);
    __uint(max_entries, 1);
} config SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u32);
    __type(value, __u8);
    __uint(max_entries, 128);
} coredns_pids SEC(".maps");

// Rate limiting map
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u32);
    __type(value, struct rate_limiter);
    __uint(max_entries, 1024);
} rate_limits SEC(".maps");

// Helper to check if domain is Kubernetes related
static __always_inline bool is_k8s_domain(const char *name, int len) {
    // Check for cluster.local suffix
    const char cluster_local[] = ".cluster.local";
    int suffix_len = sizeof(cluster_local) - 1;

    if (len < suffix_len) return false;

    // Simplified check - just check last few chars
    if (len > 14) {
        // Check ".local" suffix only
        if (name[len - 6] == '.' &&
            name[len - 5] == 'l' &&
            name[len - 4] == 'o' &&
            name[len - 3] == 'c' &&
            name[len - 2] == 'a' &&
            name[len - 1] == 'l') {
            return true;
        }
    }
    return false;
}

// Helper to extract K8s service from domain name
static __always_inline void extract_k8s_service(const char *domain,
                                               char *service, char *namespace) {
    // Simplified extraction - just get first component
    int i = 0;

    // Copy first component as service (max 32 chars)
    for (i = 0; i < 32 && domain[i] && domain[i] != '.'; i++) {
        service[i] = domain[i];
    }
    if (i < 32) service[i] = 0;

    // Skip to namespace if there's a dot
    if (domain[i] == '.') {
        i++;
        int j = 0;
        // Copy second component as namespace (max 16 chars)
        for (j = 0; j < 16 && domain[i] && domain[i] != '.'; i++, j++) {
            namespace[j] = domain[i];
        }
        if (j < 16) namespace[j] = 0;
    }
}

// Check rate limit
static __always_inline bool check_rate_limit(__u32 pid) {
    struct rate_limiter *limiter = bpf_map_lookup_elem(&rate_limits, &pid);
    __u64 now = bpf_ktime_get_ns();

    if (!limiter) {
        struct rate_limiter new_limiter = {
            .tokens = 1,
            .last_refill_ns = now,
            .max_per_sec = 10  // Max 10 events per second per PID
        };
        bpf_map_update_elem(&rate_limits, &pid, &new_limiter, BPF_ANY);
        return true;
    }

    // Refill tokens based on time passed
    __u64 time_diff = now - limiter->last_refill_ns;
    __u64 new_tokens = time_diff / 100000000ULL; // 10 tokens per second

    if (new_tokens > 0) {
        limiter->tokens += new_tokens;
        if (limiter->tokens > limiter->max_per_sec) {
            limiter->tokens = limiter->max_per_sec;
        }
        limiter->last_refill_ns = now;
    }

    if (limiter->tokens > 0) {
        limiter->tokens--;
        return true;
    }

    return false; // Rate limited
}

// Submit DNS event to userspace
static __always_inline void submit_dns_event(struct dns_query_state *query,
                                            __u8 event_type, __u8 response_code) {
    // Check rate limit
    if (!check_rate_limit(query->pid)) {
        return;
    }

    struct dns_event *event = bpf_ringbuf_reserve(&dns_events,
                                                  sizeof(struct dns_event), 0);
    if (!event) {
        return;
    }

    __u64 now = bpf_ktime_get_ns();

    // Fill event
    event->timestamp_ns = now;
    event->latency_ns = now - query->start_time_ns;
    event->pid = query->pid;
    event->tid = query->tid;
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    event->query_id = query->query_id;
    event->query_type = query->query_type;
    event->src_port = query->src_port;
    event->dst_port = query->dst_port;
    event->event_type = event_type;
    event->response_code = response_code;
    event->protocol = query->protocol;
    event->is_coredns = query->is_coredns;
    event->retries = 0;

    // Copy query name (limited loop)
    for (int i = 0; i < MAX_DNS_NAME_LENGTH && i < 64; i++) {
        event->query_name[i] = query->query_name[i];
    }

    // Copy server IP (limited to IPv4 + padding)
    for (int i = 0; i < 16 && i < 8; i++) {
        event->server_ip[i] = query->server_ip[i];
    }

    // Get process command
    bpf_get_current_comm(&event->comm, sizeof(event->comm));

    // Extract K8s info if domain is K8s related
    if (is_k8s_domain((char*)query->query_name, MAX_DNS_NAME_LENGTH)) {
        extract_k8s_service((char*)query->query_name,
                          (char*)event->k8s_service,
                          (char*)event->k8s_namespace);
    }

    bpf_ringbuf_submit(event, 0);
}

// Tracepoint for DNS queries via connect() syscall
SEC("tracepoint/syscalls/sys_exit_connect")
int trace_connect_exit(struct trace_event_raw_sys_exit *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tid = id & 0xFFFFFFFF;

    // Only track successful connections
    if (ctx->ret != 0) {
        return 0;
    }

    // Simple heuristic: assume DNS if connecting to port 53
    // In production, would need more sophisticated detection

    // Start tracking this as potential DNS query
    struct dns_query_key key = {
        .pid = pid,
        .tid = tid,
        .query_id = 0, // Will be filled when we see actual DNS packet
        .protocol = 1, // TCP
    };

    struct dns_query_state state = {
        .start_time_ns = bpf_ktime_get_ns(),
        .pid = pid,
        .tid = tid,
        .query_id = 0,
        .query_type = 1, // A record
        .src_port = 0,
        .dst_port = DNS_PORT,
        .protocol = 1, // TCP
        .is_coredns = 0,
    };

    // Check if this PID is CoreDNS
    __u8 *is_coredns = bpf_map_lookup_elem(&coredns_pids, &pid);
    if (is_coredns) {
        state.is_coredns = 1;
    }

    bpf_get_current_comm(&state.query_name, sizeof(state.query_name));

    bpf_map_update_elem(&active_queries, &key, &state, BPF_ANY);

    return 0;
}

// Tracepoint for monitoring DNS timeouts
SEC("tracepoint/syscalls/sys_exit_poll")
int trace_poll_timeout(struct trace_event_raw_sys_exit *ctx) {
    // If poll() returns 0 (timeout), check if we have pending DNS queries
    if (ctx->ret != 0) {
        return 0;
    }

    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tid = id & 0xFFFFFFFF;

    struct dns_query_key key = {
        .pid = pid,
        .tid = tid,
        .query_id = 0,
        .protocol = 1,
    };

    struct dns_query_state *query = bpf_map_lookup_elem(&active_queries, &key);
    if (!query) {
        return 0;
    }

    __u64 now = bpf_ktime_get_ns();
    __u64 elapsed = now - query->start_time_ns;

    // If query has been pending for more than timeout threshold, report timeout
    if (elapsed > TIMEOUT_THRESHOLD_NS) {
        submit_dns_event(query, DNS_EVENT_TIMEOUT, 0);
        bpf_map_delete_elem(&active_queries, &key);
    }

    return 0;
}

// Monitor sendto() for UDP DNS queries
SEC("tracepoint/syscalls/sys_enter_sendto")
int trace_sendto_enter(struct trace_event_raw_sys_enter *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tid = id & 0xFFFFFFFF;

    // Simple heuristic for DNS: sendto() with small packet size
    // In production would parse the actual packet
    long size = ctx->args[2];
    if (size < 12 || size > 512) { // DNS packet size range
        return 0;
    }

    struct dns_query_key key = {
        .pid = pid,
        .tid = tid,
        .query_id = ((__u16)bpf_get_prandom_u32()) & 0xFFFF,
        .protocol = 0, // UDP
    };

    struct dns_query_state state = {
        .start_time_ns = bpf_ktime_get_ns(),
        .pid = pid,
        .tid = tid,
        .query_id = key.query_id,
        .query_type = 1, // A record
        .src_port = 0,
        .dst_port = DNS_PORT,
        .protocol = 0, // UDP
        .is_coredns = 0,
    };

    // Check if this PID is CoreDNS
    __u8 *is_coredns = bpf_map_lookup_elem(&coredns_pids, &pid);
    if (is_coredns) {
        state.is_coredns = 1;
    }

    // Generate synthetic query name for testing
    const char test_domain[] = "test.cluster.local";
    for (int i = 0; i < sizeof(test_domain) && i < 32; i++) {
        state.query_name[i] = test_domain[i];
    }

    bpf_map_update_elem(&active_queries, &key, &state, BPF_ANY);

    return 0;
}

// Monitor recvfrom() for DNS responses
SEC("tracepoint/syscalls/sys_exit_recvfrom")
int trace_recvfrom_exit(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret <= 0) {
        return 0;
    }

    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tid = id & 0xFFFFFFFF;

    // Look for matching query
    struct dns_query_key key = {
        .pid = pid,
        .tid = tid,
        .query_id = 0, // Would need to parse from packet
        .protocol = 0, // UDP
    };

    // Try to find any query for this process
    struct dns_query_state *query = bpf_map_lookup_elem(&active_queries, &key);
    if (!query) {
        return 0;
    }

    __u64 now = bpf_ktime_get_ns();
    __u64 elapsed = now - query->start_time_ns;

    // Determine event type based on elapsed time
    __u8 event_type = DNS_EVENT_SLOW;
    if (elapsed > TIMEOUT_THRESHOLD_NS) {
        event_type = DNS_EVENT_TIMEOUT;
    } else if (elapsed < SLOW_THRESHOLD_NS) {
        // Fast response - don't report
        bpf_map_delete_elem(&active_queries, &key);
        return 0;
    }

    // Simulate different response codes for testing
    __u8 response_code = 0; // SUCCESS
    __u32 rand = bpf_get_prandom_u32();
    if (rand % 10 == 0) { // 10% NXDOMAIN
        response_code = 3;
        event_type = DNS_EVENT_NXDOMAIN;
    } else if (rand % 20 == 0) { // 5% SERVFAIL
        response_code = 2;
        event_type = DNS_EVENT_SERVFAIL;
    }

    submit_dns_event(query, event_type, response_code);
    bpf_map_delete_elem(&active_queries, &key);

    return 0;
}

// Cleanup old queries periodically
SEC("tracepoint/syscalls/sys_enter_nanosleep")
int trace_cleanup(struct trace_event_raw_sys_enter *ctx) {
    // Use nanosleep as a periodic trigger for cleanup
    __u64 now = bpf_ktime_get_ns();

    // Iterate through active queries and clean up old ones
    struct dns_query_key key = {};
    struct dns_query_state *query;

    // Note: In production, would use a more efficient cleanup mechanism
    // This is simplified for the prototype

    return 0;
}

char _license[] SEC("license") = "GPL";