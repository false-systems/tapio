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
#define MAX_CGROUP_PATH 256

// Event types
#define SYSTEMD_SERVICE_START    1
#define SYSTEMD_SERVICE_STOP     2
#define SYSTEMD_SERVICE_RESTART  3
#define SYSTEMD_SERVICE_FAILED   4
#define SYSTEMD_PROCESS_EXEC     5
#define SYSTEMD_PROCESS_EXIT     6

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
    char cgroup_path[MAX_CGROUP_PATH];
    
    // Exit/failure info
    __u32 exit_code;
    __u32 signal;
} __attribute__((packed));

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);  // PID
    __type(value, struct systemd_event);
} process_cache SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, char[MAX_SERVICE_NAME]);
    __type(value, __u64); // Last activity timestamp
} service_tracker SEC(".maps");

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

// Helper to extract service name from cgroup path
static __always_inline void extract_service_name(const char *cgroup_path, char *service_name)
{
    char temp_path[MAX_CGROUP_PATH];
    int len = bpf_probe_read_kernel_str(temp_path, sizeof(temp_path), cgroup_path);
    
    if (len <= 0) {
        service_name[0] = '\0';
        return;
    }
    
    // Look for systemd service pattern: /system.slice/service_name.service
    int service_pos = bpf_strstr_offset(temp_path, ".service", len);
    if (service_pos < 0) {
        service_name[0] = '\0';
        return;
    }
    
    // Find the start of service name (after last slash before .service)
    int name_start_pos = 0;
    #pragma unroll
    for (int i = 0; i < service_pos && i < MAX_CGROUP_PATH - 1; i++) {
        if (temp_path[i] == '/') {
            name_start_pos = i + 1;
        }
    }
    
    // Copy service name
    int name_len = service_pos - name_start_pos;
    if (name_len > 0 && name_len < MAX_SERVICE_NAME - 1) {
        #pragma unroll
        for (int i = 0; i < name_len && i < MAX_SERVICE_NAME - 1; i++) {
            service_name[i] = temp_path[name_start_pos + i];
        }
        service_name[name_len] = '\0';
    } else {
        service_name[0] = '\0';
    }
}

// Helper to create and send event
static __always_inline void send_systemd_event(struct task_struct *task, __u8 event_type, __u32 exit_code, __u32 signal)
{
    struct systemd_event *event;
    
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return;
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->ppid = BPF_CORE_READ(task, real_parent, pid);
    event->uid = bpf_get_current_uid_gid() >> 32;
    event->gid = bpf_get_current_uid_gid() & 0xffffffff;
    event->event_type = event_type;
    event->exit_code = exit_code;
    event->signal = signal;
    
    // Get process name
    bpf_probe_read_kernel_str(event->comm, sizeof(event->comm), task->comm);
    
    // Get cgroup ID and path for service correlation
    struct cgroup *cgrp = BPF_CORE_READ(task, cgroups, subsys[0], cgroup);
    if (cgrp) {
        struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
        if (kn) {
            event->cgroup_id = BPF_CORE_READ(kn, id);
            
            // Try to get cgroup path for service name extraction
            // The name field exists in kernfs_node structure
            const char *kn_name = BPF_CORE_READ(kn, name);
            if (kn_name) {
                bpf_probe_read_kernel_str(event->cgroup_path, sizeof(event->cgroup_path), kn_name);
                extract_service_name(event->cgroup_path, event->service_name);
            }
        }
    }
    
    bpf_ringbuf_submit(event, 0);
}

// Monitor process execution (systemd spawning services)
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec(struct trace_event_raw_sys_enter *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_systemd_process(task))
        return 0;
    
    send_systemd_event(task, SYSTEMD_PROCESS_EXEC, 0, 0);
    
    // Cache process info for exit correlation
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    struct systemd_event cached_event = {};
    cached_event.timestamp = bpf_ktime_get_ns();
    cached_event.pid = pid;
    bpf_probe_read_kernel_str(cached_event.comm, sizeof(cached_event.comm), task->comm);
    
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
    struct systemd_event *cached = bpf_map_lookup_elem(&process_cache, &pid);
    if (!cached)
        return 0;
    
    // Get exit code from syscall argument
    __u32 exit_code = (long)ctx->args[0];
    
    send_systemd_event(task, SYSTEMD_PROCESS_EXIT, exit_code, 0);
    
    // Clean up cache
    bpf_map_delete_elem(&process_cache, &pid);
    
    return 0;
}

// Monitor systemd service state changes via dbus signals
SEC("uprobe/dbus")
int trace_dbus_signal(struct pt_regs *ctx)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only monitor systemd or systemctl processes
    if (!is_systemd_process(task))
        return 0;
    
    // This would need more sophisticated dbus message parsing
    // For now, just report systemd activity
    send_systemd_event(task, SYSTEMD_SERVICE_START, 0, 0);
    
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

// License
char LICENSE[] SEC("license") = "GPL";