//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

char LICENSE[] SEC("license") = "GPL";

#define ETH_P_IP 0x0800
#define ETH_P_IPV6 0x86DD

// Packet statistics
struct packet_stats {
    u64 packets;
    u64 bytes;
    u64 dropped;
    u64 errors;
};

// Per-CPU stats for performance
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, u32);
    __type(value, struct packet_stats);
    __uint(max_entries, 256); // Support up to 256 protocols
} stats SEC(".maps");

// Blacklist map for dropping packets
struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __type(key, struct lpm_key);
    __type(value, u8);
    __uint(max_entries, 10000);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
    __uint(map_flags, BPF_F_NO_PREALLOC);
} blacklist SEC(".maps");

struct lpm_key {
    u32 prefixlen;
    u32 addr;
};

// Rate limiting map
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, u32); // Source IP
    __type(value, u64); // Packet count
    __uint(max_entries, 100000);
} rate_limit SEC(".maps");

// Connection tracking
struct flow_key {
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u8 protocol;
    u8 pad[3];
} __attribute__((packed));

struct flow_stats {
    u64 packets;
    u64 bytes;
    u64 first_seen;
    u64 last_seen;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __type(key, struct flow_key);
    __type(value, struct flow_stats);
    __uint(max_entries, 1000000);
} flows SEC(".maps");

// Helper to parse packet headers
static __always_inline int parse_packet(struct xdp_md *ctx, 
                                       struct ethhdr **eth,
                                       struct iphdr **ip,
                                       void **l4_hdr) {
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    
    // Parse Ethernet header
    *eth = data;
    if ((void *)(*eth + 1) > data_end)
        return -1;
    
    // Only handle IPv4 for now
    if ((*eth)->h_proto != bpf_htons(ETH_P_IP))
        return -1;
    
    // Parse IP header
    *ip = (void *)(*eth + 1);
    if ((void *)(*ip + 1) > data_end)
        return -1;
    
    // Validate IP header length
    if ((*ip)->ihl < 5)
        return -1;
    
    // Get L4 header
    *l4_hdr = (void *)(*ip) + ((*ip)->ihl * 4);
    
    return 0;
}

// Main XDP program
SEC("xdp")
int xdp_packet_monitor(struct xdp_md *ctx) {
    struct ethhdr *eth;
    struct iphdr *ip;
    void *l4_hdr;
    
    // Parse packet
    if (parse_packet(ctx, &eth, &ip, &l4_hdr) < 0) {
        return XDP_PASS;
    }
    
    // Check blacklist
    struct lpm_key key = {
        .prefixlen = 32,
        .addr = ip->saddr,
    };
    
    u8 *blocked = bpf_map_lookup_elem(&blacklist, &key);
    if (blocked && *blocked) {
        // Update drop stats
        u32 proto = ip->protocol;
        struct packet_stats *stats = bpf_map_lookup_elem(&stats, &proto);
        if (stats) {
            __sync_fetch_and_add(&stats->dropped, 1);
        }
        return XDP_DROP;
    }
    
    // Rate limiting
    u64 *pkt_count = bpf_map_lookup_elem(&rate_limit, &ip->saddr);
    if (pkt_count) {
        if (*pkt_count > 1000) { // More than 1000 packets
            return XDP_DROP;
        }
        __sync_fetch_and_add(pkt_count, 1);
    } else {
        u64 count = 1;
        bpf_map_update_elem(&rate_limit, &ip->saddr, &count, BPF_ANY);
    }
    
    // Update protocol stats
    u32 proto = ip->protocol;
    struct packet_stats *stats = bpf_map_lookup_elem(&stats, &proto);
    if (stats) {
        u64 pkt_size = (void *)(long)ctx->data_end - (void *)(long)ctx->data;
        __sync_fetch_and_add(&stats->packets, 1);
        __sync_fetch_and_add(&stats->bytes, pkt_size);
    }
    
    // Flow tracking for TCP/UDP
    if (ip->protocol == IPPROTO_TCP || ip->protocol == IPPROTO_UDP) {
        struct flow_key flow = {
            .src_ip = ip->saddr,
            .dst_ip = ip->daddr,
            .protocol = ip->protocol,
        };
        
        // Extract ports based on protocol
        if (ip->protocol == IPPROTO_TCP) {
            struct tcphdr *tcp = l4_hdr;
            if ((void *)(tcp + 1) > (void *)(long)ctx->data_end)
                goto skip_flow;
            flow.src_port = tcp->source;
            flow.dst_port = tcp->dest;
        } else {
            struct udphdr *udp = l4_hdr;
            if ((void *)(udp + 1) > (void *)(long)ctx->data_end)
                goto skip_flow;
            flow.src_port = udp->source;
            flow.dst_port = udp->dest;
        }
        
        // Update flow stats
        struct flow_stats *fstats = bpf_map_lookup_elem(&flows, &flow);
        if (fstats) {
            __sync_fetch_and_add(&fstats->packets, 1);
            __sync_fetch_and_add(&fstats->bytes, 
                               (void *)(long)ctx->data_end - (void *)(long)ctx->data);
            fstats->last_seen = bpf_ktime_get_ns();
        } else {
            struct flow_stats new_flow = {
                .packets = 1,
                .bytes = (void *)(long)ctx->data_end - (void *)(long)ctx->data,
                .first_seen = bpf_ktime_get_ns(),
                .last_seen = bpf_ktime_get_ns(),
            };
            bpf_map_update_elem(&flows, &flow, &new_flow, BPF_ANY);
        }
    }
    
skip_flow:
    return XDP_PASS;
}

