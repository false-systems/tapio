# DNS Observer CO-RE Migration - Technical Implementation

## Current State Analysis

### Files to Modify:
- `pkg/observers/dns/bpf_src/dns_monitor.c` - Main BPF program
- `pkg/observers/dns/bpf/generate.go` - bpf2go generation
- `pkg/observers/dns/collector_ebpf.go` - eBPF loader/handler

### Current Issues:
1. Using architecture-specific builds (`-target amd64,arm64`)
2. Direct struct access without CO-RE macros
3. No rate limiting implementation
4. Missing overflow handling
5. No sampling for high QPS scenarios

## Step-by-Step Migration

### Step 1: Generate vmlinux.h
```bash
# On target system with BTF support
bpftool btf dump file /sys/kernel/btf/vmlinux format c > pkg/observers/bpf_common/vmlinux.h

# Verify BTF availability
ls -la /sys/kernel/btf/vmlinux
```

### Step 2: Update BPF Source Header
```c
// pkg/observers/dns/bpf_src/dns_monitor.c

// REMOVE these includes:
// #include "../../bpf_common/vmlinux_minimal.h"
// #include "../../bpf_common/helpers.h"

// ADD CO-RE includes:
#include "vmlinux.h"  // BTF-generated kernel types
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// ADD safety defines
#define SAFE_ACCESS(x) (x ? x : 0)
#define MAX_DNS_NAME_LEN 256
#define RINGBUF_SIZE (256 * 1024)  // 256KB standard
```

### Step 3: Implement Rate Limiting
```c
// Rate limiter map
struct rate_limit_config {
    u64 max_events_per_sec;
    u64 current_tokens;
    u64 last_refill_ns;
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct rate_limit_config);
} dns_rate_limiter SEC(".maps");

// Rate limiting function
static __always_inline bool should_rate_limit(void) {
    u32 key = 0;
    struct rate_limit_config *config = bpf_map_lookup_elem(&dns_rate_limiter, &key);
    if (!config) return false;
    
    u64 now = bpf_ktime_get_ns();
    u64 elapsed = now - config->last_refill_ns;
    
    // Refill tokens (1000 events/sec = 1 event/ms)
    if (elapsed > 1000000) { // 1ms
        u64 new_tokens = elapsed / 1000000;
        config->current_tokens = min(config->current_tokens + new_tokens, 1000);
        config->last_refill_ns = now;
    }
    
    if (config->current_tokens > 0) {
        config->current_tokens--;
        return false; // Don't rate limit
    }
    
    return true; // Rate limit this event
}
```

### Step 4: Convert to CO-RE Access Patterns
```c
// OLD - Direct struct access
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    u16 dport = sk->__sk_common.skc_dport;
    u32 daddr = sk->__sk_common.skc_daddr;
    
// NEW - CO-RE access
SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
    if (!sk) return 0;
    
    // Use BPF_CORE_READ for all struct access
    u16 dport = BPF_CORE_READ(sk, __sk_common.skc_dport);
    u32 daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    u16 sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    
    // Check DNS port with byte order conversion
    if (bpf_ntohs(dport) != DNS_PORT && sport != DNS_PORT) {
        return 0;
    }
```

### Step 5: Safe Memory Access
```c
// Create safe read helpers
static __always_inline int safe_read_dns_name(char *dst, struct dnshdr *dns_hdr, void *data_end) {
    if (!dst || !dns_hdr) return -1;
    
    // Bounds check
    if ((void *)(dns_hdr + 1) > data_end) return -1;
    
    // Safe bounded loop for DNS name parsing
    u8 *pos = (u8 *)(dns_hdr + 1);
    int i = 0;
    
    #pragma unroll
    for (int j = 0; j < MAX_DNS_NAME_LEN; j++) {
        if (pos + 1 > (u8 *)data_end) break;
        
        u8 len = *pos;
        if (len == 0) break;
        
        if (len > 63) break; // DNS label limit
        
        pos++;
        if (pos + len > (u8 *)data_end) break;
        
        // Copy label
        if (i + len + 1 < MAX_DNS_NAME_LEN) {
            bpf_probe_read_kernel(dst + i, len, pos);
            i += len;
            dst[i++] = '.';
        }
        
        pos += len;
    }
    
    if (i > 0) dst[i-1] = '\0';
    return i;
}
```

### Step 6: Implement Sampling
```c
// Sampling for high QPS scenarios
#define SAMPLE_RATE 10  // Sample 1 in 10 packets

static __always_inline bool should_sample(void) {
    // Use pseudo-random sampling
    u32 rand = bpf_get_prandom_u32();
    return (rand % SAMPLE_RATE) == 0;
}

SEC("kprobe/udp_sendmsg")
int trace_udp_sendmsg(struct pt_regs *ctx) {
    // Rate limiting check
    if (should_rate_limit()) {
        __sync_fetch_and_add(&dropped_by_rate_limit, 1);
        return 0;
    }
    
    // Sampling check for high volume
    if (!should_sample()) {
        __sync_fetch_and_add(&dropped_by_sampling, 1);
        return 0;
    }
    
    // Continue with event processing...
}
```

