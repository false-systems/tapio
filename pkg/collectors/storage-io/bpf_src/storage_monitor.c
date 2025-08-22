// SPDX-License-Identifier: GPL-2.0
/* Storage I/O Monitor eBPF Program
 * Monitors VFS layer operations for Kubernetes storage performance analysis
 * Focuses on critical I/O operations: read, write, fsync, iterate_dir
 */

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "helpers.h"
#include "bpf_stats.h"

char LICENSE[] SEC("license") = "GPL";

/* Configuration constants */
#define MAX_PATH_LEN 256
#define MAX_COMM_LEN 16
#define MAX_ACTIVE_EVENTS 10240
#define SLOW_IO_THRESHOLD_NS 10000000ULL  /* 10ms in nanoseconds */

/* Event types matching VFSProbeType in Go */
#define VFS_PROBE_READ 1
#define VFS_PROBE_WRITE 2
#define VFS_PROBE_FSYNC 3
#define VFS_PROBE_ITERATE_DIR 4
#define VFS_PROBE_OPEN 5
#define VFS_PROBE_CLOSE 6

/* Storage I/O event structure - must match StorageIOEventRaw in Go */
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
    
    /* Path and command (fixed size for eBPF) */
    char path[MAX_PATH_LEN];
    char comm[MAX_COMM_LEN];
    
    /* Device information */
    __u32 dev_major;
    __u32 dev_minor;
} __attribute__((packed));

/* Active event tracking for entry/exit correlation */
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

/* Ring buffer for sending events to userspace */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024); /* 1MB ring buffer */
} events SEC(".maps");

/* Hash map to track active I/O operations */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ACTIVE_EVENTS);
    __type(key, __u64);  /* pid_tgid */
    __type(value, struct active_event);
} active_events SEC(".maps");

/* Statistics map for performance monitoring */
DEFINE_BPF_STATS_MAP(stats, 32);

/* Configuration map */
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 8);
    __type(key, __u32);
    __type(value, __u64);
} config SEC(".maps");

/* Helper functions */

static __always_inline __u64 get_current_pid_tgid_key(void)
{
    return bpf_get_current_pid_tgid();
}

static __always_inline __u32 get_current_pid(void)
{
    return bpf_get_current_pid_tgid() >> 32;
}

static __always_inline __u32 get_current_tgid(void)
{
    return bpf_get_current_pid_tgid() & 0xFFFFFFFF;
}

static __always_inline void get_current_comm(char *comm, size_t size)
{
    bpf_get_current_comm(comm, size);
}

static __always_inline __u64 get_current_cgroup_id(void)
{
    return bpf_get_current_cgroup_id();
}

static __always_inline __u32 get_current_uid_gid(__u32 *uid, __u32 *gid)
{
    __u64 uid_gid = bpf_get_current_uid_gid();
    *uid = uid_gid & 0xFFFFFFFF;
    *gid = uid_gid >> 32;
    return 0;
}

/* Extract file path from struct file */
static __always_inline int get_file_path(struct file *file, char *path, size_t path_len)
{
    struct dentry *dentry;
    struct qstr d_name;
    
    if (!file)
        return -1;
        
    /* Read dentry from file struct */
    if (bpf_core_read(&dentry, sizeof(dentry), &file->f_path.dentry) != 0)
        return -1;
        
    if (!dentry)
        return -1;
        
    /* Read the name from dentry */
    if (bpf_core_read(&d_name, sizeof(d_name), &dentry->d_name) != 0)
        return -1;
        
    /* Copy the name */
    if (bpf_core_read_str(path, path_len, d_name.name) < 0)
        return -1;
        
    return 0;
}

/* Extract inode number from struct file */
static __always_inline __u64 get_file_inode(struct file *file)
{
    struct inode *inode;
    __u64 ino = 0;
    
    if (!file)
        return 0;
        
    if (bpf_core_read(&inode, sizeof(inode), &file->f_inode) != 0)
        return 0;
        
    if (!inode)
        return 0;
        
    bpf_core_read(&ino, sizeof(ino), &inode->i_ino);
    return ino;
}

/* Extract device information from struct file */
static __always_inline void get_file_device(struct file *file, __u32 *major, __u32 *minor)
{
    struct inode *inode;
    dev_t dev = 0;
    
    *major = 0;
    *minor = 0;
    
    if (!file)
        return;
        
    if (bpf_core_read(&inode, sizeof(inode), &file->f_inode) != 0)
        return;
        
    if (!inode)
        return;
        
    if (bpf_core_read(&dev, sizeof(dev), &inode->i_rdev) != 0) {
        /* Try i_sb->s_dev if i_rdev is not available */
        struct super_block *sb;
        if (bpf_core_read(&sb, sizeof(sb), &inode->i_sb) == 0 && sb) {
            bpf_core_read(&dev, sizeof(dev), &sb->s_dev);
        }
    }
    
    *major = MAJOR(dev);
    *minor = MINOR(dev);
}

