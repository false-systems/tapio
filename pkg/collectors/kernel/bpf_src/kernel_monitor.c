// SPDX-License-Identifier: GPL-2.0
// Minimal eBPF program for kernel monitoring - focused on containers

#include "../../bpf_common/vmlinux_minimal.h"

// For macOS development, conditionally include based on availability
#ifdef __BPF_HELPERS_H__
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#else
// Include our helpers which have the necessary definitions
#include "../../bpf_common/helpers.h"
#endif

// Network protocol constants
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// Address family constants
#define AF_INET 2
#define AF_INET6 10

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
    __u8 ip_version;  // 4 for IPv4, 6 for IPv6
    __u8 protocol;    // IPPROTO_TCP or IPPROTO_UDP
    __u8 state;       // Connection state
    __u8 direction;   // 0=outgoing, 1=incoming
    __u16 sport;      // Source port
    __u16 dport;      // Destination port
    __u32 saddr_v4;   // Source IP (IPv4)
    __u32 daddr_v4;   // Destination IP (IPv4)
    __u32 saddr_v6[4]; // Source IP (IPv6)
    __u32 daddr_v6[4]; // Destination IP (IPv6)
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
    __uint(max_entries, 512 * 1024); // 512KB buffer - production optimized
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

// Helper to extract cgroup ID from task struct using proper CO-RE patterns
static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    if (!task) {
        return 0;
    }

    // Use proper CO-RE field existence check with validation
    struct css_set *css_set_ptr = NULL;
    
    // Validate task->cgroups field exists before accessing
    if (bpf_core_field_exists(task->cgroups)) {
        // Safe read with bounds checking
        if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) != 0) {
            return 0;
        }
    } else {
        return 0;
    }
    
    // Validate pointer before use
    if (!css_set_ptr) {
        return 0;
    }

    // For cgroup v2 (unified hierarchy), try subsys[0]
    struct cgroup_subsys_state *css = NULL;
    
    // Check if subsys array exists and is accessible
    if (bpf_core_field_exists(css_set_ptr->subsys)) {
        // Safely read subsys[0] with bounds validation
        // Use proper field existence check
        if (bpf_core_field_exists(css_set_ptr->subsys[0])) {
            if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[0]) != 0) {
                css = NULL;
            }
        }
        
        // Fallback to subsys[1] for cgroup v1 compatibility if needed
        if (!css && bpf_core_field_exists(css_set_ptr->subsys[1])) {
            if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[1]) != 0) {
                css = NULL;
            }
        }
    }
    
    // Validate CSS pointer
    if (!css) {
        return 0;
    }

    // Read the cgroup from the css using CO-RE with proper validation
    struct cgroup *cgroup_ptr = NULL;
    if (bpf_core_field_exists(css->cgroup)) {
        if (BPF_CORE_READ_INTO(&cgroup_ptr, css, cgroup) != 0) {
            return 0;
        }
    } else {
        return 0;
    }
    
    // Validate cgroup pointer
    if (!cgroup_ptr) {
        return 0;
    }

    // Primary method: Extract kernfs inode number (most reliable)
    if (bpf_core_field_exists(cgroup_ptr->kn)) {
        struct kernfs_node *kn = NULL;
        if (BPF_CORE_READ_INTO(&kn, cgroup_ptr, kn) == 0 && kn) {
            // Validate kn has ino field before accessing
            if (bpf_core_field_exists(kn->ino)) {
                __u64 ino = 0;
                if (BPF_CORE_READ_INTO(&ino, kn, ino) == 0 && ino != 0) {
                    // Success: we have the kernfs inode number
                    return ino;
                }
            }
        }
    }

    // Fallback method: Use cgroup ID with offset for uniqueness
    if (bpf_core_field_exists(cgroup_ptr->id)) {
        int cgroup_id = 0;
        if (BPF_CORE_READ_INTO(&cgroup_id, cgroup_ptr, id) == 0 && cgroup_id > 0) {
            // Add offset to distinguish from PIDs and ensure uniqueness
            return (__u64)cgroup_id + 0x100000000ULL;
        }
    }

    // Last resort: use a hash of the css_set pointer
    // This is safe as we've already validated css_set_ptr is not NULL
    __u64 addr = (__u64)css_set_ptr;
    return (addr >> 8) + 0x200000000ULL;
}

// Helper to get pod information for a cgroup ID
static __always_inline struct pod_info *get_pod_info(__u64 cgroup_id)
{
    if (cgroup_id == 0) {
        return NULL;
    }
    return bpf_map_lookup_elem(&pod_info_map, &cgroup_id);
}

