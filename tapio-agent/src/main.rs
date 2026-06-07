mod config;
#[cfg(target_os = "linux")]
mod enricher;
mod metrics;
mod observer;
mod sink;

use clap::Parser;
use std::io::{self, Write};
use tracing::info;

/// Stderr writer that silently swallows broken pipe errors.
/// Prevents panic=abort from killing the agent when piped to `head`, `grep`, etc.
struct BrokenPipeGuard;

impl Write for BrokenPipeGuard {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        match io::stderr().write(buf) {
            Err(e) if e.kind() == io::ErrorKind::BrokenPipe => Ok(buf.len()),
            other => other,
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        match io::stderr().flush() {
            Err(e) if e.kind() == io::ErrorKind::BrokenPipe => Ok(()),
            other => other,
        }
    }
}

#[derive(Parser)]
#[command(
    name = "tapio-agent",
    version,
    about = "Opinionated eBPF observer for Linux and Kubernetes — emits structured kernel anomaly events"
)]
struct Args {
    /// Path to TOML config file
    #[arg(long, default_value = "/etc/tapio/tapio.toml")]
    config: String,

    /// Output sinks (stdout, file, http, otlp). Can be specified multiple times.
    #[arg(long = "sink", default_values_t = vec!["stdout".to_string()])]
    sinks: Vec<String>,

    /// Directory containing compiled eBPF .o files
    #[arg(long, default_value = "/opt/tapio/ebpf")]
    ebpf_dir: String,

    /// Directory for file sink output
    #[arg(long, default_value = ".tapio/occurrences")]
    data_dir: String,

    /// Endpoint for the http sink — JSON POST ingest (e.g. http://localhost:8765)
    #[arg(long, default_value = "http://localhost:8765")]
    http_endpoint: String,
}

fn create_sinks(
    args: &Args,
    cfg: &config::Config,
) -> anyhow::Result<Vec<Box<dyn tapio_common::sink::Sink>>> {
    let mut sinks: Vec<Box<dyn tapio_common::sink::Sink>> = Vec::new();
    for name in &args.sinks {
        match name.as_str() {
            "stdout" => sinks.push(Box::new(sink::stdout::StdoutSink)),
            "file" => sinks.push(Box::new(sink::file::FileSink::new(&args.data_dir))),
            "http" => sinks.push(Box::new(sink::http::HttpSink::new(
                &args.http_endpoint,
                100,
                std::time::Duration::from_secs(1),
            ))),
            "otlp" => sinks.push(Box::new(sink::otlp::OtlpSink::new(
                &cfg.otlp.endpoint,
                cfg.otlp.auth_header.clone(),
                cfg.otlp.batch_size,
                std::time::Duration::from_secs(cfg.otlp.flush_interval_secs),
            )?)),
            other => anyhow::bail!("unknown sink: {other}"),
        }
    }
    if sinks.is_empty() {
        anyhow::bail!("at least one sink is required");
    }
    Ok(sinks)
}

/// Fan-out sink that sends to all inner sinks. Failure in one doesn't block others.
struct MultiSink {
    sinks: Vec<Box<dyn tapio_common::sink::Sink>>,
    metrics: Option<metrics::TapioMetrics>,
}

impl tapio_common::sink::Sink for MultiSink {
    fn send(
        &self,
        occurrence: &tapio_common::occurrence::Occurrence,
    ) -> Result<(), tapio_common::sink::SinkError> {
        let mut last_err = None;
        for s in &self.sinks {
            match s.send(occurrence) {
                Ok(()) => {
                    if let Some(metrics) = &self.metrics {
                        metrics
                            .sink_writes_total
                            .with_label_values(&[s.name(), "ok"])
                            .inc();
                    }
                }
                Err(e) => {
                    if let Some(metrics) = &self.metrics {
                        metrics
                            .sink_writes_total
                            .with_label_values(&[s.name(), "err"])
                            .inc();
                    }
                    tracing::warn!(sink = s.name(), error = %e, "sink error");
                    last_err = Some(e);
                }
            }
        }
        match last_err {
            Some(e) => Err(e),
            None => Ok(()),
        }
    }

    fn flush(&self) -> Result<(), tapio_common::sink::SinkError> {
        let mut last_err = None;
        for s in &self.sinks {
            if let Err(e) = s.flush() {
                tracing::warn!(sink = s.name(), error = %e, "flush error");
                last_err = Some(e);
            }
        }
        match last_err {
            Some(e) => Err(e),
            None => Ok(()),
        }
    }

