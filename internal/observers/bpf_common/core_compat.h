/* SPDX-License-Identifier: GPL-2.0 */
/* CO-RE Compatibility Layer for Tapio eBPF Programs
 * Provides runtime detection and graceful fallbacks for different kernel versions
 */

#ifndef __BPF_CORE_COMPAT_H__
#define __BPF_CORE_COMPAT_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

/* Kernel version thresholds for feature availability */
#define MIN_KERNEL_VERSION_BTF      0x050400  /* 5.4.0 - BTF support */
#define MIN_KERNEL_VERSION_RINGBUF  0x050800  /* 5.8.0 - Ring buffer support */
#define MIN_KERNEL_VERSION_CGROUP2  0x040E00  /* 4.14.0 - CGroup v2 support */

/* Feature detection flags */
struct core_features {
    __u8 has_btf;
    __u8 has_ringbuf;
    __u8 has_cgroup_v2;
    __u8 has_task_struct_cgroups;
    __u8 has_kernfs_node;
    __u8 _pad[3];
} __attribute__((packed));

/* Global feature detection (populated at program load) */
static struct core_features g_core_features = {0};

/**
 * detect_core_features - Runtime detection of CO-RE and kernel features
 * Should be called once at program initialization
 */
static __always_inline void detect_core_features(void)
{
    struct task_struct *dummy_task = NULL;
    
    /* BTF support detection */
#ifdef bpf_core_field_exists
    g_core_features.has_btf = 1;
#else
    g_core_features.has_btf = 0;
#endif

    /* Ring buffer support detection */
#ifdef BPF_FUNC_ringbuf_reserve
    g_core_features.has_ringbuf = 1;
#else
    g_core_features.has_ringbuf = 0;
#endif

    /* CGroup v2 support - assume available on modern kernels */
    g_core_features.has_cgroup_v2 = g_core_features.has_btf;

    /* Task struct cgroups field availability */
    if (g_core_features.has_btf) {
        g_core_features.has_task_struct_cgroups = bpf_core_field_exists(dummy_task->cgroups) ? 1 : 0;
        g_core_features.has_kernfs_node = bpf_core_type_exists(struct kernfs_node) ? 1 : 0;
    } else {
        /* Conservative fallback - assume older kernel without these features */
        g_core_features.has_task_struct_cgroups = 0;
        g_core_features.has_kernfs_node = 0;
    }
}

/**
 * safe_cgroup_id_extraction - Safe cgroup ID extraction with multiple methods
 * @task: task_struct pointer
 * 
 * This function tries multiple extraction methods in order of reliability:
 * 1. CO-RE with kernfs inode (most reliable)
 * 2. CO-RE with cgroup ID + offset
 * 3. Fallback methods for older kernels
 * 
 * Returns: cgroup ID or 0 on failure
 */
static __always_inline __u64 safe_cgroup_id_extraction(struct task_struct *task)
{
    if (!task) {
        return 0;
    }

    /* Method 1: Full CO-RE with BTF support */
    if (g_core_features.has_btf && g_core_features.has_task_struct_cgroups) {
        struct css_set *css_set_ptr;
        if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) == 0 && css_set_ptr) {
            
            /* Try unified cgroup hierarchy first (cgroup v2) */
            struct cgroup_subsys_state *css;
            if (BPF_CORE_READ_INTO(&css, css_set_ptr, subsys[0]) == 0 && css) {
                
                struct cgroup *cgroup_ptr;
                if (BPF_CORE_READ_INTO(&cgroup_ptr, css, cgroup) == 0 && cgroup_ptr) {
                    
                    /* Prefer kernfs inode if available */
                    if (g_core_features.has_kernfs_node && 
                        bpf_core_field_exists(cgroup_ptr->kn)) {
                        struct kernfs_node *kn;
                        if (BPF_CORE_READ_INTO(&kn, cgroup_ptr, kn) == 0 && kn) {
                            __u64 ino;
                            if (BPF_CORE_READ_INTO(&ino, kn, ino) == 0 && ino != 0) {
                                return ino;
                            }
                        }
                    }
                    
                    /* Fallback to cgroup ID with offset */
                    if (bpf_core_field_exists(cgroup_ptr->id)) {
                        int cgroup_id;
                        if (BPF_CORE_READ_INTO(&cgroup_id, cgroup_ptr, id) == 0 && 
                            cgroup_id > 0) {
                            return (__u64)cgroup_id + 0x100000000ULL;
                        }
                    }
                }
            }
        }
    }

    /* Method 2: Fallback for older kernels without BTF */
    if (!g_core_features.has_btf) {
        /* Try to use the BPF helper if available */
        __u64 cgroup_id = bpf_get_current_cgroup_id();
        if (cgroup_id != 0) {
            return cgroup_id;
        }
    }

    /* Method 3: Last resort - hash of task pointer */
    __u64 task_addr = (__u64)task;
    if (task_addr != 0) {
        return (task_addr >> 8) + 0x300000000ULL;  /* Use different offset */
    }

    return 0;
}

