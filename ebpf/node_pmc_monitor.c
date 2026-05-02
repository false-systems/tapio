// SPDX-License-Identifier: GPL-2.0
// Node PMC Monitor - eBPF program for Performance Monitoring Counters
// Based on Brendan Gregg's "CPU Utilization is Wrong" research

#include "headers/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include "headers/node_pmc_monitor.h"
#include "headers/metrics.h"

char LICENSE[] SEC("license") = "GPL";

// bpf_perf_event_read_value output struct (not in vmlinux_minimal.h)
struct bpf_perf_event_value {
	__u64 counter;
	__u64 enabled;  // time counter was enabled (ns)
	__u64 running;  // time counter was actually running (ns)
};

// Ring buffer for sending PMC events to userspace
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024); // 256KB ring buffer
} events SEC(".maps");

// Perf event array for reading PMC counters
// Populated by userspace with perf_event file descriptors
struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} pmc_cycles SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} pmc_instructions SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} pmc_stalls SEC(".maps");

// Read a PMC counter using bpf_perf_event_read_value (preferred over
// the deprecated bpf_perf_event_read). Returns the counter value,
// or -1 on error.
static __always_inline __s64 read_pmc(void *map, __u32 cpu) {
	struct bpf_perf_event_value val = {};
	if (bpf_perf_event_read_value(map, cpu, &val, sizeof(val)) != 0) {
		return -1;
	}
	return (__s64)val.counter;
}

// eBPF program attached to perf_event
// Fires on timer (e.g., every 100ms) to sample PMC counters
SEC("perf_event")
int sample_pmc(struct bpf_perf_event_data *ctx)
{
	struct pmc_event *event;
	__u32 cpu = bpf_get_smp_processor_id();
	__u64 timestamp = bpf_ktime_get_ns();

	// Reserve space in ring buffer
	event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
	if (!event) {
		metric_inc(METRIC_LOST_EVENTS);
		return 0;
	}
	__builtin_memset(event, 0, sizeof(*event));

	// Read PMC counter values via bpf_perf_event_read_value
	__s64 cycles = read_pmc(&pmc_cycles, cpu);
	__s64 instructions = read_pmc(&pmc_instructions, cpu);
	__s64 stalls = read_pmc(&pmc_stalls, cpu);

	// Check for errors (negative = read failed)
	if (cycles < 0 || instructions < 0) {
		bpf_ringbuf_discard(event, 0);
		return 0;
	}

	// Stalls might not be available on all CPUs - use 0 if error
	if (stalls < 0) {
		stalls = 0;
	}

	// Fill event structure
	event->cpu = cpu;
	event->cycles = (__u64)cycles;
	event->instructions = (__u64)instructions;
	event->stall_cycles = (__u64)stalls;
	event->timestamp = timestamp;

	// Submit event to userspace
	bpf_ringbuf_submit(event, 0);

	return 0;
}
