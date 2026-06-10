//! Evidence Profile validation and compilation.
//!
//! `tapio-profile` is pure: callers deserialize operator input into
//! [`EvidenceProfile`], pass it to [`validate`], and compile the returned
//! [`ValidatedProfile`] into [`tapio_wire::CompiledConfig`].
//!
//! ```rust
//! use tapio_profile::{compile, validate, BuiltinSet, EvidenceProfile};
//!
//! let yaml = r#"
//! apiVersion: tapio.false.systems/v0
//! kind: EvidenceProfile
//! base: production-default
//! overrides:
//!   storage:
//!     slow_io:
//!       warning_ms: 150
//!       critical_ms: 400
//! "#;
//!
//! let profile: EvidenceProfile = serde_yaml::from_str(yaml)?;
//! let validated = validate(&profile, &BuiltinSet::v0())?;
//! let compiled = compile(&validated);
//! assert_eq!(compiled.storage.slow_io_warning_ns, 150_000_000);
//! # Ok::<(), Box<dyn std::error::Error>>(())
//! ```
//!
//! The serde layer rejects unknown fields, unknown observer blocks, and type
//! mismatches through `deny_unknown_fields`. The validate layer rejects semantic
//! errors with structured [`ProfileError`] values and field paths suitable for
//! later HTTP 400 responses.
//!
//! v0 refuses file I/O, YAML/JSON parsing in the core API, async, HTTP,
//! Kubernetes, CRDs, logging, metrics, global state, content hashing,
//! generation assignment, profile CRUD, profile storage, DSLs, dotted paths,
//! expressions, templating, multi-error accumulation, per-node targeting,
//! per-namespace targeting, `conn_refused_*` overrides,
//! `rtt_min_baseline_samples` overrides, floats in compiled output, and any
//! dependency from `tapio-agent` or `tapio-cli` to this crate.

use std::collections::BTreeMap;
use std::fmt;

use serde::Deserialize;
use tapio_wire::{
    CompiledConfig, CompiledContainer, CompiledNetwork, CompiledNodePmc, CompiledStorage,
};

const API_VERSION: &str = "tapio.false.systems/v0";
const KIND: &str = "EvidenceProfile";
const PRODUCTION_DEFAULT: &str = "production-default";

const RATIO_RANGE: &str = "1..=100";
const ABS_MS_RANGE: &str = "1..=600000";
const WARNING_MS_RANGE: &str = "1..=60000";
const CRITICAL_MS_RANGE: &str = "1..=600000";
const EXIT_CODE_RANGE: &str = "0..=255";
const STALL_PCT_RANGE: &str = "0.0..=100.0";
const IPC_DEGRADATION_RANGE: &str = "0.0..=16.0";

const MAX_RTT_ABS_MS: u32 = 600_000; // * 1_000 = 600_000_000, below u32::MAX.
const MAX_WARNING_MS: u64 = 60_000; // * 1_000_000 = 60_000_000_000, fits u64.
const MAX_CRITICAL_MS: u64 = 600_000; // * 1_000_000 = 600_000_000_000, fits u64.
const MAX_IGNORE_EXIT_CODES: usize = 16;

/// Operator-facing Evidence Profile document.
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EvidenceProfile {
    #[serde(rename = "apiVersion")]
    api_version: Option<String>,
    kind: Option<String>,
    base: Option<String>,
    #[serde(default)]
    overrides: ProfileOverrides,
}

/// Closed set of named builtin base profiles.
#[derive(Debug, Clone)]
pub struct BuiltinSet {
    profiles: BTreeMap<&'static str, ResolvedProfile>,
}

/// A fully resolved, range-checked Evidence Profile.
#[derive(Debug, Clone)]
pub struct ValidatedProfile {
    resolved: ResolvedProfile,
}

/// Field path in an Evidence Profile document.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FieldPath(&'static str);

impl fmt::Display for FieldPath {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.0)
    }
}

