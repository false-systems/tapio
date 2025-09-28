#ifndef __CORE_HELPERS_H__
#define __CORE_HELPERS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Task command length
#ifndef TASK_COMM_LEN
#define TASK_COMM_LEN 16
#endif

// Ring buffer sizes
#define RINGBUF_SIZE_SMALL   (64 * 1024)
#define RINGBUF_SIZE_MEDIUM  (256 * 1024)
#define RINGBUF_SIZE_LARGE   (1024 * 1024)

// Rate limiter structure
struct rate_limiter {
    __u64 tokens;
    __u64 last_refill_ns;
    __u32 max_per_sec;
    __u32 pad;
};

// Common network structures
struct ipv4_tuple {
    __u32 saddr;
    __u32 daddr;
    __u16 sport;
    __u16 dport;
    __u8 protocol;
    __u8 pad[3];
};

struct ipv6_tuple {
    __u8 saddr[16];
    __u8 daddr[16];
    __u16 sport;
    __u16 dport;
    __u8 protocol;
    __u8 pad[3];
};

// Helper macros for safe memory access
#define BPF_CORE_READ_STR_INTO(dst, src, member) \
    bpf_core_read_str(dst, sizeof(dst), &((src)->member))

#define BPF_CORE_READ_INTO(dst, src, member) \
    bpf_core_read(dst, sizeof(*dst), &((src)->member))

// Process information helpers
static __always_inline __u32 get_current_pid_tgid_high(void) {
    return (__u32)(bpf_get_current_pid_tgid() >> 32);
}

static __always_inline __u32 get_current_pid_tgid_low(void) {
    return (__u32)(bpf_get_current_pid_tgid() & 0xFFFFFFFF);
}

// IP address formatting helpers
static __always_inline void format_ipv4(__u32 ip, __u8 *buf) {
    buf[0] = ip & 0xFF;
    buf[1] = (ip >> 8) & 0xFF;
    buf[2] = (ip >> 16) & 0xFF;
    buf[3] = (ip >> 24) & 0xFF;
    // Clear IPv6 part
    #pragma unroll
    for (int i = 4; i < 16; i++) {
        buf[i] = 0;
    }
}

static __always_inline void format_ipv6(__u8 *src, __u8 *dst) {
    #pragma unroll
    for (int i = 0; i < 16; i++) {
        dst[i] = src[i];
    }
}

// String helpers
static __always_inline int safe_strlen(const char *str, int max_len) {
    #pragma unroll
    for (int i = 0; i < max_len; i++) {
        if (str[i] == 0) {
            return i;
        }
    }
    return max_len;
}

static __always_inline void safe_strcpy(char *dst, const char *src, int max_len) {
    #pragma unroll
    for (int i = 0; i < max_len - 1; i++) {
        dst[i] = src[i];
        if (src[i] == 0) {
            return;
        }
    }
    dst[max_len - 1] = 0;
}

#endif /* __CORE_HELPERS_H__ */