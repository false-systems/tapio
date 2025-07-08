/* Minimal vmlinux.h for eBPF programs - will be replaced by bpftool on Linux */
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

struct trace_event_raw_mm_page_alloc {
    __u32 order;
};

struct trace_event_raw_mm_page_free {
    __u32 order;
};

struct trace_event_raw_sched_process_template {
    __u32 pid;
    char comm[16];
};

struct task_struct {
    void *nsproxy;
    void *thread_pid;
};

struct pid_namespace {
    unsigned int level;
};

struct pid {
    struct {
        int nr;
    } numbers[4];
};

#endif /* __VMLINUX_H__ */