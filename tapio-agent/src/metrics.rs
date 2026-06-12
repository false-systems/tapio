use std::collections::BTreeMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

#[derive(Clone)]
#[allow(dead_code)]
pub struct TapioMetrics {
    pub events_total: CounterVec,
    pub anomalies_total: CounterVec,
    pub lost_events_total: CounterVec,
    pub malformed_events_total: CounterVec,
    pub correlation_drops_total: CounterVec,
    pub drain_cap_total: CounterVec,
    pub sink_writes_total: CounterVec,
    pub sink_drops_total: CounterVec,
    pub controller_send_failures_total: CounterVec,
    pub config_fetch_total: CounterVec,
}

#[derive(Clone)]
pub struct CounterVec {
    name: &'static str,
    help: &'static str,
    labels: &'static [&'static str],
    values: Arc<Mutex<BTreeMap<Vec<String>, u64>>>,
}

pub struct Counter {
    values: Arc<Mutex<BTreeMap<Vec<String>, u64>>>,
    label_values: Vec<String>,
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
impl TapioMetrics {
    pub fn new() -> anyhow::Result<Self> {
        Ok(Self {
            events_total: CounterVec::new(
                "tapio_events_total",
                "Total events drained from ring buffer",
                &["observer"],
            ),
            anomalies_total: CounterVec::new(
                "tapio_anomalies_total",
                "Total anomalies detected and emitted",
                &["observer", "anomaly_type"],
            ),
            lost_events_total: CounterVec::new(
                "tapio_lost_events_total",
                "Ring buffer reserve failures in eBPF (events dropped)",
                &["observer"],
            ),
            malformed_events_total: CounterVec::new(
                "tapio_malformed_events_total",
                "Malformed or truncated eBPF records dropped by userspace",
                &["observer"],
            ),
            correlation_drops_total: CounterVec::new(
                "tapio_correlation_drops_total",
                "Events intentionally dropped because kernel correlation was ambiguous",
                &["observer", "reason"],
            ),
            drain_cap_total: CounterVec::new(
                "tapio_drain_cap_total",
                "Times ring buffer drain hit the per-tick cap",
                &["observer"],
            ),
            sink_writes_total: CounterVec::new(
                "tapio_sink_writes_total",
                "Total sink write attempts by result",
                &["sink", "result"],
            ),
            sink_drops_total: CounterVec::new(
                "tapio_sink_drops_total",
                "Total sink events dropped by sink and reason",
                &["sink", "reason"],
            ),
            controller_send_failures_total: CounterVec::new(
                "tapio_controller_send_failures_total",
                "Controller send failures by request kind",
                &["kind"],
            ),
            config_fetch_total: CounterVec::new(
                "tapio_config_fetch_total",
                "Controller config poll attempts by result",
                &["result"],
            ),
        })
    }

    pub fn encode(&self) -> String {
        let mut out = String::new();
        for counter in [
            &self.events_total,
            &self.anomalies_total,
            &self.lost_events_total,
            &self.malformed_events_total,
            &self.correlation_drops_total,
            &self.drain_cap_total,
            &self.sink_writes_total,
            &self.sink_drops_total,
            &self.controller_send_failures_total,
            &self.config_fetch_total,
        ] {
            counter.encode(&mut out);
        }
        out
    }
}

impl CounterVec {
    fn new(name: &'static str, help: &'static str, labels: &'static [&'static str]) -> Self {
        Self {
            name,
            help,
            labels,
            values: Arc::new(Mutex::new(BTreeMap::new())),
        }
    }

    pub fn with_label_values(&self, label_values: &[&str]) -> Counter {
        debug_assert_eq!(label_values.len(), self.labels.len());
        Counter {
            values: self.values.clone(),
            label_values: label_values
                .iter()
                .map(|value| (*value).to_string())
                .collect(),
        }
    }

    pub fn sum(&self) -> u64 {
        let values = self.values.lock().expect("metrics lock poisoned");
        values.values().copied().sum()
    }

    fn encode(&self, out: &mut String) {
        out.push_str("# HELP ");
        out.push_str(self.name);
        out.push(' ');
        out.push_str(self.help);
        out.push('\n');
        out.push_str("# TYPE ");
        out.push_str(self.name);
        out.push_str(" counter\n");

        let values = self.values.lock().expect("metrics lock poisoned");
        for (label_values, value) in values.iter() {
            out.push_str(self.name);
            if !self.labels.is_empty() {
                out.push('{');
                for (idx, (label, label_value)) in
                    self.labels.iter().zip(label_values.iter()).enumerate()
                {
                    if idx > 0 {
                        out.push(',');
                    }
                    out.push_str(label);
                    out.push_str("=\"");
                    push_escaped_label(out, label_value);
                    out.push('"');
                }
                out.push('}');
            }
            out.push(' ');
            out.push_str(&value.to_string());
            out.push('\n');
        }
    }
}

