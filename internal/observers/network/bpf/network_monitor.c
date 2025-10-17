//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Shared helpers (Cilium-style layered lib)
#include "../../common/bpf/lib/tcp.h"
#include "../../common/bpf/lib/metrics.h"

// Event types for distinguishing tracepoint sources
#define EVENT_TYPE_STATE_CHANGE  0  // inet_sock_set_state
#define EVENT_TYPE_RST_RECEIVED  1  // tcp_receive_reset
#define EVENT_TYPE_RETRANSMIT    2  // tcp_retransmit_skb
#define EVENT_TYPE_RTT_SPIKE     3  // RTT spike detection (Stage 3)

// Network event structure - MUST match Go NetworkEventBPF exactly (71 bytes packed, 72 with Go alignment)
struct network_event {
	__u32 pid;           // offset 0, size 4
	__u32 src_ip;        // offset 4, size 4
	__u32 dst_ip;        // offset 8, size 4
	__u8  src_ipv6[16];  // offset 12, size 16
	__u8  dst_ipv6[16];  // offset 28, size 16
	__u16 src_port;      // offset 44, size 2
	__u16 dst_port;      // offset 46, size 2
	__u16 family;        // offset 48, size 2
	__u8  protocol;      // offset 50, size 1
	__u8  old_state;     // offset 51, size 1 - TCP state OR total_retrans (see event_type)
	__u8  new_state;     // offset 52, size 1 - TCP state OR snd_cwnd (see event_type)
	__u8  event_type;    // offset 53, size 1 - EVENT_TYPE_STATE_CHANGE, EVENT_TYPE_RST_RECEIVED, or EVENT_TYPE_RETRANSMIT
	__u8  comm[16];      // offset 54, size 16
} __attribute__((packed));

// Ring buffer for sending events to userspace
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);  // 256KB ring buffer
} events SEC(".maps");

// NOTE: conn_key now defined in tcp.h (shared across observers)

// RTT baseline state
struct rtt_baseline {
	__u32 baseline_us;      // Baseline RTT in microseconds
	__u8  sample_count;     // How many samples collected (0-5)
	__u8  state;            // NO_BASELINE=0, LEARNING=1, STABLE=2
	__u64 last_update_ns;   // Last time we updated baseline
	__u64 last_activity_ns; // Last time we saw traffic
};

// RTT baseline tracking map (LRU auto-evicts old baselines)
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 10000);  // Track up to 10k connections
	__type(key, struct conn_key);
	__type(value, struct rtt_baseline);
	__uint(pinning, LIBBPF_PIN_BY_NAME);  // Persist across restarts
} baseline_rtt SEC(".maps");

// RTT states
#define RTT_STATE_NO_BASELINE 0
#define RTT_STATE_LEARNING    1
#define RTT_STATE_STABLE      2

// Thresholds
#define LEARNING_SAMPLES 5                     // Collect 5 samples before going STABLE
#define STALE_THRESHOLD_NS 3600000000000ULL    // 1 hour
#define IDLE_THRESHOLD_NS  300000000000ULL     // 5 minutes

// NOTE: IPPROTO_TCP, AF_INET, AF_INET6, TCP_* states now in tcp.h (shared)

// Tracepoint arguments for sock/inet_sock_set_state
// This is the stable kernel ABI - no BPF_CORE_READ needed!
//
// NOTE: This struct layout matches Linux kernel 5.8+
// Verified against: /sys/kernel/debug/tracing/events/sock/inet_sock_set_state/format
// If tracepoint fails to attach, check kernel version and tracepoint format.
// Use: cat /sys/kernel/debug/tracing/events/sock/inet_sock_set_state/format
struct trace_event_raw_inet_sock_set_state {
	__u64 unused;       // Common tracepoint header
	const void *skaddr;
	int oldstate;
	int newstate;
	__u16 sport;
	__u16 dport;
	__u16 family;
	__u16 protocol;
	__u8 saddr[4];
	__u8 daddr[4];
	__u8 saddr_v6[16];
	__u8 daddr_v6[16];
};

SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct trace_event_raw_inet_sock_set_state *args)
{
	// Verifier requirement: Check protocol early for bounds
	if (args->protocol != IPPROTO_TCP) {
		return 0;  // Only track TCP for now
	}

	// Get tcp_sock from skaddr to read RTT
	const struct sock *sk = (const struct sock *)args->skaddr;
	struct tcp_sock *tp = (struct tcp_sock *)sk;

	// Read smoothed RTT from tcp_sock (srtt_us is in microseconds, divided by 8)
	__u32 srtt_us = 0;
	bpf_core_read(&srtt_us, sizeof(srtt_us), &tp->srtt_us);
	__u32 rtt_us = srtt_us >> 3;

	// Get current time for baseline tracking
	__u64 now_ns = bpf_ktime_get_ns();

	// Create connection key (IPv4 only for now)
	struct conn_key key = {0};
	if (args->family == AF_INET) {
		__builtin_memcpy(&key.saddr, args->saddr, 4);
		__builtin_memcpy(&key.daddr, args->daddr, 4);
		key.sport = args->sport;
		key.dport = args->dport;
	}

	// RTT tracking for ESTABLISHED connections with valid RTT
	if (args->newstate == TCP_ESTABLISHED && rtt_us > 0 && args->family == AF_INET) {
		// Lookup or create baseline entry
		struct rtt_baseline *baseline = bpf_map_lookup_elem(&baseline_rtt, &key);

		if (!baseline) {
			// First measurement - initialize to LEARNING state
			struct rtt_baseline new_baseline = {
				.baseline_us = rtt_us,
				.sample_count = 1,
				.state = RTT_STATE_LEARNING,
				.last_update_ns = now_ns,
				.last_activity_ns = now_ns,
			};
			bpf_map_update_elem(&baseline_rtt, &key, &new_baseline, BPF_NOEXIST);
		} else {
			// Update last activity timestamp
			baseline->last_activity_ns = now_ns;

			// State machine logic
			if (baseline->state == RTT_STATE_LEARNING) {
				// Collect samples to establish baseline
				baseline->sample_count++;

				// Calculate running average
				baseline->baseline_us = (baseline->baseline_us * (baseline->sample_count - 1) + rtt_us) / baseline->sample_count;

				// Transition to STABLE after collecting enough samples
				if (baseline->sample_count >= LEARNING_SAMPLES) {
					baseline->state = RTT_STATE_STABLE;
				}

				bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
			} else if (baseline->state == RTT_STATE_STABLE) {
				// Check for staleness (baseline older than 1 hour)
				if (now_ns - baseline->last_update_ns > STALE_THRESHOLD_NS) {
					// Slowly update baseline (90% old + 10% new)
					baseline->baseline_us = (baseline->baseline_us * 9 + rtt_us) / 10;
					baseline->last_update_ns = now_ns;
					bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
				}

				// Check for RTT spike: >2x baseline OR >500ms absolute
				if (rtt_us > (baseline->baseline_us * 2) || rtt_us > 500000) {
					// Emit RTT spike event
					struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
					if (evt) {
						__builtin_memset(evt, 0, sizeof(*evt));

						// Extract process info
						__u64 pid_tgid = bpf_get_current_pid_tgid();
						evt->pid = pid_tgid >> 32;
						bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

						// Mark as RTT spike event
						evt->event_type = EVENT_TYPE_RTT_SPIKE;

						// Extract connection info
						evt->protocol = (__u8)args->protocol;
						evt->family = args->family;
						evt->src_port = args->sport;
						evt->dst_port = args->dport;
						__builtin_memcpy(&evt->src_ip, args->saddr, 4);
						__builtin_memcpy(&evt->dst_ip, args->daddr, 4);

						// Reuse OldState/NewState for RTT data (convert us to ms, clamp to 255)
						__u32 baseline_ms = baseline->baseline_us / 1000;
						__u32 current_ms = rtt_us / 1000;
						evt->old_state = baseline_ms > 255 ? 255 : baseline_ms;
						evt->new_state = current_ms > 255 ? 255 : current_ms;

						bpf_ringbuf_submit(evt, 0);
					}
				}
			}
		}
	}

	// NOTE: No manual cleanup needed - LRU_HASH auto-evicts old entries
	// Old code had: bpf_map_delete_elem(&baseline_rtt, &key) on TCP_CLOSE
	// LRU handles eviction based on least-recently-used policy

	// Emit regular state change event for important transitions
	// (connection failures, resets, etc - NOT every ESTABLISHED heartbeat)
	if (args->oldstate != args->newstate &&
	    (args->newstate == TCP_CLOSE || args->oldstate == TCP_SYN_SENT ||
	     args->oldstate == TCP_SYN_RECV || args->newstate == TCP_LISTEN)) {

		struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
		if (!evt) {
			return 0;
		}

		__builtin_memset(evt, 0, sizeof(*evt));

		// Extract process info
		__u64 pid_tgid = bpf_get_current_pid_tgid();
		evt->pid = pid_tgid >> 32;
		bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

		// Mark as state change event
		evt->event_type = EVENT_TYPE_STATE_CHANGE;

		// Extract TCP state transition
		evt->old_state = (__u8)args->oldstate;
		evt->new_state = (__u8)args->newstate;

		// Extract protocol and family
		evt->protocol = (__u8)args->protocol;
		evt->family = args->family;

		// Extract ports
		evt->src_port = args->sport;
		evt->dst_port = args->dport;

		// Extract IP addresses
		if (args->family == AF_INET) {
			__builtin_memcpy(&evt->src_ip, args->saddr, 4);
			__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
		} else if (args->family == AF_INET6) {
			#pragma unroll
			for (int i = 0; i < 16; i++) {
				evt->src_ipv6[i] = args->saddr_v6[i];
				evt->dst_ipv6[i] = args->daddr_v6[i];
			}
		}

		bpf_ringbuf_submit(evt, 0);
	}

	return 0;
}

