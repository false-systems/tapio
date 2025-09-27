//go:build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/ipv6.h>
#include <linux/udp.h>
#include <linux/tcp.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define DNS_PORT 53
#define COREDNS_PORT 9153
#define MAX_DNS_NAME_LENGTH 253
#define MAX_MAP_ENTRIES 20480

// DNS header structure
struct dns_header {
    __u16 id;
    __u16 flags;
    __u16 qdcount;
    __u16 ancount;
    __u16 nscount;
    __u16 arcount;
};

// DNS query tracking
struct dns_query_key {
    __u32 pid;
    __u16 query_id;
    __u8 protocol; // 0=UDP, 1=TCP
    __u8 pad;
};

struct dns_query_state {
    __u64 timestamp_ns;
    __u32 pid;
    __u32 tid;
    __u16 query_id;
    __u16 query_type;
    __u8 query_name[MAX_DNS_NAME_LENGTH];
    __u8 server_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 is_coredns;
    __u32 tcp_seq;
    __u8 k8s_namespace[64];
};

// DNS event to userspace
struct dns_event {
    __u64 timestamp_ns;
    __u64 latency_ns;
    __u32 pid;
    __u32 tid;
    __u16 query_id;
    __u16 query_type;
    __u8 problem_type;
    __u8 response_code;
    __u8 query_name[MAX_DNS_NAME_LENGTH];
    __u8 server_ip[16];
    __u8 comm[16];
    __u8 protocol;
    __u8 is_coredns;
    __u16 tcp_flags;
    __u8 k8s_service[128];
    __u8 k8s_namespace[64];
    __u32 coredns_cache_hit;
};

// Configuration
struct dns_config {
    __u64 slow_threshold_ns;
    __u64 timeout_ns;
    __u16 coredns_port;
    __u8 enable_tcp;
    __u8 enable_k8s_enrichment;
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
    __uint(max_entries, 1 << 24); // 16MB
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

// Helper to parse DNS name from packet
static __always_inline int parse_dns_name(void *data, void *data_end, void *pos,
                                          __u8 *name, int max_len) {
    __u8 *p = pos;
    int i = 0;
    int label_len;

    #pragma unroll
    for (int j = 0; j < 64 && i < max_len - 1; j++) {
        if (p + 1 > data_end)
            return -1;

        label_len = *p;
        if (label_len == 0)
            break;

        // Handle compression (0xC0)
        if (label_len & 0xC0)
            break;

        p++;
        if (p + label_len > data_end)
            return -1;

        if (i > 0 && i < max_len - 1) {
            name[i++] = '.';
        }

        #pragma unroll
        for (int k = 0; k < label_len && i < max_len - 1; k++) {
            if (p + k >= data_end)
                break;
            name[i++] = p[k];
        }

        p += label_len;
    }

    name[i] = '\0';
    return i;
}

// Check if query is for Kubernetes service
static __always_inline bool is_k8s_query(__u8 *name) {
    // Look for .cluster.local or .svc patterns
    const char cluster_local[] = "cluster.local";
    const char svc[] = ".svc";

    int len = 0;
    #pragma unroll
    for (int i = 0; i < MAX_DNS_NAME_LENGTH && name[i]; i++) {
        len++;
    }

    // Simple pattern matching for K8s domains
    if (len > sizeof(cluster_local)) {
        bool match = true;
        #pragma unroll
        for (int i = 0; i < sizeof(cluster_local) - 1; i++) {
            if (name[len - sizeof(cluster_local) + 1 + i] != cluster_local[i]) {
                match = false;
                break;
            }
        }
        if (match) return true;
    }

    return false;
}

// UDP DNS query handler
SEC("socket/udp_sendmsg")
int trace_udp_sendmsg(struct socket *sock, struct msghdr *msg, size_t len) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u32 tid = bpf_get_current_pid_tgid();

    // Get configuration
    __u32 key = 0;
    struct dns_config *cfg = bpf_map_lookup_elem(&config, &key);
    if (!cfg)
        return 0;

    // Parse packet to check if it's DNS
    struct sockaddr_in *addr = (struct sockaddr_in *)msg->msg_name;
    if (!addr)
        return 0;

    __u16 dest_port = bpf_ntohs(addr->sin_port);
    if (dest_port != DNS_PORT && dest_port != cfg->coredns_port)
        return 0;

