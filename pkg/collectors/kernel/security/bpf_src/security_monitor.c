// SPDX-License-Identifier: GPL-2.0
// Security monitoring eBPF program - privilege escalation, kernel modules, process injection

#include "../../../bpf_common/vmlinux_minimal.h"
#include "../../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Security event types
#define EVENT_TYPE_PRIVILEGE_ESCALATION 11
#define EVENT_TYPE_KERNEL_MODULE_LOAD   12
#define EVENT_TYPE_KERNEL_MODULE_UNLOAD 13
#define EVENT_TYPE_PROCESS_INJECTION    14
#define EVENT_TYPE_CORE_DUMP           15
#define EVENT_TYPE_PTRACE_ATTACH       16

// Security event structure
struct security_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u32 target_pid;      // For process injection events
    char comm[16];
    __u64 cgroup_id;
    char pod_uid[36];
    union {
        struct {
            __u32 old_uid;
            __u32 new_uid;
            __u32 old_gid;
            __u32 new_gid;
            __u64 capabilities;
        } privilege_change;
        struct {
            char module_name[64];
            __u64 module_addr;
            __u32 module_size;
        } module_info;
        struct {
            char target_comm[16];
            __u32 ptrace_request;
            __u64 ptrace_addr;
        } injection_info;
        __u8 data[96];
    };
} __attribute__((packed));

// Maps for security monitoring
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB - production optimized
} security_events SEC(".maps");

// Track container processes
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // PID
    __type(value, __u8);  // Flag
} container_pids SEC(".maps");

// Track privileged processes to detect escalation
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 5120);
    __type(key, __u32);   // PID
    __type(value, __u64); // Capabilities bitmask
} process_capabilities SEC(".maps");

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
    ret = bpf_core_read(&css, sizeof(css), &css_set_ptr->subsys[0]);
    if (ret != 0 || !css) {
        return 0;
    }

    struct cgroup *cgroup_ptr = NULL;
    ret = bpf_core_read(&cgroup_ptr, sizeof(cgroup_ptr), &css->cgroup);
    if (ret != 0 || !cgroup_ptr) {
        return 0;
    }

    struct kernfs_node *kn = NULL;
    ret = bpf_core_read(&kn, sizeof(kn), &cgroup_ptr->kn);
    if (ret == 0 && kn) {
        __u64 ino = 0;
        ret = bpf_core_read(&ino, sizeof(ino), &kn->ino);
        if (ret == 0 && ino != 0) {
            return ino;
        }
    }

    return 0;
}

// Privilege escalation detection - setuid syscalls
SEC("tracepoint/syscalls/sys_enter_setuid")
int trace_setuid(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    // Focus on container processes for security monitoring
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PRIVILEGE_ESCALATION;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get current credentials
    const struct cred *cred = NULL;
    bpf_core_read(&cred, sizeof(cred), &task->cred);
    if (cred) {
        bpf_core_read(&event->privilege_change.old_uid, sizeof(__u32), &cred->uid);
        bpf_core_read(&event->privilege_change.old_gid, sizeof(__u32), &cred->gid);
        // Extract new UID from syscall args (first argument to setuid)
        event->privilege_change.new_uid = (__u32)BPF_CORE_READ(ctx, args[0]);
        event->privilege_change.new_gid = 0; // setuid doesn't change GID
    }
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Privilege escalation detection - setgid syscalls
SEC("tracepoint/syscalls/sys_enter_setgid")
int trace_setgid(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PRIVILEGE_ESCALATION;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Capability changes detection
SEC("kprobe/commit_creds")
int trace_commit_creds(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PRIVILEGE_ESCALATION;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to read new credentials from first argument using correct helper
    void *cred_ptr = (void *)get_kprobe_func_arg(ctx, 0);
    
    if (cred_ptr) {
        struct cred *new_creds = NULL;
        bpf_probe_read_kernel(&new_creds, sizeof(new_creds), &cred_ptr);
        if (new_creds) {
            BPF_CORE_READ_INTO(&event->privilege_change.new_uid, new_creds, uid.val);
            BPF_CORE_READ_INTO(&event->privilege_change.new_gid, new_creds, gid.val);
            // Read capabilities if available
            // BPF_CORE_READ_INTO(&event->privilege_change.capabilities, new_creds, cap_effective);
        }
    }
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Kernel module loading detection
SEC("tracepoint/module/module_load")
int trace_module_load(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_KERNEL_MODULE_LOAD;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Module name would be extracted from tracepoint args
    __builtin_memcpy(event->module_info.module_name, "unknown", 7);
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Kernel module unloading detection
SEC("tracepoint/module/module_free")
int trace_module_free(void *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_KERNEL_MODULE_UNLOAD;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    __builtin_memcpy(event->module_info.module_name, "unknown", 7);
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process injection detection - ptrace attach
SEC("kprobe/ptrace_attach")
int trace_ptrace_attach(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PTRACE_ATTACH;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Extract target task from ptrace_attach first argument using architecture-independent method
    // Read target task pointer from first argument using correct helper
    void *task_ptr = (void *)get_kprobe_func_arg(ctx, 0);
    
    if (task_ptr) {
        struct task_struct *target_task = NULL;
        bpf_probe_read_kernel(&target_task, sizeof(target_task), &task_ptr);
        if (target_task) {
            BPF_CORE_READ_INTO(&event->target_pid, target_task, pid);
            bpf_core_read_str(event->injection_info.target_comm, sizeof(event->injection_info.target_comm), &target_task->comm);
        } else {
            event->target_pid = 0;
            __builtin_memcpy(event->injection_info.target_comm, "unknown", 7);
        }
    } else {
        event->target_pid = 0;
        __builtin_memcpy(event->injection_info.target_comm, "unknown", 7);
    }
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Process memory access detection
SEC("tracepoint/syscalls/sys_enter_process_vm_readv")
int trace_process_vm_readv(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_PROCESS_INJECTION;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Extract target PID from process_vm_readv first argument
    event->target_pid = (__u32)BPF_CORE_READ(ctx, args[0]);
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Core dump detection
SEC("kprobe/do_coredump")
int trace_do_coredump(struct pt_regs *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    
    if (!is_container_process(pid))
        return 0;
    
    struct security_event *event = bpf_ringbuf_reserve(&security_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = get_cgroup_id(task);
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = (__u32)pid_tgid;
    event->event_type = EVENT_TYPE_CORE_DUMP;
    event->cgroup_id = cgroup_id;
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";