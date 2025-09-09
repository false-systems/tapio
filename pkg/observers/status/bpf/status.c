//go:build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_tracing.h>

#define MAX_ENTRIES 10000
#define PATTERN_BUFFER_SIZE 256

enum error_types {
    ERR_NONE = 0,
    ERR_TIMEOUT = 1,
    ERR_REFUSED = 2,
    ERR_RESET = 3,
    ERR_5XX = 4,
    ERR_4XX = 5,
    ERR_SLOW = 6,
    ERR_PARTIAL = 7,
};

struct failure_key {
    __u32 service_hash;
    __u32 endpoint_hash;
    __u16 status_code;
    __u16 error_type;
};

struct failure_stats {
    __u64 count;
    __u64 last_seen_ns;
    __u32 latency_sum;
    __u32 latency_count;
};

struct status_event {
    __u32 service_hash;
    __u32 endpoint_hash;
    __u16 status_code;
    __u16 error_type;
    __u64 timestamp;
    __u32 latency;
    __u32 pid;
};

struct conn_state {
    __u64 start_ns;
    __u32 bytes_sent;
    __u32 bytes_recv;
    __u16 proto;
    __u8 state;
    __u8 error_flag;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct failure_key);
    __type(value, struct failure_stats);
} failure_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, struct failure_stats);
} percpu_stats SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 100000);
    __type(key, struct sock *);
    __type(value, struct conn_state);
} conn_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
} events SEC(".maps");

static __always_inline __u32 hash_string(const char *str, int len) {
    __u32 hash = 5381;
    
    #pragma unroll
    for (int i = 0; i < 32 && i < len; i++) {
        if (str[i] == 0) break;
        hash = ((hash << 5) + hash) + str[i];
    }
    
    return hash;
}

static __always_inline int parse_http_status(char *data, int len) {
    if (len < 12) return 0;
    
    if (data[0] != 'H' || data[1] != 'T' || data[2] != 'T' || data[3] != 'P')
        return 0;
    
    int status = 0;
    #pragma unroll
    for (int i = 9; i < 12; i++) {
        if (data[i] >= '0' && data[i] <= '9') {
            status = status * 10 + (data[i] - '0');
        }
    }
    
    return status;
}

static __always_inline void record_failure(__u32 service_hash, __u32 endpoint_hash, 
                                          __u16 status_code, __u16 error_type, __u32 latency) {
    struct failure_key key = {
        .service_hash = service_hash,
        .endpoint_hash = endpoint_hash,
        .status_code = status_code,
        .error_type = error_type,
    };
    
    struct failure_stats *stats = bpf_map_lookup_elem(&failure_map, &key);
    if (!stats) {
        struct failure_stats new_stats = {
            .count = 1,
            .last_seen_ns = bpf_ktime_get_ns(),
            .latency_sum = latency,
            .latency_count = 1,
        };
        bpf_map_update_elem(&failure_map, &key, &new_stats, BPF_ANY);
    } else {
        __sync_fetch_and_add(&stats->count, 1);
        stats->last_seen_ns = bpf_ktime_get_ns();
        __sync_fetch_and_add(&stats->latency_sum, latency);
        __sync_fetch_and_add(&stats->latency_count, 1);
    }
    
    struct status_event event = {
        .service_hash = service_hash,
        .endpoint_hash = endpoint_hash,
        .status_code = status_code,
        .error_type = error_type,
        .timestamp = bpf_ktime_get_ns(),
        .latency = latency,
        .pid = bpf_get_current_pid_tgid() >> 32,
    };
    
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &event, sizeof(event));
}

SEC("kprobe/tcp_connect")
int trace_connect(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    
    struct conn_state state = {
        .start_ns = bpf_ktime_get_ns(),
        .state = 1,
        .error_flag = 0,
    };
    
    bpf_map_update_elem(&conn_map, &sk, &state, BPF_ANY);
    return 0;
}

SEC("kprobe/tcp_done")
int trace_close(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    struct conn_state *state = bpf_map_lookup_elem(&conn_map, &sk);
    
    if (state) {
        __u64 duration = bpf_ktime_get_ns() - state->start_ns;
        
        if (duration < 1000000) {
            state->error_flag |= ERR_RESET;
            
            __u32 service_hash = 0;
            __u32 endpoint_hash = 0;
            
            record_failure(service_hash, endpoint_hash, 0, ERR_RESET, duration / 1000);
        }
        
        bpf_map_delete_elem(&conn_map, &sk);
    }
    
    return 0;
}

SEC("socket/http_filter")
int parse_http_response(struct __sk_buff *skb) {
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return 0;
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return 0;
    
    if (ip->protocol != IPPROTO_TCP)
        return 0;
    
    struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
    if ((void *)(tcp + 1) > data_end)
        return 0;
    
    __u32 payload_offset = sizeof(*eth) + (ip->ihl * 4) + (tcp->doff * 4);
    if (data + payload_offset + 12 > data_end)
        return 0;
    
    char *payload = data + payload_offset;
    int status = parse_http_status(payload, data_end - payload);
    
    if (status >= 400) {
        __u16 error_type = (status >= 500) ? ERR_5XX : ERR_4XX;
        
        __u32 service_hash = ip->daddr;
        __u32 endpoint_hash = 0;
        
        record_failure(service_hash, endpoint_hash, status, error_type, 0);
    }
    
    return 0;
}

char _license[] SEC("license") = "GPL";