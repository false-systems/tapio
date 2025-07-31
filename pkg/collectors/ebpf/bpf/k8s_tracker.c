//go:build ignore

#include "headers/vmlinux.h"
#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Remove these as they conflict with bpf_helpers.h
// Map definition macros are already in bpf_helpers.h

#define BPF_MAP_TYPE_PERF_EVENT_ARRAY 4
#define BPF_MAP_TYPE_RINGBUF 27
#define BPF_F_CURRENT_CPU 0xffffffffULL

// K8s event types
#define EVENT_K8S_CONTAINER_CREATE 10
#define EVENT_K8S_CONTAINER_DELETE 11
#define EVENT_K8S_NETNS_CREATE    12
#define EVENT_K8S_NETNS_DELETE    13
#define EVENT_K8S_CGROUP_CREATE   14
#define EVENT_K8S_CGROUP_DELETE   15
#define EVENT_K8S_EXEC_IN_POD     16
#define EVENT_K8S_VOLUME_MOUNT     17
#define EVENT_K8S_VOLUME_UMOUNT    18
#define EVENT_K8S_IMAGE_PULL       19
#define EVENT_K8S_NETWORK_SETUP    20
#define EVENT_K8S_POD_SANDBOX      21
#define EVENT_K8S_DNS_QUERY        22
#define EVENT_K8S_SERVICE_CONNECT  23

// TCP states
#define TCP_ESTABLISHED 1

// Network families
#ifndef AF_INET
#define AF_INET 2
#endif

// K8s event structure with enhanced context
struct k8s_event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u64 cgroup_id;         // cgroup ID (can correlate to container)
    u32 namespace_pid;     // PID in namespace
    u32 event_type;
    u32 cpu;
    char comm[TASK_COMM_LEN];
    char container_id[64]; // Container ID from cgroup path
    char pod_uid[64];      // Pod UID extracted from cgroup path
    char namespace[64];    // K8s namespace
    u32 netns_ino;        // Network namespace inode
    u32 mntns_ino;        // Mount namespace inode
    u32 pidns_ino;        // PID namespace inode
    
    // Syscall specific data
    union {
        // For mount operations
        struct {
            char source[128];
            char target[128];
            char fstype[32];
            u64 flags;
        } mount;
        
        // For network operations
        struct {
            u32 saddr;
            u32 daddr;
            u16 sport;
            u16 dport;
            u16 family;
        } net;
        
        // For file operations
        struct {
            char filename[256];
            u64 flags;
            u64 mode;
        } file;
        
        // For exec operations
        struct {
            char filename[256];
            char argv0[64];
        } exec;
    } data;
} __attribute__((packed));

// Use ring buffer for better performance
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 16 * 1024 * 1024); // 16MB
} k8s_events SEC(".maps");

// Map to track K8s pod cgroups
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, u64);   // cgroup_id
    __type(value, struct k8s_pod_info);
} k8s_pods SEC(".maps");

struct k8s_pod_info {
    char pod_uid[64];
    char namespace[64];
    char pod_name[128];
    u64 created_at;
};

// Helper to check if process is in K8s pod
static __always_inline bool is_k8s_pod(struct task_struct *task) {
    // Check if we're in a container namespace
    u32 ns_level = 0;
    if (bpf_core_field_exists(task->nsproxy)) {
        struct pid_namespace *pidns = BPF_CORE_READ(task, nsproxy, pid_ns_for_children);
        if (pidns) {
            ns_level = BPF_CORE_READ(pidns, level);
        }
    }
    
    // K8s pods are typically in namespace level > 0
    return ns_level > 0;
}

