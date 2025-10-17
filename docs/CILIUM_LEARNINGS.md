# Learnings from Cilium eBPF Implementation

## Overview

Cilium is a production-grade eBPF-based networking and observability platform for Kubernetes. After exploring their codebase, here are the key patterns and techniques we can adopt for Tapio.

## Repository Structure

```
cilium/
├── bpf/                      # eBPF C code
│   ├── bpf_host.c           # Host network datapath
│   ├── bpf_lxc.c            # Container network datapath
│   ├── bpf_sock.c           # Socket-level hooks
│   ├── bpf_xdp.c            # XDP (eXpress Data Path) programs
│   └── lib/                 # Reusable eBPF library code
│       ├── common.h         # Core definitions
│       ├── conntrack_map.h  # Connection tracking
│       ├── drop.h           # Drop reasons
│       └── metrics.h        # eBPF-side metrics
│
├── pkg/
│   ├── ebpf/                # Go eBPF management
│   │   ├── map.go           # Map lifecycle management
│   │   └── map_register.go # Global map registry
│   ├── datapath/            # Datapath implementation
│   │   ├── loader/          # eBPF program loading
│   │   ├── maps/            # Map management
│   │   └── linux/           # Linux-specific datapath
│   └── bpf/                 # BPF helpers and utilities
```

## Key Patterns We Should Adopt

### 1. **LRU Hash Maps for Connection Tracking**

Cilium uses LRU (Least Recently Used) hash maps extensively for stateful tracking. This automatically evicts old entries when the map is full.

```c
// From cilium/bpf/lib/conntrack_map.h
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __type(key, struct ipv6_ct_tuple);
    __type(value, struct ct_entry);
    __uint(pinning, LIBBPF_PIN_BY_NAME);  // Pin to filesystem
    __uint(max_entries, CT_MAP_SIZE_TCP);
    __uint(map_flags, LRU_MEM_FLAVOR);
} cilium_ct6_global __section_maps_btf;
```