/// Structured validation errors for operator-facing HTTP 400s.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum ProfileError {
    #[error("{path}: unsupported apiVersion {value:?}, expected {expected:?}")]
    UnsupportedApiVersion {
        path: FieldPath,
        value: Option<String>,
        expected: &'static str,
    },
    #[error("{path}: unsupported kind {value:?}, expected {expected:?}")]
    UnsupportedKind {
        path: FieldPath,
        value: Option<String>,
        expected: &'static str,
    },
    #[error("{path}: base profile is required")]
    MissingBase { path: FieldPath },
    #[error("{path}: invalid profile name {value:?}")]
    InvalidProfileName { path: FieldPath, value: String },
    #[error("{path}: unknown base profile {value:?}, available: {available:?}")]
    UnknownBase {
        path: FieldPath,
        value: String,
        available: Vec<String>,
    },
    #[error("{path}: value {value} out of range {range}")]
    OutOfRange {
        path: FieldPath,
        value: String,
        range: &'static str,
    },
    #[error("{path}: value must be finite")]
    NotFinite { path: FieldPath },
    #[error("{path}: {critical} must be >= {warning}")]
    ThresholdOrder {
        path: FieldPath,
        warning: String,
        critical: String,
    },
    #[error("{path}: at most 16 ignore_exit_codes, got {count}")]
    TooManyIgnoreExitCodes { path: FieldPath, count: usize },
}

impl BuiltinSet {
    pub fn v0() -> Self {
        Self {
            profiles: BTreeMap::from([(PRODUCTION_DEFAULT, production_default())]),
        }
    }
}

pub fn validate(
    profile: &EvidenceProfile,
    builtins: &BuiltinSet,
) -> Result<ValidatedProfile, ProfileError> {
    validate_envelope(profile)?;

    let base_name = match &profile.base {
        Some(base) => base,
        None => return Err(ProfileError::MissingBase { path: path("base") }),
    };

    if !valid_profile_name(base_name) {
        return Err(ProfileError::InvalidProfileName {
            path: path("base"),
            value: base_name.clone(),
        });
    }

    let mut resolved = match builtins.profiles.get(base_name.as_str()) {
        Some(base) => base.clone(),
        None => {
            return Err(ProfileError::UnknownBase {
                path: path("base"),
                value: base_name.clone(),
                available: builtins
                    .profiles
                    .keys()
                    .map(|name| (*name).to_string())
                    .collect(),
            });
        }
    };

    apply_network(&profile.overrides.network, &mut resolved.network)?;
    apply_storage(&profile.overrides.storage, &mut resolved.storage)?;
    apply_container(&profile.overrides.container, &mut resolved.container)?;
    apply_node_pmc(&profile.overrides.node_pmc, &mut resolved.node_pmc)?;

    Ok(ValidatedProfile { resolved })
}

