#ifndef __TAPIO_COMMON_H
#define __TAPIO_COMMON_H

// Basic type definitions
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef unsigned long long u64;

typedef signed char s8;
typedef signed short s16;
typedef signed int s32;
typedef signed long long s64;

typedef unsigned long size_t;
// bool is already defined in vmlinux.h, so we don't need to define it

#define true 1
#define false 0

// Type definitions that bpf_helpers.h expects
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u32 __wsum;

// Common constants for eBPF programs
#ifndef ETH_P_IP
#define ETH_P_IP 0x0800
#endif

#ifndef TC_ACT_OK
#define TC_ACT_OK 0
#endif

#ifndef UINT32_MAX
#define UINT32_MAX 0xffffffff
#endif

#ifndef BPF_F_CURRENT_CPU
#define BPF_F_CURRENT_CPU 0xffffffffULL
#endif

// BPF map types
#ifndef BPF_MAP_TYPE_HASH
#define BPF_MAP_TYPE_HASH 1
#endif

#ifndef BPF_MAP_TYPE_ARRAY
#define BPF_MAP_TYPE_ARRAY 2
#endif

#ifndef BPF_MAP_TYPE_PERF_EVENT_ARRAY
#define BPF_MAP_TYPE_PERF_EVENT_ARRAY 4
#endif

#ifndef BPF_MAP_TYPE_RINGBUF
#define BPF_MAP_TYPE_RINGBUF 27
#endif

#ifndef BPF_MAP_TYPE_LRU_HASH
#define BPF_MAP_TYPE_LRU_HASH 9
#endif

#ifndef BPF_ANY
#define BPF_ANY 0
#endif

// Maximum command name length
#define TASK_COMM_LEN 16

// Event types
#define EVENT_MEMORY_ALLOC 1
#define EVENT_MEMORY_FREE  2
#define EVENT_OOM_KILL     3
#define EVENT_PROCESS_EXIT 4

// Memory event structure (shared between eBPF and Go)
struct memory_event {
    u64 timestamp;      // Nanoseconds since boot
    u32 pid;           // Process ID
    u32 tid;           // Thread ID  
    u64 size;          // Allocation/free size
    u64 total_memory;  // Current total memory for process
    u32 event_type;    // EVENT_* constants
    char comm[TASK_COMM_LEN]; // Process name
    u8 in_container;   // 1 if in container, 0 if not
    u32 container_pid; // PID in container namespace
};

// Network event structure
struct network_event {
    u64 timestamp;
    u32 pid;
    u32 event_type;
    u32 src_ip;
    u32 dst_ip;
    u16 src_port;
    u16 dst_port;
    u32 bytes;
    u8 failed;         // 1 if connection failed
    char comm[TASK_COMM_LEN];
};

#endif /* __TAPIO_COMMON_H */