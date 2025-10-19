//go:build ignore

#ifndef __TAPIO_CONN_TRACKING_H__
#define __TAPIO_CONN_TRACKING_H__

// Shared connection tracking library for all Tapio observers
// Reusable across network, dns, http observers
// Based on Cilium's layered library approach and Brendan Gregg patterns

// Connection key for tracking TCP/UDP flows (IPv4 only for now)
// Used as key in LRU hash maps for connection state tracking
struct conn_key {
	__u32 saddr;  // Source IP address (network byte order)
	__u32 daddr;  // Destination IP address (network byte order)
	__u16 sport;  // Source port (network byte order)
	__u16 dport;  // Destination port (network byte order)
};

// Helper: Create connection key from IP/port (network byte order)
static __always_inline void make_conn_key(struct conn_key *key,
					  __u32 saddr, __u32 daddr,
					  __u16 sport, __u16 dport)
{
	key->saddr = saddr;
	key->daddr = daddr;
	key->sport = sport;
	key->dport = dport;
}

#endif /* __TAPIO_CONN_TRACKING_H__ */
