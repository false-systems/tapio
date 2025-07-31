// SPDX-License-Identifier: GPL-2.0
// etcd eBPF monitor - observe etcd at kernel level

#include "headers/vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

char LICENSE[] SEC("license") = "GPL";

// TC action codes
#define TC_ACT_OK 0
#define TC_ACT_SHOT 2

// Ethernet protocol
#define ETH_P_IP 0x0800

// Event types
#define EVENT_NETWORK 1
#define EVENT_SYSCALL 2
#define EVENT_FILE_OP 3

// Etcd ports
#define ETCD_CLIENT_PORT 2379
#define ETCD_PEER_PORT 2380

// Max sizes
#define MAX_KEY_SIZE 64  // Reduced to fit in BPF stack
#define MAX_DATA_SIZE 512

// Event structure sent to userspace
struct etcd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u8  type;
    __u8  operation; // GET=1, PUT=2, DELETE=3, WATCH=4, LEASE=5, TXN=6
    __u16 latency_ms;
    
    // Network info
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    
    // Operation details
    __u32 key_size;
    __u32 value_size;
    char  key[MAX_KEY_SIZE];
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB buffer
} events SEC(".maps");

// Track ongoing operations for latency measurement
struct op_state {
    __u64 start_ns;
    __u8  operation;
    __u32 key_size;
    char  key[MAX_KEY_SIZE];
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u64); // pid_tgid
    __type(value, struct op_state);
    __uint(max_entries, 10240);
} operation_state SEC(".maps");

// Helper to check if process is etcd
static __always_inline bool is_etcd_process(struct task_struct *task)
{
    char comm[16];
    bpf_probe_read_kernel_str(&comm, sizeof(comm), &task->comm);
    
    // Check if process name contains "etcd"
    return (comm[0] == 'e' && comm[1] == 't' && comm[2] == 'c' && comm[3] == 'd');
}

// Parse gRPC/HTTP2 frame to extract etcd operation
static __always_inline __u8 parse_etcd_operation(void *data, __u32 len)
{
    // Simplified parsing - in real implementation would parse gRPC properly
    // Look for method names in the frame
    char buf[32];
    
    if (len < 32) return 0;
    
    bpf_probe_read_kernel(&buf, sizeof(buf), data);
    
    // Check for common etcd operations
    if (buf[0] == '/' && buf[1] == 'e' && buf[2] == 't' && buf[3] == 'c') {
        if (buf[9] == 'R' && buf[10] == 'a' && buf[11] == 'n' && buf[12] == 'g' && buf[13] == 'e') {
            return 1; // GET/Range
        } else if (buf[9] == 'P' && buf[10] == 'u' && buf[11] == 't') {
            return 2; // PUT
        } else if (buf[9] == 'D' && buf[10] == 'e' && buf[11] == 'l') {
            return 3; // DELETE
        } else if (buf[9] == 'W' && buf[10] == 'a' && buf[11] == 't') {
            return 4; // WATCH
        } else if (buf[9] == 'L' && buf[10] == 'e' && buf[11] == 'a') {
            return 5; // LEASE
        } else if (buf[9] == 'T' && buf[10] == 'x' && buf[11] == 'n') {
            return 6; // TXN
        }
    }
    return 0;
}

// Network packet capture using TC
SEC("tc")
int tc_etcd_monitor(struct __sk_buff *skb)
{
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    // Minimum size check for ethernet + IP + TCP headers
    if (data + sizeof(struct ethhdr) + sizeof(struct iphdr) + sizeof(struct tcphdr) > data_end)
        return TC_ACT_OK;
    
    struct ethhdr *eth = data;
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    struct iphdr *ip = data + sizeof(*eth);
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;
    
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    __u16 dest_port = bpf_ntohs(tcp->dest);
    __u16 src_port = bpf_ntohs(tcp->source);
    
    // Check if it's etcd traffic
    if (dest_port != ETCD_CLIENT_PORT && dest_port != ETCD_PEER_PORT &&
        src_port != ETCD_CLIENT_PORT && src_port != ETCD_PEER_PORT)
        return TC_ACT_OK;
    
    // Get payload
    void *payload = (void *)tcp + (tcp->doff * 4);
    __u32 payload_len = data_end - payload;
    
    if (payload_len < 10)
        return TC_ACT_OK;
    
    // Try to parse etcd operation
    __u8 op = parse_etcd_operation(payload, payload_len);
    if (op == 0)
        return TC_ACT_OK;
    
    // Create event
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return TC_ACT_OK;
    
    e->timestamp = bpf_ktime_get_ns();
    e->type = EVENT_NETWORK;
    e->operation = op;
    e->src_ip = ip->saddr;
    e->dst_ip = ip->daddr;
    e->src_port = src_port;
    e->dst_port = dest_port;
    
    // Extract key if possible (simplified)
    if (payload_len > 50) {
        // Skip gRPC headers and try to find key
        char *key_start = payload + 40; // Approximate offset
        __u32 max_len = payload_len - 40;
        if (max_len > MAX_KEY_SIZE) max_len = MAX_KEY_SIZE;
        
        bpf_probe_read_kernel(e->key, max_len, key_start);
        e->key_size = max_len;
    }
    
    // Store operation state for latency tracking
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    struct op_state *state = bpf_map_lookup_elem(&operation_state, &pid_tgid);
    if (!state) {
        struct op_state new_state = {};
        new_state.start_ns = e->timestamp;
        new_state.operation = op;
        new_state.key_size = e->key_size;
        bpf_map_update_elem(&operation_state, &pid_tgid, &new_state, BPF_ANY);
    }
    
    bpf_ringbuf_submit(e, 0);
    return TC_ACT_OK;
}

// Trace write syscalls from etcd
SEC("tp/syscalls/sys_enter_write")
int trace_etcd_write(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!is_etcd_process(task))
        return 0;
    
    // Check if we have an ongoing operation
    struct op_state *state = bpf_map_lookup_elem(&operation_state, &pid_tgid);
    if (!state)
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tid = (__u32)pid_tgid;
    e->type = EVENT_SYSCALL;
    e->operation = state->operation;
    e->latency_ms = (e->timestamp - state->start_ns) / 1000000;
    
    // Copy key from state
    e->key_size = state->key_size;
    __builtin_memcpy(e->key, state->key, MAX_KEY_SIZE);
    
    bpf_ringbuf_submit(e, 0);
    
    // Clean up state
    bpf_map_delete_elem(&operation_state, &pid_tgid);
    
    return 0;
}

// Trace fsync for WAL operations
SEC("tp/syscalls/sys_enter_fsync")
int trace_etcd_fsync(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!is_etcd_process(task))
        return 0;
    
    struct etcd_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid >> 32;
    e->tid = (__u32)pid_tgid;
    e->type = EVENT_FILE_OP;
    e->operation = 7; // Special op for WAL sync
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}