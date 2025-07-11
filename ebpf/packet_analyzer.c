#include <linux/bpf.h>
#include <linux/in.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/ptrace.h>
#include <linux/skbuff.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_ENTRIES 8192
#define LATENCY_THRESHOLD_US 1000  // 1ms threshold

// Packet analysis event types
enum packet_event_type {
    PKT_LOSS = 1,
    PKT_HIGH_LATENCY = 2,
    PKT_REORDER = 3,
    PKT_DUPLICATE = 4,
    PKT_CORRUPTION = 5,
};

// Packet flow tracking
struct flow_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
};

struct flow_stats {
    __u64 start_time;
    __u64 last_seen;
    __u64 packets_sent;
    __u64 packets_recv;
    __u64 bytes_sent;
    __u64 bytes_recv;
    __u64 packets_lost;
    __u64 packets_reordered;
    __u64 packets_duplicated;
    __u32 rtt_min;
    __u32 rtt_max;
    __u32 rtt_sum;
    __u32 rtt_count;
    __u32 last_seq;
    __u16 expected_seq;
};

// Packet event structure
struct packet_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 event_type;
    __u32 latency_us;
    __u32 packet_size;
    __u32 sequence_num;
    __u32 ack_num;
    __u16 window_size;
    __u8 tcp_flags;
    char comm[16];
    char interface[16];
};

// Timestamp tracking for latency calculation
struct packet_timestamp {
    __u64 tx_time;
    __u32 seq_num;
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct flow_key);
    __type(value, struct flow_stats);
} flow_tracker SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} packet_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct flow_key);
    __type(value, struct packet_timestamp);
} packet_timestamps SEC(".maps");

// Helper function to get flow key
static void get_flow_key(struct flow_key *key, __u32 src_ip, __u32 dst_ip,
                        __u16 src_port, __u16 dst_port, __u8 protocol) {
    key->src_ip = src_ip;
    key->dst_ip = dst_ip;
    key->src_port = src_port;
    key->dst_port = dst_port;
    key->protocol = protocol;
}

// Helper function to emit packet event
static void emit_packet_event(__u8 event_type, __u32 pid, __u32 tgid,
                             __u32 src_ip, __u32 dst_ip, __u16 src_port, __u16 dst_port,
                             __u8 protocol, __u32 latency_us, __u32 packet_size,
                             __u32 seq_num, __u32 ack_num, __u16 window_size, __u8 tcp_flags) {
    struct packet_event *event;
    
    event = bpf_ringbuf_reserve(&packet_events, sizeof(*event), 0);
    if (!event)
        return;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = tgid;
    event->src_ip = src_ip;
    event->dst_ip = dst_ip;
    event->src_port = src_port;
    event->dst_port = dst_port;
    event->protocol = protocol;
    event->event_type = event_type;
    event->latency_us = latency_us;
    event->packet_size = packet_size;
    event->sequence_num = seq_num;
    event->ack_num = ack_num;
    event->window_size = window_size;
    event->tcp_flags = tcp_flags;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
}

// Parse IP and TCP headers
static int parse_tcp_packet(struct sk_buff *skb, struct flow_key *flow,
                           __u32 *seq_num, __u32 *ack_num, __u16 *window_size, __u8 *tcp_flags) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)eth + sizeof(*eth) > data_end)
        return -1;
    
    if (eth->h_proto != __builtin_bswap16(ETH_P_IP))
        return -1;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return -1;
    
    if (ip->protocol != IPPROTO_TCP)
        return -1;
    
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    if ((void *)tcp + sizeof(*tcp) > data_end)
        return -1;
    
    flow->src_ip = ip->saddr;
    flow->dst_ip = ip->daddr;
    flow->src_port = tcp->source;
    flow->dst_port = tcp->dest;
    flow->protocol = IPPROTO_TCP;
    
    *seq_num = __builtin_bswap32(tcp->seq);
    *ack_num = __builtin_bswap32(tcp->ack_seq);
    *window_size = __builtin_bswap16(tcp->window);
    *tcp_flags = tcp->fin | (tcp->syn << 1) | (tcp->rst << 2) | (tcp->psh << 3) |
                 (tcp->ack << 4) | (tcp->urg << 5);
    
    return 0;
}

// Track outgoing packets for latency measurement
SEC("tc")
int tc_egress(struct __sk_buff *skb) {
    struct flow_key flow;
    __u32 seq_num, ack_num;
    __u16 window_size;
    __u8 tcp_flags;
    
    if (parse_tcp_packet((struct sk_buff *)skb, &flow, &seq_num, &ack_num, &window_size, &tcp_flags) < 0)
        return TC_ACT_OK;
    
    // Store timestamp for outgoing packets
    struct packet_timestamp ts = {
        .tx_time = bpf_ktime_get_ns(),
        .seq_num = seq_num,
    };
    
    bpf_map_update_elem(&packet_timestamps, &flow, &ts, BPF_ANY);
    
    // Update flow statistics
    struct flow_stats *stats = bpf_map_lookup_elem(&flow_tracker, &flow);
    if (!stats) {
        struct flow_stats new_stats = {
            .start_time = bpf_ktime_get_ns(),
            .last_seen = bpf_ktime_get_ns(),
            .packets_sent = 1,
            .bytes_sent = skb->len,
            .rtt_min = UINT32_MAX,
            .last_seq = seq_num,
        };
        bpf_map_update_elem(&flow_tracker, &flow, &new_stats, BPF_ANY);
    } else {
        stats->last_seen = bpf_ktime_get_ns();
        stats->packets_sent++;
        stats->bytes_sent += skb->len;
        stats->last_seq = seq_num;
    }
    
    return TC_ACT_OK;
}

