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

// Network Observer Metrics (10-99)
#define METRIC_NETWORK_EVENTS_TOTAL       10
#define METRIC_NETWORK_RETRANSMITS_TOTAL  11
#define METRIC_NETWORK_RTT_SAMPLES_TOTAL  12
#define METRIC_NETWORK_RST_TOTAL          13
#define METRIC_NETWORK_RTT_SPIKES_TOTAL   14
#define METRIC_NETWORK_BASELINE_REJECTED  15

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
