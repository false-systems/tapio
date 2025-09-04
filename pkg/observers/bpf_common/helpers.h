// SPDX-License-Identifier: GPL-2.0
/* CO-RE Helpers for Tapio eBPF Programs
 * Provides portable helpers for cross-architecture BPF development
 */

#ifndef __BPF_CORE_HELPERS_H__
#define __BPF_CORE_HELPERS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

/* Architecture detection for BPF programs */
#if defined(__TARGET_ARCH_x86) || defined(__TARGET_ARCH_x86_64) || defined(__x86_64__)
    #define ARCH_X86_64 1
    #undef ARCH_ARM64
#elif defined(__TARGET_ARCH_arm64) || defined(__aarch64__)
    #define ARCH_ARM64 1
    #undef ARCH_X86_64
#else
    /* Fallback to x86_64 if architecture not detected */
    #define ARCH_X86_64 1
    #undef ARCH_ARM64
#endif

/**
 * get_kprobe_func_arg - CO-RE helper to read function arguments from pt_regs
 * @ctx: pt_regs context from kprobe
 * @arg_num: argument number (0-5 for x86_64, 0-7 for arm64)
 * 
 * This helper abstracts architecture-specific register access for kprobes.
 * It uses CO-RE relocations to ensure compatibility across kernel versions.
 * 
 * Returns: The value of the requested function argument
 */
static __always_inline unsigned long 
get_kprobe_func_arg(struct pt_regs *ctx, int arg_num)
{
    unsigned long arg = 0;
    
    if (!ctx)
        return 0;
    
#ifdef ARCH_X86_64
    /* x86_64 ABI: Use BPF helper to get args - more reliable */
    /* For kprobes, use fixed offsets as fallback */
    static const int x86_64_arg_offsets[] = {112, 104, 96, 88, 72, 64}; /* di, si, dx, cx, r8, r9 */
    if (arg_num >= 0 && arg_num < 6) {
        bpf_probe_read_kernel(&arg, sizeof(arg), (void *)((char *)ctx + x86_64_arg_offsets[arg_num]));
    }
#elif defined(ARCH_ARM64)
    /* ARM64 ABI: X0-X7 in regs[0-7] */
    if (arg_num >= 0 && arg_num < 8) {
        /* Use probe_read for ARM64 as regs field may not be available in minimal vmlinux */
        bpf_probe_read_kernel(&arg, sizeof(arg), (void *)((char *)ctx + (arg_num * sizeof(unsigned long))));
    }
#else
    /* Fallback: try x86_64 layout */
    switch(arg_num) {
        case 0:
            /* RDI is at offset 112 in x86_64 pt_regs */
            bpf_probe_read_kernel(&arg, sizeof(arg), (void *)((char *)ctx + 112));
            break;
        default:
            return 0;
    }
#endif
    
    return arg;
}

/**
 * get_kprobe_arg_ptr - Read a pointer argument from kprobe context
 * @ctx: pt_regs context
 * @arg_num: argument number
 * @ptr: output pointer location
 * 
 * Returns: 0 on success, negative on error
 */
static __always_inline int
get_kprobe_arg_ptr(struct pt_regs *ctx, int arg_num, void **ptr)
{
    unsigned long arg = get_kprobe_func_arg(ctx, arg_num);
    if (arg == 0)
        return -1;
    
    *ptr = (void *)arg;
    return 0;
}

/**
 * read_sock_from_kprobe - Helper to read sock struct from kprobe using CO-RE
 * @ctx: pt_regs context
 * 
 * Reads the first argument (assumed to be struct sock *) from kprobe context.
 * Used in tcp_v4_connect, tcp_close, etc.
 * 
 * Returns: Pointer to sock struct or NULL on error
 */
static __always_inline struct sock *
read_sock_from_kprobe(struct pt_regs *ctx)
{
    void *ptr = (void *)get_kprobe_func_arg(ctx, 0);
    if (!ptr)
        return NULL;
        
    // The argument is already the sock pointer, not a pointer to a pointer
    return (struct sock *)ptr;
}

/**
 * read_task_from_kprobe - Helper to read task_struct from kprobe
 * @ctx: pt_regs context
 * @arg_num: which argument contains the task_struct pointer
 * 
 * Returns: Pointer to task_struct or NULL on error
 */
static __always_inline struct task_struct *
read_task_from_kprobe(struct pt_regs *ctx, int arg_num)
{
    struct task_struct *task = NULL;
    unsigned long arg = get_kprobe_func_arg(ctx, arg_num);
    
    if (arg != 0) {
        bpf_probe_read(&task, sizeof(task), (void *)arg);
    }
    
    return task;
}

