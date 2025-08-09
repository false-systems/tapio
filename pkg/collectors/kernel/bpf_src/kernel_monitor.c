// SPDX-License-Identifier: GPL-2.0
// Minimal eBPF program for kernel monitoring - focused on containers

#include "../../bpf_common/vmlinux_minimal.h"
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


// Volume mount information for PVC correlation
struct volume_info {
    char pvc_name[64];     // PersistentVolumeClaim name
    char namespace[64];    // K8s namespace
    char mount_path[128];  // Mount path in container
    char volume_id[64];    // Cloud volume ID (e.g., AWS EBS vol-xxx)
} __attribute__((packed));

// Process parent-child relationship for job/cronjob tracking
struct process_lineage {
    __u32 pid;             // Process ID
    __u32 ppid;            // Parent process ID
    __u32 tgid;            // Thread group ID
    __u64 start_time;      // Process start time
    char job_name[64];     // K8s Job/CronJob name if applicable
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


// Map volume mount paths to PVC info
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 5120);
    __type(key, __u64);             // Hash of mount path
    __type(value, struct volume_info); // PVC info
} volume_info_map SEC(".maps");

// Map process relationships for Job/CronJob tracking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 20480);
    __type(key, __u32);             // PID
    __type(value, struct process_lineage); // Process lineage info
} process_lineage_map SEC(".maps");

// Helper to check if process is in a container
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != 0;
}

// Helper to extract cgroup ID from task struct
static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    if (!task) {
        return 0;
    }

    // Read cgroups css_set from task with proper error handling
    struct css_set *css_set_ptr = NULL;
    int ret = bpf_core_read(&css_set_ptr, sizeof(css_set_ptr), &task->cgroups);
    if (ret != 0 || !css_set_ptr) {
        return 0;
    }

    // Try to find a valid cgroup_subsys_state
    // For cgroup v2, we use the unified hierarchy (index 0)
    // For cgroup v1, we check multiple subsystems for compatibility
    struct cgroup_subsys_state *css = NULL;
    bool css_found = false;
    
    // First, check unified hierarchy (cgroup v2 - most common in modern systems)
    ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[0]);
    if (ret == 0 && css) {
        css_found = true;
    } else {
        // Fallback: try other subsystems for cgroup v1 compatibility
        // Use unroll to avoid verifier issues with loops
        #pragma unroll
        for (int i = 1; i < 8; i++) {  // Check first 8 subsystems
            ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[i]);
            if (ret == 0 && css) {
                css_found = true;
                break; // Found a valid subsystem
            }
        }
    }

    if (!css_found || !css) {
        return 0;
    }

    // Read the cgroup from the css
    struct cgroup *cgroup_ptr = NULL;
    ret = bpf_core_read(&cgroup_ptr, sizeof(cgroup_ptr), &css->cgroup);
    if (ret != 0 || !cgroup_ptr) {
        return 0;
    }

    // Primary method: Extract kernfs inode number (most reliable)
    struct kernfs_node *kn = NULL;
    ret = bpf_core_read(&kn, sizeof(kn), &cgroup_ptr->kn);
    if (ret == 0 && kn) {
        __u64 ino = 0;
        ret = bpf_core_read(&ino, sizeof(ino), &kn->ino);
        if (ret == 0 && ino != 0) {
            // Success: we have the kernfs inode number
            // This is the most reliable cgroup identifier
            return ino;
        }
    }

    // Fallback method: Use cgroup ID with large offset
    // This ensures we get a unique identifier even if kernfs access fails
    int cgroup_id = 0;
    ret = bpf_core_read(&cgroup_id, sizeof(cgroup_id), &cgroup_ptr->id);
    if (ret == 0 && cgroup_id > 0) {
        // Add 4GB offset to distinguish from PIDs and ensure uniqueness
        // Modern PIDs can be up to 2^22 (4M) or higher, so 0x100000000 (4GB) is safe
        // This guarantees separation from any possible PID value (max PID is typically 32-bit)
        __u64 offset_id = (__u64)cgroup_id + 0x100000000ULL;
        
        // Enhanced validation: ensure the result doesn't overflow and is reasonable
        // cgroup_id should be positive and within reasonable bounds
        // Additional validation: ensure we don't have collision with typical inode ranges
        if (cgroup_id > 0 && cgroup_id < 0x7FFFFFFF && 
            offset_id > 0x100000000ULL && offset_id < 0x200000000ULL) {
            return offset_id;
        }
    }

    // Last resort: return a derived unique ID based on css_set pointer
    // This should rarely be reached, but provides a fallback
    if (css_set_ptr) {
        // Use address hash as unique identifier with different offset
        __u64 addr = (__u64)css_set_ptr;
        // Right shift to reduce address size, then add large offset
        // Use 8GB offset to differentiate from cgroup ID fallback
        __u64 hash_id = (addr >> 8) + 0x200000000ULL;
        
        // Enhanced validation: ensure we have a reasonable hash value
        // The hash should be in a specific range to avoid collisions
        if (hash_id > 0x200000000ULL && hash_id < 0x400000000ULL && addr != 0) {
            return hash_id;
        }
    }

    return 0;
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
    
    // Get sock struct from first argument
    // Using offset-based approach for portability
    struct sock *sk = NULL;
    unsigned long arg1 = 0;
    
    // On x86_64, RDI is at offset 112 in pt_regs
    // On ARM64, X0 is at offset 0 in pt_regs
    // We'll use a simple offset that works for x86_64
    bpf_probe_read_kernel(&arg1, sizeof(arg1), (void *)((char *)ctx + 112));
    bpf_probe_read(&sk, sizeof(sk), (void *)arg1);
    
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