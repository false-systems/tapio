/* SPDX-License-Identifier: GPL-2.0 */
/* Minimal vmlinux.h for all Tapio eBPF programs */
/* Self-contained - no external includes needed */

#ifndef __VMLINUX_MINIMAL_H__
#define __VMLINUX_MINIMAL_H__

/* Basic types - self-contained, no external includes */
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef short __s16;
typedef int __s32;
typedef long long __s64;

typedef __u16 __le16;
typedef __u16 __be16;
typedef __u32 __le32;
typedef __u32 __be32;
typedef __u64 __le64;
typedef __u64 __be64;

typedef __u16 __wsum;
typedef __u32 __wsum32;

typedef __u32 uid_t;
typedef __u32 gid_t;
typedef __u32 pid_t;
typedef __u32 dev_t;
typedef __u64 loff_t;
typedef __s64 ktime_t;
typedef unsigned int fmode_t;
typedef unsigned short umode_t;
typedef unsigned long size_t;
typedef long ssize_t;
typedef __u32 gfp_t;

/* Short type aliases commonly used in eBPF programs */
typedef __u8 u8;
typedef __u16 u16;
typedef __u32 u32;
typedef __u64 u64;
typedef __s8 s8;
typedef __s16 s16;
typedef __s32 s32;
typedef __s64 s64;

/* BPF map types */
#ifndef BPF_MAP_TYPE_HASH
#define BPF_MAP_TYPE_HASH 1
#endif
#ifndef BPF_MAP_TYPE_ARRAY
#define BPF_MAP_TYPE_ARRAY 2
#endif
#ifndef BPF_MAP_TYPE_PERCPU_ARRAY
#define BPF_MAP_TYPE_PERCPU_ARRAY 6
#endif
#ifndef BPF_MAP_TYPE_LRU_HASH
#define BPF_MAP_TYPE_LRU_HASH 9
#endif
#ifndef BPF_MAP_TYPE_RINGBUF
#define BPF_MAP_TYPE_RINGBUF 27
#endif
#ifndef BPF_MAP_TYPE_BLOOM_FILTER
#define BPF_MAP_TYPE_BLOOM_FILTER 30
#endif

/* BPF map update flags */
#ifndef BPF_ANY
#define BPF_ANY 0
#endif
#ifndef BPF_NOEXIST
#define BPF_NOEXIST 1
#endif
#ifndef BPF_EXIST
#define BPF_EXIST 2
#endif

/* Common types */
#ifndef bool
typedef _Bool bool;
#define true 1
#define false 0
#endif

/* Forward declarations */
struct file;
struct inode;
struct dentry;
struct mm_struct;
struct cred;
struct vfsmount;
struct file_operations;
struct net_device;
struct siginfo;
struct k_sigaction;

/* Namespace structures */
struct ns_common {
    unsigned int inum;
} __attribute__((preserve_access_index));

struct net {
    struct ns_common ns;
} __attribute__((preserve_access_index));

/* PID namespace structure */
struct pid_namespace {
    struct ns_common ns;
    void *idr;
    void *rcu;
    unsigned int pid_allocated;
    struct task_struct *child_reaper;
    void *pid_cachep;
    unsigned int level;
    struct pid_namespace *parent;
} __attribute__((preserve_access_index));

/* IPC namespace structure */
struct ipc_namespace {
    struct ns_common ns;
} __attribute__((preserve_access_index));

/* UTS namespace structure */
struct uts_namespace {
    struct ns_common ns;
} __attribute__((preserve_access_index));

/* Mount namespace structure */
struct mnt_namespace {
    struct ns_common ns;
} __attribute__((preserve_access_index));

/* Kernfs node structure for cgroup path extraction */
struct kernfs_node {
    void *count;
    void *active;
    struct kernfs_node *parent;
    const char *name;
    void *rb;
    const void *ns;
    unsigned int hash;
    void *priv;
    u64 id;
    unsigned short flags;
    umode_t mode;
    unsigned int ino;
    void *iattr;
} __attribute__((preserve_access_index));

struct nsproxy {
    struct uts_namespace *uts_ns;
    struct ipc_namespace *ipc_ns;
    struct mnt_namespace *mnt_ns;
    struct pid_namespace *pid_ns_for_children;
    struct net *net_ns;
} __attribute__((preserve_access_index));

