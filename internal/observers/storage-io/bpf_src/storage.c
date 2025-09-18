// SPDX-License-Identifier: GPL-2.0
// Storage I/O Observer - CO-RE eBPF implementation
// Monitors VFS operations for storage performance insights

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event types
#define STORAGE_EVENT_READ      1
#define STORAGE_EVENT_WRITE     2
#define STORAGE_EVENT_FSYNC     3
#define STORAGE_EVENT_OPEN      4
#define STORAGE_EVENT_CLOSE     5
#define STORAGE_EVENT_BLOCK_IO  6
#define STORAGE_EVENT_AIO_SUBMIT 7
#define STORAGE_EVENT_AIO_COMPLETE 8

// Constants
#define MAX_PATH_LEN 256
#define TASK_COMM_LEN 16

// Storage I/O event structure - must match Go struct
struct storage_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    __u8  event_type;
    __u8  _pad[3];

    // I/O details
    __u64 inode;
    __u64 offset;
    __u64 size;
    __u64 latency_ns;
    __s32 error_code;

    // File info
    __u32 flags;
    __u32 mode;
    __u64 file_size;

    // Block layer details
    __u32 major;
    __u32 minor;
    __u64 sector;
    __u32 queue_depth;
    __u32 bio_flags;

    // Async I/O details
    __u64 aio_ctx_id;
    __u32 aio_nr_events;
    __u32 aio_flags;

    // Process info
    char comm[TASK_COMM_LEN];
    char full_path[MAX_PATH_LEN];
} __attribute__((packed));

// Active operations tracking
struct io_request {
    __u64 start_time;
    __u64 inode;
    __u64 offset;
    __u64 size;
    __u32 flags;
    __u32 major;
    __u32 minor;
    __u64 sector;
    char full_path[MAX_PATH_LEN];
};

// Block I/O tracking
struct block_io_req {
    __u64 start_time;
    __u32 pid;
    __u32 major;
    __u32 minor;
    __u64 sector;
    __u32 size;
    __u32 queue_depth;
};

// AIO tracking
struct aio_req {
    __u64 start_time;
    __u32 pid;
    __u64 ctx_id;
    __u32 nr_events;
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 * 1024 * 1024);  // 8MB
} storage_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);  // pid_tgid
    __type(value, struct io_request);
} active_io SEC(".maps");

// Block layer tracking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);  // request_ptr
    __type(value, struct block_io_req);
} active_block_io SEC(".maps");

// AIO tracking
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u64);  // aio_ctx_id
    __type(value, struct aio_req);
} active_aio SEC(".maps");

// Rate limiting
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);  // Last event time
} rate_limit SEC(".maps");

// Statistics
struct io_stats {
    __u64 total_reads;
    __u64 total_writes;
    __u64 total_fsyncs;
    __u64 slow_ios;
    __u64 errors;
    __u64 events_dropped;
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct io_stats);
} stats SEC(".maps");

// Configurable thresholds
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 2);
    __type(key, __u32);
    __type(value, __u64);
} config SEC(".maps");

static const __u32 CONFIG_SLOW_THRESHOLD_NS = 0;
static const __u32 CONFIG_RATE_LIMIT_NS = 1;

// Helper to get current cgroup ID with CO-RE
static __always_inline __u64 get_cgroup_id(struct task_struct *task) {
    __u64 cgroup_id = 0;

    // Use BPF helper if available (simpler and more reliable)
    #ifdef BPF_FUNC_get_current_cgroup_id
    cgroup_id = bpf_get_current_cgroup_id();
    #else
    // Fallback: Try to get cgroup ID using CO-RE
    if (bpf_core_field_exists(task->cgroups)) {
        struct css_set *cgroups = BPF_CORE_READ(task, cgroups);
        if (cgroups) {
            // Try subsys[0] approach (more compatible)
            if (bpf_core_field_exists(cgroups->subsys)) {
                struct cgroup_subsys_state *subsys = BPF_CORE_READ(cgroups, subsys[0]);
                if (subsys && bpf_core_field_exists(subsys->cgroup)) {
                    struct cgroup *cgrp = BPF_CORE_READ(subsys, cgroup);
                    if (cgrp && bpf_core_field_exists(cgrp->kn)) {
                        struct kernfs_node *kn = BPF_CORE_READ(cgrp, kn);
                        if (kn && bpf_core_field_exists(kn->id)) {
                            BPF_CORE_READ_INTO(&cgroup_id, kn, id);
                        }
                    }
                }
            }
        }
    }
    #endif

    return cgroup_id;
}

