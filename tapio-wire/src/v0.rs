use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

pub const WIRE_VERSION: &str = "tapio-wire/v0";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentHello {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub tapio_version: String,
    pub kernel_release: String,
    pub arch: String,
    #[serde(default)]
    pub capabilities: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentHelloResponse {
    pub wire_version: String,
    pub accepted: bool,
    pub controller_id: String,
    pub config_version: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentConfigRequest {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub current_config_version: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentConfigResponse {
    pub wire_version: String,
    pub config_version: String,
    pub config: CompiledConfig,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentHeartbeat {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub config_version: String,
    pub uptime_seconds: u64,
    pub counters: AgentCounters,
    #[serde(default)]
    pub degraded_reasons: Vec<DegradedReason>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct EventBatch {
    pub wire_version: String,
    pub agent_id: String,
    pub node_name: String,
    pub sequence: u64,
    pub sent_at_unix_nanos: u64,
    #[serde(default)]
    pub events: Vec<serde_json::Value>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct EventBatchResponse {
    pub wire_version: String,
    pub accepted_events: u64,
    pub rejected_events: u64,
    pub next_config_version: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CompiledConfig {
    pub network: CompiledNetwork,
    pub storage: CompiledStorage,
    pub container: CompiledContainer,
    pub node_pmc: CompiledNodePmc,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CompiledNetwork {
    pub enabled: bool,
    pub rtt_spike_ratio: u32,
    pub rtt_spike_abs_us: u32,
    pub rtt_min_baseline_samples: u32,
    pub conn_refused_window_ns: u64,
    pub conn_refused_min_count: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CompiledStorage {
    pub enabled: bool,
    pub slow_io_warning_ns: u64,
    pub slow_io_critical_ns: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CompiledContainer {
    pub enabled: bool,
    pub ignore_exit_codes: Vec<i32>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CompiledNodePmc {
    pub enabled: bool,
    pub stall_warning_permille: u32,
    pub stall_critical_permille: u32,
    pub ipc_degradation_milli: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentCounters {
    pub events_total: u64,
    pub anomalies_total: u64,
    pub malformed_events_total: u64,
    pub lost_events_total: u64,
    pub correlation_drops_total: u64,
    pub sink_drops_total: u64,
    pub controller_send_failures_total: u64,
    #[serde(default)]
    pub observer_events_total: BTreeMap<String, u64>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum DegradedReason {
    LostEvents,
    MalformedEvents,
    CorrelationDrops,
    SinkBackpressure,
    ControllerUnavailable,
    ObserverFailed,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn compiled_config() -> CompiledConfig {
        CompiledConfig {
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
                enabled: false,
                stall_warning_permille: 200,
                stall_critical_permille: 400,
                ipc_degradation_milli: 1000,
            },
        }
    }

    fn counters() -> AgentCounters {
        AgentCounters {
            events_total: 42,
            anomalies_total: 3,
            malformed_events_total: 0,
            lost_events_total: 0,
            correlation_drops_total: 1,
            sink_drops_total: 0,
            controller_send_failures_total: 0,
            observer_events_total: BTreeMap::from([
                ("network".to_string(), 20),
                ("storage".to_string(), 22),
            ]),
        }
    }

    #[test]
    fn agent_hello_round_trips_json() {
        let value = AgentHello {
            wire_version: WIRE_VERSION.to_string(),
            agent_id: "node/worker-1".to_string(),
            node_name: "worker-1".to_string(),
            tapio_version: "4.0.0".to_string(),
            kernel_release: "6.17.0".to_string(),
            arch: "aarch64".to_string(),
            capabilities: vec!["network".to_string(), "storage".to_string()],
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentHello = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn agent_hello_response_round_trips_json() {
        let value = AgentHelloResponse {
            wire_version: WIRE_VERSION.to_string(),
            accepted: true,
            controller_id: "tapio-controller/default".to_string(),
            config_version: "1".to_string(),
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentHelloResponse = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn agent_config_request_round_trips_json() {
        let value = AgentConfigRequest {
            wire_version: WIRE_VERSION.to_string(),
            agent_id: "node/worker-1".to_string(),
            node_name: "worker-1".to_string(),
            current_config_version: "1".to_string(),
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentConfigRequest = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn agent_config_response_round_trips_json() {
        let value = AgentConfigResponse {
            wire_version: WIRE_VERSION.to_string(),
            config_version: "2".to_string(),
            config: compiled_config(),
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentConfigResponse = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn compiled_config_round_trips_json() {
        let value = compiled_config();
        let json = serde_json::to_string(&value).unwrap();
        let parsed: CompiledConfig = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn agent_heartbeat_round_trips_json() {
        let value = AgentHeartbeat {
            wire_version: WIRE_VERSION.to_string(),
            agent_id: "node/worker-1".to_string(),
            node_name: "worker-1".to_string(),
            config_version: "2".to_string(),
            uptime_seconds: 60,
            counters: counters(),
            degraded_reasons: vec![DegradedReason::CorrelationDrops],
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentHeartbeat = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn event_batch_round_trips_json() {
        let value = EventBatch {
            wire_version: WIRE_VERSION.to_string(),
            agent_id: "node/worker-1".to_string(),
            node_name: "worker-1".to_string(),
            sequence: 7,
            sent_at_unix_nanos: 1_780_870_012_123_456_789,
            events: vec![serde_json::json!({
                "type": "kernel.network.connection_refused",
                "severity": "warning",
                "data": {
                    "dst_ip": "127.0.0.1",
                    "dst_port": 50798
                }
            })],
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: EventBatch = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }

    #[test]
    fn event_batch_response_round_trips_json() {
        let value = EventBatchResponse {
            wire_version: WIRE_VERSION.to_string(),
            accepted_events: 1,
            rejected_events: 0,
            next_config_version: "2".to_string(),
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: EventBatchResponse = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }
}
