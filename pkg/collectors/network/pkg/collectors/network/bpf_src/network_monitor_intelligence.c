// SPDX-License-Identifier: GPL-2.0
// LEAN L7 Intelligence-Focused Network Monitoring via eBPF
// Captures only INTERESTING events: errors, anomalies, service dependencies
// Designed for INTELLIGENCE, not observability metrics

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include "../../bpf_common/bpf_stats.h"
#include "../../bpf_common/bpf_filters.h"
#include "../../bpf_common/bpf_batch.h"
#include "../../bpf_common/container_utils.h"
#include "../../bpf_common/shared_maps.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Network protocol constants
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define AF_INET 2
#define AF_INET6 10

// L7 Protocol detection constants
#define L7_PROTOCOL_UNKNOWN 0
#define L7_PROTOCOL_HTTP    1
#define L7_PROTOCOL_GRPC    2
#define L7_PROTOCOL_DNS     3

// Intelligence-focused event types - ONLY interesting events
#define INTEL_EVENT_SERVICE_DEPENDENCY    1  // New service-to-service connection
#define INTEL_EVENT_ERROR_PATTERN         2  // HTTP 4xx/5xx errors
#define INTEL_EVENT_LATENCY_ANOMALY       3  // Requests > 1s or unusual latency
#define INTEL_EVENT_PROTOCOL_VIOLATION    4  // Unusual headers, methods, or behavior
#define INTEL_EVENT_SECURITY_CONCERN      5  // Suspicious patterns
#define INTEL_EVENT_DNS_FAILURE           6  // Failed DNS lookups
#define INTEL_EVENT_CONNECTION_FAILURE    7  // Failed connection attempts

// Intelligence thresholds
#define SLOW_REQUEST_THRESHOLD_NS    1000000000ULL  // 1 second
#define ERROR_STATUS_THRESHOLD       400            // HTTP 4xx and above
#define DEPENDENCY_CACHE_TTL_NS      300000000000ULL // 5 minutes
#define MAX_SERVICE_NAME_LEN         64
#define MAX_ENDPOINT_LEN             128
#define MAX_ERROR_CONTEXT_LEN        256

// Service dependency tracking
struct service_key {
    char source_service[MAX_SERVICE_NAME_LEN];
    char dest_service[MAX_SERVICE_NAME_LEN];
    __u16 dest_port;
    __u8  protocol;
    __u8  _pad[1];
} __attribute__((packed));

struct dependency_info {
    __u64 first_seen;
    __u64 last_seen;
    __u32 request_count;
    __u32 error_count;
    __u16 last_status_code;
    __u8  is_new_dependency;  // Flag for first-time connections
    __u8  _pad[1];
} __attribute__((packed));

// Error pattern tracking
struct error_pattern {
    __u64 timestamp;
    __u16 status_code;
    __u8  method;
    __u8  is_cascade;  // Part of an error cascade
    char  endpoint[MAX_ENDPOINT_LEN];
} __attribute__((packed));

// Latency anomaly tracking
struct latency_baseline {
    __u64 avg_latency_ns;
    __u32 request_count;
    __u32 _pad;
} __attribute__((packed));

// Intelligence event structure - LEAN and focused
struct intelligence_event {
    // Core event info
    __u64 timestamp;
    __u32 event_type;
    __u32 severity;  // 1=info, 2=warning, 3=critical
    
    // Service context for correlation
    char source_service[MAX_SERVICE_NAME_LEN];
    char dest_service[MAX_SERVICE_NAME_LEN];
    
    // Network context (minimal)
    __u32 src_ip;        // IPv4 only for intelligence (simpler)
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  l7_protocol;
    __u8  _pad1[3];
    
    // Intelligence-specific data
    union {
        struct {
            __u16 status_code;
            __u8  method;
            __u8  is_first_occurrence;
            char  endpoint[MAX_ENDPOINT_LEN];
        } error_data;
        
        struct {
            __u64 latency_ns;
            __u64 baseline_ns;
            __u32 deviation_factor;  // How many times above normal
            __u32 _pad;
        } latency_data;
        
