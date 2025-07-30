// SPDX-License-Identifier: GPL-2.0
// K8s systemd service syscall monitoring
// Implements the proposal for deep K8s observability

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#include "common.h"

// Clone flags we care about for K8s
#define CLONE_NEWNS     0x00020000
#define CLONE_NEWPID    0x20000000
#define CLONE_NEWNET    0x40000000

// Socket operations
#define AF_NETLINK      16
#define NETLINK_ROUTE   0
#define NETLINK_NETFILTER 12

// Helper to fill common event fields
static __always_inline void fill_event_common(struct k8s_syscall_event *e, 
                                              u8 service_type, u8 syscall_category) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->tgid = bpf_get_current_pid_tgid() >> 32;
    e->ppid = BPF_CORE_READ(task, real_parent, tgid);
    e->uid = bpf_get_current_uid_gid() >> 32;
    e->gid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    e->service_type = service_type;
    e->syscall_category = syscall_category;
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Get cgroup path for service identification
    struct kernfs_node *kn = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn);
    if (kn) {
        struct kernfs_node *parent = BPF_CORE_READ(kn, parent);
        if (parent) {
            bpf_probe_read_kernel_str(e->cgroup, sizeof(e->cgroup), 
                                      BPF_CORE_READ(parent, name));
        }
    }
}

// KUBELET SYSCALLS

// Monitor clone for pod sandbox creation
SEC("tracepoint/syscalls/sys_enter_clone")
int trace_kubelet_clone(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    // Only track kubelet clone operations
    if (service_type != SERVICE_KUBELET) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_NAMESPACE);
    e->operation_type = 1; // clone
    
    // Get clone flags
    unsigned long flags = (unsigned long)ctx->args[0];
    e->data.ns_op.flags = flags;
    
    // Check for pod sandbox creation pattern
    if ((flags & CLONE_NEWPID) && (flags & CLONE_NEWNET) && (flags & CLONE_NEWNS)) {
        bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "pod_sandbox_create");
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor mount for volume operations
SEC("tracepoint/syscalls/sys_enter_mount")
int trace_kubelet_mount(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (service_type != SERVICE_KUBELET && service_type != SERVICE_RUNTIME) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_MOUNT);
    e->operation_type = 2; // mount
    
    // Get mount source and target
    const char *source = (const char *)ctx->args[0];
    const char *target = (const char *)ctx->args[1];
    const char *fstype = (const char *)ctx->args[2];
    
    bpf_probe_read_user_str(e->data.mount_op.source, sizeof(e->data.mount_op.source), source);
    bpf_probe_read_user_str(e->data.mount_op.target, sizeof(e->data.mount_op.target), target);
    if (fstype) {
        bpf_probe_read_user_str(e->data.mount_op.fstype, sizeof(e->data.mount_op.fstype), fstype);
    }
    
    e->data.mount_op.flags = (unsigned long)ctx->args[3];
    
    // Check if it's a K8s volume mount
    if (service_type == SERVICE_KUBELET) {
        char target_buf[64];
        bpf_probe_read_user_str(target_buf, sizeof(target_buf), target);
        if (bpf_strncmp(target_buf, 13, "/var/lib/kubelet/pods") == 0) {
            bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "volume_mount");
        }
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor setns for entering container namespaces
SEC("tracepoint/syscalls/sys_enter_setns")
int trace_kubelet_setns(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (!service_type) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_NAMESPACE);
    e->operation_type = 3; // setns
    
    int fd = (int)ctx->args[0];
    int nstype = (int)ctx->args[1];
    e->data.ns_op.namespace_type = nstype;
    
    // This is typically kubectl exec or debug operations
    if (service_type == SERVICE_KUBELET) {
        bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "container_enter");
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// CONTAINER RUNTIME SYSCALLS

// Monitor openat for image layer operations
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_runtime_openat(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (service_type != SERVICE_RUNTIME) return 0;
    
    const char *pathname = (const char *)ctx->args[1];
    char path[128];
    bpf_probe_read_user_str(path, sizeof(path), pathname);
    
    // Only track image/layer operations
    if (bpf_strncmp(path, 17, "/var/lib/containerd") != 0 && 
        bpf_strncmp(path, 13, "/var/lib/docker") != 0) {
        return 0;
    }
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_IMAGE);
    e->operation_type = 4; // open
    
    bpf_probe_read_kernel_str(e->data.image_op.image_path, 
                              sizeof(e->data.image_op.image_path), path);
    
    // Check if it's a layer operation
    if (bpf_strncmp(path, 6, "layers") > 0 || bpf_strncmp(path, 11, "snapshots") > 0) {
        e->data.image_op.operation = 1; // layer access
        bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "layer_operation");
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// KUBE-PROXY SYSCALLS

// Monitor socket operations for iptables/netfilter
SEC("tracepoint/syscalls/sys_enter_socket")
int trace_proxy_socket(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (service_type != SERVICE_PROXY) return 0;
    
    int family = (int)ctx->args[0];
    int type = (int)ctx->args[1];
    
    // Only track netlink sockets (used for iptables)
    if (family != AF_NETLINK) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_NETWORK);
    e->operation_type = 5; // socket
    
    e->data.net_op.socket_family = family;
    e->data.net_op.socket_type = type;
    
    bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "netfilter_socket");
    bpf_probe_read_kernel_str(e->data.net_op.operation, 
                              sizeof(e->data.net_op.operation), "iptables_update");
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Monitor sendmsg for netlink operations (service endpoint updates)
SEC("tracepoint/syscalls/sys_enter_sendmsg")
int trace_proxy_sendmsg(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (service_type != SERVICE_PROXY) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_NETWORK);
    e->operation_type = 6; // sendmsg
    
    bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "service_update");
    bpf_probe_read_kernel_str(e->data.net_op.operation, 
                              sizeof(e->data.net_op.operation), "endpoint_sync");
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// CNI SYSCALLS

// Monitor network namespace operations by CNI plugins
SEC("tracepoint/syscalls/sys_enter_unshare")
int trace_cni_unshare(struct trace_event_raw_sys_enter *ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    u8 service_type = is_k8s_service_pid(pid);
    
    if (service_type != SERVICE_CNI) return 0;
    
    int flags = (int)ctx->args[0];
    
    // Only track network namespace operations
    if (!(flags & CLONE_NEWNET)) return 0;
    
    struct k8s_syscall_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    fill_event_common(e, service_type, SYSCALL_NAMESPACE);
    e->operation_type = 7; // unshare
    
    e->data.ns_op.flags = flags;
    bpf_probe_read_kernel_str(e->service_name, sizeof(e->service_name), "cni_netns_setup");
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Program to update service PID mappings when systemd starts a service
SEC("tracepoint/sched/sched_process_fork")
int trace_service_fork(struct trace_event_raw_sched_process_fork *ctx) {
    u32 parent_pid = ctx->parent_pid;
    u32 child_pid = ctx->child_pid;
    
    // Check if parent is a K8s service
    u8 *parent_service = bpf_map_lookup_elem(&k8s_service_pids, &parent_pid);
    if (!parent_service) return 0;
    
    // Inherit service type for child process
    u8 service_type = *parent_service;
    bpf_map_update_elem(&k8s_service_pids, &child_pid, &service_type, BPF_ANY);
    
    return 0;
}

// Program to clean up PID mappings when process exits
SEC("tracepoint/sched/sched_process_exit")
int trace_service_exit(struct trace_event_raw_sched_process_template *ctx) {
    u32 pid = ctx->pid;
    bpf_map_delete_elem(&k8s_service_pids, &pid);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";