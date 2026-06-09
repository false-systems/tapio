// SPDX-License-Identifier: GPL-2.0

#include "headers/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "headers/metrics.h"
#include "headers/config.h"

// Operation types
#define OP_READ  0
#define OP_WRITE 1

// Severity levels (anomaly detection)
#define SEVERITY_NORMAL   0
#define SEVERITY_WARNING  1
#define SEVERITY_CRITICAL 2

// Storage event structure - MUST match Rust StorageEvent in tapio-common/src/ebpf.rs (80 bytes)
struct storage_event {
	__u32 config_generation; // offset 0
	__u32 _pad0;             // offset 4, explicit alignment padding
	__u64 timestamp_ns;      // offset 8
	__u64 latency_ns;        // offset 16
	__u64 cgroup_id;         // offset 24
	__u64 sector;            // offset 32
	__u32 dev_major;         // offset 40
	__u32 dev_minor;         // offset 44
	__u32 bytes;             // offset 48
	__u32 pid;               // offset 52
	__s32 error_code;        // offset 56
	__u8  opcode;            // offset 60
	__u8  severity;          // offset 61
	__u8  comm[16];          // offset 62
	__u8  _pad[2];           // offset 78, explicit padding to 80 bytes
};

_Static_assert(sizeof(struct storage_event) == 80, "storage_event size");
_Static_assert(__builtin_offsetof(struct storage_event, config_generation) == 0, "storage_event config_generation offset");

// Key for tracking inflight I/O operations
struct io_key {
	__u32 dev_major;
	__u32 dev_minor;
	__u64 sector;
	__u32 nr_sector;
	__u8  opcode;
	__u8  padding[3];
};

// Value for inflight I/O operations
struct io_value {
	__u64 issue_ns;       // Timestamp when I/O was issued
	__u64 cgroup_id;      // Cgroup ID (captured at issue time)
	__u32 bytes;          // I/O size
	__u32 issuer_pid;     // PID that issued the I/O
	__u8  opcode;         // READ or WRITE
	__u8  ambiguous;      // Same key observed concurrently; do not emit latency
	__u8  padding[6];     // Alignment
};

// Ring buffer for sending events to userspace
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 512 * 1024);  // 512KB ring buffer
} events SEC(".maps");

// LRU map for tracking inflight I/O (auto-evicts old entries)
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 10000);  // Track up to 10k inflight I/O ops
	__type(key, struct io_key);
	__type(value, struct io_value);
} inflight_io SEC(".maps");

// block_rq_issue tracepoint - when I/O request is issued to device
// Store timestamp for latency calculation
SEC("tracepoint/block/block_rq_issue")
int trace_block_rq_issue(struct trace_event_raw_block_rq *ctx) {
	struct tapio_config cfg = {};
	if (!tapio_config_snapshot(&cfg) || !(cfg.flags & TAPIO_F_STORAGE)) {
		return 0;
	}

	struct io_key key = {};
	struct io_value val = {};

	// Extract device info
	key.dev_major = BPF_CORE_READ(ctx, dev) >> 20;
	key.dev_minor = BPF_CORE_READ(ctx, dev) & ((1U << 20) - 1);
	key.sector = BPF_CORE_READ(ctx, sector);
	key.nr_sector = BPF_CORE_READ(ctx, nr_sector);

	// Capture issue time and context
	val.issue_ns = bpf_ktime_get_ns();
	val.cgroup_id = bpf_get_current_cgroup_id();
	val.bytes = key.nr_sector * 512;  // sectors → bytes
	val.issuer_pid = bpf_get_current_pid_tgid() >> 32;

	// Determine operation type from rwbs field (CO-RE safe)
	// rwbs is a char array: R=read, W=write, D=discard, F=flush, etc.
	char rwbs[8] = {};
	bpf_core_read_str(rwbs, sizeof(rwbs), &ctx->rwbs);
	if (rwbs[0] == 'R') {
		val.opcode = OP_READ;
	} else if (rwbs[0] == 'W') {
		val.opcode = OP_WRITE;
	} else {
		return 0;  // Skip discards, flushes, etc.
	}
	key.opcode = val.opcode;

	/* The block tracepoints do not expose a request pointer. If the same
	 * device/sector/size/op is issued concurrently, correlation is ambiguous.
	 * Mark the key and drop on completion rather than emitting wrong latency.
	 */
	if (bpf_map_update_elem(&inflight_io, &key, &val, BPF_NOEXIST) != 0) {
		struct io_value *existing = bpf_map_lookup_elem(&inflight_io, &key);
		if (existing) {
			existing->ambiguous = 1;
		}
	}

	return 0;
}

