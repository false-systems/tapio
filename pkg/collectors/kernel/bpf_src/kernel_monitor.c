// SPDX-License-Identifier: GPL-2.0
// Focused kernel monitoring - ConfigMap/Secret access ONLY

#include "../../bpf_common/vmlinux_minimal.h"

#ifdef __BPF_HELPERS_H__
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#else
#include "../../bpf_common/helpers.h"
#endif

// Event types - ONLY what we actually monitor
#define EVENT_TYPE_CONFIGMAP_ACCESS   1
#define EVENT_TYPE_SECRET_ACCESS      2

// Kernel event structure (must match Go struct)
struct kernel_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 event_type;
    __u64 size;          // Kept for struct alignment
    char comm[16];
    __u64 cgroup_id;     // For pod correlation
    char pod_uid[36];    // Pod UID if available
    union {
        __u8 data[64];
        struct {
            char mount_path[64];  // ConfigMap/Secret mount path
        } config_info;
    };
} __attribute__((packed));

// Single ring buffer for events - that's all we need
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB buffer
} events SEC(".maps");

// Helper function to check if path is a ConfigMap mount
static __always_inline int is_configmap_path(const char *path) {
    const char configmap_pattern[] = "kubernetes.io~configmap";
    char buf[64];
    
    if (bpf_probe_read_user_str(buf, sizeof(buf), path) < 0) {
        return 0;
    }
    
    // Simple pattern matching for ConfigMap paths
    for (int i = 0; i < sizeof(buf) - sizeof(configmap_pattern); i++) {
        int match = 1;
        for (int j = 0; j < sizeof(configmap_pattern) - 1; j++) {
            if (buf[i + j] != configmap_pattern[j]) {
                match = 0;
                break;
            }
        }
        if (match) return 1;
    }
    return 0;
}

// Helper function to check if path is a Secret mount
static __always_inline int is_secret_path(const char *path) {
    const char secret_pattern[] = "kubernetes.io~secret";
    char buf[64];
    
    if (bpf_probe_read_user_str(buf, sizeof(buf), path) < 0) {
        return 0;
    }
    
    // Simple pattern matching for Secret paths
    for (int i = 0; i < sizeof(buf) - sizeof(secret_pattern); i++) {
        int match = 1;
        for (int j = 0; j < sizeof(secret_pattern) - 1; j++) {
            if (buf[i + j] != secret_pattern[j]) {
                match = 0;
                break;
            }
        }
        if (match) return 1;
    }
    return 0;
}

// Helper to get cgroup ID for container correlation
static __always_inline __u64 get_cgroup_id() {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    __u64 cgroup_id = 0;
    
    // Try to read cgroup ID using BPF helper
    struct cgroup *cgrp;
    if (bpf_core_field_exists(task->cgroups)) {
        bpf_probe_read_kernel(&cgrp, sizeof(cgrp), &task->cgroups->dfl_cgrp);
        if (cgrp) {
            bpf_probe_read_kernel(&cgroup_id, sizeof(cgroup_id), &cgrp->kn->id);
        }
    }
    
    return cgroup_id;
}

// Main tracepoint for openat syscall - catches ConfigMap/Secret access
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx) {
    // Get the filename from syscall arguments
    const char *filename = (const char *)ctx->args[1];
    if (!filename) {
        return 0;
    }
    
    // Check if this is a ConfigMap or Secret access
    int is_configmap = is_configmap_path(filename);
    int is_secret = is_secret_path(filename);
    
    if (!is_configmap && !is_secret) {
        return 0;  // Not interested in other files
    }
    
    // Allocate event
    struct kernel_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    // Fill basic event info
    event->timestamp = bpf_ktime_get_ns();
    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event->event_type = is_secret ? EVENT_TYPE_SECRET_ACCESS : EVENT_TYPE_CONFIGMAP_ACCESS;
    event->size = 0;
    event->cgroup_id = get_cgroup_id();
    
    // Get process name
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Copy mount path
    bpf_probe_read_user_str(&event->config_info.mount_path, 
                            sizeof(event->config_info.mount_path), 
                            filename);
    
    // Clear pod_uid for now (would need additional correlation)
    __builtin_memset(event->pod_uid, 0, sizeof(event->pod_uid));
    
    // Submit event
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL";