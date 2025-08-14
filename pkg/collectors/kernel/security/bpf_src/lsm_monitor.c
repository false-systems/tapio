// SPDX-License-Identifier: GPL-2.0
// eBPF LSM (Linux Security Module) for advanced security monitoring
// Features: File integrity, capability checks, mandatory access control

#include "../../../bpf_common/vmlinux_minimal.h"
#include "../../../bpf_common/helpers.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Security event types
#define SEC_EVENT_FILE_OPEN      1
#define SEC_EVENT_FILE_EXEC      2
#define SEC_EVENT_CAPABILITY_USE 3
#define SEC_EVENT_PTRACE         4
#define SEC_EVENT_BPRM_CHECK     5
#define SEC_EVENT_SOCKET_CREATE  6
#define SEC_EVENT_SOCKET_CONNECT 7
#define SEC_EVENT_TASK_SETUID    8
#define SEC_EVENT_MMAP_FILE      9
#define SEC_EVENT_KERNEL_MODULE  10

// File access modes
#define MAY_EXEC    0x01
#define MAY_WRITE   0x02
#define MAY_READ    0x04
#define MAY_APPEND  0x08

// Capability bits (partial list)
#define CAP_DAC_OVERRIDE     1
#define CAP_DAC_READ_SEARCH  2
#define CAP_KILL             5
#define CAP_SETUID           7
#define CAP_SETGID           8
#define CAP_NET_BIND_SERVICE 10
#define CAP_NET_RAW          13
#define CAP_SYS_MODULE       16
#define CAP_SYS_RAWIO        17
#define CAP_SYS_PTRACE       19
#define CAP_SYS_ADMIN        21
#define CAP_SYS_RESOURCE     24
#define CAP_AUDIT_WRITE      29
#define CAP_BPF              39

// Security labels for mandatory access control
struct security_label {
    __u32 level;     // Security level (0=public, 1=confidential, 2=secret, 3=top-secret)
    __u32 categories; // Category bitmap
} __attribute__((packed));

// Security event structure
struct lsm_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 uid;
    __u32 gid;
    __u32 event_type;
    __u32 flags;
    __u64 cgroup_id;
    char comm[16];
    char filename[128];
    union {
        struct {
            __u32 requested_cap;
            __u32 effective_caps;
            __u32 permitted_caps;
            __u32 inheritable_caps;
        } capability;
        struct {
            __u32 old_uid;
            __u32 new_uid;
            __u32 old_gid;
            __u32 new_gid;
        } creds;
        struct {
            __u32 target_pid;
            __u32 ptrace_mode;
        } ptrace;
        struct {
            __u32 socket_family;
            __u32 socket_type;
            __u32 socket_protocol;
        } socket;
        struct {
            __u64 addr;
            __u64 len;
            __u32 prot;
            __u32 file_mode;
        } mmap;
        __u8 data[64];
    };
} __attribute__((packed));

// File integrity information
struct file_integrity {
    __u8 hash[32];      // SHA-256 hash
    __u64 last_modified;
    __u32 uid;
    __u32 gid;
    __u32 mode;
    __u32 _pad;
} __attribute__((packed));

// Process security context
struct process_context {
    struct security_label label;
    __u32 uid;
    __u32 gid;
    __u64 capabilities;
    __u32 flags;
    __u32 _pad;
} __attribute__((packed));

// Maps for LSM
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB - production optimized
} lsm_events SEC(".maps");

// Trusted binary hashes for integrity checking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u8[32]);  // SHA-256 hash
    __type(value, struct file_integrity);
} trusted_binaries SEC(".maps");

// Process security contexts
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u32);  // PID
    __type(value, struct process_context);
} process_contexts SEC(".maps");

// Capability usage statistics
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 64);  // One per capability
    __type(key, __u32);
    __type(value, __u64);
} capability_stats SEC(".maps");

// Security policy rules
struct policy_rule {
    __u32 subject_uid;
    __u32 object_label;
    __u32 allowed_access;
    __u32 flags;
} __attribute__((packed));

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1000);
    __type(key, __u64);  // Subject-Object pair
    __type(value, struct policy_rule);
} security_policies SEC(".maps");

// Helper to get process security context
static __always_inline struct process_context *get_process_context(__u32 pid)
{
    struct process_context *ctx = bpf_map_lookup_elem(&process_contexts, &pid);
    if (!ctx) {
        // Create default context
        struct process_context new_ctx = {
            .label = { .level = 0, .categories = 0 },
            .uid = bpf_get_current_uid_gid() & 0xFFFFFFFF,
            .gid = bpf_get_current_uid_gid() >> 32,
            .capabilities = 0,
            .flags = 0
        };
        bpf_map_update_elem(&process_contexts, &pid, &new_ctx, BPF_NOEXIST);
        ctx = bpf_map_lookup_elem(&process_contexts, &pid);
    }
    return ctx;
}

// Helper to check mandatory access control
static __always_inline bool check_mac_access(struct process_context *subject,
                                             struct security_label *object,
                                             __u32 requested_access)
{
    // Bell-LaPadula model: no read up, no write down
    if (requested_access & MAY_READ) {
        if (subject->label.level < object->level)
            return false; // Cannot read higher classification
    }
    
    if (requested_access & MAY_WRITE) {
        if (subject->label.level > object->level)
            return false; // Cannot write to lower classification
    }
    
    // Check category restrictions
    if ((subject->label.categories & object->categories) != object->categories)
        return false; // Missing required categories
    
    return true;
}