pub fn compile(profile: &ValidatedProfile) -> CompiledConfig {
    let resolved = &profile.resolved;
    CompiledConfig {
        network: CompiledNetwork {
            enabled: resolved.network.enabled,
            rtt_spike_ratio: resolved.network.rtt_spike_ratio,
            rtt_spike_abs_us: resolved.network.rtt_spike_abs_ms * 1_000,
            rtt_min_baseline_samples: resolved.network.rtt_min_baseline_samples,
            conn_refused_window_ns: resolved.network.conn_refused_window_ns,
            conn_refused_min_count: resolved.network.conn_refused_min_count,
        },
        storage: CompiledStorage {
            enabled: resolved.storage.enabled,
            slow_io_warning_ns: resolved.storage.slow_io_warning_ms * 1_000_000,
            slow_io_critical_ns: resolved.storage.slow_io_critical_ms * 1_000_000,
        },
        container: CompiledContainer {
            enabled: resolved.container.enabled,
            ignore_exit_codes: resolved
                .container
                .ignore_exit_codes
                .iter()
                .map(|code| i32::from(*code))
                .collect(),
        },
        node_pmc: CompiledNodePmc {
            enabled: resolved.node_pmc.enabled,
            stall_warning_permille: (resolved.node_pmc.stall_pct_warning * 10.0).round() as u32,
            stall_critical_permille: (resolved.node_pmc.stall_pct_critical * 10.0).round() as u32,
            ipc_degradation_milli: (resolved.node_pmc.ipc_degradation * 1000.0).round() as u32,
        },
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct ProfileOverrides {
    #[serde(default)]
    network: NetworkOverride,
    #[serde(default)]
    storage: StorageOverride,
    #[serde(default)]
    container: ContainerOverride,
    #[serde(default)]
    node_pmc: NodePmcOverride,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct NetworkOverride {
    enabled: Option<bool>,
    #[serde(default)]
    rtt_spike: RttSpikeOverride,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct RttSpikeOverride {
    ratio: Option<u32>,
    abs_ms: Option<u32>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct StorageOverride {
    enabled: Option<bool>,
    #[serde(default)]
    slow_io: SlowIoOverride,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct SlowIoOverride {
    warning_ms: Option<u64>,
    critical_ms: Option<u64>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct ContainerOverride {
    enabled: Option<bool>,
    ignore_exit_codes: Option<Vec<i32>>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct NodePmcOverride {
    enabled: Option<bool>,
    #[serde(default)]
    stall_pct: StallPctOverride,
    ipc_degradation: Option<f64>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct StallPctOverride {
    warning: Option<f64>,
    critical: Option<f64>,
}

#[derive(Debug, Clone)]
struct ResolvedProfile {
    network: ResolvedNetwork,
    storage: ResolvedStorage,
    container: ResolvedContainer,
    node_pmc: ResolvedNodePmc,
}

#[derive(Debug, Clone)]
struct ResolvedNetwork {
    enabled: bool,
    rtt_spike_ratio: u32,
    rtt_spike_abs_ms: u32,
    rtt_min_baseline_samples: u32,
    conn_refused_window_ns: u64,
    conn_refused_min_count: u32,
}

#[derive(Debug, Clone)]
struct ResolvedStorage {
    enabled: bool,
    slow_io_warning_ms: u64,
    slow_io_critical_ms: u64,
}

#[derive(Debug, Clone)]
struct ResolvedContainer {
    enabled: bool,
    ignore_exit_codes: Vec<u8>,
}

#[derive(Debug, Clone)]
struct ResolvedNodePmc {
    enabled: bool,
    stall_pct_warning: f64,
    stall_pct_critical: f64,
    ipc_degradation: f64,
}

fn validate_envelope(profile: &EvidenceProfile) -> Result<(), ProfileError> {
    if profile.api_version.as_deref() != Some(API_VERSION) {
        return Err(ProfileError::UnsupportedApiVersion {
            path: path("apiVersion"),
            value: profile.api_version.clone(),
            expected: API_VERSION,
        });
    }
    if profile.kind.as_deref() != Some(KIND) {
        return Err(ProfileError::UnsupportedKind {
            path: path("kind"),
            value: profile.kind.clone(),
            expected: KIND,
        });
    }
    Ok(())
}

fn apply_network(
    input: &NetworkOverride,
    resolved: &mut ResolvedNetwork,
) -> Result<(), ProfileError> {
    if let Some(enabled) = input.enabled {
        resolved.enabled = enabled;
    }
    if let Some(ratio) = input.rtt_spike.ratio {
        if !(1..=100).contains(&ratio) {
            return Err(out_of_range(
                "overrides.network.rtt_spike.ratio",
                ratio,
                RATIO_RANGE,
            ));
        }
        resolved.rtt_spike_ratio = ratio;
    }
    if let Some(abs_ms) = input.rtt_spike.abs_ms {
        if !(1..=MAX_RTT_ABS_MS).contains(&abs_ms) {
            return Err(out_of_range(
                "overrides.network.rtt_spike.abs_ms",
                abs_ms,
                ABS_MS_RANGE,
            ));
        }
        resolved.rtt_spike_abs_ms = abs_ms;
    }
    Ok(())
}

fn apply_storage(
    input: &StorageOverride,
    resolved: &mut ResolvedStorage,
) -> Result<(), ProfileError> {
    if let Some(enabled) = input.enabled {
        resolved.enabled = enabled;
    }
    if let Some(warning_ms) = input.slow_io.warning_ms {
        if !(1..=MAX_WARNING_MS).contains(&warning_ms) {
            return Err(out_of_range(
                "overrides.storage.slow_io.warning_ms",
                warning_ms,
                WARNING_MS_RANGE,
            ));
        }
        resolved.slow_io_warning_ms = warning_ms;
    }
    if let Some(critical_ms) = input.slow_io.critical_ms {
        if !(1..=MAX_CRITICAL_MS).contains(&critical_ms) {
            return Err(out_of_range(
                "overrides.storage.slow_io.critical_ms",
                critical_ms,
                CRITICAL_MS_RANGE,
            ));
        }
        resolved.slow_io_critical_ms = critical_ms;
    }
    if resolved.slow_io_critical_ms < resolved.slow_io_warning_ms {
        return Err(ProfileError::ThresholdOrder {
            path: path("overrides.storage.slow_io"),
            warning: resolved.slow_io_warning_ms.to_string(),
            critical: resolved.slow_io_critical_ms.to_string(),
        });
    }
    Ok(())
}

fn apply_container(
    input: &ContainerOverride,
    resolved: &mut ResolvedContainer,
) -> Result<(), ProfileError> {
    if let Some(enabled) = input.enabled {
        resolved.enabled = enabled;
    }
    if let Some(codes) = &input.ignore_exit_codes {
        if codes.len() > MAX_IGNORE_EXIT_CODES {
            return Err(ProfileError::TooManyIgnoreExitCodes {
                path: path("overrides.container.ignore_exit_codes"),
                count: codes.len(),
            });
        }
        let mut checked = Vec::with_capacity(codes.len());
        for code in codes {
            if !(0..=255).contains(code) {
                return Err(out_of_range(
                    "overrides.container.ignore_exit_codes",
                    code,
                    EXIT_CODE_RANGE,
                ));
            }
            checked.push(*code as u8);
        }
        resolved.ignore_exit_codes = checked;
    }
    Ok(())
}

fn apply_node_pmc(
    input: &NodePmcOverride,
    resolved: &mut ResolvedNodePmc,
) -> Result<(), ProfileError> {
    if let Some(enabled) = input.enabled {
        resolved.enabled = enabled;
    }
    if let Some(warning) = input.stall_pct.warning {
        validate_finite_range(
            "overrides.node_pmc.stall_pct.warning",
            warning,
            0.0,
            100.0,
            STALL_PCT_RANGE,
        )?;
        resolved.stall_pct_warning = warning;
    }
    if let Some(critical) = input.stall_pct.critical {
        validate_finite_range(
            "overrides.node_pmc.stall_pct.critical",
            critical,
            0.0,
            100.0,
            STALL_PCT_RANGE,
        )?;
        resolved.stall_pct_critical = critical;
    }
    if resolved.stall_pct_critical < resolved.stall_pct_warning {
        return Err(ProfileError::ThresholdOrder {
            path: path("overrides.node_pmc.stall_pct"),
            warning: resolved.stall_pct_warning.to_string(),
            critical: resolved.stall_pct_critical.to_string(),
        });
    }
    if let Some(ipc) = input.ipc_degradation {
        validate_finite_range(
            "overrides.node_pmc.ipc_degradation",
            ipc,
            0.0,
            16.0,
            IPC_DEGRADATION_RANGE,
        )?;
        resolved.ipc_degradation = ipc;
    }
    Ok(())
}

fn validate_finite_range(
    field: &'static str,
    value: f64,
    min: f64,
    max: f64,
    range: &'static str,
) -> Result<(), ProfileError> {
    if !value.is_finite() {
        return Err(ProfileError::NotFinite { path: path(field) });
    }
    if value < min || value > max {
        return Err(ProfileError::OutOfRange {
            path: path(field),
            value: value.to_string(),
            range,
        });
    }
    Ok(())
}

fn out_of_range<T: ToString>(field: &'static str, value: T, range: &'static str) -> ProfileError {
    ProfileError::OutOfRange {
        path: path(field),
        value: value.to_string(),
        range,
    }
}

fn valid_profile_name(value: &str) -> bool {
    let bytes = value.as_bytes();
    if bytes.is_empty() || bytes.len() > 63 || !bytes[0].is_ascii_lowercase() {
        return false;
    }
    bytes[1..]
        .iter()
        .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'-')
}

fn path(value: &'static str) -> FieldPath {
    FieldPath(value)
}

fn production_default() -> ResolvedProfile {
    ResolvedProfile {
        network: ResolvedNetwork {
            enabled: true,
            rtt_spike_ratio: 2,
            rtt_spike_abs_ms: 500,
            rtt_min_baseline_samples: 5,
            conn_refused_window_ns: 0,
            conn_refused_min_count: 0,
        },
        storage: ResolvedStorage {
            enabled: true,
            slow_io_warning_ms: 50,
            slow_io_critical_ms: 200,
        },
        container: ResolvedContainer {
            enabled: true,
            ignore_exit_codes: vec![],
        },
        node_pmc: ResolvedNodePmc {
            enabled: true,
            stall_pct_warning: 20.0,
            stall_pct_critical: 40.0,
            ipc_degradation: 1.0,
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(yaml: &str) -> EvidenceProfile {
        serde_yaml::from_str(yaml).unwrap()
    }

    fn minimal_yaml() -> &'static str {
        r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
"#
    }

    fn compile_yaml(yaml: &str) -> CompiledConfig {
        let profile = parse(yaml);
        let validated = validate(&profile, &BuiltinSet::v0()).unwrap();
        compile(&validated)
    }

    fn err(yaml: &str) -> ProfileError {
        let profile = parse(yaml);
        validate(&profile, &BuiltinSet::v0()).unwrap_err()
    }

    #[test]
    fn full_valid_document_parses_validates_and_compiles() {
        let compiled = compile_yaml(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    enabled: true
    rtt_spike:
      ratio: 3
      abs_ms: 250
  storage:
    enabled: true
    slow_io:
      warning_ms: 150
      critical_ms: 400
  container:
    enabled: true
    ignore_exit_codes: [0, 143]
  node_pmc:
    enabled: false
    stall_pct:
      warning: 25.0
      critical: 50.0
    ipc_degradation: 0.8
"#,
        );

        assert!(compiled.network.enabled);
        assert_eq!(compiled.network.rtt_spike_ratio, 3);
        assert_eq!(compiled.network.rtt_spike_abs_us, 250_000);
        assert_eq!(compiled.storage.slow_io_warning_ns, 150_000_000);
        assert_eq!(compiled.storage.slow_io_critical_ns, 400_000_000);
        assert_eq!(compiled.container.ignore_exit_codes, vec![0, 143]);
        assert!(!compiled.node_pmc.enabled);
        assert_eq!(compiled.node_pmc.stall_warning_permille, 250);
        assert_eq!(compiled.node_pmc.stall_critical_permille, 500);
        assert_eq!(compiled.node_pmc.ipc_degradation_milli, 800);
    }

    #[test]
    fn unknown_top_level_field_is_rejected_by_serde() {
        let error = serde_yaml::from_str::<EvidenceProfile>(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
surprise: true
"#,
        )
        .unwrap_err();
        assert!(error.to_string().contains("surprise"));
    }

    #[test]
    fn unknown_observer_is_rejected_by_serde() {
        let error = serde_yaml::from_str::<EvidenceProfile>(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  process:
    enabled: true
"#,
        )
        .unwrap_err();
        assert!(error.to_string().contains("process"));
    }

    #[test]
    fn unknown_observer_field_is_rejected_by_serde() {
        let error = serde_yaml::from_str::<EvidenceProfile>(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    label_selector: app=api
"#,
        )
        .unwrap_err();
        assert!(error.to_string().contains("label_selector"));
    }

    #[test]
    fn wrong_typed_field_is_rejected_by_serde() {
        let error = serde_yaml::from_str::<EvidenceProfile>(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      warning_ms: "slow"
"#,
        )
        .unwrap_err();
        assert!(error.to_string().contains("warning_ms"));
    }

    #[test]
    fn minimal_document_compiles_to_production_default() {
        assert_eq!(compile_yaml(minimal_yaml()), production_default_compiled());
    }

    #[test]
    fn unsupported_api_version_reports_path_and_value() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v1
kind: EvidenceProfile
base: production-default
"#),
            ProfileError::UnsupportedApiVersion {
                path: path("apiVersion"),
                value: Some("tapio.false.systems/v1".into()),
                expected: API_VERSION,
            }
        );
    }

    #[test]
    fn missing_api_version_is_unsupported_api_version() {
        assert_eq!(
            err(r#"
kind: EvidenceProfile
base: production-default
"#),
            ProfileError::UnsupportedApiVersion {
                path: path("apiVersion"),
                value: None,
                expected: API_VERSION,
            }
        );
    }

    #[test]
    fn unsupported_kind_reports_path_and_value() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: Other
base: production-default
"#),
            ProfileError::UnsupportedKind {
                path: path("kind"),
                value: Some("Other".into()),
                expected: KIND,
            }
        );
    }

    #[test]
    fn missing_base_reports_path() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
"#),
            ProfileError::MissingBase { path: path("base") }
        );
    }

    #[test]
    fn invalid_profile_name_reports_path_and_value() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: Production
"#),
            ProfileError::InvalidProfileName {
                path: path("base"),
                value: "Production".into(),
            }
        );
    }

    #[test]
    fn unknown_base_reports_available_names() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: staging
"#),
            ProfileError::UnknownBase {
                path: path("base"),
                value: "staging".into(),
                available: vec!["production-default".into()],
            }
        );
    }

    #[test]
    fn rtt_ratio_out_of_range_reports_path() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      ratio: 101
"#),
            out_of_range("overrides.network.rtt_spike.ratio", 101_u32, RATIO_RANGE)
        );
    }

    #[test]
    fn storage_warning_below_minimum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      warning_ms: 0
