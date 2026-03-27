#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
mod enrichment;
mod observer;
mod sink;

use clap::Parser;
use tracing::info;

#[derive(Parser)]
#[command(
    name = "tapio-agent",
    version,
    about = "eBPF kernel observer for Kubernetes"
)]
struct Args {
    /// Output sinks (stdout, file). Can be specified multiple times.
    #[arg(long = "sink", default_values_t = vec!["stdout".to_string()])]
    sinks: Vec<String>,

    /// Directory containing compiled eBPF .o files
    #[arg(long, default_value = "/opt/tapio/ebpf")]
    ebpf_dir: String,

    /// Directory for file sink output
    #[arg(long, default_value = ".tapio/occurrences")]
    data_dir: String,

    /// Kubernetes node name (for enrichment)
    #[arg(long, env = "TAPIO_NODE_NAME", default_value = "unknown")]
    node_name: String,

    /// Cluster name (for enrichment)
    #[arg(long, env = "TAPIO_CLUSTER", default_value = "default")]
    cluster: String,
}

fn create_sinks(args: &Args) -> anyhow::Result<Vec<Box<dyn tapio_common::sink::Sink>>> {
    let mut sinks: Vec<Box<dyn tapio_common::sink::Sink>> = Vec::new();
    for name in &args.sinks {
        match name.as_str() {
            "stdout" => sinks.push(Box::new(sink::stdout::StdoutSink)),
            "file" => sinks.push(Box::new(sink::file::FileSink::new(&args.data_dir))),
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
}

impl tapio_common::sink::Sink for MultiSink {
    fn send(
        &self,
        occurrence: &tapio_common::occurrence::Occurrence,
    ) -> Result<(), tapio_common::sink::SinkError> {
        let mut last_err = None;
        for s in &self.sinks {
            if let Err(e) = s.send(occurrence) {
                tracing::warn!(sink = s.name(), error = %e, "sink error");
                last_err = Some(e);
            }
        }
        match last_err {
            Some(e) => Err(e),
            None => Ok(()),
        }
    }

    fn flush(&self) -> Result<(), tapio_common::sink::SinkError> {
        for s in &self.sinks {
            s.flush()?;
        }
        Ok(())
    }

    fn name(&self) -> &str {
        "multi"
    }
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    let args = Args::parse();
    info!("tapio v4 — kernel eyes");

    let sinks = create_sinks(&args)?;
    let sink_names: Vec<&str> = sinks.iter().map(|s| s.name()).collect();
    info!(sinks = ?sink_names, "sinks configured");

    let multi_sink: std::sync::Arc<MultiSink> = std::sync::Arc::new(MultiSink { sinks });

    // Pod cache for K8s enrichment (works on all platforms, populated on Linux)
    let pod_cache = std::sync::Arc::new(enrichment::PodCache::new(
        args.node_name.clone(),
        args.cluster.clone(),
    ));

    #[cfg(target_os = "linux")]
    {
        use std::sync::Arc;

        let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

        tokio::spawn(async move {
            tokio::signal::ctrl_c().await.ok();
            info!("received SIGINT, shutting down");
            let _ = shutdown_tx.send(true);
        });

        // Start K8s pod watcher (best-effort — continues even if K8s unavailable)
        let watcher_cache = pod_cache.clone();
        let watcher_node = args.node_name.clone();
        tokio::spawn(async move {
            match kube::Client::try_default().await {
                Ok(client) => {
                    if let Err(e) =
                        enrichment::watch_pods(client, watcher_cache, watcher_node).await
                    {
                        tracing::warn!(error = %e, "pod watcher stopped");
                    }
                }
                Err(e) => {
                    tracing::warn!(error = %e, "K8s client unavailable — running without enrichment");
                }
            }
        });

        let ebpf_dir = args.ebpf_dir.clone();

        let sink1: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let cache1 = pod_cache.clone();
        let rx1 = shutdown_rx.clone();
        let dir1 = ebpf_dir.clone();
        let net = tokio::spawn(async move {
            let path = format!("{dir1}/network_monitor.o");
            if let Err(e) = observer::network::run(&path, sink1.as_ref(), &cache1, rx1).await {
                tracing::error!(error = %e, "network observer failed");
            }
        });

        let sink2: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let cache2 = pod_cache.clone();
        let rx2 = shutdown_rx.clone();
        let dir2 = ebpf_dir.clone();
        let ctr = tokio::spawn(async move {
            let path = format!("{dir2}/container_monitor.o");
            if let Err(e) = observer::container::run(&path, sink2.as_ref(), &cache2, rx2).await {
                tracing::error!(error = %e, "container observer failed");
            }
        });

        let sink3: Arc<dyn tapio_common::sink::Sink> = multi_sink.clone();
        let cache3 = pod_cache.clone();
        let rx3 = shutdown_rx.clone();
        let dir3 = ebpf_dir.clone();
        let stg = tokio::spawn(async move {
            let path = format!("{dir3}/storage_monitor.o");
            if let Err(e) = observer::storage::run(&path, sink3.as_ref(), &cache3, rx3).await {
                tracing::error!(error = %e, "storage observer failed");
            }
        });

        info!("PMC observer not yet wired (requires perf_event_open setup)");

        let _ = tokio::join!(net, ctr, stg);
        info!(pods_cached = pod_cache.pod_count(), "all observers stopped");
    }

    #[cfg(not(target_os = "linux"))]
    {
        let _ = (args, multi_sink, pod_cache);
        anyhow::bail!("eBPF requires Linux — tapio-agent cannot run on this platform");
    }
}
