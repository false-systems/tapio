use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime};

use axum::extract::State;
use axum::http::{HeaderMap, HeaderValue, StatusCode, Uri, header};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Serialize;
use sha2::{Digest, Sha256};
use tapio_profile::{BuiltinSet, EvidenceProfile, ProfileError, compile, validate};
use tapio_wire::{
    BatchingConfig, CompiledConfig, ConfigResponse, EventBatchRequest, EventBatchResponse,
    HeartbeatRequest, HeartbeatResponse, HelloRequest, HelloResponse, WIRE_VERSION, WireError,
};

const CONTROLLER_ID: &str = "tapio-controller/default";
const DEFAULT_PROFILE_YAML: &str = r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
"#;
const INITIAL_GENERATION: u32 = 1;

#[derive(Debug)]
pub enum ConfigLoadError {
    ReadProfile {
        path: PathBuf,
        source: std::io::Error,
    },
    ParseProfile {
        path: Option<PathBuf>,
        source: serde_yaml::Error,
    },
    ValidateProfile {
        path: Option<PathBuf>,
        source: ProfileError,
    },
    SerializeConfig {
        source: serde_json::Error,
    },
}

impl std::fmt::Display for ConfigLoadError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::ReadProfile { path, source } => {
                write!(f, "failed to read profile {}: {source}", path.display())
            }
            Self::ParseProfile { path, source } => match path {
                Some(path) => write!(f, "failed to parse profile {}: {source}", path.display()),
                None => write!(f, "failed to parse built-in profile: {source}"),
            },
            Self::ValidateProfile { path, source } => match path {
                Some(path) => write!(f, "invalid profile {}: {source}", path.display()),
                None => write!(f, "invalid built-in profile: {source}"),
            },
            Self::SerializeConfig { source } => {
                write!(
                    f,
                    "failed to serialize compiled config for hashing: {source}"
                )
            }
        }
    }
}

impl std::error::Error for ConfigLoadError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::ReadProfile { source, .. } => Some(source),
            Self::ParseProfile { source, .. } => Some(source),
            Self::ValidateProfile { source, .. } => Some(source),
            Self::SerializeConfig { source } => Some(source),
        }
    }
}

#[derive(Debug, Clone)]
pub struct ControllerState {
    inner: Arc<Mutex<InnerState>>,
}

#[derive(Debug)]
struct InnerState {
    config: ConfigResponse,
    agents: BTreeMap<String, RegisteredAgent>,
    last_heartbeats: BTreeMap<String, StoredHeartbeat>,
    accepted_events_total: u64,
    rejected_events_total: u64,
}

#[derive(Debug, Clone)]
pub struct RegisteredAgent {
    pub hello: HelloRequest,
    pub registered_at: SystemTime,
}

