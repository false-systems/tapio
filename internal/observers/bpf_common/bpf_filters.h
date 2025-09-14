// SPDX-License-Identifier: GPL-2.0
/* BPF Dynamic Filtering Framework for Tapio Collectors
 * Provides runtime-configurable filtering without program reload
 */

#ifndef __BPF_FILTERS_H__
#define __BPF_FILTERS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Filter types
#define FILTER_TYPE_PID       0x01
#define FILTER_TYPE_NAMESPACE 0x02
#define FILTER_TYPE_NETWORK   0x04
#define FILTER_TYPE_CGROUP    0x08
#define FILTER_TYPE_UID       0x10
#define FILTER_TYPE_COMM      0x20

// Filter modes
#define FILTER_MODE_ALLOW     0  // Allowlist mode
#define FILTER_MODE_DENY      1  // Denylist mode

// Maximum filter entries
#define MAX_FILTER_ENTRIES    10000

// Filter configuration structure
struct filter_config {
    __u32 enabled_filters;    // Bitmask of enabled filter types
    __u32 filter_mode;        // Allow or deny mode
    __u32 sample_rate;        // Sampling rate (0-100%)
    __u32 batch_size;         // Batch size for event processing
    __u64 rate_limit;         // Rate limit in events per second
    __u64 last_batch_ns;      // Last batch timestamp
    __u32 current_batch_count; // Current batch counter
} __attribute__((packed));

// Network filter entry
struct network_filter {
    __u32 addr[4];      // IPv4/IPv6 address
    __u16 port;         // Port number (0 for any)
    __u8  ip_version;   // 4 or 6
    __u8  protocol;     // TCP, UDP, etc.
    __u32 prefix_len;   // Network prefix length for CIDR
} __attribute__((packed));

// Define filter maps
#define DEFINE_PID_FILTER_MAP(name) \
    struct { \
        __uint(type, BPF_MAP_TYPE_HASH); \
        __uint(max_entries, MAX_FILTER_ENTRIES); \
        __type(key, __u32); \
        __type(value, __u8); \
    } name SEC(".maps")

#define DEFINE_NS_FILTER_MAP(name) \
    struct { \
        __uint(type, BPF_MAP_TYPE_HASH); \
        __uint(max_entries, MAX_FILTER_ENTRIES); \
        __type(key, __u64); \
        __type(value, __u8); \
    } name SEC(".maps")

#define DEFINE_NET_FILTER_MAP(name) \
    struct { \
        __uint(type, BPF_MAP_TYPE_HASH); \
        __uint(max_entries, MAX_FILTER_ENTRIES); \
        __type(key, struct network_filter); \
        __type(value, __u8); \
    } name SEC(".maps")

#define DEFINE_FILTER_CONFIG_MAP(name) \
    struct { \
        __uint(type, BPF_MAP_TYPE_ARRAY); \
        __uint(max_entries, 1); \
        __type(key, __u32); \
        __type(value, struct filter_config); \
    } name SEC(".maps")

// Check if PID should be filtered
static __always_inline int
bpf_filter_check_pid(void *pid_map, struct filter_config *config, __u32 pid)
{
    if (!config || !(config->enabled_filters & FILTER_TYPE_PID))
        return 1; // Pass if filtering not enabled
    
    __u8 *exists = bpf_map_lookup_elem(pid_map, &pid);
    
    if (config->filter_mode == FILTER_MODE_ALLOW) {
        // Allowlist: pass only if PID is in map
        return exists ? 1 : 0;
    } else {
        // Denylist: pass only if PID is NOT in map
        return exists ? 0 : 1;
    }
}

// Check if namespace should be filtered
static __always_inline int
bpf_filter_check_namespace(void *ns_map, struct filter_config *config, __u64 ns_id)
{
    if (!config || !(config->enabled_filters & FILTER_TYPE_NAMESPACE))
        return 1; // Pass if filtering not enabled
    
    __u8 *exists = bpf_map_lookup_elem(ns_map, &ns_id);
    
    if (config->filter_mode == FILTER_MODE_ALLOW) {
        return exists ? 1 : 0;
    } else {
        return exists ? 0 : 1;
    }
}

