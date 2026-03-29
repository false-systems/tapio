//go:build ignore

#ifndef __TAPIO_METRICS_H__
#define __TAPIO_METRICS_H__

// Shared Per-CPU metrics for all Tapio observers
// Lock-free counters - aggregate in userspace

// Based on Cilium's metrics pattern

// Per-CPU metrics map - shared across ALL observers
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__type(key, __u32);
	__type(value, __u64);
	__uint(max_entries, 512);  // 512 metric slots
} tapio_metrics SEC(".maps");

// ============================================================================
// Metric Index Definitions (Namespaced per Observer)
// ============================================================================

// Network Observer Metrics (0-99)
#define METRIC_NETWORK_EVENTS_TOTAL       0
#define METRIC_NETWORK_RETRANSMITS_TOTAL  1
#define METRIC_NETWORK_RTT_SAMPLES_TOTAL  2
#define METRIC_NETWORK_RST_TOTAL          3
#define METRIC_NETWORK_RTT_SPIKES_TOTAL   4
#define METRIC_NETWORK_BASELINE_REJECTED  5

// Scheduler Observer Metrics (100-199) - Reserved for future
#define METRIC_SCHEDULER_DECISIONS        100
#define METRIC_SCHEDULER_QUEUE_TIME       101

// DNS Observer Metrics (200-299) - Reserved for future
#define METRIC_DNS_QUERIES_TOTAL          200
#define METRIC_DNS_TIMEOUTS_TOTAL         201

// HTTP Observer Metrics (300-399) - Reserved for future
#define METRIC_HTTP_REQUESTS_TOTAL        300
#define METRIC_HTTP_ERRORS_TOTAL          301

// ============================================================================
// Helper Functions
// ============================================================================


// Increment Per-CPU metric (lock-free)
static __always_inline void metric_inc(__u32 metric_idx)
{
	__u64 *value = bpf_map_lookup_elem(&tapio_metrics, &metric_idx);
	if (value) {
		(*value)++;
	}
}

// Add to Per-CPU metric (no atomics needed - each CPU has own copy)
//
// Per-CPU maps (BPF_MAP_TYPE_PERCPU_ARRAY) allocate a separate value for EACH CPU.
// When CPU 0 calls this function, it writes only to CPU 0's copy.
// When CPU 1 calls this function, it writes only to CPU 1's copy.
// There is NO sharing between CPUs = NO race conditions = NO atomics needed!
//
// Userspace aggregates all per-CPU copies when reading the final metric value.
// This is the standard eBPF pattern for high-performance lock-free counters.
static __always_inline void metric_add(__u32 metric_idx, __u64 delta)
{
	__u64 *value = bpf_map_lookup_elem(&tapio_metrics, &metric_idx);
	if (value) {
		(*value) += delta;  // Safe: each CPU has its own isolated copy
	}
}

#endif /* __TAPIO_METRICS_H__ */