#[derive(Debug, Clone)]
pub struct StoredHeartbeat {
    pub heartbeat: HeartbeatRequest,
    pub seen_at: SystemTime,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EventBatchOutcome {
    pub accepted: u64,
    pub rejected: u64,
}

pub fn load_profile(path: &Path) -> Result<CompiledConfig, ConfigLoadError> {
    let content = std::fs::read_to_string(path).map_err(|source| ConfigLoadError::ReadProfile {
        path: path.to_path_buf(),
        source,
    })?;
    compile_profile_yaml(&content, Some(path.to_path_buf()))
}

pub fn production_default_config() -> Result<CompiledConfig, ConfigLoadError> {
    compile_profile_yaml(DEFAULT_PROFILE_YAML, None)
}

pub fn config_response_from_compiled(
    config: CompiledConfig,
    generation: u32,
) -> Result<ConfigResponse, ConfigLoadError> {
    let config_hash = config_hash(&config)?;
    Ok(ConfigResponse {
        wire_version: WIRE_VERSION.into(),
        version: generation.to_string(),
        config_hash,
        config,
        batching: BatchingConfig {
            send_interval_ms: 1000,
            max_batch_events: 256,
        },
    })
}

pub fn default_config_response() -> Result<ConfigResponse, ConfigLoadError> {
    config_response_from_compiled(production_default_config()?, INITIAL_GENERATION)
}

pub fn config_hash(config: &CompiledConfig) -> Result<String, ConfigLoadError> {
    let bytes =
        serde_json::to_vec(config).map_err(|source| ConfigLoadError::SerializeConfig { source })?;
    let digest = Sha256::digest(&bytes);
    let mut hash = String::from("sha256:");
    for byte in digest {
        let _ = write!(&mut hash, "{byte:02x}");
    }
    Ok(hash)
}

fn compile_profile_yaml(
    yaml: &str,
    path: Option<PathBuf>,
) -> Result<CompiledConfig, ConfigLoadError> {
    let profile: EvidenceProfile =
        serde_yaml::from_str(yaml).map_err(|source| ConfigLoadError::ParseProfile {
            path: path.clone(),
            source,
        })?;
    let validated = validate(&profile, &BuiltinSet::v0())
        .map_err(|source| ConfigLoadError::ValidateProfile { path, source })?;
    Ok(compile(&validated))
}

impl Default for ControllerState {
    fn default() -> Self {
        let config = default_config_response()
            .expect("built-in production-default EvidenceProfile must compile");
        Self::new(config)
    }
}

impl ControllerState {
    pub fn new(config: ConfigResponse) -> Self {
        Self {
            inner: Arc::new(Mutex::new(InnerState {
                config,
                agents: BTreeMap::new(),
                last_heartbeats: BTreeMap::new(),
                accepted_events_total: 0,
                rejected_events_total: 0,
            })),
        }
    }

    pub fn register_agent(&self, hello: HelloRequest) -> Result<HelloResponse, WireError> {
        hello.validate()?;
        let mut inner = self.inner.lock().expect("controller state lock poisoned");
        tracing::info!(
            agent_id = %hello.agent_id,
            node_name = %hello.node_name,
            tapio_version = %hello.tapio_version,
            "agent registered"
        );
        inner.agents.insert(
            hello.agent_id.clone(),
            RegisteredAgent {
                hello,
                registered_at: SystemTime::now(),
            },
        );
        // Derive the response from the active config so the batching limits we
        // advertise here match what /v1/events enforces.
        Ok(HelloResponse::from_config(CONTROLLER_ID, &inner.config))
    }

    pub fn config_for(&self, agent_id: &str, node_name: &str) -> Result<ConfigResponse, WireError> {
        if agent_id.trim().is_empty() {
            return Err(WireError::MissingField("agent_id"));
        }
        if node_name.trim().is_empty() {
            return Err(WireError::MissingField("node_name"));
        }
        let inner = self.inner.lock().expect("controller state lock poisoned");
        Ok(inner.config.clone())
    }

    pub fn record_heartbeat(
        &self,
        heartbeat: HeartbeatRequest,
    ) -> Result<HeartbeatResponse, WireError> {
        heartbeat.validate()?;
        let mut inner = self.inner.lock().expect("controller state lock poisoned");
        let next_config_version = inner.config.version.clone();
        tracing::debug!(
            agent_id = %heartbeat.agent_id,
            node_name = %heartbeat.node_name,
            uptime_seconds = heartbeat.uptime_seconds,
            "agent heartbeat"
        );
        inner.last_heartbeats.insert(
            heartbeat.agent_id.clone(),
            StoredHeartbeat {
                heartbeat,
                seen_at: SystemTime::now(),
            },
        );
        Ok(HeartbeatResponse::accepted(next_config_version))
    }

