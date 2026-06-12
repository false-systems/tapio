use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use tapio_wire::{
    HeartbeatCounters, HeartbeatRequest, HeartbeatResponse, HelloRequest, HelloResponse,
    ObserverStatus, WIRE_VERSION,
};

use crate::controller::{ControllerConfig, ControllerState};

const MAX_HELLO_RESPONSE_BYTES: usize = 16 * 1024;
const MAX_HEARTBEAT_RESPONSE_BYTES: usize = 16 * 1024;
const INITIAL_HELLO_BACKOFF: Duration = Duration::from_secs(1);
const MAX_HELLO_BACKOFF: Duration = Duration::from_secs(60);

#[cfg(target_os = "linux")]
pub async fn registration_loop(
    controller: ControllerConfig,
    state: Arc<ControllerState>,
    metrics: crate::metrics::TapioMetrics,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) {
    let started_at = Instant::now();
    let mut backoff = INITIAL_HELLO_BACKOFF;

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!("controller registration stop");
                return;
            }
            result = hello_once_async(controller.clone()) => {
                match result {
                    Ok(response) if response.accepted => {
                        state.set_batching(response.send_interval_ms, response.max_batch_events);
                        state.set_config_version(response.config_version.clone());
                        tracing::info!(
                            controller_id = %response.controller_id,
                            config_version = %response.config_version,
                            send_interval_ms = response.send_interval_ms,
                            max_batch_events = response.max_batch_events,
                            "controller hello accepted"
                        );
                        break;
                    }
                    Ok(response) => {
                        metrics.controller_send_failures_total.with_label_values(&["hello"]).inc();
                        tracing::warn!(
                            controller_id = %response.controller_id,
                            config_version = %response.config_version,
                            "controller hello rejected"
                        );
                    }
                    Err(error) => {
                        metrics.controller_send_failures_total.with_label_values(&["hello"]).inc();
                        tracing::warn!(error = %error, "controller hello failed");
                    }
                }
            }
        }

        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!("controller registration stop");
                return;
            }
            _ = tokio::time::sleep(backoff) => {}
        }
        backoff = (backoff * 2).min(MAX_HELLO_BACKOFF);
    }

    heartbeat_loop(controller, state, metrics, started_at, shutdown).await;
}

#[cfg(target_os = "linux")]
async fn heartbeat_loop(
    controller: ControllerConfig,
    state: Arc<ControllerState>,
    metrics: crate::metrics::TapioMetrics,
    started_at: Instant,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) {
    tracing::info!(
        interval_secs = controller.heartbeat_interval.as_secs(),
        "controller heartbeat start"
    );
    let mut interval = tokio::time::interval(controller.heartbeat_interval);

    loop {
        tokio::select! {
            _ = shutdown.changed() => {
                tracing::info!("controller heartbeat stop");
                break;
            }
            _ = interval.tick() => {
                let version_before = state.config_version();
                match heartbeat_once_async(
                    controller.clone(),
                    metrics.clone(),
                    state.clone(),
                    started_at,
                ).await {
                    Ok(response) => {
                        if response.next_config_version != version_before {
                            tracing::debug!(
                                applied_config_version = %version_before,
                                next_config_version = %response.next_config_version,
                                "controller reports newer config"
                            );
                        }
                    }
                    Err(error) => {
                        metrics.controller_send_failures_total.with_label_values(&["heartbeat"]).inc();
                        tracing::warn!(error = %error, "controller heartbeat failed");
                    }
                }
            }
        }
    }
}

#[cfg(target_os = "linux")]
async fn hello_once_async(controller: ControllerConfig) -> Result<HelloResponse, String> {
    tokio::task::spawn_blocking(move || hello_once(&controller))
        .await
        .map_err(|e| format!("join hello: {e}"))?
}

#[cfg(target_os = "linux")]
async fn heartbeat_once_async(
    controller: ControllerConfig,
    metrics: crate::metrics::TapioMetrics,
    state: Arc<ControllerState>,
    started_at: Instant,
) -> Result<HeartbeatResponse, String> {
    tokio::task::spawn_blocking(move || heartbeat_once(&controller, &metrics, &state, started_at))
        .await
        .map_err(|e| format!("join heartbeat: {e}"))?
}

