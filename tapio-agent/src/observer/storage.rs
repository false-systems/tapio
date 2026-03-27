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

/// eBPF pre-filters to anomalies only — all events here are significant.
pub fn classify(event: &StorageEvent) -> Option<ClassifiedAnomaly> {
    if event.has_error() {
        Some(ClassifiedAnomaly {
            event_type: STORAGE_IO_ERROR,
            severity: Severity::Critical,
            outcome: Outcome::Failure,
            error_code: "IO_ERROR",
            error_message: format!(
                "I/O error on {}:{} (error={}, {})",
                event.dev_major,
                event.dev_minor,
                event.error_code,
                op_name(event.opcode),
            ),
        })
    } else {
        let severity = match event.severity {
            STORAGE_SEVERITY_CRITICAL => Severity::Critical,
            _ => Severity::Warning,
        };
        Some(ClassifiedAnomaly {
            event_type: STORAGE_LATENCY_SPIKE,
            severity,
            outcome: Outcome::InProgress,
            error_code: "LATENCY_SPIKE",
            error_message: format!(
                "I/O latency {:.1}ms on {}:{} ({})",
                event.latency_ms(),
                event.dev_major,
                event.dev_minor,
                op_name(event.opcode),
            ),
        })
    }
}

pub fn build_occurrence(event: &StorageEvent, anomaly: &ClassifiedAnomaly) -> Occurrence {
    Occurrence::new(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "pid": event.pid,
        "comm": event.comm_str(),
        "dev_major": event.dev_major,
        "dev_minor": event.dev_minor,
        "sector": event.sector,
        "bytes": event.bytes,
        "latency_ms": event.latency_ms(),
        "opcode": op_name(event.opcode),
        "error_code": event.error_code,
        "cgroup_id": event.cgroup_id,
        "timestamp_ns": event.timestamp_ns,
    }))
}

#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    cache: &crate::enrichment::PodCache,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::{Ebpf, maps::RingBuf, programs::TracePoint};
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading storage eBPF program");
    let mut ebpf = Ebpf::load_file(ebpf_path)?;

    for (name, category, tp) in [
        ("trace_block_rq_issue", "block", "block_rq_issue"),
        ("trace_block_rq_complete", "block", "block_rq_complete"),
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

    tracing::info!("storage observer running");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "storage observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                while let Some(item) = ring_buf.next() {
                    let data = item.as_ref();
                    if data.len() < std::mem::size_of::<StorageEvent>() {
                        continue;
                    }
                    let event = unsafe {
                        std::ptr::read_unaligned(data.as_ptr() as *const StorageEvent)
                    };
                    event_count += 1;
                    if let Some(anomaly) = classify(&event) {
                        let mut occ = build_occurrence(&event, &anomaly);
                        crate::enrichment::enrich(&mut occ, cache, &crate::enrichment::EnrichHints {
                            src_ip: None,
                            cgroup_path: None, // storage events use cgroup_id, not path — future: resolve via /proc
                        });
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

fn op_name(opcode: u8) -> &'static str {
    match opcode {
        OP_READ => "read",
        OP_WRITE => "write",
        _ => "unknown",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_event(error_code: u16, severity: u8, latency_ns: u64) -> StorageEvent {
        let mut evt = unsafe { std::mem::zeroed::<StorageEvent>() };
        evt.error_code = error_code;
        evt.severity = severity;
        evt.latency_ns = latency_ns;
        evt.dev_major = 8;
        evt.dev_minor = 0;
        evt.pid = 100;
        evt.opcode = OP_WRITE;
        evt.bytes = 4096;
        evt.comm[..4].copy_from_slice(b"psql");
        evt
    }

    #[test]
    fn classify_io_error() {
        let evt = make_event(5, STORAGE_SEVERITY_CRITICAL, 0);
        let a = classify(&evt).expect("should classify error");
        assert_eq!(a.event_type, STORAGE_IO_ERROR);
        assert!(matches!(a.severity, Severity::Critical));
    }

    #[test]
    fn classify_latency_warning() {
        let evt = make_event(0, STORAGE_SEVERITY_WARNING, 75_000_000); // 75ms
        let a = classify(&evt).expect("should classify latency");
        assert_eq!(a.event_type, STORAGE_LATENCY_SPIKE);
        assert!(matches!(a.severity, Severity::Warning));
    }

    #[test]
    fn classify_latency_critical() {
        let evt = make_event(0, STORAGE_SEVERITY_CRITICAL, 250_000_000); // 250ms
        let a = classify(&evt).expect("should classify critical latency");
        assert_eq!(a.event_type, STORAGE_LATENCY_SPIKE);
        assert!(matches!(a.severity, Severity::Critical));
    }

    #[test]
    fn build_occurrence_valid() {
        let evt = make_event(5, STORAGE_SEVERITY_CRITICAL, 0);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, STORAGE_IO_ERROR);
        let data = occ.data.unwrap();
        assert_eq!(data["comm"], "psql");
        assert_eq!(data["opcode"], "write");
    }
}
