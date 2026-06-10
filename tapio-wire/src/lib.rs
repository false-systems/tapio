use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

pub mod v0;
pub use v0::{
    CompiledConfig, CompiledContainer, CompiledNetwork, CompiledNodePmc, CompiledStorage,
};

pub const WIRE_VERSION: &str = "tapio-wire/v1";

#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum WireError {
    #[error("unsupported wire version: {0}")]
    UnsupportedVersion(String),
    #[error("missing required field: {0}")]
    MissingField(&'static str),
    #[error("invalid field {field}: {reason}")]
    InvalidField {
        field: &'static str,
        reason: &'static str,
    },
    #[error("event facts contain reasoning field: {0}")]
    ReasoningField(String),
}

pub type WireResult<T> = Result<T, WireError>;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct HelloRequest {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub tapio_version: String,
    pub kernel_release: String,
    pub arch: String,
    #[serde(default)]
    pub capabilities: Vec<String>,
    #[serde(default)]
    pub object_sizes: BTreeMap<String, u64>,
    #[serde(default)]
    pub map_counts: BTreeMap<String, u64>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct HelloResponse {
    pub wire_version: String,
    pub accepted: bool,
    pub controller_id: String,
    pub config_version: String,
    pub send_interval_ms: u64,
    pub max_batch_events: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConfigResponse {
    pub wire_version: String,
    pub version: String,
    pub config: CompiledConfig,
    pub batching: BatchingConfig,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BatchingConfig {
    pub send_interval_ms: u64,
    pub max_batch_events: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct HeartbeatRequest {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub config_version: String,
    pub uptime_seconds: u64,
    pub observers: BTreeMap<String, ObserverStatus>,
    pub counters: HeartbeatCounters,
    #[serde(default)]
    pub degraded_reasons: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ObserverStatus {
    Running,
    Disabled,
    Degraded,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct HeartbeatCounters {
    pub events_total: u64,
    pub malformed_events_total: u64,
    pub lost_events_total: u64,
    pub correlation_drops_total: u64,
    pub sink_drops_total: u64,
    pub controller_send_failures_total: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct HeartbeatResponse {
    pub wire_version: String,
    pub accepted: bool,
    pub next_config_version: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct EventBatchRequest {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub sequence: u64,
    pub sent_at_unix_nanos: u64,
    pub events: Vec<WireEvent>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct WireEvent {
    #[serde(rename = "type")]
    pub event_type: String,
    pub timestamp_unix_nanos: u64,
    pub observer: String,
    pub severity: EventSeverity,
    pub facts: serde_json::Value,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum EventSeverity {
    Debug,
    Info,
    Warning,
    Error,
    Critical,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct EventBatchResponse {
    pub wire_version: String,
    pub accepted: u64,
    pub rejected: u64,
    pub next_config_version: String,
}

impl HelloRequest {
    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("agent_id", &self.agent_id)?;
        required("node_name", &self.node_name)?;
        required("tapio_version", &self.tapio_version)?;
        required("kernel_release", &self.kernel_release)?;
        required("arch", &self.arch)?;
        Ok(())
    }
}

impl HelloResponse {
    /// Build the accepted hello response from the controller's active config.
    ///
    /// The batching limits and config version are taken from `config` so the
    /// agent is told exactly the limits that `/v1/events` later enforces.
    /// Hard-coding them here would let an agent be promised one `max_batch_events`
    /// at hello and have same-sized batches rejected afterwards.
    pub fn from_config(controller_id: impl Into<String>, config: &ConfigResponse) -> Self {
        Self {
            wire_version: WIRE_VERSION.into(),
            accepted: true,
            controller_id: controller_id.into(),
            config_version: config.version.clone(),
            send_interval_ms: config.batching.send_interval_ms,
            max_batch_events: config.batching.max_batch_events,
        }
    }

    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("controller_id", &self.controller_id)?;
        required("config_version", &self.config_version)?;
        bounded_nonzero("send_interval_ms", self.send_interval_ms)?;
        bounded_nonzero("max_batch_events", self.max_batch_events)?;
        Ok(())
    }
}

impl ConfigResponse {
    pub fn default_v1() -> Self {
        Self {
            wire_version: WIRE_VERSION.into(),
            version: "1".into(),
            config: CompiledConfig {
                network: CompiledNetwork {
                    enabled: true,
                    rtt_spike_ratio: 2,
                    rtt_spike_abs_us: 500_000,
                    rtt_min_baseline_samples: 5,
                    conn_refused_window_ns: 0,
                    conn_refused_min_count: 0,
                },
                storage: CompiledStorage {
                    enabled: true,
                    slow_io_warning_ns: 50_000_000,
                    slow_io_critical_ns: 200_000_000,
                },
                container: CompiledContainer {
                    enabled: true,
                    ignore_exit_codes: vec![],
                },
                node_pmc: CompiledNodePmc {
                    enabled: true,
                    stall_warning_permille: 200,
                    stall_critical_permille: 400,
                    ipc_degradation_milli: 1000,
                },
            },
            batching: BatchingConfig {
                send_interval_ms: 1000,
                max_batch_events: 256,
            },
        }
    }

    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("version", &self.version)?;
        bounded_nonzero(
            "config.storage.slow_io_warning_ns",
            self.config.storage.slow_io_warning_ns,
        )?;
        bounded_nonzero(
            "config.storage.slow_io_critical_ns",
            self.config.storage.slow_io_critical_ns,
        )?;
        bounded_nonzero("batching.send_interval_ms", self.batching.send_interval_ms)?;
        bounded_nonzero("batching.max_batch_events", self.batching.max_batch_events)?;
        Ok(())
    }
}

impl HeartbeatRequest {
    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("agent_id", &self.agent_id)?;
        required("node_name", &self.node_name)?;
        required("config_version", &self.config_version)?;
        if self.observers.is_empty() {
            return Err(WireError::MissingField("observers"));
        }
        Ok(())
    }
}

impl HeartbeatResponse {
    pub fn accepted(next_config_version: impl Into<String>) -> Self {
        Self {
            wire_version: WIRE_VERSION.into(),
            accepted: true,
            next_config_version: next_config_version.into(),
        }
    }

    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("next_config_version", &self.next_config_version)?;
        Ok(())
    }
}

impl EventBatchRequest {
    pub fn validate(&self, max_batch_events: usize) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("agent_id", &self.agent_id)?;
        required("node_name", &self.node_name)?;
        if self.events.is_empty() {
            return Err(WireError::MissingField("events"));
        }
        if self.events.len() > max_batch_events {
            return Err(WireError::InvalidField {
                field: "events",
                reason: "batch exceeds max_batch_events",
            });
        }
        for event in &self.events {
            event.validate()?;
        }
        Ok(())
    }
}

impl WireEvent {
    pub fn validate(&self) -> WireResult<()> {
        required("type", &self.event_type)?;
        required("observer", &self.observer)?;
        if !self.event_type.starts_with("kernel.") {
            return Err(WireError::InvalidField {
                field: "type",
                reason: "must be a kernel.* event",
            });
        }
        reject_reasoning_fields("$", &self.facts)
    }
}

impl EventBatchResponse {
    pub fn validate(&self) -> WireResult<()> {
        validate_wire_version(&self.wire_version)?;
        required("next_config_version", &self.next_config_version)?;
        Ok(())
    }
}

pub fn validate_wire_version(version: &str) -> WireResult<()> {
    if version == WIRE_VERSION {
        Ok(())
    } else {
        Err(WireError::UnsupportedVersion(version.into()))
    }
}

fn required(field: &'static str, value: &str) -> WireResult<()> {
    if value.trim().is_empty() {
        Err(WireError::MissingField(field))
    } else {
        Ok(())
    }
}

fn bounded_nonzero(field: &'static str, value: u64) -> WireResult<()> {
    if value == 0 {
        Err(WireError::InvalidField {
            field,
            reason: "must be greater than zero",
        })
    } else {
        Ok(())
    }
}

fn reject_reasoning_fields(path: &str, value: &serde_json::Value) -> WireResult<()> {
    let serde_json::Value::Object(map) = value else {
        return Ok(());
    };

    for (key, child) in map {
        if is_reasoning_key(key) {
            return Err(WireError::ReasoningField(format!("{path}.{key}")));
        }
        reject_reasoning_fields(&format!("{path}.{key}"), child)?;
    }
    Ok(())
}

fn is_reasoning_key(key: &str) -> bool {
    matches!(
        key,
        "reasoning"
            | "root_cause"
            | "rootCause"
            | "causal_chain"
            | "causalChain"
            | "explanation"
            | "possible_causes"
            | "possibleCauses"
            | "suggested_fix"
            | "suggestedFix"
            | "remediation"
            | "recommendation"
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hello() -> HelloRequest {
        HelloRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            tapio_version: "4.0.0".into(),
            kernel_release: "6.8.0".into(),
            arch: "x86_64".into(),
            capabilities: vec!["network".into(), "storage".into()],
            object_sizes: BTreeMap::from([("network_monitor.o".into(), 39832)]),
            map_counts: BTreeMap::from([("network".into(), 4)]),
        }
    }

    fn heartbeat() -> HeartbeatRequest {
        HeartbeatRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            config_version: "1".into(),
            uptime_seconds: 1234,
            observers: BTreeMap::from([
                ("network".into(), ObserverStatus::Running),
                ("node_pmc".into(), ObserverStatus::Disabled),
            ]),
            counters: HeartbeatCounters {
                events_total: 1000,
                malformed_events_total: 0,
                lost_events_total: 0,
                correlation_drops_total: 0,
                sink_drops_total: 0,
                controller_send_failures_total: 0,
            },
            degraded_reasons: vec![],
        }
    }

    fn batch() -> EventBatchRequest {
        EventBatchRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            sequence: 3912,
            sent_at_unix_nanos: 1_780_870_012_123_456_789,
            events: vec![WireEvent {
                event_type: "kernel.network.connection_refused".into(),
                timestamp_unix_nanos: 1_780_870_011_123_456_789,
                observer: "network".into(),
                severity: EventSeverity::Warning,
                facts: serde_json::json!({
                    "src_addr": "10.244.1.4",
                    "dst_addr": "10.244.2.9",
                    "dst_port": 50798,
                    "protocol": "tcp"
                }),
            }],
        }
    }

    #[test]
    fn hello_round_trip_and_validate() {
        let json = serde_json::to_string(&hello()).unwrap();
        let parsed: HelloRequest = serde_json::from_str(&json).unwrap();
        parsed.validate().unwrap();
    }

    #[test]
    fn hello_missing_required_field_fails_clearly() {
        let err = serde_json::from_str::<HelloRequest>(
            r#"{"wire_version":"tapio-wire/v1","agent_id":"node/a"}"#,
        )
        .unwrap_err();
        assert!(err.to_string().contains("node_name"));
    }

    #[test]
    fn unsupported_wire_version_fails_validation() {
        let mut req = hello();
        req.wire_version = "tapio-wire/v2".into();
        assert!(matches!(
            req.validate(),
            Err(WireError::UnsupportedVersion(version)) if version == "tapio-wire/v2"
        ));
    }

    #[test]
    fn config_round_trip_and_validate() {
        let config = ConfigResponse::default_v1();
        let json = serde_json::to_string(&config).unwrap();
        let parsed: ConfigResponse = serde_json::from_str(&json).unwrap();
        parsed.validate().unwrap();
    }

    #[test]
    fn config_unknown_fields_are_ignored() {
        let parsed: ConfigResponse = serde_json::from_str(
            r#"{
              "wire_version":"tapio-wire/v1",
              "version":"1",
              "unknown_future_field": true,
              "config":{
                "network":{
                  "enabled":true,
                  "rtt_spike_ratio":2,
                  "rtt_spike_abs_us":500000,
                  "rtt_min_baseline_samples":5,
                  "conn_refused_window_ns":0,
                  "conn_refused_min_count":0
                },
                "storage":{
                  "enabled":true,
                  "slow_io_warning_ns":50000000,
                  "slow_io_critical_ns":200000000
                },
                "container":{"enabled":true,"ignore_exit_codes":[]},
                "node_pmc":{
                  "enabled":true,
                  "stall_warning_permille":200,
                  "stall_critical_permille":400,
                  "ipc_degradation_milli":1000
                }
              },
              "batching":{"send_interval_ms":1000,"max_batch_events":256}
            }"#,
        )
        .unwrap();
        parsed.validate().unwrap();
    }

    #[test]
    fn config_missing_compiled_config_block_fails_deserialization() {
        let err = serde_json::from_str::<ConfigResponse>(
            r#"{
              "wire_version":"tapio-wire/v1",
              "version":"1",
              "batching":{"send_interval_ms":1000,"max_batch_events":256}
            }"#,
        )
        .unwrap_err();
        assert!(err.to_string().contains("config"));
    }

    #[test]
    fn config_bad_version_fails_validation() {
        let mut config = ConfigResponse::default_v1();
        config.wire_version = "tapio-wire/v0".into();
        assert!(config.validate().is_err());
    }

    #[test]
    fn heartbeat_round_trip_and_validate() {
        let json = serde_json::to_string(&heartbeat()).unwrap();
        let parsed: HeartbeatRequest = serde_json::from_str(&json).unwrap();
        parsed.validate().unwrap();
    }

    #[test]
    fn event_batch_round_trip_and_validate() {
        let json = serde_json::to_string(&batch()).unwrap();
        let parsed: EventBatchRequest = serde_json::from_str(&json).unwrap();
        parsed.validate(256).unwrap();
    }

    #[test]
    fn event_batch_rejects_reasoning_fields() {
        let mut batch = batch();
        batch.events[0].facts = serde_json::json!({
            "dst_port": 50798,
            "root_cause": "database is down"
        });
        assert!(matches!(
            batch.validate(256),
            Err(WireError::ReasoningField(field)) if field == "$.root_cause"
        ));
    }

    #[test]
    fn event_batch_rejects_non_kernel_event() {
        let mut batch = batch();
        batch.events[0].event_type = "app.database.slow".into();
        assert!(batch.validate(256).is_err());
    }
}
