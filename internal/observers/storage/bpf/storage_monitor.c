//go:build ignore

#include "../../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Operation types
#define OP_READ  0
#define OP_WRITE 1

// Severity levels (anomaly detection)
#define SEVERITY_NORMAL   0
#define SEVERITY_WARNING  1
#define SEVERITY_CRITICAL 2

// Latency thresholds (nanoseconds)
#define LATENCY_WARNING_NS  50000000ULL   // 50ms
#define LATENCY_CRITICAL_NS 200000000ULL  // 200ms

// Storage event structure - MUST match Go StorageEventBPF exactly (72 bytes with alignment)
struct storage_event {
	__u64 timestamp_ns;   // offset 0
	__u64 latency_ns;     // offset 8
	__u64 cgroup_id;      // offset 16
	__u64 sector;         // offset 24
	__u32 dev_major;      // offset 32
	__u32 dev_minor;      // offset 36
	__u32 bytes;          // offset 40
	__u32 pid;            // offset 44
	__u16 error_code;     // offset 48
	__u8  opcode;         // offset 50
	__u8  severity;       // offset 51
	__u8  comm[16];       // offset 52
	// 4 bytes end padding to align struct to 8 bytes = 72 total
};

// Key for tracking inflight I/O operations
struct io_key {
	__u32 dev_major;
	__u32 dev_minor;
	__u64 sector;
};

// Value for inflight I/O operations
struct io_value {
	__u64 issue_ns;       // Timestamp when I/O was issued
	__u64 cgroup_id;      // Cgroup ID (captured at issue time)
	__u32 bytes;          // I/O size
	__u8  opcode;         // READ or WRITE
	__u8  padding[3];     // Alignment
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
	struct io_key key = {};
	struct io_value val = {};

	// Extract device info
	key.dev_major = BPF_CORE_READ(ctx, dev) >> 20;
	key.dev_minor = BPF_CORE_READ(ctx, dev) & ((1U << 20) - 1);
	key.sector = BPF_CORE_READ(ctx, sector);

	// Capture issue time and context
	val.issue_ns = bpf_ktime_get_ns();
	val.cgroup_id = bpf_get_current_cgroup_id();
	val.bytes = BPF_CORE_READ(ctx, nr_sector) * 512;  // sectors → bytes

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

	// Store in inflight map
	bpf_map_update_elem(&inflight_io, &key, &val, BPF_ANY);

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
	__u16 error_code;
	__u8 severity;

	// Extract device info
	key.dev_major = BPF_CORE_READ(ctx, dev) >> 20;
	key.dev_minor = BPF_CORE_READ(ctx, dev) & ((1U << 20) - 1);
	key.sector = BPF_CORE_READ(ctx, sector);

	// Lookup inflight I/O
	val = bpf_map_lookup_elem(&inflight_io, &key);
	if (!val) {
		return 0;  // No matching issue event (may have been evicted)
	}

	// Calculate latency
	now_ns = bpf_ktime_get_ns();
	latency_ns = now_ns - val->issue_ns;

	// Get error code
	error_code = BPF_CORE_READ(ctx, error);

	// Determine severity - only emit events for anomalies
	if (error_code != 0) {
		severity = SEVERITY_CRITICAL;  // I/O error is always critical
	} else if (latency_ns >= LATENCY_CRITICAL_NS) {
		severity = SEVERITY_CRITICAL;
	} else if (latency_ns >= LATENCY_WARNING_NS) {
		severity = SEVERITY_WARNING;
	} else {
		// Normal I/O - don't emit event (edge filtering ~1%)
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;
	}

	// Reserve ring buffer space
	struct storage_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		bpf_map_delete_elem(&inflight_io, &key);
		return 0;  // Backpressure - drop event
	}

	// Fill event
	evt->timestamp_ns = now_ns;
	evt->latency_ns = latency_ns;
	evt->cgroup_id = val->cgroup_id;
	evt->sector = key.sector;
	evt->dev_major = key.dev_major;
	evt->dev_minor = key.dev_minor;
	evt->bytes = val->bytes;
	evt->pid = bpf_get_current_pid_tgid() >> 32;
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