"#),
            out_of_range(
                "overrides.storage.slow_io.warning_ms",
                0_u64,
                WARNING_MS_RANGE
            )
        );
    }

    #[test]
    fn override_critical_below_base_warning_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      critical_ms: 40
"#),
            ProfileError::ThresholdOrder {
                path: path("overrides.storage.slow_io"),
                warning: "50".into(),
                critical: "40".into(),
            }
        );
    }

    #[test]
    fn too_many_ignore_exit_codes_reports_count() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  container:
    ignore_exit_codes: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]
"#),
            ProfileError::TooManyIgnoreExitCodes {
                path: path("overrides.container.ignore_exit_codes"),
                count: 17,
            }
        );
    }

    #[test]
    fn ignore_exit_code_out_of_range_reports_path() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  container:
    ignore_exit_codes: [256]
"#),
            out_of_range(
                "overrides.container.ignore_exit_codes",
                256,
                EXIT_CODE_RANGE
            )
        );
    }

    #[test]
    fn non_finite_stall_warning_is_rejected() {
        let mut profile = parse(minimal_yaml());
        profile.overrides.node_pmc.stall_pct.warning = Some(f64::NAN);
        assert_eq!(
            validate(&profile, &BuiltinSet::v0()).unwrap_err(),
            ProfileError::NotFinite {
                path: path("overrides.node_pmc.stall_pct.warning"),
            }
        );
    }

    #[test]
    fn stall_warning_above_maximum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  node_pmc:
    stall_pct:
      warning: 100.1
