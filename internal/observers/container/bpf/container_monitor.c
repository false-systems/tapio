//go:build ignore

// Container Observer eBPF Program
// Captures OOM kills and process exits with cgroup context
// Following Brendan Gregg's principle: "eBPF captures, Go parses"

#include "../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event types (MUST match Go ContainerEventBPF.Type)
#define EVENT_TYPE_OOM_KILL 0
#define EVENT_TYPE_EXIT     1

// Cgroup path max length
#define CGROUP_PATH_LEN 256

// Container event structure
// MUST match Go ContainerEventBPF exactly (300 bytes)
// Field order optimized for alignment (uint64 first)
struct container_event {
	__u64 memory_limit;          // offset 0: Memory limit from cgroup
	__u64 memory_usage;          // offset 8: Memory usage from cgroup
	__u64 timestamp_ns;          // offset 16: Event timestamp in nanoseconds
	__u32 type;                  // offset 24: EVENT_TYPE_OOM_KILL or EVENT_TYPE_EXIT
	__u32 pid;                   // offset 28: Process ID
	__u32 tid;                   // offset 32: Thread ID
	__s32 exit_code;             // offset 36: Exit code
	__s32 signal;                // offset 40: Signal number
	char  cgroup_path[CGROUP_PATH_LEN]; // offset 44: cgroup path (256 bytes)
} __attribute__((packed));

// Ring buffer for events (256KB)
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);
} events SEC(".maps");

// ═══════════════════════════════════════════════════════════
// HOOK 1: OOM Kill Detection
// ═══════════════════════════════════════════════════════════
// Tracepoint: oom/mark_victim
// Triggered when kernel OOM killer selects a victim process
// We capture memory info HERE because cgroup will be deleted shortly
// ═══════════════════════════════════════════════════════════

// Tracepoint context for oom/mark_victim
// Format: cat /sys/kernel/debug/tracing/events/oom/mark_victim/format
struct trace_event_raw_mark_victim {
	__u64 __unused;           // Common tracepoint header
	int pid;                  // Victim PID
	unsigned int uid;         // Victim UID
	unsigned int gid;         // Victim GID
	unsigned long total_vm;   // Total VM pages
	unsigned long anon_rss;   // Anonymous RSS pages
	unsigned long file_rss;   // File RSS pages
	unsigned long shmem_rss;  // Shared memory RSS pages
	int oom_score_adj;        // OOM score adjustment
	long points;              // OOM badness points
	char comm[16];            // Process name
};

SEC("tracepoint/oom/mark_victim")
int handle_oom(struct trace_event_raw_mark_victim *ctx) {
	// Reserve space in ring buffer
	struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		return 0;  // Ring buffer full - drop event (backpressure)
	}

	// Capture timestamp and event type
	evt->timestamp_ns = bpf_ktime_get_ns();
	evt->type = EVENT_TYPE_OOM_KILL;

	// Capture PID/TID from tracepoint
	evt->pid = ctx->pid;
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->tid = pid_tgid & 0xFFFFFFFF;

	// OOM kills always have exit code 137 (128 + SIGKILL=9)
	evt->exit_code = 137;
	evt->signal = 9;  // SIGKILL

	// Calculate memory usage from RSS pages (page size = 4096)
	// anon_rss + file_rss + shmem_rss = total RSS
	evt->memory_usage = (((__u64)ctx->anon_rss + (__u64)ctx->file_rss +
	                      (__u64)ctx->shmem_rss) * 4096);

	// Memory limit: not directly available in tracepoint
	// Will be enriched by userspace from cgroupfs (if still available)
	evt->memory_limit = 0;

	// Get current task for cgroup path
	// Brendan Gregg: Capture cgroup path NOW before cgroup is deleted
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();

	// Try to read cgroup path from task->cgroups
	// This may fail if the task is already being cleaned up
	// Use bpf_probe_read_kernel for safety
	evt->cgroup_path[0] = '\0';  // Initialize to empty

	// Note: Getting full cgroup path in eBPF is complex
	// We capture the cgroup ID and let userspace resolve the path
	// For now, leave empty - userspace will use PID->cgroup mapping

	bpf_ringbuf_submit(evt, 0);
	return 0;
}

// ═══════════════════════════════════════════════════════════
// HOOK 2: Process Exit Detection
// ═══════════════════════════════════════════════════════════
// Tracepoint: sched/sched_process_exit
// Triggered when any process exits
// We capture the exit code and signal here
// ═══════════════════════════════════════════════════════════

// Tracepoint context for sched/sched_process_exit
// Format: cat /sys/kernel/debug/tracing/events/sched/sched_process_exit/format
struct trace_event_raw_sched_process_exit {
	__u64 __unused;           // Common tracepoint header
	char comm[16];            // Process name
	int pid;                  // Process ID
	int prio;                 // Priority
};

SEC("tracepoint/sched/sched_process_exit")
int handle_exit(struct trace_event_raw_sched_process_exit *ctx) {
	// Get current task for exit code
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();
	if (!task) {
		return 0;
	}

	// Reserve space in ring buffer
	struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		return 0;
	}

	// Capture timestamp and event type
	evt->timestamp_ns = bpf_ktime_get_ns();
	evt->type = EVENT_TYPE_EXIT;

	// Capture PID/TID
	evt->pid = ctx->pid;
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->tid = pid_tgid & 0xFFFFFFFF;

	// Read exit_code from task_struct
	// exit_code format: (exit_code << 8) | signal
	__u32 exit_code = 0;
	bpf_probe_read_kernel(&exit_code, sizeof(exit_code), &task->exit_code);

	evt->exit_code = exit_code >> 8;          // Upper byte is exit code
	evt->signal = exit_code & 0x7F;           // Lower 7 bits is signal

	// Memory info not available for regular exits
	// Userspace will enrich from cgroup monitor
	evt->memory_usage = 0;
	evt->memory_limit = 0;

	// Cgroup path - leave for userspace to resolve
	evt->cgroup_path[0] = '\0';

	bpf_ringbuf_submit(evt, 0);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
