// SPDX-License-Identifier: GPL-2.0
/* BPF Map Definitions for Unified eBPF Framework
 * Provides standardized map structures for filtering, sampling, and statistics
 */

#ifndef __BPF_MAPS_H__
#define __BPF_MAPS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>

/* Maximum entries for various map types */
#define MAX_NAMESPACE_FILTERS 1000
#define MAX_PROCESS_FILTERS   5000
#define MAX_NETWORK_FILTERS   1000
#define MAX_CGROUP_FILTERS    500
#define MAX_EVENT_TYPES       100
#define MAX_SAMPLE_RATES      256

/* Filter action types */
#define FILTER_ACTION_ALLOW  0
#define FILTER_ACTION_DROP   1
#define FILTER_ACTION_SAMPLE 2

/* Sampling strategy types */
#define SAMPLING_PROBABILISTIC 0
#define SAMPLING_ADAPTIVE      1
#define SAMPLING_HASH_BASED    2
#define SAMPLING_RATE_LIMITED  3

/* Network filter key structure */
struct network_filter_key {
    __u32 ip;     /* IPv4 address */
    __u32 mask;   /* Network mask */
    __u16 port;   /* Port number (0 for any) */
    __u8  protocol; /* IP protocol */
    __u8  direction; /* 0=ingress, 1=egress, 2=both */
} __attribute__((packed));

/* IPv6 network filter key */
struct network_filter_key_v6 {
    __u32 ip[4];   /* IPv6 address */
    __u32 mask[4]; /* Network mask */
    __u16 port;    /* Port number */
    __u8  protocol;
    __u8  direction;
} __attribute__((packed));

/* Process filter key structure */
struct process_filter_key {
    __u32 pid;        /* Process ID (0 for any) */
    __u32 ppid;       /* Parent process ID (0 for any) */
    __u32 uid;        /* User ID (0 for any) */
    __u32 gid;        /* Group ID (0 for any) */
    __u64 cgroup_id;  /* Cgroup ID (0 for any) */
    __u32 exec_hash;  /* Hash of executable path */
} __attribute__((packed));

/* Namespace filter key structure */
struct namespace_filter_key {
    __u64 netns_inode; /* Network namespace inode */
    __u32 ns_hash;     /* Hash of namespace name */
    __u32 pod_hash;    /* Hash of pod name */
} __attribute__((packed));

/* Cgroup filter key structure */
struct cgroup_filter_key {
    __u64 cgroup_id;   /* Cgroup ID */
    __u32 path_hash;   /* Hash of cgroup path */
} __attribute__((packed));

/* Filter value structure */
struct filter_value {
    __u32 action;      /* Filter action (allow/drop/sample) */
    __u32 priority;    /* Rule priority (higher = more important) */
    __u32 sample_rate; /* Sample rate in fixed-point (0-65535 = 0.0-1.0) */
    __u64 hit_count;   /* Number of times this filter matched */
    __u64 timestamp;   /* Last hit timestamp */
} __attribute__((packed));

/* Sample rate configuration structure */
struct sample_rate_config {
    __u32 event_type_hash; /* Hash of event type name */
    __u32 sample_rate;     /* Sample rate in fixed-point (0-2^32 = 0.0-1.0) */
    __u32 strategy;        /* Sampling strategy */
    __u32 random_seed;     /* Random seed for hash-based sampling */
} __attribute__((packed));

/* BPF statistics structure */
struct bpf_stats {
    __u64 events_received;   /* Total events received */
    __u64 events_processed;  /* Events successfully processed */
    __u64 events_dropped;    /* Events dropped */
    __u64 events_filtered;   /* Events filtered out */
    __u64 events_sampled;    /* Events included via sampling */
    __u64 processing_time_ns; /* Total processing time */
    __u64 last_update;       /* Last statistics update */
    __u32 ring_buffer_size;  /* Ring buffer size */
    __u32 ring_buffer_used;  /* Ring buffer utilization */
} __attribute__((packed));

/* Adaptive sampling state */
struct adaptive_sampling_state {
    __u64 window_start;      /* Start of current window */
    __u64 events_in_window;  /* Events seen in current window */
    __u64 target_rate;       /* Target events per second */
    __u32 current_sample_rate; /* Current sample rate */
    __u32 adjustment_count;  /* Number of adjustments made */
} __attribute__((packed));

/* --- BPF Map Definitions --- */

