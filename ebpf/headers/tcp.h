#ifndef __TAPIO_TCP_H__
#define __TAPIO_TCP_H__

// Shared TCP definitions and helpers for eBPF observers
// Reusable across network, dns, http observers
// Based on Cilium's layered library approach

// Import shared connection tracking library
#include "conn_tracking.h"

// TCP protocol/family/state constants are defined in vmlinux_minimal.h
// Do NOT redefine them here — include vmlinux_minimal.h before this header.

// Connection tracking structs defined in conn_tracking.h (shared):
//   - conn_key: Connection identifier (saddr, daddr, sport, dport)
//   - retransmit_stats: Retransmit/RST tracking per connection

// ============================================================================
// Kernel Socket Structures (Minimal CO-RE Definitions)
// ============================================================================
// These structs are INTENTIONALLY INCOMPLETE - we only define fields we access.
// The __attribute__((preserve_access_index)) tells the compiler to generate
// CO-RE relocations, allowing the loader to patch field offsets at runtime
// based on the running kernel's BTF (BPF Type Format).
//
// This is the "Cilium-style" approach: minimal structs with CO-RE = portability.
//
// USAGE: Always use bpf_core_read() to access these fields, never direct access!
// Example:
//   struct tcp_sock *tp = (struct tcp_sock *)sk;
//   __u32 rtt = 0;
//   if (bpf_core_read(&rtt, sizeof(rtt), &tp->srtt_us) != 0)
//       return 0;  // Failed read
//
// WHY THIS WORKS ACROSS KERNEL VERSIONS:
//   Linux 5.10: tcp_sock->srtt_us at offset 728
//   Linux 6.1:  tcp_sock->srtt_us at offset 736 (fields added before it)
//   → Same binary works on both! Loader patches offset at load time.
// ============================================================================

// Base socket structure (all sockets inherit from this)
// We don't access any sock fields directly, but need the type for safe casting.
// Used in network_monitor.c to cast tracepoint skaddr to tcp_sock.
struct sock {
	// Opaque - we don't access fields, just use for type safety
	char __opaque[0];
} __attribute__((preserve_access_index));

// Internet connection socket (TCP/UDP sockets inherit from this)
// Intermediate struct in the inheritance chain: sock → inet_connection_sock → tcp_sock
struct inet_connection_sock {
	struct sock icsk_inet;  // Parent struct (inheritance)
} __attribute__((preserve_access_index));

// TCP-specific socket (final struct in chain)
// Contains TCP-specific fields like RTT, retransmit counters, congestion window.
//
// FIELDS WE ACCESS (network_monitor.c):
//   - srtt_us:        trace_inet_sock_set_state (RTT tracking)
//   - total_retrans:  trace_tcp_retransmit_skb (retransmit tracking)
//   - snd_cwnd:       trace_tcp_retransmit_skb (congestion window)
struct tcp_sock {
	struct inet_connection_sock inet_conn;  // Parent struct (inheritance)
	__u32 srtt_us;        // Smoothed RTT in microseconds (divided by 8)
	__u32 total_retrans;  // Total retransmissions for this connection
	__u32 snd_cwnd;       // Congestion window (packets)
} __attribute__((preserve_access_index));

#endif /* __TAPIO_TCP_H__ */
