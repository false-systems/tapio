use serde::Deserialize;
use std::path::Path;
use tapio_common::ebpf::{
    TAPIO_CONFIG_ABI_VERSION, TAPIO_F_CONTAINER, TAPIO_F_NETWORK, TAPIO_F_NODE_PMC,
    TAPIO_F_STORAGE, TapioConfig,
};
use tapio_wire::CompiledConfig;

/// Agent configuration loaded from TOML file.
/// CLI flags take precedence — fields here are all optional so missing values
/// fall through to CLI defaults.
/// Operational paths (sinks, ebpf_dir, data_dir) are CLI-only flags.
/// The config file controls thresholds, metrics, and otlp sink settings.
#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Config {
    pub thresholds: Thresholds,
    pub metrics: Metrics,
    pub otlp: Otlp,
}

#[derive(Debug, Deserialize)]
#[serde(default)]
pub struct Thresholds {
    /// PMC: stall percentage for warning (memory_pressure). Default 20.0.
    pub stall_pct_warning: f64,
    /// PMC: stall percentage for critical (cpu_stall). Default 40.0.
    pub stall_pct_critical: f64,
    /// PMC: instructions-per-cycle below which IPC degradation fires. Default 1.0.
    pub ipc_degradation: f64,
    /// Network (eBPF): RTT spike ratio vs baseline. Default 2.
    pub rtt_spike_ratio: u64,
    /// Network (eBPF): absolute RTT spike threshold in microseconds. Default 500000.
    pub rtt_spike_abs_us: u64,
    /// Storage (eBPF): I/O latency warning threshold in nanoseconds. Default 50ms.
    pub io_latency_warning_ns: u64,
    /// Storage (eBPF): I/O latency critical threshold in nanoseconds. Default 200ms.
    pub io_latency_critical_ns: u64,
}

impl Default for Thresholds {
    fn default() -> Self {
        Self {
            stall_pct_warning: 20.0,
            stall_pct_critical: 40.0,
            ipc_degradation: 1.0,
            rtt_spike_ratio: 2,
            rtt_spike_abs_us: 500_000,
            io_latency_warning_ns: 50_000_000,
            io_latency_critical_ns: 200_000_000,
        }
    }
}

#[derive(Debug, Deserialize)]
#[serde(default)]
pub struct Metrics {
    pub enabled: bool,
    pub port: u16,
    /// Bind address for /metrics endpoint. Default 127.0.0.1 (localhost only).
    /// Set to "0.0.0.0" to expose to the node network.
    pub bind_address: String,
}

impl Default for Metrics {
    fn default() -> Self {
        Self {
            enabled: false,
            port: 9090,
            bind_address: "127.0.0.1".into(),
        }
    }
}

/// OTLP/HTTP sink settings. Targets any OTLP-compatible logs collector.
#[derive(Debug, Deserialize)]
#[serde(default)]
pub struct Otlp {
    pub endpoint: String,
    pub auth_header: Option<String>,
    pub batch_size: usize,
    pub flush_interval_secs: u64,
}

impl Default for Otlp {
    fn default() -> Self {
        Self {
            endpoint: "http://localhost:4318".into(),
            auth_header: None,
            batch_size: 100,
            flush_interval_secs: 5,
        }
    }
}

/// Reject CR, LF, and null bytes in the auth header to prevent HTTP header injection.
fn validate_auth_header(header: &Option<String>) -> anyhow::Result<()> {
    if let Some(value) = header
        && value.contains(&['\r', '\n', '\0'][..])
    {
        anyhow::bail!("otlp.auth_header contains invalid characters (CR/LF/null)");
    }
    Ok(())
}

/// Load config from a TOML file. Returns default config if file doesn't exist.
pub fn load(path: &Path) -> anyhow::Result<Config> {
    if !path.exists() {
        tracing::debug!(path = %path.display(), "config file not found, using defaults");
        return Ok(Config::default());
    }

    let content = std::fs::read_to_string(path)?;
    let config: Config = toml::from_str(&content)?;

    validate_auth_header(&config.otlp.auth_header)?;

    tracing::info!(path = %path.display(), "loaded config");
    Ok(config)
}

impl Config {
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub fn tapio_config(&self) -> TapioConfig {
        self.thresholds.tapio_config()
    }
}

impl Thresholds {
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub fn tapio_config(&self) -> TapioConfig {
        let rtt_spike_multiplier = self.rtt_spike_ratio.min(u32::MAX as u64) as u32;
        // RTT values in the kernel are u32 microseconds, so a clamped-to-max
        // threshold can never fire — same effect as the value the operator set.
        let rtt_spike_abs_us = self.rtt_spike_abs_us.min(u32::MAX as u64) as u32;

        // The count+array invariant is enforced at construction: all array
        // entries are populated before ignore_exit_count is set, and count
        // never exceeds the number of populated entries.
        let ignore_exit_codes = [0; tapio_common::ebpf::TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES];
        let ignore_exit_count = 0;

        TapioConfig {
            abi_version: TAPIO_CONFIG_ABI_VERSION,
            generation: 1,
            flags: TAPIO_F_NETWORK | TAPIO_F_STORAGE | TAPIO_F_CONTAINER | TAPIO_F_NODE_PMC,
            slow_io_threshold_ns: self.io_latency_warning_ns,
            io_latency_critical_ns: self.io_latency_critical_ns,
            conn_refused_window_ns: 0,
            conn_refused_min_count: 0,
            rtt_spike_multiplier,
            rtt_spike_abs_us,
            rtt_min_baseline_samples: 5,
            ignore_exit_count,
            ignore_exit_codes,
            _pad: 0,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub struct PmcThresholds {
    pub stall_pct_warning: f64,
    pub stall_pct_critical: f64,
    pub ipc_degradation: f64,
}

#[derive(Debug, PartialEq, Eq)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub enum CompiledConfigError {
    InvalidGeneration { value: String },
}

impl std::fmt::Display for CompiledConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidGeneration { value } => {
                write!(f, "config version {value:?} is not a valid u32 generation")
            }
        }
    }
}

