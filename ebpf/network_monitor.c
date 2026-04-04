// SPDX-License-Identifier: GPL-2.0

#include "headers/vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Shared helpers (Cilium-style layered lib)
#include "headers/tcp.h"
#include "headers/metrics.h"

// Event types for distinguishing tracepoint sources
#define EVENT_TYPE_STATE_CHANGE  0  // inet_sock_set_state
#define EVENT_TYPE_RST_RECEIVED  1  // tcp_receive_reset
#define EVENT_TYPE_RETRANSMIT    2  // tcp_retransmit_skb
#define EVENT_TYPE_RTT_SPIKE     3  // RTT spike detection

// Network event structure - MUST match Rust NetworkEvent in tapio-common/src/ebpf.rs (80 bytes packed)
// Each event type uses its own named fields — no overloading.
struct network_event {
	__u32 pid;              // offset 0, size 4
	__u32 src_ip;           // offset 4, size 4
	__u32 dst_ip;           // offset 8, size 4
	__u8  src_ipv6[16];     // offset 12, size 16
	__u8  dst_ipv6[16];     // offset 28, size 16
	__u16 src_port;         // offset 44, size 2
	__u16 dst_port;         // offset 46, size 2
	__u16 family;           // offset 48, size 2
	__u8  protocol;         // offset 50, size 1
	__u8  event_type;       // offset 51, size 1
	__u16 old_state;        // offset 52, size 2 - TCP state (state change events)
	__u16 new_state;        // offset 54, size 2 - TCP state (state change events)
	__u16 rtt_baseline_ms;  // offset 56, size 2 - baseline RTT in ms (RTT spike events)
	__u16 rtt_current_ms;   // offset 58, size 2 - current RTT in ms (RTT spike events)
	__u16 total_retrans;    // offset 60, size 2 - total retransmits (retransmit events)
	__u16 snd_cwnd;         // offset 62, size 2 - congestion window (retransmit events)
	__u8  comm[16];         // offset 64, size 16
} __attribute__((packed));  // 80 bytes

// Ring buffer for sending events to userspace
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);  // 256KB ring buffer
} events SEC(".maps");

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
	__uint(max_entries, 10000);
	__type(key, struct conn_key);
	__type(value, struct rtt_baseline);
	__uint(pinning, LIBBPF_PIN_BY_NAME);
} baseline_rtt SEC(".maps");

// Connection statistics tracking (LRU auto-evicts old connections)
// Tracks retransmit counts and RST flags per connection
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 10000);
	__type(key, struct conn_key);
	__type(value, struct retransmit_stats);
	__uint(pinning, LIBBPF_PIN_BY_NAME);
} conn_stats SEC(".maps");

// RTT states
#define RTT_STATE_NO_BASELINE 0
#define RTT_STATE_LEARNING    1
#define RTT_STATE_STABLE      2

// Default thresholds — overridable via config map
#define LEARNING_SAMPLES 5                     // Collect 5 samples before going STABLE
#define STALE_THRESHOLD_NS 3600000000000ULL    // 1 hour
#define RTT_SPIKE_RATIO    2                   // >Nx baseline triggers spike
#define RTT_SPIKE_ABS_US   500000              // >500ms absolute triggers spike

// Config map indices
#define CONFIG_RTT_SPIKE_RATIO    0
#define CONFIG_RTT_SPIKE_ABS_US   1
#define CONFIG_MAX_ENTRIES        2

// Runtime-configurable thresholds — populated by userspace at load time
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, CONFIG_MAX_ENTRIES);
	__type(key, __u32);
	__type(value, __u64);
} config SEC(".maps");

static __always_inline __u64 get_config(__u32 idx, __u64 default_val) {
	__u64 *val = bpf_map_lookup_elem(&config, &idx);
	return val && *val > 0 ? *val : default_val;
}

/* Tracepoint argument structs: hardcoded layout is intentional.
 * Tracepoint ABIs are stable across kernel versions (unlike internal structs).
 * CO-RE is used for tcp_sock/task_struct field access where offsets vary. */

