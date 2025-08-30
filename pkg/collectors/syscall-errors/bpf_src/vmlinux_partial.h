// SPDX-License-Identifier: GPL-2.0
/* Minimal vmlinux.h for syscall error monitoring
 * Contains only the structures we need
 */

#ifndef __VMLINUX_PARTIAL_H__
#define __VMLINUX_PARTIAL_H__

/* Basic kernel types */
typedef signed char __s8;
typedef unsigned char __u8;
typedef signed short __s16;
typedef unsigned short __u16;
typedef signed int __s32;
typedef unsigned int __u32;
typedef signed long long __s64;
typedef unsigned long long __u64;

typedef __s8 s8;
typedef __u8 u8;
typedef __s16 s16;
typedef __u16 u16;
typedef __s32 s32;
typedef __u32 u32;
typedef __s64 s64;
typedef __u64 u64;

typedef __u32 pid_t;
typedef __u32 uid_t;
typedef __u32 gid_t;

/* Network byte order types */
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u32 __wsum;

/* BPF map types */
enum bpf_map_type {
    BPF_MAP_TYPE_UNSPEC = 0,
    BPF_MAP_TYPE_HASH = 1,
    BPF_MAP_TYPE_ARRAY = 2,
    BPF_MAP_TYPE_PROG_ARRAY = 3,
    BPF_MAP_TYPE_PERF_EVENT_ARRAY = 4,
    BPF_MAP_TYPE_PERCPU_HASH = 5,
    BPF_MAP_TYPE_PERCPU_ARRAY = 6,
    BPF_MAP_TYPE_STACK_TRACE = 7,
    BPF_MAP_TYPE_CGROUP_ARRAY = 8,
    BPF_MAP_TYPE_LRU_HASH = 9,
    BPF_MAP_TYPE_LRU_PERCPU_HASH = 10,
    BPF_MAP_TYPE_LPM_TRIE = 11,
    BPF_MAP_TYPE_ARRAY_OF_MAPS = 12,
    BPF_MAP_TYPE_HASH_OF_MAPS = 13,
    BPF_MAP_TYPE_DEVMAP = 14,
    BPF_MAP_TYPE_SOCKMAP = 15,
    BPF_MAP_TYPE_CPUMAP = 16,
    BPF_MAP_TYPE_XSKMAP = 17,
    BPF_MAP_TYPE_SOCKHASH = 18,
    BPF_MAP_TYPE_CGROUP_STORAGE = 19,
    BPF_MAP_TYPE_REUSEPORT_SOCKARRAY = 20,
    BPF_MAP_TYPE_PERCPU_CGROUP_STORAGE = 21,
    BPF_MAP_TYPE_QUEUE = 22,
    BPF_MAP_TYPE_STACK = 23,
    BPF_MAP_TYPE_SK_STORAGE = 24,
    BPF_MAP_TYPE_DEVMAP_HASH = 25,
    BPF_MAP_TYPE_STRUCT_OPS = 26,
    BPF_MAP_TYPE_RINGBUF = 27,
    BPF_MAP_TYPE_INODE_STORAGE = 28,
    BPF_MAP_TYPE_TASK_STORAGE = 29,
};

/* BPF update flags */
enum {
    BPF_ANY = 0,     /* create new element or update existing */
    BPF_NOEXIST = 1, /* create new element if it didn't exist */
    BPF_EXIST = 2,   /* update existing element */
};

/* Trace entry structure */
struct trace_entry {
    unsigned short type;
    unsigned char flags;
    unsigned char preempt_count;
    int pid;
};

/* Tracepoint structures for raw_syscalls */
struct trace_event_raw_sys_enter {
    struct trace_entry ent;
    long id;                    /* syscall number */
    unsigned long args[6];      /* syscall arguments */
};

struct trace_event_raw_sys_exit {
    struct trace_entry ent;
    long id;                    /* syscall number */
    long ret;                   /* syscall return value */
};

/* Forward declaration for llist_node */
struct llist_node {
    struct llist_node *next;
};

/* Namespace structure */
struct ns_common {
    unsigned int inum;
    /* Other fields we don't need */
};

/* PID namespace */
struct pid_namespace {
    struct ns_common ns;
    /* Other fields we don't need */
};

/* Mount namespace */
struct mnt_namespace {
    struct ns_common ns;
    /* Other fields we don't need */
};

/* Network namespace */
struct net {
    struct ns_common ns;
    /* Other fields we don't need */
};

/* UTS namespace */
struct uts_namespace {
    struct ns_common ns;
    /* Other fields we don't need */
};

/* IPC namespace */
struct ipc_namespace {
    struct ns_common ns;
    /* Other fields we don't need */
};

/* Namespace proxy */
struct nsproxy {
    struct uts_namespace *uts_ns;
    struct ipc_namespace *ipc_ns;
    struct mnt_namespace *mnt_ns;
    struct pid_namespace *pid_ns_for_children;
    struct net *net_ns;
    /* Other fields we don't need */
};

/* Socket address structures */
struct sockaddr {
    __u16 sa_family;
    char sa_data[14];
};

struct in_addr {
    __u32 s_addr;
};

struct sockaddr_in {
    __u16 sin_family;
    __u16 sin_port;
    struct in_addr sin_addr;
    __u8 sin_zero[8];
};

/* Minimal task_struct definition - only fields we need */
struct task_struct {
    /* These fields are at stable offsets in most kernel versions */
    volatile long state;
    void *stack;
    unsigned int flags;
    unsigned int ptrace;
    struct llist_node wake_entry;
    unsigned int cpu;
    unsigned int wakee_flips;
    unsigned long wakee_flip_decay_ts;
    struct task_struct *last_wakee;
    int wake_cpu;
    int on_rq;
    int prio;
    int static_prio;
    int normal_prio;
    unsigned int rt_priority;
    
    /* The fields we actually need */
    pid_t pid;
    pid_t tgid;
    struct task_struct *parent;
    struct nsproxy *nsproxy;  /* Namespace proxy */
    
    /* Add padding to ensure we don't read beyond struct bounds */
    char _padding[4096];
};

#endif /* __VMLINUX_PARTIAL_H__ */