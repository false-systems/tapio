/* SPDX-License-Identifier: GPL-2.0 */
/* Minimal vmlinux.h for Tapio network observer */

#ifndef __VMLINUX_H__
#define __VMLINUX_H__

typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;

typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

// BPF map types
enum bpf_map_type {
	BPF_MAP_TYPE_RINGBUF = 27,
};

// Helper macro for map definitions
#define __uint(name, val) int (*name)[val]
#define __type(name, val) typeof(val) *name

// Section macro
#define SEC(name) __attribute__((section(name), used))

#endif /* __VMLINUX_H__ */
