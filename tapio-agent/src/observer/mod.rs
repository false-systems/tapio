/// Max events to drain from a ring buffer per tick before yielding.
/// Prevents unbounded drain from starving the tokio runtime under event storms.
#[cfg(target_os = "linux")]
pub const MAX_DRAIN_PER_TICK: usize = 2048;

/// Ticks between lost-event metric reads (~10s at 10ms/tick).
#[cfg(target_os = "linux")]
const LOST_EVENTS_CHECK_INTERVAL: u64 = 1000;

/// Metric index for lost events — must match METRIC_LOST_EVENTS in metrics.h.
#[cfg(target_os = "linux")]
const METRIC_LOST_EVENTS: u32 = 0;

/// Read a per-CPU u64 metric from a BPF PERCPU_ARRAY map by raw fd.
/// Returns the sum across all CPUs, or 0 on error.
#[cfg(target_os = "linux")]
pub fn read_percpu_sum(map_fd: std::os::fd::RawFd, key: u32, nr_cpus: usize) -> u64 {
    // Per-CPU values must be aligned to 8 bytes per the kernel BPF ABI.
    let mut values = vec![0u64; nr_cpus];

    #[repr(C)]
    struct BpfMapLookupAttr {
        map_fd: u32,
        _pad: u32,
        key: u64,
        value: u64,
        flags: u64,
    }

    let attr = BpfMapLookupAttr {
        map_fd: map_fd as u32,
        _pad: 0,
        key: &key as *const u32 as u64,
        value: values.as_mut_ptr() as u64,
        flags: 0,
    };

    let ret = unsafe {
        libc::syscall(
            libc::SYS_bpf,
            1_i64, // BPF_MAP_LOOKUP_ELEM
            &attr as *const BpfMapLookupAttr,
            std::mem::size_of::<BpfMapLookupAttr>() as u64,
        )
    };

    if ret < 0 { 0 } else { values.iter().sum() }
}

/// Extract raw fd from aya Map, then drop the borrow.
/// Must be called before taking the ring buffer map (borrow exclusion).
#[cfg(target_os = "linux")]
pub fn metrics_map_fd(ebpf: &aya::Ebpf) -> Option<std::os::fd::RawFd> {
    use std::os::fd::{AsFd, AsRawFd};
    let map = ebpf.map("tapio_metrics")?;
    let data = match map {
        aya::maps::Map::Array(d)
        | aya::maps::Map::BloomFilter(d)
        | aya::maps::Map::CpuMap(d)
        | aya::maps::Map::DevMap(d)
        | aya::maps::Map::DevMapHash(d)
        | aya::maps::Map::HashMap(d)
        | aya::maps::Map::LpmTrie(d)
        | aya::maps::Map::LruHashMap(d)
        | aya::maps::Map::PerCpuArray(d)
        | aya::maps::Map::PerCpuHashMap(d)
        | aya::maps::Map::PerCpuLruHashMap(d)
        | aya::maps::Map::PerfEventArray(d)
        | aya::maps::Map::ProgramArray(d)
        | aya::maps::Map::Queue(d)
        | aya::maps::Map::RingBuf(d)
        | aya::maps::Map::SockHash(d)
        | aya::maps::Map::SockMap(d)
        | aya::maps::Map::Stack(d)
        | aya::maps::Map::StackTraceMap(d)
        | aya::maps::Map::Unsupported(d)
        | aya::maps::Map::XskMap(d) => d,
    };
    Some(data.fd().as_fd().as_raw_fd())
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod container;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod network;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod node_pmc;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod storage;