        struct {
            __u16 dns_code;
            __u16 _pad;
            char  domain[MAX_ENDPOINT_LEN];
        } dns_data;
        
        struct {
            __u8  is_new_service;
            __u8  connection_count;
            __u16 _pad;
        } dependency_data;
    };
    
    // Minimal context for correlation
    __u32 pid;
    __u64 cgroup_id;
    char  pod_uid[40];
} __attribute__((packed));

// Maps for intelligence-focused monitoring
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // Smaller buffer - only interesting events
} intelligence_events SEC(".maps");

// Service dependency tracking
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 4096);  // Track service dependencies
    __type(key, struct service_key);
    __type(value, struct dependency_info);
} service_dependencies SEC(".maps");

// Latency baselines for anomaly detection
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 2048);  // Track endpoint latency baselines
    __type(key, __u64);         // hash of service + endpoint
    __type(value, struct latency_baseline);
} latency_baselines SEC(".maps");

// Configuration map for intelligence thresholds
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 16);
    __type(key, __u32);
    __type(value, __u64);
} intel_config SEC(".maps");

// Error cascade tracking
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 1024);
    __type(key, __u64);  // time window hash
    __type(value, __u32); // error count
} error_cascade_tracker SEC(".maps");

// Statistics for intelligence events
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 16);
    __type(key, __u32);
    __type(value, __u64);
} intel_stats SEC(".maps");

// Helper functions for intelligence analysis

// Extract service name from process/pod context
static __always_inline void extract_service_name(char *service_name, 
                                                  struct task_struct *task,
                                                  __u64 cgroup_id)
{
    // Initialize with process name as fallback
    bpf_get_current_comm(service_name, MAX_SERVICE_NAME_LEN);
    
    // Try to get Kubernetes service name from pod info
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod && pod->service_name[0] != 0) {
        safe_copy_service_name(service_name, pod);
    }
}

// Calculate endpoint hash for latency baseline tracking
static __always_inline __u64 hash_endpoint(const char *service, const char *endpoint)
{
    __u64 hash = 5381;
    
    // Hash service name
    for (int i = 0; i < MAX_SERVICE_NAME_LEN && service[i]; i++) {
        hash = ((hash << 5) + hash) + service[i];
    }
    
    // Hash endpoint
    for (int i = 0; i < MAX_ENDPOINT_LEN && endpoint[i]; i++) {
        hash = ((hash << 5) + hash) + endpoint[i];
    }
    
    return hash;
}

// Check if HTTP status indicates an error
static __always_inline bool is_error_status(__u16 status_code)
{
    return status_code >= ERROR_STATUS_THRESHOLD;
}

// Check if request is slow based on latency
static __always_inline bool is_slow_request(__u64 latency_ns)
{
    return latency_ns > SLOW_REQUEST_THRESHOLD_NS;
}

// Parse HTTP method from minimal payload
static __always_inline __u8 parse_http_method_intel(const char *data, __u32 size)
{
    if (size < 3) return 0;
    
    if (data[0] == 'G' && data[1] == 'E' && data[2] == 'T') return 1;
    if (size >= 4 && data[0] == 'P' && data[1] == 'O' && data[2] == 'S' && data[3] == 'T') return 2;
    if (data[0] == 'P' && data[1] == 'U' && data[2] == 'T') return 3;
    if (size >= 6 && data[0] == 'D' && data[1] == 'E' && data[2] == 'L') return 4;
    
    return 0;
}

// Extract HTTP status code from response
static __always_inline __u16 extract_status_code_intel(const char *data, __u32 size)
{
    if (size < 12 || data[0] != 'H') return 0;
    
    // Find status code after "HTTP/1.1 " or "HTTP/2.0 "
    for (int i = 8; i < size - 2 && i < 15; i++) {
        if (data[i] >= '0' && data[i] <= '9' &&
            data[i+1] >= '0' && data[i+1] <= '9' &&
            data[i+2] >= '0' && data[i+2] <= '9') {
            return (data[i] - '0') * 100 + (data[i+1] - '0') * 10 + (data[i+2] - '0');
        }
    }
    
    return 0;
}