/* Scheduling entity - for CFS throttling detection */
struct sched_entity {
    /* Load weight */
    struct load_weight {
        unsigned long weight;
        u32 inv_weight;
    } load;
    
    /* Runtime and vruntime tracking */
    u64 exec_start;
    u64 sum_exec_runtime;
    u64 vruntime;
    u64 prev_sum_exec_runtime;
    
    /* Statistics - location varies by kernel config */
    struct sched_statistics {
        u64 wait_start;
        u64 wait_max;
        u64 wait_count;
        u64 wait_sum;
        u64 sleep_start;
        u64 sleep_max;
        u64 block_start;
        u64 block_max;
        u64 exec_max;
        u64 slice_max;
        u64 nr_migrations_cold;
        u64 nr_failed_migrations_affine;
        u64 nr_failed_migrations_running;
        u64 nr_failed_migrations_hot;
        u64 nr_forced_migrations;
        u64 nr_wakeups;
        u64 nr_wakeups_sync;
        u64 nr_wakeups_migrate;
        u64 nr_wakeups_local;
        u64 nr_wakeups_remote;
        u64 nr_wakeups_affine;
        u64 nr_wakeups_affine_attempts;
        u64 nr_wakeups_passive;
        u64 nr_wakeups_idle;
    } statistics;
    
    /* CFS throttling fields - only in CONFIG_CFS_BANDWIDTH kernels */
    int throttled;
    int throttle_count;
    u64 throttled_clock;
    u64 throttled_clock_task;
} __attribute__((preserve_access_index));

/* Task structure - only fields we actually use */
struct task_struct {
    /* Task credentials */
    volatile long state;
    void *stack;
    unsigned int flags;
    
    /* Process IDs */
    int pid;
    int tgid;
    
    /* Parent process */
    struct task_struct *real_parent;
    struct task_struct *parent;
    
    /* Memory management */
    struct mm_struct *mm;
    
    /* Process name */
    char comm[16];
    
    /* Scheduling fields */
    int prio;
    int static_prio;
    int normal_prio;
    unsigned int policy;
    
    /* CFS scheduler entity - CO-RE will find the actual offset */
    struct sched_entity se;
    
    /* Namespaces and cgroups */
    struct nsproxy *nsproxy;
    struct css_set *cgroups;
    
    /* Credentials */
    const struct cred *cred;
} __attribute__((preserve_access_index));

/* Credentials structure */
struct cred {
    uid_t uid;
    gid_t gid;
    uid_t euid;
    gid_t egid;
} __attribute__((preserve_access_index));

/* Network address structures - needed for socket definitions */

/* IPv4 address structure */
struct in_addr {
    __u32 s_addr;
} __attribute__((preserve_access_index));

/* IPv6 address structure */
struct in6_addr {
    union {
        __u8 u6_addr8[16];
        __u32 u6_addr32[4];
    } in6_u;
} __attribute__((preserve_access_index));

/* Socket structures */
struct sock_common {
    union {
        struct {
            __be32 skc_daddr;
            __be32 skc_rcv_saddr;
        };
    };
    union {
        struct {
            __be16 skc_dport;
            __u16 skc_num;
        };
    };
    unsigned short skc_family;
    volatile unsigned char skc_state;
    unsigned char skc_reuse:4;
    unsigned char skc_reuseport:1;
} __attribute__((preserve_access_index));

/* Socket buffer list */
struct sk_buff_head {
    struct sk_buff *next;
    struct sk_buff *prev;
    __u32 qlen;
} __attribute__((preserve_access_index));

struct sock {
    struct sock_common __sk_common;
    __u16 sk_protocol;
    __u16 sk_type;
    /* Socket queues - using pointers instead of embedded structs */
    void *sk_receive_queue;
    void *sk_write_queue;
    
    /* IPv6-specific fields for CO-RE compatibility
     * These fields may not exist on all kernel versions or configurations.
     * Always use bpf_core_field_exists() before accessing them.
     * The actual offset and presence depends on kernel CONFIG_IPV6.
     */
    struct in6_addr sk_v6_rcv_saddr;  /* IPv6 source address */
    struct in6_addr sk_v6_daddr;      /* IPv6 destination address */
} __attribute__((preserve_access_index));

/* Path structures */
struct qstr {
    const unsigned char *name;
    unsigned int len;
} __attribute__((preserve_access_index));