// Check if network address should be filtered
static __always_inline int
bpf_filter_check_network(void *net_map, struct filter_config *config,
                         __u32 *addr, __u16 port, __u8 ip_version, __u8 protocol)
{
    if (!config || !(config->enabled_filters & FILTER_TYPE_NETWORK))
        return 1; // Pass if filtering not enabled
    
    struct network_filter filter = {};
    __builtin_memcpy(filter.addr, addr, sizeof(filter.addr));
    filter.port = port;
    filter.ip_version = ip_version;
    filter.protocol = protocol;
    
    __u8 *exists = bpf_map_lookup_elem(net_map, &filter);
    
    if (config->filter_mode == FILTER_MODE_ALLOW) {
        return exists ? 1 : 0;
    } else {
        return exists ? 0 : 1;
    }
}

// Check if event should be sampled
static __always_inline int
bpf_filter_should_sample(struct filter_config *config)
{
    if (!config || config->sample_rate == 0)
        return 0; // Don't sample if rate is 0
    
    if (config->sample_rate >= 100)
        return 1; // Always sample if rate is 100%
    
    __u32 random = bpf_get_prandom_u32();
    return (random % 100) < config->sample_rate;
}

// Check rate limiting
static __always_inline int
bpf_filter_check_rate_limit(struct filter_config *config)
{
    if (!config || config->rate_limit == 0)
        return 1; // No rate limiting
    
    __u64 now = bpf_ktime_get_ns();
    __u64 elapsed = now - config->last_batch_ns;
    
    // Simple token bucket algorithm
    if (elapsed > 1000000000) { // 1 second
        config->last_batch_ns = now;
        config->current_batch_count = 1;
        return 1;
    }
    
    if (config->current_batch_count >= config->rate_limit)
        return 0; // Rate limit exceeded
    
    config->current_batch_count++;
    return 1;
}

// Combined filter check for convenience
static __always_inline int
bpf_filter_check_all(void *pid_map, void *ns_map, void *net_map,
                    struct filter_config *config,
                    __u32 pid, __u64 ns_id,
                    __u32 *addr, __u16 port, __u8 ip_version, __u8 protocol)
{
    if (!config)
        return 1; // Pass all if no config
    
    // Check PID filter
    if (!bpf_filter_check_pid(pid_map, config, pid))
        return 0;
    
    // Check namespace filter
    if (!bpf_filter_check_namespace(ns_map, config, ns_id))
        return 0;
    
    // Check network filter
    if (!bpf_filter_check_network(net_map, config, addr, port, ip_version, protocol))
        return 0;
    
    // Check sampling
    if (!bpf_filter_should_sample(config))
        return 0;
    
    // Check rate limiting
    if (!bpf_filter_check_rate_limit(config))
        return 0;
    
    return 1; // Passed all filters
}

// Helper to get current task's namespace ID
static __always_inline __u64
bpf_get_current_ns_id(void)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task)
        return 0;
    
    // Try to read the namespace ID
    __u64 ns_id = 0;
    struct nsproxy *nsproxy;
    struct pid_namespace *pid_ns;
    
    // Read nsproxy from task
    BPF_CORE_READ_INTO(&nsproxy, task, nsproxy);
    if (!nsproxy)
        return 0;
    
    // Read pid namespace
    BPF_CORE_READ_INTO(&pid_ns, nsproxy, pid_ns_for_children);
    if (!pid_ns)
        return 0;
    
    // Read namespace ID (inum)
    BPF_CORE_READ_INTO(&ns_id, pid_ns, ns.inum);
    
    return ns_id;
}

// Helper to get current cgroup ID
static __always_inline __u64
bpf_get_current_cgroup_id_helper(void)
{
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    if (!task)
        return 0;
    
    return bpf_get_current_cgroup_id();
}

// Macro for easy filter initialization in probes
#define BPF_FILTER_INIT(config_map) \
    ({ \
        __u32 key = 0; \
        struct filter_config *__cfg = bpf_map_lookup_elem(config_map, &key); \
        __cfg; \
    })

#define BPF_FILTER_CHECK(pid_map, ns_map, net_map, config, pid, ns_id) \
    ({ \
        __u32 __null_addr[4] = {0}; \
        bpf_filter_check_all(pid_map, ns_map, net_map, config, \
                            pid, ns_id, __null_addr, 0, 0, 0); \
    })

#endif /* __BPF_FILTERS_H__ */