// Helper to check rate limiting
static __always_inline bool should_rate_limit(void) {
    __u32 key = 0;
    __u32 config_key = CONFIG_RATE_LIMIT_NS;
    __u64 *config_val = bpf_map_lookup_elem(&config, &config_key);
    __u64 rate_limit_ns = config_val ? *config_val : 1000000;  // Default 1ms

    __u64 now = bpf_ktime_get_ns();
    __u64 *last = bpf_map_lookup_elem(&rate_limit, &key);

    if (!last) return false;

    if (now - *last < rate_limit_ns) {
        // Update stats
        struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
        if (stat) {
            __sync_fetch_and_add(&stat->events_dropped, 1);
        }
        return true;
    }

    *last = now;
    return false;
}

// Helper to get filename (simplified for BPF verifier)
static __always_inline void get_full_path(struct file *file, char *buf, int size) {
    if (!file || size < 2) {
        if (size > 0) buf[0] = '\0';
        return;
    }

    // Get the path structure
    struct path *path_ptr = &file->f_path;
    if (!path_ptr) {
        buf[0] = '\0';
        return;
    }

    // Get dentry
    struct dentry *dentry = BPF_CORE_READ(path_ptr, dentry);
    if (!dentry) {
        buf[0] = '\0';
        return;
    }

    // Get just the filename (not full path for BPF simplicity)
    struct qstr d_name = BPF_CORE_READ(dentry, d_name);
    if (d_name.name && d_name.len > 0) {
        // Copy filename
        bpf_core_read_str(buf, size, d_name.name);
    } else {
        buf[0] = '\0';
    }

    // For full path reconstruction, we'd need userspace assistance
    // or a more complex BPF program with multiple iterations
}

// VFS read entry
SEC("kprobe/vfs_read")
int trace_vfs_read(struct pt_regs *ctx) {
    if (should_rate_limit()) return 0;

    __u64 pid_tgid = bpf_get_current_pid_tgid();

    // Get function arguments using PT_REGS macros (CO-RE safe)
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    size_t count = (size_t)PT_REGS_PARM3(ctx);
    loff_t *pos = (loff_t *)PT_REGS_PARM4(ctx);

    // Store request info
    struct io_request req = {
        .start_time = bpf_ktime_get_ns(),
    };

    if (file) {
        // Use CO-RE to read inode and device info
        struct inode *inode = BPF_CORE_READ(file, f_inode);
        if (inode) {
            req.inode = BPF_CORE_READ(inode, i_ino);

            // Get device info for block layer correlation
            // Note: vmlinux_minimal.h may not have i_sb field
            // Skip device info for now - would need full vmlinux.h
            req.major = 0;
            req.minor = 0;
        }

        // Read flags
        req.flags = BPF_CORE_READ(file, f_flags);

        // Get full path
        get_full_path(file, req.full_path, MAX_PATH_LEN);
    }

    if (pos) {
        bpf_probe_read_kernel(&req.offset, sizeof(req.offset), pos);
    }

    req.size = count;

    bpf_map_update_elem(&active_io, &pid_tgid, &req, BPF_ANY);

    // Update stats
    __u32 key = 0;
    struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
    if (stat) {
        __sync_fetch_and_add(&stat->total_reads, 1);
    }

    return 0;
}

// VFS read return
SEC("kretprobe/vfs_read")
int trace_vfs_read_ret(struct pt_regs *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __s64 ret = PT_REGS_RC(ctx);

    struct io_request *req = bpf_map_lookup_elem(&active_io, &pid_tgid);
    if (!req) return 0;

    // Allocate event from ring buffer
    struct storage_event *event = bpf_ringbuf_reserve(&storage_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_io, &pid_tgid);
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));

    // Fill basic info
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = STORAGE_EVENT_READ;
    event->pid = pid_tgid >> 32;
    event->tid = pid_tgid & 0xFFFFFFFF;

    // Get credentials using helper
    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;

    // Get task info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        bpf_get_current_comm(event->comm, sizeof(event->comm));
    }

    // I/O details
    event->inode = req->inode;
    event->offset = req->offset;
    event->size = ret > 0 ? ret : req->size;
    event->latency_ns = event->timestamp - req->start_time;
    event->error_code = ret < 0 ? ret : 0;
    event->flags = req->flags;

    // Block device info
    event->major = req->major;
    event->minor = req->minor;

    // Copy full path
    __builtin_memcpy(event->full_path, req->full_path, MAX_PATH_LEN);

    // Check for slow I/O
    __u32 key = 0;
    __u32 threshold_key = CONFIG_SLOW_THRESHOLD_NS;
    __u64 *threshold = bpf_map_lookup_elem(&config, &threshold_key);
    __u64 slow_threshold = threshold ? *threshold : 100000000;  // Default 100ms

    if (event->latency_ns > slow_threshold) {
        struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
        if (stat) {
            __sync_fetch_and_add(&stat->slow_ios, 1);
        }
    }

    // Submit event
    bpf_ringbuf_submit(event, 0);

    // Cleanup
    bpf_map_delete_elem(&active_io, &pid_tgid);

    return 0;
}

