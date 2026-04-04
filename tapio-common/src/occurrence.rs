/// FALSE Protocol Occurrence for TAPIO.
///
/// Wire format matches POLKU's OccurrenceIngestor exactly.
/// TAPIO provides context, not reasoning — AI agents and AHTI fill reasoning blocks.
use chrono::Utc;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Occurrence {
    pub id: String,
    pub timestamp: String,
    pub source: String,
    #[serde(rename = "type")]
    pub occurrence_type: String,
    pub severity: Severity,
    pub outcome: Outcome,
    pub protocol_version: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<OccurrenceError>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context: Option<Context>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reasoning: Option<Reasoning>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub history: Option<History>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum Severity {
    Debug,
    Info,
    Warning,
    Error,
    Critical,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Outcome {
    Success,
    Failure,
    Timeout,
    InProgress,
    Unknown,
}

/// Error block — factual kernel data.
/// TAPIO fills code + message. Fields like what_failed exist for POLKU schema compat.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OccurrenceError {
    pub code: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub what_failed: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub why_it_matters: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub possible_causes: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub suggested_fix: Option<String>,
}

/// Reasoning block — TAPIO never fills this. Exists for schema compat.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Reasoning {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub summary: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub explanation: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub causal_chain: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub patterns_matched: Option<Vec<String>>,
}

/// History block — TAPIO never fills this. Exists for schema compat.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct History {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub previous_ids: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lifecycle: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Context {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cluster: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub node: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub trace_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub span_id: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub entities: Vec<Entity>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub correlation_keys: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Entity {
    pub kind: String,
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
}

// --- Builder ---

impl Occurrence {
    pub fn new(occurrence_type: &str, severity: Severity, outcome: Outcome) -> Self {
        Self {
            id: ulid::Ulid::new().to_string(),
            timestamp: Utc::now().to_rfc3339(),
            source: "tapio".into(),
            occurrence_type: occurrence_type.into(),
            severity,
            outcome,
            protocol_version: "1.0".into(),
            error: None,
            context: None,
            reasoning: None,
            history: None,
            data: None,
        }
    }

    /// Create an occurrence with a precise wall-clock timestamp (nanoseconds since UNIX epoch).
    /// Use this when the event timestamp comes from bpf_ktime_get_ns() converted via boot offset.
    pub fn new_at(
        occurrence_type: &str,
        severity: Severity,
        outcome: Outcome,
        wall_ns: u64,
    ) -> Self {
        let secs = (wall_ns / 1_000_000_000) as i64;
        let nsecs = (wall_ns % 1_000_000_000) as u32;
        let ts = chrono::DateTime::from_timestamp(secs, nsecs)
            .unwrap_or_else(Utc::now)
            .to_rfc3339();
        Self {
            id: ulid::Ulid::new().to_string(),
            timestamp: ts,
            source: "tapio".into(),
            occurrence_type: occurrence_type.into(),
            severity,
            outcome,
            protocol_version: "1.0".into(),
            error: None,
            context: None,
            reasoning: None,
            history: None,
            data: None,
        }
    }

    pub fn with_error(mut self, code: &str, message: &str) -> Self {
        self.error = Some(OccurrenceError {
            code: code.into(),
            message: message.into(),
            what_failed: None,
            why_it_matters: None,
            possible_causes: None,
            suggested_fix: None,
        });
        self
    }

    pub fn with_context(mut self, ctx: Context) -> Self {
        self.context = Some(ctx);
        self
    }

    pub fn with_data(mut self, data: serde_json::Value) -> Self {
        self.data = Some(data);
        self
    }

