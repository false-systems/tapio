use tapio_common::ebpf::*;
use tapio_common::events::*;
use tapio_common::occurrence::{Occurrence, Outcome, Severity};

pub struct ClassifiedAnomaly {
    pub event_type: &'static str,
    pub severity: Severity,
    pub outcome: Outcome,
    pub error_code: &'static str,
    pub error_message: String,
    pub ebpf_event_type: u8,
}

/// Classify a raw eBPF NetworkEvent into an anomaly (or None for normal traffic).
/// Defers string allocation until we know this is an anomaly.
pub fn classify(event: &NetworkEvent) -> Option<ClassifiedAnomaly> {
    let et = event.event_type;
    let old = event.old_state;
    let new = event.new_state;

    match et {
        NET_EVENT_RST_RECEIVED => {
            let src_ip = event.src_ip_str();
            let src_port = event.src_port;
            Some(ClassifiedAnomaly {
                event_type: NETWORK_CONNECTION_REFUSED,
                severity: Severity::Warning,
                outcome: Outcome::Failure,
                error_code: "RST_RECEIVED",
                error_message: format!(
                    "TCP RST from {src_ip}:{src_port} (state={})",
                    tcp_state_name(old),
                ),
                ebpf_event_type: et,
            })
        }
        NET_EVENT_RETRANSMIT => {
            let src_ip = event.src_ip_str();
            let dst_ip = event.dst_ip_str();
            let sp = event.src_port;
            let dp = event.dst_port;
            Some(ClassifiedAnomaly {
                event_type: NETWORK_RETRANSMIT_SPIKE,
                severity: Severity::Warning,
                outcome: Outcome::Failure,
                error_code: "RETRANSMIT",
                error_message: format!(
                    "TCP retransmit {src_ip}:{sp} → {dst_ip}:{dp} (total={old}, cwnd={new})",
                ),
                ebpf_event_type: et,
            })
        }
        NET_EVENT_RTT_SPIKE => {
            let src_ip = event.src_ip_str();
            let dst_ip = event.dst_ip_str();
            let sp = event.src_port;
            let dp = event.dst_port;
            Some(ClassifiedAnomaly {
                event_type: NETWORK_RTT_DEGRADATION,
                severity: Severity::Warning,
                outcome: Outcome::InProgress,
                error_code: "RTT_SPIKE",
                error_message: format!(
                    "RTT spike {src_ip}:{sp} → {dst_ip}:{dp} (baseline={old}ms, current={new}ms)",
                ),
                ebpf_event_type: et,
            })
        }
        NET_EVENT_STATE_CHANGE => {
            if old == TCP_SYN_SENT && new == TCP_CLOSE {
                let dst_ip = event.dst_ip_str();
                let dp = event.dst_port;
                Some(ClassifiedAnomaly {
                    event_type: NETWORK_CONNECTION_TIMEOUT,
                    severity: Severity::Error,
                    outcome: Outcome::Failure,
                    error_code: "SYN_TIMEOUT",
                    error_message: format!("Connection timeout to {dst_ip}:{dp} (SYN_SENT→CLOSE)",),
                    ebpf_event_type: et,
                })
            } else {
                None
            }
        }
        _ => None,
    }
}

