#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_ENTRIES 4096
#define MAX_DOMAIN_LEN 256
#define DNS_PORT 53
#define DNS_TIMEOUT_MS 5000  // 5 second timeout

// DNS event types
enum dns_event_type {
    DNS_QUERY = 1,
    DNS_RESPONSE = 2,
    DNS_TIMEOUT = 3,
    DNS_ERROR = 4,
    DNS_NXDOMAIN = 5,
};

// DNS query tracking
struct dns_query_key {
    __u32 pid;
    __u16 query_id;
    __u32 src_ip;
};

struct dns_query_info {
    __u64 start_time;
    __u32 dst_ip;
    __u16 query_type;
    __u16 query_class;
    char domain[MAX_DOMAIN_LEN];
};

// DNS event structure
struct dns_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 uid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 query_id;
    __u16 query_type;
    __u16 query_class;
    __u8 event_type;
    __u8 response_code;
    __u32 latency_ms;
    __u32 answer_count;
    char domain[MAX_DOMAIN_LEN];
    char comm[16];
    char container_id[64];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct dns_query_key);
    __type(value, struct dns_query_info);
} dns_queries SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} dns_events SEC(".maps");

// Helper function to extract container ID
static int extract_container_id(char *container_id) {
    // Simplified container ID extraction
    __builtin_memset(container_id, 0, 64);
    bpf_probe_read_str(container_id, 8, "unknown");
    return 0;
}

// Helper function to emit DNS event
static void emit_dns_event(__u8 event_type, __u32 pid, __u32 tgid, __u32 uid,
                          __u32 src_ip, __u32 dst_ip, __u16 query_id,
                          __u16 query_type, __u16 query_class, __u8 response_code,
                          __u32 latency_ms, __u32 answer_count, const char *domain) {
    struct dns_event *event;
    
    event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event)
        return;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = tgid;
    event->uid = uid;
    event->src_ip = src_ip;
    event->dst_ip = dst_ip;
    event->query_id = query_id;
    event->query_type = query_type;
    event->query_class = query_class;
    event->event_type = event_type;
    event->response_code = response_code;
    event->latency_ms = latency_ms;
    event->answer_count = answer_count;
    
    if (domain) {
        bpf_probe_read_str(event->domain, MAX_DOMAIN_LEN, domain);
    } else {
        __builtin_memset(event->domain, 0, MAX_DOMAIN_LEN);
    }
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_container_id(event->container_id);
    
    bpf_ringbuf_submit(event, 0);
}

// Parse DNS header
struct dns_header {
    __u16 id;
    __u16 flags;
    __u16 qdcount;
    __u16 ancount;
    __u16 nscount;
    __u16 arcount;
} __attribute__((packed));

// Parse DNS query
static int parse_dns_query(void *data, void *data_end, struct dns_header *dns_hdr,
                          __u16 *query_type, __u16 *query_class, char *domain) {
    void *query_start = (void *)dns_hdr + sizeof(*dns_hdr);
    void *ptr = query_start;
    int domain_len = 0;
    
    if (ptr >= data_end)
        return -1;
    
    // Parse domain name (simplified)
    for (int i = 0; i < 32 && ptr < data_end; i++) {
        __u8 len = *(__u8 *)ptr;
        if (len == 0)
            break;
        
        if (len > 63 || ptr + len + 1 >= data_end)
            return -1;
        
        ptr++;
        
        if (domain_len + len < MAX_DOMAIN_LEN - 1) {
            if (domain_len > 0 && domain_len < MAX_DOMAIN_LEN - 1) {
                domain[domain_len++] = '.';
            }
            
            bpf_probe_read(domain + domain_len, len, ptr);
            domain_len += len;
        }
        
        ptr += len;
    }
    
    if (ptr >= data_end)
        return -1;
    
    ptr++; // Skip null terminator
    
    if (ptr + 4 >= data_end)
        return -1;
    
    *query_type = __builtin_bswap16(*(__u16 *)ptr);
    ptr += 2;
    *query_class = __builtin_bswap16(*(__u16 *)ptr);
    
    domain[domain_len] = '\0';
    
    return 0;
}

