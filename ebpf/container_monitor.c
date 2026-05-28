// SPDX-License-Identifier: GPL-2.0

// Container Observer eBPF Program
// Captures OOM kills and process exits with cgroup context
// Following Brendan Gregg's principle: "eBPF captures, Rust parses"

#include "headers/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "headers/metrics.h"

// Event types (MUST match Rust ContainerEvent.event_type in tapio-common/src/ebpf.rs)
#define EVENT_TYPE_OOM_KILL 0
#define EVENT_TYPE_EXIT     1

// Container event structure
// MUST match Rust ContainerEvent in tapio-common/src/ebpf.rs (52 bytes packed)
// Field order optimized for alignment (u64 first)
struct container_event {
	__u64 memory_limit;          // offset 0: Memory limit from cgroup
	__u64 memory_usage;          // offset 8: Memory usage from cgroup
	__u64 timestamp_ns;          // offset 16: Event timestamp in nanoseconds
	__u64 cgroup_id;             // offset 24: Cgroup ID — userspace derives K8s pod context from this ID
	__u32 type;                  // offset 32: EVENT_TYPE_OOM_KILL or EVENT_TYPE_EXIT
	__u32 pid;                   // offset 36: Process ID
	__u32 tid;                   // offset 40: Thread ID
	__s32 exit_code;             // offset 44: Exit code
	__s32 signal;                // offset 48: Signal number
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

/* Tracepoint argument structs: hardcoded layout is intentional.
 * Tracepoint ABIs are stable across kernel versions (unlike internal structs).
 * CO-RE is used for task_struct field access where offsets vary. */

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
		metric_inc(METRIC_LOST_EVENTS);
		return 0;
	}

	// Capture timestamp and event type
	evt->timestamp_ns = bpf_ktime_get_ns();
	evt->type = EVENT_TYPE_OOM_KILL;

	// ctx->pid is the VICTIM's PID (from tracepoint args).
	// bpf_get_current_pid_tgid() returns the OOM KILLER's context, not the victim's.
	// We emit the victim PID; userspace enriches via /proc/<pid>/cgroup.
	evt->pid = ctx->pid;
	evt->tid = 0;  // victim TID not available from this tracepoint

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

	// cgroup_id from bpf_get_current_cgroup_id() is the OOM KILLER's cgroup,
	// not the victim's. Set to 0 so userspace knows to enrich via victim PID instead.
	evt->cgroup_id = 0;

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

	// Read exit_code from task_struct (CO-RE: field offset resolved at load time via BTF)
	// exit_code format: (exit_code << 8) | signal
	__u32 exit_code = 0;
	if (bpf_core_read(&exit_code, sizeof(exit_code), &task->exit_code) != 0)
		return 0;

	__s32 code = exit_code >> 8;
	__s32 sig = exit_code & 0x7F;

	/* BPF-side filter: only emit abnormal exits (non-zero exit code or signal) */
	if (code == 0 && sig == 0) {
		return 0;
	}

	// Reserve space in ring buffer
	struct container_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		metric_inc(METRIC_LOST_EVENTS);
		return 0;
	}

	// Capture timestamp and event type
	evt->timestamp_ns = bpf_ktime_get_ns();
	evt->type = EVENT_TYPE_EXIT;

	// Capture PID/TID
	evt->pid = ctx->pid;
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->tid = pid_tgid & 0xFFFFFFFF;

	evt->exit_code = code;
	evt->signal = sig;

	// Memory info not available for regular exits
	// Userspace may enrich from cgroupfs if still available
	evt->memory_usage = 0;
	evt->memory_limit = 0;

	// Capture cgroup ID - survives cgroup deletion (Issue #566)
	// K8s pod context derived in Rust userspace using this ID
	evt->cgroup_id = bpf_get_current_cgroup_id();

	bpf_ringbuf_submit(evt, 0);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
