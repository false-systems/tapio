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

pub fn build_occurrence(
    event: &StorageEvent,
    anomaly: &ClassifiedAnomaly,
    boot_offset_ns: u64,
) -> Occurrence {
    let wall_ns = boot_offset_ns.wrapping_add(event.timestamp_ns);
    Occurrence::new_at(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
        wall_ns,
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "config_generation": event.config_generation,
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
    boot_offset_ns: u64,
    tapio_config: tapio_common::ebpf::TapioConfig,
    metrics: &crate::metrics::TapioMetrics,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::{maps::RingBuf, programs::TracePoint};
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading storage eBPF program");
    let mut ebpf = super::load_ebpf(ebpf_path, "storage")?;
    if !super::write_tapio_config(&mut ebpf, "storage", &tapio_config) {
        anyhow::bail!("storage observer: failed to initialize tapio_config carrier");
    }

    for (name, category, tp) in [
        ("trace_block_rq_issue", "block", "block_rq_issue"),
        ("trace_block_rq_complete", "block", "block_rq_complete"),
    ] {
        let prog: &mut TracePoint = ebpf
            .program_mut(name)
            .ok_or_else(|| anyhow::anyhow!("program not found: {name}"))?
            .try_into()?;
        prog.load()
            .map_err(|e| anyhow::anyhow!("failed to load storage program {name}: {e}"))?;
        prog.attach(category, tp).map_err(|e| {
            anyhow::anyhow!("failed to attach storage program {name} to {category}/{tp}: {e}")
        })?;
        tracing::info!(tracepoint = %format!("{category}/{tp}"), "attached");
    }

    let nr_cpus = aya::util::nr_cpus().map_err(|(msg, e)| anyhow::anyhow!("{msg}: {e}"))?;
    let metrics_fd = super::metrics_map_fd(&ebpf);

    let events_map = ebpf
        .map_mut("events")
        .ok_or_else(|| anyhow::anyhow!("map not found: events"))?;
    let mut ring_buf = RingBuf::try_from(events_map)?;

    tracing::info!("storage observer running");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;
    let mut tick_count: u64 = 0;
    let mut prev_lost: u64 = 0;
    let mut prev_ambiguous: u64 = 0;
    let mut malformed_count: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "storage observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                tick_count += 1;
                if tick_count.is_multiple_of(super::LOST_EVENTS_CHECK_INTERVAL)
                    && let Some(fd) = metrics_fd {
                        let lost = super::read_percpu_sum(fd, super::METRIC_LOST_EVENTS, nr_cpus);
                        if lost > prev_lost {
                            metrics.lost_events_total
                                .with_label_values(&["storage"])
                                .inc_by(lost - prev_lost);
                            tracing::warn!(
                                observer = "storage",
                                lost_total = lost,
                                lost_delta = lost - prev_lost,
                                "ring buffer events lost"
                            );
                            prev_lost = lost;
                        }
                        let ambiguous = super::read_percpu_sum(fd, super::METRIC_STORAGE_AMBIGUOUS_IO, nr_cpus);
                        if ambiguous > prev_ambiguous {
                            let delta = ambiguous - prev_ambiguous;
                            metrics.correlation_drops_total
                                .with_label_values(&["storage", "ambiguous_inflight_io"])
                                .inc_by(delta);
                            tracing::warn!(
                                observer = "storage",
                                ambiguous_total = ambiguous,
                                ambiguous_delta = delta,
                                "dropped ambiguous storage I/O correlation"
                            );
                            prev_ambiguous = ambiguous;
                        }
                }
                let drained = tokio::task::block_in_place(|| {
                    let mut count = 0usize;
                    while let Some(item) = ring_buf.next() {
                        let data: &[u8] = item.as_ref();
                        let expected_len = std::mem::size_of::<StorageEvent>();
                        if data.len() < expected_len {
                            malformed_count += 1;
                            metrics.malformed_events_total.with_label_values(&["storage"]).inc();
                            if super::should_warn_malformed_event(malformed_count) {
                                tracing::warn!(
                                    observer = "storage",
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
                            std::ptr::read_unaligned(data.as_ptr() as *const StorageEvent)
                        };
                        event_count += 1;
                        metrics.events_total.with_label_values(&["storage"]).inc();
                        if let Some(anomaly) = classify(&event) {
                            let occ = build_occurrence(&event, &anomaly, boot_offset_ns);
                            anomaly_count += 1;
                            metrics.anomalies_total.with_label_values(&["storage", anomaly.event_type]).inc();
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
                    metrics.drain_cap_total.with_label_values(&["storage"]).inc();
                    tracing::warn!(observer = "storage", drained, "ring buffer drain capped");
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

    fn make_event(error_code: i32, severity: u8, latency_ns: u64) -> StorageEvent {
        let mut evt = unsafe { std::mem::zeroed::<StorageEvent>() };
        evt.error_code = error_code;
        evt.config_generation = 17;
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
        let evt = make_event(-5, STORAGE_SEVERITY_CRITICAL, 0);
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
        let evt = make_event(-5, STORAGE_SEVERITY_CRITICAL, 0);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a, 0);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, STORAGE_IO_ERROR);
        let data = occ.data.unwrap();
        assert_eq!(data["config_generation"], 17);
        assert_eq!(data["comm"], "psql");
        assert_eq!(data["opcode"], "write");
        assert_eq!(data["error_code"], -5);
    }
}
