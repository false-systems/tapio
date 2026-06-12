use std::sync::Arc;
use std::sync::RwLock;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

#[cfg(target_os = "linux")]
use tapio_common::ebpf::TapioConfig;
#[cfg(target_os = "linux")]
use tapio_wire::ConfigResponse;

#[cfg(target_os = "linux")]
use crate::observer::ConfigCarriers;

#[derive(Debug, Clone)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub struct ControllerConfig {
    pub endpoint: String,
    pub agent_id: String,
    pub node_name: String,
    pub poll_interval: Duration,
    pub heartbeat_interval: Duration,
}

#[derive(Debug)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub struct ControllerState {
    config_version: RwLock<String>,
    send_interval_ms: AtomicU64,
    max_batch_events: AtomicU64,
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
impl ControllerState {
    pub fn new(config_version: impl Into<String>) -> Arc<Self> {
        Arc::new(Self {
            config_version: RwLock::new(config_version.into()),
            send_interval_ms: AtomicU64::new(1000),
            max_batch_events: AtomicU64::new(256),
        })
    }

    pub fn config_version(&self) -> String {
        self.config_version
            .read()
            .map(|version| version.clone())
            .unwrap_or_else(|_| "0".into())
    }

    pub fn set_config_version(&self, version: impl Into<String>) {
        if let Ok(mut current) = self.config_version.write() {
            *current = version.into();
        }
    }

    pub fn batching(&self) -> (u64, u64) {
        (
            self.send_interval_ms.load(Ordering::Relaxed),
            self.max_batch_events.load(Ordering::Relaxed),
        )
    }

    pub fn set_batching(&self, send_interval_ms: u64, max_batch_events: u64) {
        self.send_interval_ms
            .store(send_interval_ms.max(1), Ordering::Relaxed);
        self.max_batch_events
            .store(max_batch_events.max(1), Ordering::Relaxed);
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub enum PollOutcome {
    Applied { generation: u32, hash: String },
    NotModified,
}

#[cfg(target_os = "linux")]
pub async fn poll_loop(
    controller: ControllerConfig,
    carriers: ConfigCarriers,
    pmc_thresholds_tx: tokio::sync::watch::Sender<crate::observer::node_pmc::PmcThresholds>,
    metrics: crate::metrics::TapioMetrics,
    state: Arc<ControllerState>,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) {
    let mut etag: Option<String> = None;
    tracing::info!(
        endpoint = %controller.endpoint,
        interval_secs = controller.poll_interval.as_secs(),
        "config poll start"
    );
    let mut interval = tokio::time::interval(controller.poll_interval);

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!("config poll stop");
                break;
            }
            _ = interval.tick() => {
                match fetch_once(&controller, etag.as_deref(), &carriers, &pmc_thresholds_tx).await {
                    Ok(PollOutcome::Applied { generation, hash }) => {
                        etag = Some(format!("\"{hash}\""));
                        state.set_config_version(generation.to_string());
                        let label = "applied";
                        metrics.config_fetch_total.with_label_values(&[label]).inc();
                        tracing::info!(generation, hash = %hash, "config applied");
                    }
                    Ok(PollOutcome::NotModified) => {
                        metrics.config_fetch_total.with_label_values(&["not_modified"]).inc();
                    }
                    Err(FetchError::Rejected(error)) => {
                        metrics.config_fetch_total.with_label_values(&["rejected"]).inc();
                        tracing::warn!(error = %error, "config rejected");
                    }
                    Err(FetchError::Transport(error)) => {
                        metrics.config_fetch_total.with_label_values(&["error"]).inc();
                        tracing::warn!(error = %error, "config fetch failed");
                    }
                }
            }
        }
    }
}