// block_rq_complete tracepoint - when I/O request completes
// Calculate latency and emit event if anomaly detected
SEC("tracepoint/block/block_rq_complete")
int trace_block_rq_complete(struct trace_event_raw_block_rq_completion *ctx) {
	struct io_key key = {};
	struct io_value *val;
	__u64 now_ns;
	__u64 latency_ns;
	__s32 error_code;
	__u8 severity;

	// Extract device info
	key.dev_major = BPF_CORE_READ(ctx, dev) >> 20;
	key.dev_minor = BPF_CORE_READ(ctx, dev) & ((1U << 20) - 1);
	key.sector = BPF_CORE_READ(ctx, sector);
	key.nr_sector = BPF_CORE_READ(ctx, nr_sector);

	char rwbs[8] = {};
	bpf_core_read_str(rwbs, sizeof(rwbs), &ctx->rwbs);
	if (rwbs[0] == 'R') {
		key.opcode = OP_READ;
	} else if (rwbs[0] == 'W') {
		key.opcode = OP_WRITE;
	} else {
		return 0;
	}

	// Lookup inflight I/O
	val = bpf_map_lookup_elem(&inflight_io, &key);
	if (!val) {
		return 0;  // No matching issue event (may have been evicted)
	}

	// Config gates emission, not cleanup — completions must clear inflight
	// state even when storage is disabled mid-flight, or stale entries
	// accumulate in the LRU and corrupt latency math after re-enable.
	struct tapio_config cfg = {};
	if (!tapio_config_snapshot(&cfg) || !(cfg.flags & TAPIO_F_STORAGE)) {
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;
	}

	if (val->ambiguous) {
		metric_inc(METRIC_STORAGE_AMBIGUOUS_IO);
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;
	}

	// Calculate latency
	now_ns = bpf_ktime_get_ns();
	latency_ns = now_ns - val->issue_ns;

	// Get error code
	error_code = BPF_CORE_READ(ctx, error);

	// Determine severity - only emit events for anomalies.
	// Zero thresholds are inert and do not emit latency events.
	if (error_code != 0) {
		severity = SEVERITY_CRITICAL;  // I/O error is always critical
	} else if (cfg.io_latency_critical_ns > 0 && latency_ns >= cfg.io_latency_critical_ns) {
		severity = SEVERITY_CRITICAL;
	} else if (cfg.slow_io_threshold_ns > 0 && latency_ns >= cfg.slow_io_threshold_ns) {
		severity = SEVERITY_WARNING;
	} else {
		// Normal I/O - don't emit event (edge filtering ~1%)
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;
	}

	// Reserve ring buffer space
	struct storage_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		metric_inc(METRIC_LOST_EVENTS);
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;
	}

	// Zero-init before filling — prevents leaking kernel stack via padding bytes
	__builtin_memset(evt, 0, sizeof(*evt));

	// Fill event
	evt->config_generation = cfg.generation;
	evt->timestamp_ns = now_ns;
	evt->latency_ns = latency_ns;
	evt->cgroup_id = val->cgroup_id;
	evt->sector = key.sector;
	evt->dev_major = key.dev_major;
	evt->dev_minor = key.dev_minor;
	evt->bytes = val->bytes;
	evt->pid = val->issuer_pid;
	evt->error_code = error_code;
	evt->opcode = val->opcode;
	evt->severity = severity;
	bpf_get_current_comm(evt->comm, sizeof(evt->comm));

	// Submit event
	bpf_ringbuf_submit(evt, 0);

	// Cleanup inflight map
	bpf_map_delete_elem(&inflight_io, &key);

	return 0;
}

char LICENSE[] SEC("license") = "GPL";
