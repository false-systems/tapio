// SPDX-License-Identifier: GPL-2.0
// TC eBPF classifier for advanced packet manipulation and service mesh integration
// Features: L7 protocol detection, request routing, latency injection for chaos engineering

#include "../../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// TC actions
#define TC_ACT_OK       0
#define TC_ACT_SHOT     2  // Drop packet
#define TC_ACT_REDIRECT 7
#define TC_ACT_PIPE     3  // Continue to next filter

// Packet directions
#define TC_INGRESS 0
#define TC_EGRESS  1

// L7 Protocol identifiers
#define L7_PROTO_HTTP    1
#define L7_PROTO_GRPC    2
#define L7_PROTO_MYSQL   3
#define L7_PROTO_REDIS   4
#define L7_PROTO_KAFKA   5
#define L7_PROTO_POSTGRES 6

// Service mesh headers
#define ENVOY_HEADER_LEN 32
#define ISTIO_TRACE_ID_LEN 32

// Maximum payload inspection size
#define MAX_PAYLOAD_SIZE 256

struct packet_meta {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8 protocol;
    __u8 l7_protocol;
    __u16 flags;
    __u64 timestamp;
    __u32 seq_num;
    __u32 ack_num;
    char trace_id[ISTIO_TRACE_ID_LEN];
} __attribute__((packed));

// Service endpoint information for routing
struct service_route {
    __u32 backend_ip;
    __u16 backend_port;
    __u16 weight;        // For weighted load balancing
    __u32 version;       // Service version for canary deployments
    __u8 active;        // Is this backend active?
    __u8 _pad[3];
} __attribute__((packed));

// L7 protocol signature
struct protocol_signature {
    __u8 signature[16];  // Protocol signature bytes
    __u8 sig_len;       // Length of signature
    __u8 offset;        // Offset in payload to check
    __u8 l7_protocol;   // Protocol ID if matched
    __u8 _pad;
} __attribute__((packed));

// Per-flow statistics
struct flow_stats {
    __u64 packets;
    __u64 bytes;
    __u64 first_seen;
    __u64 last_seen;
    __u64 total_latency;  // Cumulative latency in nanoseconds
    __u32 retransmits;
    __u32 errors;
} __attribute__((packed));

// Maps for TC classifier
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u64);  // Service endpoint key (IP:Port)
    __type(value, struct service_route[4]); // Multiple backends per service
} service_routes SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 16);
    __type(key, __u32);
    __type(value, struct protocol_signature);
} protocol_signatures SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 50000);
    __type(key, __u64);  // Flow hash
    __type(value, struct flow_stats);
} flow_statistics SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 8);
    __type(key, __u32);
    __type(value, __u64);  // Counter for different L7 protocols
} l7_protocol_counts SEC(".maps");

// Chaos engineering: latency injection configuration
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 100);
    __type(key, __u32);   // Service ID
    __type(value, __u32); // Latency in microseconds (0 = disabled)
} latency_injection SEC(".maps");

// Circuit breaker state
struct circuit_breaker {
    __u32 failure_count;
    __u32 success_count;
    __u64 last_failure_time;
    __u8 state; // 0=closed, 1=open, 2=half-open
    __u8 _pad[3];
} __attribute__((packed));

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1000);
    __type(key, __u32);  // Backend IP
    __type(value, struct circuit_breaker);
} circuit_breakers SEC(".maps");

// Helper to detect HTTP traffic
static __always_inline bool is_http_request(void *data, void *data_end)
{
    // Check for common HTTP methods
    if (data + 16 > data_end)
        return false;
    
    char *payload = data;
    
    // Check for GET, POST, PUT, DELETE, HEAD, OPTIONS
    if ((payload[0] == 'G' && payload[1] == 'E' && payload[2] == 'T') ||
        (payload[0] == 'P' && payload[1] == 'O' && payload[2] == 'S' && payload[3] == 'T') ||
        (payload[0] == 'P' && payload[1] == 'U' && payload[2] == 'T') ||
        (payload[0] == 'D' && payload[1] == 'E' && payload[2] == 'L') ||
        (payload[0] == 'H' && payload[1] == 'E' && payload[2] == 'A' && payload[3] == 'D'))
        return true;
    
    // Check for HTTP response
    if (payload[0] == 'H' && payload[1] == 'T' && payload[2] == 'T' && payload[3] == 'P')
        return true;
    
    return false;
}