/* Check if we should filter this process */
static __always_inline bool should_filter_process(void)
{
    __u32 pid = get_current_pid();
    
    /* Filter out kernel threads (PID < 2) */
    if (pid < 2)
        return true;
        
    /* Get process name for additional filtering */
    char comm[MAX_COMM_LEN];
    get_current_comm(comm, sizeof(comm));
    
    /* Filter common kernel threads */
    if (comm[0] == 'k' && comm[1] == 't') /* kernel threads often start with "kt" */
        return true;
        
    if (comm[0] == 'r' && comm[1] == 'c' && comm[2] == 'u') /* RCU threads */
        return true;
        
    return false;
}

/* Check if path should be monitored */
static __always_inline bool should_monitor_path(const char *path)
{
    /* Always monitor if path starts with Kubernetes paths */
    const char k8s_paths[][32] = {
        "/var/lib/kubelet/",
        "/var/lib/docker/",
        "/var/lib/containerd/",
        "/var/log/containers/",
        "/etc/kubernetes/",
        "/var/lib/etcd/"
    };
    
    for (int i = 0; i < 6; i++) {
        if (bpf_strncmp(path, k8s_paths[i], bpf_strlen(k8s_paths[i])) == 0)
            return true;
    }
    
    return false;
}

/* Submit storage I/O event to ring buffer */
static __always_inline int submit_storage_event(__u8 event_type, struct active_event *active, __s32 error_code)
{
    struct storage_io_event *event;
    __u32 uid, gid;
    
    /* Reserve space in ring buffer */
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return -1;
    
    /* Fill event structure */
    event->event_type = event_type;
    event->pid = get_current_pid();
    event->ppid = 0; /* TODO: Get PPID from task_struct */
    get_current_uid_gid(&uid, &gid);
    event->uid = uid;
    event->gid = gid;
    event->cgroup_id = get_current_cgroup_id();
    
    event->start_time_ns = active->start_time;
    event->end_time_ns = bpf_ktime_get_ns();
    
    event->inode = active->inode;
    event->size = active->size;
    event->offset = active->offset;
    event->flags = active->flags;
    event->mode = active->mode;
    event->error_code = error_code;
    
    /* Copy path and command */
    bpf_probe_read_str(event->path, sizeof(event->path), active->path);
    get_current_comm(event->comm, sizeof(event->comm));
    
    /* Device information is set to 0 for now - TODO: extract from file */
    event->dev_major = 0;
    event->dev_minor = 0;
    
    /* Submit event */
    bpf_ringbuf_submit(event, 0);
    
    return 0;
}

/* VFS Read Probes */