struct path {
    struct vfsmount *mnt;
    struct dentry *dentry;
} __attribute__((preserve_access_index));

/* File operations */
struct file {
    struct path f_path;
    struct inode *f_inode;
    const struct file_operations *f_op;
    unsigned int f_flags;
    fmode_t f_mode;
    loff_t f_pos;
} __attribute__((preserve_access_index));

struct inode {
    umode_t i_mode;
    unsigned short i_opflags;
    uid_t i_uid;
    gid_t i_gid;
    unsigned long i_ino;
    dev_t i_rdev;
    loff_t i_size;
} __attribute__((preserve_access_index));

struct dentry {
    struct qstr d_name;
    struct inode *d_inode;
    struct dentry *d_parent;
} __attribute__((preserve_access_index));

/* Memory management */
struct mm_struct {
    unsigned long total_vm;     /* Total pages mapped */
    unsigned long locked_vm;    /* Pages that have PG_mlocked set */
    unsigned long pinned_vm;    /* Refcount permanently increased */
    unsigned long data_vm;      /* VM_WRITE & ~VM_SHARED & ~VM_STACK */
    unsigned long exec_vm;      /* VM_EXEC & ~VM_WRITE & ~VM_STACK */
    unsigned long stack_vm;     /* VM_STACK */
} __attribute__((preserve_access_index));

/* Cgroup for container detection */
struct css_set {
    struct cgroup_subsys_state *subsys[16];  /* Simplified */
} __attribute__((preserve_access_index));

struct cgroup_subsys_state {
    struct cgroup *cgroup;
    unsigned long flags;
} __attribute__((preserve_access_index));

/* Note: kernfs_node is already defined above with name field */

struct cgroup {
    int id;
    struct kernfs_node *kn;  /* kernfs node for this cgroup */
    /* We mainly care about the kernfs inode for correlation */
} __attribute__((preserve_access_index));

/* Network structures */

/* Socket address structures */
struct sockaddr {
    __u16 sa_family;
    char sa_data[14];
} __attribute__((preserve_access_index));

struct sockaddr_in {
    __u16 sin_family;
    __u16 sin_port;
    struct in_addr sin_addr;
    char sin_zero[8];
} __attribute__((preserve_access_index));

struct sockaddr_in6 {
    __u16 sin6_family;
    __u16 sin6_port;
    __u32 sin6_flowinfo;
    struct in6_addr sin6_addr;
    __u32 sin6_scope_id;
} __attribute__((preserve_access_index));

/* Network sk_buff for packet processing */
struct sk_buff {
    struct sk_buff *next;
    struct sk_buff *prev;
    struct sock *sk;
    ktime_t tstamp;
    struct net_device *dev;
    /* Network headers */
    unsigned char *head;
    unsigned char *data;
    unsigned int len;
    unsigned int data_len;
} __attribute__((preserve_access_index));

/* Message header structure for socket operations */
struct iovec {
    void *iov_base;
    size_t iov_len;
} __attribute__((preserve_access_index));

struct iov_iter {
    __u8 iter_type;
    bool data_source;
    size_t count;
    union {
        const struct iovec *iov;
        const void *kvec;
    };
    unsigned long nr_segs;
} __attribute__((preserve_access_index));

struct msghdr {
    void *msg_name;
    int msg_namelen;
    struct iov_iter msg_iter;
    void *msg_control;
    __u32 msg_controllen;
    unsigned int msg_flags;
} __attribute__((preserve_access_index));

/* Slab flags type */
typedef __u32 slab_flags_t;

/* Memory management structures */
struct kmem_cache {
    unsigned int object_size;
    unsigned int size;
    const char *name;
    unsigned int align;
    slab_flags_t flags;
} __attribute__((preserve_access_index));

/* Scheduler policy constants */
#ifndef SCHED_NORMAL
#define SCHED_NORMAL 0
#endif
#ifndef SCHED_FIFO
#define SCHED_FIFO 1
#endif
#ifndef SCHED_RR
#define SCHED_RR 2
#endif
#ifndef SCHED_BATCH
#define SCHED_BATCH 3
#endif
#ifndef SCHED_IDLE
#define SCHED_IDLE 5
#endif
#ifndef SCHED_DEADLINE
#define SCHED_DEADLINE 6
#endif

/* Event priority flags */
#ifndef EVENT_PRIORITY_LOW
#define EVENT_PRIORITY_LOW (1 << 29)
#endif