/* Network filter maps */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_NETWORK_FILTERS);
    __type(key, struct network_filter_key);
    __type(value, struct filter_value);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} network_filters_v4 SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_NETWORK_FILTERS);
    __type(key, struct network_filter_key_v6);
    __type(value, struct filter_value);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} network_filters_v6 SEC(".maps");

/* Process filter map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_PROCESS_FILTERS);
    __type(key, struct process_filter_key);
    __type(value, struct filter_value);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} process_filters SEC(".maps");

/* Namespace filter map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_NAMESPACE_FILTERS);
    __type(key, struct namespace_filter_key);
    __type(value, struct filter_value);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} namespace_filters SEC(".maps");

/* Cgroup filter map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_CGROUP_FILTERS);
    __type(key, struct cgroup_filter_key);
    __type(value, struct filter_value);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} cgroup_filters SEC(".maps");

/* Sample rate configuration map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_SAMPLE_RATES);
    __type(key, __u32); /* Event type hash */
    __type(value, struct sample_rate_config);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} sample_rates SEC(".maps");

/* BPF statistics map */
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct bpf_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} bpf_statistics SEC(".maps");

/* Adaptive sampling state map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_EVENT_TYPES);
    __type(key, __u32); /* Event type hash */
    __type(value, struct adaptive_sampling_state);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} adaptive_sampling_state SEC(".maps");

/* Random seed map for consistent sampling */
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} random_seed SEC(".maps");

/* --- Helper Functions --- */

/**
 * hash_string - Simple string hashing function
 * @str: String to hash
 * @len: Length of string
 * 
 * Returns: 32-bit hash value
 */
static __always_inline __u32 hash_string(const char *str, int len)
{
    __u32 hash = 2166136261U; /* FNV offset basis */
    
    #pragma unroll
    for (int i = 0; i < len && i < 256; i++) {
        if (str[i] == '\0')
            break;
        hash ^= (__u32)str[i];
        hash *= 16777619U; /* FNV prime */
    }
    
    return hash;
}

/**
 * get_current_timestamp - Get current timestamp in nanoseconds
 */
static __always_inline __u64 get_current_timestamp(void)
{
    return bpf_ktime_get_ns();
}

/**
 * check_network_filter - Check if packet matches network filter
 * @src_ip: Source IP address
 * @dst_ip: Destination IP address
 * @src_port: Source port
 * @dst_port: Destination port
 * @protocol: IP protocol
 * @is_ingress: True for ingress, false for egress
 * 
 * Returns: Pointer to filter value if match found, NULL otherwise
 */