// LSM hook for file open
SEC("lsm/file_open")
int BPF_PROG(lsm_file_open, struct file *file, int mask)
{
    if (!file)
        return 0;
    
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    struct process_context *proc_ctx = get_process_context(pid);
    if (!proc_ctx)
        return 0;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tgid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    event->uid = proc_ctx->uid;
    event->gid = proc_ctx->gid;
    event->event_type = SEC_EVENT_FILE_OPEN;
    event->flags = mask;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Try to get filename from dentry
    struct dentry *dentry = BPF_CORE_READ(file, f_path.dentry);
    if (dentry) {
        bpf_core_read_str(event->filename, sizeof(event->filename), 
                         &dentry->d_name.name);
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// LSM hook for bprm check (program execution)
SEC("lsm/bprm_check_security")
int BPF_PROG(lsm_bprm_check, struct linux_binprm *bprm)
{
    if (!bprm)
        return 0;
    
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_BPRM_CHECK;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get executable path
    struct file *file = BPF_CORE_READ(bprm, file);
    if (file) {
        struct dentry *dentry = BPF_CORE_READ(file, f_path.dentry);
        if (dentry) {
            bpf_core_read_str(event->filename, sizeof(event->filename),
                            &dentry->d_name.name);
        }
    }
    
    // TODO: Check file hash against trusted binaries
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// LSM hook for capability checks
SEC("lsm/capable")
int BPF_PROG(lsm_capable, const struct cred *cred, struct user_namespace *ns,
            int cap, unsigned int opts)
{
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // Update capability statistics
    if (cap >= 0 && cap < 64) {
        __u64 *counter = bpf_map_lookup_elem(&capability_stats, &cap);
        if (counter)
            __sync_fetch_and_add(counter, 1);
    }
    
    // Only log sensitive capabilities
    if (cap != CAP_SYS_ADMIN && cap != CAP_SYS_MODULE && 
        cap != CAP_SYS_RAWIO && cap != CAP_SYS_PTRACE)
        return 0;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_CAPABILITY_USE;
    event->capability.requested_cap = cap;
    
    if (cred) {
        // Read capability sets from credentials
        struct kernel_cap_struct cap_effective, cap_permitted, cap_inheritable;
        
        if (bpf_core_field_exists(cred->cap_effective)) {
            BPF_CORE_READ_INTO(&cap_effective, cred, cap_effective);
            event->capability.effective_caps = cap_effective.cap[0];
        }
        
        if (bpf_core_field_exists(cred->cap_permitted)) {
            BPF_CORE_READ_INTO(&cap_permitted, cred, cap_permitted);
            event->capability.permitted_caps = cap_permitted.cap[0];
        }
        
        if (bpf_core_field_exists(cred->cap_inheritable)) {
            BPF_CORE_READ_INTO(&cap_inheritable, cred, cap_inheritable);
            event->capability.inheritable_caps = cap_inheritable.cap[0];
        }
    }
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// LSM hook for ptrace access
SEC("lsm/ptrace_access_check")
int BPF_PROG(lsm_ptrace_access, struct task_struct *child, unsigned int mode)
{
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u32 target_pid = BPF_CORE_READ(child, pid);
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_PTRACE;
    event->ptrace.target_pid = target_pid;
    event->ptrace.ptrace_mode = mode;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get target process name
    if (child) {
        bpf_core_read_str(event->filename, sizeof(event->filename), &child->comm);
    }
    
    bpf_ringbuf_submit(event, 0);
    
    // Could deny access by returning -EPERM
    return 0;
}

// LSM hook for socket creation
SEC("lsm/socket_create")
int BPF_PROG(lsm_socket_create, int family, int type, int protocol, int kern)
{
    // Only monitor non-kernel socket creation
    if (kern)
        return 0;
    
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    // Only log raw sockets and packet sockets (potential security risk)
    if (type != SOCK_RAW && type != SOCK_PACKET)
        return 0;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_SOCKET_CREATE;
    event->socket.socket_family = family;
    event->socket.socket_type = type;
    event->socket.socket_protocol = protocol;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// LSM hook for mmap with PROT_EXEC (code injection detection)
SEC("lsm/mmap_file")
int BPF_PROG(lsm_mmap_file, struct file *file, unsigned long reqprot,
            unsigned long prot, unsigned long flags)
{
    // Only interested in executable mappings
    if (!(prot & PROT_EXEC))
        return 0;
    
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_MMAP_FILE;
    event->mmap.prot = prot;
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    if (file) {
        struct dentry *dentry = BPF_CORE_READ(file, f_path.dentry);
        if (dentry) {
            bpf_core_read_str(event->filename, sizeof(event->filename),
                            &dentry->d_name.name);
        }
    } else {
        // Anonymous executable mapping (potential code injection)
        __builtin_memcpy(event->filename, "[anonymous]", 11);
        event->flags = 0x1; // Flag as suspicious
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// LSM hook for kernel module loading
SEC("lsm/kernel_module_request")
int BPF_PROG(lsm_kernel_module, char *kmod_name)
{
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct lsm_event *event = bpf_ringbuf_reserve(&lsm_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->event_type = SEC_EVENT_KERNEL_MODULE;
    event->flags = 0x2; // Flag as high-risk operation
    
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    if (kmod_name) {
        bpf_core_read_str(event->filename, sizeof(event->filename), kmod_name);
    }
    
    bpf_ringbuf_submit(event, 0);
    
    // Could deny module loading by returning -EPERM
    return 0;
}

char LICENSE[] SEC("license") = "GPL";