// Track incoming packets for latency and loss detection
SEC("tc")
int tc_ingress(struct __sk_buff *skb) {
    struct flow_key flow, reverse_flow;
    __u32 seq_num, ack_num;
    __u16 window_size;
    __u8 tcp_flags;
    
    if (parse_tcp_packet((struct sk_buff *)skb, &flow, &seq_num, &ack_num, &window_size, &tcp_flags) < 0)
        return TC_ACT_OK;
    
    // Create reverse flow key for lookup
    reverse_flow.src_ip = flow.dst_ip;
    reverse_flow.dst_ip = flow.src_ip;
    reverse_flow.src_port = flow.dst_port;
    reverse_flow.dst_port = flow.src_port;
    reverse_flow.protocol = flow.protocol;
    
    // Check if this is an ACK for an outgoing packet
    if (tcp_flags & (1 << 4)) { // ACK flag
        struct packet_timestamp *ts = bpf_map_lookup_elem(&packet_timestamps, &reverse_flow);
        if (ts && ack_num > ts->seq_num) {
            __u64 rtt_ns = bpf_ktime_get_ns() - ts->tx_time;
            __u32 rtt_us = rtt_ns / 1000;
            
            // Update flow RTT statistics
            struct flow_stats *stats = bpf_map_lookup_elem(&flow_tracker, &reverse_flow);
            if (stats) {
                if (rtt_us < stats->rtt_min)
                    stats->rtt_min = rtt_us;
                if (rtt_us > stats->rtt_max)
                    stats->rtt_max = rtt_us;
                
                stats->rtt_sum += rtt_us;
                stats->rtt_count++;
                
                // Check for high latency
                if (rtt_us > LATENCY_THRESHOLD_US) {
                    __u64 id = bpf_get_current_pid_tgid();
                    __u32 pid = id >> 32;
                    __u32 tgid = id;
                    
                    emit_packet_event(PKT_HIGH_LATENCY, pid, tgid,
                                     reverse_flow.src_ip, reverse_flow.dst_ip,
                                     reverse_flow.src_port, reverse_flow.dst_port,
                                     reverse_flow.protocol, rtt_us, skb->len,
                                     seq_num, ack_num, window_size, tcp_flags);
                }
            }
            
            // Clean up timestamp entry
            bpf_map_delete_elem(&packet_timestamps, &reverse_flow);
        }
    }
    
    // Check for packet reordering
    struct flow_stats *stats = bpf_map_lookup_elem(&flow_tracker, &flow);
    if (stats) {
        if (seq_num < stats->last_seq) {
            // Potential reordering detected
            stats->packets_reordered++;
            
            __u64 id = bpf_get_current_pid_tgid();
            __u32 pid = id >> 32;
            __u32 tgid = id;
            
            emit_packet_event(PKT_REORDER, pid, tgid,
                             flow.src_ip, flow.dst_ip,
                             flow.src_port, flow.dst_port,
                             flow.protocol, 0, skb->len,
                             seq_num, ack_num, window_size, tcp_flags);
        }
        
        stats->packets_recv++;
        stats->bytes_recv += skb->len;
        stats->last_seen = bpf_ktime_get_ns();
    }
    
    return TC_ACT_OK;
}

// Track packet drops in network stack
SEC("tracepoint/skb/kfree_skb")
int trace_packet_drop(struct trace_event_raw_kfree_skb *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    
    // Extract packet information if possible
    struct sk_buff *skb = (struct sk_buff *)ctx->skbaddr;
    if (!skb)
        return 0;
    
    // This is a simplified version - in reality we'd parse the packet
    emit_packet_event(PKT_LOSS, pid, tgid, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0);
    
    return 0;
}

// Track socket buffer allocation failures
SEC("kprobe/alloc_skb")
int alloc_skb_entry(struct pt_regs *ctx) {
    return 0;
}

SEC("kretprobe/alloc_skb")
int alloc_skb_exit(struct pt_regs *ctx) {
    void *ret = (void *)PT_REGS_RC(ctx);
    
    if (!ret) {
        // Allocation failed - potential memory pressure
        __u64 id = bpf_get_current_pid_tgid();
        __u32 pid = id >> 32;
        __u32 tgid = id;
        
        emit_packet_event(PKT_LOSS, pid, tgid, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0);
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";