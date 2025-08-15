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

/* Namespace structures */
struct ns_common {
    unsigned int inum;
} __attribute__((preserve_access_index));

struct net {
    struct ns_common ns;
} __attribute__((preserve_access_index));

struct nsproxy {
    struct uts_namespace *uts_ns;
    struct ipc_namespace *ipc_ns;
    struct mnt_namespace *mnt_ns;
    struct pid_namespace *pid_ns_for_children;
    struct net *net_ns;
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

/* kernfs node structure for cgroup inode extraction */
struct kernfs_node {
    __u32 id;
    __u64 ino;  /* inode number - unique identifier */
} __attribute__((preserve_access_index));

struct cgroup {
    int id;
    struct kernfs_node *kn;  /* kernfs node for this cgroup */
    /* We mainly care about the kernfs inode for correlation */
} __attribute__((preserve_access_index));

/* Network structures */

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

/* Tracepoint context structures */
struct trace_event_raw_sys_enter {
    __u64 args[6];
} __attribute__((preserve_access_index));

struct trace_event_raw_sys_exit {
    long ret;
} __attribute__((preserve_access_index));

/* PT_REGS for different architectures */
struct pt_regs {
#ifdef __x86_64__
    unsigned long r15;
    unsigned long r14;
    unsigned long r13;
    unsigned long r12;
    unsigned long bp;
    unsigned long bx;
    unsigned long r11;
    unsigned long r10;
    unsigned long r9;
    unsigned long r8;
    unsigned long ax;
    unsigned long cx;
    unsigned long dx;
    unsigned long si;
    unsigned long di;
    unsigned long orig_ax;
    unsigned long ip;
    unsigned long cs;
    unsigned long flags;
    unsigned long sp;
    unsigned long ss;
#elif defined(__aarch64__)
    __u64 regs[31];
    __u64 sp;
    __u64 pc;
    __u64 pstate;
#endif
} __attribute__((preserve_access_index));

#endif /* __VMLINUX_MINIMAL_H__ */