// SPDX-License-Identifier: GPL-2.0
/* Syscall Error Monitor - Production-grade syscall error capture
 * Captures critical syscall errors: ENOSPC, ENOMEM, ECONNREFUSED
 * Using tracepoints for stable kernel interface
 */

#include "vmlinux_partial.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

/* Configuration constants */
#define MAX_PATH_LEN 256
#define MAX_COMM_LEN 16
#define MAX_ACTIVE_SYSCALLS 10240
#define MAX_ERROR_AGGREGATION 1024

/* Critical error codes we monitor */
#define ENOSPC 28     /* No space left on device */
#define ENOMEM 12     /* Out of memory */
#define ECONNREFUSED 111  /* Connection refused */
#define EACCES 13     /* Permission denied */
#define EPERM 1       /* Operation not permitted */
#define EIO 5         /* I/O error */
#define ENOENT 2      /* No such file or directory */
#define EAGAIN 11     /* Try again */
#define ETIMEDOUT 110 /* Connection timed out */
#define EMFILE 24     /* Too many open files */
#define EDQUOT 122    /* Disk quota exceeded */

/* Error severity levels for adaptive rate limiting */
#define SEVERITY_CRITICAL 1  /* ENOMEM, ENOSPC, EDQUOT */
#define SEVERITY_HIGH 2      /* EIO, EMFILE, ECONNREFUSED */
#define SEVERITY_MEDIUM 3    /* EACCES, EPERM, ETIMEDOUT */
#define SEVERITY_LOW 4       /* ENOENT, EAGAIN */

/* Syscall categories */
#define SYSCALL_CAT_FILE 1
#define SYSCALL_CAT_NETWORK 2
#define SYSCALL_CAT_MEMORY 3
#define SYSCALL_CAT_PROCESS 4
#define SYSCALL_CAT_OTHER 5

/* Syscall error event structure */
struct syscall_error_event {
    /* Event header */
    __u64 timestamp_ns;
    __u32 pid;
    __u32 ppid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    
    /* Syscall information */
    __s32 syscall_nr;
    __s32 error_code;
    __u8 category;
    __u8 _pad[3];  /* Padding for alignment */
    
    /* Context information */
    char comm[MAX_COMM_LEN];
    char path[MAX_PATH_LEN];  /* For file-related syscalls */
    
    /* Network context (for network syscalls) */
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    
    /* Additional context */
    __u64 arg1;  /* First syscall argument */
    __u64 arg2;  /* Second syscall argument */
    __u64 arg3;  /* Third syscall argument */
    
    /* Error frequency tracking */
    __u32 error_count;  /* How many times this error occurred recently */
    __u32 _pad2;        /* Padding for alignment */
} __attribute__((packed));

/* Error aggregation key */
struct error_key {
    __u32 pid;
    __s32 syscall_nr;
    __s32 error_code;
};

/* Error aggregation value */
struct error_stats {
    __u64 count;
    __u64 last_timestamp;
    __u64 first_timestamp;
    __u64 burst_count;      /* Count within current burst window */
    __u64 burst_start;      /* Start of current burst window */
    __u8 severity;          /* Error severity level */
    __u8 _pad[7];          /* Padding for alignment */
};

/* Active syscall tracking */
struct active_syscall {
    __u64 timestamp;
    __s32 syscall_nr;
    __u32 _pad;
    __u64 arg1;
    __u64 arg2;
    __u64 arg3;
    char path[MAX_PATH_LEN];
    /* Network context for socket operations */
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    /* Container/namespace info */
    __u32 ns_inum;          /* Namespace inode number */
    __u32 mnt_ns_inum;      /* Mount namespace inode */
    char cgroup_path[64];   /* Shortened cgroup path for container detection */
};

/* Statistics structure */
struct collector_stats {
    __u64 total_errors;
    __u64 enospc_count;
    __u64 enomem_count;
    __u64 econnrefused_count;
    __u64 eio_count;
    __u64 emfile_count;
    __u64 edquot_count;
    __u64 events_sent;
    __u64 events_dropped;
};

/* BPF Maps */

/* Ring buffer for sending events to userspace */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 * 1024 * 1024);  /* 8MB ring buffer */
} events SEC(".maps");

/* Track active syscalls */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ACTIVE_SYSCALLS);
    __type(key, __u64);  /* PID-TID combo */
    __type(value, struct active_syscall);
} active_syscalls SEC(".maps");

