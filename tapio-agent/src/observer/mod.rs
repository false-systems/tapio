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

/// Metric index for ambiguous storage I/O — must match metrics.h.
#[cfg(target_os = "linux")]
const METRIC_STORAGE_AMBIGUOUS_IO: u32 = 1;

#[cfg(target_os = "linux")]
#[derive(Clone, Default)]
pub struct ConfigCarriers {
    inner: std::sync::Arc<std::sync::Mutex<ConfigCarrierState>>,
}

#[cfg(target_os = "linux")]
#[derive(Clone, Copy)]
struct ConfigCarrier {
    observer: &'static str,
    map_fd: std::os::fd::RawFd,
}

#[cfg(target_os = "linux")]
#[derive(Default)]
struct ConfigCarrierState {
    carriers: Vec<ConfigCarrier>,
    current: Option<tapio_common::ebpf::TapioConfig>,
}

#[cfg(target_os = "linux")]
impl ConfigCarriers {
    pub fn register(&self, observer: &'static str, map_fd: std::os::fd::RawFd) {
        let current = {
            let mut state = self.inner.lock().expect("config carriers lock poisoned");
            state.carriers.push(ConfigCarrier { observer, map_fd });
            state.current
        };
        if let Some(config) = current {
            match bpf_map_update_tapio_config(map_fd, &config) {
                Ok(()) => {
                    tracing::info!(
                        observer,
                        generation = config.generation,
                        flags = config.flags,
                        "config applied to late carrier"
                    );
                }
                Err(e) => {
                    tracing::error!(
                        observer,
                        map = "tapio_config",
                        error = %e,
                        generation = config.generation,
                        "late config apply failed"
                    );
                }
            }
        }
    }

    pub fn update_all(&self, config: &tapio_common::ebpf::TapioConfig) -> Vec<&'static str> {
        let carriers = {
            let mut state = self.inner.lock().expect("config carriers lock poisoned");
            state.current = Some(*config);
            state.carriers.clone()
        };
        let mut failed = Vec::new();
        for carrier in carriers {
            match bpf_map_update_tapio_config(carrier.map_fd, config) {
                Ok(()) => {}
                Err(e) => {
                    tracing::error!(
                        observer = carrier.observer,
                        map = "tapio_config",
                        error = %e,
                        generation = config.generation,
                        "config fan-out failed"
                    );
                    failed.push(carrier.observer);
                }
            }
        }
        failed
    }
}

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
    let map = ebpf.map("tapio_metrics")?;
    Some(map_raw_fd(map))
}

#[cfg(target_os = "linux")]
fn map_raw_fd(map: &aya::maps::Map) -> std::os::fd::RawFd {
    use std::os::fd::{AsFd, AsRawFd};
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
    data.fd().as_fd().as_raw_fd()
}

#[cfg(target_os = "linux")]
fn bpf_map_update_tapio_config(
    map_fd: std::os::fd::RawFd,
    config: &tapio_common::ebpf::TapioConfig,
) -> std::io::Result<()> {
    #[repr(C)]
    struct BpfMapUpdateAttr {
        map_fd: u32,
        _pad: u32,
        key: u64,
        value: u64,
        flags: u64,
    }

    let key = 0_u32;
    let attr = BpfMapUpdateAttr {
        map_fd: map_fd as u32,
        _pad: 0,
        key: &key as *const u32 as u64,
        value: config as *const tapio_common::ebpf::TapioConfig as u64,
        flags: 0, // BPF_ANY
    };

    let ret = unsafe {
        libc::syscall(
            libc::SYS_bpf,
            2_i64, // BPF_MAP_UPDATE_ELEM
            &attr as *const BpfMapUpdateAttr,
            std::mem::size_of::<BpfMapUpdateAttr>() as u64,
        )
    };

    if ret < 0 {
        Err(std::io::Error::last_os_error())
    } else {
        Ok(())
    }
}

#[cfg(target_os = "linux")]
pub fn init_and_register_tapio_config(
    ebpf: &mut aya::Ebpf,
    observer: &'static str,
    config: &tapio_common::ebpf::TapioConfig,
    carriers: &ConfigCarriers,
) -> bool {
    let Some(map) = ebpf.map("tapio_config") else {
        tracing::error!(observer, map = "tapio_config", "config map missing");
        return false;
    };
    let map_fd = map_raw_fd(map);
    match bpf_map_update_tapio_config(map_fd, config) {
        Ok(()) => {
            carriers.register(observer, map_fd);
            tracing::info!(
                observer,
                generation = config.generation,
                flags = config.flags,
                "config carrier registered"
            );
            true
        }
        Err(e) => {
            tracing::error!(
                observer,
                map = "tapio_config",
                error = %e,
                "config init failed"
            );
            false
        }
    }
}

#[cfg(target_os = "linux")]
pub fn load_ebpf(path: &str, observer: &str) -> anyhow::Result<aya::Ebpf> {
    use aya::{EbpfLoader, VerifierLogLevel};

    EbpfLoader::new()
        .verifier_log_level(VerifierLogLevel::VERBOSE | VerifierLogLevel::STATS)
        .load_file(path)
        .map_err(|e| anyhow::anyhow!("failed to load {observer} eBPF object {path}: {e}"))
}

#[cfg(target_os = "linux")]
pub fn should_warn_malformed_event(count: u64) -> bool {
    count <= 10 || count.is_power_of_two()
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod container;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod network;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod node_pmc;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod storage;
