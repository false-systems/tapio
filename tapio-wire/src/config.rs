use serde::{Deserialize, Serialize};

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
                enabled: true,
                stall_warning_permille: 200,
                stall_critical_permille: 400,
                ipc_degradation_milli: 1000,
            },
        }
    }

    #[test]
    fn compiled_config_round_trips_json() {
        let value = compiled_config();
        let json = serde_json::to_string(&value).unwrap();
        let parsed: CompiledConfig = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, value);
    }
}
