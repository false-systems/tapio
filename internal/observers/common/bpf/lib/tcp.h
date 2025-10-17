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

#endif /* __TAPIO_TCP_H__ */
