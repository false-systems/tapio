//go:build ignore

#ifndef __TAPIO_TCP_H__
#define __TAPIO_TCP_H__

// Shared TCP definitions and helpers for eBPF observers
// Reusable across network, dns, http observers
// Based on Cilium's layered library approach

// TCP protocol number
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// Address families
#define AF_INET  2
#define AF_INET6 10

// TCP states (from linux/tcp.h)
#define TCP_ESTABLISHED 1
#define TCP_SYN_SENT    2
#define TCP_SYN_RECV    3
#define TCP_FIN_WAIT1   4
#define TCP_FIN_WAIT2   5
#define TCP_TIME_WAIT   6
#define TCP_CLOSE       7
#define TCP_CLOSE_WAIT  8
#define TCP_LAST_ACK    9
#define TCP_LISTEN      10
#define TCP_CLOSING     11

// Helper: Get TCP state name (for debugging)
static __always_inline const char *tcp_state_name(__u8 state)
{
	switch (state) {
	case TCP_ESTABLISHED: return "ESTABLISHED";
	case TCP_SYN_SENT:    return "SYN_SENT";
	case TCP_SYN_RECV:    return "SYN_RECV";
	case TCP_FIN_WAIT1:   return "FIN_WAIT1";
	case TCP_FIN_WAIT2:   return "FIN_WAIT2";
	case TCP_TIME_WAIT:   return "TIME_WAIT";
	case TCP_CLOSE:       return "CLOSE";
	case TCP_CLOSE_WAIT:  return "CLOSE_WAIT";
	case TCP_LAST_ACK:    return "LAST_ACK";
	case TCP_LISTEN:      return "LISTEN";
	case TCP_CLOSING:     return "CLOSING";
	default:              return "UNKNOWN";
	}
}

// Connection key for tracking TCP flows (IPv4 only for now)
struct conn_key {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
};

// Helper: Create connection key from tracepoint args
static __always_inline void make_conn_key(struct conn_key *key,
					  __u32 saddr, __u32 daddr,
					  __u16 sport, __u16 dport)
{
	key->saddr = saddr;
	key->daddr = daddr;
	key->sport = sport;
	key->dport = dport;
}

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
// network_monitor.c:105: const struct sock *sk = args->skaddr;
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
//   - srtt_us:        Line 110 (RTT tracking)
//   - total_retrans:  Line 397 (retransmit tracking)
//   - snd_cwnd:       Line 402 (congestion window)
struct tcp_sock {
	struct inet_connection_sock inet_conn;  // Parent struct (inheritance)
	__u32 srtt_us;        // Smoothed RTT in microseconds (divided by 8)
	__u32 total_retrans;  // Total retransmissions for this connection
	__u32 snd_cwnd;       // Congestion window (packets)
} __attribute__((preserve_access_index));

#endif /* __TAPIO_TCP_H__ */
