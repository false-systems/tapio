// SPDX-License-Identifier: GPL-2.0
// Systemd service monitoring via eBPF

#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include "../../bpf_common/bpf_stats.h"
#include "../../bpf_common/bpf_filters.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

#define TASK_COMM_LEN 16
#define MAX_SERVICE_NAME 64
#define MAX_CGROUP_PATH 64  // Reduced to fit in BPF stack limit

// Event types
#define SYSTEMD_SERVICE_START    1
#define SYSTEMD_SERVICE_STOP     2
#define SYSTEMD_SERVICE_RESTART  3
#define SYSTEMD_SERVICE_FAILED   4
#define SYSTEMD_PROCESS_EXEC     5
#define SYSTEMD_PROCESS_EXIT     6
#define SYSTEMD_SIGNAL_EVENT     7

// Signal numbers we care about
#define SIGHUP    1
#define SIGKILL   9
#define SIGSEGV   11
#define SIGTERM   15
#define SIGABRT   6

// Systemd event structure for userspace
struct systemd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    
    __u8 event_type;
    __u8 pad[3];
    
    char comm[TASK_COMM_LEN];
    char service_name[MAX_SERVICE_NAME];
    char cgroup_path[MAX_CGROUP_PATH];  // Now 64 bytes instead of 256
    
    // Exit/failure info
    __u32 exit_code;
    __u32 signal;
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB ring buffer for production
} events SEC(".maps");

// Smaller cache entry to avoid stack issues
struct process_cache_entry {
    __u64 timestamp;
    __u32 pid;
    char comm[TASK_COMM_LEN];
    char service_name[MAX_SERVICE_NAME];
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 32768); // Increased for large clusters
    __type(key, __u32);  // PID
    __type(value, struct process_cache_entry);
} process_cache SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192); // Increased for more services
    __type(key, char[MAX_SERVICE_NAME]);
    __type(value, __u64); // Last activity timestamp
} service_tracker SEC(".maps");

// Statistics map for tracking dropped events
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct bpf_statistics);
} systemd_stats SEC(".maps");

// Forward declaration for detect_restart
static __always_inline bool detect_restart(char *service_name);

// BPF-safe string search implementation
static __always_inline int bpf_strstr_offset(const char *haystack, const char *needle, int max_len)
{
    int needle_len = 0;
    
    // Calculate needle length (bounded)
    #pragma unroll
    for (int i = 0; i < 32; i++) {  // Max needle length of 32
        if (needle[i] == '\0')
            break;
        needle_len++;
    }
    
    if (needle_len == 0)
        return -1;
    
    // Search for needle in haystack
    #pragma unroll
    for (int i = 0; i < max_len - needle_len + 1 && i < MAX_CGROUP_PATH - 32; i++) {
        bool match = true;
        
        #pragma unroll
        for (int j = 0; j < needle_len && j < 32; j++) {
            if (haystack[i + j] != needle[j]) {
                match = false;
                break;
            }
        }
        
        if (match)
            return i;
    }
    
    return -1;
}

// Helper to check if process is systemd-related
static __always_inline bool is_systemd_process(struct task_struct *task)
{
    char comm[TASK_COMM_LEN];
    
    if (bpf_probe_read_kernel_str(comm, sizeof(comm), task->comm) < 0)
        return false;
    
    // Check for systemd, systemctl, or service managers
    if (__builtin_memcmp(comm, "systemd", 7) == 0 ||
        __builtin_memcmp(comm, "systemctl", 9) == 0 ||
        __builtin_memcmp(comm, "dbus", 4) == 0)
        return true;
    
    return false;
}

// Helper to get cgroup path - simplified to avoid stack overflow
static __always_inline void get_cgroup_path(struct kernfs_node *kn, char *buffer, int buffer_size)
{
    buffer[0] = '\0';
    
    if (!kn)
        return;
    
    // Just get the immediate cgroup name, not full path (to avoid stack issues)
    const char *name = BPF_CORE_READ(kn, name);
    if (name) {
        bpf_probe_read_kernel_str(buffer, buffer_size, name);
    }
}