/* Error aggregation to prevent event storms */
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_ERROR_AGGREGATION);
    __type(key, struct error_key);
    __type(value, struct error_stats);
} error_aggregation SEC(".maps");

/* Per-CPU statistics */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct collector_stats);
} stats SEC(".maps");

/* Helper function to determine if we should track this error */
static __always_inline int should_track_error(__s64 error_code)
{
    /* Track critical errors that indicate system issues */
    switch (-error_code) {
    case ENOSPC:
    case ENOMEM:
    case ECONNREFUSED:
    case EIO:
    case EACCES:
    case EPERM:
    case ETIMEDOUT:
    case EMFILE:
    case EDQUOT:
        return 1;
    default:
        return 0;
    }
}

/* Get error severity for adaptive rate limiting */
static __always_inline __u8 get_error_severity(__s64 error_code)
{
    switch (-error_code) {
    case ENOMEM:
    case ENOSPC:
    case EDQUOT:
        return SEVERITY_CRITICAL;
    case EIO:
    case EMFILE:
    case ECONNREFUSED:
        return SEVERITY_HIGH;
    case EACCES:
    case EPERM:
    case ETIMEDOUT:
        return SEVERITY_MEDIUM;
    case ENOENT:
    case EAGAIN:
        return SEVERITY_LOW;
    default:
        return SEVERITY_LOW;
    }
}

/* Get rate limit based on severity (in nanoseconds) */
static __always_inline __u64 get_rate_limit(__u8 severity)
{
    switch (severity) {
    case SEVERITY_CRITICAL:
        return 50000000;   /* 50ms for critical errors */
    case SEVERITY_HIGH:
        return 75000000;   /* 75ms for high severity */
    case SEVERITY_MEDIUM:
        return 100000000;  /* 100ms for medium severity */
    case SEVERITY_LOW:
        return 200000000;  /* 200ms for low severity */
    default:
        return 100000000;  /* Default 100ms */
    }
}

/* Helper function to categorize syscall */
static __always_inline __u8 categorize_syscall(long syscall_nr)
{
    /* File-related syscalls (x86_64 syscall numbers) */
    switch (syscall_nr) {
    case 0:   /* read */
    case 1:   /* write */
    case 2:   /* open */
    case 3:   /* close */
    case 4:   /* stat */
    case 5:   /* fstat */
    case 6:   /* lstat */
    case 8:   /* lseek */
    case 9:   /* mmap */
    case 16:  /* ioctl */
    case 17:  /* pread64 */
    case 18:  /* pwrite64 */
    case 19:  /* readv */
    case 20:  /* writev */
    case 21:  /* access */
    case 74:  /* fsync */
    case 75:  /* fdatasync */
    case 76:  /* truncate */
    case 77:  /* ftruncate */
    case 257: /* openat */
    case 258: /* mkdirat */
    case 259: /* mknodat */
    case 260: /* fchownat */
    case 262: /* newfstatat */
    case 263: /* unlinkat */
        return SYSCALL_CAT_FILE;
    
    /* Network syscalls */
    case 41:  /* socket */
    case 42:  /* connect */
    case 43:  /* accept */
    case 44:  /* sendto */
    case 45:  /* recvfrom */
    case 46:  /* sendmsg */
    case 47:  /* recvmsg */
    case 48:  /* shutdown */
    case 49:  /* bind */
    case 50:  /* listen */
    case 51:  /* getsockname */
    case 52:  /* getpeername */
    case 53:  /* socketpair */
    case 54:  /* setsockopt */
    case 288: /* accept4 */
        return SYSCALL_CAT_NETWORK;
    
    /* Memory syscalls */
    case 10:  /* mprotect */
    case 11:  /* munmap */
    case 12:  /* brk */
    case 25:  /* mremap */
    case 26:  /* msync */
    case 27:  /* mincore */
    case 28:  /* madvise */
        return SYSCALL_CAT_MEMORY;
    
    /* Process syscalls */
    case 56:  /* clone */
    case 57:  /* fork */
    case 58:  /* vfork */
    case 59:  /* execve */
    case 60:  /* exit */
    case 61:  /* wait4 */
    case 62:  /* kill */
    case 247: /* waitid */
        return SYSCALL_CAT_PROCESS;
    
    default:
        return SYSCALL_CAT_OTHER;
    }
}

