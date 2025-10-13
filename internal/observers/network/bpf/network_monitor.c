//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_endian.h>

// Network event structure - MUST match Go NetworkEventBPF exactly (70 bytes packed, 72 with Go alignment)
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
	__u8  old_state;     // offset 51, size 1
	__u8  new_state;     // offset 52, size 1
	__u8  pad;           // offset 53, size 1
	__u8  comm[16];      // offset 54, size 16
} __attribute__((packed));

// Ring buffer for sending events to userspace
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);  // 256KB ring buffer
} events SEC(".maps");

// TCP protocol number
#define IPPROTO_TCP 6

// Address families
#define AF_INET  2
#define AF_INET6 10

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

	// Reserve ring buffer space
	struct network_event *evt = bpf_ringbuf_reserve(&events, sizeof(*evt), 0);
	if (!evt) {
		// Ring buffer full - drop event gracefully
		return 0;
	}

	// Zero-initialize the event (verifier likes this)
	__builtin_memset(evt, 0, sizeof(*evt));

	// Extract process info
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	evt->pid = pid_tgid >> 32;

	// Get process name (comm)
	bpf_get_current_comm(&evt->comm, sizeof(evt->comm));

	// Extract TCP state transition
	evt->old_state = (__u8)args->oldstate;
	evt->new_state = (__u8)args->newstate;

	// Extract protocol and family
	evt->protocol = (__u8)args->protocol;
	evt->family = args->family;

	// Extract ports (already in host byte order from tracepoint)
	evt->src_port = args->sport;
	evt->dst_port = args->dport;

	// Extract IP addresses based on family
	if (args->family == AF_INET) {
		// IPv4 addresses
		// Copy 4 bytes from saddr/daddr to our uint32
		__builtin_memcpy(&evt->src_ip, args->saddr, 4);
		__builtin_memcpy(&evt->dst_ip, args->daddr, 4);
	} else if (args->family == AF_INET6) {
		// IPv6 addresses
		// Verifier requires explicit bounds check on array access
		#pragma unroll
		for (int i = 0; i < 16; i++) {
			evt->src_ipv6[i] = args->saddr_v6[i];
			evt->dst_ipv6[i] = args->daddr_v6[i];
		}
	}

	// Submit event to userspace
	bpf_ringbuf_submit(evt, 0);

	return 0;
}

char LICENSE[] SEC("license") = "GPL";
