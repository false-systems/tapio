// SPDX-License-Identifier: GPL-2.0
// Production DNS monitoring via eBPF with IPv4/IPv6 and UDP/TCP support

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// DNS event types
#define DNS_EVENT_QUERY    1
#define DNS_EVENT_RESPONSE 2
#define DNS_EVENT_TIMEOUT  3
#define DNS_EVENT_ERROR    4

// DNS constants
#define DNS_PORT 53
#define MAX_DNS_NAME_LEN 128    // Increased for longer domain names
#define MAX_DNS_DATA 512        // Increased for larger DNS packets
#define IPPROTO_UDP 17
#define IPPROTO_TCP 6
#define AF_INET 2
#define AF_INET6 10

// DNS query types
#define DNS_TYPE_A     1
#define DNS_TYPE_NS    2  
#define DNS_TYPE_CNAME 5
#define DNS_TYPE_SOA   6
#define DNS_TYPE_PTR   12
#define DNS_TYPE_MX    15
#define DNS_TYPE_TXT   16
#define DNS_TYPE_AAAA  28
#define DNS_TYPE_SRV   33

// DNS response codes
#define DNS_RCODE_NOERROR  0
#define DNS_RCODE_FORMERR  1
#define DNS_RCODE_SERVFAIL 2
#define DNS_RCODE_NXDOMAIN 3
#define DNS_RCODE_NOTIMP   4
#define DNS_RCODE_REFUSED  5

// IP address structures for IPv4/IPv6 support
struct ipv4_addr {
    __u32 addr;
} __attribute__((packed));

struct ipv6_addr {
    __u32 addr[4];
} __attribute__((packed));

// DNS header - complete RFC structure
struct dnshdr {
    __u16 id;
    __u16 flags;
    __u16 qdcount;  // Questions
    __u16 ancount;  // Answer RRs
    __u16 nscount;  // Authority RRs
    __u16 arcount;  // Additional RRs
} __attribute__((packed));

// DNS question structure
struct dns_question {
    // Name is variable length, followed by:
    __u16 qtype;
    __u16 qclass;
} __attribute__((packed));

// Enhanced DNS event for userspace with IPv4/IPv6 support
struct dns_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    
    __u8 event_type;
    __u8 protocol;
    __u8 ip_version;  // 4 or 6
    __u8 pad1;
    
    // Network info
    union {
        struct ipv4_addr ipv4_src;
        struct ipv6_addr ipv6_src;
    } src_addr;
    
    union {
        struct ipv4_addr ipv4_dst;
        struct ipv6_addr ipv6_dst;
    } dst_addr;
    
    __u16 src_port;
    __u16 dst_port;
    
    // DNS info
    __u16 dns_id;
    __u16 dns_flags;
    __u8 dns_opcode;
    __u8 dns_rcode;
    __u16 dns_qtype;
    
    __u32 data_len;
    __u32 latency_ns;  // For response correlation
    
    char query_name[MAX_DNS_NAME_LEN];
    char data[MAX_DNS_DATA];
} __attribute__((packed));

// Ring buffer for events - increased size for production
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024); // 1MB ring buffer
} dns_events SEC(".maps");

// Map to track DNS-related processes and containers
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096); // Support more processes
    __type(key, __u32);
    __type(value, __u8);
} dns_pids SEC(".maps");

// Enhanced query tracking with more context
struct query_context {
    __u64 start_timestamp;
    __u32 pid;
    __u32 tid;
    __u64 cgroup_id;
    __u8 protocol;
    __u8 ip_version;
    __u16 src_port;
    union {
        struct ipv4_addr ipv4_src;
        struct ipv6_addr ipv6_src;
    } src_addr;
} __attribute__((packed));

// Map to track pending DNS queries for latency calculation
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192); // Support more concurrent queries
    __type(key, __u16);  // DNS query ID
    __type(value, struct query_context);
} pending_queries SEC(".maps");

// Map to track DNS server sockets for filtering
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32); // socket FD
    __type(value, __u8); // 1 if DNS socket
} dns_sockets SEC(".maps");

// Map for container ID to cgroup correlation
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64); // cgroup_id
    __type(value, __u8); // container flag
} container_cgroups SEC(".maps");

