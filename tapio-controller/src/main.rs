use std::net::SocketAddr;
use std::path::PathBuf;

use tapio_controller::{
    ControllerState, config_response_from_compiled, default_config_response, load_profile, router,
};

#[derive(Debug)]
struct Args {
    addr: SocketAddr,
    profile: Option<PathBuf>,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    let args = parse_args(std::env::args().skip(1))?;
    let config = match &args.profile {
        Some(path) => config_response_from_compiled(load_profile(path)?, 1)?,
        None => default_config_response()?,
    };

    let state = ControllerState::new(config);
    tracing::info!(addr = %args.addr, "tapio-controller starting");
    let listener = tokio::net::TcpListener::bind(args.addr).await?;
    axum::serve(listener, router(state)).await?;
    Ok(())
}

fn parse_args(args: impl IntoIterator<Item = String>) -> anyhow::Result<Args> {
    let mut addr = None;
    let mut profile = None;
    let mut iter = args.into_iter();

    while let Some(arg) = iter.next() {
        if arg == "--profile" {
            let path = iter
                .next()
                .ok_or_else(|| anyhow::anyhow!("--profile requires a path"))?;
            if profile.replace(PathBuf::from(path)).is_some() {
                anyhow::bail!("--profile may be provided at most once in v0");
            }
        } else if arg.starts_with("--") {
            anyhow::bail!("unknown argument {arg}");
        } else if addr.replace(arg.parse::<SocketAddr>()?).is_some() {
            anyhow::bail!("controller address may be provided at most once");
        }
    }

    Ok(Args {
        addr: addr.unwrap_or_else(|| SocketAddr::from(([127, 0, 0, 1], 8080))),
        profile,
    })
}