pub fn hello_once(controller: &ControllerConfig) -> Result<HelloResponse, String> {
    let request = build_hello_request(controller, kernel_release());
    request.validate().map_err(|e| e.to_string())?;
    let body = serde_json::to_vec(&request).map_err(|e| format!("encode HelloRequest: {e}"))?;
    let response =
        crate::httpc::post_json_response(&hello_url(controller), &body, MAX_HELLO_RESPONSE_BYTES)?;
    if !matches!(response.status, 200..=299) {
        return Err(format!("HTTP {}", response.status));
    }
    let response: HelloResponse =
        serde_json::from_slice(&response.body).map_err(|e| format!("decode HelloResponse: {e}"))?;
    response.validate().map_err(|e| e.to_string())?;
    Ok(response)
}

pub fn heartbeat_once(
    controller: &ControllerConfig,
    metrics: &crate::metrics::TapioMetrics,
    state: &ControllerState,
    started_at: Instant,
) -> Result<HeartbeatResponse, String> {
    let request = build_heartbeat_request(controller, metrics, state, started_at);
    request.validate().map_err(|e| e.to_string())?;
    let body = serde_json::to_vec(&request).map_err(|e| format!("encode HeartbeatRequest: {e}"))?;
    let response = crate::httpc::post_json_response(
        &heartbeat_url(controller),
        &body,
        MAX_HEARTBEAT_RESPONSE_BYTES,
    )?;
    if !matches!(response.status, 200..=299) {
        return Err(format!("HTTP {}", response.status));
    }
    let response: HeartbeatResponse = serde_json::from_slice(&response.body)
        .map_err(|e| format!("decode HeartbeatResponse: {e}"))?;
    response.validate().map_err(|e| e.to_string())?;
    Ok(response)
}

fn build_hello_request(controller: &ControllerConfig, kernel_release: String) -> HelloRequest {
    HelloRequest {
        wire_version: WIRE_VERSION.into(),
        agent_id: controller.agent_id.clone(),
        node_name: controller.node_name.clone(),
        tapio_version: env!("CARGO_PKG_VERSION").into(),
        kernel_release,
        arch: std::env::consts::ARCH.into(),
        capabilities: Vec::new(),
        object_sizes: BTreeMap::new(),
        map_counts: BTreeMap::new(),
    }
}

fn build_heartbeat_request(
    controller: &ControllerConfig,
    metrics: &crate::metrics::TapioMetrics,
    state: &ControllerState,
    started_at: Instant,
) -> HeartbeatRequest {
    HeartbeatRequest {
        wire_version: WIRE_VERSION.into(),
        agent_id: controller.agent_id.clone(),
        node_name: controller.node_name.clone(),
        config_version: state.config_version(),
        uptime_seconds: started_at.elapsed().as_secs(),
        observers: BTreeMap::from([
            ("network".into(), ObserverStatus::Running),
            ("container".into(), ObserverStatus::Running),
            ("storage".into(), ObserverStatus::Running),
            ("node_pmc".into(), ObserverStatus::Running),
        ]),
        counters: HeartbeatCounters {
            events_total: metrics.events_total.sum(),
            malformed_events_total: metrics.malformed_events_total.sum(),
            lost_events_total: metrics.lost_events_total.sum(),
            correlation_drops_total: metrics.correlation_drops_total.sum(),
            sink_drops_total: metrics.sink_drops_total.sum(),
            controller_send_failures_total: metrics.controller_send_failures_total.sum(),
        },
        degraded_reasons: Vec::new(),
    }
}

pub fn kernel_release() -> String {
    kernel_release_from("/proc/sys/kernel/osrelease")
}

fn kernel_release_from(path: &str) -> String {
    std::fs::read_to_string(path)
        .map(|value| value.trim().to_string())
        .ok()
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| "unknown".into())
}

fn hello_url(controller: &ControllerConfig) -> String {
    format!(
        "{}/v1/agents/hello",
        controller.endpoint.trim_end_matches('/')
    )
}