#[cfg(target_os = "linux")]
async fn fetch_once(
    controller: &ControllerConfig,
    etag: Option<&str>,
    carriers: &ConfigCarriers,
    pmc_thresholds_tx: &tokio::sync::watch::Sender<crate::observer::node_pmc::PmcThresholds>,
) -> Result<PollOutcome, FetchError> {
    let url = config_url(controller);
    let mut headers = Vec::new();
    if let Some(etag) = etag {
        headers.push(("If-None-Match", etag.to_string()));
    }
    let response = tokio::task::spawn_blocking(move || {
        crate::httpc::get(
            &url,
            &headers,
            crate::httpc::DEFAULT_TIMEOUT,
            crate::httpc::DEFAULT_MAX_RESPONSE_BYTES,
        )
    })
    .await
    .map_err(|e| FetchError::Transport(format!("join fetch: {e}")))?
    .map_err(FetchError::Transport)?;

    match response.status {
        304 => Ok(PollOutcome::NotModified),
        200 => {
            let config: ConfigResponse = serde_json::from_slice(&response.body)
                .map_err(|e| FetchError::Rejected(format!("decode ConfigResponse: {e}")))?;
            let generation = config.version.parse::<u32>().map_err(|_| {
                FetchError::Rejected(format!("bad config version {:?}", config.version))
            })?;
            config
                .validate()
                .map_err(|e| FetchError::Rejected(e.to_string()))?;
            reject_empty_config_hash(&config.config_hash)?;
            let tapio_config =
                crate::config::tapio_config_from_compiled(&config.config, &config.version)
                    .map_err(|e| FetchError::Rejected(e.to_string()))?;
            apply_config(carriers, pmc_thresholds_tx, &tapio_config, &config.config)?;
            Ok(PollOutcome::Applied {
                generation,
                hash: config.config_hash,
            })
        }
        status => Err(FetchError::Transport(format!("HTTP {status}"))),
    }
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn reject_empty_config_hash(hash: &str) -> Result<(), FetchError> {
    if hash.is_empty() {
        return Err(FetchError::Rejected(
            "config response missing config_hash".into(),
        ));
    }
    Ok(())
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
#[cfg(target_os = "linux")]
fn apply_config(
    carriers: &ConfigCarriers,
    pmc_thresholds_tx: &tokio::sync::watch::Sender<crate::observer::node_pmc::PmcThresholds>,
    tapio_config: &TapioConfig,
    compiled: &tapio_wire::CompiledConfig,
) -> Result<(), FetchError> {
    let failed = carriers.update_all(tapio_config);
    if !failed.is_empty() {
        tracing::warn!(failed = ?failed, "partial config fan-out");
    }
    pmc_thresholds_tx
        .send(crate::config::pmc_thresholds_from_compiled(compiled))
        .map_err(|e| FetchError::Transport(format!("PMC thresholds: {e}")))?;
    Ok(())
}

#[derive(Debug, PartialEq, Eq)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub enum FetchError {
    Transport(String),
    Rejected(String),
}

impl std::fmt::Display for FetchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Transport(error) | Self::Rejected(error) => f.write_str(error),
        }
    }
}

impl std::error::Error for FetchError {}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub fn config_url(controller: &ControllerConfig) -> String {
    format!(
        "{}/v1/agents/config?agent_id={}&node_name={}",
        controller.endpoint.trim_end_matches('/'),
        url_component(&controller.agent_id),
        url_component(&controller.node_name)
    )
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn url_component(value: &str) -> String {
    let mut out = String::new();
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char)
            }
            other => {
                out.push('%');
                out.push(HEX[(other >> 4) as usize] as char);
                out.push(HEX[(other & 0x0f) as usize] as char);
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn config_url_encodes_agent_and_node() {
        let controller = ControllerConfig {
            endpoint: "http://controller:8080/".into(),
            agent_id: "node/worker 1".into(),
            node_name: "worker/1".into(),
            poll_interval: Duration::from_secs(30),
            heartbeat_interval: Duration::from_secs(30),
        };
        assert_eq!(
            config_url(&controller),
            "http://controller:8080/v1/agents/config?agent_id=node%2Fworker%201&node_name=worker%2F1"
        );
    }

    #[test]
    fn empty_config_hash_is_rejected() {
        assert_eq!(
            reject_empty_config_hash("").unwrap_err(),
            FetchError::Rejected("config response missing config_hash".into())
        );
        assert!(reject_empty_config_hash("sha256:abc").is_ok());
    }
}
