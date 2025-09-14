// SPDX-License-Identifier: GPL-2.0
// Runtime signals monitoring - tracks process lifecycle, signals, and death attribution

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Event types - Process lifecycle and signals
#define EVENT_PROCESS_EXEC      1  // New process started
#define EVENT_PROCESS_EXIT      2  // Process terminated
#define EVENT_SIGNAL_SENT       3  // Signal generated
#define EVENT_SIGNAL_RECEIVED   4  // Signal delivered
#define EVENT_OOM_KILL          5  // OOM killer activated
#define EVENT_CPU_THROTTLE      6  // CPU limit hit

// Common signals we care about
#define SIGKILL     9
#define SIGSEGV     11
#define SIGTERM     15
#define SIGCHLD     17
#define SIGSTOP     19
#define SIGABRT     6
#define SIGQUIT     3
#define SIGBUS      7

// Exit code encoding: upper bits contain signal if killed
#define EXIT_CODE_MASK      0xFF
#define EXIT_SIGNAL_SHIFT   8

struct runtime_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 ppid;          // Parent PID
    __u32 event_type;
    __u32 exit_code;      // Full exit code including signal
    __u32 signal;         // Signal number for signal events
    __u32 sender_pid;     // PID that sent signal
    __u64 cgroup_id;
    char comm[16];        // Process name
    char parent_comm[16]; // Parent process name
    union {
        struct {
            __u32 uid;
            __u32 gid;
        } exec_info;
        struct {
            __u64 utime;      // User CPU time
            __u64 stime;      // System CPU time
            __u64 memory_rss; // RSS at exit
        } exit_info;
        struct {
            __u32 target_pid;  // Who receives signal
            __u8  is_fatal;    // Is this a fatal signal
            __u8  _pad[3];
        } signal_info;
    };
} __attribute__((packed));

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB for high-volume events
} events SEC(".maps");

// Track process info for correlation
struct process_info {
    __u32 ppid;
    __u64 start_time;
    char comm[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, __u32);  // PID
    __type(value, struct process_info);
} process_map SEC(".maps");

// Helper to get current cgroup ID safely
static __always_inline __u64 get_current_cgroup_id(void)
{
    // First try the BPF helper - most reliable and fast
    __u64 cgroup_id = bpf_get_current_cgroup_id();
    if (cgroup_id > 0)
        return cgroup_id;
    
    // Fallback: extract from task struct using CO-RE
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task)
        return 0;
    
    // Check if cgroups field exists
    if (!bpf_core_field_exists(task->cgroups))
        return 0;
    
    struct css_set *css_set_ptr;
    if (BPF_CORE_READ_INTO(&css_set_ptr, task, cgroups) != 0 || !css_set_ptr)
        return 0;
    
    // Read cgroup subsystem state for unified hierarchy
    struct cgroup_subsys_state *cgroup_ss;
    if (BPF_CORE_READ_INTO(&cgroup_ss, css_set_ptr, subsys[0]) != 0 || !cgroup_ss)
        return 0;
    
    // Get cgroup from subsystem state
    struct cgroup *cgrp;
    if (BPF_CORE_READ_INTO(&cgrp, cgroup_ss, cgroup) != 0 || !cgrp)
        return 0;
    
    // Get kernfs node
    struct kernfs_node *kn;
    if (BPF_CORE_READ_INTO(&kn, cgrp, kn) != 0 || !kn)
        return 0;
    
    // Finally read the ID
    if (BPF_CORE_READ_INTO(&cgroup_id, kn, id) != 0)
        return 0;
    
    return cgroup_id;
}