"#),
            ProfileError::OutOfRange {
                path: path("overrides.node_pmc.stall_pct.warning"),
                value: "100.1".into(),
                range: STALL_PCT_RANGE,
            }
        );
    }

    #[test]
    fn stall_critical_below_base_warning_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  node_pmc:
    stall_pct:
      critical: 10.0
"#),
            ProfileError::ThresholdOrder {
                path: path("overrides.node_pmc.stall_pct"),
                warning: "20".into(),
                critical: "10".into(),
            }
        );
    }

    #[test]
    fn non_finite_ipc_degradation_is_rejected() {
        let mut profile = parse(minimal_yaml());
        profile.overrides.node_pmc.ipc_degradation = Some(f64::INFINITY);
        assert_eq!(
            validate(&profile, &BuiltinSet::v0()).unwrap_err(),
            ProfileError::NotFinite {
                path: path("overrides.node_pmc.ipc_degradation"),
            }
        );
    }

    #[test]
    fn some_override_wins_and_none_inherits_field_by_field() {
        let compiled = compile_yaml(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      ratio: 4
"#,
        );

        assert_eq!(compiled.network.rtt_spike_ratio, 4);
        assert_eq!(compiled.network.rtt_spike_abs_us, 500_000);
        assert_eq!(compiled.storage, production_default_compiled().storage);
        assert_eq!(compiled.container, production_default_compiled().container);
        assert_eq!(compiled.node_pmc, production_default_compiled().node_pmc);
    }

    #[test]
    fn range_minima_are_accepted() {
        let compiled = compile_yaml(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      ratio: 1
      abs_ms: 1
  storage:
    slow_io:
      warning_ms: 1
      critical_ms: 1
  node_pmc:
    stall_pct:
      warning: 0.0
      critical: 0.0
    ipc_degradation: 0.0
"#,
        );

        assert_eq!(compiled.network.rtt_spike_ratio, 1);
        assert_eq!(compiled.network.rtt_spike_abs_us, 1_000);
        assert_eq!(compiled.storage.slow_io_warning_ns, 1_000_000);
        assert_eq!(compiled.storage.slow_io_critical_ns, 1_000_000);
        assert_eq!(compiled.node_pmc.stall_warning_permille, 0);
        assert_eq!(compiled.node_pmc.stall_critical_permille, 0);
        assert_eq!(compiled.node_pmc.ipc_degradation_milli, 0);
    }

    #[test]
    fn rtt_ratio_zero_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      ratio: 0
