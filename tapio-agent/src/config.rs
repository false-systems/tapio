use serde::Deserialize;
use std::path::Path;

/// Agent configuration loaded from TOML file.
/// CLI flags take precedence — fields here are all optional so missing values
/// fall through to CLI defaults.
/// Agent configuration loaded from TOML file.
/// Operational paths (sinks, ebpf_dir, data_dir) are CLI-only flags.
/// The config file controls thresholds, metrics, and grafana settings.
#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Config {
    pub thresholds: Thresholds,
    pub metrics: Metrics,
    pub grafana: Grafana,
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
}

impl Default for Metrics {
    fn default() -> Self {
        Self {
            enabled: false,
            port: 9090,
        }
    }
}

#[derive(Debug, Deserialize)]
#[serde(default)]
pub struct Grafana {
    pub endpoint: String,
    pub auth_header: Option<String>,
    pub batch_size: usize,
    pub flush_interval_secs: u64,
}

impl Default for Grafana {
    fn default() -> Self {
        Self {
            endpoint: "http://localhost:4318".into(),
            auth_header: None,
            batch_size: 100,
            flush_interval_secs: 5,
        }
    }
}

/// Load config from a TOML file. Returns default config if file doesn't exist.
pub fn load(path: &Path) -> anyhow::Result<Config> {
    if !path.exists() {
        tracing::debug!(path = %path.display(), "config file not found, using defaults");
        return Ok(Config::default());
    }

    let content = std::fs::read_to_string(path)?;
    let config: Config = toml::from_str(&content)?;
    tracing::info!(path = %path.display(), "loaded config");
    Ok(config)
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
}
