#ifndef __TAPIO_CONFIG_H__
#define __TAPIO_CONFIG_H__

/*
 * Agent -> eBPF runtime config ABI.
 *
 * This is Tapio's private fixed-layout ABI, not CO-RE. Any layout change that
 * changes size, field order, field meaning, or interpretation must bump
 * TAPIO_CONFIG_ABI_VERSION and the Rust mirror in tapio-common/src/ebpf.rs.
 */
#define TAPIO_CONFIG_ABI_VERSION 2

#define TAPIO_F_NETWORK   (1ULL << 0)
#define TAPIO_F_STORAGE   (1ULL << 1)
#define TAPIO_F_CONTAINER (1ULL << 2)
#define TAPIO_F_NODE_PMC  (1ULL << 3)

#define TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES 16

struct tapio_config {
	__u32 abi_version;            /* TAPIO_CONFIG_ABI_VERSION */
	__u32 generation;             /* profile generation stamped into events */
	__u64 flags;                  /* TAPIO_F_* enable bits */
	__u64 slow_io_threshold_ns;   /* storage latency warning; 0 = inert */
	__u64 io_latency_critical_ns; /* storage latency critical; 0 = inert */
	__u64 conn_refused_window_ns;
	__u32 conn_refused_min_count;
	__u32 rtt_spike_multiplier;   /* RTT spike vs baseline ratio; 0 = inert */
	__u32 rtt_spike_abs_us;       /* RTT spike absolute threshold; 0 = inert */
	__u32 rtt_min_baseline_samples;
	__u32 ignore_exit_count;
	__s32 ignore_exit_codes[TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES];
	__u32 _pad;
};

_Static_assert(sizeof(struct tapio_config) == 128, "tapio_config size");
_Static_assert(__builtin_offsetof(struct tapio_config, abi_version) == 0, "tapio_config abi_version offset");
_Static_assert(__builtin_offsetof(struct tapio_config, generation) == 4, "tapio_config generation offset");
_Static_assert(__builtin_offsetof(struct tapio_config, flags) == 8, "tapio_config flags offset");
_Static_assert(__builtin_offsetof(struct tapio_config, slow_io_threshold_ns) == 16, "tapio_config slow_io_threshold_ns offset");
_Static_assert(__builtin_offsetof(struct tapio_config, io_latency_critical_ns) == 24, "tapio_config io_latency_critical_ns offset");
_Static_assert(__builtin_offsetof(struct tapio_config, conn_refused_window_ns) == 32, "tapio_config conn_refused_window_ns offset");
_Static_assert(__builtin_offsetof(struct tapio_config, conn_refused_min_count) == 40, "tapio_config conn_refused_min_count offset");
_Static_assert(__builtin_offsetof(struct tapio_config, rtt_spike_multiplier) == 44, "tapio_config rtt_spike_multiplier offset");
_Static_assert(__builtin_offsetof(struct tapio_config, rtt_spike_abs_us) == 48, "tapio_config rtt_spike_abs_us offset");
_Static_assert(__builtin_offsetof(struct tapio_config, rtt_min_baseline_samples) == 52, "tapio_config rtt_min_baseline_samples offset");
_Static_assert(__builtin_offsetof(struct tapio_config, ignore_exit_count) == 56, "tapio_config ignore_exit_count offset");
_Static_assert(__builtin_offsetof(struct tapio_config, ignore_exit_codes) == 60, "tapio_config ignore_exit_codes offset");
_Static_assert(__builtin_offsetof(struct tapio_config, _pad) == 124, "tapio_config _pad offset");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct tapio_config);
} tapio_config SEC(".maps");

static __always_inline int tapio_config_snapshot(struct tapio_config *dst)
{
	__u32 key = 0;
	struct tapio_config *cfg = bpf_map_lookup_elem(&tapio_config, &key);
	if (!cfg) {
		return 0;
	}
	__builtin_memcpy(dst, cfg, sizeof(*dst));
	return dst->abi_version == TAPIO_CONFIG_ABI_VERSION;
}

static __always_inline __u32 tapio_clamp_ignore_exit_count(__u32 count)
{
	return count > TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES ? TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES : count;
}

#endif /* __TAPIO_CONFIG_H__ */