// Tracepoint arguments for sock/inet_sock_set_state
//
// NOTE: This struct layout matches Linux kernel 5.8+
// Verified against: /sys/kernel/debug/tracing/events/sock/inet_sock_set_state/format
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
	if (args->protocol != IPPROTO_TCP) {
		return 0;
	}

	// Get tcp_sock from skaddr to read RTT
	const struct sock *sk = (const struct sock *)args->skaddr;
	struct tcp_sock *tp = (struct tcp_sock *)sk;

	// Read smoothed RTT from tcp_sock (srtt_us is in microseconds, divided by 8)
	__u32 srtt_us = 0;
	if (bpf_core_read(&srtt_us, sizeof(srtt_us), &tp->srtt_us) != 0) {
		goto skip_rtt_tracking;
	}
	__u32 rtt_us = srtt_us >> 3;

	__u64 now_ns = bpf_ktime_get_ns();

	// Create connection key (IPv4 and IPv6)
	struct conn_key key = {0};
	if (args->family == AF_INET) {
		fill_conn_key_v4(&key, args->saddr, args->daddr,
		                 args->sport, args->dport);
	} else if (args->family == AF_INET6) {
		fill_conn_key_v6(&key, args->saddr_v6, args->daddr_v6,
		                 args->sport, args->dport);
	} else {
		goto skip_rtt_tracking;
	}

	// RTT tracking for ESTABLISHED connections with valid RTT
	if (args->newstate == TCP_ESTABLISHED && rtt_us > 0) {
		struct rtt_baseline *baseline = bpf_map_lookup_elem(&baseline_rtt, &key);

		if (!baseline) {
			struct rtt_baseline new_baseline = {
				.baseline_us = rtt_us,
				.sample_count = 1,
				.state = RTT_STATE_LEARNING,
				.last_update_ns = now_ns,
				.last_activity_ns = now_ns,
			};
			bpf_map_update_elem(&baseline_rtt, &key, &new_baseline, BPF_NOEXIST);
		} else {
			baseline->last_activity_ns = now_ns;

			if (baseline->state == RTT_STATE_LEARNING) {
				baseline->sample_count++;
				baseline->baseline_us = (baseline->baseline_us * (baseline->sample_count - 1) + rtt_us) / baseline->sample_count;

				if (baseline->sample_count >= LEARNING_SAMPLES) {
					if (baseline->baseline_us > 100000) {
						baseline->sample_count = 0;
						baseline->baseline_us = 0;
						baseline->state = RTT_STATE_LEARNING;
						metric_inc(METRIC_NETWORK_BASELINE_REJECTED);
					} else {
						baseline->state = RTT_STATE_STABLE;
					}
				}

				bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
			} else if (baseline->state == RTT_STATE_STABLE) {
				if (now_ns - baseline->last_update_ns > STALE_THRESHOLD_NS) {
					baseline->baseline_us = (baseline->baseline_us * 9 + rtt_us) / 10;
					baseline->last_update_ns = now_ns;
					bpf_map_update_elem(&baseline_rtt, &key, baseline, BPF_EXIST);
				}

				// Check for RTT spike: >Nx baseline OR absolute threshold
				__u64 ratio = get_config(CONFIG_RTT_SPIKE_RATIO, RTT_SPIKE_RATIO);
				__u64 abs_us = get_config(CONFIG_RTT_SPIKE_ABS_US, RTT_SPIKE_ABS_US);
				if (rtt_us > (baseline->baseline_us * ratio) || rtt_us > abs_us) {
					struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
					if (!evt) {
						metric_inc(METRIC_LOST_EVENTS);
					} else {
						__builtin_memset(evt, 0, sizeof(*evt));

						__u64 pid_tgid = bpf_get_current_pid_tgid();
						evt->pid = pid_tgid >> 32;
						bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

						evt->event_type = EVENT_TYPE_RTT_SPIKE;
						evt->protocol = (__u8)args->protocol;
						evt->family = args->family;
						evt->src_port = args->sport;
						evt->dst_port = args->dport;

						if (args->family == AF_INET) {
							__builtin_memcpy(&evt->src_ip, args->saddr, 4);
							__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
						} else if (args->family == AF_INET6) {
							__builtin_memcpy(evt->src_ipv6, args->saddr_v6, 16);
							__builtin_memcpy(evt->dst_ipv6, args->daddr_v6, 16);
						}

						__u32 baseline_ms = baseline->baseline_us / 1000;
						__u32 current_ms = rtt_us / 1000;
						evt->rtt_baseline_ms = baseline_ms > 65535 ? 65535 : (__u16)baseline_ms;
						evt->rtt_current_ms = current_ms > 65535 ? 65535 : (__u16)current_ms;

						bpf_ringbuf_submit(evt, 0);
					}
				}
			}
		}
	}

skip_rtt_tracking:
	// Emit regular state change event for important transitions
	if (args->oldstate != args->newstate &&
	    (args->newstate == TCP_CLOSE || args->oldstate == TCP_SYN_SENT ||
	     args->oldstate == TCP_SYN_RECV || args->newstate == TCP_LISTEN)) {

		struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
		if (!evt) {
			metric_inc(METRIC_LOST_EVENTS);
			return 0;
		}

		__builtin_memset(evt, 0, sizeof(*evt));

		__u64 pid_tgid = bpf_get_current_pid_tgid();
		evt->pid = pid_tgid >> 32;
		bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

		evt->event_type = EVENT_TYPE_STATE_CHANGE;
		evt->old_state = (__u16)args->oldstate;
		evt->new_state = (__u16)args->newstate;
		evt->protocol = (__u8)args->protocol;
		evt->family = args->family;
		evt->src_port = args->sport;
		evt->dst_port = args->dport;

		if (args->family == AF_INET) {
			__builtin_memcpy(&evt->src_ip, args->saddr, 4);
			__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
		} else if (args->family == AF_INET6) {
			__builtin_memcpy(evt->src_ipv6, args->saddr_v6, 16);
			__builtin_memcpy(evt->dst_ipv6, args->daddr_v6, 16);
		}

		bpf_ringbuf_submit(evt, 0);
	}

	return 0;
}

