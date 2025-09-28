//go:build ignore

// SPDX-License-Identifier: GPL-2.0
// Health Monitor eBPF Program - Real kernel-level syscall monitoring
// Detects system health issues via syscall failure patterns

#include "../../bpf_common/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Health event structure (must match Go struct exactly)
struct health_event {
    u64 timestamp_ns;
    u32 pid;
    u32 ppid;
    u32 tid;         // Added to match Go struct
    u32 uid;
    u32 gid;
    u64 cgroup_id;

    // Syscall info
    u32 syscall_nr;
    s32 error_code;    // Negative errno
    u8 category;       // 1=file, 2=network, 3=memory, 4=process
    u8 _pad[3];        // Padding

    // Context
    char comm[16];       // Process name
    char path[256];      // File path (if applicable)

    // Network context (if applicable)
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;

    // Additional args from Go struct
    u64 arg1;
    u64 arg2;
    u64 arg3;
    u32 error_count;   // How many times this error happened
    u32 _pad2;         // Final padding
};

// Error tracking key
struct error_key {
    u32 pid;
    u32 syscall_nr;
    s32 error_code;
};

// Error statistics
struct error_stats {
    u32 count;
    u64 first_seen_ns;
    u64 last_seen_ns;
};

// Configuration
struct health_config {
    u32 rate_limit_ns;     // Minimum time between events
    u32 max_error_count;   // Max errors before rate limiting
    u8 enable_file;        // Monitor file operations
    u8 enable_network;     // Monitor network operations
    u8 enable_memory;      // Monitor memory operations
    u8 enable_process;     // Monitor process operations
};

// BPF Maps
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, struct error_key);
    __type(value, struct error_stats);
} error_tracking SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} health_events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct health_config);
} config SEC(".maps");

// Rate limiting map (per-PID)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, u32);
    __type(value, u64);
} rate_limit SEC(".maps");

// Helper to check if we should rate limit
static __always_inline int should_rate_limit(u32 pid) {
    u64 now = bpf_ktime_get_ns();
    u64 *last_event = bpf_map_lookup_elem(&rate_limit, &pid);

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg) {
        return 1; // Rate limit if no config
    }

    if (last_event) {
        if (now - *last_event < cfg->rate_limit_ns) {
            return 1; // Too soon
        }
    }

    // Update last event time
    bpf_map_update_elem(&rate_limit, &pid, &now, BPF_ANY);
    return 0;
}

// Helper to update error tracking
static __always_inline void update_error_tracking(u32 pid, u32 syscall_nr, s32 error_code) {
    struct error_key key = {
        .pid = pid,
        .syscall_nr = syscall_nr,
        .error_code = error_code,
    };

    u64 now = bpf_ktime_get_ns();
    struct error_stats *stats = bpf_map_lookup_elem(&error_tracking, &key);

    if (stats) {
        stats->count++;
        stats->last_seen_ns = now;
    } else {
        struct error_stats new_stats = {
            .count = 1,
            .first_seen_ns = now,
            .last_seen_ns = now,
        };
        bpf_map_update_elem(&error_tracking, &key, &new_stats, BPF_NOEXIST);
    }
}

// Helper to send health event
static __always_inline void send_health_event(u32 syscall_nr, s32 error_code, u8 category) {
    struct health_event *event;
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;

    // Rate limiting
    if (should_rate_limit(pid)) {
        return;
    }

    // Reserve space in ring buffer
    event = bpf_ringbuf_reserve(&health_events, sizeof(*event), 0);
    if (!event) {
        return;
    }

    // Fill event
    event->timestamp_ns = bpf_ktime_get_ns();
    event->pid = pid;
    event->ppid = pid_tgid & 0xFFFFFFFF; // Simplified
    event->tid = pid_tgid & 0xFFFFFFFF;  // Thread ID (same as PID for now)

    u64 uid_gid = bpf_get_current_uid_gid();
    event->uid = uid_gid & 0xFFFFFFFF;
    event->gid = uid_gid >> 32;

    event->cgroup_id = bpf_get_current_cgroup_id();
    event->syscall_nr = syscall_nr;
    event->error_code = error_code;
    event->category = category;

    // Initialize additional fields
    event->arg1 = 0;
    event->arg2 = 0;
    event->arg3 = 0;

    // Get process name
    bpf_get_current_comm(event->comm, sizeof(event->comm));

    // Update error tracking
    update_error_tracking(pid, syscall_nr, error_code);

    // Get error count
    struct error_key key = {
        .pid = pid,
        .syscall_nr = syscall_nr,
        .error_code = error_code,
    };
    struct error_stats *stats = bpf_map_lookup_elem(&error_tracking, &key);
    event->error_count = stats ? stats->count : 1;

    // Submit event
    bpf_ringbuf_submit(event, 0);
}