"#),
            out_of_range("overrides.network.rtt_spike.ratio", 0_u32, RATIO_RANGE)
        );
    }

    #[test]
    fn rtt_abs_ms_above_maximum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      abs_ms: 600001
"#),
            out_of_range(
                "overrides.network.rtt_spike.abs_ms",
                600_001_u32,
                ABS_MS_RANGE
            )
        );
    }

    #[test]
    fn storage_warning_above_maximum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      warning_ms: 60001
      critical_ms: 600000
"#),
            out_of_range(
                "overrides.storage.slow_io.warning_ms",
                60_001_u64,
                WARNING_MS_RANGE
            )
        );
    }

    #[test]
    fn storage_critical_above_maximum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  storage:
    slow_io:
      critical_ms: 600001
"#),
            out_of_range(
                "overrides.storage.slow_io.critical_ms",
                600_001_u64,
                CRITICAL_MS_RANGE
            )
        );
    }

    #[test]
    fn negative_stall_warning_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  node_pmc:
    stall_pct:
      warning: -0.1
"#),
            ProfileError::OutOfRange {
                path: path("overrides.node_pmc.stall_pct.warning"),
                value: "-0.1".into(),
                range: STALL_PCT_RANGE,
            }
        );
    }

    #[test]
    fn ipc_degradation_above_maximum_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  node_pmc:
    ipc_degradation: 16.1