// Tracepoint arguments for tcp/tcp_receive_reset
// Fires when kernel receives RST packet (connection refused/reset)
struct trace_event_raw_tcp_receive_reset {
	__u64 unused;       // Common tracepoint header
	const void *skaddr;
	int state;          // TCP state when RST received
	__u16 sport;
	__u16 dport;
	__u16 family;
	__u8 saddr[4];
	__u8 daddr[4];
	__u8 saddr_v6[16];
	__u8 daddr_v6[16];
};

SEC("tracepoint/tcp/tcp_receive_reset")
int trace_tcp_receive_reset(struct trace_event_raw_tcp_receive_reset *args)
{
	// Reserve ring buffer space
	struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		return 0;  // Ring buffer full
	}

	// Zero-initialize
	__builtin_memset(evt, 0, sizeof(*evt));

	// Mark as RST received event
	evt->event_type = EVENT_TYPE_RST_RECEIVED;

	// Extract process info
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->pid = pid_tgid >> 32;
	bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

	// Store current state in old_state field (state when RST received)
	evt->old_state = (__u8)args->state;
	evt->new_state = 0;  // Not applicable for RST

	// Extract protocol and family
	evt->protocol = IPPROTO_TCP;
	evt->family = args->family;

	// Extract ports (host byte order)
	evt->src_port = args->sport;
	evt->dst_port = args->dport;

	// Extract IP addresses
	if (args->family == AF_INET) {
		__builtin_memcpy(&evt->src_ip, args->saddr, 4);
		__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
	} else if (args->family == AF_INET6) {
		#pragma unroll
		for (int i = 0; i < 16; i++) {
			evt->src_ipv6[i] = args->saddr_v6[i];
			evt->dst_ipv6[i] = args->daddr_v6[i];
		}
	}

	// Submit event
	bpf_ringbuf_submit(evt, 0);

	// Increment Per-CPU metrics (lock-free)
	metric_inc(METRIC_NETWORK_RST_TOTAL);

	return 0;
}

// Tracepoint arguments for tcp/tcp_retransmit_skb
// This tracepoint fires when TCP retransmits a packet (packet loss detected)
struct trace_event_raw_tcp_retransmit_skb {
	__u64 unused;            // Common tracepoint header
	const void *skbaddr;
	const void *skaddr;      // struct sock pointer
	int state;
	__u16 sport;
	__u16 dport;
	__u16 family;
	__u8 saddr[4];
	__u8 daddr[4];
	__u8 saddr_v6[16];
	__u8 daddr_v6[16];
};

SEC("tracepoint/tcp/tcp_retransmit_skb")
int trace_tcp_retransmit_skb(struct trace_event_raw_tcp_retransmit_skb *args)
{
	struct network_event *evt;

	// Reserve ring buffer entry
	evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt)
		return 0;  // Buffer full, drop event

	// Zero initialize
	__builtin_memset(evt, 0, sizeof(*evt));

	// Mark as retransmit event
	evt->event_type = EVENT_TYPE_RETRANSMIT;

	// Get process context
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->pid = pid_tgid >> 32;

	// Get process name
	bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

	// Copy network info from tracepoint args
	evt->src_port = args->sport;
	evt->dst_port = args->dport;
	evt->family = args->family;
	evt->protocol = IPPROTO_TCP;

	// Copy IP addresses based on family
	if (args->family == AF_INET) {
		__builtin_memcpy(&evt->src_ip, args->saddr, 4);
		__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
	} else if (args->family == AF_INET6) {
		#pragma unroll
		for (int i = 0; i < 16; i++) {
			evt->src_ipv6[i] = args->saddr_v6[i];
			evt->dst_ipv6[i] = args->daddr_v6[i];
		}
	}

	// Extract TCP socket info using BPF_CORE_READ
	// We need: total_retrans and snd_cwnd from struct tcp_sock
	const struct sock *sk = args->skaddr;
	if (sk) {
		// Read tcp_sock fields (requires BTF)
		// old_state = total_retrans (clamped to u8)
		// new_state = snd_cwnd (clamped to u8)

		// SAFETY: tcp_sock contains inet_connection_sock contains inet_sock contains sock
		// We can safely cast to tcp_sock since this is tcp_retransmit_skb
		struct tcp_sock *tp = (struct tcp_sock *)sk;

		// Read total retransmits
		__u8 total_retrans = 0;
		bpf_core_read(&total_retrans, sizeof(total_retrans), &tp->total_retrans);
		evt->old_state = total_retrans;  // Reuse old_state field

		// Read congestion window (clamped to u8)
		__u32 snd_cwnd = 0;
		bpf_core_read(&snd_cwnd, sizeof(snd_cwnd), &tp->snd_cwnd);
		evt->new_state = snd_cwnd > 255 ? 255 : (__u8)snd_cwnd;  // Reuse new_state field
	}

	// Submit event
	bpf_ringbuf_submit(evt, 0);

	// Increment Per-CPU metrics (lock-free)
	metric_inc(METRIC_NETWORK_RETRANSMITS_TOTAL);

	return 0;
}

char LICENSE[] SEC("license") = "GPL";
