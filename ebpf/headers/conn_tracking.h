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

// Per-connection retransmit/RST tracking (stored in LRU map)
// Retransmit rate is not tracked at BPF level — no hook counts all TCP segments.
struct retransmit_stats {
	__u64 retransmits;        // Number of retransmissions
	__u64 last_retransmit_ns; // Timestamp of last retransmit
	__u8  rst_received;       // 1 if RST packet received, 0 otherwise
	__u8  padding[7];         // Align to 8 bytes
};

#endif /* __TAPIO_CONN_TRACKING_H__ */