impl Counter {
    pub fn inc(&self) {
        self.inc_by(1);
    }

    pub fn inc_by(&self, value: u64) {
        let mut values = self.values.lock().expect("metrics lock poisoned");
        *values.entry(self.label_values.clone()).or_insert(0) += value;
    }

    #[cfg(test)]
    pub fn value(&self) -> u64 {
        let values = self.values.lock().expect("metrics lock poisoned");
        values.get(&self.label_values).copied().unwrap_or(0)
    }
}

fn push_escaped_label(out: &mut String, value: &str) {
    for ch in value.chars() {
        match ch {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            other => out.push(other),
        }
    }
}

/// Serve /metrics on the given address. Runs until the shutdown signal is received.
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub async fn serve(
    metrics: TapioMetrics,
    addr: SocketAddr,
    shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    tokio::task::spawn_blocking(move || serve_blocking(metrics, addr, shutdown)).await?
}

fn serve_blocking(
    metrics: TapioMetrics,
    addr: SocketAddr,
    shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::time::Duration;

    tracing::info!(%addr, "metrics endpoint starting");

    let listener = TcpListener::bind(addr)?;
    listener.set_nonblocking(true)?;

    while !*shutdown.borrow() {
        match listener.accept() {
            Ok((mut stream, _)) => {
                let mut request = [0u8; 1024];
                stream.set_read_timeout(Some(Duration::from_secs(2))).ok();
                let n = stream.read(&mut request)?;
                let first_line = std::str::from_utf8(&request[..n])
                    .unwrap_or_default()
                    .lines()
                    .next()
                    .unwrap_or_default();

                if !first_line.starts_with("GET /metrics ") {
                    stream.write_all(
                        b"HTTP/1.1 404 Not Found\r\nContent-Length: 9\r\nConnection: close\r\n\r\nnot found",
                    )?;
                    continue;
                }

                let body = metrics.encode();
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: text/plain; version=0.0.4\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
                stream.write_all(response.as_bytes())?;
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                std::thread::sleep(Duration::from_millis(100));
            }
            Err(e) => return Err(e.into()),
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metrics_register_without_panic() {
        let m = TapioMetrics::new().expect("metrics registration");
        m.events_total.with_label_values(&["network"]).inc();
        m.anomalies_total
            .with_label_values(&["network", "kernel.network.connection_refused"])
            .inc();
        m.sink_writes_total
            .with_label_values(&["stdout", "ok"])
            .inc();
        m.malformed_events_total
            .with_label_values(&["network"])
            .inc();
        m.correlation_drops_total
            .with_label_values(&["storage", "ambiguous_inflight_io"])
            .inc();

        let buf = m.encode();
        assert!(buf.contains("tapio_events_total"));
        assert!(buf.contains("tapio_anomalies_total"));
        assert!(buf.contains("tapio_malformed_events_total"));
        assert!(buf.contains("tapio_correlation_drops_total"));
        assert!(buf.contains("tapio_sink_drops_total"));
        assert!(buf.contains("tapio_controller_send_failures_total"));
    }

    #[test]
    fn loss_and_malformed_counters_increment() {
        let m = TapioMetrics::new().expect("metrics registration");
        m.lost_events_total
            .with_label_values(&["network"])
            .inc_by(3);
        m.malformed_events_total
            .with_label_values(&["network"])
            .inc_by(2);
        m.correlation_drops_total
            .with_label_values(&["storage", "ambiguous_inflight_io"])
            .inc_by(1);

        let buf = m.encode();
        assert!(buf.contains("tapio_lost_events_total{observer=\"network\"} 3"));
        assert!(buf.contains("tapio_malformed_events_total{observer=\"network\"} 2"));
        assert!(buf.contains(
            "tapio_correlation_drops_total{observer=\"storage\",reason=\"ambiguous_inflight_io\"} 1"
        ));
    }

    #[test]
    fn label_values_are_escaped() {
        let m = TapioMetrics::new().expect("metrics registration");
        m.sink_writes_total
            .with_label_values(&["quote\"slash\\newline\n", "ok"])
            .inc();

        let buf = m.encode();
        assert!(buf.contains("quote\\\"slash\\\\newline\\n"));
    }

    #[test]
    fn counters_can_be_read() {
        let m = TapioMetrics::new().expect("metrics registration");
        let network = m.events_total.with_label_values(&["network"]);
        let storage = m.events_total.with_label_values(&["storage"]);
        network.inc_by(2);
        storage.inc_by(3);

        assert_eq!(network.value(), 2);
        assert_eq!(m.events_total.sum(), 5);
    }
}