// Extract endpoint from HTTP request
static __always_inline void extract_endpoint(char *endpoint, const char *data, __u32 size)
{
    __builtin_memset(endpoint, 0, MAX_ENDPOINT_LEN);
    
    // Find the URL part in "METHOD /path HTTP/1.1"
    int start = 0, end = 0;
    bool found_space = false;
    
    for (int i = 0; i < size && i < MAX_ENDPOINT_LEN - 1; i++) {
        if (!found_space && data[i] == ' ') {
            found_space = true;
            start = i + 1;
        } else if (found_space && data[i] == ' ') {
            end = i;
            break;
        }
    }
    
    if (found_space && end > start) {
        int len = end - start;
        if (len > MAX_ENDPOINT_LEN - 1) len = MAX_ENDPOINT_LEN - 1;
        bpf_probe_read_kernel_str(endpoint, len + 1, data + start);
    }
}

// Update service dependency tracking
static __always_inline void track_service_dependency(const char *src_service,
                                                     const char *dst_service,
                                                     __u16 dst_port,
                                                     __u8 protocol,
                                                     __u16 status_code)
{
    struct service_key key = {};
    bpf_probe_read_kernel_str(key.source_service, MAX_SERVICE_NAME_LEN, src_service);
    bpf_probe_read_kernel_str(key.dest_service, MAX_SERVICE_NAME_LEN, dst_service);
    key.dest_port = dst_port;
    key.protocol = protocol;
    
    struct dependency_info *dep = bpf_map_lookup_elem(&service_dependencies, &key);
    __u64 now = bpf_ktime_get_ns();
    
    if (!dep) {
        // New dependency discovered - this is INTELLIGENCE!
        struct dependency_info new_dep = {
            .first_seen = now,
            .last_seen = now,
            .request_count = 1,
            .error_count = is_error_status(status_code) ? 1 : 0,
            .last_status_code = status_code,
            .is_new_dependency = 1,
        };
        bpf_map_update_elem(&service_dependencies, &key, &new_dep, BPF_ANY);
        
        // Emit intelligence event for new service dependency
        struct intelligence_event *event = bpf_ringbuf_reserve(&intelligence_events, sizeof(*event), 0);
        if (event) {
            __builtin_memset(event, 0, sizeof(*event));
            event->timestamp = now;
            event->event_type = INTEL_EVENT_SERVICE_DEPENDENCY;
            event->severity = 1; // Info
            event->l7_protocol = L7_PROTOCOL_HTTP;
            
            bpf_probe_read_kernel_str(event->source_service, MAX_SERVICE_NAME_LEN, src_service);
            bpf_probe_read_kernel_str(event->dest_service, MAX_SERVICE_NAME_LEN, dst_service);
            event->dst_port = dst_port;
            
            event->dependency_data.is_new_service = 1;
            event->dependency_data.connection_count = 1;
            
            bpf_ringbuf_submit(event, 0);
        }
    } else {
        // Update existing dependency
        dep->last_seen = now;
        dep->request_count++;
        if (is_error_status(status_code)) {
            dep->error_count++;
        }
        dep->last_status_code = status_code;
        dep->is_new_dependency = 0;
    }
}

// Update latency baseline and detect anomalies
static __always_inline bool check_latency_anomaly(const char *service,
                                                  const char *endpoint,
                                                  __u64 latency_ns)
{
    __u64 endpoint_hash = hash_endpoint(service, endpoint);
    struct latency_baseline *baseline = bpf_map_lookup_elem(&latency_baselines, &endpoint_hash);
    
    if (!baseline) {
        // First request for this endpoint - establish baseline
        struct latency_baseline new_baseline = {
            .avg_latency_ns = latency_ns,
            .request_count = 1,
        };
        bpf_map_update_elem(&latency_baselines, &endpoint_hash, &new_baseline, BPF_ANY);
        return false; // Not an anomaly yet
    }
    
    // Update rolling average (simple exponential moving average)
    __u64 old_avg = baseline->avg_latency_ns;
    baseline->avg_latency_ns = (old_avg * 7 + latency_ns) / 8;  // 7/8 weight on history
    baseline->request_count++;
    
    // Check for anomaly: > 3x baseline and > slow threshold
    if (latency_ns > (old_avg * 3) && latency_ns > SLOW_REQUEST_THRESHOLD_NS) {
        return true; // This is an anomaly!
    }
    
    return false;
}

