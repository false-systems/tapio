// SPDX-License-Identifier: GPL-2.0
// Process monitoring eBPF program - execution, memory, lineage tracking

#include "../../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// BPF map update flags
#ifndef BPF_ANY
#define BPF_ANY 0
#endif
#ifndef BPF_NOEXIST
#define BPF_NOEXIST 1
#endif
#ifndef BPF_EXIST
#define BPF_EXIST 2
#endif

// Process event types
#define EVENT_TYPE_MEMORY_ALLOC 1
#define EVENT_TYPE_MEMORY_FREE  2
#define EVENT_TYPE_PROCESS_EXEC 3
#define EVENT_TYPE_PROCESS_EXIT 17
#define EVENT_TYPE_PROCESS_FORK 18

// Process event structure
struct process_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u64 size;           // Memory size for alloc/free events
    char comm[16];
    __u64 cgroup_id;
    char pod_uid[36];
    union {
        struct {
            __u32 ppid;
            __u32 tgid;
            __u64 start_time;
            char filename[64];
            char args[128];     // Command arguments (truncated)
        } exec_info;
        struct {
            __u32 parent_pid;
            __u32 child_pid;
            __u64 clone_flags;
        } fork_info;
        struct {
            __s32 exit_code;
            __u64 runtime_ns;
        } exit_info;
        __u8 data[200];     // Union must accommodate largest struct
    };
} __attribute__((packed));

// Process lineage structure for job/cronjob tracking
struct process_lineage {
    __u32 pid;
    __u32 ppid;
    __u32 tgid;
    __u64 start_time;
    char job_name[64];
} __attribute__((packed));

// Maps for process monitoring
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 2 * 1024 * 1024); // 2MB buffer for process events
} process_events SEC(".maps");

// Track container processes
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u8);  // Flag
} container_pids SEC(".maps");

// Map process relationships for Job/CronJob tracking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 20480);
    __type(key, __u32);                    // PID
    __type(value, struct process_lineage); // Process lineage info
} process_lineage_map SEC(".maps");

// Map cgroup ID to pod information
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);      // cgroup ID
    __type(value, char[36]); // pod UID
} pod_uid_map SEC(".maps");

// Helper functions
static __always_inline bool is_container_process(__u32 pid)
{
    __u8 *flag = bpf_map_lookup_elem(&container_pids, &pid);
    return flag != 0;
}

static __always_inline __u64 get_cgroup_id(struct task_struct *task)
{
    if (!task) {
        return 0;
    }

    struct css_set *css_set_ptr = NULL;
    int ret = bpf_core_read(&css_set_ptr, sizeof(css_set_ptr), &task->cgroups);
    if (ret != 0 || !css_set_ptr) {
        return 0;
    }

    struct cgroup_subsys_state *css = NULL;
    bool css_found = false;
    
    // Check unified hierarchy (cgroup v2)
    ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[0]);
    if (ret == 0 && css) {
        css_found = true;
    } else {
        // Fallback: try other subsystems for cgroup v1
        #pragma unroll
        for (int i = 1; i < 8; i++) {
            ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[i]);
            if (ret == 0 && css) {
                css_found = true;
                break;
            }
        }
    }

    if (!css_found || !css) {
        return 0;
    }

    struct cgroup *cgroup_ptr = NULL;
    ret = bpf_core_read(&cgroup_ptr, sizeof(cgroup_ptr), &css->cgroup);
    if (ret != 0 || !cgroup_ptr) {
        return 0;
    }

    // Extract kernfs inode number
    struct kernfs_node *kn = NULL;
    ret = bpf_core_read(&kn, sizeof(kn), &cgroup_ptr->kn);
    if (ret == 0 && kn) {
        __u64 ino = 0;
        ret = bpf_core_read(&ino, sizeof(ino), &kn->ino);
        if (ret == 0 && ino != 0) {
            return ino;
        }
    }

    // Fallback: use cgroup ID with offset
    int cgroup_id = 0;
    ret = bpf_core_read(&cgroup_id, sizeof(cgroup_id), &cgroup_ptr->id);
    if (ret == 0 && cgroup_id > 0) {
        return (__u64)cgroup_id + 0x100000000ULL;
    }

    return 0;
}

// Memory allocation tracing
SEC("tracepoint/kmem/kmalloc")
int trace_process_malloc(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct process_event *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_ALLOC;
    event->size = 0; // Would need to be extracted from tracepoint args
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get pod UID
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Memory free tracing
SEC("tracepoint/kmem/kfree")
int trace_process_free(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct process_event *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_MEMORY_FREE;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process execution tracing
SEC("tracepoint/sched/sched_process_exec")
int trace_process_exec(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct process_event *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_EXEC;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill exec info
    __builtin_memset(&event->exec_info, 0, sizeof(event->exec_info));
    
    // Get parent PID and TGID
    bpf_core_read(&event->exec_info.ppid, sizeof(__u32), &task->parent);
    bpf_core_read(&event->exec_info.tgid, sizeof(__u32), &task->tgid);
    
    // Get process start time
    event->exec_info.start_time = event->timestamp;
    
    // Filename would be extracted from tracepoint args in real implementation
    __builtin_memcpy(event->exec_info.filename, event->comm, 16);
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    // Update process lineage map
    struct process_lineage lineage = {
        .pid = pid,
        .ppid = event->exec_info.ppid,
        .tgid = event->exec_info.tgid,
        .start_time = event->exec_info.start_time,
    };
    __builtin_memset(lineage.job_name, 0, sizeof(lineage.job_name));
    bpf_map_update_elem(&process_lineage_map, &pid, &lineage, BPF_ANY);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process exit tracing
SEC("tracepoint/sched/sched_process_exit")
int trace_process_exit(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct process_event *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_EXIT;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill exit info
    __builtin_memset(&event->exit_info, 0, sizeof(event->exit_info));
    // For exit events, we'll get the actual exit code from the tracepoint context
    // task_struct doesn't reliably have exit_code/exit_signal fields across kernel versions
    event->exit_info.exit_code = 0; // Will be filled by tracepoint context if available
    
    // Calculate runtime from lineage map
    struct process_lineage *lineage = bpf_map_lookup_elem(&process_lineage_map, &pid);
    if (lineage) {
        event->exit_info.runtime_ns = event->timestamp - lineage->start_time;
    }
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    // Clean up lineage map entry
    bpf_map_delete_elem(&process_lineage_map, &pid);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process fork tracing - use void* ctx to avoid struct definition issues
SEC("tracepoint/sched/sched_process_fork")
int trace_process_fork(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct process_event *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_FORK;
    event->size = 0;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Fill fork info
    __builtin_memset(&event->fork_info, 0, sizeof(event->fork_info));
    event->fork_info.parent_pid = pid;
    // For fork events, we'll use parent PID and let userspace correlate
    // Reading child_pid from void* ctx requires proper offset which varies by kernel
    event->fork_info.child_pid = 0; // Will be filled by child process exec event
    
    char *pod_uid = bpf_map_lookup_elem(&pod_uid_map, &cgroup_id);
    if (pod_uid) {
        __builtin_memcpy(event->pod_uid, pod_uid, sizeof(event->pod_uid));
    } else {
        __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";