fn heartbeat_url(controller: &ControllerConfig) -> String {
    format!(
        "{}/v1/agents/heartbeat",
        controller.endpoint.trim_end_matches('/')
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::thread;

    fn controller(endpoint: String) -> ControllerConfig {
        ControllerConfig {
            endpoint,
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            poll_interval: Duration::from_secs(30),
            heartbeat_interval: Duration::from_secs(30),
        }
    }

    fn with_server(
        response: &'static [u8],
        f: impl FnOnce(String),
        check_request: impl FnOnce(&str) + Send + 'static,
    ) {
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(e)
                if e.kind() == std::io::ErrorKind::PermissionDenied
                    && std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_none() =>
            {
                eprintln!("SKIP registration loopback test: loopback bind not permitted ({e})");
                return;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener
            .local_addr()
            .expect("listener local address available");
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept test connection");
            let request = read_http_request(&mut stream);
            check_request(&String::from_utf8_lossy(&request));
            stream.write_all(response).expect("write response");
        });
        f(format!("http://{addr}"));
        handle.join().expect("server thread");
    }

    fn read_http_request(stream: &mut std::net::TcpStream) -> Vec<u8> {
        let mut request = Vec::new();
        let mut chunk = [0u8; 1024];
        loop {
            let n = stream.read(&mut chunk).expect("read request");
            if n == 0 {
                break;
            }
            request.extend_from_slice(&chunk[..n]);
            let Some(split) = request.windows(4).position(|window| window == b"\r\n\r\n") else {
                continue;
            };
            let headers = String::from_utf8_lossy(&request[..split]);
            let content_length = headers
                .lines()
                .find_map(|line| line.strip_prefix("Content-Length: "))
                .and_then(|value| value.parse::<usize>().ok())
                .unwrap_or(0);
            if request.len() >= split + 4 + content_length {
                break;
            }
        }
        request
    }

    #[test]
    fn kernel_release_falls_back_to_unknown() {
        assert_eq!(
            kernel_release_from("/definitely/not/a/kernel/release"),
            "unknown"
        );
    }

    #[test]
    fn hello_happy_path() {
        with_server(
            b"HTTP/1.1 200 OK\r\nContent-Length: 132\r\n\r\n{\"wire_version\":\"tapio-wire/v1\",\"accepted\":true,\"controller_id\":\"c1\",\"config_version\":\"7\",\"send_interval_ms\":500,\"max_batch_events\":64}",
            |endpoint| {
                let response = hello_once(&controller(endpoint)).expect("hello succeeds");
                assert_eq!(response.controller_id, "c1");
                assert_eq!(response.config_version, "7");
            },
            |request| {
                assert!(request.starts_with("POST /v1/agents/hello HTTP/1.1"));
                assert!(request.contains("\"wire_version\":\"tapio-wire/v1\""));
            },
        );
    }

    #[test]
    fn hello_5xx_is_retryable_error() {
        with_server(
            b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n",
            |endpoint| {
                let err = hello_once(&controller(endpoint)).unwrap_err();
                assert!(err.contains("HTTP 500"));
            },
            |_| {},
        );
    }

    #[test]
    fn heartbeat_body_is_valid_wire_request() {
        with_server(
            b"HTTP/1.1 200 OK\r\nContent-Length: 78\r\n\r\n{\"wire_version\":\"tapio-wire/v1\",\"accepted\":true,\"next_config_version\":\"8\"}",
            |endpoint| {
                let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
                metrics.events_total.with_label_values(&["network"]).inc_by(3);
                metrics
                    .controller_send_failures_total
                    .with_label_values(&["events"])
                    .inc();
                let state = ControllerState::new("7");
                let response =
                    heartbeat_once(&controller(endpoint), &metrics, &state, Instant::now())
                        .expect("heartbeat succeeds");
                assert_eq!(response.next_config_version, "8");
            },
            |request| {
                assert!(request.starts_with("POST /v1/agents/heartbeat HTTP/1.1"));
                let body = request
                    .split("\r\n\r\n")
                    .nth(1)
                    .expect("request body present");
                let heartbeat: HeartbeatRequest =
                    serde_json::from_str(body).expect("valid heartbeat json");
                heartbeat.validate().expect("valid heartbeat request");
                assert_eq!(heartbeat.config_version, "7");
                assert_eq!(heartbeat.counters.events_total, 3);
                assert_eq!(heartbeat.counters.controller_send_failures_total, 1);
            },
        );
    }

    #[test]
    fn hello_rejects_https() {
        let err = hello_once(&controller("https://controller:8443".into())).unwrap_err();
        assert!(err.contains("http://"));
    }
}