/* Helper to get current task info */
static __always_inline void get_task_info(struct syscall_error_event *event)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u64 uid_gid = bpf_get_current_uid_gid();
    
    event->pid = pid_tgid >> 32;
    event->tid = pid_tgid & 0xFFFFFFFF;
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    /* Get parent PID - using BPF CO-RE for safety */
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    struct task_struct *parent = BPF_CORE_READ(task, parent);
    if (parent) {
        event->ppid = BPF_CORE_READ(parent, tgid);
    } else {
        event->ppid = 0;
    }
    
    /* Get cgroup ID */
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    /* Get command name */
    bpf_get_current_comm(event->comm, sizeof(event->comm));
}

/* Syscall enter tracepoint - track syscall arguments */
SEC("tracepoint/raw_syscalls/sys_enter")
int trace_sys_enter(struct trace_event_raw_sys_enter *ctx)
{
    __u64 pid_tid = bpf_get_current_pid_tgid();
    struct active_syscall active = {};
    
    active.timestamp = bpf_ktime_get_ns();
    active.syscall_nr = ctx->id;
    
    /* Save first 3 arguments for context */
    active.arg1 = ctx->args[0];
    active.arg2 = ctx->args[1];
    active.arg3 = ctx->args[2];
    
    /* For file syscalls, try to capture the path with bounds checking */
    __u8 category = categorize_syscall(ctx->id);
    if (category == SYSCALL_CAT_FILE && ctx->args[0]) {
        /* For openat and similar, the path is the second argument */
        if (ctx->id == 257 || ctx->id == 258 || ctx->id == 259 || 
            ctx->id == 260 || ctx->id == 262 || ctx->id == 263) {
            if (ctx->args[1]) {
                const char *pathname = (const char *)ctx->args[1];
                /* Bounds checking for user data */
                if (pathname) {
                    bpf_probe_read_user_str(active.path, sizeof(active.path), pathname);
                }
            }
        } else {
            /* For most file syscalls, path is the first argument */
            const char *pathname = (const char *)ctx->args[0];
            /* Bounds checking for user data */
            if (pathname) {
                bpf_probe_read_user_str(active.path, sizeof(active.path), pathname);
            }
        }
    }
    
    /* For network syscalls, try to capture socket info */
    if (category == SYSCALL_CAT_NETWORK) {
        /* For connect syscall, extract address info if available */
        if (ctx->id == 42 && ctx->args[1]) {  /* connect syscall */
            struct sockaddr *addr = (struct sockaddr *)ctx->args[1];
            __u16 family = 0;
            bpf_probe_read_user(&family, sizeof(family), &addr->sa_family);
            
            if (family == 2) {  /* AF_INET */
                struct sockaddr_in *addr_in = (struct sockaddr_in *)addr;
                bpf_probe_read_user(&active.dst_ip, sizeof(active.dst_ip), &addr_in->sin_addr);
                bpf_probe_read_user(&active.dst_port, sizeof(active.dst_port), &addr_in->sin_port);
                active.dst_port = __builtin_bswap16(active.dst_port);
            }
        }
    }
    
    /* Capture container/namespace information */
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        /* Get namespace inode numbers for container detection */
        struct nsproxy *nsproxy = BPF_CORE_READ(task, nsproxy);
        if (nsproxy) {
            struct pid_namespace *pid_ns = BPF_CORE_READ(nsproxy, pid_ns_for_children);
            if (pid_ns) {
                active.ns_inum = BPF_CORE_READ(pid_ns, ns.inum);
            }
            struct mnt_namespace *mnt_ns = BPF_CORE_READ(nsproxy, mnt_ns);
            if (mnt_ns) {
                active.mnt_ns_inum = BPF_CORE_READ(mnt_ns, ns.inum);
            }
        }
    }
    
    bpf_map_update_elem(&active_syscalls, &pid_tid, &active, BPF_ANY);
    return 0;
}