    pub fn record_event_batch(
        &self,
        batch: EventBatchRequest,
    ) -> Result<EventBatchResponse, WireError> {
        let max_batch_events = {
            let inner = self.inner.lock().expect("controller state lock poisoned");
            inner.config.batching.max_batch_events as usize
        };

        match batch.validate(max_batch_events) {
            Ok(()) => {
                let accepted = batch.events.len() as u64;
                let mut inner = self.inner.lock().expect("controller state lock poisoned");
                inner.accepted_events_total += accepted;
                tracing::debug!(
                    agent_id = %batch.agent_id,
                    sequence = batch.sequence,
                    accepted,
                    "event batch accepted"
                );
                Ok(EventBatchResponse {
                    wire_version: tapio_wire::WIRE_VERSION.into(),
                    accepted,
                    rejected: 0,
                    next_config_version: inner.config.version.clone(),
                })
            }
            Err(error) => {
                let rejected = batch.events.len() as u64;
                let mut inner = self.inner.lock().expect("controller state lock poisoned");
                inner.rejected_events_total += rejected;
                tracing::warn!(
                    agent_id = %batch.agent_id,
                    rejected,
                    error = %error,
                    "event batch rejected"
                );
                Err(error)
            }
        }
    }

    pub fn agent_count(&self) -> usize {
        self.inner
            .lock()
            .expect("controller state lock poisoned")
            .agents
            .len()
    }

    pub fn last_heartbeat(&self, agent_id: &str) -> Option<StoredHeartbeat> {
        self.inner
            .lock()
            .expect("controller state lock poisoned")
            .last_heartbeats
            .get(agent_id)
            .cloned()
    }

    pub fn stale_agents(&self, max_age: Duration) -> Vec<String> {
        let now = SystemTime::now();
        let inner = self.inner.lock().expect("controller state lock poisoned");
        inner
            .agents
            .keys()
            .filter(|agent_id| {
                inner
                    .last_heartbeats
                    .get(*agent_id)
                    .and_then(|heartbeat| now.duration_since(heartbeat.seen_at).ok())
                    .is_none_or(|age| age > max_age)
            })
            .cloned()
            .collect()
    }

