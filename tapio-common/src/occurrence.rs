/// FALSE Protocol Occurrence builder for TAPIO.
/// TAPIO provides context, not reasoning.
use chrono::Utc;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Occurrence {
    pub id: String,
    pub timestamp: String,
    pub source: String,
    pub occurrence_type: String,
    pub severity: Severity,
    pub outcome: Outcome,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<OccurrenceError>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context: Option<Context>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Severity {
    Debug,
    Info,
    Warning,
    Error,
    Critical,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Outcome {
    Success,
    Failure,
    Timeout,
    Unknown,
}

/// Error block — factual, no reasoning.
/// TAPIO fills code + message. AI agents fill the rest.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OccurrenceError {
    pub code: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Context {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cluster: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub node: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    #[serde(default)]
    pub entities: Vec<Entity>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Entity {
    pub kind: String,
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
}

impl Occurrence {
    pub fn new(occurrence_type: &str, severity: Severity, outcome: Outcome) -> Self {
        Self {
            id: ulid::Ulid::new().to_string(),
            timestamp: Utc::now().to_rfc3339(),
            source: "tapio".to_string(),
            occurrence_type: occurrence_type.to_string(),
            severity,
            outcome,
            error: None,
            context: None,
            data: None,
        }
    }

    pub fn with_error(mut self, code: &str, message: &str) -> Self {
        self.error = Some(OccurrenceError {
            code: code.to_string(),
            message: message.to_string(),
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
}
