#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>

#define MAX_ENTRIES 4096
#define MAX_DOMAIN_LEN 256
#define DNS_PORT 53
#define DNS_TIMEOUT_MS 5000  // 5 second timeout
#define IPPROTO_UDP 17

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
    u32 pid;
    u16 query_id;
    u32 src_ip;
};

struct dns_query_info {
    u64 start_time;
    u32 dst_ip;
    u16 query_type;
    u16 query_class;
    char domain[MAX_DOMAIN_LEN];
};

// DNS event structure
struct dns_event {
    u64 timestamp;
    u32 pid;
    u32 tgid;
    u32 uid;
    u32 src_ip;
    u32 dst_ip;
    u16 query_id;
    u16 query_type;
    u16 query_class;
    u8 event_type;
    u8 response_code;
    u32 latency_ms;
    u32 answer_count;
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
static __always_inline int extract_container_id(char *container_id) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        __builtin_memcpy(container_id, "host\0", 5);
        return 0;
    }
    
    // For now, simplified container detection
    __builtin_memcpy(container_id, "container\0", 10);
    return 0;
}

// Helper function to emit DNS event
static __always_inline void emit_dns_event(u8 event_type, u32 pid, u32 tgid, u32 uid,
                          u32 src_ip, u32 dst_ip, u16 query_id,
                          u16 query_type, u16 query_class, u8 response_code,
                          u32 latency_ms, u32 answer_count, const char *domain) {
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
        bpf_probe_read_kernel_str(event->domain, MAX_DOMAIN_LEN, domain);
    } else {
        __builtin_memset(event->domain, 0, MAX_DOMAIN_LEN);
    }
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_container_id(event->container_id);
    
    bpf_ringbuf_submit(event, 0);
}

// Parse DNS header
struct dns_header {
    u16 id;
    u16 flags;
    u16 qdcount;
    u16 ancount;
    u16 nscount;
    u16 arcount;
} __attribute__((packed));

