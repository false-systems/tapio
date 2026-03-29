// SPDX-License-Identifier: GPL-2.0 OR BSD-3-Clause
// Minimal vmlinux.h for Tapio observers
// Shared across all eBPF programs for consistency

#ifndef __VMLINUX_MINIMAL_H__
#define __VMLINUX_MINIMAL_H__

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Waddress-of-packed-member"
#pragma clang diagnostic ignored "-Wgnu-variable-sized-type-not-at-end"

// Basic types
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;

typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

typedef __u8 u8;
typedef __u16 u16;
typedef __u32 u32;
typedef __u64 u64;

typedef __s8 s8;
typedef __s16 s16;
typedef __s32 s32;
typedef __s64 s64;

// Network byte order types
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u32 __wsum;

// Common kernel types
typedef unsigned int __kernel_size_t;
typedef int __kernel_ssize_t;
typedef long long __kernel_loff_t;
typedef long __kernel_time_t;
typedef long __kernel_suseconds_t;
typedef int __kernel_pid_t;
typedef unsigned int __kernel_uid32_t;
typedef unsigned int __kernel_gid32_t;

// Socket address families
#define AF_UNSPEC 0
#define AF_INET 2
#define AF_INET6 10

// TCP states (from include/net/tcp_states.h)
enum {
    TCP_ESTABLISHED = 1,
    TCP_SYN_SENT,
    TCP_SYN_RECV,
    TCP_FIN_WAIT1,
    TCP_FIN_WAIT2,
    TCP_TIME_WAIT,
    TCP_CLOSE,
    TCP_CLOSE_WAIT,
    TCP_LAST_ACK,
    TCP_LISTEN,
    TCP_CLOSING,
    TCP_NEW_SYN_RECV,
    TCP_MAX_STATES
};

// IP protocols
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// Task comm size
#define TASK_COMM_LEN 16

// IPv6 address
struct in6_addr {
    union {
        __u8 u6_addr8[16];
        __u16 u6_addr16[8];
        __u32 u6_addr32[4];
    } in6_u;
};

// Socket address structures (minimal)
struct sockaddr {
    unsigned short sa_family;
    char sa_data[14];
};

// BPF helper functions are provided by <bpf/bpf_helpers.h>
// Do NOT define them here - causes conflicts with libbpf headers

// BPF map types
enum bpf_map_type {
    BPF_MAP_TYPE_UNSPEC = 0,
    BPF_MAP_TYPE_HASH,
    BPF_MAP_TYPE_ARRAY,
    BPF_MAP_TYPE_PROG_ARRAY,
    BPF_MAP_TYPE_PERF_EVENT_ARRAY,
    BPF_MAP_TYPE_PERCPU_HASH,
    BPF_MAP_TYPE_PERCPU_ARRAY,
    BPF_MAP_TYPE_STACK_TRACE,
    BPF_MAP_TYPE_CGROUP_ARRAY,
    BPF_MAP_TYPE_LRU_HASH,
    BPF_MAP_TYPE_LRU_PERCPU_HASH,
    BPF_MAP_TYPE_LPM_TRIE,
    BPF_MAP_TYPE_ARRAY_OF_MAPS,
    BPF_MAP_TYPE_HASH_OF_MAPS,
    BPF_MAP_TYPE_DEVMAP,
    BPF_MAP_TYPE_SOCKMAP,
    BPF_MAP_TYPE_CPUMAP,
    BPF_MAP_TYPE_XSKMAP,
    BPF_MAP_TYPE_SOCKHASH,
    BPF_MAP_TYPE_CGROUP_STORAGE,
    BPF_MAP_TYPE_REUSEPORT_SOCKARRAY,
    BPF_MAP_TYPE_PERCPU_CGROUP_STORAGE,
    BPF_MAP_TYPE_QUEUE,
    BPF_MAP_TYPE_STACK,
    BPF_MAP_TYPE_SK_STORAGE,
    BPF_MAP_TYPE_DEVMAP_HASH,
    BPF_MAP_TYPE_STRUCT_OPS,
    BPF_MAP_TYPE_RINGBUF,
};

// BPF flags (provided by bpf_helpers.h in modern libbpf)
// Only define if not already defined
#ifndef BPF_ANY
enum {
    BPF_NOEXIST = 1,
    BPF_EXIST = 2,
    BPF_ANY = 0,
};
#endif

// Pin types are provided by <bpf/bpf_helpers.h>
// Do NOT define them here - causes conflicts

// Helper macros for map definitions
#ifndef __uint
#define __uint(name, val) int (*name)[val]
#endif
#ifndef __type
#define __type(name, val) typeof(val) *name
#endif

// Section macro for BPF programs
#ifndef SEC
#define SEC(name) __attribute__((section(name), used))
#endif

// License macro
#ifndef __LICENSE
#define __LICENSE(x) SEC("license") const char LICENSE[] = (x)
#endif

// Device ID type (if not defined)
#ifndef dev_t
typedef __u32 dev_t;
#endif

// Sector type (if not defined)
#ifndef sector_t
typedef __u64 sector_t;
#endif

// Task structure (minimal CO-RE definition for container observer)
// Only the fields we access — exit_code for process exit tracking
struct task_struct {
    int exit_code;
} __attribute__((preserve_access_index));

// Block I/O tracepoint structures (for storage observer)
// CO-RE compatible with preserve_access_index - field offsets resolved at runtime via BTF
// Reference: Brendan Gregg's BPF Performance Tools, Chapter 2

// Base block request tracepoint (block_rq_issue, block_rq_insert)
// Fields defined for CO-RE struct layout - we compute bytes from nr_sector*512
// and use bpf_get_current_comm() instead of comm field for reliability
struct trace_event_raw_block_rq {
	__u64 __unused;           // Common trace fields (skipped)
	dev_t dev;                // Device ID (major << 20 | minor)
	sector_t sector;          // Starting sector
	unsigned int nr_sector;   // Number of sectors
	unsigned int bytes;       // (unused - compute from nr_sector*512)
	char rwbs[8];             // R=read, W=write, D=discard, etc.
	char comm[TASK_COMM_LEN]; // (unused - use bpf_get_current_comm)
} __attribute__((preserve_access_index));

// Block request completion tracepoint (block_rq_complete)
// comm field included for struct layout but we use bpf_get_current_comm()
struct trace_event_raw_block_rq_completion {
	__u64 __unused;           // Common trace fields (skipped)
	dev_t dev;                // Device ID
	sector_t sector;          // Starting sector
	unsigned int nr_sector;   // Number of sectors
	int error;                // Error code (0 = success)
	char rwbs[8];             // R=read, W=write, D=discard, etc.
	char comm[TASK_COMM_LEN]; // (unused - use bpf_get_current_comm)
} __attribute__((preserve_access_index));

#pragma clang diagnostic pop

#endif // __VMLINUX_MINIMAL_H__
