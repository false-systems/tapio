//! Tapio anomaly types in the kernel.* namespace.
//! Each constant names a specific failure pattern Tapio detects close to the node.
//! These are stable product concepts, not arbitrary event labels.

/// Hierarchical event type. Format: kernel.<observer>.<anomaly>
#[derive(Debug, Clone, PartialEq, Eq, Hash, serde::Serialize, serde::Deserialize)]
pub struct EventType(pub String);

// Network anomalies
pub const NETWORK_CONNECTION_REFUSED: &str = "kernel.network.connection_refused";
pub const NETWORK_CONNECTION_TIMEOUT: &str = "kernel.network.connection_timeout";
pub const NETWORK_RETRANSMIT_SPIKE: &str = "kernel.network.retransmit_spike";
pub const NETWORK_RTT_DEGRADATION: &str = "kernel.network.rtt_degradation";

// Container anomalies
pub const CONTAINER_OOM_KILL: &str = "kernel.container.oom_kill";
pub const CONTAINER_ABNORMAL_EXIT: &str = "kernel.container.abnormal_exit";

// Storage anomalies
pub const STORAGE_IO_ERROR: &str = "kernel.storage.io_error";
pub const STORAGE_LATENCY_SPIKE: &str = "kernel.storage.latency_spike";

// Node anomalies (PMC)
pub const NODE_CPU_STALL: &str = "kernel.node.cpu_stall";
pub const NODE_MEMORY_PRESSURE: &str = "kernel.node.memory_pressure";
pub const NODE_IPC_DEGRADATION: &str = "kernel.node.ipc_degradation";
