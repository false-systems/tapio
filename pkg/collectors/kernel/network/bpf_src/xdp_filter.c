// SPDX-License-Identifier: GPL-2.0
// XDP program for high-performance packet filtering and DDoS protection
// Features: Connection tracking, rate limiting, early drop for suspicious traffic

#include "../../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// Ethernet and IP protocol definitions
#define ETH_P_IP    0x0800
#define ETH_P_IPV6  0x86DD
#define ETH_HLEN    14

#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define IPPROTO_ICMP 1

// XDP actions
#define XDP_PASS     2
#define XDP_DROP     1
#define XDP_TX       3
#define XDP_REDIRECT 4

// Rate limiting thresholds
#define MAX_PACKETS_PER_SEC_PER_IP 1000
#define MAX_NEW_CONNECTIONS_PER_SEC 100
#define RATE_LIMIT_WINDOW_NS (1000000000ULL) // 1 second

// Connection states
#define CONN_STATE_NEW         0
#define CONN_STATE_ESTABLISHED 1
#define CONN_STATE_CLOSING     2

// CPU map value structure
struct bpf_cpumap_val {
    __u32 qsize; // Queue size for CPU
};

struct ethhdr {
    unsigned char h_dest[6];
    unsigned char h_source[6];
    __be16 h_proto;
} __attribute__((packed));

struct iphdr {
    __u8 ihl:4;
    __u8 version:4;
    __u8 tos;
    __be16 tot_len;
    __be16 id;
    __be16 frag_off;
    __u8 ttl;
    __u8 protocol;
    __sum16 check;
    __be32 saddr;
    __be32 daddr;
} __attribute__((packed));

struct ipv6hdr {
    __u8 priority:4,
         version:4;
    __u8 flow_lbl[3];
    __be16 payload_len;
    __u8 nexthdr;
    __u8 hop_limit;
    struct in6_addr saddr;
    struct in6_addr daddr;
} __attribute__((packed));

struct tcphdr {
    __be16 source;
    __be16 dest;
    __be32 seq;
    __be32 ack_seq;
    __u16 res1:4;
    __u16 doff:4;
    __u16 fin:1;
    __u16 syn:1;
    __u16 rst:1;
    __u16 psh:1;
    __u16 ack:1;
    __u16 urg:1;
    __u16 ece:1;
    __u16 cwr:1;
    __be16 window;
    __sum16 check;
    __be16 urg_ptr;
} __attribute__((packed));

struct udphdr {
    __be16 source;
    __be16 dest;
    __be16 len;
    __sum16 check;
} __attribute__((packed));

// Connection tracking entry
struct conn_track_entry {
    __u64 last_seen;
    __u64 packets;
    __u64 bytes;
    __u8 state;
    __u8 flags;
    __u16 _pad;
} __attribute__((packed));

// Rate limiting entry
struct rate_limit_entry {
    __u64 window_start;
    __u32 packet_count;
    __u32 drop_count;
} __attribute__((packed));

// Per-IP statistics
struct ip_stats {
    __u64 packets;
    __u64 bytes;
    __u64 syn_packets;
    __u64 last_packet_time;
    __u32 connections;
    __u32 _pad;
} __attribute__((packed));

// Maps for XDP processing
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 100000);
    __type(key, __u64); // Connection 5-tuple hash
    __type(value, struct conn_track_entry);
} connection_tracking SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 50000);
    __type(key, __u32); // Source IP
    __type(value, struct rate_limit_entry);
} rate_limiter SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_HASH);
    __uint(max_entries, 10000);
    __type(key, __u32); // IP address
    __type(value, struct ip_stats);
} ip_statistics SEC(".maps");

// Blocklist for malicious IPs
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u32); // IP address
    __type(value, __u64); // Block expiry time
} ip_blocklist SEC(".maps");

// Per-CPU packet counter for performance stats
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 4); // PASS, DROP, TX, REDIRECT
    __type(key, __u32);
    __type(value, __u64);
} xdp_stats SEC(".maps");

