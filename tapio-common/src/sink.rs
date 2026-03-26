use crate::occurrence::Occurrence;

/// Sink receives occurrences and sends them somewhere.
/// Implementations: stdout, file, polku (gRPC), grafana (OTLP).
#[async_trait::async_trait]
pub trait Sink: Send + Sync {
    async fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError>;
    async fn flush(&self) -> Result<(), SinkError>;
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
