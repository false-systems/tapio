// SPDX-License-Identifier: GPL-2.0
// Network monitoring eBPF program - connections, DNS, service mapping

#include "../../../bpf_common/vmlinux_minimal.h"
#include "../../../bpf_common/helpers.h"
#include "../../../bpf_common/bpf_stats.h"
#include "../../../bpf_common/bpf_filters.h"
#include "../../../bpf_common/bpf_batch.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// BPF map update flags
#ifndef BPF_ANY
#define BPF_ANY 0
#endif
#ifndef BPF_NOEXIST
#define BPF_NOEXIST 1
#endif
#ifndef BPF_EXIST
#define BPF_EXIST 2
#endif

// Network protocol constants
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// Network event types
#define EVENT_TYPE_NETWORK_CONN   5
#define EVENT_TYPE_NETWORK_ACCEPT 6
#define EVENT_TYPE_NETWORK_CLOSE  7
#define EVENT_TYPE_DNS_REQUEST    19
#define EVENT_TYPE_DNS_RESPONSE   20

// Network connection information
struct network_info {
    __u32 saddr;    // Source IP (IPv4)
    __u32 daddr;    // Destination IP (IPv4)
    __u16 sport;    // Source port
    __u16 dport;    // Destination port
    __u8 protocol;  // IPPROTO_TCP or IPPROTO_UDP
    __u8 state;     // Connection state
    __u8 direction; // 0=outgoing, 1=incoming
    __u8 _pad;      // Padding
} __attribute__((packed));

// Network event structure
struct network_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u32 data_len;        // Length of network data
    char comm[16];
    __u64 cgroup_id;
    char pod_uid[36];
    struct network_info net_info;
    union {
        struct {
            char query_name[128];
            __u16 query_type;
            __u16 query_class;
        } dns_request;
        struct {
            char response_name[128];
            __u32 resolved_ip;
            __u16 response_type;
            __u16 ttl;
        } dns_response;
        __u8 data[132];
    };
} __attribute__((packed));

// Service endpoint information for correlation
struct service_endpoint {
    char service_name[64];  // K8s service name
    char namespace[64];     // K8s namespace
    char cluster_ip[16];    // Service cluster IP
    __u16 port;            // Service port
    __u8 _pad[2];          // Padding
} __attribute__((packed));

// Maps for network monitoring
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB - production optimized
} network_events SEC(".maps");

// Track container processes
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u8);  // Flag
} container_pids SEC(".maps");

// Map service endpoints (IP:Port -> Service info)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);                    // Combined IP:Port as key
    __type(value, struct service_endpoint); // service info
} service_endpoints_map SEC(".maps");

// Map cgroup ID to pod information
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);      // cgroup ID
    __type(value, char[36]); // pod UID
} pod_uid_map SEC(".maps");

// Connection tracking for stateful monitoring
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 20480);
    __type(key, __u64);                // Connection 4-tuple hash
    __type(value, struct network_info); // Connection info
} connection_tracker SEC(".maps");

// Statistics tracking maps
DEFINE_BPF_STATS_MAP(network_stats, 4); // 4 probes: connect, accept, close, dns
DEFINE_GLOBAL_STATS_MAP(network_global_stats);

// Dynamic filtering maps
DEFINE_PID_FILTER_MAP(network_pid_filter);
DEFINE_NS_FILTER_MAP(network_ns_filter);
DEFINE_NET_FILTER_MAP(network_net_filter);
DEFINE_FILTER_CONFIG_MAP(network_filter_config);

// Batch processing map
DEFINE_BATCH_MAP(network_batch_buffer);

// Sampling configuration
struct sampling_config {
    __u32 sample_rate;     // Sampling rate (0-100%)
    __u32 sample_interval; // Sample every N events
    __u64 event_counter;   // Event counter for interval sampling
    __u8 enabled;          // Sampling enabled flag
    __u8 pad[3];
};

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct sampling_config);
} sampling_config_map SEC(".maps");

// Helper functions
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != 0;
}

