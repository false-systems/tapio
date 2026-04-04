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
            error_message: format!(
                "OOM kill pid={} (usage={}MB, limit={}MB)",
                f.pid,
                f.memory_usage / 1024 / 1024,
                f.memory_limit / 1024 / 1024,
            ),
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
        // BPF tracepoint doesn't expose the cgroup memory limit.
        // Resolve via /proc/<pid>/cgroup + cgroupfs. Cached by cgroup_id.
        // Very fast OOM kills may still get 0 if the cgroup is deleted before we read.
        enrich_memory_limit(f.cgroup_id, f.pid).unwrap_or(0)
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
        "tid": f.tid,
        "exit_code": f.exit_code,
        "signal": f.signal,
        "memory_usage_bytes": f.memory_usage,
        "memory_limit_bytes": memory_limit,
        "cgroup_id": f.cgroup_id,
        "timestamp_ns": f.timestamp_ns,
    }))
}

use std::collections::HashMap;
use std::sync::Mutex;

static CGROUP_LIMIT_CACHE: std::sync::LazyLock<Mutex<HashMap<u64, u64>>> =
    std::sync::LazyLock::new(|| Mutex::new(HashMap::new()));

/// Resolve cgroup memory limit. Uses a cache keyed by cgroup_id to avoid
/// repeated filesystem walks on busy nodes.
fn enrich_memory_limit(cgroup_id: u64, pid: u32) -> Option<u64> {
    if cgroup_id == 0 {
        return None;
    }

    // Check cache first
    if let Ok(cache) = CGROUP_LIMIT_CACHE.lock()
        && let Some(&limit) = cache.get(&cgroup_id)
    {
        return Some(limit);
    }

    // Resolve cgroup path via /proc/<pid>/cgroup (fast, no tree walk)
    let limit = resolve_via_proc(pid)
        .or_else(|| resolve_via_proc(1)) // fallback: init's cgroup hierarchy
        .unwrap_or(0);

    // Cache the result (even 0, to avoid re-walking for deleted cgroups)
    if let Ok(mut cache) = CGROUP_LIMIT_CACHE.lock() {
        // Cap cache size to prevent unbounded growth
        if cache.len() > 10_000 {
            cache.clear();
        }
        cache.insert(cgroup_id, limit);
    }

    if limit > 0 { Some(limit) } else { None }
}

/// Read cgroup path from /proc/<pid>/cgroup and resolve memory limit from cgroupfs.
fn resolve_via_proc(pid: u32) -> Option<u64> {
    use std::fs;

    let cgroup_file = format!("/proc/{pid}/cgroup");
    let content = fs::read_to_string(&cgroup_file).ok()?;

    for line in content.lines() {
        // cgroups v2: "0::/kubepods/burstable/pod-xyz/container-abc"
        // cgroups v1: "6:memory:/kubepods/burstable/pod-xyz/container-abc"
        let parts: Vec<&str> = line.splitn(3, ':').collect();
        if parts.len() < 3 {
            continue;
        }

        let (hierarchy, controller, path) = (parts[0], parts[1], parts[2]);

        // cgroups v2 unified
        if hierarchy == "0" && controller.is_empty() && !path.is_empty() {
            let mem_max = format!("/sys/fs/cgroup{path}/memory.max");
            if let Some(limit) = read_limit_file(&mem_max) {
                return Some(limit);
            }
        }

        // cgroups v1 memory controller
        if controller.contains("memory") && !path.is_empty() {
            let limit_file = format!("/sys/fs/cgroup/memory{path}/memory.limit_in_bytes");
            if let Some(limit) = read_limit_file(&limit_file) {
                return Some(limit);
            }
        }
    }

    None
}

fn read_limit_file(path: &str) -> Option<u64> {
    use std::fs;

    let content = fs::read_to_string(path).ok()?;
    let trimmed = content.trim();

    // cgroups v2: "max" means no limit
    if trimmed == "max" {
        return Some(u64::MAX);
    }

    let val: u64 = trimmed.parse().ok()?;

    // cgroups v1: very large value means no limit
    if val >= (1_u64 << 62) {
        return Some(u64::MAX);
    }

    Some(val)
}

#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    enricher: Option<&crate::enricher::K8sEnricher>,
    boot_offset_ns: u64,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::{Ebpf, maps::RingBuf, programs::TracePoint};
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading container eBPF program");
    let mut ebpf = Ebpf::load_file(ebpf_path)?;

    for (name, category, tp) in [
        ("handle_oom", "oom", "mark_victim"),
        ("handle_exit", "sched", "sched_process_exit"),
    ] {
        let prog: &mut TracePoint = ebpf
            .program_mut(name)
            .ok_or_else(|| anyhow::anyhow!("program not found: {name}"))?
            .try_into()?;
        prog.load()?;
        prog.attach(category, tp)?;
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
    let mut enrich_total: u64 = 0;
    let mut enrich_miss: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "container observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                tick_count += 1;
                if tick_count % super::LOST_EVENTS_CHECK_INTERVAL == 0 {
                    if let Some(fd) = metrics_fd {
                        let lost = super::read_percpu_sum(fd, super::METRIC_LOST_EVENTS, nr_cpus);
                        if lost > prev_lost {
                            tracing::warn!(
                                observer = "container",
                                lost_total = lost,
                                lost_delta = lost - prev_lost,
                                "ring buffer events lost"
                            );
                            prev_lost = lost;
                        }
                    }
                    if enrich_total > 0 {
                        let miss_pct = (enrich_miss as f64 / enrich_total as f64) * 100.0;
                        if miss_pct > 10.0 {
                            tracing::warn!(
                                observer = "container",
                                miss_pct = format!("{miss_pct:.1}"),
                                enrich_miss,
                                enrich_total,
                                "enrichment miss rate exceeds 10%"
                            );
                        }
                    }
                }
                let drained = tokio::task::block_in_place(|| {
                    let mut count = 0usize;
                    while let Some(item) = ring_buf.next() {
                        let data = item.as_ref();
                        if data.len() < std::mem::size_of::<ContainerEvent>() {
                            count += 1;
                            if count >= super::MAX_DRAIN_PER_TICK { break; }
                            continue;
                        }
                        let event = unsafe {
                            std::ptr::read_unaligned(data.as_ptr() as *const ContainerEvent)
                        };
                        event_count += 1;
                        if let Some(anomaly) = classify(&event) {
                            let mut occ = build_occurrence(&event, &anomaly, boot_offset_ns);
                            if let Some(enricher) = enricher {
                                let cgroup_id = event.cgroup_id;
                                if cgroup_id != 0 {
                                    enrich_total += 1;
                                    if let Some(ctx) = enricher.enrich(cgroup_id) {
                                        occ.context = Some(ctx);
                                    } else {
                                        enrich_miss += 1;
                                    }
                                }
                            }
                            anomaly_count += 1;
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

    fn make_event(event_type: u32, exit_code: i32, signal: i32) -> ContainerEvent {
        let mut evt = unsafe { std::mem::zeroed::<ContainerEvent>() };
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
        assert!(a.error_message.contains("512MB"));
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
        assert_eq!(data["pid"], 1234);
        assert_eq!(data["memory_usage_bytes"], 536_870_912_u64);
    }
}