impl std::error::Error for CompiledConfigError {}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub fn tapio_config_from_compiled(
    config: &CompiledConfig,
    generation: &str,
) -> Result<TapioConfig, CompiledConfigError> {
    let generation =
        generation
            .parse::<u32>()
            .map_err(|_| CompiledConfigError::InvalidGeneration {
                value: generation.to_string(),
            })?;

    let mut flags = 0;
    if config.network.enabled {
        flags |= TAPIO_F_NETWORK;
    }
    if config.storage.enabled {
        flags |= TAPIO_F_STORAGE;
    }
    if config.container.enabled {
        flags |= TAPIO_F_CONTAINER;
    }
    if config.node_pmc.enabled {
        flags |= TAPIO_F_NODE_PMC;
    }

    let mut ignore_exit_codes = [0; tapio_common::ebpf::TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES];
    for (slot, code) in ignore_exit_codes
        .iter_mut()
        .zip(config.container.ignore_exit_codes.iter())
    {
        *slot = *code;
    }
    let ignore_exit_count = config
        .container
        .ignore_exit_codes
        .len()
        .min(tapio_common::ebpf::TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES)
        as u32;

    Ok(TapioConfig {
        abi_version: TAPIO_CONFIG_ABI_VERSION,
        generation,
        flags,
        slow_io_threshold_ns: config.storage.slow_io_warning_ns,
        io_latency_critical_ns: config.storage.slow_io_critical_ns,
        conn_refused_window_ns: config.network.conn_refused_window_ns,
        conn_refused_min_count: config.network.conn_refused_min_count,
        rtt_spike_multiplier: config.network.rtt_spike_ratio,
        rtt_spike_abs_us: config.network.rtt_spike_abs_us,
        rtt_min_baseline_samples: config.network.rtt_min_baseline_samples,
        ignore_exit_count,
        ignore_exit_codes,
        _pad: 0,
    })
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub fn pmc_thresholds_from_compiled(config: &CompiledConfig) -> PmcThresholds {
    PmcThresholds {
        stall_pct_warning: config.node_pmc.stall_warning_permille as f64 / 10.0,
        stall_pct_critical: config.node_pmc.stall_critical_permille as f64 / 10.0,
        ipc_degradation: config.node_pmc.ipc_degradation_milli as f64 / 1000.0,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_toml_uses_defaults() {
        let config: Config = toml::from_str("").unwrap();
        assert_eq!(config.thresholds.stall_pct_warning, 20.0);
        assert_eq!(config.thresholds.ipc_degradation, 1.0);
        assert_eq!(config.metrics.port, 9090);
        assert!(!config.metrics.enabled);
    }

    #[test]
    fn partial_thresholds_override() {
        let config: Config = toml::from_str(
            r#"
            [thresholds]
            stall_pct_warning = 15.0
            "#,
        )
        .unwrap();
        assert_eq!(config.thresholds.stall_pct_warning, 15.0);
        assert_eq!(config.thresholds.stall_pct_critical, 40.0);
    }

    #[test]
    fn unknown_fields_ignored() {
        // Operational paths (sinks, ebpf_dir) are CLI-only — config ignores them
        let config: Config = toml::from_str(
            r#"
            sinks = ["stdout", "file"]
            ebpf_dir = "/custom/ebpf"
            "#,
        )
        .unwrap();
        // Should parse without error, fields silently ignored
        assert_eq!(config.thresholds.stall_pct_warning, 20.0);
    }

    #[test]
    fn auth_header_rejects_cr() {
        let header = Some("Bearer token\rfoo".into());
        assert!(validate_auth_header(&header).is_err());
    }

    #[test]
    fn auth_header_rejects_lf() {
        let header = Some("Bearer token\nfoo".into());
        assert!(validate_auth_header(&header).is_err());
    }

    #[test]
    fn auth_header_rejects_null() {
        let header = Some("Bearer token\0foo".into());
        assert!(validate_auth_header(&header).is_err());
    }

    #[test]
    fn auth_header_accepts_valid() {
        let header = Some("Bearer my-secret-token".into());
        assert!(validate_auth_header(&header).is_ok());
    }

    #[test]
    fn auth_header_accepts_none() {
        assert!(validate_auth_header(&None).is_ok());
    }

    #[test]
    fn metrics_section() {
        let config: Config = toml::from_str(
            r#"
            [metrics]
            enabled = true
            port = 8080
            "#,
        )
        .unwrap();
        assert!(config.metrics.enabled);
        assert_eq!(config.metrics.port, 8080);
    }

    #[test]
    fn metrics_default_bind_address_is_valid_ip() {
        let config = Config::default();
        let ip: std::net::IpAddr = config.metrics.bind_address.parse().unwrap();
        assert!(ip.is_loopback());
    }

    #[test]
    fn metrics_bind_address_ipv6() {
        let config: Config = toml::from_str(
            r#"
            [metrics]
            enabled = true
            bind_address = "::1"
            "#,
        )
        .unwrap();
        let ip: std::net::IpAddr = config.metrics.bind_address.parse().unwrap();
        assert!(ip.is_loopback());
        let addr = std::net::SocketAddr::new(ip, config.metrics.port);
        assert_eq!(addr.to_string(), "[::1]:9090");
    }

    #[test]
    fn tapio_config_uses_generation_one_for_nonzero_config() {
        let config = Config::default().tapio_config();
        assert_eq!(config.abi_version, TAPIO_CONFIG_ABI_VERSION);
        assert_eq!(config.generation, 1);
        assert_eq!(
            config.flags,
            TAPIO_F_NETWORK | TAPIO_F_STORAGE | TAPIO_F_CONTAINER | TAPIO_F_NODE_PMC
        );
    }

    #[test]
    fn tapio_config_sets_primitive_thresholds() {
        let thresholds = Thresholds {
            rtt_spike_ratio: 3,
            rtt_spike_abs_us: 250_000,
            io_latency_warning_ns: 123,
            io_latency_critical_ns: 456,
            ..Thresholds::default()
        };
        let config = thresholds.tapio_config();
        assert_eq!(config.rtt_spike_multiplier, 3);
        assert_eq!(config.rtt_spike_abs_us, 250_000);
        assert_eq!(config.rtt_min_baseline_samples, 5);
        assert_eq!(config.slow_io_threshold_ns, 123);
        assert_eq!(config.io_latency_critical_ns, 456);
    }

    #[test]
    fn tapio_config_clamps_rtt_multiplier_to_u32() {
        let thresholds = Thresholds {
            rtt_spike_ratio: u64::MAX,
            ..Thresholds::default()
        };
        let config = thresholds.tapio_config();
        assert_eq!(config.rtt_spike_multiplier, u32::MAX);
    }

    #[test]
    fn tapio_config_clamps_rtt_abs_threshold_to_u32() {
        let thresholds = Thresholds {
            rtt_spike_abs_us: u64::MAX,
            ..Thresholds::default()
        };
        let config = thresholds.tapio_config();
        assert_eq!(config.rtt_spike_abs_us, u32::MAX);
    }

    #[test]
    fn tapio_config_ignore_exit_count_never_exceeds_populated_entries() {
        let config = Config::default().tapio_config();
        assert_eq!(config.ignore_exit_count, 0);
        assert!(config.ignore_exit_count as usize <= config.ignore_exit_codes.len());
    }

    fn compiled_config() -> CompiledConfig {
        CompiledConfig {
            network: tapio_wire::CompiledNetwork {
                enabled: true,
                rtt_spike_ratio: 3,
                rtt_spike_abs_us: 250_000,
                rtt_min_baseline_samples: 7,
                conn_refused_window_ns: 1_000_000_000,
                conn_refused_min_count: 4,
            },
            storage: tapio_wire::CompiledStorage {
                enabled: true,
                slow_io_warning_ns: 150_000_000,
                slow_io_critical_ns: 400_000_000,
            },
            container: tapio_wire::CompiledContainer {
                enabled: true,
                ignore_exit_codes: vec![0, 143],
            },
            node_pmc: tapio_wire::CompiledNodePmc {
                enabled: false,
                stall_warning_permille: 250,
                stall_critical_permille: 500,
                ipc_degradation_milli: 800,
            },
        }
    }

    #[test]
    fn compiled_config_maps_to_tapio_config_fields() {
        let config = tapio_config_from_compiled(&compiled_config(), "17").unwrap();
        assert_eq!(config.abi_version, TAPIO_CONFIG_ABI_VERSION);
        assert_eq!(config.generation, 17);
        assert_eq!(
            config.flags,
            TAPIO_F_NETWORK | TAPIO_F_STORAGE | TAPIO_F_CONTAINER
        );
        assert_eq!(config.rtt_spike_multiplier, 3);
        assert_eq!(config.rtt_spike_abs_us, 250_000);
        assert_eq!(config.rtt_min_baseline_samples, 7);
        assert_eq!(config.conn_refused_window_ns, 1_000_000_000);
        assert_eq!(config.conn_refused_min_count, 4);
        assert_eq!(config.slow_io_threshold_ns, 150_000_000);
        assert_eq!(config.io_latency_critical_ns, 400_000_000);
        assert_eq!(config.ignore_exit_count, 2);
        assert_eq!(config.ignore_exit_codes[0], 0);
        assert_eq!(config.ignore_exit_codes[1], 143);
    }

    #[test]
    fn compiled_config_rejects_invalid_generation() {
        assert_eq!(
            tapio_config_from_compiled(&compiled_config(), "not-a-number").unwrap_err(),
            CompiledConfigError::InvalidGeneration {
                value: "not-a-number".into(),
            }
        );
    }

    #[test]
    fn compiled_config_truncates_ignore_exit_codes_to_abi_bound() {
        let mut compiled = compiled_config();
        compiled.container.ignore_exit_codes = (0..32).collect();
        let config = tapio_config_from_compiled(&compiled, "1").unwrap();
        assert_eq!(
            config.ignore_exit_count as usize,
            tapio_common::ebpf::TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES
        );
        assert_eq!(config.ignore_exit_codes[15], 15);
    }

    #[test]
    fn compiled_config_maps_pmc_thresholds_to_f64() {
        let thresholds = pmc_thresholds_from_compiled(&compiled_config());
        assert_eq!(thresholds.stall_pct_warning, 25.0);
        assert_eq!(thresholds.stall_pct_critical, 50.0);
        assert_eq!(thresholds.ipc_degradation, 0.8);
    }
}