// XDP program for DDoS mitigation
SEC("xdp/ddos_mitigate")
int xdp_ddos_mitigate(struct xdp_md *ctx) {
    struct ethhdr *eth;
    struct iphdr *ip;
    void *l4_hdr;
    
    // Parse packet
    if (parse_packet(ctx, &eth, &ip, &l4_hdr) < 0) {
        return XDP_PASS;
    }
    
    // Simple SYN flood mitigation for TCP
    if (ip->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = l4_hdr;
        if ((void *)(tcp + 1) > (void *)(long)ctx->data_end)
            return XDP_PASS;
        
        // Drop SYN packets if rate is too high
        if (tcp->syn && !tcp->ack) {
            u64 *syn_count = bpf_map_lookup_elem(&rate_limit, &ip->saddr);
            if (syn_count && *syn_count > 100) { // More than 100 SYN packets
                return XDP_DROP;
            }
        }
    }
    
    return XDP_PASS;
}

// Program to redirect packets to different CPUs
SEC("xdp/cpu_redirect")
int xdp_cpu_redirect(struct xdp_md *ctx) {
    struct ethhdr *eth;
    struct iphdr *ip;
    void *l4_hdr;
    
    // Parse packet
    if (parse_packet(ctx, &eth, &ip, &l4_hdr) < 0) {
        return XDP_PASS;
    }
    
    // Hash based on flow to ensure same flow goes to same CPU
    u32 hash = ip->saddr ^ ip->daddr;
    if (ip->protocol == IPPROTO_TCP || ip->protocol == IPPROTO_UDP) {
        struct udphdr *udp = l4_hdr;
        if ((void *)(udp + 1) <= (void *)(long)ctx->data_end) {
            hash ^= udp->source ^ udp->dest;
        }
    }
    
    // Redirect to CPU based on hash
    u32 cpu = hash % bpf_num_possible_cpus();
    return bpf_redirect_map(&cpu_map, cpu, 0);
}

// CPU map for XDP CPU redirect
struct {
    __uint(type, BPF_MAP_TYPE_CPUMAP);
    __uint(max_entries, 128); // Max CPUs
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u32));
} cpu_map SEC(".maps");