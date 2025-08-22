//go:build ignore

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_COMM_LEN 16
#define MAX_FILENAME_LEN 256
#define MAX_UNIT_NAME_LEN 64

// Event types for systemd services
#define EVENT_SERVICE_START 1
#define EVENT_SERVICE_STOP 2
#define EVENT_SERVICE_RESTART 3
#define EVENT_SERVICE_RELOAD 4
#define EVENT_SERVICE_FAILED 5
#define EVENT_EXEC 6
#define EVENT_EXIT 7
#define EVENT_SIGNAL 8

// Service state tracking
struct service_state {
    __u64 start_time;
    __u64 stop_time;
    __u32 main_pid;
    __u32 control_pid;
    __u8 state; // 0=inactive, 1=activating, 2=active, 3=deactivating, 4=failed
    __u8 restart_count;
    char unit_name[MAX_UNIT_NAME_LEN];
};

// Event structure for systemd monitoring
struct systemd_event {
    __u64 timestamp;
    __u32 pid;
    __u32 ppid;
    __u32 tgid;
    __u32 event_type;
    __u32 exit_code;
    __u32 signal;
    __u32 uid;
    __u32 gid;
    __u64 cgroup_id;
    char comm[MAX_COMM_LEN];
    char filename[MAX_FILENAME_LEN];
    char unit_name[MAX_UNIT_NAME_LEN];
    __u8 service_state;
    __u8 restart_count;
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// Map to track systemd PIDs
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, __u8);
} systemd_pids SEC(".maps");

// Map to track service states by cgroup ID
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, __u64); // cgroup ID
    __type(value, struct service_state);
} service_states SEC(".maps");

// Map to track service PIDs to cgroup mapping
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192);
    __type(key, __u32); // PID
    __type(value, __u64); // cgroup ID
} pid_to_cgroup SEC(".maps");

// Helper to check if PID is systemd-related
static __always_inline int is_systemd_process(__u32 pid) {
    // Check if PID 1 (systemd) or in systemd_pids map
    if (pid == 1) {
        return 1;
    }
    return bpf_map_lookup_elem(&systemd_pids, &pid) != NULL;
}

// Helper to get cgroup ID for current task
static __always_inline __u64 get_current_cgroup_id() {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        return 0;
    }
    
    // Use BPF helper to get cgroup ID
    return bpf_get_current_cgroup_id();
}

// Helper to extract unit name from comm (simplified)
static __always_inline void extract_unit_name(char *comm, char *unit_name, int max_len) {
    int i;
    #pragma unroll
    for (i = 0; i < MAX_UNIT_NAME_LEN - 1 && i < max_len - 1; i++) {
        if (comm[i] == '\0') {
            break;
        }
        unit_name[i] = comm[i];
    }
    unit_name[i] = '\0';
}

// Track process execution for systemd services
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_exec(void *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task) {
        return 0;
    }
    
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid = pid_tgid >> 32;
    __u32 tgid = pid_tgid & 0xFFFFFFFF;
    
    // Use CO-RE to safely read parent PID
    __u32 ppid = 0;
    if (bpf_core_field_exists(task->real_parent) && 
        bpf_core_field_exists(((struct task_struct *)0)->tgid)) {
        struct task_struct *parent;
        if (BPF_CORE_READ_INTO(&parent, task, real_parent) == 0 && parent) {
            BPF_CORE_READ_INTO(&ppid, parent, tgid);
        }
    }

    // Check if this is systemd-related
    if (!is_systemd_process(ppid) && ppid != 1) {
        return 0;
    }

    // Get cgroup ID to track service
    __u64 cgroup_id = get_current_cgroup_id();
    if (cgroup_id == 0) {
        return 0;
    }

    struct systemd_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->ppid = ppid;
    event->tgid = tgid;
    event->cgroup_id = cgroup_id;
    event->event_type = EVENT_SERVICE_START;
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;

    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    extract_unit_name(event->comm, event->unit_name, sizeof(event->comm));
    
    // Track this service PID
    __u8 val = 1;
    bpf_map_update_elem(&systemd_pids, &pid, &val, BPF_ANY);
    bpf_map_update_elem(&pid_to_cgroup, &pid, &cgroup_id, BPF_ANY);
    
    // Update service state
    struct service_state state = {0};
    state.start_time = event->timestamp;
    state.main_pid = pid;
    state.state = 1; // activating
    __builtin_memcpy(state.unit_name, event->unit_name, sizeof(state.unit_name));
    bpf_map_update_elem(&service_states, &cgroup_id, &state, BPF_ANY);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track process exit for systemd services
SEC("tracepoint/sched/sched_process_exit")
int trace_exit(struct trace_event_raw_sched_process_template *ctx) {
    __u32 pid = ctx->pid;
    
    // Check if this PID is tracked
    __u64 *cgroup_id = bpf_map_lookup_elem(&pid_to_cgroup, &pid);
    if (!cgroup_id) {
        return 0;
    }

    struct systemd_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->cgroup_id = *cgroup_id;
    event->event_type = EVENT_SERVICE_STOP;
    
    // Update service state
    struct service_state *state = bpf_map_lookup_elem(&service_states, cgroup_id);
    if (state) {
        state->stop_time = event->timestamp;
        state->state = 0; // inactive
        __builtin_memcpy(event->unit_name, state->unit_name, sizeof(event->unit_name));
        event->service_state = state->state;
        event->restart_count = state->restart_count;
    }

    // Get comm from the trace event context 
    if (ctx->comm) {
        bpf_probe_read_kernel_str(&event->comm, sizeof(event->comm), ctx->comm);
    }
    
    // Clean up tracking maps
    bpf_map_delete_elem(&systemd_pids, &pid);
    bpf_map_delete_elem(&pid_to_cgroup, &pid);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Track signals sent to services (for restart/reload detection)
SEC("tracepoint/signal/signal_generate")
int trace_signal(struct trace_event_raw_signal_generate *ctx) {
    __u32 pid = ctx->pid;
    __u32 sig = ctx->sig;
    
    // Only track SIGHUP (reload) and SIGTERM (stop)
    if (sig != 1 && sig != 15) {
        return 0;
    }
    
    // Check if this PID is tracked
    __u64 *cgroup_id = bpf_map_lookup_elem(&pid_to_cgroup, &pid);
    if (!cgroup_id) {
        return 0;
    }

    struct systemd_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    __builtin_memset(event, 0, sizeof(*event));
    event->timestamp = bpf_ktime_get_ns();
    event->pid = pid;
    event->cgroup_id = *cgroup_id;
    event->signal = sig;
    
    // Determine event type based on signal
    if (sig == 1) { // SIGHUP
        event->event_type = EVENT_SERVICE_RELOAD;
    } else if (sig == 15) { // SIGTERM
        event->event_type = EVENT_SERVICE_STOP;
    }
    
    // Get service state
    struct service_state *state = bpf_map_lookup_elem(&service_states, cgroup_id);
    if (state) {
        __builtin_memcpy(event->unit_name, state->unit_name, sizeof(event->unit_name));
        event->service_state = state->state;
        
        // Update restart count if restarting
        if (state->state == 3 && sig == 15) { // deactivating and got SIGTERM
            state->restart_count++;
        }
        event->restart_count = state->restart_count;
    }

    // Get comm from task struct
    if (ctx->task) {
        bpf_probe_read_kernel_str(&event->comm, sizeof(event->comm), ctx->task->comm);
    }
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";