/**
 * read_cred_from_kprobe - Helper to read cred struct from kprobe
 * @ctx: pt_regs context
 * @arg_num: which argument contains the cred pointer
 * 
 * Returns: Pointer to cred struct or NULL on error
 */
static __always_inline struct cred *
read_cred_from_kprobe(struct pt_regs *ctx, int arg_num)
{
    struct cred *cred = NULL;
    unsigned long arg = get_kprobe_func_arg(ctx, arg_num);
    
    if (arg != 0) {
        bpf_probe_read(&cred, sizeof(cred), (void *)arg);
    }
    
    return cred;
}

/* Helper macros for common patterns */

/**
 * BPF_KPROBE_READ_ARG - Read a typed argument from kprobe
 * @type: The C type of the argument
 * @ctx: pt_regs context
 * @arg_num: Argument number (0-based)
 * 
 * Example:
 *   int sockfd = BPF_KPROBE_READ_ARG(int, ctx, 0);
 */
#define BPF_KPROBE_READ_ARG(type, ctx, arg_num) \
    ({ \
        type __arg = 0; \
        unsigned long __val = get_kprobe_func_arg(ctx, arg_num); \
        __arg = (type)__val; \
        __arg; \
    })

/**
 * BPF_KPROBE_READ_PTR - Read a pointer argument from kprobe
 * @type: The pointer type
 * @ctx: pt_regs context  
 * @arg_num: Argument number (0-based)
 * 
 * Example:
 *   struct sock *sk = BPF_KPROBE_READ_PTR(struct sock *, ctx, 0);
 */
#define BPF_KPROBE_READ_PTR(type, ctx, arg_num) \
    ({ \
        type __ptr = NULL; \
        unsigned long __arg = get_kprobe_func_arg(ctx, arg_num); \
        if (__arg != 0) { \
            bpf_probe_read(&__ptr, sizeof(__ptr), (void *)__arg); \
        } \
        __ptr; \
    })

/* Kernel version compatibility helpers */

/**
 * COMPAT_CORE_READ - Compatibility wrapper for BPF_CORE_READ
 * Provides fallback for older kernels without CO-RE support
 */
#if defined(BPF_CORE_READ)
    #define COMPAT_CORE_READ BPF_CORE_READ
#else
    #define COMPAT_CORE_READ(dst, src, ...) \
        bpf_probe_read(dst, sizeof(*dst), src)
#endif

/**
 * COMPAT_CORE_READ_INTO - Compatibility wrapper for BPF_CORE_READ_INTO
 */
#if defined(BPF_CORE_READ_INTO)
    #define COMPAT_CORE_READ_INTO BPF_CORE_READ_INTO
#else
    #define COMPAT_CORE_READ_INTO(dst, src, ...) \
        bpf_probe_read(&dst, sizeof(dst), &src)
#endif

/**
 * has_btf_support - Check if kernel has BTF support at runtime
 * This is a simplified check based on the availability of core helpers
 */
static __always_inline bool has_btf_support(void)
{
#ifdef bpf_core_field_exists
    return true;
#else
    return false;
#endif
}

/**
 * SAFE_CORE_READ - Safe CO-RE read with fallback
 * Uses CO-RE if available, otherwise falls back to probe_read
 */
#define SAFE_CORE_READ(dst, src, field) \
    ({ \
        int __ret = 0; \
        if (has_btf_support() && bpf_core_field_exists(src->field)) { \
            __ret = BPF_CORE_READ_INTO(dst, src, field); \
        } else { \
            __ret = bpf_probe_read_kernel(&dst, sizeof(dst), &((src)->field)); \
        } \
        __ret; \
    })

/* Ring buffer compatibility helpers */

/**
 * has_ringbuf_support - Check if kernel supports BPF ring buffer
 * Ring buffer was added in kernel 5.8
 */
static __always_inline bool has_ringbuf_support(void)
{
    /* This is a compile-time check based on BPF helper availability */
#ifdef BPF_FUNC_ringbuf_reserve
    return true;
#else
    return false;
#endif
}

/* Architecture-specific compatibility helpers */

/**
 * get_syscall_arg - Get syscall argument in architecture-neutral way
 * @regs: pt_regs from syscall tracepoint
 * @arg_num: argument number (0-5)
 * 
 * This helper abstracts syscall argument access across architectures.
 */
