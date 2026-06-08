use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime};

use axum::extract::State;
use axum::http::{StatusCode, Uri};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use tapio_wire::{
    ConfigResponse, EventBatchRequest, EventBatchResponse, HeartbeatRequest, HeartbeatResponse,
    HelloRequest, HelloResponse, WireError,
};

const CONTROLLER_ID: &str = "tapio-controller/default";

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

impl Default for ControllerState {
    fn default() -> Self {
        Self::new(ConfigResponse::default_v1())
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
        let config_version = inner.config.version.clone();
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
        Ok(HelloResponse::accepted(CONTROLLER_ID, config_version))
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
    uri: Uri,
) -> Result<Json<ConfigResponse>, ApiError> {
    let agent_id = query_param(&uri, "agent_id").ok_or(WireError::MissingField("agent_id"))?;
    let node_name = query_param(&uri, "node_name").ok_or(WireError::MissingField("node_name"))?;
    Ok(Json(state.config_for(&agent_id, &node_name)?))
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
        Json(serde_json::json!({
            "error": "unknown tapio-controller endpoint"
        })),
    )
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
        let status = match self.0 {
            WireError::UnsupportedVersion(_) => StatusCode::BAD_REQUEST,
            WireError::MissingField(_) => StatusCode::BAD_REQUEST,
            WireError::InvalidField { .. } => StatusCode::BAD_REQUEST,
            WireError::ReasoningField(_) => StatusCode::UNPROCESSABLE_ENTITY,
        };
        (
            status,
            Json(serde_json::json!({
                "error": self.0.to_string()
            })),
        )
            .into_response()
    }
}

fn query_param(uri: &Uri, name: &str) -> Option<String> {
    uri.query()?.split('&').find_map(|pair| {
        let (key, value) = pair.split_once('=')?;
        (key == name).then(|| value.to_string())
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpStream;
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

    #[test]
    fn hello_registers_agent() {
        let state = ControllerState::default();
        let response = state.register_agent(hello_req()).unwrap();
        assert!(response.accepted);
        assert_eq!(state.agent_count(), 1);
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

    #[tokio::test]
    async fn unknown_endpoint_returns_clear_error() {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let app = router(ControllerState::default());
        let server = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let response = tokio::task::spawn_blocking(move || {
            let mut stream = TcpStream::connect(addr).unwrap();
            stream
                .write_all(b"GET /v1/nope HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
                .unwrap();
            let mut response = String::new();
            stream.read_to_string(&mut response).unwrap();
            response
        })
        .await
        .unwrap();

        server.abort();
        assert!(response.starts_with("HTTP/1.1 404"));
        assert!(response.contains("unknown tapio-controller endpoint"));
    }
}
