// SPDX-License-Identifier: GPL-2.0
// Helm operation tracker - Monitor helm/kubectl for failure correlation

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event types
#define EVENT_PROCESS_EXEC    1
#define EVENT_PROCESS_EXIT    2
#define EVENT_FILE_OPEN       3
#define EVENT_TCP_SEND        4
#define EVENT_WRITE_OUTPUT    5

// File types
#define FILE_TYPE_VALUES      1
#define FILE_TYPE_TEMPLATE    2
#define FILE_TYPE_CHART       3
#define FILE_TYPE_OTHER       4

// Process info
struct process_info {
    __u32 pid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    __u64 start_time;
    char comm[16];
    char filename[256];
};

// Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);   // PID
    __type(value, struct process_info);
} tracked_processes SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24); // 16MB
} events SEC(".maps");

// Event structures
struct helm_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 uid;
    __u32 gid;
    __u32 event_type;
    union {
        struct {
            char comm[16];
            char filename[256];
            char args[512]; // Command line arguments
        } exec;
        struct {
            __s32 exit_code;
            __s32 signal;
            __u64 duration_ns;
        } exit;
        struct {
            char path[256];
            __u32 flags;
            __u32 mode;
            __u32 size;
            __u8 file_type;
        } file;
        struct {
            __u32 saddr;
            __u32 daddr;
            __u16 sport;
            __u16 dport;
            __u32 size;
        } tcp;
        struct {
            __u32 fd;
            __u32 count;
            char data[256]; // First 256 bytes of output
        } write;
    } data;
};

// Helper to check if process should be tracked
static __always_inline bool should_track_process(const char *comm) {
    // Track helm and kubectl
    if (comm[0] == 'h' && comm[1] == 'e' && comm[2] == 'l' && comm[3] == 'm') {
        return true;
    }
    if (comm[0] == 'k' && comm[1] == 'u' && comm[2] == 'b' && 
        comm[3] == 'e' && comm[4] == 'c' && comm[5] == 't' && comm[6] == 'l') {
        return true;
    }
    return false;
}

// Helper to determine file type
static __always_inline __u8 get_file_type(const char *path) {
    // Check for values files
    if (path[0] == 'v' && path[1] == 'a' && path[2] == 'l' && 
        path[3] == 'u' && path[4] == 'e' && path[5] == 's') {
        return FILE_TYPE_VALUES;
    }
    
    // Check for .yaml or .yml extension
    int i;
    for (i = 0; i < 256 && path[i]; i++) {
        if (path[i] == '.') {
            if ((path[i+1] == 'y' && path[i+2] == 'a' && path[i+3] == 'm' && path[i+4] == 'l') ||
                (path[i+1] == 'y' && path[i+2] == 'm' && path[i+3] == 'l')) {
                // Check if it's in templates directory
                int j;
                for (j = 0; j < i; j++) {
                    if (path[j] == 't' && path[j+1] == 'e' && path[j+2] == 'm' &&
                        path[j+3] == 'p' && path[j+4] == 'l' && path[j+5] == 'a' &&
                        path[j+6] == 't' && path[j+7] == 'e') {
                        return FILE_TYPE_TEMPLATE;
                    }
                }
                return FILE_TYPE_CHART;
            }
        }
    }
    
    return FILE_TYPE_OTHER;
}

