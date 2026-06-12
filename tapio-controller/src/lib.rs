use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use axum::extract::DefaultBodyLimit;
use axum::extract::State;
use axum::extract::rejection::JsonRejection;
use axum::http::{HeaderMap, HeaderValue, StatusCode, Uri, header};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Serialize;
use sha2::{Digest, Sha256};
use tapio_profile::{BuiltinSet, EvidenceProfile, ProfileError, compile, validate};
use tapio_wire::{
    AgentStatus, BatchingConfig, CompiledConfig, ConfigResponse, EventBatchRequest,
    EventBatchResponse, HeartbeatCounters, HeartbeatRequest, HeartbeatResponse, HelloRequest,
    HelloResponse, SequenceStatus, StatusConfig, StatusResponse, StatusTotals, WIRE_VERSION,
    WireError,
};

const CONTROLLER_ID: &str = "tapio-controller/default";
const MAX_REQUEST_BODY_BYTES: usize = 262_144;
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
    started_at: SystemTime,
    agents: BTreeMap<String, RegisteredAgent>,
    last_heartbeats: BTreeMap<String, StoredHeartbeat>,
    sequences: BTreeMap<String, SequenceTracking>,
    accepted_events_total: u64,
    rejected_events_total: u64,
    batches_accepted_total: u64,
    batches_rejected_total: u64,
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

#[derive(Debug, Clone, Default, PartialEq, Eq)]
struct SequenceTracking {
    last_seen: Option<u64>,
    gaps_total: u64,
    regressions_total: u64,
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
                started_at: SystemTime::now(),
                agents: BTreeMap::new(),
                last_heartbeats: BTreeMap::new(),
                sequences: BTreeMap::new(),
                accepted_events_total: 0,
                rejected_events_total: 0,
                batches_accepted_total: 0,
                batches_rejected_total: 0,
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
        let agent_id = hello.agent_id.clone();
        inner.agents.insert(
            agent_id.clone(),
            RegisteredAgent {
                hello,
                registered_at: SystemTime::now(),
            },
        );
        inner
            .sequences
            .insert(agent_id, SequenceTracking::default());
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
                inner.batches_accepted_total += 1;
                update_sequence_tracking(&mut inner, &batch.agent_id, batch.sequence);
                for event in &batch.events {
                    match serde_json::to_string(event) {
                        Ok(event_json) => {
                            tracing::trace!(
                                agent_id = %batch.agent_id,
                                sequence = batch.sequence,
                                event = %event_json,
                                "event accepted"
                            );
                        }
                        Err(error) => {
                            tracing::trace!(
                                agent_id = %batch.agent_id,
                                sequence = batch.sequence,
                                error = %error,
                                "event trace serialization failed"
                            );
                        }
                    }
                }
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
                inner.batches_rejected_total += 1;
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

