use std::fs;
use std::os::unix::fs::MetadataExt;
use std::path::Path;

use tapio_common::ebpf::*;
use tapio_common::events::*;
use tapio_common::occurrence::{Occurrence, Outcome, Severity};

pub struct ClassifiedAnomaly {
    pub event_type: &'static str,
    pub severity: Severity,
    pub outcome: Outcome,
    pub error_code: &'static str,
    pub error_message: String,
}

struct EventFields {
    config_generation: u32,
    event_type: u32,
    pid: u32,
    tid: u32,
    exit_code: i32,
    signal: i32,
    memory_usage: u64,
    memory_limit: u64,
    cgroup_id: u64,
    timestamp_ns: u64,
}

impl EventFields {
    fn from(event: &ContainerEvent) -> Self {
        Self {
            config_generation: event.config_generation,
            event_type: event.event_type,
            pid: event.pid,
            tid: event.tid,
            exit_code: event.exit_code,
            signal: event.signal,
            memory_usage: event.memory_usage,
            memory_limit: event.memory_limit,
            cgroup_id: event.cgroup_id,
            timestamp_ns: event.timestamp_ns,
        }
    }
}

/// OOM kills are always anomalies. Exits are anomalies only if non-zero code or signal.
pub fn classify(event: &ContainerEvent) -> Option<ClassifiedAnomaly> {
    let f = EventFields::from(event);

    match f.event_type {
        CONTAINER_EVENT_OOM => Some(ClassifiedAnomaly {
            event_type: CONTAINER_OOM_KILL,
            severity: Severity::Critical,
            outcome: Outcome::Failure,
            error_code: "OOM_KILL",
            error_message: if f.memory_usage == 0 {
                format!("OOM kill pid={}", f.pid)
            } else {
                format!(
                    "OOM kill pid={} (usage={}MB)",
                    f.pid,
                    f.memory_usage / 1024 / 1024
                )
            },
        }),
        CONTAINER_EVENT_EXIT => {
            if f.exit_code == 0 && f.signal == 0 {
                return None;
            }
            let severity = if f.signal != 0 {
                Severity::Error
            } else {
                Severity::Warning
            };
            Some(ClassifiedAnomaly {
                event_type: CONTAINER_ABNORMAL_EXIT,
                severity,
                outcome: Outcome::Failure,
                error_code: "ABNORMAL_EXIT",
                error_message: format!(
                    "Abnormal exit pid={} (code={}, signal={})",
                    f.pid, f.exit_code, f.signal,
                ),
            })
        }
        _ => None,
    }
}

pub fn build_occurrence(
    event: &ContainerEvent,
    anomaly: &ClassifiedAnomaly,
    boot_offset_ns: u64,
) -> Occurrence {
    let f = EventFields::from(event);
    let memory_limit = if f.memory_limit == 0 {
        // The kernel event carries no limit. 0 stays "unknown" unless cgroupfs
        // still holds the cgroup the kernel stamped on the event.
        cgroup_memory_limit(
            Path::new("/proc"),
            Path::new("/sys/fs/cgroup"),
            f.pid,
            f.cgroup_id,
        )
        .unwrap_or(0)
    } else {
        f.memory_limit
    };

    let wall_ns = boot_offset_ns.wrapping_add(f.timestamp_ns);
    Occurrence::new_at(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
        wall_ns,
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "pid": f.pid,
        "config_generation": f.config_generation,
        "tid": f.tid,
        "exit_code": f.exit_code,
        "signal": f.signal,
        "memory_usage_bytes": f.memory_usage,
        "memory_limit_bytes": memory_limit,
        "cgroup_id": f.cgroup_id,
        "timestamp_ns": f.timestamp_ns,
    }))
}

/// `memory.max` of the cgroup v2 that `pid` is in, but only when that cgroup is the
/// one the kernel stamped on the event: a cgroup directory's inode number is its id.
/// A reaped or reused pid, or a removed cgroup, gives `None`, never another cgroup's
/// limit. Not cached: an in-place resize changes `memory.max` under the same id.
fn cgroup_memory_limit(
    proc_root: &Path,
    cgroup_root: &Path,
    pid: u32,
    cgroup_id: u64,
) -> Option<u64> {
    if cgroup_id == 0 {
        return None;
    }
    let membership = fs::read_to_string(proc_root.join(pid.to_string()).join("cgroup")).ok()?;
    let path = membership
        .lines()
        .find_map(|line| line.strip_prefix("0::"))?;
    let dir = cgroup_root.join(path.trim_start_matches('/'));
    if fs::metadata(&dir).ok()?.ino() != cgroup_id {
        return None;
    }
    let memory_max = fs::read_to_string(dir.join("memory.max")).ok()?;
    match memory_max.trim() {
        "max" => Some(u64::MAX),
        bytes => bytes.parse().ok(),
    }
}

