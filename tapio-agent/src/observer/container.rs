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
    cgroup_path: String,
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
            cgroup_path: event.cgroup_path_str().to_string(),
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

pub fn build_occurrence(event: &ContainerEvent, anomaly: &ClassifiedAnomaly) -> Occurrence {
    let f = EventFields::from(event);
    Occurrence::new(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "pid": f.pid,
        "tid": f.tid,
        "exit_code": f.exit_code,
        "signal": f.signal,
        "memory_usage_bytes": f.memory_usage,
        "memory_limit_bytes": f.memory_limit,
        "cgroup_id": f.cgroup_id,
        "cgroup_path": f.cgroup_path,
        "timestamp_ns": f.timestamp_ns,
    }))
}

#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    enricher: Option<&crate::enricher::K8sEnricher>,
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

    let events_map = ebpf
        .map_mut("events")
        .ok_or_else(|| anyhow::anyhow!("map not found: events"))?;
    let mut ring_buf = RingBuf::try_from(events_map)?;

    tracing::info!("container observer running");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "container observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                while let Some(item) = ring_buf.next() {
                    let data = item.as_ref();
                    if data.len() < std::mem::size_of::<ContainerEvent>() {
                        continue;
                    }
                    let event = unsafe {
                        std::ptr::read_unaligned(data.as_ptr() as *const ContainerEvent)
                    };
                    event_count += 1;
                    if let Some(anomaly) = classify(&event) {
                        let mut occ = build_occurrence(&event, &anomaly);
                        if let Some(enricher) = enricher {
                            let cgroup_id = event.cgroup_id;
                            if let Some(ctx) = enricher.enrich(cgroup_id) {
                                occ.context = Some(ctx);
                            }
                        }
                        anomaly_count += 1;
                        if let Err(e) = sink.send(&occ) {
                            tracing::warn!(error = %e, "sink error");
                        }
                    }
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
        let occ = build_occurrence(&evt, &a);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, CONTAINER_OOM_KILL);
        let data = occ.data.unwrap();
        assert_eq!(data["pid"], 1234);
        assert_eq!(data["memory_usage_bytes"], 536_870_912_u64);
    }
}