// Per-CPU scratch buffer for DNS packet processing
// Eliminates stack overflow issues with large buffers
struct dns_scratch_buffer {
    char data[MAX_DNS_DATA];  // 512 bytes for DNS packet data
    char name_buf[MAX_DNS_NAME_LEN];  // 128 bytes for domain name extraction
    union {
        struct sockaddr_in addr4;
        struct sockaddr_in6 addr6;
    } addr_buf;  // Reusable buffer for address structures
    __u8 pad[356];  // Padding to align to cache line (28 bytes used by addr6)
};

// Per-CPU map for scratch buffers - one per CPU for lock-free access
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);  // Single entry per CPU
    __type(key, __u32);
    __type(value, struct dns_scratch_buffer);
} dns_scratch SEC(".maps");

// Helper to check if process is DNS-related (CO-RE enabled)
static __always_inline bool is_dns_process(struct task_struct *task)
{
    char comm[16];
    if (bpf_core_read_str(&comm, sizeof(comm), &task->comm) < 0)
        return false;
    
    // Check for common DNS processes with improved pattern matching
    // systemd-resolved
    if (comm[0] == 's' && comm[1] == 'y' && comm[2] == 's' && comm[3] == 't' && 
        comm[4] == 'e' && comm[5] == 'm' && comm[6] == 'd' && comm[7] == '-')
        return true;
    
    // dnsmasq
    if (comm[0] == 'd' && comm[1] == 'n' && comm[2] == 's' && comm[3] == 'm')
        return true;
    
    // coredns
    if (comm[0] == 'c' && comm[1] == 'o' && comm[2] == 'r' && comm[3] == 'e')
        return true;
    
    // bind/named
    if (comm[0] == 'n' && comm[1] == 'a' && comm[2] == 'm' && comm[3] == 'e' && comm[4] == 'd')
        return true;
    
    // unbound
    if (comm[0] == 'u' && comm[1] == 'n' && comm[2] == 'b' && comm[3] == 'o')
        return true;
    
    return false;
}

// Helper to get socket address family safely using CO-RE
static __always_inline int get_sock_family(struct sock *sk) 
{
    if (!sk)
        return -1;
    
    // Check if socket common structure exists
    if (!bpf_core_field_exists(sk->__sk_common) || 
        !bpf_core_field_exists(sk->__sk_common.skc_family)) {
        return -1;
    }
        
    // Use CO-RE to read socket family
    __u16 family;
    if (BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family) != 0)
        return -1;
    
    return (int)family;
}

// Helper to extract IPv4 address from sockaddr using CO-RE
static __always_inline void extract_ipv4_addr(const struct sockaddr_in *addr, struct ipv4_addr *dst)
{
    if (!addr || !dst) {
        return;
    }
    
    // Use safer probe_read_user for userspace addresses
    if (bpf_probe_read_user(&dst->addr, sizeof(dst->addr), &addr->sin_addr.s_addr) != 0) {
        dst->addr = 0;
    }
}

// Helper to extract IPv6 address from sockaddr using safe userspace reads
static __always_inline void extract_ipv6_addr(const struct sockaddr_in6 *addr, struct ipv6_addr *dst)
{
    if (!addr || !dst) {
        return;
    }
    
    // Use safer probe_read_user for userspace addresses
    if (bpf_probe_read_user(dst->addr, sizeof(dst->addr), &addr->sin6_addr.in6_u.u6_addr32) != 0) {
        __builtin_memset(dst->addr, 0, sizeof(dst->addr));
    }
}

// Helper to check if port is DNS (53)
static __always_inline bool is_dns_port(__u16 port)
{
    return bpf_ntohs(port) == DNS_PORT;
}

// Helper to get current cgroup ID using proper CO-RE
static __always_inline __u64 get_current_cgroup_id(void)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        return 0;
    }
    
    // Check if cgroups field exists (kernel compatibility)
    if (!bpf_core_field_exists(task->cgroups)) {
        return 0;
    }
    
    // Read css_set pointer using CO-RE
    struct css_set *css_set_ptr;
    if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) != 0 || !css_set_ptr) {
        return 0;
    }
    
    // Get first valid cgroup subsystem state
    struct cgroup_subsys_state *css;
    if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[0]) != 0 || !css) {
        return 0;
    }
    
    // Read cgroup pointer
    struct cgroup *cgroup_ptr;
    if (BPF_CORE_READ_INTO(&cgroup_ptr, css, cgroup) != 0 || !cgroup_ptr) {
        return 0;
    }
    
    // Get kernfs inode number if available
    if (bpf_core_field_exists(cgroup_ptr->kn)) {
        struct kernfs_node *kn;
        if (BPF_CORE_READ_INTO(&kn, cgroup_ptr, kn) == 0 && kn) {
            __u64 ino;
            if (BPF_CORE_READ_INTO(&ino, kn, ino) == 0) {
                return ino;
            }
        }
    }
    
    // Fallback to cgroup ID
    if (bpf_core_field_exists(cgroup_ptr->id)) {
        int cgroup_id;
        if (BPF_CORE_READ_INTO(&cgroup_id, cgroup_ptr, id) == 0 && cgroup_id > 0) {
            return (__u64)cgroup_id + 0x100000000ULL;
        }
    }
    
    return 0;
}

