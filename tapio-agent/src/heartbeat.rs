#![cfg_attr(not(target_os = "linux"), allow(dead_code))]

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use tapio_wire::{
    DegradedReason, HeartbeatCounters, HeartbeatRequest, ObserverStatus, WIRE_VERSION,
};

const OBSERVERS: [(&str, u64); 4] = [
    ("network", tapio_common::ebpf::TAPIO_F_NETWORK),
    ("storage", tapio_common::ebpf::TAPIO_F_STORAGE),
    ("container", tapio_common::ebpf::TAPIO_F_CONTAINER),
    ("node", tapio_common::ebpf::TAPIO_F_NODE_PMC),
];

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AppliedConfigIdentity {
    pub version: String,
    pub hash: String,
    pub flags: u64,
}

impl AppliedConfigIdentity {
    pub fn new(version: impl Into<String>, hash: impl Into<String>, flags: u64) -> Self {
        Self {
            version: version.into(),
            hash: hash.into(),
            flags,
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct ActiveConfigIdentity {
    inner: Arc<Mutex<Option<AppliedConfigIdentity>>>,
}

impl ActiveConfigIdentity {
    pub fn applied(&self) -> Option<AppliedConfigIdentity> {
        self.inner
            .lock()
            .expect("active config identity lock poisoned")
            .clone()
    }

    pub fn mark_applied(&self, identity: AppliedConfigIdentity) {
        *self
            .inner
            .lock()
            .expect("active config identity lock poisoned") = Some(identity);
    }
}

pub struct HeartbeatSnapshot {
    pub agent_id: String,
    pub node_name: String,
    pub uptime_seconds: u64,
    pub observers: BTreeMap<String, ObserverStatus>,
    pub counters: HeartbeatCounters,
    pub controller_mode: bool,
    pub active_config: Option<AppliedConfigIdentity>,
}

pub fn build_heartbeat(snapshot: HeartbeatSnapshot) -> HeartbeatRequest {
    let active_config = snapshot.active_config;
    let degraded_reasons = if snapshot.controller_mode && active_config.is_none() {
        vec![DegradedReason::Unconfigured]
    } else {
        Vec::new()
    };
    let (config_version, config_hash) = match active_config {
        Some(config) => (config.version, config.hash),
        None => ("0".into(), String::new()),
    };

    HeartbeatRequest {
        wire_version: WIRE_VERSION.into(),
        agent_id: snapshot.agent_id,
        node_name: snapshot.node_name,
        config_version,
        config_hash,
        uptime_seconds: snapshot.uptime_seconds,
        observers: snapshot.observers,
        counters: snapshot.counters,
        degraded_reasons,
    }
}

pub(crate) fn observer_statuses(
    active_config: Option<&AppliedConfigIdentity>,
) -> BTreeMap<String, ObserverStatus> {
    OBSERVERS
        .into_iter()
        .map(|(observer, flag)| {
            let status = match active_config {
                Some(config) if (config.flags & flag) != 0 => ObserverStatus::Running,
                _ => ObserverStatus::Disabled,
            };
            (observer.to_string(), status)
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn counters() -> HeartbeatCounters {
        HeartbeatCounters {
            events_total: 0,
            malformed_events_total: 0,
            lost_events_total: 0,
            correlation_drops_total: 0,
            sink_drops_total: 0,
            controller_send_failures_total: 0,
        }
    }

    fn snapshot(
        controller_mode: bool,
        active_config: Option<AppliedConfigIdentity>,
    ) -> HeartbeatSnapshot {
        HeartbeatSnapshot {
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            uptime_seconds: 12,
            observers: BTreeMap::from([("network".into(), ObserverStatus::Running)]),
            counters: counters(),
            controller_mode,
            active_config,
        }
    }

    #[test]
    fn heartbeat_builder_uses_applied_not_fetched_hash() {
        let active = ActiveConfigIdentity::default();
        let fetched_but_not_applied = AppliedConfigIdentity::new("18", "sha256:fetched", 0);
        active.mark_applied(AppliedConfigIdentity::new(
            "17",
            "sha256:applied",
            tapio_common::ebpf::TAPIO_F_NETWORK,
        ));

        let heartbeat = build_heartbeat(snapshot(true, active.applied()));

        assert_ne!(heartbeat.config_hash, fetched_but_not_applied.hash);
        assert_eq!(heartbeat.config_version, "17");
        assert_eq!(heartbeat.config_hash, "sha256:applied");
        assert!(heartbeat.degraded_reasons.is_empty());
    }

    #[test]
    fn unconfigured_controller_mode_reports_empty_hash_and_degraded_reason() {
        let heartbeat = build_heartbeat(snapshot(true, None));

        assert_eq!(heartbeat.config_version, "0");
        assert_eq!(heartbeat.config_hash, "");
        assert_eq!(
            heartbeat.degraded_reasons,
            vec![DegradedReason::Unconfigured]
        );
    }

    #[test]
    fn standalone_without_config_hash_is_not_degraded() {
        let heartbeat = build_heartbeat(snapshot(false, None));

        assert_eq!(heartbeat.config_version, "0");
        assert_eq!(heartbeat.config_hash, "");
        assert!(heartbeat.degraded_reasons.is_empty());
    }

    #[test]
    fn unconfigured_observers_are_reported_disabled() {
        let observers = observer_statuses(None);

        assert_eq!(observers.len(), 4);
        assert!(
            observers
                .values()
                .all(|status| *status == ObserverStatus::Disabled)
        );
    }

    #[test]
    fn configured_observers_are_reported_from_applied_flags() {
        let active = AppliedConfigIdentity::new(
            "1",
            "sha256:active",
            tapio_common::ebpf::TAPIO_F_NETWORK | tapio_common::ebpf::TAPIO_F_STORAGE,
        );
        let observers = observer_statuses(Some(&active));

        assert_eq!(observers.len(), 4);
        assert_eq!(observers["network"], ObserverStatus::Running);
        assert_eq!(observers["storage"], ObserverStatus::Running);
        assert_eq!(observers["container"], ObserverStatus::Disabled);
        assert_eq!(observers["node"], ObserverStatus::Disabled);
    }
}