// Helper to safely copy pod UID with bounds checking
static __always_inline void safe_copy_pod_uid(char *dest, struct pod_info *pod)
{
    if (!dest || !pod) {
        if (dest) {
            __builtin_memset(dest, 0, 36);
        }
        return;
    }
    
    // Use bpf_probe_read_kernel_str for safe string copy with null termination
    // This ensures we don't read beyond bounds and properly null-terminate
    if (bpf_probe_read_kernel_str(dest, 36, pod->pod_uid) < 0) {
        // On error, clear the destination
        __builtin_memset(dest, 0, 36);
    }
}

// Helper to get container information for a PID
static __always_inline struct container_info *get_container_info(__u32 pid)
{
    if (pid == 0) {
        return NULL;
    }
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

// Helper to hash a string (simple DJB2 hash) with safe memory access
static __always_inline __u64 hash_path(const char *path, int len)
{
    __u64 hash = 5381;
    char ch;
    
    // Validate input parameters
    if (!path || len <= 0) {
        return 0;
    }
    
    // Ensure len is bounded for verifier (maximum 64 characters)
    if (len > 64) {
        len = 64;
    }
    
    // Use bounded loop with explicit unroll for verifier
    #pragma unroll
    for (int i = 0; i < 64; i++) {
        // Double check bounds
        if (i >= len) {
            break;
        }
        
        // Use safe memory read for each character with error checking
        if (bpf_probe_read_kernel(&ch, sizeof(ch), &path[i]) != 0) {
            // Error reading memory, stop hashing
            break;
        }
        
        // Check for null terminator
        if (ch == '\0') {
            break;
        }
        
        // Update hash value
        hash = ((hash << 5) + hash) + (__u8)ch;
    }
    
    return hash;
}

// Helper to get mount info for a path
static __always_inline struct mount_info *get_mount_info(const char *path)
{
    if (!path) {
        return NULL;
    }
    
    __u64 key = hash_path(path, 64);
    if (key == 0) {
        return NULL;
    }
    
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
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
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

// Network connection tracing - track tcp connections using CO-RE
SEC("kprobe/tcp_v4_connect")
int trace_tcp_v4_connect(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct from first argument using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
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
    
    // Fill network info from socket using CO-RE with field validation
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    event->net_info.ip_version = 4;
    
    // Validate socket fields exist and read safely with error checking
    if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
        __u16 sport = 0;
        if (BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num) == 0) {
            event->net_info.sport = sport;
        }
    }
    
    if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
        __u16 dport = 0;
        if (BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport) == 0) {
            // Convert network byte order to host byte order for port
            event->net_info.dport = __builtin_bswap16(dport);
        }
    }
    
    if (bpf_core_field_exists(sk->__sk_common.skc_rcv_saddr)) {
        __u32 saddr = 0;
        if (BPF_CORE_READ_INTO(&saddr, sk, __sk_common.skc_rcv_saddr) == 0) {
            event->net_info.saddr_v4 = saddr;
        }
    }
    
    if (bpf_core_field_exists(sk->__sk_common.skc_daddr)) {
        __u32 daddr = 0;
        if (BPF_CORE_READ_INTO(&daddr, sk, __sk_common.skc_daddr) == 0) {
            event->net_info.daddr_v4 = daddr;
        }
    }
    
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 1; // Connecting
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// IPv6 TCP connection tracing - dedicated function for IPv6
SEC("kprobe/tcp_v6_connect")
int trace_tcp_v6_connect(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct from first argument using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
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
    
    // Fill network info from socket using CO-RE with field validation
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    event->net_info.ip_version = 6;
    
    // Read IPv6 port information with field validation
    if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
        __u16 sport = 0;
        if (BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num) == 0) {
            event->net_info.sport = sport;
        }
    }
    
    if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
        __u16 dport = 0;
        if (BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport) == 0) {
            // Convert network byte order to host byte order for port
            event->net_info.dport = __builtin_bswap16(dport);
        }
    }
    
    // For IPv6 addresses, we need to use the full socket structure
    // Check if sk_v6_rcv_saddr and sk_v6_daddr fields exist using CO-RE
    if (bpf_core_field_exists(sk->sk_v6_rcv_saddr)) {
        // Read source IPv6 address with safe memory copy and bounds checking
        struct in6_addr src_addr;
        if (BPF_CORE_READ_INTO(&src_addr, sk, sk_v6_rcv_saddr) == 0) {
            // Use safe bounded memory copy for the address data
            int ret = bpf_probe_read_kernel(event->net_info.saddr_v6, 
                                           sizeof(event->net_info.saddr_v6), 
                                           &src_addr);
            if (ret != 0) {
                __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
            }
        } else {
            __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
        }
        
        // Read destination IPv6 address with safe memory copy
        if (bpf_core_field_exists(sk->sk_v6_daddr)) {
            struct in6_addr dst_addr;
            if (BPF_CORE_READ_INTO(&dst_addr, sk, sk_v6_daddr) == 0) {
                // Use safe bounded memory copy for the address data
                int ret = bpf_probe_read_kernel(event->net_info.daddr_v6, 
                                               sizeof(event->net_info.daddr_v6), 
                                               &dst_addr);
                if (ret != 0) {
                    __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
                }
            } else {
                __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
            }
        } else {
            // Field doesn't exist, clear destination
            __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
        }
    } else {
        // Fallback: fields not available in this kernel version
        __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
        __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
    }
    
    event->net_info.protocol = IPPROTO_TCP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 1; // Connecting
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// UDP socket tracing for both IPv4 and IPv6
SEC("kprobe/udp_sendmsg")
int trace_udp_send(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get sock struct from first argument using CO-RE helper
    struct sock *sk = read_sock_from_kprobe(ctx);
    
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
    
    // Fill network info from socket using CO-RE with field validation
    __builtin_memset(&event->net_info, 0, sizeof(event->net_info));
    
    // Check socket family for IPv4/IPv6 with field validation
    __u16 family = 0;
    if (bpf_core_field_exists(sk->__sk_common.skc_family)) {
        if (BPF_CORE_READ_INTO(&family, sk, __sk_common.skc_family) != 0) {
            family = 0;
        }
    }
    
    if (family == AF_INET) {
        event->net_info.ip_version = 4;
        
        // Use CO-RE to read IPv4 socket addresses safely with field validation
        if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
            __u16 sport = 0;
            if (BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num) == 0) {
                event->net_info.sport = sport;
            }
        }
        
        if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
            __u16 dport = 0;
            if (BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport) == 0) {
                // Convert network byte order to host byte order for port
                event->net_info.dport = __builtin_bswap16(dport);
            }
        }
        
        if (bpf_core_field_exists(sk->__sk_common.skc_rcv_saddr)) {
            __u32 saddr = 0;
            if (BPF_CORE_READ_INTO(&saddr, sk, __sk_common.skc_rcv_saddr) == 0) {
                event->net_info.saddr_v4 = saddr;
            }
        }
        
        if (bpf_core_field_exists(sk->__sk_common.skc_daddr)) {
            __u32 daddr = 0;
            if (BPF_CORE_READ_INTO(&daddr, sk, __sk_common.skc_daddr) == 0) {
                event->net_info.daddr_v4 = daddr;
            }
        }
    } else if (family == AF_INET6) {
        event->net_info.ip_version = 6;
        
        // Read IPv6 port information with field validation
        if (bpf_core_field_exists(sk->__sk_common.skc_num)) {
            __u16 sport = 0;
            if (BPF_CORE_READ_INTO(&sport, sk, __sk_common.skc_num) == 0) {
                event->net_info.sport = sport;
            }
        }
        
        if (bpf_core_field_exists(sk->__sk_common.skc_dport)) {
            __u16 dport = 0;
            if (BPF_CORE_READ_INTO(&dport, sk, __sk_common.skc_dport) == 0) {
                // Convert network byte order to host byte order for port
                event->net_info.dport = __builtin_bswap16(dport);
            }
        }
        
        // For IPv6 addresses, use full socket structure if available
        if (bpf_core_field_exists(sk->sk_v6_rcv_saddr)) {
            struct in6_addr src_addr;
            if (BPF_CORE_READ_INTO(&src_addr, sk, sk_v6_rcv_saddr) == 0) {
                // Use safe bounded memory copy for the address data
                int ret = bpf_probe_read_kernel(event->net_info.saddr_v6, 
                                               sizeof(event->net_info.saddr_v6), 
                                               &src_addr);
                if (ret != 0) {
                    __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
                }
            } else {
                __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
            }
            
            if (bpf_core_field_exists(sk->sk_v6_daddr)) {
                struct in6_addr dst_addr;
                if (BPF_CORE_READ_INTO(&dst_addr, sk, sk_v6_daddr) == 0) {
                    // Use safe bounded memory copy for the address data
                    int ret = bpf_probe_read_kernel(event->net_info.daddr_v6, 
                                                   sizeof(event->net_info.daddr_v6), 
                                                   &dst_addr);
                    if (ret != 0) {
                        __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
                    }
                } else {
                    __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
                }
            } else {
                __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
            }
        } else {
            // Fallback for minimal vmlinux.h
            __builtin_memset(event->net_info.saddr_v6, 0, sizeof(event->net_info.saddr_v6));
            __builtin_memset(event->net_info.daddr_v6, 0, sizeof(event->net_info.daddr_v6));
        }
    }
    
    event->net_info.protocol = IPPROTO_UDP;
    event->net_info.direction = 0; // Outgoing
    event->net_info.state = 0; // Stateless (UDP)
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
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
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";