// eBPF-based sampling using random number generation
static __always_inline bool should_sample_event_ebpf(void)
{
    __u32 key = 0;
    struct sampling_config *config = bpf_map_lookup_elem(&sampling_config_map, &key);
    
    if (!config || !config->enabled)
        return true; // Sample all if not configured
    
    // Probabilistic sampling using BPF random
    if (config->sample_rate > 0 && config->sample_rate < 100) {
        __u32 random = bpf_get_prandom_u32();
        return (random % 100) < config->sample_rate;
    }
    
    // Interval-based sampling
    if (config->sample_interval > 1) {
        __u64 current = __sync_fetch_and_add(&config->event_counter, 1);
        return (current % config->sample_interval) == 0;
    }
    
    return config->sample_rate >= 100; // Always sample if rate is 100%
}

static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    if (!task) {
        return 0;
    }

    struct css_set *css_set_ptr = NULL;
    int ret = bpf_core_read(&css_set_ptr, sizeof(css_set_ptr), &task->cgroups);
    if (ret != 0 || !css_set_ptr) {
        return 0;
    }

    struct cgroup_subsys_state *css = NULL;
    ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[0]);
    if (ret != 0 || !css) {
        return 0;
    }

    struct cgroup *cgroup_ptr = NULL;
    ret = bpf_core_read(&cgroup_ptr, sizeof(cgroup_ptr), &css->cgroup);
    if (ret != 0 || !cgroup_ptr) {
        return 0;
    }

    struct kernfs_node *kn = NULL;
    ret = bpf_core_read(&kn, sizeof(kn), &cgroup_ptr->kn);
    if (ret == 0 && kn) {
        __u64 ino = 0;
        ret = bpf_core_read(&ino, sizeof(ino), &kn->ino);
        if (ret == 0 && ino != 0) {
            return ino;
        }
    }

    return 0;
}

// Helper to create service endpoint key from IP and port
static __always_inline __u64 make_endpoint_key(__u32 ip, __u16 port)
{
    return ((__u64)ip << 16) | port;
}

// Helper to get service endpoint information
static __always_inline struct service_endpoint *get_service_endpoint(__u32 ip, __u16 port)
{
    __u64 key = make_endpoint_key(ip, port);
    return bpf_map_lookup_elem(&service_endpoints_map, &key);
}

// Helper to create connection tracking key
static __always_inline __u64 make_connection_key(struct network_info *net)
{
    // Create a hash from the 4-tuple
    __u64 key = ((__u64)net->saddr << 32) | net->daddr;
    key ^= ((__u64)net->sport << 16) | net->dport;
    key ^= (__u64)net->protocol << 48;
    return key;
}