// CPU redirect map for load balancing
struct {
    __uint(type, BPF_MAP_TYPE_CPUMAP);
    __uint(max_entries, 256); // Support up to 256 CPUs
    __type(key, __u32);
    __type(value, struct bpf_cpumap_val);
} cpu_map SEC(".maps");

// Helper to create connection hash from 5-tuple
static __always_inline __u64 make_conn_hash(__u32 saddr, __u32 daddr, 
                                            __u16 sport, __u16 dport, __u8 proto)
{
    __u64 hash = saddr;
    hash = (hash << 16) ^ daddr;
    hash = (hash << 16) ^ (((__u32)sport << 16) | dport);
    hash = (hash << 8) ^ proto;
    return hash;
}

// Helper to update XDP statistics
static __always_inline void update_xdp_stats(__u32 action)
{
    __u64 *counter = bpf_map_lookup_elem(&xdp_stats, &action);
    if (counter)
        __sync_fetch_and_add(counter, 1);
}

// Rate limiting check
static __always_inline bool is_rate_limited(__u32 saddr)
{
    __u64 now = bpf_ktime_get_ns();
    struct rate_limit_entry *entry = bpf_map_lookup_elem(&rate_limiter, &saddr);
    
    if (!entry) {
        // Create new entry
        struct rate_limit_entry new_entry = {
            .window_start = now,
            .packet_count = 1,
            .drop_count = 0
        };
        bpf_map_update_elem(&rate_limiter, &saddr, &new_entry, BPF_NOEXIST);
        return false;
    }
    
    // Check if we're in a new window
    if (now - entry->window_start > RATE_LIMIT_WINDOW_NS) {
        entry->window_start = now;
        entry->packet_count = 1;
        entry->drop_count = 0;
        return false;
    }
    
    // Increment packet count
    entry->packet_count++;
    
    // Check if over limit
    if (entry->packet_count > MAX_PACKETS_PER_SEC_PER_IP) {
        entry->drop_count++;
        return true;
    }
    
    return false;
}

// Check if IP is blocklisted
static __always_inline bool is_blocked(__u32 addr)
{
    __u64 *expiry = bpf_map_lookup_elem(&ip_blocklist, &addr);
    if (!expiry)
        return false;
    
    __u64 now = bpf_ktime_get_ns();
    if (now > *expiry) {
        // Block expired, remove from list
        bpf_map_delete_elem(&ip_blocklist, &addr);
        return false;
    }
    
    return true;
}

// SYN flood detection
static __always_inline bool detect_syn_flood(__u32 saddr, struct tcphdr *tcp)
{
    if (!tcp->syn || tcp->ack)
        return false; // Not a SYN packet
    
    struct ip_stats *stats = bpf_map_lookup_elem(&ip_statistics, &saddr);
    if (!stats) {
        struct ip_stats new_stats = {
            .packets = 1,
            .syn_packets = 1,
            .last_packet_time = bpf_ktime_get_ns()
        };
        bpf_map_update_elem(&ip_statistics, &saddr, &new_stats, BPF_NOEXIST);
        return false;
    }
    
    __u64 now = bpf_ktime_get_ns();
    
    // Reset stats if window expired
    if (now - stats->last_packet_time > RATE_LIMIT_WINDOW_NS) {
        stats->syn_packets = 1;
        stats->last_packet_time = now;
        return false;
    }
    
    stats->syn_packets++;
    stats->last_packet_time = now;
    
    // Detect SYN flood: too many SYN packets in window
    if (stats->syn_packets > MAX_NEW_CONNECTIONS_PER_SEC) {
        // Add to blocklist for 60 seconds
        __u64 block_until = now + (60 * 1000000000ULL);
        bpf_map_update_elem(&ip_blocklist, &saddr, &block_until, BPF_ANY);
        return true;
    }
    
    return false;
}