    pub fn event_totals(&self) -> EventBatchOutcome {
        let inner = self.inner.lock().expect("controller state lock poisoned");
        EventBatchOutcome {
            accepted: inner.accepted_events_total,
            rejected: inner.rejected_events_total,
        }
    }
}

pub fn router(state: ControllerState) -> Router {
    Router::new()
        .route("/v1/agents/hello", post(hello))
        .route("/v1/agents/config", get(config))
        .route("/v1/agents/heartbeat", post(heartbeat))
        .route("/v1/events", post(events))
        .fallback(not_found)
        .with_state(state)
}

async fn hello(
    State(state): State<ControllerState>,
    Json(req): Json<HelloRequest>,
) -> Result<Json<HelloResponse>, ApiError> {
    Ok(Json(state.register_agent(req)?))
}

async fn config(
    State(state): State<ControllerState>,
    headers: HeaderMap,
    uri: Uri,
) -> Result<Response, ApiError> {
    let agent_id = query_param(&uri, "agent_id").ok_or(WireError::MissingField("agent_id"))?;
    let node_name = query_param(&uri, "node_name").ok_or(WireError::MissingField("node_name"))?;
    let config = state.config_for(&agent_id, &node_name)?;
    let etag = quoted_etag(&config.config_hash);

    if if_none_match_matches(&headers, &etag) {
        let mut response = StatusCode::NOT_MODIFIED.into_response();
        insert_etag(response.headers_mut(), &etag);
        return Ok(response);
    }

    let mut response = Json(config).into_response();
    insert_etag(response.headers_mut(), &etag);
    Ok(response)
}

async fn heartbeat(
    State(state): State<ControllerState>,
    Json(req): Json<HeartbeatRequest>,
) -> Result<Json<HeartbeatResponse>, ApiError> {
    Ok(Json(state.record_heartbeat(req)?))
}

async fn events(
    State(state): State<ControllerState>,
    Json(req): Json<EventBatchRequest>,
) -> Result<Json<EventBatchResponse>, ApiError> {
    Ok(Json(state.record_event_batch(req)?))
}

async fn not_found() -> impl IntoResponse {
    (
        StatusCode::NOT_FOUND,
        Json(ErrorEnvelope::new(
            "UNKNOWN_ENDPOINT",
            "unknown tapio-controller endpoint",
        )),
    )
}

#[derive(Debug, Serialize)]
struct ErrorEnvelope {
    error: ErrorBody,
}

#[derive(Debug, Serialize)]
struct ErrorBody {
    code: &'static str,
    message: String,
}

impl ErrorEnvelope {
    fn new(code: &'static str, message: impl Into<String>) -> Self {
        Self {
            error: ErrorBody {
                code,
                message: message.into(),
            },
        }
    }
}

#[derive(Debug)]
pub struct ApiError(WireError);

impl From<WireError> for ApiError {
    fn from(value: WireError) -> Self {
        Self(value)
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        let status = match &self.0 {
            WireError::UnsupportedVersion(_) => StatusCode::BAD_REQUEST,
            WireError::MissingField(_) => StatusCode::BAD_REQUEST,
            WireError::InvalidField { .. } => StatusCode::BAD_REQUEST,
            WireError::ReasoningField(_) => StatusCode::UNPROCESSABLE_ENTITY,
        };
        (status, Json(error_envelope(&self.0))).into_response()
    }
}

fn error_envelope(error: &WireError) -> ErrorEnvelope {
    // Error-envelope messages are HTTP API surface. Keep them stable and
    // operator-facing instead of reusing WireError Display, which is primarily
    // for logs and Rust-side diagnostics.
    match error {
        WireError::UnsupportedVersion(version) => ErrorEnvelope::new(
            "UNSUPPORTED_VERSION",
            format!("unsupported wire version {version}"),
        ),
        WireError::MissingField(field) => {
            ErrorEnvelope::new("MISSING_FIELD", format!("{field} is required"))
        }
        WireError::InvalidField { field, reason } => {
            ErrorEnvelope::new("INVALID_FIELD", format!("{field}: {reason}"))
        }
        WireError::ReasoningField(field) => ErrorEnvelope::new(
            "REASONING_FIELD",
            format!("event facts contain reasoning field {field}"),
        ),
    }
}

fn query_param(uri: &Uri, name: &str) -> Option<String> {
    uri.query()?.split('&').find_map(|pair| {
        let (key, value) = pair.split_once('=')?;
        (key == name).then(|| value.to_string())
    })
}

fn quoted_etag(config_hash: &str) -> String {
    format!("\"{config_hash}\"")
}

fn if_none_match_matches(headers: &HeaderMap, etag: &str) -> bool {
    headers
        .get(header::IF_NONE_MATCH)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| value.trim() == etag)
}