// Helper to extract service name from cgroup path
static __always_inline void extract_service_name(const char *cgroup_path, char *service_name)
{
    // Initialize service_name
    service_name[0] = '\0';
    
    if (!cgroup_path || cgroup_path[0] == '\0')
        return;
    
    // Look for .service pattern
    int path_len = 0;
    int service_end = -1;
    int last_slash = -1;
    
    #pragma unroll
    for (int i = 0; i < MAX_CGROUP_PATH && i < 256; i++) {
        if (cgroup_path[i] == '\0')
            break;
        path_len = i;
        
        // Track last slash position
        if (cgroup_path[i] == '/')
            last_slash = i;
            
        // Check for ".service" suffix
        if (i >= 7) {
            if (cgroup_path[i-7] == '.' &&
                cgroup_path[i-6] == 's' &&
                cgroup_path[i-5] == 'e' &&
                cgroup_path[i-4] == 'r' &&
                cgroup_path[i-3] == 'v' &&
                cgroup_path[i-2] == 'i' &&
                cgroup_path[i-1] == 'c' &&
                cgroup_path[i] == 'e') {
                service_end = i - 7;
                break;
            }
        }
    }
    
    if (service_end < 0) {
        // Also try .scope pattern
        #pragma unroll
        for (int i = 0; i < path_len && i < 256; i++) {
            if (i >= 5) {
                if (cgroup_path[i-5] == '.' &&
                    cgroup_path[i-4] == 's' &&
                    cgroup_path[i-3] == 'c' &&
                    cgroup_path[i-2] == 'o' &&
                    cgroup_path[i-1] == 'p' &&
                    cgroup_path[i] == 'e') {
                    service_end = i - 5;
                    break;
                }
            }
        }
    }
    
    if (service_end > 0) {
        // Find start position (after last slash)
        int start = (last_slash >= 0 && last_slash < service_end) ? last_slash + 1 : 0;
        int name_len = service_end - start;
        
        if (name_len > 0 && name_len < MAX_SERVICE_NAME) {
            // Use fixed-size loop with bounds checking for verifier
            #pragma unroll
            for (int i = 0; i < MAX_SERVICE_NAME - 1; i++) {
                if (i < name_len && (start + i) < 256) {
                    service_name[i] = cgroup_path[start + i];
                } else {
                    service_name[i] = '\0';
                    break;
                }
            }
        }
    }
}

// Helper to create and send event
static __always_inline void send_systemd_event(struct task_struct *task, __u8 event_type, __u32 exit_code, __u32 signal)
{
    struct systemd_event *event;
    
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        // Track dropped event in stats
        update_probe_stats_event_dropped();
        return;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->ppid = BPF_CORE_READ(task, real_parent, pid);
    
    // FIX: Correctly extract UID and GID
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid >> 32;  // Upper 32 bits is UID
    event->gid = uid_gid & 0xffffffff; // Lower 32 bits is GID
    event->event_type = event_type;
    event->exit_code = exit_code;
    event->signal = signal;
    
    // Get process name
    bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), task->comm);
    
    // Get cgroup ID and full path for service correlation
    struct cgroup *cgrp = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (cgrp) {
        struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
        if (kn) {
            event->cgroup_id = BPF_CORE_READ(kn, id);
            
            // Get the full cgroup path by walking hierarchy
            get_cgroup_path(kn, event->cgroup_path, sizeof(event->cgroup_path));
            
            // Extract service name from the full path
            extract_service_name(event->cgroup_path, event->service_name);
            
            // If service name is empty, try to use the last component
            if (event->service_name[0] == '\0') {
                const char *kn_name = BPF_CORE_READ(kn, name);
                if (kn_name) {
                    bpf_probe_read_kernel_str(event->service_name, sizeof(event->service_name), kn_name);
                }
            }
        }
    }
    
    bpf_ringbuf_submit(event, 0);
    
    // Update success stats
    update_probe_stats_event_sent(sizeof(*event));
    
    // Track service activity if we have a service name
    if (event->service_name[0] != '\0') {
        __u64 timestamp = event->timestamp;
        bpf_map_update_elem(&service_tracker, event->service_name, &timestamp, BPF_ANY);
    }
}

// Monitor process execution (systemd spawning services)
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Check for systemd-managed processes, not just systemd itself
    struct cgroup *cgrp = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (!cgrp)
        return 0;
    
    struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
    if (!kn)
        return 0;
    
    // Get cgroup path to check if it's systemd-managed
    char cgroup_path[MAX_CGROUP_PATH];
    get_cgroup_path(kn, cgroup_path, sizeof(cgroup_path));
    
    // Check if this is a systemd service
    if (bpf_strstr_offset(cgroup_path, ".service", sizeof(cgroup_path)) < 0 &&
        bpf_strstr_offset(cgroup_path, ".scope", sizeof(cgroup_path)) < 0 &&
        !is_systemd_process(task)) {
        return 0;
    }
    
    // Extract service name for restart detection
    char service_name[MAX_SERVICE_NAME];
    extract_service_name(cgroup_path, service_name);
    
    // Check if this might be a restart
    __u8 event_type = SYSTEMD_PROCESS_EXEC;
    if (service_name[0] != '\0' && detect_restart(service_name)) {
        event_type = SYSTEMD_SERVICE_RESTART;
    }
    
    send_systemd_event(task, event_type, 0, 0);
    
    // Cache process info for exit correlation using smaller struct
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    struct process_cache_entry cached_event = {};
    cached_event.timestamp = bpf_ktime_get_ns();
    cached_event.pid = pid;
    bpf_probe_read_kernel_str(cached_event.comm, sizeof(cached_event.comm), task->comm);
    
    // Store the service name in cache too
    if (service_name[0] != '\0') {
        #pragma unroll
        for (int i = 0; i < MAX_SERVICE_NAME && i < 64; i++) {
            cached_event.service_name[i] = service_name[i];
            if (service_name[i] == '\0')
                break;
        }
    }
    
    bpf_map_update_elem(&process_cache, &pid, &cached_event, BPF_ANY);
    
    return 0;
}