### Step 7: Ring Buffer Overflow Handling
```c
// Overflow counter map
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, u64);
} overflow_counter SEC(".maps");

// Safe event submission
static __always_inline int submit_dns_event(struct dns_event *event) {
    // Reserve space in ring buffer
    struct dns_event *e = bpf_ringbuf_reserve(&dns_events, sizeof(*e), 0);
    if (!e) {
        // Increment overflow counter
        u32 key = 0;
        u64 *counter = bpf_map_lookup_elem(&overflow_counter, &key);
        if (counter) {
            __sync_fetch_and_add(counter, 1);
        }
        return -1;
    }
    
    // Copy event data
    __builtin_memcpy(e, event, sizeof(*event));
    
    // Submit event
    bpf_ringbuf_submit(e, 0);
    return 0;
}
```

### Step 8: Update bpf2go Generation
```go
// pkg/observers/dns/bpf/generate.go

// OLD
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang dnsmonitor ../bpf_src/dns_monitor.c -- -I../../bpf_common -I../bpf_src -g -O2 -Wall -Wextra

// NEW - CO-RE compliant
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpf -cc clang-14 dnsmonitor ../bpf_src/dns_monitor.c -- -I../../bpf_common -g -O2 -Wall -Wextra -D__TARGET_ARCH_x86 -Wno-compare-distinct-pointer-types
```

### Step 9: Update Go Loader
```go
// pkg/observers/dns/collector_ebpf.go

func (c *Collector) loadBPF() error {
    // Load with BTF support
    spec, err := loadDnsmonitor()
    if err != nil {
        return fmt.Errorf("loading BPF spec: %w", err)
    }
    
    // Ensure BTF is available
    if spec.Types == nil {
        return fmt.Errorf("BTF information not available - CO-RE requires BTF")
    }
    
    // Set resource limits
    if err := rlimit.RemoveMemlock(); err != nil {
        return fmt.Errorf("removing memlock: %w", err)
    }
    
    // Load with CO-RE relocations
    opts := &ebpf.CollectionOptions{
        Programs: ebpf.ProgramOptions{
            LogLevel: ebpf.LogLevelInfo,
            LogSize:  64 * 1024 * 1024, // 64MB for verifier logs
        },
    }
    
    coll, err := ebpf.NewCollectionWithOptions(spec, opts)
    if err != nil {
        var ve *ebpf.VerifierError
        if errors.As(err, &ve) {
            return fmt.Errorf("BPF verifier error: %v\n%s", err, ve.Log)
        }
        return fmt.Errorf("loading BPF collection: %w", err)
    }
    
    c.bpfCollection = coll
    return nil
}
```

### Step 10: Add Metrics Collection
```go
// Collect overflow and drop metrics
func (c *Collector) collectMetrics() {
    // Read overflow counter
    var overflowCount uint64
    if err := c.bpfMaps["overflow_counter"].Lookup(uint32(0), &overflowCount); err == nil {
        c.metricsEventsDropped.Add(context.Background(), int64(overflowCount),
            metric.WithAttributes(attribute.String("reason", "ringbuf_full")))
    }
    
    // Read rate limit drops
    var rateLimitDrops uint64
    if err := c.bpfMaps["dropped_by_rate_limit"].Lookup(uint32(0), &rateLimitDrops); err == nil {
        c.metricsEventsDropped.Add(context.Background(), int64(rateLimitDrops),
            metric.WithAttributes(attribute.String("reason", "rate_limit")))
    }
}
```

## Testing Plan

### 1. Kernel Compatibility Test
```bash
# Test on different kernels
for kernel in 5.4 5.10 5.15 6.0; do
    docker run --rm -it \
        -v $(pwd):/tapio \
        --privileged \
        kernel-test:$kernel \
        /tapio/test-dns-observer.sh
done
```

### 2. Load Test
```bash
# Generate high DNS load
dnsperf -s 127.0.0.1 -d queries.txt -c 100 -T 10 -Q 10000

# Verify rate limiting works
# Should see ~1000 events/sec max
```

### 3. Memory Test
```bash
# Run with memory profiling
go test -memprofile=mem.prof -bench=. ./pkg/observers/dns/

# Check for leaks
go tool pprof mem.prof
```

### 4. Verifier Test
```bash
# Load BPF program with verbose verifier
sudo bpftool prog load dns_monitor.o /sys/fs/bpf/dns_monitor \
    type kprobe \
    pinmaps /sys/fs/bpf/dns_maps

# Check verifier output for complexity
sudo bpftool prog show name dns_monitor -j | jq .
```

## Validation Checklist

- [ ] Compiles with `-target bpf`
- [ ] Loads on kernel 5.4+
- [ ] All struct access via BPF_CORE_READ
- [ ] Rate limiting at 1000 events/sec
- [ ] Sampling at 1:10 ratio
- [ ] Overflow counter working
- [ ] No memory leaks after 1M events
- [ ] Verifier complexity < 1M instructions
- [ ] CPU overhead < 2%
- [ ] Memory usage < 50MB

## Common Issues and Solutions

### Issue: Verifier Rejection
```
Error: BPF program too complex
Solution: Reduce loop iterations, use pragma unroll
```

### Issue: BTF Not Found
```
Error: BTF information not available
Solution: Ensure kernel compiled with CONFIG_DEBUG_INFO_BTF=y
```

### Issue: CO-RE Relocation Failed
```
Error: failed to relocate: no BTF for kernel type
Solution: Regenerate vmlinux.h from target kernel
```

## NO COMPROMISES
Following CLAUDE.md:
- Complete CO-RE implementation or don't deploy
- All safety checks or reject
- Test on 4+ kernels or not ready
- 80% test coverage minimum
- NO STUBS, NO TODOs