// Enhanced DNS query name extraction with compression support
static __always_inline int extract_query_name(void *dns_data, void *data_end, char *name, int max_len) {
    if (dns_data + sizeof(struct dnshdr) > data_end) 
        return -1;
    
    char *qname = (char *)dns_data + sizeof(struct dnshdr);
    int pos = 0;
    int jumps = 0; // Prevent infinite loops in compression
    __u32 max_offset = (char *)data_end - (char *)dns_data;  // Calculate actual DNS packet size
    
    // Safely parse DNS labels with compression support
    #pragma unroll
    for (int i = 0; i < 32 && pos < max_len - 1 && jumps < 5; i++) {
        if (qname >= (char *)data_end) 
            break;
        
        __u8 label_len = *qname;
        
        // Check for DNS compression (top 2 bits set)
        if ((label_len & 0xC0) == 0xC0) {
            // Compression pointer - read 2 bytes for offset
            if (qname + 1 >= (char *)data_end)
                break;
                
            __u16 offset = ((__u16)(label_len & 0x3F) << 8) | (__u16)(*(qname + 1));
            
            // Validate offset to prevent out-of-bounds access - must be within DNS packet
            if (offset >= max_offset || offset < sizeof(struct dnshdr))
                break;
                
            qname = (char *)dns_data + offset;
            jumps++;
            continue;
        }
        
        qname++; // Move past length byte
        
        if (label_len == 0) 
            break; // End of name
        
        if (label_len > 63 || qname + label_len > (char *)data_end) 
            return -1; // Invalid label
        
        // Add dot separator
        if (pos > 0 && pos < max_len - 1) 
            name[pos++] = '.';
        
        // Copy label characters with bounds checking
        // Manual byte-by-byte copy to avoid compiler generating memmove
        #pragma unroll
        for (int j = 0; j < 63; j++) {
            if (j >= label_len || pos >= max_len - 1)
                break;
            if (qname >= (char *)data_end) 
                break;
            // Use volatile to prevent compiler optimization
            volatile char c = *qname;
            name[pos] = c;
            pos++;
            qname++;
        }
    }
    
    if (pos < max_len) 
        name[pos] = '\0';
    
    return pos > 0 ? 0 : -1;
}

// Extract DNS query type from question section
static __always_inline __u16 extract_query_type(void *dns_data, void *data_end, int name_len) {
    if (dns_data + sizeof(struct dnshdr) + name_len + 4 > data_end)
        return 0;
    
    // Skip DNS header and name (with null terminator)
    char *qtype_ptr = (char *)dns_data + sizeof(struct dnshdr) + name_len + 1;
    
    if (qtype_ptr + 2 > (char *)data_end)
        return 0;
    
    __u16 qtype;
    bpf_probe_read(&qtype, sizeof(qtype), qtype_ptr);
    return bpf_ntohs(qtype);
}

