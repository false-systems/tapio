//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define TC_ACT_OK 0
#define ETH_P_IP 0x0800
#define IPPROTO_UDP 17
#define DNS_PORT 53
#define MAX_DNS_NAME 253

// Ethernet header
struct ethhdr {
    __u8 h_dest[6];
    __u8 h_source[6];
    __be16 h_proto;
} __attribute__((packed));

// IPv4 header
struct iphdr {
    __u8 ihl:4;
    __u8 version:4;
    __u8 tos;
    __be16 tot_len;
    __be16 id;
    __be16 frag_off;
    __u8 ttl;
    __u8 protocol;
    __be16 check;
    __be32 saddr;
    __be32 daddr;
} __attribute__((packed));

// UDP header
struct udphdr {
    __be16 source;
    __be16 dest;
    __be16 len;
    __be16 check;
} __attribute__((packed));

// DNS header (12 bytes)
struct dnshdr {
    __be16 id;
    __be16 flags;
    __be16 qdcount;
    __be16 ancount;
    __be16 nscount;
    __be16 arcount;
} __attribute__((packed));

// DNS query tracking
struct dns_query_key {
    __be32 saddr;
    __be16 sport;
    __be16 dns_id;
};

struct dns_query_state {
    __u64 timestamp_ns;
    __u8 name[MAX_DNS_NAME];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __type(key, struct dns_query_key);
    __type(value, struct dns_query_state);
    __uint(max_entries, 10240);
} dns_queries SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} dns_events SEC(".maps");

// DNS event structure
struct dns_event {
    __u64 timestamp_ns;
    __u64 latency_ns;
    __be32 saddr;
    __be32 daddr;
    __be16 sport;
    __be16 dport;
    __be16 dns_id;
    __u8 rcode;
    __u8 is_slow;
    __u8 name[MAX_DNS_NAME];
};

// Parse DNS name from packet - simplified to avoid instruction limit
static __always_inline int parse_dns_name(struct __sk_buff *skb, __u32 offset,
                                          __u8 *name, __u32 max_len) {
    __u8 byte;

    // Read first label length
    if (bpf_skb_load_bytes(skb, offset, &byte, 1) < 0)
        return -1;

    if (byte == 0 || (byte & 0xC0)) {
        // Empty or compressed
        name[0] = 0;
        return 0;
    }

    __u8 label_len = byte;
    if (label_len > 63) {
        label_len = 63; // Cap at max DNS label size
    }

    offset++;

    // Read fixed 63 bytes (max DNS label) then use only what we need
    __u8 buffer[63];
    if (bpf_skb_load_bytes(skb, offset, buffer, 63) < 0) {
        // Try smaller read
        if (bpf_skb_load_bytes(skb, offset, buffer, 16) < 0) {
            name[0] = 0;
            return 0;
        }
        label_len = 16;
    }

    // Copy to output
    #pragma unroll
    for (int i = 0; i < 16 && i < max_len - 1; i++) {
        if (i < label_len)
            name[i] = buffer[i];
        else
            break;
    }

    // Null terminate
    if (label_len < max_len)
        name[label_len] = 0;
    else
        name[15] = 0;

    return 0;
}

SEC("tc")
int tc_dns_monitor(struct __sk_buff *skb) {
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    // Parse Ethernet
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;

    // Parse IP
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;

    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    // Parse UDP
    __u32 ip_hlen = ip->ihl * 4;
    if (ip_hlen < sizeof(struct iphdr))
        return TC_ACT_OK;

    struct udphdr *udp = (void *)((char *)ip + ip_hlen);
    if ((void *)(udp + 1) > data_end)
        return TC_ACT_OK;

    __u16 sport = bpf_ntohs(udp->source);
    __u16 dport = bpf_ntohs(udp->dest);

    // Check DNS port
    if (sport != DNS_PORT && dport != DNS_PORT)
        return TC_ACT_OK;

    // Parse DNS header
    struct dnshdr *dns = (void *)(udp + 1);
    if ((void *)(dns + 1) > data_end)
        return TC_ACT_OK;

    __u16 flags = bpf_ntohs(dns->flags);
    __u8 is_response = (flags >> 15) & 1;
    __u8 rcode = flags & 0x0F;

    __u64 now = bpf_ktime_get_ns();

    if (!is_response) {
        // DNS Query - store it
        if (bpf_ntohs(dns->qdcount) < 1)
            return TC_ACT_OK;

        struct dns_query_key key = {
            .saddr = ip->saddr,
            .sport = udp->source,
            .dns_id = dns->id,
        };

        struct dns_query_state state = {
            .timestamp_ns = now,
        };

        // Parse query name
        __u32 name_offset = sizeof(struct ethhdr) + ip_hlen +
                           sizeof(struct udphdr) + sizeof(struct dnshdr);
        parse_dns_name(skb, name_offset, state.name, MAX_DNS_NAME);

        bpf_map_update_elem(&dns_queries, &key, &state, BPF_ANY);

    } else {
        // DNS Response - match with query
        struct dns_query_key key = {
            .saddr = ip->daddr,  // Swapped for response
            .sport = udp->dest,   // Swapped for response
            .dns_id = dns->id,
        };

        struct dns_query_state *query = bpf_map_lookup_elem(&dns_queries, &key);
        if (!query)
            return TC_ACT_OK;

        __u64 latency = now - query->timestamp_ns;

        // Check if this is a problem
        __u8 is_slow = (latency > 100000000);  // > 100ms
        __u8 is_error = (rcode == 2 || rcode == 3 || rcode == 5);  // SERVFAIL, NXDOMAIN, REFUSED

        if (is_slow || is_error) {
            // Report problem
            struct dns_event *event = bpf_ringbuf_reserve(&dns_events,
                                                          sizeof(*event), 0);
            if (event) {
                event->timestamp_ns = now;
                event->latency_ns = latency;
                event->saddr = ip->saddr;
                event->daddr = ip->daddr;
                event->sport = udp->source;
                event->dport = udp->dest;
                event->dns_id = dns->id;
                event->rcode = rcode;
                event->is_slow = is_slow;

                // Copy name (limited to avoid instruction explosion)
                for (int i = 0; i < 64; i++) {
                    if (i < MAX_DNS_NAME)
                        event->name[i] = query->name[i];
                }

                bpf_ringbuf_submit(event, 0);
            }
        }

        // Clean up
        bpf_map_delete_elem(&dns_queries, &key);
    }

    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";