// File operation syscalls
SEC("tracepoint/syscalls/sys_exit_open")
int trace_exit_open(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0; // Success
    }

    // Check if file monitoring is enabled
    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_file) {
        return 0;
    }

    // Important file errors
    s32 error = -ctx->ret;
    if (error == 28 ||  // ENOSPC - No space left
        error == 24 ||  // EMFILE - Too many open files
        error == 13) {  // EACCES - Permission denied
        send_health_event(2, -error, 1); // Category 1 = file
    }

    return 0;
}

SEC("tracepoint/syscalls/sys_exit_openat")
int trace_exit_openat(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_file) {
        return 0;
    }

    s32 error = -ctx->ret;
    if (error == 28 ||  // ENOSPC - No space left
        error == 24 ||  // EMFILE - Too many open files
        error == 13 ||  // EACCES - Permission denied
        error == 2) {   // ENOENT - No such file or directory
        send_health_event(257, -error, 1); // openat syscall
    }

    return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int trace_exit_write(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_file) {
        return 0;
    }

    s32 error = -ctx->ret;
    if (error == 28 ||  // ENOSPC
        error == 5) {   // EIO - I/O error
        send_health_event(1, -error, 1);
    }

    return 0;
}

// Memory allocation syscalls
SEC("tracepoint/syscalls/sys_exit_mmap")
int trace_exit_mmap(struct trace_event_raw_sys_exit *ctx) {
    // mmap returns -errno on failure (not -1)
    if ((long)ctx->ret >= 0 || (long)ctx->ret < -4095) {
        return 0; // Success or not an error
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_memory) {
        return 0;
    }

    // Extract actual error code
    s32 error = -(s32)ctx->ret;
    send_health_event(9, -error, 3); // category 3 = memory
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_brk")
int trace_exit_brk(struct trace_event_raw_sys_exit *ctx) {
    // brk returns 0 on failure, current break on success
    if (ctx->ret != 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_memory) {
        return 0;
    }

    send_health_event(12, -12, 3); // brk failed, ENOMEM
    return 0;
}

// Network syscalls
SEC("tracepoint/syscalls/sys_exit_connect")
int trace_exit_connect(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_network) {
        return 0;
    }

    s32 error = -ctx->ret;
    // Capture more network errors
    send_health_event(42, -error, 2); // Category 2 = network

    return 0;
}

SEC("tracepoint/syscalls/sys_exit_bind")
int trace_exit_bind(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_network) {
        return 0;
    }

    s32 error = -ctx->ret;
    // Capture all bind errors
    send_health_event(49, -error, 2);

    return 0;
}

// Process syscalls
SEC("tracepoint/syscalls/sys_exit_fork")
int trace_exit_fork(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_process) {
        return 0;
    }

    s32 error = -ctx->ret;
    if (error == 11 ||   // EAGAIN - Resource temporarily unavailable
        error == 12) {   // ENOMEM
        send_health_event(57, -error, 4); // Category 4 = process
    }

    return 0;
}

SEC("tracepoint/syscalls/sys_exit_clone")
int trace_exit_clone(struct trace_event_raw_sys_exit *ctx) {
    if (ctx->ret >= 0) {
        return 0;
    }

    u32 config_key = 0;
    struct health_config *cfg = bpf_map_lookup_elem(&config, &config_key);
    if (!cfg || !cfg->enable_process) {
        return 0;
    }

    s32 error = -ctx->ret;
    // Capture all clone errors
    send_health_event(56, -error, 4);

    return 0;
}

char _license[] SEC("license") = "GPL";