// Helper to extract K8s context from cgroup path
// Cgroup v2 paths: /kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod<pod-uid>.slice/cri-containerd-<container-id>.scope
// Cgroup v1 paths: /kubepods/burstable/pod<pod-uid>/<container-id>
static __always_inline void extract_k8s_context(struct k8s_event *event) {
    // Look up pod info from our tracking map
    struct k8s_pod_info *pod_info = bpf_map_lookup_elem(&k8s_pods, &event->cgroup_id);
    if (pod_info) {
        __builtin_memcpy(event->pod_uid, pod_info->pod_uid, sizeof(event->pod_uid));
        __builtin_memcpy(event->namespace, pod_info->namespace, sizeof(event->namespace));
    } else {
        // Fallback: generate a cgroup-based identifier
        event->pod_uid[0] = 'c';
        event->pod_uid[1] = 'g';
        event->pod_uid[2] = ':';
        // Convert cgroup_id to hex string
        for (int i = 0; i < 8 && i < 30; i++) {
            u8 nibble = (event->cgroup_id >> (i * 8)) & 0xFF;
            event->pod_uid[3 + i * 2] = "0123456789abcdef"[(nibble >> 4) & 0xF];
            event->pod_uid[3 + i * 2 + 1] = "0123456789abcdef"[nibble & 0xF];
        }
        event->pod_uid[19] = '\0';
        
        // Mark as unknown namespace
        __builtin_memcpy(event->namespace, "unknown", 8);
    }
    
    // Container ID is typically the last component of cgroup path
    // For now, use a simplified version
    __builtin_memcpy(event->container_id, event->pod_uid, 20);
    event->container_id[19] = '\0';
}

// Helper to get namespace inodes
static __always_inline void get_namespace_inodes(struct k8s_event *event, struct task_struct *task) {
    if (bpf_core_field_exists(task->nsproxy)) {
        struct nsproxy *nsproxy = BPF_CORE_READ(task, nsproxy);
        if (nsproxy) {
            // Network namespace
            struct net *net_ns = BPF_CORE_READ(nsproxy, net_ns);
            if (net_ns) {
                event->netns_ino = BPF_CORE_READ(net_ns, ns.inum);
            }
            
            // Mount namespace
            struct mnt_namespace *mnt_ns = BPF_CORE_READ(nsproxy, mnt_ns);
            if (mnt_ns) {
                event->mntns_ino = BPF_CORE_READ(mnt_ns, ns.inum);
            }
            
            // PID namespace
            struct pid_namespace *pid_ns = BPF_CORE_READ(nsproxy, pid_ns_for_children);
            if (pid_ns) {
                event->pidns_ino = BPF_CORE_READ(pid_ns, ns.inum);
            }
        }
    }
}

// Common event initialization
static __always_inline void init_k8s_event(struct k8s_event *event, u32 event_type) {
    __builtin_memset(event, 0, sizeof(*event));
    
    u64 pid_tgid = bpf_get_current_pid_tgid();
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid_tgid >> 32;
    event->tid = (u32)pid_tgid;
    event->cpu = bpf_get_smp_processor_id();
    event->cgroup_id = bpf_get_current_cgroup_id();
    event->event_type = event_type;
    
    bpf_get_current_comm(event->comm, sizeof(event->comm));
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    extract_k8s_context(event);
    get_namespace_inodes(event, task);
}