static __always_inline struct filter_value *
check_network_filter(__u32 src_ip, __u32 dst_ip, __u16 src_port, __u16 dst_port, 
                     __u8 protocol, bool is_ingress)
{
    struct network_filter_key key = {};
    struct filter_value *value;
    
    /* Check source address filter */
    key.ip = src_ip;
    key.mask = 0xFFFFFFFF; /* Exact match first */
    key.port = src_port;
    key.protocol = protocol;
    key.direction = is_ingress ? 0 : 1;
    
    value = bpf_map_lookup_elem(&network_filters_v4, &key);
    if (value) {
        /* Update hit count and timestamp */
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Check destination address filter */
    key.ip = dst_ip;
    key.port = dst_port;
    
    value = bpf_map_lookup_elem(&network_filters_v4, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Check wildcard filters (port = 0, any direction) */
    key.port = 0;
    key.direction = 2; /* Both directions */
    
    /* Try source IP wildcard */
    key.ip = src_ip;
    value = bpf_map_lookup_elem(&network_filters_v4, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Try destination IP wildcard */
    key.ip = dst_ip;
    value = bpf_map_lookup_elem(&network_filters_v4, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    return NULL;
}

/**
 * check_process_filter - Check if process matches filter
 * @pid: Process ID
 * @ppid: Parent process ID
 * @uid: User ID
 * @gid: Group ID
 * @cgroup_id: Cgroup ID
 * @exec_path: Executable path
 * 
 * Returns: Pointer to filter value if match found, NULL otherwise
 */
static __always_inline struct filter_value *
check_process_filter(__u32 pid, __u32 ppid, __u32 uid, __u32 gid, 
                     __u64 cgroup_id, const char *exec_path)
{
    struct process_filter_key key = {};
    struct filter_value *value;
    
    /* Try exact PID match first */
    key.pid = pid;
    key.ppid = ppid;
    key.uid = uid;
    key.gid = gid;
    key.cgroup_id = cgroup_id;
    key.exec_hash = hash_string(exec_path, 256);
    
    value = bpf_map_lookup_elem(&process_filters, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Try wildcard matches */
    key.pid = 0; /* Any PID */
    value = bpf_map_lookup_elem(&process_filters, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Try UID-only match */
    key.ppid = 0;
    key.gid = 0;
    key.cgroup_id = 0;
    key.exec_hash = 0;
    
    value = bpf_map_lookup_elem(&process_filters, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    return NULL;
}

/**
 * check_namespace_filter - Check if namespace matches filter
 * @netns_inode: Network namespace inode
 * @ns_name: Namespace name
 * @pod_name: Pod name
 * 
 * Returns: Pointer to filter value if match found, NULL otherwise
 */
static __always_inline struct filter_value *
check_namespace_filter(__u64 netns_inode, const char *ns_name, const char *pod_name)
{
    struct namespace_filter_key key = {};
    struct filter_value *value;
    
    key.netns_inode = netns_inode;
    key.ns_hash = hash_string(ns_name, 64);
    key.pod_hash = hash_string(pod_name, 64);
    
    value = bpf_map_lookup_elem(&namespace_filters, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    /* Try namespace-only match */
    key.pod_hash = 0;
    value = bpf_map_lookup_elem(&namespace_filters, &key);
    if (value) {
        __sync_fetch_and_add(&value->hit_count, 1);
        value->timestamp = get_current_timestamp();
        return value;
    }
    
    return NULL;
}

/**
 * get_sample_rate - Get sample rate for event type
 * @event_type: Event type string
 * 
 * Returns: Sample rate in fixed-point format (0-2^32)
 */
static __always_inline __u32 get_sample_rate(const char *event_type)
{
    __u32 event_hash = hash_string(event_type, 64);
    struct sample_rate_config *config;
    
    config = bpf_map_lookup_elem(&sample_rates, &event_hash);
    if (config) {
        return config->sample_rate;
    }
    
    /* Check for default rate (key = 0) */
    __u32 default_key = 0;
    config = bpf_map_lookup_elem(&sample_rates, &default_key);
    if (config) {
        return config->sample_rate;
    }
    
    /* Default to 100% sampling */
    return 0xFFFFFFFF;
}

/**
 * should_sample - Make sampling decision
 * @event_type: Event type string
 * @random_value: Random value for decision
 * 
 * Returns: True if event should be sampled
 */
static __always_inline bool should_sample(const char *event_type, __u32 random_value)
{
    __u32 sample_rate = get_sample_rate(event_type);
    return random_value <= sample_rate;
}

/**
 * should_sample_consistent - Make consistent sampling decision based on key
 * @event_type: Event type string
 * @consistency_key: Key for consistent hashing
 * 
 * Returns: True if event should be sampled
 */
static __always_inline bool should_sample_consistent(const char *event_type, __u64 consistency_key)
{
    __u32 event_hash = hash_string(event_type, 64);
    struct sample_rate_config *config;
    
    config = bpf_map_lookup_elem(&sample_rates, &event_hash);
    if (!config) {
        return true; /* Sample if no config found */
    }
    
    /* Hash the consistency key with random seed */
    __u32 seed = config->random_seed;
    __u32 hash_input = (__u32)(consistency_key ^ ((__u64)seed << 32));
    
    /* Simple hash function */
    hash_input = hash_input ^ (hash_input >> 16);
    hash_input = hash_input * 0x45d9f3b;
    hash_input = hash_input ^ (hash_input >> 16);
    
    return hash_input <= config->sample_rate;
}

/**
 * update_bpf_statistics - Update BPF program statistics
 * @events_received: Number of events received
 * @events_processed: Number of events processed
 * @events_dropped: Number of events dropped
 * @processing_time_ns: Processing time in nanoseconds
 */
static __always_inline void update_bpf_statistics(__u64 events_received, 
                                                  __u64 events_processed,
                                                  __u64 events_dropped,
                                                  __u64 processing_time_ns)
{
    __u32 key = 0;
    struct bpf_stats *stats;
    
    stats = bpf_map_lookup_elem(&bpf_statistics, &key);
    if (stats) {
        __sync_fetch_and_add(&stats->events_received, events_received);
        __sync_fetch_and_add(&stats->events_processed, events_processed);
        __sync_fetch_and_add(&stats->events_dropped, events_dropped);
        __sync_fetch_and_add(&stats->processing_time_ns, processing_time_ns);
        stats->last_update = get_current_timestamp();
    }
}

#endif /* __BPF_MAPS_H__ */