#ifndef __TAPIO_CONN_TRACKING_H__
#define __TAPIO_CONN_TRACKING_H__

// Shared connection tracking library for all Tapio observers
// Reusable across network, dns, http observers
// Based on Cilium's layered library approach and Brendan Gregg patterns

// Connection key for tracking TCP/UDP flows (IPv4 and IPv6)
// IPv4 addresses are stored as IPv4-mapped IPv6: ::ffff:a.b.c.d
// This gives a single key format for both families, so IPv4 and IPv6
// connections always get distinct map entries.
struct conn_key {
	__u8  saddr[16];  // Source address (IPv4-mapped for v4: ::ffff:a.b.c.d)
	__u8  daddr[16];  // Destination address (IPv4-mapped for v4)
	__u16 sport;      // Source port
	__u16 dport;      // Destination port
	__u8  family;     // AF_INET or AF_INET6
	__u8  _pad[3];    // Alignment
};

// Fill conn_key for an IPv4 connection (stored as IPv4-mapped IPv6)
static __always_inline void fill_conn_key_v4(
	struct conn_key *key,
	__u8 saddr[4], __u8 daddr[4],
	__u16 sport, __u16 dport)
{
	__builtin_memset(key, 0, sizeof(*key));
	key->saddr[10] = 0xff; key->saddr[11] = 0xff;
	__builtin_memcpy(&key->saddr[12], saddr, 4);
	key->daddr[10] = 0xff; key->daddr[11] = 0xff;
	__builtin_memcpy(&key->daddr[12], daddr, 4);
	key->sport = sport;
	key->dport = dport;
	key->family = AF_INET;
}

// Fill conn_key for an IPv6 connection
static __always_inline void fill_conn_key_v6(
	struct conn_key *key,
	__u8 saddr[16], __u8 daddr[16],
	__u16 sport, __u16 dport)
{
	__builtin_memset(key, 0, sizeof(*key));
	__builtin_memcpy(key->saddr, saddr, 16);
	__builtin_memcpy(key->daddr, daddr, 16);
	key->sport = sport;
	key->dport = dport;
	key->family = AF_INET6;
}

// Per-connection retransmit/RST tracking (stored in LRU map)
// Retransmit rate is not tracked at BPF level — no hook counts all TCP segments.
struct retransmit_stats {
	__u64 retransmits;        // Number of retransmissions
	__u64 last_retransmit_ns; // Timestamp of last retransmit
	__u8  rst_received;       // 1 if RST packet received, 0 otherwise
	__u8  padding[7];         // Align to 8 bytes
};

#endif /* __TAPIO_CONN_TRACKING_H__ */