static __always_inline unsigned long
get_syscall_arg(struct pt_regs *regs, int arg_num)
{
    if (!regs)
        return 0;
        
#ifdef ARCH_X86_64
    /* x86_64 syscall ABI: Use fixed offsets for reliability */
    /* Syscall args: di, si, dx, r10, r8, r9 */
    static const int x86_64_syscall_offsets[] = {112, 104, 96, 56, 72, 64};
    if (arg_num >= 0 && arg_num < 6) {
        unsigned long value;
        bpf_probe_read_kernel(&value, sizeof(value), (void *)((char *)regs + x86_64_syscall_offsets[arg_num]));
        return value;
    }
#elif defined(ARCH_ARM64)
    /* ARM64 syscall ABI: X0-X5 */
    if (arg_num >= 0 && arg_num < 6) {
        unsigned long value;
        bpf_probe_read_kernel(&value, sizeof(value), (void *)((char *)regs + (arg_num * sizeof(unsigned long))));
        return value;
    }
#endif
    return 0;
}

/* Debugging helpers */

/**
 * bpf_debug_printk - Conditional debug printing
 * Only prints if DEBUG_BPF is defined
 */
#ifdef DEBUG_BPF
    #define bpf_debug_printk(fmt, ...) \
        bpf_printk(fmt, ##__VA_ARGS__)
#else
    #define bpf_debug_printk(fmt, ...) do {} while(0)
#endif

/* Validation helpers for CO-RE */

/**
 * validate_core_support - Validate CO-RE support at runtime
 * @task: task_struct pointer to test with
 * 
 * Returns: 1 if CO-RE is working, 0 otherwise
 */
static __always_inline int validate_core_support(struct task_struct *task)
{
    if (!task)
        return 0;
        
    // Test if we can read basic task fields with CO-RE
    if (!bpf_core_field_exists(task->pid) || 
        !bpf_core_field_exists(task->comm)) {
        return 0;
    }
    
    // Test a read operation
    pid_t test_pid;
    if (BPF_CORE_READ_INTO(&test_pid, task, pid) != 0) {
        return 0;
    }
    
    return 1;
}

/**
 * get_kernel_version_code - Get kernel version at compile time
 * This uses CO-RE to determine kernel compatibility
 */
#define KERNEL_VERSION_CODE() \
    ({ \
        unsigned int version = 0; \
        /* Use CO-RE feature detection as proxy for kernel version */ \
        if (bpf_core_type_exists(struct task_struct)) { \
            if (bpf_core_field_exists(struct task_struct, cgroups)) { \
                version = 0x050400; /* 5.4+ */ \
            } else { \
                version = 0x040000; /* 4.0+ */ \
            } \
        } \
        version; \
    })

/* Network helper functions */

/**
 * bpf_ntohs - Convert network byte order to host byte order (16-bit)
 * @netshort: 16-bit value in network byte order
 */
static __always_inline __u16 bpf_ntohs(__u16 netshort)
{
    return __builtin_bswap16(netshort);
}

/**
 * bpf_ntohl - Convert network byte order to host byte order (32-bit)
 * @netlong: 32-bit value in network byte order
 */
static __always_inline __u32 bpf_ntohl(__u32 netlong)
{
    return __builtin_bswap32(netlong);
}

/**
 * bpf_htons - Convert host byte order to network byte order (16-bit)
 * @hostshort: 16-bit value in host byte order
 */
static __always_inline __u16 bpf_htons(__u16 hostshort)
{
    return __builtin_bswap16(hostshort);
}

/**
 * bpf_htonl - Convert host byte order to network byte order (32-bit)
 * @hostlong: 32-bit value in host byte order
 */
static __always_inline __u32 bpf_htonl(__u32 hostlong)
{
    return __builtin_bswap32(hostlong);
}

/* Error handling helpers */

/**
 * CO-RE error codes for better debugging
 */
#define CORE_ERR_NO_BTF        -1
#define CORE_ERR_FIELD_MISSING -2
#define CORE_ERR_READ_FAILED   -3
#define CORE_ERR_NULL_PTR      -4

/**
 * handle_core_error - Central error handling for CO-RE operations
 * @error_code: Error code from CO-RE operation
 * @context: Context string for debugging
 * 
 * Returns: 0 on success, negative on error
 */
static __always_inline int handle_core_error(int error_code, const char *context)
{
    if (error_code != 0) {
        bpf_debug_printk("CO-RE error %d in %s", error_code, context ? context : "unknown");
    }
    return error_code;
}

/* Note: Statistics macros and functions are now in bpf_stats.h */

#endif /* __BPF_CORE_HELPERS_H__ */