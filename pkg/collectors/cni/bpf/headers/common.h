// Common BPF helpers and definitions
#ifndef __COMMON_H
#define __COMMON_H

// Helper macros
#define MAX_STACK_DEPTH 20
#define TASK_COMM_LEN 16

// Common event header
struct event_header {
    __u64 timestamp;
    __u32 pid;
    __u32 tid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    char comm[TASK_COMM_LEN];
};

#endif /* __COMMON_H */