**Why this matters for Tapio**:
- **RTT baseline map** should be LRU_HASH, not plain HASH
- Automatically handles cleanup of stale connections
- No manual eviction needed (unlike Datner's current STALE_THRESHOLD logic)

**Recommendation for network observer**:
```c
// Replace:
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10000);
    __type(key, struct conn_key);
    __type(value, struct rtt_baseline);
} baseline_rtt SEC(".maps");

// With:
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);  // Auto-evict LRU entries
    __uint(max_entries, 10000);
    __type(key, struct conn_key);
    __type(value, struct rtt_baseline);
} baseline_rtt SEC(".maps");
```

### 2. **Map Pinning for Persistence**

Cilium pins maps to the filesystem (`/sys/fs/bpf/`) so they persist across program reloads.

```c
__uint(pinning, LIBBPF_PIN_BY_NAME);
```

**Benefits**:
- Observer restart doesn't lose RTT baselines
- Multiple eBPF programs can share same map
- Debugging: inspect maps with `bpftool map show`

**For Tapio**: Pin RTT baseline map so we don't re-learn baselines on every restart.

### 3. **Array-of-Maps for Multi-Cluster Support**

Cilium uses `BPF_MAP_TYPE_ARRAY_OF_MAPS` for per-cluster connection tracking:

```c
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY_OF_MAPS);
    __type(key, __u32);  // cluster_id
    __type(value, __u32);
    __uint(max_entries, 256);  // 256 clusters max
    __array(values, struct {
        __uint(type, BPF_MAP_TYPE_LRU_HASH);
        __type(key, struct ipv6_ct_tuple);
        __type(value, struct ct_entry);
        __uint(max_entries, CT_MAP_SIZE_TCP);
    });
} cilium_per_cluster_ct_tcp6 __section_maps_btf;
```

**For Tapio**: If we support multi-cluster observability, use array-of-maps pattern:
- Index by cluster ID
- Each cluster gets its own RTT baseline map
- Avoids overlapping IP conflicts

### 4. **Conditional Compilation with Feature Flags**

Cilium uses `#ifdef` extensively for optional features:

```c
#ifdef ENABLE_IPV6
    // IPv6-specific code
#endif

#ifdef ENABLE_CLUSTER_AWARE_ADDRESSING
    // Multi-cluster code
#endif
```

**For Tapio**: Use feature flags for optional stages:
```c
#ifdef ENABLE_RTT_TRACKING
    // Stage 3: RTT spike detection
#endif

#ifdef ENABLE_DNS_TRACKING
    // Stage 2: DNS tracking
#endif
```

### 5. **Structured Helper Libraries**

Cilium organizes eBPF code into reusable libraries:

- `lib/common.h` - Core definitions (IP addresses, endianness)
- `lib/conntrack.h` - Connection tracking logic
- `lib/drop.h` - Drop reason codes
- `lib/metrics.h` - eBPF metrics helpers

**For Tapio**: Create `internal/observers/common/bpf/lib/`:
```
lib/
├── common.h         # IP address unions, endianness helpers
├── tcp.h            # TCP state definitions, helpers
├── metrics.h        # Common metric helpers
└── maps.h           # Map accessor helpers
```

### 6. **Map Memory Flags Configuration**

Cilium configures map memory behavior per deployment:

```c
#ifdef PREALLOCATE_MAPS
#define CONDITIONAL_PREALLOC 0
#else
#define CONDITIONAL_PREALLOC BPF_F_NO_PREALLOC
#endif

#ifdef NO_COMMON_MEM_MAPS
#define LRU_MEM_FLAVOR BPF_F_NO_COMMON_LRU
#else
#define LRU_MEM_FLAVOR 0
#endif

// Apply to maps:
__uint(map_flags, LRU_MEM_FLAVOR);
```

**For Tapio**: Make map memory configurable:
- **NO_PREALLOC** for memory-constrained nodes
- **COMMON_LRU** vs **PER_CPU_LRU** based on load

### 7. **Helper Functions with `__always_inline`**

Cilium marks all helpers as `static __always_inline` to reduce BPF instruction count:

```c
static __always_inline bool is_v4_loopback(__be32 daddr)
{
    /* Check for 127.0.0.0/8 range */
    return (daddr & bpf_htonl(0xff000000)) == bpf_htonl(0x7f000000);
}

static __always_inline __be16
ctx_dst_port(const struct bpf_sock_addr *ctx)
{
    volatile __u32 dport = ctx->user_port;
    return (__be16)dport;
}
```

**Why**: BPF verifier counts instructions. Inlining reduces function call overhead and helps pass verifier limits.

**For Tapio**: Apply to all helper functions:
```c
static __always_inline bool is_rtt_spike(__u32 baseline_us, __u32 current_us)
{
    return current_us > (baseline_us * 2) || current_us > 500000;
}
```

### 8. **Volatile for Narrow Context Access**

Cilium uses `volatile` to work around narrow context access limitations:

```c
// Hack due to missing narrow ctx access
#define ctx_protocol(__ctx) ((__u8)(volatile __u32)(__ctx)->protocol)

static __always_inline __be16
ctx_src_port(const struct bpf_sock *ctx)
{
    volatile __u16 sport = (__u16)ctx->src_port;
    return (__be16)bpf_htons(sport);
}
```

**Issue**: BPF verifier sometimes rejects direct narrow field access.
**Solution**: Cast through volatile to force proper access width.

**For Tapio**: Apply if we hit verifier issues reading tcp_sock fields.

### 9. **Go-Side Map Management with Locking**

Cilium wraps cilium/ebpf maps with their own abstraction:

```go
type Map struct {
    logger *slog.Logger
    lock   lock.RWMutex  // Thread-safe access
    *ciliumebpf.Map

    spec *MapSpec
    path string
}

func (m *Map) OpenOrCreate() error {
    m.lock.Lock()
    defer m.lock.Unlock()

    // Check if already open
    if m.Map != nil {
        return nil
    }

    // Create or open pinned map
    opts := ciliumebpf.MapOptions{
        PinPath: bpf.TCGlobalsPath(),
    }

    newMap, err := ciliumebpf.NewMapWithOptions(m.spec, opts)
    // ...
}
```

**Benefits**:
- Thread-safe map access
- Automatic pinning
- Graceful handling of incompatible existing maps
- Logger integration

**For Tapio**: Wrap our maps similarly for safe concurrent access.

### 10. **Endianness Helpers**

Cilium provides comprehensive endianness conversion:

```c
// lib/endian.h
#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
# define __bpf_ntohs(x)		__builtin_bswap16(x)
# define __bpf_htons(x)		__builtin_bswap16(x)
# define __bpf_ntohl(x)		__builtin_bswap32(x)
# define __bpf_htonl(x)		__builtin_bswap32(x)
#else
# define __bpf_ntohs(x)		(x)
# define __bpf_htons(x)		(x)
# define __bpf_ntohl(x)		(x)
# define __bpf_htonl(x)		(x)
#endif

#define bpf_htons(x) (__builtin_constant_p(x) ? \
                     __constant_htons(x) : __bpf_htons(x))
```

**For Tapio**: Use `bpf_htons()` for port conversions, not raw `htons()`.

### 11. **Drop Reasons with Metrics**

Cilium tracks why packets are dropped with structured codes:

```c
// lib/drop_reasons.h
enum drop_reason {
    DROP_INVALID_SIP = 0,
    DROP_INVALID_SMAC,
    DROP_INVALID_DMAC,
    DROP_CT_INVALID_HDR,
    // ... 100+ drop reasons
};

// Emit drop with reason
send_drop_notify(ctx, DROP_CT_INVALID_HDR, 0, 0, ...);
```

**For Tapio Network Observer**: Add structured failure reasons:
```c
enum network_failure_reason {
    FAILURE_RST_RECEIVED = 0,
    FAILURE_SYN_TIMEOUT,
    FAILURE_RTT_SPIKE,
    FAILURE_RETRANSMIT_THRESHOLD,
};

struct network_event {
    // ...
    __u8 failure_reason;  // Only populated for failure events
};
```

### 12. **Per-CPU Maps for High-Throughput Metrics**

Cilium uses per-CPU maps for lock-free metrics:

```c
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, struct metrics_value);
    __uint(max_entries, METRIC_INDEX_MAX);
} cilium_metrics SEC(".maps");
```

**Why**: Lock-free atomic updates, aggregate in userspace.

**For Tapio**: Use per-CPU for high-frequency counters:
- Retransmit counts per connection
- RTT sample counts
- Packet counters

### 13. **Logging with Source Info**

Cilium embeds source location in logs:

```c
// lib/source_info.h
#define __SOURCE_INFO__ __FILE__ ":" stringify(__LINE__)

// Usage:
cilium_dbg(ctx, DBG_CT_LOOKUP, __SOURCE_INFO__);
```

**For Tapio**: Add debug mode with source info:
```c
#ifdef DEBUG_MODE
#define DEBUG_PRINT(msg) \
    bpf_printk("[%s:%d] " msg, __FILE__, __LINE__)
#else
#define DEBUG_PRINT(msg)
#endif
```

## Architectural Patterns

### 1. **Separation of Concerns**

```
bpf/
├── bpf_sock.c        # Socket hooks (connect, bind, sendmsg)
├── bpf_host.c        # Host networking (routing, NAT)
├── bpf_lxc.c         # Container networking (veth, policies)
└── bpf_xdp.c         # XDP fast path (early drop, LB)
```

**For Tapio**: Separate observers by hook type:
```
internal/observers/
├── network/          # TCP/UDP tracepoints
├── socket/           # Socket attach hooks
├── xdp/              # XDP performance monitoring
└── scheduler/        # K8s scheduler events (API watch)
```

### 2. **Layered Libraries**

```
bpf/lib/
├── common.h          # Foundation (types, endianness)
├── conntrack.h       # Uses: common.h
├── nat.h             # Uses: common.h, conntrack.h
└── policy.h          # Uses: all above
```

**For Tapio**: Build layered eBPF libraries:
```
internal/observers/common/bpf/lib/
├── common.h          # IP types, endianness
├── tcp.h             # TCP helpers (uses common.h)
├── metrics.h         # Metric helpers (uses common.h)
└── correlation.h     # Correlation keys (uses tcp.h)
```

### 3. **Feature Gates in Go**

Cilium uses feature detection in Go:

```go
func SupportsFeature(feature string) bool {
    switch feature {
    case "LRU_HASH":
        return kernelVersion >= "4.10"
    case "BTF":
        return kernelVersion >= "5.2"
    }
}
```

**For Tapio**: Gracefully degrade based on kernel version:
```go
func (n *NetworkObserver) Start(ctx context.Context) error {
    if !SupportsTracepoint("tcp", "tcp_retransmit_skb") {
        n.logger.Warn().Msg("tcp_retransmit_skb not available, skipping Stage 2")
        n.stages &= ^STAGE_RETRANSMIT
    }

    if !SupportsTracepoint("sock", "inet_sock_set_state") {
        return fmt.Errorf("inet_sock_set_state required (kernel 5.8+)")
    }

    return n.loadAndAttach(ctx)
}
```

## Specific Improvements for Tapio Network Observer

### 1. **Replace HASH with LRU_HASH for RTT Baseline Map**

```diff
// internal/observers/network/bpf/network_monitor.c

struct {
-   __uint(type, BPF_MAP_TYPE_HASH);
+   __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
+   __uint(pinning, LIBBPF_PIN_BY_NAME);  // Persist across restarts
    __type(key, struct conn_key);
    __type(value, struct rtt_baseline);
} baseline_rtt SEC(".maps");
```

**Remove manual cleanup code**:
```diff
- // Cleanup on TCP_CLOSE
- if (args->newstate == TCP_CLOSE && args->family == AF_INET) {
-     bpf_map_delete_elem(&baseline_rtt, &key);
- }
```

LRU automatically evicts old entries. Cleanup on close is redundant.

### 2. **Add Per-CPU Retransmit Counters**

```c
// High-frequency retransmit counter (lock-free)
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_HASH);
    __uint(max_entries, 10000);
    __type(key, struct conn_key);
    __type(value, __u64);  // retransmit count
} retransmit_counts SEC(".maps");

// In tcp_retransmit_skb handler:
__u64 *count = bpf_map_lookup_elem(&retransmit_counts, &key);
if (count) {
    (*count)++;  // Lock-free increment on this CPU
} else {
    __u64 initial = 1;
    bpf_map_update_elem(&retransmit_counts, &key, &initial, BPF_NOEXIST);
}
```

### 3. **Create Shared eBPF Library**

```c
// internal/observers/common/bpf/lib/tcp.h
#pragma once

#include "common.h"

// TCP state definitions
#define TCP_ESTABLISHED 1
#define TCP_SYN_SENT    2
#define TCP_SYN_RECV    3
// ... (move from network_monitor.c)

// TCP state name helper
static __always_inline const char *tcp_state_name(__u8 state)
{
    switch (state) {
    case TCP_ESTABLISHED: return "ESTABLISHED";
    case TCP_SYN_SENT:    return "SYN_SENT";
    // ...
    default: return "UNKNOWN";
    }
}
```

### 4. **Add RTT Sampling Rate Limiting**

Learn from Cilium's throttling:

```c
// Sample RTT only every N packets (reduce overhead)
#define RTT_SAMPLE_RATE 10

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, __u64);  // packet counter
    __uint(max_entries, 1);
} rtt_sample_counter SEC(".maps");

// In inet_sock_set_state handler:
__u32 key = 0;
__u64 *counter = bpf_map_lookup_elem(&rtt_sample_counter, &key);
if (counter) {
    (*counter)++;
    if (*counter % RTT_SAMPLE_RATE != 0) {
        goto skip_rtt_sampling;
    }
}

// Proceed with RTT sampling...
```

### 5. **Add Map Cleanup on Idle**

```c
// LRU handles most cleanup, but add idle detection:
#define IDLE_THRESHOLD_NS 3600000000000ULL  // 1 hour

if (now_ns - baseline->last_activity_ns > IDLE_THRESHOLD_NS) {
    // Connection idle for 1 hour, delete baseline
    bpf_map_delete_elem(&baseline_rtt, &key);
    return 0;
}
```

## Testing Patterns from Cilium

Cilium has extensive eBPF testing:

```
bpf/tests/
├── tc_egressgw_snat4.c     # Test SNAT at egress gateway
├── tc_nodeport_lb4_nat.c   # Test NodePort NAT
└── unit/                   # Unit tests for helpers
```

**For Tapio**: Add unit tests:
```
internal/observers/network/bpf/tests/
├── rtt_baseline_test.c     # Test RTT baseline learning
├── retransmit_test.c       # Test retransmit detection
└── unit/
    ├── tcp_helpers_test.c  # Test helper functions
    └── map_access_test.c   # Test map operations
```

## Performance Optimizations

### 1. **Minimize Map Lookups**

Cilium does 1 lookup per packet path:

```c
// BAD: Multiple lookups
struct ct_entry *ct = lookup_ct(key);
if (ct->flag1) { ... }
ct = lookup_ct(key);  // Redundant!
if (ct->flag2) { ... }

// GOOD: Single lookup, cache result
struct ct_entry *ct = lookup_ct(key);
if (!ct) return DROP;

if (ct->flag1) { ... }
if (ct->flag2) { ... }
```

**For Tapio**: Cache baseline lookup in RTT handler.

### 2. **Use __builtin_memcpy for Structs**

```c
// GOOD: Compiler optimizes to direct assignment
__builtin_memcpy(&evt->src_ip, args->saddr, 4);

// BAD: Byte-by-byte copy confuses verifier
for (int i = 0; i < 4; i++) {
    evt->src_ip_bytes[i] = args->saddr[i];
}
```

### 3. **Unroll Loops Explicitly**

```c
#pragma unroll
for (int i = 0; i < 16; i++) {
    evt->src_ipv6[i] = args->saddr_v6[i];
}
```

Verifier requires bounded loops. `#pragma unroll` ensures this.

## Summary: Top 5 Improvements for Tapio

1. **Switch to LRU_HASH maps** - Auto-eviction, no manual cleanup
2. **Pin maps to filesystem** - Persist RTT baselines across restarts
3. **Create shared eBPF library** - DRY principle for TCP helpers
4. **Use per-CPU maps for counters** - Lock-free high-frequency metrics
5. **Add feature detection** - Graceful degradation on older kernels

## References

- [Cilium GitHub](https://github.com/cilium/cilium)
- [Cilium eBPF Datapath](https://docs.cilium.io/en/latest/bpf/)
- [Cilium/eBPF Library](https://github.com/cilium/ebpf)
- [BPF Map Types](https://www.kernel.org/doc/html/latest/bpf/maps.html)