    pub fn validate(&self) -> Result<(), Vec<String>> {
        let mut errors = Vec::new();

        if self.source.is_empty() {
            errors.push("source is required".into());
        }
        if self.occurrence_type.is_empty() {
            errors.push("type is required".into());
        } else if !self.occurrence_type.contains('.') {
            errors.push("type must have at least 2 dot-separated segments".into());
        }
        if self.protocol_version.is_empty() {
            errors.push("protocol_version is required".into());
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn occurrence_serialization_round_trip() {
        let occ = Occurrence::new(
            "kernel.container.oom_kill",
            Severity::Critical,
            Outcome::Failure,
        )
        .with_error("OOM_KILL", "Container killed by OOM killer")
        .with_context(Context {
            cluster: Some("prod".into()),
            node: Some("worker-3".into()),
            namespace: Some("default".into()),
            trace_id: None,
            span_id: None,
            entities: vec![Entity {
                kind: "pod".into(),
                id: "default/nginx-abc".into(),
                name: Some("nginx-abc".into()),
                version: None,
            }],
            correlation_keys: vec![],
        })
        .with_data(serde_json::json!({
            "memory_usage_bytes": 536870912_u64,
            "memory_limit_bytes": 536870912_u64,
            "pid": 1234,
        }));

        let json = serde_json::to_string(&occ).unwrap();
        let parsed: Occurrence = serde_json::from_str(&json).unwrap();

        assert_eq!(parsed.source, "tapio");
        assert_eq!(parsed.occurrence_type, "kernel.container.oom_kill");
        assert_eq!(parsed.severity, Severity::Critical);
        assert_eq!(parsed.outcome, Outcome::Failure);
        assert_eq!(parsed.protocol_version, "1.0");
        assert!(parsed.error.is_some());
        assert!(parsed.context.is_some());
        assert!(parsed.data.is_some());
    }

    #[test]
    fn type_field_serializes_as_type_not_occurrence_type() {
        let occ = Occurrence::new(
            "kernel.network.rst_storm",
            Severity::Error,
            Outcome::Failure,
        );
        let json = serde_json::to_string(&occ).unwrap();
        assert!(json.contains("\"type\":\"kernel.network.rst_storm\""));
        assert!(!json.contains("occurrence_type"));
    }

    #[test]
    fn outcome_includes_in_progress() {
        let occ = Occurrence::new(
            "kernel.node.cpu_stall",
            Severity::Warning,
            Outcome::InProgress,
        );
        let json = serde_json::to_string(&occ).unwrap();
        assert!(json.contains("\"in_progress\""));
    }

    #[test]
    fn severity_serializes_lowercase() {
        let json = serde_json::to_string(&Severity::Critical).unwrap();
        assert_eq!(json, "\"critical\"");
    }

    #[test]
    fn validate_catches_empty_source() {
        let mut occ = Occurrence::new("kernel.test", Severity::Info, Outcome::Success);
        occ.source = String::new();
        assert!(occ.validate().is_err());
    }

    #[test]
    fn validate_catches_bad_type_format() {
        let occ = Occurrence::new("kernel", Severity::Info, Outcome::Success);
        assert!(occ.validate().is_err());
    }

    #[test]
    fn validate_passes_for_valid_occurrence() {
        let occ = Occurrence::new(
            "kernel.container.oom_kill",
            Severity::Critical,
            Outcome::Failure,
        );
        assert!(occ.validate().is_ok());
    }

    #[test]
    fn skip_serializing_none_fields() {
        let occ = Occurrence::new("kernel.test.event", Severity::Info, Outcome::Success);
        let json = serde_json::to_string(&occ).unwrap();
        assert!(!json.contains("error"));
        assert!(!json.contains("context"));
        assert!(!json.contains("reasoning"));
        assert!(!json.contains("history"));
        assert!(!json.contains("data"));
    }

    #[test]
    fn new_at_uses_provided_timestamp() {
        // 2024-01-15T12:00:00Z = 1705320000 seconds
        let wall_ns: u64 = 1_705_320_000 * 1_000_000_000;
        let occ = Occurrence::new_at(
            "kernel.test.event",
            Severity::Info,
            Outcome::Success,
            wall_ns,
        );
        assert!(occ.timestamp.starts_with("2024-01-15"));
        assert!(occ.validate().is_ok());
    }
}