// Helper to detect gRPC traffic (HTTP/2 with specific headers)
static __always_inline bool is_grpc_traffic(void *data, void *data_end)
{
    if (data + 9 > data_end)
        return false;
    
    char *payload = data;
    
    // HTTP/2 magic preface for gRPC
    if (payload[0] == 'P' && payload[1] == 'R' && payload[2] == 'I' &&
        payload[3] == ' ' && payload[4] == '*' && payload[5] == ' ' &&
        payload[6] == 'H' && payload[7] == 'T' && payload[8] == 'T')
        return true;
    
    return false;
}

// Helper to detect L7 protocol
static __always_inline __u8 detect_l7_protocol(void *data, void *data_end, __u16 dst_port)
{
    // Quick port-based detection first
    switch (dst_port) {
        case 80:
        case 8080:
        case 8081:
            if (is_http_request(data, data_end))
                return L7_PROTO_HTTP;
            break;
        case 50051: // Common gRPC port
            if (is_grpc_traffic(data, data_end))
                return L7_PROTO_GRPC;
            break;
        case 3306:
            return L7_PROTO_MYSQL;
        case 6379:
            return L7_PROTO_REDIS;
        case 9092:
            return L7_PROTO_KAFKA;
        case 5432:
            return L7_PROTO_POSTGRES;
    }
    
    // Signature-based detection
    #pragma unroll
    for (int i = 0; i < 8; i++) {
        struct protocol_signature *sig = bpf_map_lookup_elem(&protocol_signatures, &i);
        if (!sig || sig->sig_len == 0)
            continue;
        
        if (data + sig->offset + sig->sig_len > data_end)
            continue;
        
        char *payload = data + sig->offset;
        bool match = true;
        
        #pragma unroll
        for (int j = 0; j < 16 && j < sig->sig_len; j++) {
            if (payload[j] != sig->signature[j]) {
                match = false;
                break;
            }
        }
        
        if (match)
            return sig->l7_protocol;
    }
    
    return 0; // Unknown
}

// Helper to apply circuit breaker logic
static __always_inline bool is_circuit_open(__u32 backend_ip)
{
    struct circuit_breaker *cb = bpf_map_lookup_elem(&circuit_breakers, &backend_ip);
    if (!cb)
        return false;
    
    if (cb->state == 1) { // Open state
        __u64 now = bpf_ktime_get_ns();
        // Check if we should transition to half-open (after 30 seconds)
        if (now - cb->last_failure_time > 30000000000ULL) {
            cb->state = 2; // Half-open
            cb->success_count = 0;
            cb->failure_count = 0;
        } else {
            return true; // Circuit still open
        }
    }
    
    return false;
}

// Helper to select backend using weighted round-robin
static __always_inline struct service_route *select_backend(__u64 service_key, __u32 hash)
{
    struct service_route (*routes)[4] = bpf_map_lookup_elem(&service_routes, &service_key);
    if (!routes)
        return NULL;
    
    __u32 total_weight = 0;
    __u32 active_count = 0;
    
    // Calculate total weight of active backends
    #pragma unroll
    for (int i = 0; i < 4; i++) {
        if ((*routes)[i].active && !is_circuit_open((*routes)[i].backend_ip)) {
            total_weight += (*routes)[i].weight;
            active_count++;
        }
    }
    
    if (active_count == 0)
        return NULL;
    
    // Select backend based on hash and weight
    __u32 selection = hash % total_weight;
    __u32 accumulated = 0;
    
    #pragma unroll
    for (int i = 0; i < 4; i++) {
        if (!(*routes)[i].active || is_circuit_open((*routes)[i].backend_ip))
            continue;
        
        accumulated += (*routes)[i].weight;
        if (selection < accumulated)
            return &(*routes)[i];
    }
    
    return &(*routes)[0]; // Fallback
}