    pub fn unconverged_agents(&self, target_hash: &str) -> Vec<String> {
        let inner = self.inner.lock().expect("controller state lock poisoned");
        inner
            .agents
            .keys()
            .filter(|agent_id| {
                inner
                    .last_heartbeats
                    .get(*agent_id)
                    .is_none_or(|stored| stored.heartbeat.config_hash != target_hash)
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

    pub fn status(&self) -> StatusResponse {
        let now = SystemTime::now();
        let inner = self.inner.lock().expect("controller state lock poisoned");
        let agents = inner
            .agents
            .iter()
            .map(|(agent_id, registered)| {
                let heartbeat = inner.last_heartbeats.get(agent_id);
                let sequence = inner.sequences.get(agent_id).cloned().unwrap_or_default();
                AgentStatus {
                    agent_id: registered.hello.agent_id.clone(),
                    node_name: registered.hello.node_name.clone(),
                    tapio_version: registered.hello.tapio_version.clone(),
                    registered_at_unix: unix_seconds(registered.registered_at),
                    last_heartbeat_age_seconds: heartbeat.and_then(|stored| {
                        now.duration_since(stored.seen_at)
                            .ok()
                            .map(|age| age.as_secs())
                    }),
                    reported_config_version: heartbeat
                        .map(|stored| stored.heartbeat.config_version.clone())
                        .unwrap_or_default(),
                    reported_counters: heartbeat
                        .map(|stored| stored.heartbeat.counters.clone())
                        .unwrap_or_else(zero_heartbeat_counters),
                    observers: heartbeat
                        .map(|stored| stored.heartbeat.observers.clone())
                        .unwrap_or_default(),
                    sequence: SequenceStatus {
                        last_seen: sequence.last_seen,
                        gaps_total: sequence.gaps_total,
                        regressions_total: sequence.regressions_total,
                    },
                }
            })
            .collect();

        StatusResponse {
            wire_version: WIRE_VERSION.into(),
            controller_id: CONTROLLER_ID.into(),
            started_at_unix: unix_seconds(inner.started_at),
            config: StatusConfig {
                version: inner.config.version.clone(),
                config_hash: inner.config.config_hash.clone(),
            },
            totals: StatusTotals {
                accepted_events_total: inner.accepted_events_total,
                rejected_events_total: inner.rejected_events_total,
                batches_accepted_total: inner.batches_accepted_total,
                batches_rejected_total: inner.batches_rejected_total,
            },
            agents,
        }
    }
}

fn update_sequence_tracking(inner: &mut InnerState, agent_id: &str, sequence: u64) {
    let tracking = inner.sequences.entry(agent_id.to_string()).or_default();
    match tracking.last_seen {
        None => tracking.last_seen = Some(sequence),
        Some(last_seen) if sequence == last_seen.saturating_add(1) => {
            tracking.last_seen = Some(sequence);
        }
        Some(last_seen) if sequence > last_seen.saturating_add(1) => {
            tracking.gaps_total = tracking
                .gaps_total
                .saturating_add(sequence.saturating_sub(last_seen.saturating_add(1)));
            tracking.last_seen = Some(sequence);
        }
        Some(_) => {
            tracking.regressions_total = tracking.regressions_total.saturating_add(1);
        }
    }
}

fn zero_heartbeat_counters() -> HeartbeatCounters {
    HeartbeatCounters {
        events_total: 0,
        malformed_events_total: 0,
        lost_events_total: 0,
        correlation_drops_total: 0,
        sink_drops_total: 0,
        controller_send_failures_total: 0,
    }
}

fn unix_seconds(time: SystemTime) -> u64 {
    time.duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0)
}

pub fn router(state: ControllerState) -> Router {
    Router::new()
        .route("/v1/agents/hello", post(hello))
        .route("/v1/agents/config", get(config))
        .route("/v1/agents/heartbeat", post(heartbeat))
        .route("/v1/events", post(events))
        .route("/v1/status", get(status))
        .fallback(not_found)
        .layer(DefaultBodyLimit::max(MAX_REQUEST_BODY_BYTES))
        .with_state(state)
}

async fn hello(
    State(state): State<ControllerState>,
    req: Result<Json<HelloRequest>, JsonRejection>,
) -> Result<Json<HelloResponse>, ApiError> {
    let req = json_payload(req)?;
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
    req: Result<Json<HeartbeatRequest>, JsonRejection>,
) -> Result<Json<HeartbeatResponse>, ApiError> {
    let req = json_payload(req)?;
    Ok(Json(state.record_heartbeat(req)?))
}

async fn events(
    State(state): State<ControllerState>,
    req: Result<Json<EventBatchRequest>, JsonRejection>,
) -> Result<Json<EventBatchResponse>, ApiError> {
    let req = json_payload(req)?;
    Ok(Json(state.record_event_batch(req)?))
}

async fn status(State(state): State<ControllerState>) -> Json<StatusResponse> {
    Json(state.status())
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

fn json_payload<T>(payload: Result<Json<T>, JsonRejection>) -> Result<T, ApiError> {
    payload.map(|Json(value)| value).map_err(|error| {
        if error.status() == StatusCode::PAYLOAD_TOO_LARGE {
            ApiError::PayloadTooLarge(error.to_string())
        } else {
            ApiError::MalformedJson(error.to_string())
        }
    })
}

#[derive(Debug)]
pub enum ApiError {
    Wire(WireError),
    MalformedJson(String),
    PayloadTooLarge(String),
}

impl From<WireError> for ApiError {
    fn from(value: WireError) -> Self {
        Self::Wire(value)
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        match self {
            Self::Wire(error) => {
                let status = match &error {
                    WireError::UnsupportedVersion(_) => StatusCode::BAD_REQUEST,
                    WireError::MissingField(_) => StatusCode::BAD_REQUEST,
                    WireError::InvalidField { .. } => StatusCode::BAD_REQUEST,
                    WireError::ReasoningField(_) => StatusCode::UNPROCESSABLE_ENTITY,
                };
                (status, Json(error_envelope(&error))).into_response()
            }
            Self::MalformedJson(message) => (
                StatusCode::BAD_REQUEST,
                Json(ErrorEnvelope::new("MALFORMED_JSON", message)),
            )
                .into_response(),
            Self::PayloadTooLarge(message) => (
                StatusCode::PAYLOAD_TOO_LARGE,
                Json(ErrorEnvelope::new("PAYLOAD_TOO_LARGE", message)),
            )
                .into_response(),
        }
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
    use axum::body::{Body, to_bytes};
    use axum::http::{Method, Request};
    use std::io::{Read, Write};
    use std::net::TcpStream;
    use std::path::Path;
    use tapio_wire::{EventSeverity, HeartbeatCounters, ObserverStatus, WIRE_VERSION, WireEvent};
    use tower::ServiceExt;

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
            config_hash: "sha256:abc".into(),
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

    fn event_batch_with_sequence(sequence: u64) -> EventBatchRequest {
        EventBatchRequest {
            sequence,
            ..event_batch_req()
        }
    }

    fn status_agent(status: &StatusResponse, agent_id: &str) -> AgentStatus {
        status
            .agents
            .iter()
            .find(|agent| agent.agent_id == agent_id)
            .cloned()
            .unwrap()
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

    async fn router_json_response(
        state: ControllerState,
        method: Method,
        uri: &str,
        body: impl Into<Body>,
    ) -> (StatusCode, serde_json::Value) {
        let response = router(state)
            .oneshot(
                Request::builder()
                    .method(method)
                    .uri(uri)
                    .header(header::CONTENT_TYPE, "application/json")
                    .body(body.into())
                    .unwrap(),
            )
            .await
            .unwrap();
        let status = response.status();
        let body = to_bytes(response.into_body(), MAX_REQUEST_BODY_BYTES * 2)
            .await
            .unwrap();
        let json = serde_json::from_slice(&body).unwrap_or_else(|error| {
            panic!(
                "response body was not JSON: {error}; body={}",
                String::from_utf8_lossy(&body)
            )
        });
        (status, json)
    }

    async fn router_status_response(
        state: ControllerState,
        method: Method,
        uri: &str,
        body: impl Into<Body>,
    ) -> StatusCode {
        router(state)
            .oneshot(
                Request::builder()
                    .method(method)
                    .uri(uri)
                    .header(header::CONTENT_TYPE, "application/json")
                    .body(body.into())
                    .unwrap(),
            )
            .await
            .unwrap()
            .status()
    }

    fn json_body<T: Serialize>(value: &T) -> String {
        serde_json::to_string(value).unwrap()
    }

    fn event_batch_body_with_len(target_len: usize) -> String {
        let mut batch = event_batch_req();
        batch.events[0].facts = serde_json::json!({
            "dst_port": 50798,
            "protocol": "tcp",
            "padding": "",
        });
        let empty_padding_len = json_body(&batch).len();
        assert!(
            empty_padding_len <= target_len,
            "event batch without padding exceeded target length"
        );
        batch.events[0].facts = serde_json::json!({
            "dst_port": 50798,
            "protocol": "tcp",
            "padding": "x".repeat(target_len - empty_padding_len),
        });
        let body = json_body(&batch);
        assert_eq!(body.len(), target_len);
        body
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
    fn unconverged_agents_reports_missing_and_mismatched_hashes() {
        let state = ControllerState::default();

        let mut worker1 = hello_req();
        worker1.agent_id = "node/worker-1".into();
        worker1.node_name = "worker-1".into();
        state.register_agent(worker1).unwrap();

        let mut worker2 = hello_req();
        worker2.agent_id = "node/worker-2".into();
        worker2.node_name = "worker-2".into();
        state.register_agent(worker2).unwrap();

        let mut worker3 = hello_req();
        worker3.agent_id = "node/worker-3".into();
        worker3.node_name = "worker-3".into();
        state.register_agent(worker3).unwrap();

        let mut converged = heartbeat_req();
        converged.agent_id = "node/worker-1".into();
        converged.node_name = "worker-1".into();
        converged.config_hash = "sha256:target".into();
        state.record_heartbeat(converged).unwrap();

        let mut mismatched = heartbeat_req();
        mismatched.agent_id = "node/worker-2".into();
        mismatched.node_name = "worker-2".into();
        mismatched.config_hash = "sha256:old".into();
        state.record_heartbeat(mismatched).unwrap();

        assert_eq!(
            state.unconverged_agents("sha256:target"),
            vec!["node/worker-2".to_string(), "node/worker-3".to_string()]
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

    #[test]
    fn status_reflects_hello_heartbeat_and_batch() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state.record_heartbeat(heartbeat_req()).unwrap();
        state.record_event_batch(event_batch_req()).unwrap();

        let status = state.status();
        status.validate().unwrap();
        assert_eq!(status.controller_id, CONTROLLER_ID);
        assert_eq!(status.config.version, "1");
        assert_eq!(status.totals.accepted_events_total, 1);
        assert_eq!(status.totals.rejected_events_total, 0);
        assert_eq!(status.totals.batches_accepted_total, 1);
        assert_eq!(status.totals.batches_rejected_total, 0);

        let agent = status_agent(&status, "node/worker-1");
        assert_eq!(agent.node_name, "worker-1");
        assert_eq!(agent.tapio_version, "4.0.0");
        assert_eq!(agent.reported_config_version, "1");
        assert_eq!(agent.reported_counters.events_total, 10);
        assert_eq!(agent.observers["network"], ObserverStatus::Running);
        assert_eq!(agent.sequence.last_seen, Some(1));
        assert_eq!(agent.sequence.gaps_total, 0);
        assert_eq!(agent.sequence.regressions_total, 0);
        assert!(agent.last_heartbeat_age_seconds.is_some());
    }

    #[test]
    fn status_reports_none_age_for_agent_without_heartbeat() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();

        let status = state.status();
        let agent = status_agent(&status, "node/worker-1");
        assert_eq!(agent.last_heartbeat_age_seconds, None);
        assert_eq!(agent.reported_config_version, "");
        assert_eq!(agent.reported_counters.events_total, 0);
        assert!(agent.observers.is_empty());
    }

    #[test]
    fn sequence_tracking_counts_gaps_and_regressions() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();

        for sequence in [1, 2, 3] {
            state
                .record_event_batch(event_batch_with_sequence(sequence))
                .unwrap();
        }
        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(3));
        assert_eq!(agent.sequence.gaps_total, 0);
        assert_eq!(agent.sequence.regressions_total, 0);

        state
            .record_event_batch(event_batch_with_sequence(5))
            .unwrap();
        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(5));
        assert_eq!(agent.sequence.gaps_total, 1);
        assert_eq!(agent.sequence.regressions_total, 0);

        state
            .record_event_batch(event_batch_with_sequence(4))
            .unwrap();
        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(5));
        assert_eq!(agent.sequence.gaps_total, 1);
        assert_eq!(agent.sequence.regressions_total, 1);
    }

    #[test]
    fn sequence_gap_counts_missing_batches() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(2))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(5))
            .unwrap();

        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(5));
        assert_eq!(agent.sequence.gaps_total, 2);
    }

    #[test]
    fn hello_resets_sequence_tracking_and_first_batch_sets_baseline() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(5))
            .unwrap();
        assert_eq!(
            status_agent(&state.status(), "node/worker-1")
                .sequence
                .gaps_total,
            3
        );

        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(1));
        assert_eq!(agent.sequence.gaps_total, 0);
        assert_eq!(agent.sequence.regressions_total, 0);

        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(47))
            .unwrap();
        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(47));
        assert_eq!(agent.sequence.gaps_total, 0);
        assert_eq!(agent.sequence.regressions_total, 0);
    }

    #[test]
    fn status_counts_accepted_and_rejected_batches() {
        let state = ControllerState::default();
        state.record_event_batch(event_batch_req()).unwrap();

        let mut invalid = event_batch_req();
        invalid.events[0].facts = serde_json::json!({"possible_causes":["guess"]});
        assert!(state.record_event_batch(invalid).is_err());

        let status = state.status();
        assert_eq!(status.totals.accepted_events_total, 1);
        assert_eq!(status.totals.rejected_events_total, 1);
        assert_eq!(status.totals.batches_accepted_total, 1);
        assert_eq!(status.totals.batches_rejected_total, 1);
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

    #[tokio::test]
    async fn status_endpoint_returns_state_snapshot() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state.record_heartbeat(heartbeat_req()).unwrap();
        state.record_event_batch(event_batch_req()).unwrap();
        let Some(response) = http_response_with_state(
            "GET /v1/status HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
            state,
        )
        .await
        else {
            return;
        };

        assert!(response.starts_with("HTTP/1.1 200"));
        let body = response_json(&response);
        assert_eq!(body["wire_version"], WIRE_VERSION);
        assert_eq!(body["totals"]["accepted_events_total"], 1);
        assert_eq!(body["agents"][0]["agent_id"], "node/worker-1");
    }

    #[tokio::test]
    async fn event_body_over_explicit_limit_returns_413() {
        let body = "{".to_string() + &" ".repeat(MAX_REQUEST_BODY_BYTES + 1);
        let request = format!(
            "POST /v1/events HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
            body.len(),
            body
        );
        let Some(response) = http_response(&request).await else {
            return;
        };
        assert!(response.starts_with("HTTP/1.1 413"));
    }

    #[tokio::test]
    async fn hostile_01_wrong_wire_version_on_json_requests_returns_unsupported_version() {
        let mut hello = hello_req();
        hello.wire_version = "tapio-wire/v2".into();
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/agents/hello",
            json_body(&hello),
        )
        .await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body["error"]["code"], "UNSUPPORTED_VERSION");

        let mut heartbeat = heartbeat_req();
        heartbeat.wire_version = "tapio-wire/v2".into();
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/agents/heartbeat",
            json_body(&heartbeat),
        )
        .await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body["error"]["code"], "UNSUPPORTED_VERSION");

        let mut batch = event_batch_req();
        batch.wire_version = "tapio-wire/v2".into();
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/events",
            json_body(&batch),
        )
        .await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body["error"]["code"], "UNSUPPORTED_VERSION");
    }

    #[tokio::test]
    async fn hostile_02_event_batch_over_max_rejects_and_does_not_advance_sequence() {
        let mut config = ConfigResponse::default_v1();
        config.batching.max_batch_events = 1;
        let state = ControllerState::new(config);
        state.register_agent(hello_req()).unwrap();

        let mut batch = event_batch_req();
        batch.events.push(batch.events[0].clone());
        let (status, body) =
            router_json_response(state.clone(), Method::POST, "/v1/events", json_body(&batch))
                .await;

        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body["error"]["code"], "INVALID_FIELD");
        let status = state.status();
        let agent = status_agent(&status, "node/worker-1");
        assert_eq!(agent.sequence.last_seen, None);
        assert_eq!(status.totals.accepted_events_total, 0);
        assert_eq!(status.totals.rejected_events_total, 2);
        assert_eq!(status.totals.batches_accepted_total, 0);
        assert_eq!(status.totals.batches_rejected_total, 1);
    }

    #[test]
    fn hostile_03_sequence_zero_after_baseline_counts_regression() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(0))
            .unwrap();

        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(1));
        assert_eq!(agent.sequence.regressions_total, 1);
    }

    #[test]
    fn hostile_04_duplicate_sequence_counts_one_regression_per_duplicate() {
        let state = ControllerState::default();
        state.register_agent(hello_req()).unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();
        state
            .record_event_batch(event_batch_with_sequence(1))
            .unwrap();

        let agent = status_agent(&state.status(), "node/worker-1");
        assert_eq!(agent.sequence.last_seen, Some(1));
        assert_eq!(agent.sequence.regressions_total, 2);
    }

    #[tokio::test]
    async fn hostile_05_empty_agent_id_or_node_name_returns_400_and_preserves_registry() {
        for (field, mut hello) in [
            (
                "agent_id",
                HelloRequest {
                    agent_id: String::new(),
                    ..hello_req()
                },
            ),
            (
                "node_name",
                HelloRequest {
                    node_name: String::new(),
                    ..hello_req()
                },
            ),
        ] {
            let state = ControllerState::default();
            let (status, body) = router_json_response(
                state.clone(),
                Method::POST,
                "/v1/agents/hello",
                json_body(&hello),
            )
            .await;
            assert_eq!(status, StatusCode::BAD_REQUEST);
            assert_eq!(body["error"]["code"], "MISSING_FIELD");
            assert_eq!(body["error"]["message"], format!("{field} is required"));
            assert_eq!(state.agent_count(), 0);

            hello.agent_id = "node/worker-1".into();
            hello.node_name = "worker-1".into();
        }

        for (field, heartbeat) in [
            (
                "agent_id",
                HeartbeatRequest {
                    agent_id: String::new(),
                    ..heartbeat_req()
                },
            ),
            (
                "node_name",
                HeartbeatRequest {
                    node_name: String::new(),
                    ..heartbeat_req()
                },
            ),
        ] {
            let state = ControllerState::default();
            let (status, body) = router_json_response(
                state.clone(),
                Method::POST,
                "/v1/agents/heartbeat",
                json_body(&heartbeat),
            )
            .await;
            assert_eq!(status, StatusCode::BAD_REQUEST);
            assert_eq!(body["error"]["code"], "MISSING_FIELD");
            assert_eq!(body["error"]["message"], format!("{field} is required"));
            assert_eq!(state.agent_count(), 0);
            assert!(state.last_heartbeat("").is_none());
        }
    }

    #[tokio::test]
    async fn hostile_06_body_exactly_at_limit_is_accepted_and_one_byte_over_is_413() {
        let exact_body = event_batch_body_with_len(MAX_REQUEST_BODY_BYTES);
        let state = ControllerState::default();
        let (status, body) = router_json_response(
            state.clone(),
            Method::POST,
            "/v1/events",
            exact_body.clone(),
        )
        .await;
        assert_eq!(exact_body.len(), MAX_REQUEST_BODY_BYTES);
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body["accepted"], 1);
        assert_eq!(state.status().totals.accepted_events_total, 1);

        let over_body = exact_body + " ";
        let status = router_status_response(
            ControllerState::default(),
            Method::POST,
            "/v1/events",
            over_body,
        )
        .await;
        assert_eq!(status, StatusCode::PAYLOAD_TOO_LARGE);
    }

    #[tokio::test]
    async fn hostile_07_unknown_json_fields_are_ignored_on_requests() {
        let hello = serde_json::json!({
            "wire_version": WIRE_VERSION,
            "agent_id": "node/worker-1",
            "node_name": "worker-1",
            "tapio_version": "4.0.0",
            "kernel_release": "6.8.0",
            "arch": "x86_64",
            "future_field": true
        });
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/agents/hello",
            hello.to_string(),
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body["accepted"], true);

        let heartbeat = serde_json::json!({
            "wire_version": WIRE_VERSION,
            "agent_id": "node/worker-1",
            "node_name": "worker-1",
            "config_version": "1",
            "config_hash": "sha256:abc",
            "uptime_seconds": 12,
            "observers": {"network": "running"},
            "counters": {
                "events_total": 10,
                "malformed_events_total": 0,
                "lost_events_total": 0,
                "correlation_drops_total": 0,
                "sink_drops_total": 0,
                "controller_send_failures_total": 0
            },
            "future_field": true
        });
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/agents/heartbeat",
            heartbeat.to_string(),
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body["accepted"], true);

        let batch = serde_json::json!({
            "wire_version": WIRE_VERSION,
            "agent_id": "node/worker-1",
            "node_name": "worker-1",
            "sequence": 1,
            "sent_at_unix_nanos": 10,
            "events": [{
                "type": "kernel.network.connection_refused",
                "timestamp_unix_nanos": 9,
                "observer": "network",
                "severity": "warning",
                "facts": {"dst_port": 50798, "protocol": "tcp"},
                "future_event_field": true
            }],
            "future_field": true
        });
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/events",
            batch.to_string(),
        )
        .await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(body["accepted"], 1);
    }

    #[tokio::test]
    async fn hostile_08_malformed_json_body_returns_400_error_envelope() {
        let (status, body) = router_json_response(
            ControllerState::default(),
            Method::POST,
            "/v1/agents/hello",
            r#"{"wire_version":"tapio-wire/v1","#,
        )
        .await;

        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body["error"]["code"], "MALFORMED_JSON");
    }

    #[test]
    fn hostile_09_heartbeat_for_unknown_agent_is_accepted_and_stored_but_not_registered() {
        let state = ControllerState::default();
        let response = state.record_heartbeat(heartbeat_req()).unwrap();

        assert!(response.accepted);
        assert!(state.last_heartbeat("node/worker-1").is_some());
        assert_eq!(state.agent_count(), 0);
        assert!(state.status().agents.is_empty());
    }
}