// VFS write entry
SEC("kprobe/vfs_write")
int trace_vfs_write(struct pt_regs *ctx) {
    if (should_rate_limit()) return 0;

    __u64 pid_tgid = bpf_get_current_pid_tgid();

    // Get function arguments
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    size_t count = (size_t)PT_REGS_PARM3(ctx);
    loff_t *pos = (loff_t *)PT_REGS_PARM4(ctx);

    // Store request info
    struct io_request req = {
        .start_time = bpf_ktime_get_ns(),
    };

    if (file) {
        struct inode *inode = BPF_CORE_READ(file, f_inode);
        if (inode) {
            req.inode = BPF_CORE_READ(inode, i_ino);
        }
        req.flags = BPF_CORE_READ(file, f_flags);
    }

    if (pos) {
        bpf_probe_read_kernel(&req.offset, sizeof(req.offset), pos);
    }

    req.size = count;

    bpf_map_update_elem(&active_io, &pid_tgid, &req, BPF_ANY);

    // Update stats
    __u32 key = 0;
    struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
    if (stat) {
        __sync_fetch_and_add(&stat->total_writes, 1);
    }

    return 0;
}

// VFS write return
SEC("kretprobe/vfs_write")
int trace_vfs_write_ret(struct pt_regs *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __s64 ret = PT_REGS_RC(ctx);

    struct io_request *req = bpf_map_lookup_elem(&active_io, &pid_tgid);
    if (!req) return 0;

    // Allocate event from ring buffer
    struct storage_event *event = bpf_ringbuf_reserve(&storage_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_io, &pid_tgid);
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));

    // Fill event
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = STORAGE_EVENT_WRITE;
    event->pid = pid_tgid >> 32;
    event->tid = pid_tgid & 0xFFFFFFFF;

    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;

    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        bpf_get_current_comm(event->comm, sizeof(event->comm));
    }

    event->inode = req->inode;
    event->offset = req->offset;
    event->size = ret > 0 ? ret : req->size;
    event->latency_ns = event->timestamp - req->start_time;
    event->error_code = ret < 0 ? ret : 0;
    event->flags = req->flags;

    // Block device info
    event->major = req->major;
    event->minor = req->minor;

    // Copy full path
    __builtin_memcpy(event->full_path, req->full_path, MAX_PATH_LEN);

    // Check for slow I/O
    __u32 key = 0;
    __u64 *threshold = bpf_map_lookup_elem(&config, &CONFIG_SLOW_THRESHOLD_NS);
    __u64 slow_threshold = threshold ? *threshold : 100000000;

    if (event->latency_ns > slow_threshold) {
        struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
        if (stat) {
            __sync_fetch_and_add(&stat->slow_ios, 1);
        }
    }

    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_io, &pid_tgid);

    return 0;
}

// VFS fsync entry
SEC("kprobe/vfs_fsync")
int trace_vfs_fsync(struct pt_regs *ctx) {
    if (should_rate_limit()) return 0;

    __u64 pid_tgid = bpf_get_current_pid_tgid();

    struct file *file = (struct file *)PT_REGS_PARM1(ctx);

    struct io_request req = {
        .start_time = bpf_ktime_get_ns(),
    };

    if (file) {
        struct inode *inode = BPF_CORE_READ(file, f_inode);
        if (inode) {
            req.inode = BPF_CORE_READ(inode, i_ino);
            req.size = BPF_CORE_READ(inode, i_size);
        }
        req.flags = BPF_CORE_READ(file, f_flags);
    }

    bpf_map_update_elem(&active_io, &pid_tgid, &req, BPF_ANY);

    __u32 key = 0;
    struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
    if (stat) {
        __sync_fetch_and_add(&stat->total_fsyncs, 1);
    }

    return 0;
}

// VFS fsync return
SEC("kretprobe/vfs_fsync")
int trace_vfs_fsync_ret(struct pt_regs *ctx) {
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __s32 ret = PT_REGS_RC(ctx);

    struct io_request *req = bpf_map_lookup_elem(&active_io, &pid_tgid);
    if (!req) return 0;

    struct storage_event *event = bpf_ringbuf_reserve(&storage_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_io, &pid_tgid);
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));

    event->timestamp = bpf_ktime_get_ns();
    event->event_type = STORAGE_EVENT_FSYNC;
    event->pid = pid_tgid >> 32;
    event->tid = pid_tgid & 0xFFFFFFFF;

    __u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;

    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        bpf_get_current_comm(event->comm, sizeof(event->comm));
    }

    event->inode = req->inode;
    event->file_size = req->size;
    event->latency_ns = event->timestamp - req->start_time;
    event->error_code = ret;
    event->flags = req->flags;

    // Fsync is often slow
    __u32 key = 0;
    __u64 *threshold = bpf_map_lookup_elem(&config, &CONFIG_SLOW_THRESHOLD_NS);
    __u64 slow_threshold = threshold ? *threshold : 100000000;

    if (event->latency_ns > slow_threshold) {
        struct io_stats *stat = bpf_map_lookup_elem(&stats, &key);
        if (stat) {
            __sync_fetch_and_add(&stat->slow_ios, 1);
        }
    }

    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_io, &pid_tgid);

    return 0;
}