// Network connection tracing - TCP connect
SEC("kprobe/tcp_v4_connect")
int trace_network_tcp_connect(struct pt_regs *ctx)
{
    // Record probe hit in statistics
    BPF_STATS_ENTER(&network_stats, 0);
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Get filter configuration
    struct filter_config *filter_cfg = BPF_FILTER_INIT(&network_filter_config);
    
    // Apply dynamic filtering
    __u64 ns_id = bpf_get_current_ns_id();
    
    if (filter_cfg) {
        // Check PID and namespace filters
        if (!bpf_filter_check_pid(&network_pid_filter, filter_cfg, pid)) {
            BPF_STATS_EXIT_ERROR(&network_stats, 0, STATS_EVENT_FILTERED);
            return 0;
        }
        if (!bpf_filter_check_namespace(&network_ns_filter, filter_cfg, ns_id)) {
            BPF_STATS_EXIT_ERROR(&network_stats, 0, STATS_EVENT_FILTERED);
            return 0;
        }
    } else {
        // Fall back to container process check
        if (!is_container_process(pid)) {
            BPF_STATS_EXIT_ERROR(&network_stats, 0, STATS_EVENT_FILTERED);
            return 0;
        }
    }
    
    // Apply eBPF-based sampling
    if (!should_sample_event_ebpf()) {
        BPF_STATS_EXIT_ERROR(&network_stats, 0, STATS_EVENT_SAMPLED);
        return 0;
    }
    
    // Get sock struct from first argument using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
    if (!sk) {
        BPF_STATS_EXIT_ERROR(&network_stats, 0, STATS_EVENT_ERROR);
        return 0;
    }
    
    // Create event
    struct network_event event = {};
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CONN;
    event->data_len = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill network info from socket using CO-RE
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    // Use CO-RE for portable socket field access
    __u16 sport = 0, dport = 0;
    __u32 saddr = 0, daddr = 0;
    
    BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num);
    BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport);
    BPF_CORE_READ_INTO(&saddr, sk, __sk_common.skc_rcv_saddr);
    BPF_CORE_READ_INTO(&daddr, sk, __sk_common.skc_daddr);
    
    event->net_info.sport = sport;
    event->net_info.dport = __builtin_bswap16(dport);
    event->net_info.saddr = saddr;
    event->net_info.daddr = daddr;
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 1; // Connecting
    
    // Add to connection tracker
    __u64 conn_key = make_connection_key(&event->net_info);
    bpf_map_update_elem(&connection_tracker, &conn_key, &event->net_info, BPF_ANY);
    
    // Get pod UID
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Network connection tracing - TCP accept (incoming)
SEC("kprobe/inet_csk_accept")
int trace_network_tcp_accept(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_ACCEPT;
    event->data_len = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Network info would be filled from accept() return value
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 1; // Incoming
    event->net_info.state = 2; // Established
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Network connection close tracing
SEC("kprobe/tcp_close")
int trace_network_tcp_close(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
    if (!sk)
        return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CLOSE;
    event->data_len = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill network info from socket using CO-RE
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    // Use CO-RE for portable socket field access
    __u16 sport = 0, dport = 0;
    __u32 saddr = 0, daddr = 0;
    
    BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num);
    BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport);
    BPF_CORE_READ_INTO(&saddr, sk, __sk_common.skc_rcv_saddr);
    BPF_CORE_READ_INTO(&daddr, sk, __sk_common.skc_daddr);
    
    event->net_info.sport = sport;
    event->net_info.dport = __builtin_bswap16(dport);
    event->net_info.saddr = saddr;
    event->net_info.daddr = daddr;
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.state = 3; // Closing
    
    // Remove from connection tracker
    __u64 conn_key = make_connection_key(&event->net_info);
    bpf_map_delete_elem(&connection_tracker, &conn_key);
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// UDP socket tracing - sendmsg (outgoing UDP)
SEC("kprobe/udp_sendmsg")
int trace_network_udp_send(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
    if (!sk)
        return 0;
    
    struct network_event *event = bpf_ringbuf_reserve(&network_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CONN;
    event->data_len = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill network info
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    struct sock_common sk_common = {};
    bpf_probe_read_kernel(&sk_common, sizeof(sk_common), &sk->__sk_common);
    
    event->net_info.sport = sk_common.skc_num;
    event->net_info.dport = __builtin_bswap16(sk_common.skc_dport);
    event->net_info.saddr = sk_common.skc_rcv_saddr;
    event->net_info.daddr = sk_common.skc_daddr;
    event->net_info.protocol = IPPROTO_UDP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 0; // Stateless
    
    // Check for DNS (port 53) - both source and destination
    if (event->net_info.dport == 53) {
        event->event_type = EVENT_TYPE_DNS_REQUEST;
        // DNS query parsing would be implemented here
        __builtin_memcpy(event->dns_request.query_name, "unknown.domain", 14);
        event->dns_request.query_type = 1; // A record
        event->dns_request.query_class = 1; // IN
    } else if (event->net_info.sport == 53) {
        // DNS response from server
        event->event_type = EVENT_TYPE_DNS_RESPONSE;
        __builtin_memcpy(event->dns_response.response_name, "unknown.domain", 14);
        event->dns_response.response_type = 1; // A record
        event->dns_response.resolved_ip = event->net_info.saddr; // Server IP
    }
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";