// Main XDP program
SEC("xdp")
int xdp_packet_filter(struct xdp_md *ctx)
{
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    
    // Parse Ethernet header
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return XDP_DROP;
    
    // Only process IP packets
    if (eth->h_proto != bpf_htons(ETH_P_IP) && 
        eth->h_proto != bpf_htons(ETH_P_IPV6))
        return XDP_PASS;
    
    if (eth->h_proto == bpf_htons(ETH_P_IP)) {
        // Parse IPv4 header
        struct iphdr *ip = (void *)(eth + 1);
        if ((void *)(ip + 1) > data_end)
            return XDP_DROP;
        
        __u32 saddr = bpf_ntohl(ip->saddr);
        __u32 daddr = bpf_ntohl(ip->daddr);
        
        // Check blocklist first
        if (is_blocked(saddr)) {
            update_xdp_stats(XDP_DROP);
            return XDP_DROP;
        }
        
        // Apply rate limiting
        if (is_rate_limited(saddr)) {
            update_xdp_stats(XDP_DROP);
            return XDP_DROP;
        }
        
        // Process based on protocol
        if (ip->protocol == IPPROTO_TCP) {
            struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
            if ((void *)(tcp + 1) > data_end)
                return XDP_DROP;
            
            // Detect SYN flood
            if (detect_syn_flood(saddr, tcp)) {
                update_xdp_stats(XDP_DROP);
                return XDP_DROP;
            }
            
            // Connection tracking
            __u16 sport = bpf_ntohs(tcp->source);
            __u16 dport = bpf_ntohs(tcp->dest);
            __u64 conn_hash = make_conn_hash(saddr, daddr, sport, dport, IPPROTO_TCP);
            
            struct conn_track_entry *conn = bpf_map_lookup_elem(&connection_tracking, &conn_hash);
            if (!conn) {
                // New connection
                if (tcp->syn && !tcp->ack) {
                    struct conn_track_entry new_conn = {
                        .last_seen = bpf_ktime_get_ns(),
                        .packets = 1,
                        .bytes = bpf_ntohs(ip->tot_len),
                        .state = CONN_STATE_NEW,
                        .flags = 0
                    };
                    bpf_map_update_elem(&connection_tracking, &conn_hash, &new_conn, BPF_NOEXIST);
                }
            } else {
                // Update existing connection
                conn->last_seen = bpf_ktime_get_ns();
                conn->packets++;
                conn->bytes += bpf_ntohs(ip->tot_len);
                
                // State machine
                if (tcp->fin || tcp->rst) {
                    conn->state = CONN_STATE_CLOSING;
                } else if (conn->state == CONN_STATE_NEW && tcp->ack) {
                    conn->state = CONN_STATE_ESTABLISHED;
                }
            }
        } else if (ip->protocol == IPPROTO_UDP) {
            struct udphdr *udp = (void *)ip + (ip->ihl * 4);
            if ((void *)(udp + 1) > data_end)
                return XDP_DROP;
            
            // DNS amplification attack detection
            __u16 sport = bpf_ntohs(udp->source);
            __u16 dport = bpf_ntohs(udp->dest);
            
            // Block common amplification ports if source
            if (sport == 53 || sport == 123 || sport == 161) { // DNS, NTP, SNMP
                __u16 udp_len = bpf_ntohs(udp->len);
                if (udp_len > 512) { // Suspiciously large response
                    update_xdp_stats(XDP_DROP);
                    return XDP_DROP;
                }
            }
        }
    }
    
    update_xdp_stats(XDP_PASS);
    return XDP_PASS;
}

// XDP program for redirect to CPU (for complex processing)
SEC("xdp_redirect")
int xdp_redirect_cpu(struct xdp_md *ctx)
{
    // This program can redirect packets to specific CPUs for load balancing
    __u32 cpu = bpf_get_smp_processor_id();
    
    // Simple round-robin to next CPU (limit to available CPUs)
    __u32 target_cpu = (cpu + 1) % 256; // Use max CPU count constant
    
    // Check if CPU exists in map before redirect
    struct bpf_cpumap_val *cpu_val = bpf_map_lookup_elem(&cpu_map, &target_cpu);
    if (!cpu_val) {
        // Fallback to CPU 0 if target doesn't exist
        target_cpu = 0;
    }
    
    return bpf_redirect_map(&cpu_map, target_cpu, 0);
}

char LICENSE[] SEC("license") = "GPL";