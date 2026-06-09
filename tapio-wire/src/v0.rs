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
    pub observers: ObserverConfig,
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
pub struct ObserverConfig {
    pub network_enabled: bool,
    pub storage_enabled: bool,
    pub container_enabled: bool,
    pub node_pmc_enabled: bool,
    pub storage_latency_warning_ns: u64,
    pub storage_latency_critical_ns: u64,
    pub rtt_spike_ratio: u64,
    pub rtt_spike_abs_us: u64,
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

    fn observer_config() -> ObserverConfig {
        ObserverConfig {
            network_enabled: true,
            storage_enabled: true,
            container_enabled: true,
            node_pmc_enabled: false,
            storage_latency_warning_ns: 50_000_000,
            storage_latency_critical_ns: 200_000_000,
            rtt_spike_ratio: 2,
            rtt_spike_abs_us: 500_000,
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
            observers: observer_config(),
        };

        let json = serde_json::to_string(&value).unwrap();
        let parsed: AgentConfigResponse = serde_json::from_str(&json).unwrap();
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
