// SPDX-License-Identifier: GPL-2.0
// Refactored kernel monitoring - network functionality moved to network-activity collector

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

// Include shared utilities for container context
#include "../../bpf_common/shared_maps.h"
#include "../../bpf_common/container_utils.h"

// Event types - network events moved to network-activity collector
#define EVENT_TYPE_MEMORY_ALLOC 1
#define EVENT_TYPE_MEMORY_FREE  2
#define EVENT_TYPE_PROCESS_EXEC 3
#define EVENT_TYPE_POD_SYSCALL  4
#define EVENT_TYPE_FILE_OPEN    8
#define EVENT_TYPE_FILE_READ    9
#define EVENT_TYPE_FILE_WRITE   10

// File operation information
struct file_info {
    char filename[56];  // File path (truncated)
    __u32 flags;        // Open flags
    __u32 mode;         // File mode
} __attribute__((packed));

// Kernel event structure (must match Go struct) - simplified without network info
struct kernel_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u64 size;
    char comm[16];
    __u64 cgroup_id;  // For pod correlation
    char pod_uid[36]; // Pod UID
    union {
        __u8 data[64];
        struct file_info file_info;   // File info for file operations
    };
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

// Maps - kernel-specific only (shared maps are in shared_maps.h)

// Ring buffer for kernel events (non-network)
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB buffer - reduced since network events moved
} events SEC(".maps");

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
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_FREE;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod information and safely copy UID
    struct pod_info *pod = get_pod_info(cgroup_id);
    safe_copy_pod_uid(event->pod_uid, pod);
    
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
    
    // Get current task for cgroup info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_EXEC;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
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