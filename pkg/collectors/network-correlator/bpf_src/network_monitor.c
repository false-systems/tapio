// SPDX-License-Identifier: GPL-2.0
// Network failure correlator - Track ONLY failures, ignore success!

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

// Event types - only failures matter
#define EVENT_TCP_SYN_TIMEOUT    1  // SYN sent, no SYN-ACK
#define EVENT_TCP_RESET          2  // Connection refused
#define EVENT_ARP_TIMEOUT        3  // ARP request, no reply
#define EVENT_ICMP_UNREACHABLE   4  // Host/port unreachable
#define EVENT_FIN_NO_ACK         5  // FIN sent, no ACK (half-closed)
#define EVENT_ORPHAN_ACK         6  // ACK without SYN (connection hijack?)
#define EVENT_ORPHAN_RST         7  // RST for unknown connection
#define EVENT_DUP_SYN            8  // Duplicate SYNs (retry storm)
#define EVENT_BLACK_HOLE         9  // Packets disappear (no response at all)
#define EVENT_WRONG_DIRECTION    10 // Packet flow in wrong direction
#define EVENT_TTL_EXPIRED        11 // Routing loops

// Failure codes
#define TIMEOUT_NO_RESPONSE      1
#define ARP_NO_RESPONSE          2
#define CONNECTION_REFUSED       3
#define HOST_UNREACHABLE         4
#define PORT_UNREACHABLE         5

// Ethernet protocol types
#define ETH_P_IP    0x0800
#define ETH_P_ARP   0x0806

// IP protocols
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define IPPROTO_ICMP 1

// TCP flags
#define TCP_FIN 0x01
#define TCP_SYN 0x02
#define TCP_RST 0x04
#define TCP_PSH 0x08
#define TCP_ACK 0x10

// Simple event structure - just facts, no analysis
struct network_event {
    __u64 timestamp;
    __u32 event_type;
    
    // L2 info
    __u8  src_mac[6];
    __u8  dst_mac[6];
    
    // L3/L4 info
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  tcp_flags;  // For TCP analysis
    
    // Failure info
    __u32 failure_code;  // RST, timeout, etc.
    __u64 duration_ns;   // How long until failure
    
    // Context
    __u64 cgroup_id;     // For pod correlation
    __u32 netns_id;      // Network namespace
    char  comm[16];      // Process name
} __attribute__((packed));

// Ring buffer for failures only (small!)
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4 * 1024 * 1024); // 4MB plenty for failures
} failure_events SEC(".maps");

// Track pending SYNs (to detect timeouts)
struct syn_attempt {
    __u64 timestamp;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  src_mac[6];
    __u8  dst_mac[6];
    __u64 cgroup_id;
    __u8  retry_count;  // Track retries for black hole detection
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);  // Only pending connections
    __type(key, __u64);   // Hash of 5-tuple
    __type(value, struct syn_attempt);
} pending_syns SEC(".maps");

// Track active connections (to detect orphan packets)
struct connection_state {
    __u64 established_time;
    __u8  state;  // SYN_SENT, ESTABLISHED, FIN_WAIT, etc.
    __u32 last_seq;
    __u32 last_ack;
    __u8  fin_sent;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 50000);  // Active connections
    __type(key, __u64);   // Connection hash
    __type(value, struct connection_state);
} active_connections SEC(".maps");

// Track pending ARPs (to detect L2 failures)
struct arp_request {
    __u64 timestamp;
    __u32 requester_ip;
    __u32 target_ip;
    __u8  requester_mac[6];
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 1000);  // ARPs are less frequent
    __type(key, __u32);   // Target IP
    __type(value, struct arp_request);
} pending_arps SEC(".maps");

// Helper: Generate connection hash for 5-tuple
static __always_inline __u64 conn_hash(__u32 sip, __u32 dip, __u16 sport, __u16 dport) {
    __u64 hash = sip;
    hash = (hash << 16) ^ dip;
    hash = (hash << 8) ^ sport;
    hash = (hash << 8) ^ dport;
    return hash;
}

