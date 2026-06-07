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

// Cross-observer metrics (shared index space)
#define METRIC_LOST_EVENTS                0  // Ring buffer reserve failures (events dropped)

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

#endif /* __TAPIO_METRICS_H__ */