// Parse DNS query - simplified version that handles basic domains
static __always_inline int parse_dns_query(void *data, void *data_end, struct dns_header *dns_hdr,
                          u16 *query_type, u16 *query_class, char *domain) {
    void *query_start = (void *)dns_hdr + sizeof(*dns_hdr);
    void *ptr = query_start;
    int domain_len = 0;
    
    if (ptr >= data_end)
        return -1;
    
    // Parse domain name
    #pragma unroll
    for (int i = 0; i < 32 && ptr < data_end; i++) {
        u8 len;
        if (bpf_probe_read_kernel(&len, 1, ptr) < 0)
            return -1;
            
        if (len == 0)
            break;
        
        if (len > 63 || ptr + len + 1 >= data_end)
            return -1;
        
        ptr++;
        
        if (domain_len + len < MAX_DOMAIN_LEN - 1) {
            if (domain_len > 0 && domain_len < MAX_DOMAIN_LEN - 1) {
                domain[domain_len++] = '.';
            }
            
            if (ptr + len <= data_end) {
                bpf_probe_read_kernel(domain + domain_len, len, ptr);
                domain_len += len;
            }
        }
        
        ptr += len;
    }
    
    if (ptr >= data_end)
        return -1;
    
    ptr++; // Skip null terminator
    
    if (ptr + 4 > data_end)
        return -1;
    
    bpf_probe_read_kernel(query_type, 2, ptr);
    *query_type = bpf_ntohs(*query_type);
    ptr += 2;
    
    bpf_probe_read_kernel(query_class, 2, ptr);
    *query_class = bpf_ntohs(*query_class);
    
    if (domain_len < MAX_DOMAIN_LEN)
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
    
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;
    
    struct udphdr *udp = (void *)ip + (ip->ihl * 4);
    if ((void *)udp + sizeof(*udp) > data_end)
        return TC_ACT_OK;
    
    if (bpf_ntohs(udp->dest) != DNS_PORT)
        return TC_ACT_OK;
    
    struct dns_header *dns_hdr = (void *)udp + sizeof(*udp);
    if ((void *)dns_hdr + sizeof(*dns_hdr) > data_end)
        return TC_ACT_OK;
    
    u16 query_id = bpf_ntohs(dns_hdr->id);
    u16 flags = bpf_ntohs(dns_hdr->flags);
    u16 qdcount = bpf_ntohs(dns_hdr->qdcount);
    
    // Check if this is a query (QR bit = 0)
    if ((flags & 0x8000) != 0 || qdcount == 0)
        return TC_ACT_OK;
    
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    u16 query_type = 0, query_class = 0;
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
    
    __builtin_memcpy(info.domain, domain, MAX_DOMAIN_LEN);
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
    
    if (bpf_ntohs(eth->h_proto) != ETH_P_IP)
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)eth + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end)
        return TC_ACT_OK;
    
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;
    
    struct udphdr *udp = (void *)ip + (ip->ihl * 4);
    if ((void *)udp + sizeof(*udp) > data_end)
        return TC_ACT_OK;
    
    if (bpf_ntohs(udp->source) != DNS_PORT)
        return TC_ACT_OK;
    
    struct dns_header *dns_hdr = (void *)udp + sizeof(*udp);
    if ((void *)dns_hdr + sizeof(*dns_hdr) > data_end)
        return TC_ACT_OK;
    
    u16 query_id = bpf_ntohs(dns_hdr->id);
    u16 flags = bpf_ntohs(dns_hdr->flags);
    u16 ancount = bpf_ntohs(dns_hdr->ancount);
    
    // Check if this is a response (QR bit = 1)
    if ((flags & 0x8000) == 0)
        return TC_ACT_OK;
    
    u8 response_code = flags & 0x000F;
    
    // Try to find matching query
    u64 min_latency = UINT32_MAX;
    struct dns_query_info *found_query = NULL;
    
    // Search for query (simplified - in production would use better lookup)
    #pragma unroll
    for (int i = 0; i < 10; i++) {
        struct dns_query_key key = {
            .pid = i,  // This is simplified
            .query_id = query_id,
            .src_ip = ip->daddr,  // Reversed for response
        };
        
        struct dns_query_info *query_info = bpf_map_lookup_elem(&dns_queries, &key);
        if (query_info) {
            u64 latency = bpf_ktime_get_ns() - query_info->start_time;
            if (latency < min_latency) {
                min_latency = latency;
                found_query = query_info;
            }
        }
    }
    
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    u8 event_type;
    if (response_code == 0) {
        event_type = DNS_RESPONSE;
    } else if (response_code == 3) {
        event_type = DNS_NXDOMAIN;
    } else {
        event_type = DNS_ERROR;
    }
    
    u32 latency_ms = 0;
    char *domain = NULL;
    
    if (found_query) {
        latency_ms = min_latency / 1000000;  // Convert to ms
        domain = found_query->domain;
    }
    
    emit_dns_event(event_type, pid, tgid, uid, ip->daddr, ip->saddr,
                   query_id, 0, 0, response_code, latency_ms, ancount, domain);
    
    return TC_ACT_OK;
}

// Track getaddrinfo() calls for higher-level DNS monitoring
SEC("uprobe/libc:getaddrinfo")
int BPF_KPROBE(getaddrinfo_entry, const char *node, const char *service) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    
    if (!node)
        return 0;
    
    // Store the hostname being queried
    char domain[MAX_DOMAIN_LEN] = {0};
    bpf_probe_read_user_str(domain, sizeof(domain), node);
    
    // We could store this for correlation with the return
    
    return 0;
}

SEC("uretprobe/libc:getaddrinfo")
int BPF_KRETPROBE(getaddrinfo_exit, int ret) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    if (ret != 0) {
        // DNS resolution failed at libc level
        emit_dns_event(DNS_ERROR, pid, tgid, uid, 0, 0, 0, 0, 0, ret, 0, 0, NULL);
    }
    
    return 0;
}

// Track gethostbyname() calls
SEC("uprobe/libc:gethostbyname")
int BPF_KPROBE(gethostbyname_entry, const char *name) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    
    if (!name)
        return 0;
    
    // Store the hostname being queried
    char domain[MAX_DOMAIN_LEN] = {0};
    bpf_probe_read_user_str(domain, sizeof(domain), name);
    
    return 0;
}

SEC("uretprobe/libc:gethostbyname")
int BPF_KRETPROBE(gethostbyname_exit, struct hostent *ret) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;
    u32 tgid = id;
    u32 uid = bpf_get_current_uid_gid();
    
    if (!ret) {
        // DNS resolution failed
        emit_dns_event(DNS_ERROR, pid, tgid, uid, 0, 0, 0, 0, 0, 1, 0, 0, NULL);
    }
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";