/// Build a FALSE Protocol Occurrence from a classified anomaly.
/// Encodes event-type-specific fields correctly (old_state/new_state semantics differ per event type).
pub fn build_occurrence(event: &NetworkEvent, anomaly: &ClassifiedAnomaly) -> Occurrence {
    let pid = event.pid;
    let comm = event.comm_str().to_string();
    let src_ip = event.src_ip_str();
    let dst_ip = event.dst_ip_str();
    let src_port = event.src_port;
    let dst_port = event.dst_port;
    let is_ipv6 = event.is_ipv6();
    let old = event.old_state;
    let new = event.new_state;

    let mut data = serde_json::json!({
        "pid": pid,
        "comm": comm,
        "src_ip": src_ip,
        "dst_ip": dst_ip,
        "src_port": src_port,
        "dst_port": dst_port,
        "family": if is_ipv6 { "ipv6" } else { "ipv4" },
        "protocol": "tcp",
    });

    // old_state/new_state semantics differ per event type
    if let serde_json::Value::Object(ref mut map) = data {
        match anomaly.ebpf_event_type {
            NET_EVENT_STATE_CHANGE | NET_EVENT_RST_RECEIVED => {
                map.insert("old_state".into(), serde_json::json!(tcp_state_name(old)));
                map.insert("new_state".into(), serde_json::json!(tcp_state_name(new)));
            }
            NET_EVENT_RETRANSMIT => {
                map.insert("total_retrans".into(), serde_json::json!(old));
                map.insert("snd_cwnd".into(), serde_json::json!(new));
            }
            NET_EVENT_RTT_SPIKE => {
                map.insert("baseline_ms".into(), serde_json::json!(old));
                map.insert("current_ms".into(), serde_json::json!(new));
            }
            _ => {}
        }
    }

    Occurrence::new(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(data)
}

/// Load eBPF program and start the observation loop.
#[cfg(target_os = "linux")]
pub async fn run(
    ebpf_path: &str,
    sink: &dyn tapio_common::sink::Sink,
    enricher: Option<&crate::enricher::K8sEnricher>,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use aya::{Ebpf, maps::RingBuf, programs::TracePoint};
    use std::time::Duration;

    tracing::info!(path = ebpf_path, "loading network eBPF program");
    let mut ebpf = Ebpf::load_file(ebpf_path)?;

    for (name, category, tp) in [
        ("trace_inet_sock_set_state", "sock", "inet_sock_set_state"),
        ("trace_tcp_receive_reset", "tcp", "tcp_receive_reset"),
        ("trace_tcp_retransmit_skb", "tcp", "tcp_retransmit_skb"),
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

    tracing::info!("network observer running (RTT baseline floor: 100ms)");
    let mut event_count: u64 = 0;
    let mut anomaly_count: u64 = 0;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!(events = event_count, anomalies = anomaly_count, "network observer shutting down");
                break;
            }
            _ = tokio::time::sleep(Duration::from_millis(10)) => {
                while let Some(item) = ring_buf.next() {
                    let data = item.as_ref();
                    if data.len() < std::mem::size_of::<NetworkEvent>() {
                        continue;
                    }
                    let event = unsafe {
                        std::ptr::read_unaligned(data.as_ptr() as *const NetworkEvent)
                    };
                    event_count += 1;
                    if let Some(anomaly) = classify(&event) {
                        let mut occ = build_occurrence(&event, &anomaly);
                        if let Some(enricher) = enricher {
                            let pid = event.pid;
                            if let Some(ctx) = enricher.enrich_by_pid(pid) {
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

fn tcp_state_name(state: u8) -> &'static str {
    match state {
        TCP_ESTABLISHED => "ESTABLISHED",
        TCP_SYN_SENT => "SYN_SENT",
        TCP_SYN_RECV => "SYN_RECV",
        TCP_FIN_WAIT1 => "FIN_WAIT1",
        TCP_FIN_WAIT2 => "FIN_WAIT2",
        TCP_TIME_WAIT => "TIME_WAIT",
        TCP_CLOSE => "CLOSE",
        TCP_CLOSE_WAIT => "CLOSE_WAIT",
        TCP_LAST_ACK => "LAST_ACK",
        TCP_LISTEN => "LISTEN",
        _ => "UNKNOWN",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_event(event_type: u8, old_state: u8, new_state: u8) -> NetworkEvent {
        let mut evt = unsafe { std::mem::zeroed::<NetworkEvent>() };
        evt.event_type = event_type;
        evt.old_state = old_state;
        evt.new_state = new_state;
        evt.family = AF_INET;
        evt.src_ip = 0x0100007f; // 127.0.0.1
        evt.dst_ip = 0x0200007f; // 127.0.0.2
        evt.src_port = 12345;
        evt.dst_port = 80;
        evt.pid = 42;
        evt.comm[..5].copy_from_slice(b"nginx");
        evt
    }

    #[test]
    fn classify_rst_received() {
        let evt = make_event(NET_EVENT_RST_RECEIVED, TCP_SYN_SENT, 0);
        let a = classify(&evt).expect("should classify RST");
        assert_eq!(a.event_type, NETWORK_CONNECTION_REFUSED);
        assert_eq!(a.error_code, "RST_RECEIVED");
        // RST "from" should be src (the remote sender)
        assert!(a.error_message.contains("127.0.0.1:12345"));
    }

    #[test]
    fn classify_retransmit() {
        let evt = make_event(NET_EVENT_RETRANSMIT, 5, 10);
        let a = classify(&evt).expect("should classify retransmit");
        assert_eq!(a.event_type, NETWORK_RETRANSMIT_SPIKE);
        assert!(a.error_message.contains("total=5"));
        assert!(a.error_message.contains("cwnd=10"));
    }

    #[test]
    fn classify_rtt_spike() {
        let evt = make_event(NET_EVENT_RTT_SPIKE, 2, 50);
        let a = classify(&evt).expect("should classify RTT spike");
        assert_eq!(a.event_type, NETWORK_RTT_DEGRADATION);
        assert!(a.error_message.contains("baseline=2ms"));
        assert!(a.error_message.contains("current=50ms"));
    }

    #[test]
    fn classify_syn_timeout() {
        let evt = make_event(NET_EVENT_STATE_CHANGE, TCP_SYN_SENT, TCP_CLOSE);
        let a = classify(&evt).expect("should classify SYN timeout");
        assert_eq!(a.event_type, NETWORK_CONNECTION_TIMEOUT);
    }

    #[test]
    fn classify_normal_state_change_returns_none() {
        let evt = make_event(NET_EVENT_STATE_CHANGE, TCP_ESTABLISHED, TCP_FIN_WAIT1);
        assert!(classify(&evt).is_none());
    }

    #[test]
    fn classify_unknown_event_type_returns_none() {
        let evt = make_event(99, 0, 0);
        assert!(classify(&evt).is_none());
    }

    #[test]
    fn build_occurrence_state_change_has_tcp_states() {
        let evt = make_event(NET_EVENT_RST_RECEIVED, TCP_SYN_SENT, 0);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        assert!(occ.validate().is_ok());
        let data = occ.data.unwrap();
        assert_eq!(data["old_state"], "SYN_SENT");
    }

    #[test]
    fn build_occurrence_retransmit_has_metrics() {
        let evt = make_event(NET_EVENT_RETRANSMIT, 5, 10);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        let data = occ.data.unwrap();
        assert_eq!(data["total_retrans"], 5);
        assert_eq!(data["snd_cwnd"], 10);
        assert!(data.get("old_state").is_none());
    }

    #[test]
    fn build_occurrence_rtt_spike_has_rtt_fields() {
        let evt = make_event(NET_EVENT_RTT_SPIKE, 2, 50);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        let data = occ.data.unwrap();
        assert_eq!(data["baseline_ms"], 2);
        assert_eq!(data["current_ms"], 50);
        assert!(data.get("old_state").is_none());
    }

    #[test]
    fn build_occurrence_no_reasoning() {
        let evt = make_event(NET_EVENT_RTT_SPIKE, 1, 100);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        assert!(occ.reasoning.is_none());
        assert!(occ.history.is_none());
    }

    #[test]
    fn tcp_state_names() {
        assert_eq!(tcp_state_name(TCP_ESTABLISHED), "ESTABLISHED");
        assert_eq!(tcp_state_name(TCP_CLOSE), "CLOSE");
        assert_eq!(tcp_state_name(255), "UNKNOWN");
    }
}
