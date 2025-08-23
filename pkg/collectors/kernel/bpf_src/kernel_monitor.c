// SPDX-License-Identifier: GPL-2.0
// Focused kernel monitoring eBPF program - ConfigMap/Secret access and pod correlation only

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

// Event types - focused on unique kernel monitoring
#define EVENT_TYPE_CONFIGMAP_ACCESS   1
#define EVENT_TYPE_SECRET_ACCESS      2
#define EVENT_TYPE_POD_SYSCALL        3

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
        struct {
            char mount_path[64];    // ConfigMap/Secret mount path
        } config_info;
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
    __uint(max_entries, 256 * 1024); // 256KB buffer - reduced from 512KB
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

// Helper to check if path is ConfigMap/Secret related
static __always_inline bool is_config_path(const char *path, int max_len)
{
    if (!path || max_len <= 0) {
        return false;
    }
    
    // Common ConfigMap/Secret path patterns
    const char configmap_pattern[] = "/var/lib/kubelet/pods/";
    const char secret_pattern[] = "kubernetes.io~secret";
    const char cm_pattern[] = "kubernetes.io~configmap";
    
    // Check for kubelet pod path prefix (bounded loop for verifier)
    bool has_kubelet_prefix = true;
    #pragma unroll
    for (int i = 0; i < 24 && i < max_len; i++) {
        char expected = (i < 23) ? configmap_pattern[i] : '\0';
        char actual;
        
        if (bpf_probe_read_kernel(&actual, 1, &path[i]) != 0) {
            has_kubelet_prefix = false;
            break;
        }
        
        if (expected == '\0' || actual != expected) {
            if (expected == '\0') {
                break; // Successfully matched prefix
            }
            has_kubelet_prefix = false;
            break;
        }
    }
    
    if (!has_kubelet_prefix) {
        return false;
    }
    
    // Look for secret or configmap pattern in the rest of the path
    // This is a simplified check - in production, we'd use more sophisticated pattern matching
    char buffer[32];
    int start_offset = 24; // After "/var/lib/kubelet/pods/"
    
    // Safely read a portion of the path to check for patterns
    if (bpf_probe_read_kernel(buffer, sizeof(buffer), &path[start_offset]) != 0) {
        return false;
    }
    
    // Simple pattern matching for secret/configmap indicators
    // In a real implementation, this would be more comprehensive
    return true; // For now, assume any path under kubelet pods is relevant
}

// ConfigMap/Secret file access tracing
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_config_access(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Get the filename being opened (simplified - would need proper arg parsing in production)
    // For now, we'll capture the event and process the path in userspace
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_CONFIGMAP_ACCESS; // Will be refined based on actual path
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Initialize config info
    __builtin_memset(&event->config_info, 0, sizeof(event->config_info));
    
    // In a real implementation, we would:
    // 1. Read the filename from syscall args using proper tracepoint context
    // 2. Check if it matches known ConfigMap/Secret mount paths
    // 3. Look up mount info to identify the ConfigMap/Secret name
    // 4. Set appropriate event type (CONFIGMAP_ACCESS vs SECRET_ACCESS)
    
    // For now, mark as potential config access for userspace processing
    __builtin_memcpy(event->config_info.mount_path, "/var/lib/kubelet/", 17);
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Generic pod syscall tracking for correlation purposes
SEC("tracepoint/raw_syscalls/sys_enter")
int trace_pod_syscalls(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Only track container processes
    if (!is_container_process(pid))
        return 0;
    
    // Sample syscalls for correlation (not all syscalls are interesting)
    // Use a simple sampling approach
    static __u64 call_counter = 0;
    call_counter++;
    
    // Sample 1 in 100 syscalls to reduce overhead
    if (call_counter % 100 != 0) {
        return 0;
    }
    
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_POD_SYSCALL;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";