fn insert_etag(headers: &mut HeaderMap, etag: &str) {
    if let Ok(value) = HeaderValue::from_str(etag) {
        headers.insert(header::ETAG, value);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpStream;
    use std::path::Path;
    use tapio_wire::{EventSeverity, HeartbeatCounters, ObserverStatus, WIRE_VERSION, WireEvent};

    fn hello_req() -> HelloRequest {
        HelloRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            tapio_version: "4.0.0".into(),
            kernel_release: "6.8.0".into(),
            arch: "x86_64".into(),
            capabilities: vec!["network".into()],
            object_sizes: BTreeMap::new(),
            map_counts: BTreeMap::new(),
        }
    }

    fn heartbeat_req() -> HeartbeatRequest {
        HeartbeatRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            config_version: "1".into(),
            uptime_seconds: 12,
            observers: BTreeMap::from([("network".into(), ObserverStatus::Running)]),
            counters: HeartbeatCounters {
                events_total: 10,
                malformed_events_total: 0,
                lost_events_total: 0,
                correlation_drops_total: 0,
                sink_drops_total: 0,
                controller_send_failures_total: 0,
            },
            degraded_reasons: vec![],
        }
    }

    fn event_batch_req() -> EventBatchRequest {
        EventBatchRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            sequence: 1,
            sent_at_unix_nanos: 10,
            events: vec![WireEvent {
                event_type: "kernel.network.connection_refused".into(),
                timestamp_unix_nanos: 9,
                observer: "network".into(),
                severity: EventSeverity::Warning,
                facts: serde_json::json!({"dst_port": 50798, "protocol": "tcp"}),
            }],
        }
    }

    fn write_temp_profile(name: &str, content: &str) -> PathBuf {
        let mut path = std::env::temp_dir();
        path.push(format!(
            "tapio-{name}-{}-{}.yaml",
            std::process::id(),
            SystemTime::now()
                .duration_since(SystemTime::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        std::fs::write(&path, content).unwrap();
        path
    }

    fn remove_temp_profile(path: &Path) {
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn hello_registers_agent() {
        let state = ControllerState::default();
        let response = state.register_agent(hello_req()).unwrap();
        assert!(response.accepted);
        assert_eq!(state.agent_count(), 1);
    }

    #[test]
    fn default_controller_config_is_hashed_production_default() {
        let state = ControllerState::default();
        let config = state.config_for("node/worker-1", "worker-1").unwrap();
        let expected = config_hash(&production_default_config().unwrap()).unwrap();
        assert_eq!(config.version, "1");
        assert_eq!(config.config_hash, expected);
        assert!(config.config_hash.starts_with("sha256:"));
    }

    #[test]
    fn valid_profile_loads_and_compiles() {
        let path = write_temp_profile(
            "valid-profile",
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      warning_ms: 150
      critical_ms: 400
"#,
        );

        let compiled = load_profile(&path).unwrap();
        remove_temp_profile(&path);
        assert_eq!(compiled.storage.slow_io_warning_ns, 150_000_000);
        assert_eq!(compiled.storage.slow_io_critical_ns, 400_000_000);
    }

    #[test]
    fn invalid_profile_range_error_includes_field_path() {
        let path = write_temp_profile(
            "bad-range",
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      warning_ms: 0
"#,
        );

        let error = load_profile(&path).unwrap_err().to_string();
        remove_temp_profile(&path);
        assert!(error.contains("overrides.storage.slow_io.warning_ms"));
    }

    #[test]
    fn unknown_field_profile_error_includes_field_name() {
        let path = write_temp_profile(
            "unknown-field",
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
surprise: true
"#,
        );

        let error = load_profile(&path).unwrap_err().to_string();
        remove_temp_profile(&path);
        assert!(error.contains("surprise"));
    }

    #[test]
    fn unknown_base_profile_error_includes_field_path() {
        let path = write_temp_profile(
            "unknown-base",
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: staging
"#,
        );

        let error = load_profile(&path).unwrap_err().to_string();
        remove_temp_profile(&path);
        assert!(error.contains("base"));
        assert!(error.contains("staging"));
    }

    #[test]
    fn config_hash_is_stable_and_changes_with_profile_content() {
        let first = default_config_response().unwrap();
        let second = default_config_response().unwrap();
        assert_eq!(first.config_hash, second.config_hash);

        let mut changed = production_default_config().unwrap();
        changed.storage.slow_io_warning_ns = 150_000_000;
        let changed = config_response_from_compiled(changed, 1).unwrap();
        assert_ne!(first.config_hash, changed.config_hash);
    }

    #[test]
    fn hello_response_reflects_controller_config() {
        // A controller built with a non-default config must advertise that
        // config's batching limits at hello, not the wire defaults — otherwise
        // /v1/events would reject batches the agent was told were acceptable.
        let mut config = ConfigResponse::default_v1();
        config.version = "7".into();
        config.batching.send_interval_ms = 500;
        config.batching.max_batch_events = 2;
        let state = ControllerState::new(config);

        let response = state.register_agent(hello_req()).unwrap();
        assert!(response.accepted);
        assert_eq!(response.config_version, "7");
        assert_eq!(response.send_interval_ms, 500);
        assert_eq!(response.max_batch_events, 2);

        // The advertised limit is the enforced limit: a batch within it is
        // accepted, and one over it is rejected.
        assert!(state.record_event_batch(event_batch_req()).is_ok());
        let mut oversized = event_batch_req();
        let extra = oversized.events[0].clone();
        oversized.events.push(extra.clone());
        oversized.events.push(extra);
        assert_eq!(oversized.events.len() as u64, response.max_batch_events + 1);
        assert!(state.record_event_batch(oversized).is_err());
    }

    #[test]
    fn hello_rejects_unsupported_wire_version() {
        let state = ControllerState::default();
        let mut req = hello_req();
        req.wire_version = "tapio-wire/v2".into();
        assert!(state.register_agent(req).is_err());
        assert_eq!(state.agent_count(), 0);
    }

    #[test]
    fn heartbeat_updates_last_seen_state() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        let response = state.record_heartbeat(heartbeat_req()).unwrap();
        assert!(response.accepted);
        assert!(state.last_heartbeat("node/worker-1").is_some());
    }

    #[test]
    fn stale_agent_detection_reports_missing_heartbeat() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        assert_eq!(
            state.stale_agents(Duration::from_secs(30)),
            vec!["node/worker-1".to_string()]
        );
    }

    #[test]
    fn event_batch_accepts_valid_events() {
        let state = ControllerState::default();
        let response = state.record_event_batch(event_batch_req()).unwrap();
        assert_eq!(response.accepted, 1);
        assert_eq!(response.rejected, 0);
        assert_eq!(
            state.event_totals(),
            EventBatchOutcome {
                accepted: 1,
                rejected: 0,
            }
        );
    }

    #[test]
    fn event_batch_rejects_invalid_events() {
        let state = ControllerState::default();
        let mut req = event_batch_req();
        req.events[0].facts = serde_json::json!({
            "dst_port": 50798,
            "explanation": "service is probably gone"
        });
        assert!(state.record_event_batch(req).is_err());
        assert_eq!(
            state.event_totals(),
            EventBatchOutcome {
                accepted: 0,
                rejected: 1,
            }
        );
    }

    async fn http_response(request: &str) -> Option<String> {
        http_response_with_state(request, ControllerState::default()).await
    }

    async fn http_response_with_state(request: &str, state: ControllerState) -> Option<String> {
        let require_net = std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_some();
        let listener = match tokio::net::TcpListener::bind("127.0.0.1:0").await {
            Ok(listener) => listener,
            Err(e) if e.kind() == std::io::ErrorKind::PermissionDenied && !require_net => {
                eprintln!(
                    "SKIP controller HTTP response test: loopback bind not permitted \
                     in this host/sandbox ({e}); set TAPIO_LEAN_REQUIRE_NET=1 to require it"
                );
                return None;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener.local_addr().unwrap();
        let app = router(state);
        let server = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let request = request.to_string();
        let response = tokio::task::spawn_blocking(move || {
            let mut stream = TcpStream::connect(addr).unwrap();
            stream.write_all(request.as_bytes()).unwrap();
            let mut response = String::new();
            stream.read_to_string(&mut response).unwrap();
            response
        })
        .await
        .unwrap();

        server.abort();
        Some(response)
    }

    fn response_json(response: &str) -> serde_json::Value {
        let (_, body) = response.split_once("\r\n\r\n").unwrap();
        serde_json::from_str(body).unwrap()
    }

    fn response_body(response: &str) -> &str {
        response.split_once("\r\n\r\n").map_or("", |(_, body)| body)
    }

    #[tokio::test]
    async fn unknown_endpoint_returns_error_envelope() {
        let Some(response) =
            http_response("GET /v1/nope HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
                .await
        else {
            return;
        };
        assert!(response.starts_with("HTTP/1.1 404"));
        let body = response_json(&response);
        assert_eq!(body["error"]["code"], "UNKNOWN_ENDPOINT");
        assert_eq!(
            body["error"]["message"],
            "unknown tapio-controller endpoint"
        );
    }

    #[tokio::test]
    async fn unsupported_wire_version_returns_error_envelope() {
        let body = serde_json::json!({
            "wire_version": "tapio-wire/v2",
            "agent_id": "node/worker-1",
            "node_name": "worker-1",
            "tapio_version": "4.0.0",
            "kernel_release": "6.8.0",
            "arch": "x86_64"
        })
        .to_string();
        let request = format!(
            "POST /v1/agents/hello HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
            body.len(),
            body
        );
        let Some(response) = http_response(&request).await else {
            return;
        };
        assert!(response.starts_with("HTTP/1.1 400"));
        let body = response_json(&response);
        assert_eq!(body["error"]["code"], "UNSUPPORTED_VERSION");
        assert_eq!(
            body["error"]["message"],
            "unsupported wire version tapio-wire/v2"
        );
    }

    #[tokio::test]
    async fn missing_field_returns_error_envelope() {
        let Some(response) = http_response(
            "GET /v1/agents/config?node_name=worker-1 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
        )
        .await
        else {
            return;
        };
        assert!(response.starts_with("HTTP/1.1 400"));
        let body = response_json(&response);
        assert_eq!(body["error"]["code"], "MISSING_FIELD");
        assert_eq!(body["error"]["message"], "agent_id is required");
    }

    #[tokio::test]
    async fn config_endpoint_returns_etag_and_hash() {
        let state = ControllerState::default();
        let expected = state.config_for("node/worker-1", "worker-1").unwrap();
        let Some(response) = http_response_with_state(
            "GET /v1/agents/config?agent_id=node/worker-1&node_name=worker-1 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
            state,
        )
        .await
        else {
            return;
        };

        assert!(response.starts_with("HTTP/1.1 200"));
        assert!(response.contains(&format!("etag: \"{}\"", expected.config_hash)));
        let body = response_json(&response);
        assert_eq!(body["config_hash"], expected.config_hash);
    }

    #[tokio::test]
    async fn config_endpoint_returns_304_for_matching_if_none_match() {
        let state = ControllerState::default();
        let expected = state.config_for("node/worker-1", "worker-1").unwrap();
        let request = format!(
            "GET /v1/agents/config?agent_id=node/worker-1&node_name=worker-1 HTTP/1.1\r\nHost: localhost\r\nIf-None-Match: \"{}\"\r\nConnection: close\r\n\r\n",
            expected.config_hash
        );
        let Some(response) = http_response_with_state(&request, state).await else {
            return;
        };

        assert!(response.starts_with("HTTP/1.1 304"));
        assert!(response.contains(&format!("etag: \"{}\"", expected.config_hash)));
        assert_eq!(response_body(&response), "");
    }

    #[tokio::test]
    async fn config_endpoint_returns_200_for_stale_if_none_match() {
        let state = ControllerState::default();
        let expected = state.config_for("node/worker-1", "worker-1").unwrap();
        let Some(response) = http_response_with_state(
            "GET /v1/agents/config?agent_id=node/worker-1&node_name=worker-1 HTTP/1.1\r\nHost: localhost\r\nIf-None-Match: \"sha256:stale\"\r\nConnection: close\r\n\r\n",
            state,
        )
        .await
        else {
            return;
        };

        assert!(response.starts_with("HTTP/1.1 200"));
        let body = response_json(&response);
        assert_eq!(body["config_hash"], expected.config_hash);
    }
}