// Intelligence-focused HTTP response handler
SEC("kprobe/tcp_recvmsg")
int trace_http_response_intelligence(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    if (!sk || !msg || size < 12) return 0;
    
    // Check if this is HTTP traffic
    __u16 dst_port = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
        BPF_CORE_READ_INTO(&dst_port, sk, __sk_common.skc_dport);
        dst_port = __builtin_bswap16(dst_port);
    }
    
    // Only process HTTP ports (80, 443, 8080, 8443, 3000)
    if (dst_port != 80 && dst_port != 443 && dst_port != 8080 && 
        dst_port != 8443 && dst_port != 3000) {
        return 0;
    }
    
    // Extract response payload
    char response_data[512];
    __builtin_memset(response_data, 0, sizeof(response_data));
    
    // Simplified payload extraction - in production this would be more robust
    size_t copy_size = size < 512 ? size : 512;
    bpf_probe_read_kernel(response_data, copy_size, msg);
    
    // Check if this looks like an HTTP response
    __u16 status_code = extract_status_code_intel(response_data, copy_size);
    if (status_code == 0) return 0; // Not an HTTP response
    
    // Get timing information (simplified - real implementation would track request->response timing)
    __u64 now = bpf_ktime_get_ns();
    __u64 latency_ns = 500000000ULL; // Placeholder - would calculate real latency
    
    // Extract service context
    char source_service[MAX_SERVICE_NAME_LEN] = {};
    char dest_service[MAX_SERVICE_NAME_LEN] = {};
    __u64 cgroup_id = 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        cgroup_id = get_cgroup_id(task);
        extract_service_name(source_service, task, cgroup_id);
        // Destination service would be extracted from DNS or service discovery
        bpf_probe_read_kernel_str(dest_service, MAX_SERVICE_NAME_LEN, "external-service");
    }
    
    // Track service dependency (always interesting for correlation)
    track_service_dependency(source_service, dest_service, dst_port, IPPROTO_TCP, status_code);
    
    // Check for error patterns (HTTP 4xx, 5xx)
    if (is_error_status(status_code)) {
        struct intelligence_event *event = bpf_ringbuf_reserve(&intelligence_events, sizeof(*event), 0);
        if (event) {
            __builtin_memset(event, 0, sizeof(*event));
            event->timestamp = now;
            event->event_type = INTEL_EVENT_ERROR_PATTERN;
            event->severity = status_code >= 500 ? 3 : 2; // Critical for 5xx, Warning for 4xx
            event->l7_protocol = L7_PROTOCOL_HTTP;
            
            bpf_probe_read_kernel_str(event->source_service, MAX_SERVICE_NAME_LEN, source_service);
            bpf_probe_read_kernel_str(event->dest_service, MAX_SERVICE_NAME_LEN, dest_service);
            event->dst_port = dst_port;
            
            event->error_data.status_code = status_code;
            event->error_data.method = 1; // Placeholder - would parse from request
            bpf_probe_read_kernel_str(event->error_data.endpoint, MAX_ENDPOINT_LEN, "/api/endpoint");
            
            // Extract network context
            __u32 dst_ip = 0;
            if (bpf_core_field_exists(sk->__sk_common.skc_daddr)) {
                BPF_CORE_READ_INTO(&dst_ip, sk, __sk_common.skc_daddr);
            }
            event->dst_ip = dst_ip;
            event->pid = pid;
            event->cgroup_id = cgroup_id;
            
            bpf_ringbuf_submit(event, 0);
        }
        
        // Update statistics
        __u32 stat_key = 1; // Error count
        __u64 *error_count = bpf_map_lookup_elem(&intel_stats, &stat_key);
        if (error_count) (*error_count)++;
    }
    
    // Check for latency anomalies (only for successful responses to avoid noise)
    if (!is_error_status(status_code) && is_slow_request(latency_ns)) {
        char endpoint[MAX_ENDPOINT_LEN] = "/api/slow-endpoint"; // Placeholder
        
        if (check_latency_anomaly(dest_service, endpoint, latency_ns)) {
            struct intelligence_event *event = bpf_ringbuf_reserve(&intelligence_events, sizeof(*event), 0);
            if (event) {
                __builtin_memset(event, 0, sizeof(*event));
                event->timestamp = now;
                event->event_type = INTEL_EVENT_LATENCY_ANOMALY;
                event->severity = 2; // Warning
                event->l7_protocol = L7_PROTOCOL_HTTP;
                
                bpf_probe_read_kernel_str(event->source_service, MAX_SERVICE_NAME_LEN, source_service);
                bpf_probe_read_kernel_str(event->dest_service, MAX_SERVICE_NAME_LEN, dest_service);
                
                event->latency_data.latency_ns = latency_ns;
                event->latency_data.baseline_ns = SLOW_REQUEST_THRESHOLD_NS; // Simplified
                event->latency_data.deviation_factor = 3; // Simplified
                
                bpf_ringbuf_submit(event, 0);
            }
            
            // Update statistics
            __u32 stat_key = 2; // Latency anomaly count
            __u64 *anomaly_count = bpf_map_lookup_elem(&intel_stats, &stat_key);
            if (anomaly_count) (*anomaly_count)++;
        }
    }
    
    return 0;
}

