use crate::occurrence::Occurrence;

/// Sink contract — where anomaly events go.
/// Implementations live in tapio-agent (stdout, file, http, otlp).
/// This trait is sync — async wrappers added in the agent crate.
pub trait Sink: Send + Sync {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError>;
    fn flush(&self) -> Result<(), SinkError>;
    fn name(&self) -> &str;
}

#[derive(Debug, thiserror::Error)]
pub enum SinkError {
    #[error("connection failed: {0}")]
    Connection(String),
    #[error("send failed: {0}")]
    Send(String),
    #[error("serialization failed: {0}")]
    Serialization(String),
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
}