// Block layer monitoring for device-level insights
SEC("kprobe/blk_mq_start_request")
int trace_block_io_start(struct pt_regs *ctx) {
    if (should_rate_limit()) return 0;

    struct request *rq = (struct request *)PT_REGS_PARM1(ctx);
    if (!rq) return 0;

    // Get block device info with CO-RE
    struct gendisk *disk = BPF_CORE_READ(rq, rq_disk);
    if (!disk) return 0;

    __u32 major = BPF_CORE_READ(disk, major);
    __u32 minor = BPF_CORE_READ(disk, first_minor);
    __u64 sector = BPF_CORE_READ(rq, __sector);
    __u32 size = BPF_CORE_READ(rq, __data_len);

    // Store block I/O request
    struct block_io_req bio_req = {
        .start_time = bpf_ktime_get_ns(),
        .pid = bpf_get_current_pid_tgid() >> 32,
        .major = major,
        .minor = minor,
        .sector = sector,
        .size = size,
        .queue_depth = 1, // Simplified - would need request_queue->nr_requests
    };

    __u64 rq_ptr = (__u64)rq;
    bpf_map_update_elem(&active_block_io, &rq_ptr, &bio_req, BPF_ANY);

    return 0;
}

// Block layer completion
SEC("kprobe/blk_account_io_done")
int trace_block_io_done(struct pt_regs *ctx) {
    struct request *rq = (struct request *)PT_REGS_PARM1(ctx);
    if (!rq) return 0;

    __u64 rq_ptr = (__u64)rq;
    struct block_io_req *bio_req = bpf_map_lookup_elem(&active_block_io, &rq_ptr);
    if (!bio_req) return 0;

    // Create block I/O completion event
    struct storage_event *event = bpf_ringbuf_reserve(&storage_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_block_io, &rq_ptr);
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = STORAGE_EVENT_BLOCK_IO;
    event->pid = bio_req->pid;

    // Process info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        bpf_get_current_comm(event->comm, sizeof(event->comm));
    }

    // Block details
    event->major = bio_req->major;
    event->minor = bio_req->minor;
    event->sector = bio_req->sector;
    event->size = bio_req->size;
    event->latency_ns = event->timestamp - bio_req->start_time;
    event->queue_depth = bio_req->queue_depth;

    // Submit event
    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_block_io, &rq_ptr);

    return 0;
}

// Async I/O monitoring
SEC("kprobe/io_submit")
int trace_aio_submit(struct pt_regs *ctx) {
    if (should_rate_limit()) return 0;

    __u64 ctx_id = (__u64)PT_REGS_PARM1(ctx);
    long nr_events = (long)PT_REGS_PARM2(ctx);

    if (nr_events <= 0) return 0;

    struct aio_req aio = {
        .start_time = bpf_ktime_get_ns(),
        .pid = bpf_get_current_pid_tgid() >> 32,
        .ctx_id = ctx_id,
        .nr_events = (__u32)nr_events,
    };

    bpf_map_update_elem(&active_aio, &ctx_id, &aio, BPF_ANY);

    return 0;
}

// AIO completion
SEC("kprobe/io_getevents")
int trace_aio_complete(struct pt_regs *ctx) {
    __u64 ctx_id = (__u64)PT_REGS_PARM1(ctx);

    struct aio_req *aio = bpf_map_lookup_elem(&active_aio, &ctx_id);
    if (!aio) return 0;

    // Create AIO completion event
    struct storage_event *event = bpf_ringbuf_reserve(&storage_events, sizeof(*event), 0);
    if (!event) {
        bpf_map_delete_elem(&active_aio, &ctx_id);
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->event_type = STORAGE_EVENT_AIO_COMPLETE;
    event->pid = aio->pid;

    // Process info
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (task) {
        event->cgroup_id = get_cgroup_id(task);
        bpf_get_current_comm(event->comm, sizeof(event->comm));
    }

    // AIO details
    event->aio_ctx_id = aio->ctx_id;
    event->aio_nr_events = aio->nr_events;
    event->latency_ns = event->timestamp - aio->start_time;

    bpf_ringbuf_submit(event, 0);
    bpf_map_delete_elem(&active_aio, &ctx_id);

    return 0;
}

char _license[] SEC("license") = "GPL";