#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    boot_offset_ns: u64,
    tapio_config: tapio_common::ebpf::TapioConfig,
    carriers: &super::ConfigCarriers,
    metrics: &crate::metrics::TapioMetrics,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::{maps::RingBuf, programs::TracePoint};
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading container eBPF program");
    let mut ebpf = super::load_ebpf(ebpf_path, "container")?;
    if !super::init_and_register_tapio_config(&mut ebpf, "container", &tapio_config, carriers) {
        anyhow::bail!("container observer: failed to initialize tapio_config carrier");
    }

    for (name, category, tp) in [
        ("handle_oom", "oom", "mark_victim"),
        ("handle_exit", "sched", "sched_process_exit"),
    ] {
        let prog: &mut TracePoint = ebpf
            .program_mut(name)
            .ok_or_else(|| anyhow::anyhow!("program not found: {name}"))?
            .try_into()?;
        prog.load()
            .map_err(|e| anyhow::anyhow!("failed to load container program {name}: {e}"))?;
        prog.attach(category, tp).map_err(|e| {
            anyhow::anyhow!("failed to attach container program {name} to {category}/{tp}: {e}")
        })?;
        tracing::info!(tracepoint = %format!("{category}/{tp}"), "attached");
    }

    let nr_cpus = aya::util::nr_cpus().map_err(|(msg, e)| anyhow::anyhow!("{msg}: {e}"))?;
    let metrics_fd = super::metrics_map_fd(&ebpf);

    let events_map = ebpf
        .map_mut("events")
        .ok_or_else(|| anyhow::anyhow!("map not found: events"))?;
    let mut ring_buf = RingBuf::try_from(events_map)?;

    tracing::info!("container observer running");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;
    let mut tick_count: u64 = 0;
    let mut prev_lost: u64 = 0;
    let mut malformed_count: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "container observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                tick_count += 1;
                if tick_count.is_multiple_of(super::LOST_EVENTS_CHECK_INTERVAL)
                    && let Some(fd) = metrics_fd {
                        let lost = super::read_percpu_sum(fd, super::METRIC_LOST_EVENTS, nr_cpus);
                        if lost > prev_lost {
                            metrics.lost_events_total
                                .with_label_values(&["container"])
                                .inc_by(lost - prev_lost);
                            tracing::warn!(
                                observer = "container",
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
                        let expected_len = std::mem::size_of::<ContainerEvent>();
                        if data.len() < expected_len {
                            malformed_count += 1;
                            metrics.malformed_events_total.with_label_values(&["container"]).inc();
                            if super::should_warn_malformed_event(malformed_count) {
                                tracing::warn!(
                                    observer = "container",
                                    source = "ring_buffer",
                                    len = data.len(),
                                    expected_len,
                                    malformed_total = malformed_count,
                                    "dropped malformed eBPF record"
                                );
                            }
                            count += 1;
                            if count >= super::MAX_DRAIN_PER_TICK { break; }
                            continue;
                        }
                        let event = unsafe {
                            std::ptr::read_unaligned(data.as_ptr() as *const ContainerEvent)
                        };
                        event_count += 1;
                        metrics.events_total.with_label_values(&["container"]).inc();
                        if let Some(anomaly) = classify(&event) {
                            let occ = build_occurrence(&event, &anomaly, boot_offset_ns);
                            anomaly_count += 1;
                            metrics.anomalies_total.with_label_values(&["container", anomaly.event_type]).inc();
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
                    metrics.drain_cap_total.with_label_values(&["container"]).inc();
                    tracing::warn!(observer = "container", drained, "ring buffer drain capped");
                }
            }
        }
    }

    if let Err(e) = sink.flush() {
        tracing::warn!(error = %e, "sink flush error on shutdown");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn make_event(event_type: u32, exit_code: i32, signal: i32) -> ContainerEvent {
        let mut evt = unsafe { std::mem::zeroed::<ContainerEvent>() };
        evt.config_generation = 17;
        evt.event_type = event_type;
        evt.exit_code = exit_code;
        evt.signal = signal;
        evt.pid = 1234;
        evt.tid = 1234;
        evt.memory_usage = 536_870_912; // 512MB
        evt.memory_limit = 536_870_912;
        evt.cgroup_id = 42;
        evt
    }

    #[test]
    fn classify_oom_kill() {
        let evt = make_event(CONTAINER_EVENT_OOM, 137, 9);
        let a = classify(&evt).expect("should classify OOM");
        assert_eq!(a.event_type, CONTAINER_OOM_KILL);
        assert!(matches!(a.severity, Severity::Critical));
        assert_eq!(a.error_message, "OOM kill pid=1234 (usage=512MB)");
    }

    #[test]
    fn oom_message_omits_usage_the_kernel_did_not_report() {
        let mut evt = make_event(CONTAINER_EVENT_OOM, 137, 9);
        evt.memory_usage = 0;
        let a = classify(&evt).expect("should classify OOM");
        assert_eq!(a.error_message, "OOM kill pid=1234");
    }

    #[test]
    fn oom_occurrence_reports_unknown_limit_and_cgroup_as_zero() {
        let mut evt = make_event(CONTAINER_EVENT_OOM, 137, 9);
        evt.memory_limit = 0;
        evt.cgroup_id = 0;
        let a = classify(&evt).expect("should classify OOM");
        let data = build_occurrence(&evt, &a, 0).data.expect("occurrence data");
        assert_eq!(data["memory_limit_bytes"], 0);
        assert_eq!(data["cgroup_id"], 0);
    }

    struct FakeHost {
        root: PathBuf,
    }

    impl FakeHost {
        fn new(name: &str) -> Self {
            let root =
                std::env::temp_dir().join(format!("tapio-cgroup-{}-{name}", std::process::id()));
            let _ = fs::remove_dir_all(&root);
            fs::create_dir_all(root.join("proc")).expect("create proc root");
            fs::create_dir_all(root.join("cgroup")).expect("create cgroup root");
            Self { root }
        }

        fn proc_root(&self) -> PathBuf {
            self.root.join("proc")
        }

        fn cgroup_root(&self) -> PathBuf {
            self.root.join("cgroup")
        }

        fn cgroup(&self, path: &str, memory_max: &str) -> u64 {
            let dir = self.cgroup_root().join(path);
            fs::create_dir_all(&dir).expect("create cgroup dir");
            fs::write(dir.join("memory.max"), format!("{memory_max}\n")).expect("write memory.max");
            fs::metadata(&dir).expect("stat cgroup dir").ino()
        }

        fn process(&self, pid: u32, cgroup_path: &str) {
            let dir = self.proc_root().join(pid.to_string());
            fs::create_dir_all(&dir).expect("create proc pid dir");
            fs::write(dir.join("cgroup"), format!("0::{cgroup_path}\n")).expect("write cgroup");
        }

        fn limit(&self, pid: u32, cgroup_id: u64) -> Option<u64> {
            cgroup_memory_limit(&self.proc_root(), &self.cgroup_root(), pid, cgroup_id)
        }
    }

    impl Drop for FakeHost {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.root);
        }
    }

    #[test]
    fn memory_limit_comes_from_the_events_own_cgroup() {
        let host = FakeHost::new("own");
        let id = host.cgroup("kubepods/pod-a/ctr", "268435456");
        host.process(1234, "/kubepods/pod-a/ctr");
        assert_eq!(host.limit(1234, id), Some(268_435_456));
    }

    #[test]
    fn unlimited_cgroup_reports_max() {
        let host = FakeHost::new("unlimited");
        let id = host.cgroup("system.slice/a.service", "max");
        host.process(1234, "/system.slice/a.service");
        assert_eq!(host.limit(1234, id), Some(u64::MAX));
    }

    #[test]
    fn memory_limit_is_unknown_once_the_process_is_reaped() {
        let host = FakeHost::new("reaped");
        let id = host.cgroup("kubepods/pod-a/ctr", "268435456");
        host.cgroup("init.scope", "max");
        host.process(1, "/init.scope");
        assert_eq!(host.limit(1234, id), None);
    }

    #[test]
    fn memory_limit_is_unknown_when_the_pid_now_sits_in_another_cgroup() {
        let host = FakeHost::new("reused");
        let id = host.cgroup("kubepods/pod-a/ctr", "268435456");
        host.cgroup("kubepods/pod-b/ctr", "1073741824");
        host.process(1234, "/kubepods/pod-b/ctr");
        assert_eq!(host.limit(1234, id), None);
    }

    #[test]
    fn memory_limit_is_unknown_when_the_cgroup_was_removed() {
        let host = FakeHost::new("removed");
        let id = host.cgroup("kubepods/pod-a/ctr", "268435456");
        host.process(1234, "/kubepods/pod-a/ctr (deleted)");
        assert_eq!(host.limit(1234, id), None);
    }

    #[test]
    fn memory_limit_is_unknown_without_a_cgroup_id() {
        let host = FakeHost::new("no-id");
        host.cgroup("kubepods/pod-a/ctr", "268435456");
        host.process(1234, "/kubepods/pod-a/ctr");
        assert_eq!(host.limit(1234, 0), None);
    }

    #[test]
    fn classify_abnormal_exit_signal() {
        let evt = make_event(CONTAINER_EVENT_EXIT, 0, 9);
        let a = classify(&evt).expect("should classify signal kill");
        assert_eq!(a.event_type, CONTAINER_ABNORMAL_EXIT);
        assert!(matches!(a.severity, Severity::Error));
    }

    #[test]
    fn classify_abnormal_exit_code() {
        let evt = make_event(CONTAINER_EVENT_EXIT, 1, 0);
        let a = classify(&evt).expect("should classify non-zero exit");
        assert_eq!(a.event_type, CONTAINER_ABNORMAL_EXIT);
        assert!(matches!(a.severity, Severity::Warning));
    }

    #[test]
    fn classify_normal_exit_returns_none() {
        let evt = make_event(CONTAINER_EVENT_EXIT, 0, 0);
        assert!(classify(&evt).is_none());
    }

    #[test]
    fn build_occurrence_valid() {
        let evt = make_event(CONTAINER_EVENT_OOM, 137, 9);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a, 0);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, CONTAINER_OOM_KILL);
        let data = occ.data.unwrap();
        assert_eq!(data["config_generation"], 17);
        assert_eq!(data["pid"], 1234);
        assert_eq!(data["memory_usage_bytes"], 536_870_912_u64);
    }
}