    fn name(&self) -> &str {
        "multi"
    }
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Ignore SIGPIPE so broken pipes (e.g. piping to `head`) return EPIPE
    // instead of aborting the process (panic=abort in release profile).
    #[cfg(target_os = "linux")]
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_IGN);
        // Prevent the process from gaining new privileges (e.g., via execve of
        // setuid binaries). Reduces blast radius if the agent is compromised.
        libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0);
    }

    // Use a non-panicking writer for tracing — default stderr writer panics
    // on broken pipe, which with panic=abort kills the process.
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_writer(|| BrokenPipeGuard)
        .init();

    let args = Args::parse();
    #[allow(unused_variables)]
    let cfg = config::load(std::path::Path::new(&args.config))?;
    info!("tapio v4 — kernel eyes");

    let tapio_metrics = metrics::TapioMetrics::new()?;
    let sinks = create_sinks(&args, &cfg)?;
    let sink_names: Vec<&str> = sinks.iter().map(|s| s.name()).collect();
    info!(sinks = ?sink_names, "sinks configured");

    let multi_sink: std::sync::Arc<MultiSink> = std::sync::Arc::new(MultiSink {
        sinks,
        metrics: Some(tapio_metrics.clone()),
    });

    #[cfg(target_os = "linux")]
    {
        use std::sync::Arc;

        // Calculate boot-time offset: wall_clock_ns - monotonic_ns.
        // bpf_ktime_get_ns() returns CLOCK_MONOTONIC nanoseconds.
        // Adding this offset converts event timestamps to wall clock.
        let boot_offset_ns = {
            let wall_ns = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .expect("system clock before UNIX epoch")
                .as_nanos() as u64;
            let mut ts = libc::timespec {
                tv_sec: 0,
                tv_nsec: 0,
            };
            unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
            let mono_ns = ts.tv_sec as u64 * 1_000_000_000 + ts.tv_nsec as u64;
            wall_ns.wrapping_sub(mono_ns)
        };
        info!(boot_offset_ns, "clock offset calculated");

        // Try to set up K8s enrichment (optional — agent runs without it)
        let enricher = match enricher::K8sEnricher::new().await {
            Ok(e) => {
                info!("K8s enrichment enabled");
                Some(Arc::new(e))
            }
            Err(e) => {
                tracing::warn!(error = %e, "K8s enrichment disabled");
                None
            }
        };

        if enricher.is_some() {
            tapio_metrics.k8s_reflector_up.set(1);
        }

        let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

        // Start Prometheus metrics server if enabled
        if cfg.metrics.enabled {
            let registry = tapio_metrics.registry.clone();
            let metrics_ip: std::net::IpAddr = cfg
                .metrics
                .bind_address
                .parse()
                .map_err(|e| anyhow::anyhow!("invalid metrics bind_address: {e}"))?;
            let metrics_addr = std::net::SocketAddr::new(metrics_ip, cfg.metrics.port);
            let metrics_shutdown_rx = shutdown_rx.clone();
            tokio::spawn(async move {
                if let Err(e) = metrics::serve(registry, metrics_addr, metrics_shutdown_rx).await {
                    tracing::error!(error = %e, "metrics server failed");
                }
            });
        }

        tokio::spawn(async move {
            tokio::signal::ctrl_c().await.ok();
            info!("received SIGINT, shutting down");
            let _ = shutdown_tx.send(true);
        });

        let ebpf_dir = args.ebpf_dir.clone();

        let net_thresholds = observer::network::NetworkThresholds {
            rtt_spike_ratio: cfg.thresholds.rtt_spike_ratio,
            rtt_spike_abs_us: cfg.thresholds.rtt_spike_abs_us,
        };
        let stg_thresholds = observer::storage::StorageThresholds {
            io_latency_warning_ns: cfg.thresholds.io_latency_warning_ns,
            io_latency_critical_ns: cfg.thresholds.io_latency_critical_ns,
        };

        let sink1: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let enricher1 = enricher.clone();
        let metrics1 = tapio_metrics.clone();
        let rx1 = shutdown_rx.clone();
        let dir1 = ebpf_dir.clone();
        let net = tokio::spawn(async move {
            let path = format!("{dir1}/network_monitor.o");
            if let Err(e) = observer::network::run(
                &path,
                sink1.as_ref(),
                enricher1.as_deref(),
                boot_offset_ns,
                net_thresholds,
                &metrics1,
                rx1,
            )
            .await
            {
                tracing::error!(error = %e, "network observer failed");
            }
        });

        let sink2: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let enricher2 = enricher.clone();
        let metrics2 = tapio_metrics.clone();
        let rx2 = shutdown_rx.clone();
        let dir2 = ebpf_dir.clone();
        let ctr = tokio::spawn(async move {
            let path = format!("{dir2}/container_monitor.o");
            if let Err(e) = observer::container::run(
                &path,
                sink2.as_ref(),
                enricher2.as_deref(),
                boot_offset_ns,
                &metrics2,
                rx2,
            )
            .await
            {
                tracing::error!(error = %e, "container observer failed");
            }
        });

        let sink3: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let enricher3 = enricher.clone();
        let metrics3 = tapio_metrics.clone();
        let rx3 = shutdown_rx.clone();
        let dir3 = ebpf_dir.clone();
        let stg = tokio::spawn(async move {
            let path = format!("{dir3}/storage_monitor.o");
            if let Err(e) = observer::storage::run(
                &path,
                sink3.as_ref(),
                enricher3.as_deref(),
                boot_offset_ns,
                stg_thresholds,
                &metrics3,
                rx3,
            )
            .await
            {
                tracing::error!(error = %e, "storage observer failed");
            }
        });

        let pmc_thresholds = observer::node_pmc::PmcThresholds {
            stall_pct_warning: cfg.thresholds.stall_pct_warning,
            stall_pct_critical: cfg.thresholds.stall_pct_critical,
            ipc_degradation: cfg.thresholds.ipc_degradation,
        };

        let sink4: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let metrics4 = tapio_metrics.clone();
        let rx4 = shutdown_rx.clone();
        let dir4 = ebpf_dir.clone();
        let pmc = tokio::spawn(async move {
            let path = format!("{dir4}/node_pmc_monitor.o");
            if let Err(e) = observer::node_pmc::run(
                &path,
                sink4.as_ref(),
                boot_offset_ns,
                pmc_thresholds,
                &metrics4,
                rx4,
            )
            .await
            {
                tracing::error!(error = %e, "PMC observer failed");
            }
        });

        let _ = tokio::join!(net, ctr, stg, pmc);
        info!("all observers stopped");
        Ok(())
    }

    #[cfg(not(target_os = "linux"))]
    {
        let _ = (args, multi_sink, tapio_metrics);
        anyhow::bail!("eBPF requires Linux — tapio-agent cannot run on this platform");
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use tapio_common::occurrence::{Occurrence, Outcome, Severity};
    use tapio_common::sink::{Sink, SinkError};

    struct CountingSink {
        name: &'static str,
        sends: Arc<AtomicUsize>,
        fail: bool,
    }

    impl Sink for CountingSink {
        fn send(&self, _occurrence: &Occurrence) -> Result<(), SinkError> {
            self.sends.fetch_add(1, Ordering::Relaxed);
            if self.fail {
                Err(SinkError::Send("test failure".into()))
            } else {
                Ok(())
            }
        }

        fn flush(&self) -> Result<(), SinkError> {
            Ok(())
        }

        fn name(&self) -> &str {
            self.name
        }
    }

    #[test]
    fn multisink_continues_after_one_sink_fails() {
        let failed_sends = Arc::new(AtomicUsize::new(0));
        let ok_sends = Arc::new(AtomicUsize::new(0));
        let metrics = metrics::TapioMetrics::new().unwrap();
        let multi = MultiSink {
            sinks: vec![
                Box::new(CountingSink {
                    name: "fail",
                    sends: failed_sends.clone(),
                    fail: true,
                }),
                Box::new(CountingSink {
                    name: "ok",
                    sends: ok_sends.clone(),
                    fail: false,
                }),
            ],
            metrics: Some(metrics),
        };
        let occ = Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        );

        assert!(multi.send(&occ).is_err());
        assert_eq!(failed_sends.load(Ordering::Relaxed), 1);
        assert_eq!(ok_sends.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn multisink_records_partial_failure_metrics() {
        let metrics = metrics::TapioMetrics::new().unwrap();
        let registry = metrics.registry.clone();
        let multi = MultiSink {
            sinks: vec![
                Box::new(CountingSink {
                    name: "fail",
                    sends: Arc::new(AtomicUsize::new(0)),
                    fail: true,
                }),
                Box::new(CountingSink {
                    name: "ok",
                    sends: Arc::new(AtomicUsize::new(0)),
                    fail: false,
                }),
            ],
            metrics: Some(metrics),
        };
        let occ = Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        );

        assert!(multi.send(&occ).is_err());
        let gathered = registry.gather();
        let mut encoded = String::new();
        prometheus::TextEncoder::new()
            .encode_utf8(&gathered, &mut encoded)
            .unwrap();
        assert!(encoded.contains("tapio_sink_writes_total{result=\"err\",sink=\"fail\"} 1"));
        assert!(encoded.contains("tapio_sink_writes_total{result=\"ok\",sink=\"ok\"} 1"));
    }

    #[test]
    fn multisink_records_real_http_export_failure_as_err() {
        let metrics = metrics::TapioMetrics::new().unwrap();
        let registry = metrics.registry.clone();
        let multi = MultiSink {
            sinks: vec![Box::new(sink::http::HttpSink::new(
                "http://127.0.0.1:1",
                1,
                std::time::Duration::from_secs(3600),
            ))],
            metrics: Some(metrics),
        };
        let occ = Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        );

        assert!(multi.send(&occ).is_err());
        let gathered = registry.gather();
        let mut encoded = String::new();
        prometheus::TextEncoder::new()
            .encode_utf8(&gathered, &mut encoded)
            .unwrap();
        assert!(encoded.contains("tapio_sink_writes_total{result=\"err\",sink=\"http\"} 1"));
    }
}
