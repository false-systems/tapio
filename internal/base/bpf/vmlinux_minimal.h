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

// BPF helper function declarations
static void *(*bpf_map_lookup_elem)(void *map, const void *key) = (void *) 1;
static int (*bpf_map_update_elem)(void *map, const void *key, const void *value, __u64 flags) = (void *) 2;
static int (*bpf_map_delete_elem)(void *map, const void *key) = (void *) 3;
static int (*bpf_probe_read)(void *dst, __u32 size, const void *unsafe_ptr) = (void *) 4;
static int (*bpf_probe_read_kernel)(void *dst, __u32 size, const void *unsafe_ptr) = (void *) 113;
static int (*bpf_probe_read_user)(void *dst, __u32 size, const void *unsafe_ptr) = (void *) 112;
static __u64 (*bpf_ktime_get_ns)(void) = (void *) 5;
static int (*bpf_trace_printk)(const char *fmt, __u32 fmt_size, ...) = (void *) 6;
static __u32 (*bpf_get_prandom_u32)(void) = (void *) 7;
static __u32 (*bpf_get_smp_processor_id)(void) = (void *) 8;
static int (*bpf_get_current_comm)(void *buf, __u32 size_of_buf) = (void *) 16;
static __u64 (*bpf_get_current_pid_tgid)(void) = (void *) 14;
static __u64 (*bpf_get_current_uid_gid)(void) = (void *) 15;
static long (*bpf_ringbuf_output)(void *ringbuf, void *data, __u64 size, __u64 flags) = (void *) 130;
static void *(*bpf_ringbuf_reserve)(void *ringbuf, __u64 size, __u64 flags) = (void *) 131;
static void (*bpf_ringbuf_submit)(void *data, __u64 flags) = (void *) 132;
static void (*bpf_ringbuf_discard)(void *data, __u64 flags) = (void *) 133;

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

// BPF flags
enum {
    BPF_NOEXIST = 1,
    BPF_EXIST = 2,
    BPF_ANY = 0,
};

// Pin types
#define LIBBPF_PIN_NONE         0
#define LIBBPF_PIN_BY_NAME      1

// Helper macros for map definitions
#define __uint(name, val) int (*name)[val]
#define __type(name, val) typeof(val) *name

// Section macro for BPF programs
#ifndef SEC
#define SEC(name) __attribute__((section(name), used))
#endif

// License macro
#ifndef __LICENSE
#define __LICENSE(x) SEC("license") const char LICENSE[] = (x)
#endif

#pragma clang diagnostic pop

#endif // __VMLINUX_MINIMAL_H__
