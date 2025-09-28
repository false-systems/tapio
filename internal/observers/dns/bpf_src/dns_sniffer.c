//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define TC_ACT_OK 0
#define ETH_P_IP 0x0800
#define IPPROTO_UDP 17
#define DNS_PORT 53
#define MAX_PACKET_SIZE 512

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

// DNS packet event - just raw data
struct dns_packet_event {
    __u64 timestamp_ns;
    __be32 saddr;
    __be32 daddr;
    __be16 sport;
    __be16 dport;
    __u16 dns_data_len;
    __u8 dns_data[MAX_PACKET_SIZE]; // Raw DNS payload
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} dns_packets SEC(".maps");

SEC("tc")
int tc_dns_sniffer(struct __sk_buff *skb) {
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

    // Calculate DNS data offset and length
    __u32 dns_offset = sizeof(*eth) + ip_hlen + sizeof(*udp);
    __u16 udp_len = bpf_ntohs(udp->len);
    __u16 dns_len = udp_len - sizeof(*udp);

    // Sanity check
    if (dns_len > MAX_PACKET_SIZE || dns_len < 12) // DNS header is 12 bytes minimum
        return TC_ACT_OK;

    // Reserve space in ring buffer
    struct dns_packet_event *event = bpf_ringbuf_reserve(&dns_packets,
                                                          sizeof(*event), 0);
    if (!event)
        return TC_ACT_OK;

    // Fill metadata
    event->timestamp_ns = bpf_ktime_get_ns();
    event->saddr = ip->saddr;
    event->daddr = ip->daddr;
    event->sport = udp->source;
    event->dport = udp->dest;
    event->dns_data_len = dns_len;

    // Copy raw DNS payload to event (fixed size for verifier)
    // Use minimum of dns_len and MAX_PACKET_SIZE
    __u16 copy_len = MAX_PACKET_SIZE;
    if (dns_len < MAX_PACKET_SIZE) {
        // Try smaller fixed sizes
        if (dns_len <= 128) {
            copy_len = 128;
        } else if (dns_len <= 256) {
            copy_len = 256;
        } else {
            copy_len = MAX_PACKET_SIZE;
        }
    }

    // Use fixed-size read
    if (copy_len == 128) {
        if (bpf_skb_load_bytes(skb, dns_offset, event->dns_data, 128) < 0) {
            bpf_ringbuf_discard(event, 0);
            return TC_ACT_OK;
        }
    } else if (copy_len == 256) {
        if (bpf_skb_load_bytes(skb, dns_offset, event->dns_data, 256) < 0) {
            bpf_ringbuf_discard(event, 0);
            return TC_ACT_OK;
        }
    } else {
        if (bpf_skb_load_bytes(skb, dns_offset, event->dns_data, 512) < 0) {
            bpf_ringbuf_discard(event, 0);
            return TC_ACT_OK;
        }
    }

    // Submit to userspace
    bpf_ringbuf_submit(event, 0);

    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";