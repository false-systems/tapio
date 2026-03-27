mod observer;
mod sink;

use clap::Parser;
use tracing::info;

#[derive(Parser)]
#[command(name = "tapio-agent", version, about = "eBPF kernel observer for Kubernetes")]
struct Args {
    /// Output sink (stdout)
    #[arg(long, default_value = "stdout")]
    sink: String,

    /// Path to compiled eBPF object file
    #[arg(long, default_value = "/opt/tapio/ebpf/network_monitor.o")]
    ebpf_path: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    let args = Args::parse();
    info!("tapio v4 — kernel eyes");

    let sink: Box<dyn tapio_common::sink::Sink> = match args.sink.as_str() {
        "stdout" => Box::new(sink::stdout::StdoutSink),
        other => anyhow::bail!("unknown sink: {other}"),
    };

    #[cfg(target_os = "linux")]
    {
        let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

        tokio::spawn(async move {
            tokio::signal::ctrl_c().await.ok();
            info!("received SIGINT, shutting down");
            let _ = shutdown_tx.send(true);
        });

        observer::network::run(&args.ebpf_path, sink.as_ref(), shutdown_rx).await?;
    }

    #[cfg(not(target_os = "linux"))]
    {
        let _ = (args, sink);
        anyhow::bail!("eBPF requires Linux — tapio-agent cannot run on this platform");
    }
}