// Track DNS queries going out
SEC("tc")
int tc_dns_egress(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)eth + sizeof(*eth) > data_end)
        return TC_ACT_OK;
    
    if (eth->h_proto != __builtin_bswap16(ETH_P_IP))
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;
    
    struct udphdr *udp = (void *)ip + (ip->ihl * 4);
    if ((void *)udp + sizeof(*udp) > data_end)
        return TC_ACT_OK;
    
    if (udp->dest != __builtin_bswap16(DNS_PORT))
        return TC_ACT_OK;
    
    struct dns_header *dns_hdr = (void *)udp + sizeof(*udp);
    if ((void *)dns_hdr + sizeof(*dns_hdr) > data_end)
        return TC_ACT_OK;
    
    __u16 query_id = __builtin_bswap16(dns_hdr->id);
    __u16 flags = __builtin_bswap16(dns_hdr->flags);
    __u16 qdcount = __builtin_bswap16(dns_hdr->qdcount);
    
    // Check if this is a query (QR bit = 0)
    if ((flags & 0x8000) != 0 || qdcount == 0)
        return TC_ACT_OK;
    
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    __u16 query_type, query_class;
    char domain[MAX_DOMAIN_LEN] = {0};
    
    if (parse_dns_query(data, data_end, dns_hdr, &query_type, &query_class, domain) < 0)
        return TC_ACT_OK;
    
    // Store query information for tracking
    struct dns_query_key key = {
        .pid = pid,
        .query_id = query_id,
        .src_ip = ip->saddr,
    };
    
    struct dns_query_info info = {
        .start_time = bpf_ktime_get_ns(),
        .dst_ip = ip->daddr,
        .query_type = query_type,
        .query_class = query_class,
    };
    
    bpf_probe_read_str(info.domain, MAX_DOMAIN_LEN, domain);
    bpf_map_update_elem(&dns_queries, &key, &info, BPF_ANY);
    
    // Emit query event
    emit_dns_event(DNS_QUERY, pid, tgid, uid, ip->saddr, ip->daddr,
                   query_id, query_type, query_class, 0, 0, 0, domain);
    
    return TC_ACT_OK;
}

// Track DNS responses coming in
SEC("tc")
int tc_dns_ingress(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)eth + sizeof(*eth) > data_end)
        return TC_ACT_OK;
    
    if (eth->h_proto != __builtin_bswap16(ETH_P_IP))
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;
    
    struct udphdr *udp = (void *)ip + (ip->ihl * 4);
    if ((void *)udp + sizeof(*udp) > data_end)
        return TC_ACT_OK;
    
    if (udp->source != __builtin_bswap16(DNS_PORT))
        return TC_ACT_OK;
    
    struct dns_header *dns_hdr = (void *)udp + sizeof(*udp);
    if ((void *)dns_hdr + sizeof(*dns_hdr) > data_end)
        return TC_ACT_OK;
    
    __u16 query_id = __builtin_bswap16(dns_hdr->id);
    __u16 flags = __builtin_bswap16(dns_hdr->flags);
    __u16 ancount = __builtin_bswap16(dns_hdr->ancount);
    
    // Check if this is a response (QR bit = 1)
    if ((flags & 0x8000) == 0)
        return TC_ACT_OK;
    
    __u8 response_code = flags & 0x000F;
    
    // Find matching query
    struct dns_query_key key = {
        .query_id = query_id,
        .src_ip = ip->daddr,  // Reversed for response
    };
    
    // Try to find the query by iterating through possible PIDs
    // This is simplified - in practice we'd need a better lookup mechanism
    struct dns_query_info *query_info = NULL;
    
    // For now, just emit the response event without latency calculation
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    __u8 event_type;
    if (response_code == 0) {
        event_type = DNS_RESPONSE;
    } else if (response_code == 3) {
        event_type = DNS_NXDOMAIN;
    } else {
        event_type = DNS_ERROR;
    }
    
    emit_dns_event(event_type, pid, tgid, uid, ip->daddr, ip->saddr,
                   query_id, 0, 0, response_code, 0, ancount, NULL);
    
    return TC_ACT_OK;
}

// Track DNS timeouts using a periodic cleanup
SEC("kprobe/do_sys_poll")
int check_dns_timeouts(struct pt_regs *ctx) {
    // This is a simplified timeout detection mechanism
    // In practice, we'd use a more sophisticated approach
    
    return 0;
}

// Track getaddrinfo() calls for higher-level DNS monitoring
SEC("uretprobe/getaddrinfo")
int getaddrinfo_ret(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    int ret = PT_REGS_RC(ctx);
    
    if (ret != 0) {
        // DNS resolution failed at libc level
        emit_dns_event(DNS_ERROR, pid, tgid, uid, 0, 0, 0, 0, 0, ret, 0, 0, NULL);
    }
    
    return 0;
}

// Track gethostbyname() calls
SEC("uretprobe/gethostbyname")
int gethostbyname_ret(struct pt_regs *ctx) {
    __u64 id = bpf_get_current_pid_tgid();
    __u32 pid = id >> 32;
    __u32 tgid = id;
    __u32 uid = bpf_get_current_uid_gid();
    
    void *ret = (void *)PT_REGS_RC(ctx);
    
    if (!ret) {
        // DNS resolution failed
        emit_dns_event(DNS_ERROR, pid, tgid, uid, 0, 0, 0, 0, 0, 1, 0, 0, NULL);
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";