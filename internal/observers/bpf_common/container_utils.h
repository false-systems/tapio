//go:build ignore

#ifndef __CONTAINER_UTILS_H__
#define __CONTAINER_UTILS_H__

#include "vmlinux_minimal.h"
#include "shared_maps.h"
#include "helpers.h"

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

#endif // __CONTAINER_UTILS_H__