// Track pod sandbox creation via clone syscall
SEC("tp_btf/sched/sched_process_fork")
int BPF_PROG(trace_pod_sandbox_create, struct task_struct *parent, struct task_struct *child) {
    // Only track K8s pods
    if (!is_k8s_pod(parent))
        return 0;
    
    // Check if this is creating new namespaces (pod sandbox)
    if (!BPF_CORE_READ(child, nsproxy))
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_POD_SANDBOX);
    
    // Store parent and child PIDs in file data fields
    event->data.file.flags = BPF_CORE_READ(parent, pid);  // Parent PID
    event->data.file.mode = BPF_CORE_READ(child, pid);    // Child PID
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track volume mount operations
SEC("tp_btf/fs/do_mount")
int BPF_PROG(trace_volume_mount, struct path *path, const char *dev_name, 
             const char *dir_name, const char *type, unsigned long flags) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only track K8s pods
    if (!is_k8s_pod(task))
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_VOLUME_MOUNT);
    
    // Copy mount details
    bpf_probe_read_kernel_str(event->data.mount.source, sizeof(event->data.mount.source), dev_name);
    bpf_probe_read_kernel_str(event->data.mount.target, sizeof(event->data.mount.target), dir_name);
    bpf_probe_read_kernel_str(event->data.mount.fstype, sizeof(event->data.mount.fstype), type);
    event->data.mount.flags = flags;
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track exec calls in containers (kubectl exec, debugging)
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec_in_pod(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only track K8s pods
    if (!is_k8s_pod(task))
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_EXEC_IN_POD);
    
    // Get filename from execve args
    const char *filename = (const char *)ctx->args[0];
    bpf_probe_read_user_str(event->data.exec.filename, sizeof(event->data.exec.filename), filename);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track image pulls via file operations
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_image_operations(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only track K8s pods
    if (!is_k8s_pod(task))
        return 0;
    
    // Get filename
    const char *filename = (const char *)ctx->args[1];
    char fname[256];
    bpf_probe_read_user_str(fname, sizeof(fname), filename);
    
    // Check if this looks like an image operation
    // Look for patterns like /var/lib/containerd, /var/lib/docker, layers, diff
    bool is_image_op = false;
    
    // Simple pattern matching (would be more sophisticated in production)
    if (fname[0] == '/' && fname[1] == 'v' && fname[2] == 'a' && fname[3] == 'r') {
        is_image_op = true;
    }
    
    if (!is_image_op)
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_IMAGE_PULL);
    
    __builtin_memcpy(event->data.file.filename, fname, sizeof(event->data.file.filename));
    event->data.file.flags = ctx->args[2];  // open flags
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track network operations for service connections
SEC("tp_btf/sock/inet_sock_set_state")
int BPF_PROG(trace_service_connect, struct sock *sk, int oldstate, int newstate) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only track K8s pods
    if (!is_k8s_pod(task))
        return 0;
    
    // Only track new connections
    if (newstate != TCP_ESTABLISHED)
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_SERVICE_CONNECT);
    
    // Extract connection info
    event->data.net.family = BPF_CORE_READ(sk, __sk_common.skc_family);
    
    if (event->data.net.family == AF_INET) {
        event->data.net.saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
        event->data.net.daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
        event->data.net.sport = BPF_CORE_READ(sk, __sk_common.skc_num);
        event->data.net.dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track cgroup operations for pod lifecycle
SEC("tp_btf/cgroup/cgroup_mkdir")
int BPF_PROG(trace_cgroup_create, struct cgroup *cgrp) {
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_CGROUP_CREATE);
    
    // TODO: Extract cgroup path to identify if it's a K8s pod cgroup
    // For now, we track all cgroup creations and filter in userspace
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track DNS queries from pods
SEC("tracepoint/syscalls/sys_enter_sendto")
int trace_dns_query(struct trace_event_raw_sys_enter *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Only track K8s pods
    if (!is_k8s_pod(task))
        return 0;
    
    // Check if this is a DNS query (port 53)
    struct sockaddr_in *addr = (struct sockaddr_in *)ctx->args[4];
    struct sockaddr_in addr_copy;
    
    if (bpf_probe_read_user(&addr_copy, sizeof(addr_copy), addr) != 0)
        return 0;
    
    if (addr_copy.sin_family != AF_INET || bpf_ntohs(addr_copy.sin_port) != 53)
        return 0;
    
    struct k8s_event *event = bpf_ringbuf_reserve(&k8s_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    init_k8s_event(event, EVENT_K8S_DNS_QUERY);
    
    // Store DNS server info
    event->data.net.family = AF_INET;
    event->data.net.daddr = addr_copy.sin_addr.s_addr;
    event->data.net.dport = 53;
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char _license[] SEC("license") = "GPL";