/* Syscall exit tracepoint - capture errors */
SEC("tracepoint/raw_syscalls/sys_exit")
int trace_sys_exit(struct trace_event_raw_sys_exit *ctx)
{
    long ret = ctx->ret;
    
    /* Only track errors (negative return values) */
    if (ret >= 0)
        return 0;
    
    /* Check if this is an error we care about */
    if (!should_track_error(ret))
        return 0;
    
    __u64 pid_tid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tid >> 32;
    
    /* Look up the syscall entry info */
    struct active_syscall *active = bpf_map_lookup_elem(&active_syscalls, &pid_tid);
    if (!active)
        return 0;
    
    /* Check for error aggregation to prevent storms */
    struct error_key ekey = {
        .pid = pid,
        .syscall_nr = active->syscall_nr,
        .error_code = ret
    };
    
    struct error_stats *estats = bpf_map_lookup_elem(&error_aggregation, &ekey);
    __u64 now = bpf_ktime_get_ns();
    __u32 error_count = 1;
    __u8 severity = get_error_severity(ret);
    __u64 rate_limit = get_rate_limit(severity);
    
    if (estats) {
        /* Adaptive rate limiting based on severity */
        __u64 time_since_last = now - estats->last_timestamp;
        
        /* Burst detection for critical errors */
        if (severity == SEVERITY_CRITICAL) {
            /* Allow bursts for critical errors - track within 1 second windows */
            if (now - estats->burst_start > 1000000000) {  /* New burst window */
                estats->burst_start = now;
                estats->burst_count = 1;
            } else {
                estats->burst_count++;
                /* Allow up to 5 critical errors per second */
                if (estats->burst_count > 5 && time_since_last < rate_limit) {
                    estats->count++;
                    estats->last_timestamp = now;
                    
                    /* Update stats but don't send event */
                    __u32 key = 0;
                    struct collector_stats *st = bpf_map_lookup_elem(&stats, &key);
                    if (st) {
                        __sync_fetch_and_add(&st->events_dropped, 1);
                    }
                    
                    goto cleanup;
                }
            }
        } else {
            /* Regular rate limiting for non-critical errors */
            if (time_since_last < rate_limit) {
                estats->count++;
                estats->last_timestamp = now;
                
                /* Update stats but don't send event */
                __u32 key = 0;
                struct collector_stats *st = bpf_map_lookup_elem(&stats, &key);
                if (st) {
                    __sync_fetch_and_add(&st->events_dropped, 1);
                }
                
                goto cleanup;
            }
        }
        
        error_count = estats->count;
        estats->count = 1;  /* Reset count */
        estats->last_timestamp = now;
        estats->severity = severity;
    } else {
        /* New error, add to aggregation */
        struct error_stats new_stats = {
            .count = 1,
            .first_timestamp = now,
            .last_timestamp = now,
            .burst_start = now,
            .burst_count = 1,
            .severity = severity
        };
        bpf_map_update_elem(&error_aggregation, &ekey, &new_stats, BPF_ANY);
    }
    
    /* Allocate event */
    struct syscall_error_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        goto cleanup;
    
    /* Fill event data */
    event->timestamp_ns = now;
    event->syscall_nr = active->syscall_nr;
    event->error_code = ret;
    event->category = categorize_syscall(active->syscall_nr);
    event->error_count = error_count;
    
    /* Get task info */
    get_task_info(event);
    
    /* Copy context from active syscall */
    event->arg1 = active->arg1;
    event->arg2 = active->arg2;
    event->arg3 = active->arg3;
    __builtin_memcpy(event->path, active->path, MAX_PATH_LEN);
    
    /* Copy network context if available */
    event->src_ip = active->src_ip;
    event->dst_ip = active->dst_ip;
    event->src_port = active->src_port;
    event->dst_port = active->dst_port;
    
    /* Submit event */
    bpf_ringbuf_submit(event, 0);
    
    /* Update statistics */
    __u32 key = 0;
    struct collector_stats *st = bpf_map_lookup_elem(&stats, &key);
    if (st) {
        __sync_fetch_and_add(&st->total_errors, 1);
        __sync_fetch_and_add(&st->events_sent, 1);
        
        switch (-ret) {
        case ENOSPC:
            __sync_fetch_and_add(&st->enospc_count, 1);
            break;
        case ENOMEM:
            __sync_fetch_and_add(&st->enomem_count, 1);
            break;
        case ECONNREFUSED:
            __sync_fetch_and_add(&st->econnrefused_count, 1);
            break;
        case EIO:
            __sync_fetch_and_add(&st->eio_count, 1);
            break;
        case EMFILE:
            __sync_fetch_and_add(&st->emfile_count, 1);
            break;
        case EDQUOT:
            __sync_fetch_and_add(&st->edquot_count, 1);
            break;
        }
    }

cleanup:
    /* Clean up active syscall entry */
    bpf_map_delete_elem(&active_syscalls, &pid_tid);
    return 0;
}