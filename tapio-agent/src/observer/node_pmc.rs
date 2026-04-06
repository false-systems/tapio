use tapio_common::ebpf::*;
use tapio_common::events::*;
use tapio_common::occurrence::{Occurrence, Outcome, Severity};

pub struct PmcThresholds {
    pub stall_pct_warning: f64,
    pub stall_pct_critical: f64,
    pub ipc_degradation: f64,
}

pub struct ClassifiedAnomaly {
    pub event_type: &'static str,
    pub severity: Severity,
    pub outcome: Outcome,
    pub error_code: &'static str,
    pub error_message: String,
}

pub fn classify(event: &PmcEvent, thresholds: &PmcThresholds) -> Option<ClassifiedAnomaly> {
    let cpu = event.cpu;
    let ipc = event.ipc();
    let stall_pct = event.stall_pct();
    let cycles = event.cycles;

    if stall_pct >= thresholds.stall_pct_critical {
        return Some(ClassifiedAnomaly {
            event_type: NODE_CPU_STALL,
            severity: Severity::Critical,
            outcome: Outcome::InProgress,
            error_code: "CPU_STALL",
            error_message: format!("CPU {cpu} stall {stall_pct:.1}% (ipc={ipc:.2})"),
        });
    }

    if stall_pct >= thresholds.stall_pct_warning {
        return Some(ClassifiedAnomaly {
            event_type: NODE_MEMORY_PRESSURE,
            severity: Severity::Warning,
            outcome: Outcome::InProgress,
            error_code: "MEMORY_PRESSURE",
            error_message: format!(
                "CPU {cpu} memory pressure stall {stall_pct:.1}% (ipc={ipc:.2})",
            ),
        });
    }

    if ipc < thresholds.ipc_degradation && cycles > 0 {
        return Some(ClassifiedAnomaly {
            event_type: NODE_IPC_DEGRADATION,
            severity: Severity::Warning,
            outcome: Outcome::InProgress,
            error_code: "IPC_DEGRADATION",
            error_message: format!("CPU {cpu} low IPC {ipc:.2} (stall {stall_pct:.1}%)"),
        });
    }

    None
}

pub fn build_occurrence(
    event: &PmcEvent,
    anomaly: &ClassifiedAnomaly,
    boot_offset_ns: u64,
) -> Occurrence {
    let cpu = event.cpu;
    let cycles = event.cycles;
    let instructions = event.instructions;
    let stall_cycles = event.stall_cycles;
    let ipc = event.ipc();
    let stall_pct = event.stall_pct();
    let timestamp = event.timestamp;

    let wall_ns = boot_offset_ns.wrapping_add(timestamp);
    Occurrence::new_at(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
        wall_ns,
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "cpu": cpu,
        "cycles": cycles,
        "instructions": instructions,
        "stall_cycles": stall_cycles,
        "ipc": ipc,
        "stall_pct": stall_pct,
        "timestamp_ns": timestamp,
    }))
}

#[cfg(target_os = "linux")]
const HW_CPU_CYCLES: u64 = 0;
#[cfg(target_os = "linux")]
const HW_INSTRUCTIONS: u64 = 1;
#[cfg(target_os = "linux")]
const HW_STALLED_CYCLES_BACKEND: u64 = 8;

/// Open a hardware performance counter for a specific CPU.
/// Uses the raw perf_event_open(2) syscall with a manually-laid-out attr struct.
#[cfg(target_os = "linux")]
fn perf_event_open_hw(config: u64, cpu: i32) -> std::io::Result<std::os::fd::OwnedFd> {
    use std::os::fd::FromRawFd;

    // perf_event_attr layout (kernel 5.8+, 136 bytes):
    //   offset 0:  type (u32)   — PERF_TYPE_HARDWARE = 0
    //   offset 4:  size (u32)   — sizeof(perf_event_attr)
    //   offset 8:  config (u64) — HW counter ID
    //   offset 40: flags (u64)  — bit 6 = exclude_hv
    const ATTR_SIZE: usize = 136;
    let mut attr = [0u8; ATTR_SIZE];

    // type = PERF_TYPE_HARDWARE (0) — already zero
    // size
    attr[4..8].copy_from_slice(&(ATTR_SIZE as u32).to_ne_bytes());
    // config = HW counter ID
    attr[8..16].copy_from_slice(&config.to_ne_bytes());
    // flags: exclude_hv (bit 6)
    let flags: u64 = 1 << 6;
    attr[40..48].copy_from_slice(&flags.to_ne_bytes());

    unsafe {
        let fd = libc::syscall(
            libc::SYS_perf_event_open,
            attr.as_ptr(),
            -1_i32, // all processes
            cpu,
            -1_i32, // no group leader
            0_u64,  // flags
        );

        if fd < 0 {
            return Err(std::io::Error::last_os_error());
        }

        Ok(std::os::fd::OwnedFd::from_raw_fd(fd as i32))
    }
}