// Track process execution
SEC("tracepoint/sched/sched_process_exec")
int trace_exec(struct trace_event_raw_sched_process_exec *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    char comm[16];
    bpf_probe_read_kernel_str(comm, sizeof(comm), ctx->comm);
    
    if (!should_track_process(comm)) {
        return 0;
    }
    
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u32 tid = bpf_get_current_pid_tgid();
    __u32 uid = bpf_get_current_uid_gid() >> 32;
    __u32 gid = bpf_get_current_uid_gid();
    
    // Store process info
    struct process_info info = {};
    info.pid = pid;
    info.ppid = BPF_CORE_READ(task, real_parent, tgid);
    info.uid = uid;
    info.gid = gid;
    info.start_time = bpf_ktime_get_ns();
    bpf_probe_read_kernel_str(info.comm, sizeof(info.comm), comm);
    bpf_probe_read_kernel_str(info.filename, sizeof(info.filename), ctx->filename);
    
    bpf_map_update_elem(&tracked_processes, &pid, &info, BPF_ANY);
    
    // Emit exec event
    struct helm_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = tid;
    event->uid = uid;
    event->gid = gid;
    event->event_type = EVENT_PROCESS_EXEC;
    
    bpf_probe_read_kernel_str(event->data.exec.comm, sizeof(event->data.exec.comm), comm);
    bpf_probe_read_kernel_str(event->data.exec.filename, sizeof(event->data.exec.filename), ctx->filename);
    
    // Try to get command line arguments
    struct mm_struct *mm = BPF_CORE_READ(task, mm);
    if (mm) {
        unsigned long arg_start = BPF_CORE_READ(mm, arg_start);
        unsigned long arg_end = BPF_CORE_READ(mm, arg_end);
        unsigned long arg_len = arg_end - arg_start;
        if (arg_len > 0 && arg_len < sizeof(event->data.exec.args)) {
            bpf_probe_read_user_str(event->data.exec.args, sizeof(event->data.exec.args), 
                                   (void *)arg_start);
        }
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track process exit
SEC("tracepoint/sched/sched_process_exit")
int trace_exit(struct trace_event_raw_sched_process_template *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct process_info *info = bpf_map_lookup_elem(&tracked_processes, &pid);
    if (!info) {
        return 0; // Not a tracked process
    }
    
    struct helm_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = bpf_get_current_pid_tgid();
    event->uid = info->uid;
    event->gid = info->gid;
    event->event_type = EVENT_PROCESS_EXIT;
    
    // Get exit code from task struct
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    event->data.exit.exit_code = BPF_CORE_READ(task, exit_code) >> 8;
    event->data.exit.signal = BPF_CORE_READ(task, exit_code) & 0xFF;
    event->data.exit.duration_ns = bpf_ktime_get_ns() - info->start_time;
    
    bpf_ringbuf_submit(event, 0);
    
    // Clean up
    bpf_map_delete_elem(&tracked_processes, &pid);
    
    return 0;
}

// Track file operations
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct process_info *info = bpf_map_lookup_elem(&tracked_processes, &pid);
    if (!info) {
        return 0; // Not a tracked process
    }
    
    struct helm_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = bpf_get_current_pid_tgid();
    event->uid = info->uid;
    event->gid = info->gid;
    event->event_type = EVENT_FILE_OPEN;
    
    // Get file path
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(event->data.file.path, sizeof(event->data.file.path), pathname);
    
    event->data.file.flags = (int)ctx->args[2];
    event->data.file.mode = (int)ctx->args[3];
    event->data.file.file_type = get_file_type(event->data.file.path);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track TCP sends (API calls)
SEC("kprobe/tcp_sendmsg")
int trace_tcp_send(struct pt_regs *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct process_info *info = bpf_map_lookup_elem(&tracked_processes, &pid);
    if (!info) {
        return 0; // Not a tracked process
    }
    
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    size_t size = (size_t)PT_REGS_PARM3(ctx);
    
    struct helm_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = bpf_get_current_pid_tgid();
    event->uid = info->uid;
    event->gid = info->gid;
    event->event_type = EVENT_TCP_SEND;
    
    // Get connection info
    event->data.tcp.saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    event->data.tcp.daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    event->data.tcp.sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    event->data.tcp.dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    event->data.tcp.size = size;
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track write syscalls (stdout/stderr)
SEC("tracepoint/syscalls/sys_exit_write")
int trace_write_exit(struct trace_event_raw_sys_exit *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    
    struct process_info *info = bpf_map_lookup_elem(&tracked_processes, &pid);
    if (!info) {
        return 0; // Not a tracked process
    }
    
    // Only track stdout (1) and stderr (2)
    long fd = ctx->args[0];
    if (fd != 1 && fd != 2) {
        return 0;
    }
    
    long ret = ctx->ret;
    if (ret <= 0) {
        return 0; // Write failed or nothing written
    }
    
    struct helm_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }
    
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->tid = bpf_get_current_pid_tgid();
    event->uid = info->uid;
    event->gid = info->gid;
    event->event_type = EVENT_WRITE_OUTPUT;
    
    event->data.write.fd = fd;
    event->data.write.count = ret;
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";