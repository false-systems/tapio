# Shared eBPF Libraries for Tapio Observers

Reusable eBPF libraries following Cilium's layered library approach and Brendan Gregg BPF Performance Tools patterns.

## 📚 Available Libraries

### `conn_tracking.h` - Connection Tracking
**Purpose:** Shared connection state tracking across observers

**Provides:**
- `struct conn_key` - Connection identifier (saddr, daddr, sport, dport)
- `struct retransmit_stats` - Retransmit/RST tracking per connection
- `make_conn_key()` - Helper to create connection keys

**Use Cases:**
- Network observer: TCP state transitions, retransmits, RST flags
- DNS observer (future): Query/response correlation by connection
- HTTP observer (future): Request/response tracking by connection

**Example Usage:**
```c
#include "../../common/bpf/lib/conn_tracking.h"

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, struct conn_key);           // ✅ Shared struct
    __type(value, struct retransmit_stats); // ✅ Shared struct
} conn_stats SEC(".maps");

// Create connection key
struct conn_key key;
make_conn_key(&key, saddr, daddr, sport, dport); // ✅ Shared helper
```

---

### `tcp.h` - TCP Protocol Helpers
**Purpose:** TCP-specific definitions and socket structures

**Provides:**
- TCP state constants (`TCP_ESTABLISHED`, `TCP_SYN_SENT`, etc.)
- Protocol numbers (`IPPROTO_TCP`, `IPPROTO_UDP`)
- Address families (`AF_INET`, `AF_INET6`)
- `tcp_state_name()` - Convert state to string
- Minimal CO-RE socket structs (`tcp_sock`, `sock`)

**Note:** Automatically includes `conn_tracking.h`

**Use Cases:**
- Network observer: TCP state machine tracking
- Any observer needing TCP-specific fields (RTT, cwnd, retransmits)

**Example Usage:**
```c
#include "../../common/bpf/lib/tcp.h"

// Check TCP state
if (args->newstate == TCP_ESTABLISHED) {
    // Connection established
}

// Read RTT from tcp_sock (CO-RE)
struct tcp_sock *tp = (struct tcp_sock *)sk;
__u32 srtt_us = 0;
bpf_core_read(&srtt_us, sizeof(srtt_us), &tp->srtt_us);
```

---

### `metrics.h` - Per-CPU Metrics
**Purpose:** Lock-free performance counters

**Provides:**
- `tapio_metrics` - Shared per-CPU array map (512 slots)
- `metric_inc()` - Increment counter
- `metric_add()` - Add value to counter
- Metric index namespaces (0-99: network, 100-199: scheduler, etc.)

**Use Cases:**
- High-frequency event counting (lock-free)
- Cross-observer metric aggregation

**Example Usage:**
```c
#include "../../common/bpf/lib/metrics.h"

// Increment network retransmit counter
metric_inc(METRIC_NETWORK_RETRANSMITS_TOTAL);

// Add batch value
metric_add(METRIC_NETWORK_EVENTS_TOTAL, batch_size);
```

---

## 🏗️ Architecture Patterns

### 1. **Layered Includes** (Cilium Pattern)
```
network_monitor.c
├─ tcp.h (TCP-specific)
│  └─ conn_tracking.h (shared connection tracking)
└─ metrics.h (shared metrics)
```

### 2. **LRU Hash Maps** (Brendan Gregg Pattern)
```c
// Auto-evicting map - bounded memory
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);  // Fixed size
    __type(key, struct conn_key);
    __type(value, struct retransmit_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);  // Persist across restarts
} conn_stats SEC(".maps");
```

**Benefits:**
- Automatic eviction of least-recently-used entries
- Bounded memory (no leaks)
- No manual cleanup needed

### 3. **Per-CPU Maps** (Lock-Free Metrics)
```c
// Each CPU writes to its own copy - zero contention
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, __u32);
    __type(value, __u64);
    __uint(max_entries, 512);
} tapio_metrics SEC(".maps");

// Userspace aggregates all per-CPU copies
```

### 4. **CO-RE (Compile Once, Run Everywhere)**
```c
struct tcp_sock {
    struct inet_connection_sock inet_conn;
    __u32 srtt_us;  // Only fields we access
} __attribute__((preserve_access_index));

// Always use bpf_core_read() - never direct access
bpf_core_read(&rtt, sizeof(rtt), &tp->srtt_us);
```

---

## 📝 Adding New Observers

### Step 1: Include Shared Libraries
```c
//go:build ignore

#include "../../base/bpf/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Shared libraries
#include "../../common/bpf/lib/conn_tracking.h"  // ✅ Connection tracking
#include "../../common/bpf/lib/metrics.h"        // ✅ Metrics
#include "../../common/bpf/lib/tcp.h"            // If TCP-specific
```

### Step 2: Use Shared Structs
```c
// LRU map for connection tracking
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 10000);
    __type(key, struct conn_key);           // ✅ Reuse
    __type(value, struct your_stats);
} your_map SEC(".maps");
```

### Step 3: Record Metrics
```c
// Define your metric index in metrics.h first:
// #define METRIC_YOUR_OBSERVER_EVENTS 200

metric_inc(METRIC_YOUR_OBSERVER_EVENTS);
```

---

## ✅ Benefits of Shared Libraries

1. **Code Reuse** - Write once, use everywhere
2. **Consistency** - Same connection tracking across observers
3. **Maintainability** - Fix bugs in one place
4. **Performance** - Proven patterns (Cilium, Netflix, Brendan Gregg)
5. **Correlation** - Shared conn_key enables event correlation

---

## 📖 References

- **Cilium**: https://github.com/cilium/cilium/tree/master/bpf/lib
- **Brendan Gregg BPF Performance Tools**: https://github.com/brendangregg/bpf-perf-tools-book
- **ADR 002**: Observer Consolidation Architecture
- **CLAUDE.md**: Tapio production standards

---

## 🚀 Future Additions

Planned shared libraries:
- `dns.h` - DNS query/response parsing
- `http.h` - HTTP request/response parsing
- `tls.h` - TLS handshake tracking
- `sampling.h` - High-volume event sampling

**Contribution Guidelines:** Follow CLAUDE.md standards (TDD, small commits, 80% coverage)