    // Track the DNS query
    struct dns_query_state state = {};
    state.timestamp_ns = bpf_ktime_get_ns();
    state.pid = pid;
    state.tid = tid;
    state.dst_port = dest_port;
    state.protocol = 0; // UDP

    // Check if this is CoreDNS
    __u8 *is_coredns = bpf_map_lookup_elem(&coredns_pids, &pid);
    if (is_coredns || dest_port == cfg->coredns_port) {
        state.is_coredns = 1;
    }

    // Store query state
    struct dns_query_key qkey = {
        .pid = pid,
        .query_id = 0, // Will be updated when we parse the packet
        .protocol = 0,
    };

    bpf_map_update_elem(&active_queries, &qkey, &state, BPF_ANY);

    return 0;
}

// UDP DNS response handler
SEC("socket/udp_recvmsg")
int trace_udp_recvmsg(struct socket *sock, struct msghdr *msg, size_t len,
                      int flags, int *addr_len) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;

    // Look up the corresponding query
    struct dns_query_key qkey = {
        .pid = pid,
        .query_id = 0, // Need to parse from packet
        .protocol = 0,
    };

    struct dns_query_state *query = bpf_map_lookup_elem(&active_queries, &qkey);
    if (!query)
        return 0;

    // Calculate latency
    __u64 now = bpf_ktime_get_ns();
    __u64 latency_ns = now - query->timestamp_ns;

    // Get configuration for thresholds
    __u32 cfg_key = 0;
    struct dns_config *cfg = bpf_map_lookup_elem(&config, &cfg_key);
    if (!cfg)
        return 0;

    // Determine if this is a problem
    __u8 problem_type = 0;
    if (latency_ns > cfg->timeout_ns) {
        problem_type = 4; // Timeout
    } else if (latency_ns > cfg->slow_threshold_ns) {
        problem_type = 1; // Slow
    }

    // Only report problems (not all queries)
    if (problem_type > 0) {
        // Reserve space in ring buffer
        struct dns_event *event = bpf_ringbuf_reserve(&dns_events,
                                                      sizeof(struct dns_event), 0);
        if (!event) {
            bpf_map_delete_elem(&active_queries, &qkey);
            return 0;
        }

        // Fill event
        event->timestamp_ns = now;
        event->latency_ns = latency_ns;
        event->pid = query->pid;
        event->tid = query->tid;
        event->query_id = query->query_id;
        event->query_type = query->query_type;
        event->problem_type = problem_type;
        event->protocol = 0; // UDP
        event->is_coredns = query->is_coredns;

        // Copy query name
        #pragma unroll
        for (int i = 0; i < MAX_DNS_NAME_LENGTH; i++) {
            event->query_name[i] = query->query_name[i];
        }

        // Get process name
        bpf_get_current_comm(&event->comm, sizeof(event->comm));

        // Check for K8s enrichment
        if (cfg->enable_k8s_enrichment && is_k8s_query(query->query_name)) {
            // Extract namespace from query name if possible
            // Format: service.namespace.svc.cluster.local
            // This is simplified - real implementation would parse properly
        }

        // Submit event
        bpf_ringbuf_submit(event, 0);
    }

    // Clean up query tracking
    bpf_map_delete_elem(&active_queries, &qkey);

    return 0;
}

// TCP DNS connection tracking
SEC("kprobe/tcp_connect")
int trace_tcp_connect(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

    __u16 dport = 0;
    bpf_probe_read_kernel(&dport, sizeof(dport), &sk->__sk_common.skc_dport);
    dport = bpf_ntohs(dport);

    // Check if this is DNS port
    if (dport != DNS_PORT && dport != COREDNS_PORT)
        return 0;

    // Track TCP DNS session
    __u32 pid = bpf_get_current_pid_tgid() >> 32;

    struct dns_query_state state = {};
    state.timestamp_ns = bpf_ktime_get_ns();
    state.pid = pid;
    state.tid = bpf_get_current_pid_tgid();
    state.dst_port = dport;
    state.protocol = 1; // TCP

    // Check if CoreDNS
    if (dport == COREDNS_PORT) {
        state.is_coredns = 1;
    }

    // Store with socket as key for TCP tracking
    // (simplified - real implementation would track properly)
    struct dns_query_key qkey = {
        .pid = pid,
        .query_id = 0,
        .protocol = 1,
    };

    bpf_map_update_elem(&active_queries, &qkey, &state, BPF_ANY);

    return 0;
}

// License
char _license[] SEC("license") = "GPL";