SEC("kprobe/vfs_read")
int trace_vfs_read_entry(struct pt_regs *ctx)
{
    if (should_filter_process())
        return 0;
    
    BPF_STATS_ENTER(&stats, VFS_PROBE_READ);
    
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    /* Get function arguments */
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    char __user *buf = (char __user *)PT_REGS_PARM2(ctx);
    size_t count = (size_t)PT_REGS_PARM3(ctx);
    loff_t *pos = (loff_t *)PT_REGS_PARM4(ctx);
    
    if (!file)
        return 0;
    
    /* Extract file information */
    active.inode = get_file_inode(file);
    active.size = count;
    active.start_time = bpf_ktime_get_ns();
    
    if (pos) {
        bpf_probe_read(&active.offset, sizeof(active.offset), pos);
    }
    
    /* Get file path */
    if (get_file_path(file, active.path, sizeof(active.path)) != 0) {
        /* Fallback to empty path if extraction fails */
        active.path[0] = '\0';
    }
    
    /* Only monitor relevant paths */
    if (active.path[0] != '\0' && !should_monitor_path(active.path))
        return 0;
    
    /* Store active event */
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/vfs_read")
int trace_vfs_read_exit(struct pt_regs *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    __s32 ret = (__s32)PT_REGS_RC(ctx);
    
    /* Look up active event */
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    /* Update size with actual bytes read */
    if (ret > 0)
        active->size = ret;
    
    /* Submit event */
    submit_storage_event(VFS_PROBE_READ, active, ret < 0 ? -ret : 0);
    
    /* Clean up active event */
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    return 0;
}

/* VFS Write Probes */

SEC("kprobe/vfs_write")
int trace_vfs_write_entry(struct pt_regs *ctx)
{
    if (should_filter_process())
        return 0;
    
    BPF_STATS_ENTER(&stats, VFS_PROBE_WRITE);
    
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    /* Get function arguments */
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    const char __user *buf = (const char __user *)PT_REGS_PARM2(ctx);
    size_t count = (size_t)PT_REGS_PARM3(ctx);
    loff_t *pos = (loff_t *)PT_REGS_PARM4(ctx);
    
    if (!file)
        return 0;
    
    /* Extract file information */
    active.inode = get_file_inode(file);
    active.size = count;
    active.start_time = bpf_ktime_get_ns();
    
    if (pos) {
        bpf_probe_read(&active.offset, sizeof(active.offset), pos);
    }
    
    /* Get file path */
    if (get_file_path(file, active.path, sizeof(active.path)) != 0) {
        active.path[0] = '\0';
    }
    
    /* Only monitor relevant paths */
    if (active.path[0] != '\0' && !should_monitor_path(active.path))
        return 0;
    
    /* Store active event */
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/vfs_write")
int trace_vfs_write_exit(struct pt_regs *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    __s32 ret = (__s32)PT_REGS_RC(ctx);
    
    /* Look up active event */
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    /* Update size with actual bytes written */
    if (ret > 0)
        active->size = ret;
    
    /* Submit event */
    submit_storage_event(VFS_PROBE_WRITE, active, ret < 0 ? -ret : 0);
    
    /* Clean up active event */
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    return 0;
}

/* VFS Fsync Probes */

SEC("kprobe/vfs_fsync")
int trace_vfs_fsync_entry(struct pt_regs *ctx)
{
    if (should_filter_process())
        return 0;
    
    BPF_STATS_ENTER(&stats, VFS_PROBE_FSYNC);
    
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    /* Get function arguments */
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    int datasync = (int)PT_REGS_PARM2(ctx);
    
    if (!file)
        return 0;
    
    /* Extract file information */
    active.inode = get_file_inode(file);
    active.size = 0; /* fsync doesn't have a size */
    active.start_time = bpf_ktime_get_ns();
    active.flags = datasync;
    
    /* Get file path */
    if (get_file_path(file, active.path, sizeof(active.path)) != 0) {
        active.path[0] = '\0';
    }
    
    /* Only monitor relevant paths */
    if (active.path[0] != '\0' && !should_monitor_path(active.path))
        return 0;
    
    /* Store active event */
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/vfs_fsync")
int trace_vfs_fsync_exit(struct pt_regs *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    __s32 ret = (__s32)PT_REGS_RC(ctx);
    
    /* Look up active event */
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    /* Submit event */
    submit_storage_event(VFS_PROBE_FSYNC, active, ret < 0 ? -ret : 0);
    
    /* Clean up active event */
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    return 0;
}

/* VFS Iterate Dir Probes */

SEC("kprobe/iterate_dir")
int trace_iterate_dir_entry(struct pt_regs *ctx)
{
    if (should_filter_process())
        return 0;
    
    BPF_STATS_ENTER(&stats, VFS_PROBE_ITERATE_DIR);
    
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event active = {};
    
    /* Get function arguments */
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    struct dir_context *ctx_arg = (struct dir_context *)PT_REGS_PARM2(ctx);
    
    if (!file)
        return 0;
    
    /* Extract file information */
    active.inode = get_file_inode(file);
    active.size = 0; /* iterate_dir doesn't have a size */
    active.start_time = bpf_ktime_get_ns();
    
    /* Get file path */
    if (get_file_path(file, active.path, sizeof(active.path)) != 0) {
        active.path[0] = '\0';
    }
    
    /* Only monitor relevant paths */
    if (active.path[0] != '\0' && !should_monitor_path(active.path))
        return 0;
    
    /* Store active event */
    bpf_map_update_elem(&active_events, &pid_tgid, &active, BPF_ANY);
    
    return 0;
}

SEC("kretprobe/iterate_dir")
int trace_iterate_dir_exit(struct pt_regs *ctx)
{
    __u64 pid_tgid = get_current_pid_tgid_key();
    struct active_event *active;
    __s32 ret = (__s32)PT_REGS_RC(ctx);
    
    /* Look up active event */
    active = bpf_map_lookup_elem(&active_events, &pid_tgid);
    if (!active)
        return 0;
    
    /* Submit event */
    submit_storage_event(VFS_PROBE_ITERATE_DIR, active, ret < 0 ? -ret : 0);
    
    /* Clean up active event */
    bpf_map_delete_elem(&active_events, &pid_tgid);
    
    return 0;
}