"#),
            ProfileError::OutOfRange {
                path: path("overrides.node_pmc.ipc_degradation"),
                value: "16.1".into(),
                range: IPC_DEGRADATION_RANGE,
            }
        );
    }

    #[test]
    fn negative_ignore_exit_code_is_rejected() {
        assert_eq!(
            err(r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  container:
    ignore_exit_codes: [-1]
"#),
            out_of_range("overrides.container.ignore_exit_codes", -1, EXIT_CODE_RANGE)
        );
    }

    #[test]
    fn exactly_sixteen_ignore_exit_codes_are_accepted() {
        let compiled = compile_yaml(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  container:
    ignore_exit_codes: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15]
"#,
        );
        assert_eq!(
            compiled.container.ignore_exit_codes,
            (0..16).collect::<Vec<i32>>()
        );
    }

    #[test]
    fn range_maxima_convert_without_overflow() {
        let compiled = compile_yaml(
            r#"
apiVersion: tapio.false.systems/v0
kind: EvidenceProfile
base: production-default
overrides:
  network:
    rtt_spike:
      ratio: 100
      abs_ms: 600000
  storage:
    slow_io:
      warning_ms: 60000
      critical_ms: 600000
  node_pmc:
    stall_pct:
      warning: 100.0
      critical: 100.0
    ipc_degradation: 16.0
"#,
        );

        assert_eq!(compiled.network.rtt_spike_abs_us, 600_000_000);
        assert_eq!(compiled.storage.slow_io_warning_ns, 60_000_000_000);
        assert_eq!(compiled.storage.slow_io_critical_ns, 600_000_000_000);
        assert_eq!(compiled.node_pmc.stall_warning_permille, 1000);
        assert_eq!(compiled.node_pmc.stall_critical_permille, 1000);
        assert_eq!(compiled.node_pmc.ipc_degradation_milli, 16_000);
    }

    #[test]
    fn compile_is_deterministic_and_json_bytes_are_stable() {
        let profile = parse(minimal_yaml());
        let validated = validate(&profile, &BuiltinSet::v0()).unwrap();
        let first = compile(&validated);
        let second = compile(&validated);
        assert_eq!(first, second);
        assert_eq!(
            serde_json::to_vec(&first).unwrap(),
            serde_json::to_vec(&second).unwrap()
        );
    }

    #[test]
    fn production_default_validates_and_matches_current_agent_defaults() {
        let compiled = compile_yaml(minimal_yaml());
        assert_eq!(compiled, production_default_compiled());
        assert!(compiled.network.enabled);
        assert!(compiled.storage.enabled);
        assert!(compiled.container.enabled);
        assert!(compiled.node_pmc.enabled);
        assert_eq!(compiled.network.rtt_spike_ratio, 2);
        assert_eq!(compiled.network.rtt_spike_abs_us, 500_000);
        assert_eq!(compiled.network.rtt_min_baseline_samples, 5);
        assert_eq!(compiled.network.conn_refused_window_ns, 0);
        assert_eq!(compiled.network.conn_refused_min_count, 0);
        assert_eq!(compiled.storage.slow_io_warning_ns, 50_000_000);
        assert_eq!(compiled.storage.slow_io_critical_ns, 200_000_000);
        assert_eq!(compiled.container.ignore_exit_codes, Vec::<i32>::new());
        assert_eq!(compiled.node_pmc.stall_warning_permille, 200);
        assert_eq!(compiled.node_pmc.stall_critical_permille, 400);
        assert_eq!(compiled.node_pmc.ipc_degradation_milli, 1000);
    }

    fn production_default_compiled() -> CompiledConfig {
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
}