// Helper: Emit failure event
static __always_inline void emit_failure_event(__u32 event_type, 
                                                __u32 src_ip, __u32 dst_ip,
                                                __u16 src_port, __u16 dst_port,
                                                __u8 *src_mac, __u8 *dst_mac,
                                                __u32 failure_code,
                                                __u64 duration_ns) {
    struct network_event *e = bpf_ringbuf_reserve(&failure_events, sizeof(*e), 0);
    if (!e)
        return;
    
    e->timestamp = bpf_ktime_get_ns();
    e->event_type = event_type;
    e->src_ip = src_ip;
    e->dst_ip = dst_ip;
    e->src_port = src_port;
    e->dst_port = dst_port;
    e->failure_code = failure_code;
    e->duration_ns = duration_ns;
    e->cgroup_id = bpf_get_current_cgroup_id();
    
    if (src_mac && dst_mac) {
        __builtin_memcpy(e->src_mac, src_mac, 6);
        __builtin_memcpy(e->dst_mac, dst_mac, 6);
    }
    
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    
    bpf_ringbuf_submit(e, 0);
}

// Main TCP tracking - captures failures only!
SEC("tc/ingress")
int track_tcp_failures(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    // Parse Ethernet header
    struct ethhdr {
        __u8 h_dest[6];
        __u8 h_source[6];
        __be16 h_proto;
    } __attribute__((packed));
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;
    
    // Only care about IP packets
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    // Parse IP header
    struct iphdr {
        __u8    ihl:4,
                version:4;
        __u8    tos;
        __be16  tot_len;
        __be16  id;
        __be16  frag_off;
        __u8    ttl;
        __u8    protocol;
        __sum16 check;
        __be32  saddr;
        __be32  daddr;
    } __attribute__((packed));
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    
    // Check for TTL expiry (routing loops)
    if (ip->ttl <= 1) {
        emit_failure_event(EVENT_TTL_EXPIRED, 
                          bpf_ntohl(ip->saddr), bpf_ntohl(ip->daddr),
                          0, 0, eth->h_source, eth->h_dest,
                          ip->ttl, 0);
    }
    
    // Only care about TCP
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;
    
    // Parse TCP header
    struct tcphdr {
        __be16  source;
        __be16  dest;
        __be32  seq;
        __be32  ack_seq;
        __u16   res1:4,
                doff:4,
                fin:1,
                syn:1,
                rst:1,
                psh:1,
                ack:1,
                urg:1,
                ece:1,
                cwr:1;
        __be16  window;
        __sum16 check;
        __be16  urg_ptr;
    } __attribute__((packed));
    
    __u32 tcp_header_offset = ip->ihl * 4;
    struct tcphdr *tcp = (void *)ip + tcp_header_offset;
    if ((void *)(tcp + 1) > data_end)
        return TC_ACT_OK;
    
    __u32 src_ip = bpf_ntohl(ip->saddr);
    __u32 dst_ip = bpf_ntohl(ip->daddr);
    __u16 src_port = bpf_ntohs(tcp->source);
    __u16 dst_port = bpf_ntohs(tcp->dest);
    
    __u64 hash = conn_hash(src_ip, dst_ip, src_port, dst_port);
    __u64 reverse_hash = conn_hash(dst_ip, src_ip, dst_port, src_port);
    
    // Track SYN packets (connection attempts)
    if (tcp->syn && !tcp->ack) {
        struct syn_attempt *existing = bpf_map_lookup_elem(&pending_syns, &hash);
        
        if (existing) {
            // Duplicate SYN - retry detected!
            existing->retry_count++;
            
            if (existing->retry_count > 3) {
                // Too many retries - black hole or severe packet loss
                __u64 duration = bpf_ktime_get_ns() - existing->timestamp;
                emit_failure_event(EVENT_DUP_SYN,
                                 src_ip, dst_ip, src_port, dst_port,
                                 eth->h_source, eth->h_dest,
                                 existing->retry_count, duration);
            }
        } else {
            // New SYN attempt
            struct syn_attempt attempt = {};
            attempt.timestamp = bpf_ktime_get_ns();
            attempt.src_ip = src_ip;
            attempt.dst_ip = dst_ip;
            attempt.src_port = src_port;
            attempt.dst_port = dst_port;
            __builtin_memcpy(attempt.src_mac, eth->h_source, 6);
            __builtin_memcpy(attempt.dst_mac, eth->h_dest, 6);
            attempt.cgroup_id = bpf_get_current_cgroup_id();
            attempt.retry_count = 0;
            
            bpf_map_update_elem(&pending_syns, &hash, &attempt, BPF_ANY);
        }
        
        // Also track as active connection
        struct connection_state conn = {};
        conn.established_time = bpf_ktime_get_ns();
        conn.state = 1; // SYN_SENT
        bpf_map_update_elem(&active_connections, &hash, &conn, BPF_ANY);
    }
    
    // SYN-ACK means connection succeeded - remove from tracking
    else if (tcp->syn && tcp->ack) {
        // Connection succeeded - we don't care!
        bpf_map_delete_elem(&pending_syns, &reverse_hash);
        
        // Update connection state
        struct connection_state *conn = bpf_map_lookup_elem(&active_connections, &reverse_hash);
        if (conn) {
            conn->state = 2; // ESTABLISHED
        }
    }
    
    // RST means connection refused or terminated
    else if (tcp->rst) {
        struct syn_attempt *pending = bpf_map_lookup_elem(&pending_syns, &reverse_hash);
        struct connection_state *conn = bpf_map_lookup_elem(&active_connections, &hash);
        
        if (pending) {
            // Connection refused (RST in response to SYN)
            __u64 duration = bpf_ktime_get_ns() - pending->timestamp;
            emit_failure_event(EVENT_TCP_RESET,
                             pending->src_ip, pending->dst_ip,
                             pending->src_port, pending->dst_port,
                             pending->src_mac, pending->dst_mac,
                             CONNECTION_REFUSED, duration);
            
            bpf_map_delete_elem(&pending_syns, &reverse_hash);
        } else if (!conn) {
            // Orphan RST - RST for unknown connection
            emit_failure_event(EVENT_ORPHAN_RST,
                             src_ip, dst_ip, src_port, dst_port,
                             eth->h_source, eth->h_dest,
                             0, 0);
        }
        
        // Clean up connection state
        bpf_map_delete_elem(&active_connections, &hash);
        bpf_map_delete_elem(&active_connections, &reverse_hash);
    }
    
    // FIN tracking for half-closed connections
    else if (tcp->fin) {
        struct connection_state *conn = bpf_map_lookup_elem(&active_connections, &hash);
        
        if (conn) {
            if (conn->fin_sent) {
                // Both sides sent FIN - connection closing normally
                bpf_map_delete_elem(&active_connections, &hash);
            } else {
                // First FIN - mark and wait for response
                conn->fin_sent = 1;
                conn->state = 4; // FIN_WAIT
            }
        } else {
            // FIN for unknown connection
            emit_failure_event(EVENT_FIN_NO_ACK,
                             src_ip, dst_ip, src_port, dst_port,
                             eth->h_source, eth->h_dest,
                             0, 0);
        }
    }
    
    // ACK without connection (orphan ACK)
    else if (tcp->ack && !tcp->syn) {
        struct connection_state *conn = bpf_map_lookup_elem(&active_connections, &hash);
        if (!conn && !bpf_map_lookup_elem(&active_connections, &reverse_hash)) {
            // ACK for completely unknown connection
            // This often happens after pod restarts
            emit_failure_event(EVENT_ORPHAN_ACK,
                             src_ip, dst_ip, src_port, dst_port,
                             eth->h_source, eth->h_dest,
                             0, 0);
        }
    }
    
    return TC_ACT_OK;
}