/**
 * safe_task_parent_read - Safely read task parent with CO-RE
 * @task: task_struct pointer
 * @parent_pid: output for parent PID
 * 
 * Returns: 0 on success, negative on error
 */
static __always_inline int safe_task_parent_read(struct task_struct *task, __u32 *parent_pid)
{
    if (!task || !parent_pid) {
        return -1;
    }

    *parent_pid = 0;

    if (g_core_features.has_btf && 
        bpf_core_field_exists(task->real_parent) &&
        bpf_core_field_exists(((struct task_struct *)0)->tgid)) {
        
        struct task_struct *parent;
        if (BPF_CORE_READ_INTO(&parent, task, real_parent) == 0 && parent) {
            return BPF_CORE_READ_INTO(parent_pid, parent, tgid);
        }
    }

    /* Fallback: try to use parent field if real_parent doesn't exist */
    if (g_core_features.has_btf && bpf_core_field_exists(task->parent)) {
        struct task_struct *parent;
        if (BPF_CORE_READ_INTO(&parent, task, parent) == 0 && parent) {
            if (bpf_core_field_exists(parent->tgid)) {
                return BPF_CORE_READ_INTO(parent_pid, parent, tgid);
            }
        }
    }

    return -1;
}

/**
 * safe_sock_read - Safely read socket information with CO-RE
 * @sk: sock pointer
 * @saddr: output source address
 * @daddr: output destination address
 * @sport: output source port
 * @dport: output destination port
 * 
 * Returns: 0 on success, negative on error
 */
static __always_inline int safe_sock_read(struct sock *sk, __u32 *saddr, __u32 *daddr, 
                                         __u16 *sport, __u16 *dport)
{
    if (!sk || !saddr || !daddr || !sport || !dport) {
        return -1;
    }

    /* Initialize outputs */
    *saddr = *daddr = 0;
    *sport = *dport = 0;

    if (g_core_features.has_btf) {
        /* Use CO-RE to read socket fields */
        if (bpf_core_field_exists(sk->__sk_common)) {
            BPF_CORE_READ_INTO(sport, sk, __sk_common.skc_num);
            BPF_CORE_READ_INTO(dport, sk, __sk_common.skc_dport);
            BPF_CORE_READ_INTO(saddr, sk, __sk_common.skc_rcv_saddr);
            BPF_CORE_READ_INTO(daddr, sk, __sk_common.skc_daddr);
            
            /* Convert port from network byte order */
            *dport = __builtin_bswap16(*dport);
            return 0;
        }
    }

    /* Fallback: use probe_read_kernel for older kernels */
    struct sock_common sk_common;
    if (bpf_probe_read_kernel(&sk_common, sizeof(sk_common), &sk->__sk_common) == 0) {
        *sport = sk_common.skc_num;
        *dport = __builtin_bswap16(sk_common.skc_dport);
        *saddr = sk_common.skc_rcv_saddr;
        *daddr = sk_common.skc_daddr;
        return 0;
    }

    return -1;
}

/**
 * is_core_compatible - Check if current kernel supports required CO-RE features
 * @required_features: Bitmask of required features
 * 
 * Returns: 1 if compatible, 0 otherwise
 */
#define CORE_FEAT_BTF         (1 << 0)
#define CORE_FEAT_RINGBUF     (1 << 1)
#define CORE_FEAT_CGROUP_V2   (1 << 2)
#define CORE_FEAT_TASK_CGROUPS (1 << 3)

static __always_inline int is_core_compatible(__u8 required_features)
{
    if ((required_features & CORE_FEAT_BTF) && !g_core_features.has_btf)
        return 0;
    if ((required_features & CORE_FEAT_RINGBUF) && !g_core_features.has_ringbuf)
        return 0;
    if ((required_features & CORE_FEAT_CGROUP_V2) && !g_core_features.has_cgroup_v2)
        return 0;
    if ((required_features & CORE_FEAT_TASK_CGROUPS) && !g_core_features.has_task_struct_cgroups)
        return 0;
    
    return 1;
}

/**
 * INIT_CORE_COMPAT - Macro to initialize CO-RE compatibility in each program
 * Should be called at the beginning of main tracepoint/kprobe handlers
 */
#define INIT_CORE_COMPAT() \
    do { \
        static __u8 _initialized = 0; \
        if (!_initialized) { \
            detect_core_features(); \
            _initialized = 1; \
        } \
    } while (0)

/**
 * REQUIRE_CORE_FEATURES - Macro to check required features and exit if not available
 */
#define REQUIRE_CORE_FEATURES(features) \
    do { \
        if (!is_core_compatible(features)) { \
            bpf_printk("Required CO-RE features not available: %d", features); \
            return 0; \
        } \
    } while (0)

#endif /* __BPF_CORE_COMPAT_H__ */