// Main TC classifier program
SEC("tc")
int tc_packet_classifier(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;
    
    // Parse Ethernet header
    if (data + sizeof(struct ethhdr) > data_end)
        return TC_ACT_OK;
    
    struct ethhdr *eth = data;
    
    // Only process IP packets
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;
    
    // Parse IP header
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    
    // Only process TCP for now
    if (ip->protocol != IPPROTO_TCP)
        return TC_ACT_OK;
    
    // Parse TCP header
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    if ((void *)(tcp + 1) > data_end)
        return TC_ACT_OK;
    
    // Extract packet metadata
    struct packet_meta meta = {
        .src_ip = bpf_ntohl(ip->saddr),
        .dst_ip = bpf_ntohl(ip->daddr),
        .src_port = bpf_ntohs(tcp->source),
        .dst_port = bpf_ntohs(tcp->dest),
        .protocol = ip->protocol,
        .timestamp = bpf_ktime_get_ns(),
        .seq_num = bpf_ntohl(tcp->seq),
        .ack_num = bpf_ntohl(tcp->ack_seq)
    };
    
    // Get TCP payload
    __u32 tcp_hlen = tcp->doff * 4;
    void *payload = (void *)tcp + tcp_hlen;
    
    // Detect L7 protocol
    if (payload < data_end) {
        meta.l7_protocol = detect_l7_protocol(payload, data_end, meta.dst_port);
        
        // Update L7 protocol statistics
        if (meta.l7_protocol > 0) {
            __u64 *counter = bpf_map_lookup_elem(&l7_protocol_counts, &meta.l7_protocol);
            if (counter)
                __sync_fetch_and_add(counter, 1);
        }
    }
    
    // Create flow hash for statistics
    __u64 flow_hash = ((__u64)meta.src_ip << 32) | meta.dst_ip;
    flow_hash ^= ((__u64)meta.src_port << 16) | meta.dst_port;
    
    // Update flow statistics
    struct flow_stats *stats = bpf_map_lookup_elem(&flow_statistics, &flow_hash);
    if (!stats) {
        struct flow_stats new_stats = {
            .packets = 1,
            .bytes = skb->len,
            .first_seen = meta.timestamp,
            .last_seen = meta.timestamp
        };
        bpf_map_update_elem(&flow_statistics, &flow_hash, &new_stats, BPF_NOEXIST);
    } else {
        stats->packets++;
        stats->bytes += skb->len;
        stats->last_seen = meta.timestamp;
        
        // Detect retransmits (simplified)
        if (tcp->syn && tcp->ack)
            stats->retransmits++;
    }
    
    // Service routing for ingress traffic
    if (skb->ingress_ifindex > 0) { // Ingress traffic
        __u64 service_key = ((__u64)meta.dst_ip << 16) | meta.dst_port;
        struct service_route *route = select_backend(service_key, flow_hash);
        
        if (route && route->active) {
            // Rewrite destination IP and port for load balancing
            ip->daddr = bpf_htonl(route->backend_ip);
            tcp->dest = bpf_htons(route->backend_port);
            
            // Recalculate checksums
            ip->check = 0;
            tcp->check = 0;
            
            // Mark packet for redirect
            skb->cb[0] = 1; // Custom flag for redirected packet
        }
    }
    
    // Chaos engineering: inject latency if configured
    __u32 service_id = meta.dst_port; // Simple service ID based on port
    __u32 *latency_us = bpf_map_lookup_elem(&latency_injection, &service_id);
    if (latency_us && *latency_us > 0) {
        // Delay packet by sleeping (simplified - real implementation would queue)
        // Note: This is pseudo-code, actual implementation would use queue+timer
        // bpf_delay_packet(skb, *latency_us);
    }
    
    return TC_ACT_OK;
}

// TC program for egress traffic shaping
SEC("tc_egress")
int tc_egress_shaper(struct __sk_buff *skb)
{
    // Implement token bucket algorithm for rate limiting
    // This is a simplified version
    
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;
    
    if (data + sizeof(struct ethhdr) + sizeof(struct iphdr) > data_end)
        return TC_ACT_OK;
    
    // Apply different rate limits based on traffic class
    __u32 priority = skb->priority;
    
    if (priority > 3) {
        // Low priority traffic - apply strict rate limiting
        // Simplified: drop 1 in 10 packets
        __u32 rand = bpf_get_prandom_u32();
        if ((rand % 10) == 0)
            return TC_ACT_SHOT; // Drop packet
    }
    
    return TC_ACT_OK;
}

char LICENSE[] SEC("license") = "GPL";