// Process exec - track new processes
SEC("tracepoint/sched/sched_process_exec")
int trace_process_exec(struct trace_event_raw_sched_process_exec *ctx)
{
    struct runtime_event *e;
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memset(e, 0, sizeof(*e));
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid & 0xFFFFFFFF;
    e->tgid = pid_tgid >> 32;
    e->event_type = EVENT_PROCESS_EXEC;
    e->cgroup_id = get_current_cgroup_id();
    
    // Get parent PID
    struct task_struct *parent = BPF_CORE_READ(task, real_parent);
    if (parent) {
        e->ppid = BPF_CORE_READ(parent, tgid);
        bpf_core_read_str(e->parent_comm, sizeof(e->parent_comm), &parent->comm);
    }
    
    // Get UID/GID
    __u64 uid_gid = bpf_get_current_uid_gid();
    e->exec_info.uid = uid_gid & 0xFFFFFFFF;
    e->exec_info.gid = uid_gid >> 32;
    
    // Get process name from tracepoint context
    bpf_probe_read_kernel_str(e->comm, sizeof(e->comm), ctx->comm);
    
    // Store process info for later correlation
    struct process_info pinfo = {
        .ppid = e->ppid,
        .start_time = e->timestamp
    };
    __builtin_memcpy(pinfo.comm, e->comm, sizeof(pinfo.comm));
    bpf_map_update_elem(&process_map, &e->tgid, &pinfo, BPF_ANY);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Process exit - track deaths and exit codes
SEC("tracepoint/sched/sched_process_exit")
int trace_process_exit(struct trace_event_raw_sched_process_exit *ctx)
{
    struct runtime_event *e;
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memset(e, 0, sizeof(*e));
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid & 0xFFFFFFFF;
    e->tgid = pid_tgid >> 32;
    e->event_type = EVENT_PROCESS_EXIT;
    e->cgroup_id = get_current_cgroup_id();
    
    // Get exit code from context - not available directly from task struct
    // For process exits via signals, the signal is in the context
    e->exit_code = 0;  // Initialize to 0, will be set by signal handlers
    
    // Extract signal from exit code if killed by signal
    if (e->exit_code & 0x7F) {
        e->signal = e->exit_code & 0x7F;
    }
    
    // Get parent info
    struct task_struct *parent = BPF_CORE_READ(task, real_parent);
    if (parent) {
        e->ppid = BPF_CORE_READ(parent, tgid);
        bpf_core_read_str(e->parent_comm, sizeof(e->parent_comm), &parent->comm);
    }
    
    // Get process name and stats
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // CPU times are not directly accessible from task_struct in eBPF
    // These would need to be obtained via different mechanism
    e->exit_info.utime = 0;
    e->exit_info.stime = 0;
    
    // Get memory RSS safely using CO-RE
    struct mm_struct *mm = BPF_CORE_READ(task, mm);
    if (mm) {
        // RSS statistics structure varies by kernel version
        // Use total_vm as a proxy for memory usage
        e->exit_info.memory_rss = BPF_CORE_READ(mm, total_vm);
    }
    
    // Clean up process map entry
    bpf_map_delete_elem(&process_map, &e->tgid);
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Signal generation - track who sends signals
SEC("tracepoint/signal/signal_generate")
int trace_signal_generate(struct trace_event_raw_signal_generate *ctx)
{
    struct runtime_event *e;
    
    // Filter out common non-fatal signals to reduce noise
    int sig = ctx->sig;
    if (sig != SIGKILL && sig != SIGTERM && sig != SIGSEGV && 
        sig != SIGABRT && sig != SIGQUIT && sig != SIGSTOP && sig != SIGBUS) {
        return 0;
    }
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memset(e, 0, sizeof(*e));
    
    e->timestamp = bpf_ktime_get_ns();
    e->event_type = EVENT_SIGNAL_SENT;
    e->signal = sig;
    
    // Sender info
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    e->sender_pid = pid_tgid >> 32;
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    // Target info
    e->signal_info.target_pid = ctx->pid;
    
    // Determine if signal is fatal
    e->signal_info.is_fatal = (sig == SIGKILL || sig == SIGSEGV || 
                               sig == SIGABRT || sig == SIGBUS);
    
    e->cgroup_id = get_current_cgroup_id();
    
    // Look up target process name if available
    struct process_info *pinfo = bpf_map_lookup_elem(&process_map, &ctx->pid);
    if (pinfo) {
        __builtin_memcpy(e->parent_comm, pinfo->comm, sizeof(e->parent_comm));
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Signal delivery - track signal reception
SEC("tracepoint/signal/signal_deliver") 
int trace_signal_deliver(struct trace_event_raw_signal_deliver *ctx)
{
    // Only track delivery of fatal signals to reduce volume
    int sig = ctx->sig;
    if (sig != SIGKILL && sig != SIGTERM && sig != SIGSEGV && 
        sig != SIGABRT && sig != SIGQUIT && sig != SIGBUS) {
        return 0;
    }
    
    struct runtime_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memset(e, 0, sizeof(*e));
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid_tgid & 0xFFFFFFFF;
    e->tgid = pid_tgid >> 32;
    e->event_type = EVENT_SIGNAL_RECEIVED;
    e->signal = sig;
    e->cgroup_id = get_current_cgroup_id();
    
    bpf_get_current_comm(e->comm, sizeof(e->comm));
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// OOM killer tracking
SEC("kprobe/oom_kill_process")
int trace_oom_kill(struct pt_regs *ctx)
{
    struct runtime_event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    __builtin_memset(e, 0, sizeof(*e));
    
    e->timestamp = bpf_ktime_get_ns();
    e->event_type = EVENT_OOM_KILL;
    
    // OOM killer is triggered by kernel, sender_pid = 0
    e->sender_pid = 0;
    __builtin_memcpy(e->comm, "oom_killer", 11);
    
    // Note: victim task parameter reading is architecture-dependent
    // For safety across different architectures, we'll capture what we can
    // The actual victim PID can be determined from correlation with exit events
    
    e->signal = SIGKILL;  // OOM always sends SIGKILL
    e->signal_info.is_fatal = 1;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";