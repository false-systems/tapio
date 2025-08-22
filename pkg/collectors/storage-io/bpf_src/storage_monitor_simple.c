// SPDX-License-Identifier: GPL-2.0
/* Simplified Storage I/O Monitor eBPF Program
 * Monitors VFS layer operations for Kubernetes storage performance analysis
 */

/* Basic type definitions for eBPF */
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

/* Boolean type */
typedef _Bool bool;
#define true 1
#define false 0

/* Network byte order types (needed by bpf_helpers.h) */
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u32 __wsum;

/* Size types */
typedef __u64 size_t;
typedef __s64 loff_t;
typedef __u32 dev_t;

/* BPF map types */
#define BPF_MAP_TYPE_HASH 1
#define BPF_MAP_TYPE_ARRAY 2
#define BPF_MAP_TYPE_RINGBUF 27

/* BPF update flags */
#define BPF_ANY 0
#define BPF_NOEXIST 1
#define BPF_EXIST 2

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

char LICENSE[] SEC("license") = "GPL";

/* Configuration constants */
#define MAX_PATH_LEN 256
#define MAX_COMM_LEN 16
#define MAX_ACTIVE_EVENTS 10240

/* Event types */
#define VFS_PROBE_READ 1
#define VFS_PROBE_WRITE 2
#define VFS_PROBE_FSYNC 3

/* Storage I/O event structure */
struct storage_io_event {
    /* Event header */
    __u8 event_type;
    __u32 pid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    
    /* Timing information */
    __u64 start_time_ns;
    __u64 end_time_ns;
    
    /* File operation details */
    __u64 inode;
    __s64 size;
    __s64 offset;
    __u32 flags;
    __u32 mode;
    
    /* Error information */
    __s32 error_code;
    
    /* Path and command */
    char path[MAX_PATH_LEN];
    char comm[MAX_COMM_LEN];
    
    /* Device information */
    __u32 dev_major;
    __u32 dev_minor;
} __attribute__((packed));

/* Active event tracking */
struct active_event {
    __u64 start_time;
    __u64 inode;
    __s64 size;
    __s64 offset;
    __u32 flags;
    __u32 mode;
    char path[MAX_PATH_LEN];
};

/* Maps */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024);
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ACTIVE_EVENTS);
    __type(key, __u64);
    __type(value, struct active_event);
} active_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 32);
    __type(key, __u32);
    __type(value, __u64);
} stats SEC(".maps");

/* Helper functions */
static __always_inline __u64 get_current_pid_tgid_key(void)
{
    return bpf_get_current_pid_tgid();
}

static __always_inline __u32 get_current_pid(void)
{
    return bpf_get_current_pid_tgid() >> 32;
}

static __always_inline void get_current_comm(char *comm, size_t size)
{
    bpf_get_current_comm(comm, size);
}

static __always_inline __u64 get_current_cgroup_id(void)
{
    return bpf_get_current_cgroup_id();
}

/* Simple VFS read tracking */
SEC("tracepoint/syscalls/sys_enter_read")
int trace_read_enter(void *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    active.start_time = bpf_ktime_get_ns();
    active.size = 0;  // Will be populated from syscall args if needed
    active.offset = 0;
    
    /* Store active event */
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int trace_read_exit(void *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    /* Allocate event from ring buffer */
    struct storage_io_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_events, &pid_tgid);
        return 0;
    }
    
    /* Fill event data */
    event->event_type = VFS_PROBE_READ;
    event->pid = get_current_pid();
    event->ppid = 0;  // Would need task_struct access
    
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    event->cgroup_id = get_current_cgroup_id();
    event->start_time_ns = active->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    
    event->inode = active->inode;
    event->size = active->size;
    event->offset = active->offset;
    event->flags = active->flags;
    event->mode = active->mode;
    event->error_code = 0;
    
    get_current_comm(event->comm, sizeof(event->comm));
    
    /* Copy path */
    for (int i = 0; i < MAX_PATH_LEN && i < sizeof(active->path); i++) {
        event->path[i] = active->path[i];
        if (active->path[i] == '\0')
            break;
    }
    
    event->dev_major = 0;
    event->dev_minor = 0;
    
    /* Submit event */
    bpf_ringbuf_submit(event, 0);
    
    /* Clean up */
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    /* Update stats */
    __u32 stat_key = VFS_PROBE_READ;
    __u64 *stat_val = bpf_map_lookup_elem(&stats, &stat_key);
    if (stat_val)
        __sync_fetch_and_add(stat_val, 1);
    
    return 0;
}

/* Simple VFS write tracking */
SEC("tracepoint/syscalls/sys_enter_write")
int trace_write_enter(void *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    active.start_time = bpf_ktime_get_ns();
    active.size = 0;
    active.offset = 0;
    
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int trace_write_exit(void *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    struct storage_io_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_events, &pid_tgid);
        return 0;
    }
    
    event->event_type = VFS_PROBE_WRITE;
    event->pid = get_current_pid();
    event->ppid = 0;
    
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;
    
    event->cgroup_id = get_current_cgroup_id();
    event->start_time_ns = active->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    
    event->inode = active->inode;
    event->size = active->size;
    event->offset = active->offset;
    event->flags = active->flags;
    event->mode = active->mode;
    event->error_code = 0;
    
    get_current_comm(event->comm, sizeof(event->comm));
    
    for (int i = 0; i < MAX_PATH_LEN && i < sizeof(active->path); i++) {
        event->path[i] = active->path[i];
        if (active->path[i] == '\0')
            break;
    }
    
    event->dev_major = 0;
    event->dev_minor = 0;
    
    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    __u32 stat_key = VFS_PROBE_WRITE;
    __u64 *stat_val = bpf_map_lookup_elem(&stats, &stat_key);
    if (stat_val)
        __sync_fetch_and_add(stat_val, 1);
    
    return 0;
}