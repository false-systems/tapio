#ifndef __TAPIO_COMMON_H
#define __TAPIO_COMMON_H

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

// Maximum command name length
#define TASK_COMM_LEN 16

// Event types
#define EVENT_MEMORY_ALLOC 1
#define EVENT_MEMORY_FREE  2
#define EVENT_OOM_KILL     3
#define EVENT_PROCESS_EXIT 4

// Memory event structure (shared between eBPF and Go)
struct memory_event {
    __u64 timestamp;      // Nanoseconds since boot
    __u32 pid;           // Process ID
    __u32 tid;           // Thread ID  
    __u64 size;          // Allocation/free size
    __u64 total_memory;  // Current total memory for process
    __u32 event_type;    // EVENT_* constants
    char comm[TASK_COMM_LEN]; // Process name
    __u8 in_container;   // 1 if in container, 0 if not
    __u32 container_pid; // PID in container namespace
};

// Network event structure
struct network_event {
    __u64 timestamp;
    __u32 pid;
    __u32 event_type;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 bytes;
    __u8 failed;         // 1 if connection failed
    char comm[TASK_COMM_LEN];
};

#endif /* __TAPIO_COMMON_H */