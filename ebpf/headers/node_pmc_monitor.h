/* SPDX-License-Identifier: GPL-2.0 */
/* Node PMC Monitor - Shared header between eBPF and Rust */

#ifndef __NODE_PMC_MONITOR_H
#define __NODE_PMC_MONITOR_H

/* PMC event sent from eBPF to userspace via ring buffer */
struct pmc_event {
	__u32 cpu;            /* CPU core ID */
	__u64 cycles;         /* CPU cycles (cumulative) */
	__u64 instructions;   /* Instructions retired (cumulative) */
	__u64 stall_cycles;   /* Memory stall cycles (cumulative) */
	__u64 timestamp;      /* Timestamp in nanoseconds */
} __attribute__((packed));

#endif /* __NODE_PMC_MONITOR_H */
