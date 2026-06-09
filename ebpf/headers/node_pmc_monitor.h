/* SPDX-License-Identifier: GPL-2.0 */
/* Node PMC Monitor - Shared header between eBPF and Rust */

#ifndef __NODE_PMC_MONITOR_H
#define __NODE_PMC_MONITOR_H

/* PMC event sent from eBPF to userspace via ring buffer */
struct pmc_event {
	__u32 config_generation; /* Config generation that judged this event */
	__u32 cpu;            /* CPU core ID */
	__u64 cycles;         /* CPU cycles (cumulative) */
	__u64 instructions;   /* Instructions retired (cumulative) */
	__u64 stall_cycles;   /* Memory stall cycles (cumulative) */
	__u64 timestamp;      /* Timestamp in nanoseconds */
} __attribute__((packed));

_Static_assert(sizeof(struct pmc_event) == 40, "pmc_event size");
_Static_assert(__builtin_offsetof(struct pmc_event, config_generation) == 0, "pmc_event config_generation offset");

#endif /* __NODE_PMC_MONITOR_H */