/* Tracepoint context structures */
struct trace_event_raw_sys_enter {
    __u64 args[6];
} __attribute__((preserve_access_index));

struct trace_event_raw_sys_exit {
    long ret;
} __attribute__((preserve_access_index));

/* Systemd-specific trace events */
struct trace_event_raw_cgroup_mkdir {
    __u64 common_field;
    __u32 root;
    __u32 ssid;
    char path[256];
} __attribute__((preserve_access_index));

struct trace_event_raw_cgroup_rmdir {
    __u64 common_field;
    __u32 root;
    __u32 ssid;
    char path[256];
} __attribute__((preserve_access_index));

struct trace_event_raw_sched_process_template {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 prio;
} __attribute__((preserve_access_index));

/* Scheduler process exec/exit tracepoint structures */
struct trace_event_raw_sched_process_exec {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 old_pid;
} __attribute__((preserve_access_index));

struct trace_event_raw_sched_process_exit {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 prio;
} __attribute__((preserve_access_index));

/* Scheduler statistics tracepoint structures */
struct trace_event_raw_sched_stat_wait {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 tgid;
    __u64 delay;
} __attribute__((preserve_access_index));

struct trace_event_raw_sched_stat_runtime {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 tgid;
    __u64 runtime;
    __u64 vruntime;
} __attribute__((preserve_access_index));

struct trace_event_raw_sched_migrate_task {
    __u64 common_field;
    char comm[16];
    __u32 pid;
    __u32 tgid;
    __u32 prio;
    __u32 orig_cpu;
    __u32 dest_cpu;
} __attribute__((preserve_access_index));

struct trace_event_raw_sched_switch {
    __u64 common_field;
    char prev_comm[16];
    __u32 prev_pid;
    __u32 prev_prio;
    long prev_state;
    char next_comm[16];
    __u32 next_pid;
    __u32 next_prio;
} __attribute__((preserve_access_index));

struct trace_event_raw_signal_generate {
    __u64 common_field;
    __u32 pid;
    __u32 sig;
    char comm[16];
    struct task_struct *task;
} __attribute__((preserve_access_index));

struct trace_event_raw_signal_deliver {
    __u64 common_field;
    __u32 sig;
    struct siginfo *info;
    struct k_sigaction *ka;
} __attribute__((preserve_access_index));

/* OOM-related trace events */
struct trace_event_raw_oom_kill_process {
    __u64 common_field;
    struct task_struct *task;
    __s32 oom_score_adj;
    __u32 gfp_mask;
    __u32 order;
} __attribute__((preserve_access_index));

struct trace_event_raw_mm_page_alloc_extfrag {
    __u64 common_field;
    __u32 alloc_order;
    __u32 fallback_order;
    __u32 alloc_migratetype;
    __u32 fallback_migratetype;
    __u32 change_ownership;
} __attribute__((preserve_access_index));

/* Raw tracepoint args structure */
struct bpf_raw_tracepoint_args {
    __u64 args[0];
} __attribute__((preserve_access_index));

/* PT_REGS structure for kprobe/uprobe parameter access - matches kernel layout */
struct pt_regs {
#ifdef __TARGET_ARCH_x86
    unsigned long r15;
    unsigned long r14;
    unsigned long r13;
    unsigned long r12;
    unsigned long rbp;
    unsigned long rbx;
    unsigned long r11;
    unsigned long r10;
    unsigned long r9;
    unsigned long r8;
    unsigned long rax;
    unsigned long rcx;
    unsigned long rdx;
    unsigned long rsi;
    unsigned long rdi;
    unsigned long orig_rax;
    unsigned long rip;
    unsigned long cs;
    unsigned long eflags;
    unsigned long rsp;
    unsigned long ss;
#elif defined(__TARGET_ARCH_arm64)
    __u64 regs[31];
    __u64 sp;
    __u64 pc;
    __u64 pstate;
#endif
} __attribute__((preserve_access_index));

/* ARM64 also needs user_pt_regs for PT_REGS macros */
#ifdef __TARGET_ARCH_arm64
struct user_pt_regs {
    __u64 regs[31];
    __u64 sp;
    __u64 pc;
    __u64 pstate;
} __attribute__((preserve_access_index));
#endif

#endif /* __VMLINUX_MINIMAL_H__ */