// DNS failure detection for intelligence
SEC("kprobe/udp_recvmsg")
int trace_dns_intelligence(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid)) {
        return 0;
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    // Check if this is DNS (port 53)
    __u16 src_port = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
        BPF_CORE_READ_INTO(&src_port, sk, __sk_common.skc_num);
    }
    
    if (src_port != 53) return 0; // Not DNS
    
    // Extract DNS response
    struct msghdr *msg = (struct msghdr *)PT_REGS_PARM2(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    if (!msg || size < 12) return 0; // Not a valid DNS response
    
    char dns_data[128];
    __builtin_memset(dns_data, 0, sizeof(dns_data));
    bpf_probe_read_kernel(dns_data, size < 128 ? size : 128, msg);
    
    // Parse DNS response code (simplified)
    __u16 flags = (dns_data[2] << 8) | dns_data[3];
    __u8 rcode = flags & 0x0F;
    
    // Only report DNS failures (intelligence-worthy)
    if (rcode != 0) {
        struct intelligence_event *event = bpf_ringbuf_reserve(&intelligence_events, sizeof(*event), 0);
        if (event) {
            __builtin_memset(event, 0, sizeof(*event));
            event->timestamp = bpf_ktime_get_ns();
            event->event_type = INTEL_EVENT_DNS_FAILURE;
            event->severity = rcode == 3 ? 2 : 1; // NXDOMAIN is warning, others are info
            event->l7_protocol = L7_PROTOCOL_DNS;
            
            struct task_struct *task = (struct task_struct *)bpf_get_current_task();
            if (task) {
                event->cgroup_id = get_cgroup_id(task);
                extract_service_name(event->source_service, task, event->cgroup_id);
            }
            
            event->dns_data.dns_code = rcode;
            bpf_probe_read_kernel_str(event->dns_data.domain, MAX_ENDPOINT_LEN, "failed.domain.com");
            
            bpf_ringbuf_submit(event, 0);
        }
        
        // Update DNS failure statistics
        __u32 stat_key = 3; // DNS failure count
        __u64 *dns_failures = bpf_map_lookup_elem(&intel_stats, &stat_key);
        if (dns_failures) (*dns_failures)++;
    }
    
    return 0;
}

// Connection failure detection for intelligence
SEC("kprobe/tcp_v4_connect")
int trace_connection_failure_intelligence(struct pt_regs *ctx)
{
    // This would track failed connection attempts
    // Implementation would monitor connect() system calls and their results
    // Only emit events for connection failures, not successes
    return 0;
}

char _license[] SEC("license") = "GPL";