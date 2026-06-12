mod config;
mod controller;
mod httpc;
mod metrics;
mod observer;
mod registration;
mod sink;

use std::io::{self, Write};
use std::time::Duration;
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

#[derive(Debug)]
struct Args {
    config: String,
    sinks: Vec<String>,
    ebpf_dir: String,
    data_dir: String,
    http_endpoint: String,
    controller_endpoint: Option<String>,
    config_poll_interval: Duration,
    heartbeat_interval: Duration,
}

impl Default for Args {
    fn default() -> Self {
        Self {
            config: "/etc/tapio/tapio.toml".into(),
            sinks: vec!["stdout".into()],
            ebpf_dir: "/opt/tapio/ebpf".into(),
            data_dir: ".tapio/occurrences".into(),
            http_endpoint: "http://localhost:8765".into(),
            controller_endpoint: None,
            config_poll_interval: Duration::from_secs(30),
            heartbeat_interval: Duration::from_secs(30),
        }
    }
}

impl Args {
    fn parse() -> anyhow::Result<Self> {
        Self::parse_from(std::env::args().skip(1))
    }

    fn parse_from(values: impl IntoIterator<Item = String>) -> anyhow::Result<Self> {
        let mut args = Self::default();
        let mut explicit_sinks = Vec::new();
        let mut iter = values.into_iter().peekable();

        while let Some(arg) = iter.next() {
            let (flag, inline_value) = match arg.split_once('=') {
                Some((flag, value)) => (flag, Some(value.to_string())),
                None => (arg.as_str(), None),
            };

            match flag {
                "--help" | "-h" => {
                    print_help()?;
                    std::process::exit(0);
                }
                "--version" | "-V" => {
                    print_version()?;
                    std::process::exit(0);
                }
                "--config" => args.config = next_arg(flag, inline_value, &mut iter)?,
                "--sink" => explicit_sinks.push(next_arg(flag, inline_value, &mut iter)?),
                "--ebpf-dir" => args.ebpf_dir = next_arg(flag, inline_value, &mut iter)?,
                "--data-dir" => args.data_dir = next_arg(flag, inline_value, &mut iter)?,
                "--http-endpoint" => args.http_endpoint = next_arg(flag, inline_value, &mut iter)?,
                "--controller-endpoint" => {
                    let endpoint = next_arg(flag, inline_value, &mut iter)?;
                    validate_http_endpoint("--controller-endpoint", &endpoint)?;
                    args.controller_endpoint = Some(endpoint);
                }
                "--config-poll-interval" => {
                    let value = next_arg(flag, inline_value, &mut iter)?;
                    let seconds = value.parse::<u64>().map_err(|e| {
                        anyhow::anyhow!("--config-poll-interval must be seconds: {e}")
                    })?;
                    if seconds < 5 {
                        anyhow::bail!("--config-poll-interval must be at least 5 seconds");
                    }
                    args.config_poll_interval = Duration::from_secs(seconds);
                }
                "--heartbeat-interval" => {
                    let value = next_arg(flag, inline_value, &mut iter)?;
                    let seconds = value.parse::<u64>().map_err(|e| {
                        anyhow::anyhow!("--heartbeat-interval must be seconds: {e}")
                    })?;
                    if seconds < 5 {
                        anyhow::bail!("--heartbeat-interval must be at least 5 seconds");
                    }
                    args.heartbeat_interval = Duration::from_secs(seconds);
                }
                other => anyhow::bail!("unknown argument: {other}"),
            }
        }

        if !explicit_sinks.is_empty() {
            args.sinks = explicit_sinks;
        }

        Ok(args)
    }
}

fn validate_http_endpoint(flag: &str, endpoint: &str) -> anyhow::Result<()> {
    if endpoint.starts_with("https://") {
        anyhow::bail!("{flag} does not support https://; use an explicit http:// endpoint");
    }
    if !endpoint.starts_with("http://") {
        anyhow::bail!("{flag} must start with http://");
    }
    Ok(())
}

fn next_arg<I>(
    flag: &str,
    inline_value: Option<String>,
    iter: &mut std::iter::Peekable<I>,
) -> anyhow::Result<String>
where
    I: Iterator<Item = String>,
{
    if let Some(value) = inline_value {
        if value.is_empty() {
            anyhow::bail!("{flag} requires a non-empty value");
        }
        return Ok(value);
    }

    match iter.next() {
        Some(value) if !value.starts_with('-') => Ok(value),
        Some(value) => anyhow::bail!("{flag} requires a value, got {value}"),
        None => anyhow::bail!("{flag} requires a value"),
    }
}

fn print_version() -> anyhow::Result<()> {
    writeln!(io::stdout(), "tapio-agent {}", env!("CARGO_PKG_VERSION"))?;
    Ok(())
}