/// Insert a perf event fd into a BPF PERF_EVENT_ARRAY map.
#[cfg(target_os = "linux")]
fn bpf_map_set_perf_fd(
    map_fd: std::os::fd::RawFd,
    cpu: u32,
    perf_fd: std::os::fd::RawFd,
) -> std::io::Result<()> {
    #[repr(C)]
    struct BpfMapUpdateAttr {
        map_fd: u32,
        _pad: u32,
        key: u64,
        value: u64,
        flags: u64,
    }

    let key = cpu;
    let value = perf_fd as u32;

    let attr = BpfMapUpdateAttr {
        map_fd: map_fd as u32,
        _pad: 0,
        key: &key as *const u32 as u64,
        value: &value as *const u32 as u64,
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

/// Extract raw fd from an aya Map. All variants wrap MapData — exhaustive match
/// ensures a compile error if aya adds new variants in a future version.
#[cfg(target_os = "linux")]
fn map_raw_fd(map: &aya::maps::Map) -> std::os::fd::RawFd {
    use aya::maps::Map;
    use std::os::fd::{AsFd, AsRawFd};
    let data = match map {
        Map::Array(d)
        | Map::BloomFilter(d)
        | Map::CpuMap(d)
        | Map::DevMap(d)
        | Map::DevMapHash(d)
        | Map::HashMap(d)
        | Map::LpmTrie(d)
        | Map::LruHashMap(d)
        | Map::PerCpuArray(d)
        | Map::PerCpuHashMap(d)
        | Map::PerCpuLruHashMap(d)
        | Map::PerfEventArray(d)
        | Map::ProgramArray(d)
        | Map::Queue(d)
        | Map::RingBuf(d)
        | Map::SockHash(d)
        | Map::SockMap(d)
        | Map::Stack(d)
        | Map::StackTraceMap(d)
        | Map::Unsupported(d)
        | Map::XskMap(d) => d,
    };
    data.fd().as_fd().as_raw_fd()
}

/// Populate a PERF_EVENT_ARRAY map with a perf event fd for a given CPU.
#[cfg(target_os = "linux")]
fn set_perf_event_map(
    ebpf: &mut aya::Ebpf,
    map_name: &str,
    cpu: u32,
    fd: &std::os::fd::OwnedFd,
) -> anyhow::Result<()> {
    use std::os::fd::AsRawFd;
    let map_fd = {
        let map = ebpf
            .map_mut(map_name)
            .ok_or_else(|| anyhow::anyhow!("map not found: {map_name}"))?;
        map_raw_fd(map)
    };
    bpf_map_set_perf_fd(map_fd, cpu, fd.as_raw_fd())?;
    Ok(())
}

/// Load eBPF program and start the PMC observation loop.
#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    boot_offset_ns: u64,
    thresholds: PmcThresholds,
    metrics: &crate::metrics::TapioMetrics,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::Ebpf;
    use aya::maps::RingBuf;
    use aya::programs::perf_event::{PerfEvent, PerfEventScope, PerfTypeId, SamplePolicy};
    use std::os::fd::OwnedFd;
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading PMC eBPF program");
    let mut ebpf = Ebpf::load_file(ebpf_path)?;

    let num_cpus = aya::util::nr_cpus().map_err(|(msg, e)| anyhow::anyhow!("{msg}: {e}"))? as u32;
    tracing::info!(num_cpus, "detected CPUs for PMC");

    // Open hardware performance counters and populate PERF_EVENT_ARRAY maps.
    // Keep FDs alive for the duration of the observer.
    let mut perf_fds: Vec<OwnedFd> = Vec::new();

    for cpu in 0..num_cpus {
        let cpu_i32 = cpu as i32;
        let cycles = match perf_event_open_hw(HW_CPU_CYCLES, cpu_i32) {
            Ok(fd) => fd,
            Err(e) => {
                if cpu == 0 {
                    tracing::warn!(error = %e, "perf_event_open failed — PMC observer disabled");
                    return Ok(());
                }
                tracing::warn!(cpu, error = %e, "perf_event_open failed for CPU, skipping");
                continue;
            }
        };
        let instructions = match perf_event_open_hw(HW_INSTRUCTIONS, cpu_i32) {
            Ok(fd) => fd,
            Err(e) => {
                tracing::warn!(cpu, error = %e, "perf_event_open instructions failed, skipping");
                continue;
            }
        };
        let stalls = match perf_event_open_hw(HW_STALLED_CYCLES_BACKEND, cpu_i32) {
            Ok(fd) => fd,
            Err(e) => {
                tracing::warn!(cpu, error = %e, "perf_event_open stalls failed, skipping");
                continue;
            }
        };

        set_perf_event_map(&mut ebpf, "pmc_cycles", cpu, &cycles)?;
        set_perf_event_map(&mut ebpf, "pmc_instructions", cpu, &instructions)?;
        set_perf_event_map(&mut ebpf, "pmc_stalls", cpu, &stalls)?;

        perf_fds.push(cycles);
        perf_fds.push(instructions);
        perf_fds.push(stalls);
    }

    // Load and attach the BPF program to a software timer on each CPU (10 Hz = 100ms).
    let prog: &mut PerfEvent = ebpf
        .program_mut("sample_pmc")
        .ok_or_else(|| anyhow::anyhow!("program not found: sample_pmc"))?
        .try_into()?;
    prog.load()?;

    for cpu in 0..num_cpus {
        prog.attach(
            PerfTypeId::Software,
            0, // PERF_COUNT_SW_CPU_CLOCK
            PerfEventScope::AllProcessesOneCpu { cpu },
            SamplePolicy::Frequency(10),
            false,
        )?;
    }

    let metrics_fd = super::metrics_map_fd(&ebpf);

    let events_map = ebpf
        .map_mut("events")
        .ok_or_else(|| anyhow::anyhow!("map not found: events"))?;
    let mut ring_buf = RingBuf::try_from(events_map)?;

    tracing::info!("PMC observer running");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;
    let mut tick_count: u64 = 0;
    let mut prev_lost: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "PMC observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                tick_count += 1;
                if tick_count.is_multiple_of(super::LOST_EVENTS_CHECK_INTERVAL) && let Some(fd) = metrics_fd {
                    let lost = super::read_percpu_sum(fd, super::METRIC_LOST_EVENTS, num_cpus as usize);
                    if lost > prev_lost {
                        tracing::warn!(
                            observer = "pmc",
                            lost_total = lost,
                            lost_delta = lost - prev_lost,
                            "ring buffer events lost"
                        );
                        prev_lost = lost;
                    }
                }
                let drained = tokio::task::block_in_place(|| {
                    let mut count = 0usize;
                    while let Some(item) = ring_buf.next() {
                        let data: &[u8] = item.as_ref();
                        if data.len() < std::mem::size_of::<PmcEvent>() {
                            count += 1;
                            if count >= super::MAX_DRAIN_PER_TICK { break; }
                            continue;
                        }
                        let event = unsafe {
                            std::ptr::read_unaligned(data.as_ptr() as *const PmcEvent)
                        };
                        event_count += 1;
                        metrics.events_total.with_label_values(&["node_pmc"]).inc();
                        if let Some(anomaly) = classify(&event, &thresholds) {
                            let occ = build_occurrence(&event, &anomaly, boot_offset_ns);
                            anomaly_count += 1;
                            metrics.anomalies_total.with_label_values(&["node_pmc", anomaly.event_type]).inc();
                            if let Err(e) = sink.send(&occ) {
                                tracing::warn!(error = %e, "sink error");
                            }
                        }
                        count += 1;
                        if count >= super::MAX_DRAIN_PER_TICK { break; }
                    }
                    count
                });
                if drained >= super::MAX_DRAIN_PER_TICK {
                    metrics.drain_cap_total.with_label_values(&["node_pmc"]).inc();
                    tracing::warn!(observer = "pmc", drained, "ring buffer drain capped");
                }
            }
        }
    }

    if let Err(e) = sink.flush() {
        tracing::warn!(error = %e, "sink flush error on shutdown");
    }

    // Keep perf FDs alive until shutdown
    drop(perf_fds);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn default_thresholds() -> PmcThresholds {
        PmcThresholds {
            stall_pct_warning: 20.0,
            stall_pct_critical: 40.0,
            ipc_degradation: 1.0,
        }
    }

    fn make_pmc(cycles: u64, instructions: u64, stall_cycles: u64) -> PmcEvent {
        PmcEvent {
            cpu: 0,
            cycles,
            instructions,
            stall_cycles,
            timestamp: 1_000_000_000,
        }
    }

    #[test]
    fn classify_critical_stall() {
        let evt = make_pmc(1000, 400, 500); // 50% stalls
        let a = classify(&evt, &default_thresholds()).expect("should classify critical stall");
        assert_eq!(a.event_type, NODE_CPU_STALL);
        assert!(matches!(a.severity, Severity::Critical));
    }

    #[test]
    fn classify_memory_pressure() {
        let evt = make_pmc(1000, 600, 250); // 25% stalls
        let a = classify(&evt, &default_thresholds()).expect("should classify memory pressure");
        assert_eq!(a.event_type, NODE_MEMORY_PRESSURE);
        assert!(matches!(a.severity, Severity::Warning));
    }

    #[test]
    fn classify_ipc_degradation() {
        let evt = make_pmc(1000, 500, 100); // IPC=0.5, stalls=10%
        let a = classify(&evt, &default_thresholds()).expect("should classify IPC degradation");
        assert_eq!(a.event_type, NODE_IPC_DEGRADATION);
        assert!(a.error_message.contains("0.50"));
    }

    #[test]
    fn classify_normal_returns_none() {
        let evt = make_pmc(1000, 1500, 100); // IPC=1.5, stalls=10%
        assert!(classify(&evt, &default_thresholds()).is_none());
    }

    #[test]
    fn classify_zero_cycles_returns_none() {
        let evt = make_pmc(0, 0, 0);
        assert!(classify(&evt, &default_thresholds()).is_none());
    }

    #[test]
    fn build_occurrence_valid() {
        let evt = make_pmc(1000, 400, 500);
        let a = classify(&evt, &default_thresholds()).unwrap();
        let occ = build_occurrence(&evt, &a, 0);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, NODE_CPU_STALL);
        let data = occ.data.unwrap();
        assert_eq!(data["cpu"], 0);
    }
}