// Track ARP failures for L2 correlation
SEC("tc/ingress")
int track_arp_failures(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr {
        __u8 h_dest[6];
        __u8 h_source[6];
        __be16 h_proto;
    } __attribute__((packed));
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;
    
    // Only care about ARP
    if (bpf_ntohs(eth->h_proto) != ETH_P_ARP)
        return TC_ACT_OK;
    
    // ARP header
    struct arphdr {
        __be16 ar_hrd;      // Hardware type
        __be16 ar_pro;      // Protocol type
        __u8   ar_hln;      // Hardware address length
        __u8   ar_pln;      // Protocol address length
        __be16 ar_op;       // Operation (1=request, 2=reply)
        __u8   ar_sha[6];   // Sender hardware address
        __be32 ar_sip;      // Sender IP address
        __u8   ar_tha[6];   // Target hardware address
        __be32 ar_tip;      // Target IP address
    } __attribute__((packed));
    
    struct arphdr *arp = (void *)(eth + 1);
    if ((void *)(arp + 1) > data_end)
        return TC_ACT_OK;
    
    __u16 op = bpf_ntohs(arp->ar_op);
    __u32 target_ip = bpf_ntohl(arp->ar_tip);
    __u32 sender_ip = bpf_ntohl(arp->ar_sip);
    
    if (op == 1) {  // ARP Request
        struct arp_request req = {};
        req.timestamp = bpf_ktime_get_ns();
        req.requester_ip = sender_ip;
        req.target_ip = target_ip;
        __builtin_memcpy(req.requester_mac, arp->ar_sha, 6);
        
        bpf_map_update_elem(&pending_arps, &target_ip, &req, BPF_ANY);
    } else if (op == 2) {  // ARP Reply
        // Got reply - remove from pending
        bpf_map_delete_elem(&pending_arps, &sender_ip);
    }
    
    return TC_ACT_OK;
}