// Enhanced DNS sendto monitoring with IPv4/IPv6 and protocol detection
SEC("tracepoint/syscalls/sys_enter_sendto")
int trace_dns_sendto(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Get process info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 tid = (__u32)pid_tgid;
    __u64 uid_gid = bpf_get_current_uid_gid();
    __u32 uid = uid_gid & 0xFFFFFFFF;
    __u32 gid = uid_gid >> 32;
    
    // Filter - check DNS-related processes or monitored PIDs
    __u8 *val = bpf_map_lookup_elem(&dns_pids, &pid);
    if (!val && !is_dns_process(task))
        return 0;
    
    // Get syscall arguments
    int sockfd = (int)ctx->args[0];
    const void *buf = (const void *)ctx->args[1];
    size_t len = (size_t)ctx->args[2];
    const struct sockaddr *dest_addr = (const struct sockaddr *)ctx->args[4];
    
    // Check for reasonable DNS packet size (12 bytes header minimum, 1500 max)
    if (len < sizeof(struct dnshdr) || len > 1500)
        return 0;
        
    // Check if destination is DNS port
    if (dest_addr) {
        __u16 port;
        __u16 family;
        
        if (bpf_probe_read_user(&family, sizeof(family), &dest_addr->sa_family) != 0)
            return 0;
            
        if (family == AF_INET) {
            struct sockaddr_in *addr4 = (struct sockaddr_in *)dest_addr;
            if (bpf_probe_read_user(&port, sizeof(port), &addr4->sin_port) != 0)
                return 0;
        } else if (family == AF_INET6) {
            struct sockaddr_in6 *addr6 = (struct sockaddr_in6 *)dest_addr;
            if (bpf_probe_read_user(&port, sizeof(port), &addr6->sin6_port) != 0)
                return 0;
        } else {
            return 0; // Not IP
        }
        
        if (!is_dns_port(port))
            return 0;
    }
    
    // Reserve enhanced DNS event
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Initialize enhanced event structure
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = tid;
    event->uid = uid;
    event->gid = gid;
    event->cgroup_id = get_current_cgroup_id();
    event->event_type = DNS_EVENT_QUERY;
    event->data_len = (__u32)len;
    
    // Read DNS header safely
    struct dnshdr dns_hdr;
    if (bpf_probe_read_user(&dns_hdr, sizeof(dns_hdr), buf) != 0) {
        bpf_ringbuf_discard(event, 0);
        return 0;
    }
    
    // Extract DNS fields
    event->dns_id = bpf_ntohs(dns_hdr.id);
    event->dns_flags = bpf_ntohs(dns_hdr.flags);
    event->dns_opcode = (event->dns_flags >> 11) & 0x0F;
    event->dns_rcode = event->dns_flags & 0x000F;
    
    // Copy DNS data for analysis (limited size)
    __u32 copy_len = len > MAX_DNS_DATA ? MAX_DNS_DATA : (__u32)len;
    if (bpf_probe_read_user(event->data, copy_len, buf) == 0) {
        // Extract query name and type
        if (extract_query_name(event->data, event->data + copy_len, 
                              event->query_name, MAX_DNS_NAME_LEN) == 0) {
            
            // Calculate name length for query type extraction
            int name_len = 0;
            #pragma unroll
            for (int i = 0; i < MAX_DNS_NAME_LEN - 1; i++) {
                if (event->query_name[i] == '\0') {
                    name_len = i;
                    break;
                }
            }
            
            // Convert dots to length-prefixed format length
            int wire_name_len = name_len + 2; // +1 for final null, +1 for estimation
            event->dns_qtype = extract_query_type(event->data, event->data + copy_len, wire_name_len);
        }
    }
    
    // Get per-CPU scratch buffer for address structures
    __u32 zero = 0;
    struct dns_scratch_buffer *scratch = bpf_map_lookup_elem(&dns_scratch, &zero);
    if (!scratch) {
        // Fallback: still submit event without address parsing
        bpf_ringbuf_submit(event, 0);
        return 0;
    }
    
    // Extract network information from destination address
    if (dest_addr) {
        __u16 family;
        if (bpf_probe_read_user(&family, sizeof(family), &dest_addr->sa_family) == 0) {
            if (family == AF_INET) {
                event->ip_version = 4;
                event->protocol = IPPROTO_UDP; // Assume UDP for now
                if (bpf_probe_read_user(&scratch->addr_buf.addr4, sizeof(scratch->addr_buf.addr4), dest_addr) == 0) {
                    event->dst_addr.ipv4_dst.addr = scratch->addr_buf.addr4.sin_addr.s_addr;
                    event->dst_port = scratch->addr_buf.addr4.sin_port;
                }
            } else if (family == AF_INET6) {
                event->ip_version = 6;
                event->protocol = IPPROTO_UDP; // Assume UDP for now
                if (bpf_probe_read_user(&scratch->addr_buf.addr6, sizeof(scratch->addr_buf.addr6), dest_addr) == 0) {
                    __builtin_memcpy(event->dst_addr.ipv6_dst.addr, scratch->addr_buf.addr6.sin6_addr.in6_u.u6_addr32, 16);
                    event->dst_port = scratch->addr_buf.addr6.sin6_port;
                }
            }
        }
    }
    
    // Store query context for response correlation
    struct query_context qctx = {
        .start_timestamp = event->timestamp,
        .pid = pid,
        .tid = tid,
        .cgroup_id = event->cgroup_id,
        .protocol = event->protocol,
        .ip_version = event->ip_version,
        .src_port = 0, // Will be filled by response
    };
    
    if (event->ip_version == 4) {
        qctx.src_addr.ipv4_src = event->dst_addr.ipv4_dst; // Swap for response correlation
    } else {
        qctx.src_addr.ipv6_src = event->dst_addr.ipv6_dst; // Swap for response correlation
    }
    
    bpf_map_update_elem(&pending_queries, &event->dns_id, &qctx, BPF_ANY);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Store recvfrom args for response correlation
struct recvfrom_args {
    void *buf;
    size_t len;
    struct sockaddr *src_addr;
};

// Map to store recvfrom arguments by PID
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u32);  // PID
    __type(value, struct recvfrom_args);
} recvfrom_args_map SEC(".maps");