// Tracepoint arguments for tcp/tcp_receive_reset
// Uses tcp_event_sk event class — no state field (unlike tcp_retransmit_skb)
// Verify: cat /sys/kernel/debug/tracing/events/tcp/tcp_receive_reset/format
struct trace_event_raw_tcp_receive_reset {
	__u64 unused;       // Common tracepoint header
	const void *skaddr;
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
	struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		metric_inc(METRIC_LOST_EVENTS);
		return 0;
	}

	__builtin_memset(evt, 0, sizeof(*evt));

	evt->event_type = EVENT_TYPE_RST_RECEIVED;

	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->pid = pid_tgid >> 32;
	bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

	evt->protocol = IPPROTO_TCP;
	evt->family = args->family;
	evt->src_port = args->sport;
	evt->dst_port = args->dport;

	if (args->family == AF_INET) {
		__builtin_memcpy(&evt->src_ip, args->saddr, 4);
		__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
	} else if (args->family == AF_INET6) {
		__builtin_memcpy(evt->src_ipv6, args->saddr_v6, 16);
		__builtin_memcpy(evt->dst_ipv6, args->daddr_v6, 16);
	}

	// Update conn_stats: mark RST received (IPv4 and IPv6)
	{
		struct conn_key key = {0};
		if (args->family == AF_INET) {
			fill_conn_key_v4(&key, args->saddr, args->daddr,
			                 args->sport, args->dport);
		} else if (args->family == AF_INET6) {
			fill_conn_key_v6(&key, args->saddr_v6, args->daddr_v6,
			                 args->sport, args->dport);
		} else {
			goto submit_rst;
		}

		struct retransmit_stats *stats = bpf_map_lookup_elem(&conn_stats, &key);
		if (stats) {
			stats->rst_received = 1;
		} else {
			struct retransmit_stats new_stats = {
				.retransmits = 0,
				.last_retransmit_ns = 0,
				.rst_received = 1,
			};
			bpf_map_update_elem(&conn_stats, &key, &new_stats, BPF_NOEXIST);
		}
	}

submit_rst:
	bpf_ringbuf_submit(evt, 0);
	metric_inc(METRIC_NETWORK_RST_TOTAL);

	return 0;
}

// Tracepoint arguments for tcp/tcp_retransmit_skb
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

	evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		metric_inc(METRIC_LOST_EVENTS);
		return 0;
	}

	__builtin_memset(evt, 0, sizeof(*evt));

	evt->event_type = EVENT_TYPE_RETRANSMIT;

	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->pid = pid_tgid >> 32;
	bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

	evt->src_port = args->sport;
	evt->dst_port = args->dport;
	evt->family = args->family;
	evt->protocol = IPPROTO_TCP;

	if (args->family == AF_INET) {
		__builtin_memcpy(&evt->src_ip, args->saddr, 4);
		__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
	} else if (args->family == AF_INET6) {
		__builtin_memcpy(evt->src_ipv6, args->saddr_v6, 16);
		__builtin_memcpy(evt->dst_ipv6, args->daddr_v6, 16);
	}

	// Update conn_stats: track retransmits (IPv4 and IPv6)
	{
		struct conn_key key = {0};
		if (args->family == AF_INET) {
			fill_conn_key_v4(&key, args->saddr, args->daddr,
			                 args->sport, args->dport);
		} else if (args->family == AF_INET6) {
			fill_conn_key_v6(&key, args->saddr_v6, args->daddr_v6,
			                 args->sport, args->dport);
		} else {
			goto read_tcp_sock;
		}

		struct retransmit_stats *stats = bpf_map_lookup_elem(&conn_stats, &key);
		if (stats) {
			stats->retransmits++;
			stats->last_retransmit_ns = bpf_ktime_get_ns();
		} else {
			struct retransmit_stats new_stats = {
				.retransmits = 1,
				.last_retransmit_ns = bpf_ktime_get_ns(),
				.rst_received = 0,
			};
			bpf_map_update_elem(&conn_stats, &key, &new_stats, BPF_NOEXIST);
		}
	}

read_tcp_sock:
	// Read tcp_sock fields (requires BTF)
	{
		const struct sock *sk = args->skaddr;
		if (sk) {
			struct tcp_sock *tp = (struct tcp_sock *)sk;

			__u32 total_retrans = 0;
			if (bpf_core_read(&total_retrans, sizeof(total_retrans), &tp->total_retrans) != 0) {
				bpf_ringbuf_discard(evt, 0);
				return 0;
			}
			evt->total_retrans = total_retrans > 65535 ? 65535 : (__u16)total_retrans;

			__u32 snd_cwnd = 0;
			if (bpf_core_read(&snd_cwnd, sizeof(snd_cwnd), &tp->snd_cwnd) != 0) {
				bpf_ringbuf_discard(evt, 0);
				return 0;
			}
			evt->snd_cwnd = snd_cwnd > 65535 ? 65535 : (__u16)snd_cwnd;
		}
	}

	bpf_ringbuf_submit(evt, 0);
	metric_inc(METRIC_NETWORK_RETRANSMITS_TOTAL);

	return 0;
}

char LICENSE[] SEC("license") = "GPL";
