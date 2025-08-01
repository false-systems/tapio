// SPDX-License-Identifier: GPL-2.0
// Minimal eBPF program for kernel monitoring - focused on containers

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Network protocol constants
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// Event types
#define EVENT_TYPE_MEMORY_ALLOC 1
#define EVENT_TYPE_MEMORY_FREE  2
#define EVENT_TYPE_PROCESS_EXEC 3
#define EVENT_TYPE_POD_SYSCALL  4
#define EVENT_TYPE_NETWORK_CONN 5
#define EVENT_TYPE_NETWORK_ACCEPT 6
#define EVENT_TYPE_NETWORK_CLOSE 7
#define EVENT_TYPE_FILE_OPEN 8
#define EVENT_TYPE_FILE_READ 9
#define EVENT_TYPE_FILE_WRITE 10

// Network connection information
struct network_info {
    __u32 saddr;    // Source IP (IPv4)
    __u32 daddr;    // Destination IP (IPv4)
    __u16 sport;    // Source port
    __u16 dport;    // Destination port
    __u8 protocol;  // IPPROTO_TCP or IPPROTO_UDP
    __u8 state;     // Connection state
    __u8 direction; // 0=outgoing, 1=incoming
    __u8 _pad;      // Padding
} __attribute__((packed));

// File operation information
struct file_info {
    char filename[56];  // File path (truncated)
    __u32 flags;        // Open flags
    __u32 mode;         // File mode
} __attribute__((packed));

// Kernel event structure (must match Go struct)
struct kernel_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u64 size;
    char comm[16];
    __u64 cgroup_id;  // Add cgroup ID for pod correlation
    char pod_uid[36]; // Add pod UID
    union {
        __u8 data[64];
        struct network_info net_info; // Network info for connection events
        struct file_info file_info;   // File info for file operations
    };
} __attribute__((packed));

// Pod information structure
struct pod_info {
    char pod_uid[36];
    char namespace[64];
    char pod_name[128];
    __u64 created_at;
} __attribute__((packed));

// Container information structure for PID correlation
struct container_info {
    char container_id[64];  // Docker/containerd ID
    char pod_uid[36];       // Associated pod
    char image[128];        // Container image
    __u64 started_at;       // Container start time
} __attribute__((packed));

// Service endpoint information for correlation
struct service_endpoint {
    char service_name[64];  // K8s service name
    char namespace[64];     // K8s namespace
    char cluster_ip[16];    // Service cluster IP
    __u16 port;            // Service port
    __u8 _pad[2];          // Padding
} __attribute__((packed));

// ConfigMap/Secret mount information
struct mount_info {
    char name[64];         // ConfigMap/Secret name
    char namespace[64];    // K8s namespace
    char mount_path[128];  // Mount path in container
    __u8 is_secret;        // 1 if secret, 0 if configmap
    __u8 _pad[7];          // Padding
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 4 * 1024 * 1024); // 4MB buffer
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u8);  // Flag
} container_pids SEC(".maps");

// Map cgroup ID to pod information
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);          // cgroup ID
    __type(value, struct pod_info); // pod info
} pod_info_map SEC(".maps");

// Map PID to container information
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 20480);
    __type(key, __u32);             // PID
    __type(value, struct container_info); // container info
} container_info_map SEC(".maps");

// Map service endpoints (IP:Port -> Service info)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);             // Combined IP:Port as key
    __type(value, struct service_endpoint); // service info
} service_endpoints_map SEC(".maps");

// Map mount paths to ConfigMap/Secret info
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);             // Hash of mount path
    __type(value, struct mount_info); // mount info
} mount_info_map SEC(".maps");

// Helper to check if process is in a container
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != 0;
}

// Helper to extract cgroup ID from task struct
static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    // This is a simplified version - in practice, we'd traverse the cgroup hierarchy
    // For now, use a hash of the PID as a pseudo-cgroup ID
    __u32 pid = BPF_CORE_READ(task, pid);
    return (__u64)pid; // Simplified - would need proper cgroup traversal
}

// Helper to get pod information for a cgroup ID
static __always_inline struct pod_info *get_pod_info(__u64 cgroup_id)
{
    return bpf_map_lookup_elem(&pod_info_map, &cgroup_id);
}

// Helper to get container information for a PID
static __always_inline struct container_info *get_container_info(__u32 pid)
{
    return bpf_map_lookup_elem(&container_info_map, &pid);
}

// Helper to create service endpoint key from IP and port
static __always_inline __u64 make_endpoint_key(__u32 ip, __u16 port)
{
    return ((__u64)ip << 16) | port;
}

// Helper to get service endpoint information
static __always_inline struct service_endpoint *get_service_endpoint(__u32 ip, __u16 port)
{
    __u64 key = make_endpoint_key(ip, port);
    return bpf_map_lookup_elem(&service_endpoints_map, &key);
}

// Helper to hash a string (simple DJB2 hash)
static __always_inline __u64 hash_path(const char *path, int len)
{
    __u64 hash = 5381;
    #pragma unroll
    for (int i = 0; i < 64 && i < len; i++) {
        if (path[i] == 0) break;
        hash = ((hash << 5) + hash) + path[i];
    }
    return hash;
}

// Helper to get mount info for a path
static __always_inline struct mount_info *get_mount_info(const char *path)
{
    __u64 key = hash_path(path, 64);
    return bpf_map_lookup_elem(&mount_info_map, &key);
}

// Memory allocation tracing - using generic tracepoint
SEC("tracepoint/kmem/kmalloc")
int trace_malloc(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_ALLOC;
    event->size = 0; // Can't easily get size from generic tracepoint
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod information
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod) {
        __builtin_memcpy(event->pod_uid, pod->pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Memory free tracing
SEC("tracepoint/kmem/kfree")
int trace_free(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_FREE;
    event->size = 0;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process execution tracing
SEC("tracepoint/sched/sched_process_exec")  
int trace_exec(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_EXEC;
    event->size = 0;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Network connection tracing - track tcp connections
SEC("kprobe/tcp_v4_connect")
int trace_tcp_connect(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct from first argument - x86_64 specific
    struct sock *sk;
    bpf_probe_read(&sk, sizeof(sk), (void *)ctx->di);
    if (!sk)
        return 0;
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_NETWORK_CONN;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill network info from socket
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    // Read socket addresses from sock struct
    struct sock_common sk_common = {};
    bpf_probe_read_kernel(&sk_common, sizeof(sk_common), &sk->__sk_common);
    
    // Extract addresses and ports
    event->net_info.sport = sk_common.skc_num;
    event->net_info.dport = __builtin_bswap16(sk_common.skc_dport);
    event->net_info.saddr = sk_common.skc_rcv_saddr;
    event->net_info.daddr = sk_common.skc_daddr;
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 1; // Connecting
    
    // Try to get pod information
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod) {
        __builtin_memcpy(event->pod_uid, pod->pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// File open tracing - track access to ConfigMaps/Secrets
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get the filename being opened (simplified - would need proper arg parsing)
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_FILE_OPEN;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Initialize file info
    __builtin_memset(&event->file_info, 0, sizeof(event->file_info));
    
    // In a real implementation, we would:
    // 1. Read the filename from syscall args
    // 2. Check if it matches known ConfigMap/Secret mount paths
    // 3. Look up mount info to identify the ConfigMap/Secret
    
    // For now, just track that a file operation occurred
    __builtin_memcpy(event->file_info.filename, "configmap/secret", 16);
    
    // Try to get pod information
    struct pod_info *pod = get_pod_info(cgroup_id);
    if (pod) {
        __builtin_memcpy(event->pod_uid, pod->pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";