// Track ICMP errors (unreachable, etc)
SEC("tc/ingress")
int track_icmp_errors(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr {
        __u8 h_dest[6];
        __u8 h_source[6];
        __be16 h_proto;
    } __attribute__((packed));
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;
    
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    struct iphdr {
        __u8    ihl:4,
                version:4;
        __u8    tos;
        __be16  tot_len;
        __be16  id;
        __be16  frag_off;
        __u8    ttl;
        __u8    protocol;
        __sum16 check;
        __be32  saddr;
        __be32  daddr;
    } __attribute__((packed));
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_ICMP)
        return TC_ACT_OK;
    
    struct icmphdr {
        __u8  type;
        __u8  code;
        __sum16 checksum;
        union {
            struct {
                __be16 id;
                __be16 sequence;
            } echo;
            __be32 gateway;
            struct {
                __be16 __unused;
                __be16 mtu;
            } frag;
        } un;
    } __attribute__((packed));
    
    __u32 icmp_offset = ip->ihl * 4;
    struct icmphdr *icmp = (void *)ip + icmp_offset;
    if ((void *)(icmp + 1) > data_end)
        return TC_ACT_OK;
    
    // Track ICMP errors
    if (icmp->type == 3) {  // Destination Unreachable
        __u32 code = icmp->code;
        __u32 failure_code = HOST_UNREACHABLE;
        
        if (code == 3)  // Port unreachable
            failure_code = PORT_UNREACHABLE;
        
        emit_failure_event(EVENT_ICMP_UNREACHABLE,
                         bpf_ntohl(ip->saddr), bpf_ntohl(ip->daddr),
                         0, 0, eth->h_source, eth->h_dest,
                         failure_code, 0);
    }
    
    return TC_ACT_OK;
}

// Periodic cleanup (called from userspace timer)
// We detect timeouts in userspace to keep kernel code simple
SEC("perf_event")
int cleanup_stale_entries(struct bpf_perf_event_data *ctx) {
    // Userspace handles timeout detection
    // This is just a placeholder for potential future optimization
    return 0;
}

char LICENSE[] SEC("license") = "GPL";