// Monitor process exit (service stops)
SEC("tracepoint/syscalls/sys_enter_exit")
int trace_exit(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // Check if we cached this process
    struct process_cache_entry *cached = bpf_map_lookup_elem(&process_cache, &pid);
    if (!cached)
        return 0;
    
    // Get exit code from syscall argument
    __u32 exit_code = (long)ctx->args[0];
    
    send_systemd_event(task, SYSTEMD_PROCESS_EXIT, exit_code, 0);
    
    // Clean up cache
    bpf_map_delete_elem(&process_cache, &pid);
    
    return 0;
}

// Monitor signals sent to processes (catch SIGKILL, SIGSEGV, SIGABRT)
SEC("tracepoint/signal/signal_generate")
int trace_signal(struct trace_event_raw_signal_generate *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Get signal number from the trace event context
    int sig = ctx->sig;
    
    // Only track critical signals
    if (sig != SIGKILL && sig != SIGSEGV && sig != SIGABRT && 
        sig != SIGTERM && sig != SIGHUP) {
        return 0;
    }
    
    // Check if it's a systemd-managed process by checking cgroup
    struct cgroup *cgrp = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (!cgrp)
        return 0;
        
    struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
    if (!kn)
        return 0;
        
    // Check if this is under systemd management (has .service or .scope)
    char cgroup_path[256];
    get_cgroup_path(kn, cgroup_path, sizeof(cgroup_path));
    
    // Quick check for systemd patterns
    bool is_systemd_managed = false;
    if (bpf_strstr_offset(cgroup_path, ".service", sizeof(cgroup_path)) >= 0 ||
        bpf_strstr_offset(cgroup_path, ".scope", sizeof(cgroup_path)) >= 0 ||
        bpf_strstr_offset(cgroup_path, "system.slice", sizeof(cgroup_path)) >= 0) {
        is_systemd_managed = true;
    }
    
    if (!is_systemd_managed)
        return 0;
    
    // Send signal event
    __u8 event_type = (sig == SIGKILL || sig == SIGSEGV || sig == SIGABRT) ? 
                      SYSTEMD_SERVICE_FAILED : SYSTEMD_SIGNAL_EVENT;
    
    send_systemd_event(task, event_type, 0, sig);
    
    return 0;
}

// Monitor cgroup changes (service lifecycle)
SEC("tracepoint/cgroup/cgroup_mkdir")
int trace_cgroup_mkdir(struct trace_event_raw_cgroup_mkdir *ctx)
{
    // Service creation
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_systemd_process(task))
        return 0;
    
    send_systemd_event(task, SYSTEMD_SERVICE_START, 0, 0);
    
    return 0;
}

SEC("tracepoint/cgroup/cgroup_rmdir")
int trace_cgroup_rmdir(struct trace_event_raw_cgroup_rmdir *ctx)
{
    // Service termination
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_systemd_process(task))
        return 0;
    
    send_systemd_event(task, SYSTEMD_SERVICE_STOP, 0, 0);
    
    return 0;
}

// Implementation of detect_restart
static __always_inline bool detect_restart(char *service_name)
{
    if (!service_name || service_name[0] == '\0')
        return false;
        
    __u64 *last_activity = bpf_map_lookup_elem(&service_tracker, service_name);
    if (!last_activity)
        return false;
        
    __u64 now = bpf_ktime_get_ns();
    __u64 time_diff = now - *last_activity;
    
    // If service was active less than 10 seconds ago, might be a restart
    // (10 seconds = 10,000,000,000 nanoseconds)
    if (time_diff < 10000000000ULL) {
        return true;
    }
    
    return false;
}

// License
char LICENSE[] SEC("license") = "GPL";