fn print_help() -> anyhow::Result<()> {
    write!(
        io::stdout(),
        "\
tapio-agent {}
Opinionated eBPF observer for Linux and Kubernetes

Usage: tapio-agent [OPTIONS]

Options:
  --config <path>          TOML config file [default: /etc/tapio/tapio.toml]
  --sink <name>            Output sink: stdout, file, http, controller; otlp with --features otlp [default: stdout]
  --ebpf-dir <path>        Directory containing compiled eBPF .o files [default: /opt/tapio/ebpf]
  --data-dir <path>        Directory for file sink output [default: .tapio/occurrences]
  --http-endpoint <url>    Endpoint for the http sink [default: http://localhost:8765]
  --controller-endpoint <url> Controller config URL (http:// only)
  --config-poll-interval <seconds> Config poll interval, minimum 5 [default: 30]
  --heartbeat-interval <seconds> Controller heartbeat interval, minimum 5 [default: 30]
  -h, --help               Print help
  -V, --version            Print version",
        env!("CARGO_PKG_VERSION")
    )?;
    Ok(())
}

fn create_sinks(
    args: &Args,
    cfg: &config::Config,
    controller_state: std::sync::Arc<controller::ControllerState>,
    metrics: metrics::TapioMetrics,
) -> anyhow::Result<Vec<Box<dyn tapio_common::sink::Sink>>> {
    #[cfg(not(feature = "otlp"))]
    let _ = cfg;

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
            "controller" => {
                let endpoint = args.controller_endpoint.clone().ok_or_else(|| {
                    anyhow::anyhow!("--sink controller requires --controller-endpoint")
                })?;
                let controller = controller::ControllerConfig {
                    endpoint,
                    agent_id: agent_id(),
                    node_name: node_name(),
                    poll_interval: args.config_poll_interval,
                    heartbeat_interval: args.heartbeat_interval,
                };
                sinks.push(Box::new(sink::controller::ControllerSink::new(
                    controller,
                    controller_state.clone(),
                    metrics.clone(),
                )));
            }
            "otlp" => {
                #[cfg(feature = "otlp")]
                {
                    sinks.push(Box::new(sink::otlp::OtlpSink::new(
                        &cfg.otlp.endpoint,
                        cfg.otlp.auth_header.clone(),
                        cfg.otlp.batch_size,
                        std::time::Duration::from_secs(cfg.otlp.flush_interval_secs),
                    )?));
                }
                #[cfg(not(feature = "otlp"))]
                {
                    anyhow::bail!("otlp sink requires building tapio-agent with --features otlp");
                }
            }
            other => anyhow::bail!("unknown sink: {other}"),
        }
    }
    if sinks.is_empty() {
        anyhow::bail!("at least one sink is required");
    }
    Ok(sinks)
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn node_name() -> String {
    std::env::var("TAPIO_NODE_NAME")
        .or_else(|_| std::env::var("HOSTNAME"))
        .unwrap_or_else(|_| "unknown-node".into())
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn agent_id() -> String {
    std::env::var("TAPIO_AGENT_ID").unwrap_or_else(|_| format!("node/{}", node_name()))
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn config_file_mentions_thresholds(path: &std::path::Path) -> bool {
    std::fs::read_to_string(path)
        .map(|content| content.lines().any(|line| line.trim() == "[thresholds]"))
        .unwrap_or(false)
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
        .with_max_level(log_level_from_env())
        .with_writer(|| BrokenPipeGuard)
        .init();

    let args = Args::parse()?;
    #[allow(unused_variables)]
    let cfg = config::load(std::path::Path::new(&args.config))?;
    info!("tapio v4 — kernel eyes");

    let tapio_metrics = metrics::TapioMetrics::new()?;
    let controller_state =
        controller::ControllerState::new(if args.controller_endpoint.is_some() {
            "0"
        } else {
            "1"
        });
    let sinks = create_sinks(&args, &cfg, controller_state.clone(), tapio_metrics.clone())?;
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

        let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

        // Start metrics server if enabled
        if cfg.metrics.enabled {
            let metrics_state = tapio_metrics.clone();
            let metrics_ip: std::net::IpAddr = cfg
                .metrics
                .bind_address
                .parse()
                .map_err(|e| anyhow::anyhow!("invalid metrics bind_address: {e}"))?;
            let metrics_addr = std::net::SocketAddr::new(metrics_ip, cfg.metrics.port);
            let metrics_shutdown_rx = shutdown_rx.clone();
            tokio::spawn(async move {
                if let Err(e) =
                    metrics::serve(metrics_state, metrics_addr, metrics_shutdown_rx).await
                {
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
        let controller_mode = args.controller_endpoint.is_some();
        if controller_mode && config_file_mentions_thresholds(std::path::Path::new(&args.config)) {
            tracing::warn!(
                config = %args.config,
                "TOML [thresholds] ignored"
            );
        }
        let tapio_config = if controller_mode {
            tapio_common::ebpf::TapioConfig::default()
        } else {
            cfg.tapio_config()
        };
        let carriers = observer::ConfigCarriers::default();
        let standalone_thresholds = observer::node_pmc::PmcThresholds {
            stall_pct_warning: cfg.thresholds.stall_pct_warning,
            stall_pct_critical: cfg.thresholds.stall_pct_critical,
            ipc_degradation: cfg.thresholds.ipc_degradation,
        };
        let (pmc_thresholds_tx, pmc_thresholds_rx) =
            tokio::sync::watch::channel(standalone_thresholds);
        info!(
            generation = tapio_config.generation,
            flags = tapio_config.flags,
            "compiled primitive tapio_config"
        );

        let sink1: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let mut tasks = tokio::task::JoinSet::new();

        if let Some(endpoint) = args.controller_endpoint.clone() {
            tracing::info!(endpoint = %endpoint, "controller mode");
            let controller = controller::ControllerConfig {
                endpoint,
                agent_id: agent_id(),
                node_name: node_name(),
                poll_interval: args.config_poll_interval,
                heartbeat_interval: args.heartbeat_interval,
            };
            let registration_controller = controller.clone();
            let registration_metrics = tapio_metrics.clone();
            let registration_shutdown = shutdown_rx.clone();
            let registration_state = controller_state.clone();
            tasks.spawn(async move {
                registration::registration_loop(
                    registration_controller,
                    registration_state,
                    registration_metrics,
                    registration_shutdown,
                )
                .await;
            });
            let poll_carriers = carriers.clone();
            let poll_thresholds = pmc_thresholds_tx.clone();
            let poll_metrics = tapio_metrics.clone();
            let poll_state = controller_state.clone();
            let poll_shutdown = shutdown_rx.clone();
            tasks.spawn(async move {
                controller::poll_loop(
                    controller,
                    poll_carriers,
                    poll_thresholds,
                    poll_metrics,
                    poll_state,
                    poll_shutdown,
                )
                .await;
            });
        } else {
            tracing::info!("standalone mode");
        }

        let metrics1 = tapio_metrics.clone();
        let rx1 = shutdown_rx.clone();
        let dir1 = ebpf_dir.clone();
        let config1 = tapio_config;
        let carriers1 = carriers.clone();
        tasks.spawn(async move {
            let path = format!("{dir1}/network_monitor.o");
            if let Err(e) = observer::network::run(
                &path,
                sink1.as_ref(),
                boot_offset_ns,
                config1,
                &carriers1,
                &metrics1,
                rx1,
            )
            .await
            {
                tracing::error!(error = %e, "network observer failed");
            }
        });

        let sink2: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let metrics2 = tapio_metrics.clone();
        let rx2 = shutdown_rx.clone();
        let dir2 = ebpf_dir.clone();
        let config2 = tapio_config;
        let carriers2 = carriers.clone();
        tasks.spawn(async move {
            let path = format!("{dir2}/container_monitor.o");
            if let Err(e) = observer::container::run(
                &path,
                sink2.as_ref(),
                boot_offset_ns,
                config2,
                &carriers2,
                &metrics2,
                rx2,
            )
            .await
            {
                tracing::error!(error = %e, "container observer failed");
            }
        });

        let sink3: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let metrics3 = tapio_metrics.clone();
        let rx3 = shutdown_rx.clone();
        let dir3 = ebpf_dir.clone();
        let config3 = tapio_config;
        let carriers3 = carriers.clone();
        tasks.spawn(async move {
            let path = format!("{dir3}/storage_monitor.o");
            if let Err(e) = observer::storage::run(
                &path,
                sink3.as_ref(),
                boot_offset_ns,
                config3,
                &carriers3,
                &metrics3,
                rx3,
            )
            .await
            {
                tracing::error!(error = %e, "storage observer failed");
            }
        });

        let sink4: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let metrics4 = tapio_metrics.clone();
        let rx4 = shutdown_rx.clone();
        let dir4 = ebpf_dir.clone();
        let config4 = tapio_config;
        let carriers4 = carriers.clone();
        let thresholds4 = pmc_thresholds_rx.clone();
        let pmc_runtime = observer::node_pmc::PmcRuntimeConfig {
            tapio_config: config4,
            carriers: carriers4,
            thresholds_rx: thresholds4,
        };
        tasks.spawn(async move {
            let path = format!("{dir4}/node_pmc_monitor.o");
            if let Err(e) = observer::node_pmc::run(
                &path,
                sink4.as_ref(),
                boot_offset_ns,
                pmc_runtime,
                &metrics4,
                rx4,
            )
            .await
            {
                tracing::error!(error = %e, "PMC observer failed");
            }
        });

        while let Some(result) = tasks.join_next().await {
            if let Err(e) = result {
                tracing::error!(error = %e, "agent task failed");
            }
        }
        info!("all observers stopped");
        Ok(())
    }

    #[cfg(not(target_os = "linux"))]
    {
        let _ = (args, multi_sink, tapio_metrics);
        anyhow::bail!("eBPF requires Linux — tapio-agent cannot run on this platform");
    }
}

fn log_level_from_env() -> tracing::level_filters::LevelFilter {
    log_level_from_rust_log(std::env::var("RUST_LOG").ok().as_deref())
}

fn log_level_from_rust_log(value: Option<&str>) -> tracing::level_filters::LevelFilter {
    let Some(value) = value else {
        return tracing::level_filters::LevelFilter::ERROR;
    };
    let directive = value
        .split(',')
        .next()
        .map(|part| part.rsplit_once('=').map_or(part, |(_, level)| level))
        .unwrap_or("")
        .trim()
        .to_ascii_lowercase();
    match directive.as_str() {
        "trace" => tracing::level_filters::LevelFilter::TRACE,
        "debug" => tracing::level_filters::LevelFilter::DEBUG,
        "info" => tracing::level_filters::LevelFilter::INFO,
        "warn" => tracing::level_filters::LevelFilter::WARN,
        "error" => tracing::level_filters::LevelFilter::ERROR,
        "off" => tracing::level_filters::LevelFilter::OFF,
        _ => tracing::level_filters::LevelFilter::ERROR,
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
            metrics: Some(metrics.clone()),
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
            metrics: Some(metrics.clone()),
        };
        let occ = Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        );

        assert!(multi.send(&occ).is_err());
        let encoded = metrics.encode();
        assert!(encoded.contains("tapio_sink_writes_total{sink=\"fail\",result=\"err\"} 1"));
        assert!(encoded.contains("tapio_sink_writes_total{sink=\"ok\",result=\"ok\"} 1"));
    }

    #[test]
    fn multisink_records_real_http_export_failure_as_err() {
        let metrics = metrics::TapioMetrics::new().unwrap();
        let multi = MultiSink {
            sinks: vec![Box::new(sink::http::HttpSink::new(
                "http://127.0.0.1:1",
                1,
                std::time::Duration::from_secs(3600),
            ))],
            metrics: Some(metrics.clone()),
        };
        let occ = Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        );

        assert!(multi.send(&occ).is_err());
        let encoded = metrics.encode();
        assert!(encoded.contains("tapio_sink_writes_total{sink=\"http\",result=\"err\"} 1"));
    }

    #[test]
    fn rust_log_level_parser_accepts_plain_and_targeted_levels() {
        assert_eq!(
            log_level_from_rust_log(Some("debug")),
            tracing::level_filters::LevelFilter::DEBUG
        );
        assert_eq!(
            log_level_from_rust_log(Some("tapio_agent=info")),
            tracing::level_filters::LevelFilter::INFO
        );
        assert_eq!(
            log_level_from_rust_log(Some("off")),
            tracing::level_filters::LevelFilter::OFF
        );
    }

    #[test]
    fn rust_log_level_parser_defaults_to_error_for_absent_or_unknown() {
        assert_eq!(
            log_level_from_rust_log(None),
            tracing::level_filters::LevelFilter::ERROR
        );
        assert_eq!(
            log_level_from_rust_log(Some("verbose")),
            tracing::level_filters::LevelFilter::ERROR
        );
    }

    #[test]
    fn args_controller_endpoint_accepts_http_only() {
        let args = Args::parse_from([
            "--controller-endpoint".to_string(),
            "http://controller:8080".to_string(),
        ])
        .unwrap();

        assert_eq!(
            args.controller_endpoint.as_deref(),
            Some("http://controller:8080")
        );
    }

    #[test]
    fn args_controller_endpoint_rejects_https() {
        let err = Args::parse_from([
            "--controller-endpoint".to_string(),
            "https://controller:8443".to_string(),
        ])
        .unwrap_err()
        .to_string();

        assert!(err.contains("does not support https://"));
    }

    #[test]
    fn args_config_poll_interval_enforces_minimum() {
        let err = Args::parse_from(["--config-poll-interval".to_string(), "4".to_string()])
            .unwrap_err()
            .to_string();

        assert!(err.contains("at least 5 seconds"));
    }
}
