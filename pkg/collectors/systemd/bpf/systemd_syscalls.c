// SPDX-License-Identifier: GPL-2.0
// Systemd K8s service syscall monitoring for deep observability

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Syscall categories for K8s operations
#define SYSCALL_NAMESPACE   1  // clone, unshare, setns
#define SYSCALL_MOUNT       2  // mount, umount
#define SYSCALL_CGROUP      3  // cgroup operations
#define SYSCALL_NETWORK     4  // socket, iptables
#define SYSCALL_CONTAINER   5  // container runtime ops
#define SYSCALL_IMAGE       6  // image layer operations

// K8s service types
#define SERVICE_KUBELET     1
#define SERVICE_RUNTIME     2  // containerd/docker
#define SERVICE_PROXY       3  // kube-proxy
#define SERVICE_CNI         4  // calico, cilium, etc

struct k8s_syscall_event {
    u64 timestamp;
    u32 pid;
    u32 tgid;
    u32 ppid;
    u32 uid;
    u32 gid;
    u8  syscall_category;
    u8  service_type;
    u8  operation_type;
    u8  pad;
    char comm[16];
    char cgroup[64];      // /system.slice/kubelet.service
    char service_name[32]; // kubelet.service
    
    union {
        // Namespace operations (clone, unshare, setns)
        struct {
            u32 flags;
            u32 target_pid;
            u32 namespace_type;
            char namespace_path[64];
        } ns_op;
        
        // Mount operations
        struct {
            char source[64];
            char target[64];
            char fstype[16];
            u32 flags;
        } mount_op;
        
        // Container operations
        struct {
            char container_id[64];
            u32 operation; // create, start, stop
        } container_op;
        
        // Network operations
        struct {
            u32 socket_family;
            u32 socket_type;
            u32 port;
            char operation[32]; // iptables, netlink, etc
        } net_op;
        
        // Image operations
        struct {
            char image_path[64];
            char layer_id[64];
            u32 operation; // pull, extract, mount
        } image_op;
    } data;
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024);
} events SEC(".maps");

// Map to track PIDs belonging to K8s services
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, u32);   // PID
    __type(value, u8);  // Service type
} k8s_service_pids SEC(".maps");

// Map to track cgroup to service mapping
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 100);
    __type(key, char[64]);  // cgroup path
    __type(value, u8);      // Service type  
} cgroup_to_service SEC(".maps");

// Helper to get service type from cgroup path
static inline u8 get_service_from_cgroup(struct task_struct *task) {
    char cgroup_path[64] = {};
    
    // Read cgroup path from task
    // This is simplified - in reality we'd walk the cgroup hierarchy
    struct kernfs_node *kn = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn);
    if (kn) {
        bpf_probe_read_kernel_str(cgroup_path, sizeof(cgroup_path), 
                                  BPF_CORE_READ(kn, name));
    }
    
    // Check if it's a K8s service cgroup
    if (bpf_strncmp(cgroup_path, 7, "kubelet") == 0) return SERVICE_KUBELET;
    if (bpf_strncmp(cgroup_path, 11, "containerd") == 0) return SERVICE_RUNTIME;
    if (bpf_strncmp(cgroup_path, 10, "docker") == 0) return SERVICE_RUNTIME;
    if (bpf_strncmp(cgroup_path, 10, "kube-proxy") == 0) return SERVICE_PROXY;
    if (bpf_strncmp(cgroup_path, 6, "calico") == 0) return SERVICE_CNI;
    if (bpf_strncmp(cgroup_path, 6, "cilium") == 0) return SERVICE_CNI;
    
    return 0;
}

// Helper to check if PID belongs to K8s service
static inline u8 is_k8s_service_pid(u32 pid) {
    u8 *service_type = bpf_map_lookup_elem(&k8s_service_pids, &pid);
    return service_type ? *service_type : 0;
}

// Monitor clone - container/namespace creation by kubelet
SEC("tracepoint/syscalls/sys_enter_clone")
int trace_clone(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // Check if this PID belongs to a K8s service
    u8 service_type = is_k8s_service_pid(pid);
    if (!service_type) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tgid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() >> 32;
    e->gid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->syscall_category = SYSCALL_NAMESPACE;
    e->service_type = service_type;
    e->operation_type = 1; // clone
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Get clone flags
    unsigned long flags = (unsigned long)ctx->args[0];
    e->data.ns_op.flags = flags;
    
    // Track if this is creating new namespaces (pod sandbox)
    if (flags & (CLONE_NEWNS | CLONE_NEWPID | CLONE_NEWNET)) {
        // This is likely pod sandbox creation
        e->data.ns_op.namespace_type = flags;
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor mount - volume operations by kubelet/runtime
SEC("tracepoint/syscalls/sys_enter_mount")
int trace_mount(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    u8 service_type = is_k8s_service_pid(pid);
    if (!service_type) return 0;
    
    pid_t target_pid = (pid_t)ctx->args[0];
    int sig = (int)ctx->args[1];
    
    // Skip self-signals
    if (target_pid == bpf_get_current_pid_tgid() >> 32) return 0;
    
    struct systemd_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->event_type = SYSTEMD_KILL;
    e->data.kill.target_pid = target_pid;
    e->data.kill.signal = sig;
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Try to get target process comm
    struct task_struct *target = bpf_get_task_by_pid(target_pid);
    if (target) {
        bpf_probe_read_kernel_str(e->target_comm, sizeof(e->target_comm), target->comm);
        e->target_k8s = is_k8s_component(e->target_comm);
        bpf_task_release(target);
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor setns - namespace switching for container entry
SEC("tracepoint/syscalls/sys_enter_setns")
int trace_setns(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    u8 service_type = is_k8s_service_pid(pid);
    if (!service_type) return 0;
    
    const char *pathname = (const char *)ctx->args[1];
    char path[64];
    bpf_probe_read_user_str(path, sizeof(path), pathname);
    
    // Only track cgroup operations
    if (bpf_strncmp(path, 13, "/sys/fs/cgroup") != 0) return 0;
    
    struct systemd_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->event_type = SYSTEMD_CGROUP;
    e->data.cgroup.operation = 1; // open
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    bpf_probe_read_kernel_str(e->data.cgroup.cgroup_path, sizeof(e->data.cgroup.cgroup_path), path);
    
    // Check if it's K8s-related cgroup
    e->target_k8s = (bpf_strncmp(path, 7, "kubelet") == 0 || 
                     bpf_strncmp(path, 11, "kubepods") == 0);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor mount operations - critical for K8s volumes
SEC("tracepoint/syscalls/sys_enter_mount")
int trace_mount(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    if (!is_systemd(task)) return 0;
    
    struct systemd_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->event_type = SYSTEMD_MOUNT;
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Get mount target
    const char *target = (const char *)ctx->args[1];
    bpf_probe_read_user_str(e->data.mount.mount_path, sizeof(e->data.mount.mount_path), target);
    
    // Get filesystem type
    const char *fstype = (const char *)ctx->args[2];
    if (fstype) {
        bpf_probe_read_user_str(e->data.mount.fstype, sizeof(e->data.mount.fstype), fstype);
    }
    
    // Check if K8s-related mount
    e->target_k8s = (bpf_strncmp(e->data.mount.mount_path, 13, "/var/lib/kubelet") == 0);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor reboot/shutdown - critical for K8s node availability
SEC("tracepoint/syscalls/sys_enter_reboot")
int trace_reboot(struct trace_event_raw_sys_enter *ctx) {
    struct systemd_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->event_type = SYSTEMD_REBOOT;
    e->target_k8s = 1; // Always relevant for K8s
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";