// Capture recvfrom entry to save buffer pointer
SEC("tracepoint/syscalls/sys_enter_recvfrom")
int trace_dns_recvfrom_enter(struct trace_event_raw_sys_enter *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u8 *val = bpf_map_lookup_elem(&dns_pids, &pid);
    if (!val && !is_dns_process(task))
        return 0;
    
    struct recvfrom_args args = {
        .buf = (void *)ctx->args[1],
        .len = (size_t)ctx->args[2],
        .src_addr = (struct sockaddr *)ctx->args[4]
    };
    
    bpf_map_update_elem(&recvfrom_args_map, &pid, &args, BPF_ANY);
    return 0;
}

// Enhanced DNS recvfrom monitoring with actual response data
SEC("tracepoint/syscalls/sys_exit_recvfrom") 
int trace_dns_recvfrom(struct trace_event_raw_sys_exit *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Get process info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 tid = (__u32)pid_tgid;
    __u64 uid_gid = bpf_get_current_uid_gid();
    __u32 uid = uid_gid & 0xFFFFFFFF;
    __u32 gid = uid_gid >> 32;
    
    // Filter - check DNS-related processes or monitored PIDs
    __u8 *val = bpf_map_lookup_elem(&dns_pids, &pid);
    if (!val && !is_dns_process(task))
        return 0;
    
    // Check if syscall was successful
    long ret = ctx->ret;
    if (ret <= 0 || ret > 1500) // DNS response size limit
        return 0;
    
    // Get saved args
    struct recvfrom_args *args = bpf_map_lookup_elem(&recvfrom_args_map, &pid);
    if (!args)
        return 0;
    
    // Reserve enhanced DNS response event
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&recvfrom_args_map, &pid);
        return 0;
    }
    
    // Initialize response event
    __builtin_memset(event, 0, sizeof(*event));
    __u64 current_time = bpf_ktime_get_ns();
    event->timestamp = current_time;
    event->pid = pid;
    event->tid = tid;
    event->uid = uid;
    event->gid = gid;
    event->cgroup_id = get_current_cgroup_id();
    event->event_type = DNS_EVENT_RESPONSE;
    event->data_len = (__u32)ret;
    
    // Read DNS response data to get ID for correlation
    __u32 copy_len = ret > MAX_DNS_DATA ? MAX_DNS_DATA : (__u32)ret;
    if (args->buf && bpf_probe_read_user(event->data, copy_len, args->buf) == 0) {
        if (copy_len >= sizeof(struct dnshdr)) {
            struct dnshdr *dns_hdr = (struct dnshdr *)event->data;
            event->dns_id = bpf_ntohs(dns_hdr->id);
            event->dns_flags = bpf_ntohs(dns_hdr->flags);
            event->dns_rcode = event->dns_flags & 0x000F;
            
            // Look up pending query for latency calculation
            struct query_context *qctx = bpf_map_lookup_elem(&pending_queries, &event->dns_id);
            if (qctx) {
                event->latency_ns = current_time - qctx->start_timestamp;
                bpf_map_delete_elem(&pending_queries, &event->dns_id);
            }
        }
    }
    
    // Parse source address if available
    if (args->src_addr) {
        __u16 family;
        if (bpf_probe_read_user(&family, sizeof(family), &args->src_addr->sa_family) == 0) {
            event->ip_version = (family == AF_INET) ? 4 : 6;
            event->protocol = IPPROTO_UDP;
        }
    }
    
    bpf_map_delete_elem(&recvfrom_args_map, &pid);
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Additional tracepoint for TCP DNS support
SEC("tracepoint/syscalls/sys_enter_connect")
int trace_tcp_connect(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Quick filter for DNS processes
    __u8 *val = bpf_map_lookup_elem(&dns_pids, &pid);
    if (!val && !is_dns_process(task))
        return 0;
    
    int sockfd = (int)ctx->args[0];
    const struct sockaddr *addr = (const struct sockaddr *)ctx->args[1];
    
    if (!addr)
        return 0;
    
    // Check if connecting to DNS port 53
    __u16 family;
    if (bpf_probe_read_user(&family, sizeof(family), &addr->sa_family) != 0)
        return 0;
    
    __u16 port = 0;
    if (family == AF_INET) {
        struct sockaddr_in *addr4 = (struct sockaddr_in *)addr;
        if (bpf_probe_read_user(&port, sizeof(port), &addr4->sin_port) != 0)
            return 0;
    } else if (family == AF_INET6) {
        struct sockaddr_in6 *addr6 = (struct sockaddr_in6 *)addr;
        if (bpf_probe_read_user(&port, sizeof(port), &addr6->sin6_port) != 0)
            return 0;
    }
    
    if (is_dns_port(port)) {
        // Mark this socket as a DNS socket for TCP monitoring
        __u8 dns_flag = 1;
        bpf_map_update_elem(&dns_sockets, &sockfd, &dns_flag, BPF_ANY);
    }
    
    return 0;
}

