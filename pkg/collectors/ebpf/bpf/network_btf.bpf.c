//go:build ignore

#include "headers/vmlinux.h"

// Map definition macros
#define __uint(name, val) int(*name)[val]
#define __type(name, val) typeof(val) *name
#define __array(name, val) typeof(val) *name[]

#define BPF_MAP_TYPE_RINGBUF 27

char LICENSE[] __attribute__((section("license"), used)) = "GPL";

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events __attribute__((section(".maps"), used));

// BPF helper prototypes - modern way
static long (*bpf_ktime_get_ns)(void) = (void *) 5;
static long (*bpf_get_current_pid_tgid)(void) = (void *) 14;
static void *(*bpf_ringbuf_reserve)(void *ringbuf, __u64 size, __u64 flags) = (void *) 131;
static void (*bpf_ringbuf_submit)(void *data, __u64 flags) = (void *) 132;

struct net_event {
    __u64 timestamp;
    __u32 pid;
};

__attribute__((section("kprobe/tcp_v4_connect"), used))
int trace_connect(struct pt_regs *ctx) {
    struct net_event *e;
    
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
        
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}