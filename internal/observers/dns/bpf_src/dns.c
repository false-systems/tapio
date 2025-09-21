//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "../../bpf_common/core_helpers.h"

#define MAX_DNS_NAME_LEN 253
#define DNS_PORT 53
#define TASK_COMM_LEN 16

// DNS problem types
enum dns_problem_type {
    DNS_PROBLEM_SLOW = 1,
    DNS_PROBLEM_NXDOMAIN = 2,
    DNS_PROBLEM_SERVFAIL = 3,
    DNS_PROBLEM_TIMEOUT = 4,
    DNS_PROBLEM_REFUSED = 5,
    DNS_PROBLEM_TRUNCATED = 6,
};

// Configuration indexes
enum config_index {
    CONFIG_SLOW_THRESHOLD_NS = 0,
    CONFIG_TIMEOUT_NS = 1,
};

// DNS header flags
#define DNS_QR_MASK 0x8000  // Query/Response flag
#define DNS_RCODE_MASK 0x000F  // Response code mask

// DNS response codes
#define DNS_RCODE_NOERROR 0
#define DNS_RCODE_SERVFAIL 2
#define DNS_RCODE_NXDOMAIN 3
#define DNS_RCODE_REFUSED 5

// Event structure - must match Go struct
struct dns_event {
    __u64 timestamp;
    __u8 problem_type;
    __u8 pad1[7];
    __u64 latency_ns;

    // Query details
    char query_name[MAX_DNS_NAME_LEN];
    __u16 query_type;
    __u8 server_ip[16];

    // Process context
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    char comm[TASK_COMM_LEN];

    // Network context
    __u16 src_port;
    __u16 dst_port;

    // Error details
    __u8 response_code;
    __u8 retries;
    __u8 pad2[2];
} __attribute__((packed));

// DNS query tracking
struct dns_query_state {
    __u64 start_time;
    char query_name[MAX_DNS_NAME_LEN];
    __u16 query_type;
    __u16 query_id;
    __u8 server_ip[16];
    __u16 src_port;
    __u16 dst_port;
    __u8 retries;
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u64);  // PID + port combination
    __type(value, struct dns_query_state);
} active_queries SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4 * 1024 * 1024);  // 4MB ring buffer
} dns_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 2);
    __type(key, __u32);
    __type(value, __u64);
} config SEC(".maps");

// Helper to parse DNS query name from packet
// Currently unused but will be needed for parsing DNS packets
// static __always_inline int parse_dns_name(char *packet, int offset, char *name_out) {
//     #pragma unroll
//     for (int i = 0; i < MAX_DNS_NAME_LEN; i++) {
//         if (i >= 253) break;
//
//         __u8 len;
//         if (bpf_probe_read(&len, 1, packet + offset) < 0)
//             return -1;
//
//         if (len == 0) {
//             name_out[i] = '\0';
//             return offset + 1;
//         }
//
//         // Handle compression (not fully implemented)
//         if (len >= 0xC0) {
//             name_out[i] = '\0';
//             return offset + 2;
//         }
//
//         offset++;
//         if (i > 0) {
//             name_out[i-1] = '.';
//         }
//
//         if (bpf_probe_read(name_out + i, len, packet + offset) < 0)
//             return -1;
//
//         offset += len;
//     }
//     return offset;
// }

// Track DNS query on send
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;

    // Get socket info
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

    // Check if this is DNS (port 53)
    __u16 dport;
    BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport);
    dport = bpf_ntohs(dport);

    if (dport != DNS_PORT) {
        return 0;
    }

    // Get source port
    __u16 sport;
    BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num);

    // Create query state
    struct dns_query_state state = {};
    state.start_time = bpf_ktime_get_ns();
    state.src_port = sport;
    state.dst_port = dport;

    // Get destination IP
    __u32 daddr;
    BPF_CORE_READ_INTO(&daddr, sk, __sk_common.skc_daddr);
    state.server_ip[0] = daddr & 0xFF;
    state.server_ip[1] = (daddr >> 8) & 0xFF;
    state.server_ip[2] = (daddr >> 16) & 0xFF;
    state.server_ip[3] = (daddr >> 24) & 0xFF;

    // Store query state
    __u64 key = ((__u64)pid << 32) | sport;
    struct dns_query_state *existing = bpf_map_lookup_elem(&active_queries, &key);
    if (existing) {
        // This is a retry
        state.retries = existing->retries + 1;
        __builtin_memcpy(state.query_name, existing->query_name, MAX_DNS_NAME_LEN);
        state.query_type = existing->query_type;
    }

    bpf_map_update_elem(&active_queries, &key, &state, BPF_ANY);
    return 0;
}

// Check DNS response for problems
SEC("kprobe/udp_recvmsg")
int trace_udp_recvmsg(struct pt_regs *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 tid = pid_tgid;

    // Get socket info
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

    // Check if this is DNS response
    __u16 sport;
    BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num);

    // Look up query state
    __u64 key = ((__u64)pid << 32) | sport;
    struct dns_query_state *state = bpf_map_lookup_elem(&active_queries, &key);
    if (!state) {
        return 0;  // Not tracking this query
    }

    // Calculate latency
    __u64 now = bpf_ktime_get_ns();
    __u64 latency_ns = now - state->start_time;

    // Check thresholds
    __u32 config_key = CONFIG_SLOW_THRESHOLD_NS;
    __u64 *slow_threshold = bpf_map_lookup_elem(&config, &config_key);
    if (!slow_threshold) {
        return 0;
    }

    // Determine if this is a problem
    enum dns_problem_type problem_type = 0;

    if (latency_ns > *slow_threshold) {
        problem_type = DNS_PROBLEM_SLOW;
    }

    // For now, we only detect slow queries
    // Real implementation would parse DNS response for NXDOMAIN, SERVFAIL, etc.

    if (problem_type == 0) {
        // No problem detected, clean up and return
        bpf_map_delete_elem(&active_queries, &key);
        return 0;
    }

    // Create DNS event
    struct dns_event *event;
    event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    // Fill event data
    event->timestamp = now;
    event->problem_type = problem_type;
    event->latency_ns = latency_ns;

    // Copy query details
    __builtin_memcpy(event->query_name, state->query_name, MAX_DNS_NAME_LEN);
    event->query_type = state->query_type;
    __builtin_memcpy(event->server_ip, state->server_ip, 16);

    // Process info
    event->pid = pid;
    event->tid = tid;
    event->uid = bpf_get_current_uid_gid();
    event->gid = bpf_get_current_uid_gid() >> 32;
    event->cgroup_id = bpf_get_current_cgroup_id();
    bpf_get_current_comm(event->comm, sizeof(event->comm));

    // Network info
    event->src_port = state->src_port;
    event->dst_port = state->dst_port;
    event->retries = state->retries;

    // Submit event
    bpf_ringbuf_submit(event, 0);

    // Clean up
    bpf_map_delete_elem(&active_queries, &key);

    return 0;
}

// Timeout detection helper (called periodically from userspace)
SEC("kprobe/sys_getpid")
int check_timeouts(struct pt_regs *ctx) {
    // Get timeout threshold
    __u32 config_key = CONFIG_TIMEOUT_NS;
    __u64 *timeout_ns = bpf_map_lookup_elem(&config, &config_key);
    if (!timeout_ns) {
        return 0;
    }

    // This is a simplified timeout check
    // Real implementation would iterate through active_queries map
    // and detect queries that have exceeded timeout

    return 0;
}

char _license[] SEC("license") = "GPL";