// TCP send monitoring for DNS over TCP
SEC("tracepoint/syscalls/sys_enter_send")
int trace_tcp_send(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 tid = (__u32)pid_tgid;
    
    // Quick filter for DNS processes
    __u8 *val = bpf_map_lookup_elem(&dns_pids, &pid);
    if (!val && !is_dns_process(task))
        return 0;
    
    int sockfd = (int)ctx->args[0];
    const void *buf = (const void *)ctx->args[1];
    size_t len = (size_t)ctx->args[2];
    
    // Check if this is a DNS socket
    __u8 *dns_sock = bpf_map_lookup_elem(&dns_sockets, &sockfd);
    if (!dns_sock)
        return 0;
    
    // TCP DNS has 2-byte length prefix, minimum size check
    if (len < sizeof(struct dnshdr) + 2 || len > 65535)
        return 0;
    
    // Get per-CPU scratch buffer to avoid stack overflow
    __u32 zero = 0;
    struct dns_scratch_buffer *scratch = bpf_map_lookup_elem(&dns_scratch, &zero);
    if (!scratch)
        return 0;
    
    // Create DNS event for TCP query
    struct dns_event *event = bpf_ringbuf_reserve(&dns_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = tid;
    event->event_type = DNS_EVENT_QUERY;
    event->protocol = IPPROTO_TCP;
    event->data_len = (__u32)len;
    
    // Read TCP DNS data into per-CPU buffer (skip 2-byte length prefix)
    __u32 copy_len = len > MAX_DNS_DATA ? MAX_DNS_DATA : (__u32)len;
    
    if (bpf_probe_read_user(scratch->data, copy_len, buf) == 0 && copy_len >= 2) {
        // Skip 2-byte length prefix for TCP DNS
        __u32 dns_len = copy_len - 2;
        // Use explicit copy loop instead of memcpy for BPF compatibility
        #pragma unroll
        for (int i = 0; i < MAX_DNS_DATA && i < dns_len; i++) {
            event->data[i] = scratch->data[i + 2];
        }
        event->data_len = dns_len;
        
        // Extract DNS header from TCP payload
        if (dns_len >= sizeof(struct dnshdr)) {
            struct dnshdr *dns_hdr = (struct dnshdr *)(scratch->data + 2);
            event->dns_id = bpf_ntohs(dns_hdr->id);
            event->dns_flags = bpf_ntohs(dns_hdr->flags);
            event->dns_opcode = (event->dns_flags >> 11) & 0x0F;
            event->dns_rcode = event->dns_flags & 0x000F;
            
            // Extract query name from TCP DNS payload
            extract_query_name(event->data, event->data + dns_len, 
                             event->query_name, MAX_DNS_NAME_LEN);
        }
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";