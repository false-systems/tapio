use std::io::{self, Write};
use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

pub struct StdoutSink;

impl Sink for StdoutSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        let mut out = io::stdout().lock();
        serde_json::to_writer(&mut out, occurrence)
            .map_err(|e| SinkError::Serialization(e.to_string()))?;
        out.write_all(b"\n").map_err(SinkError::Io)?;
        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        io::stdout().flush().map_err(SinkError::Io)
    }

    fn name(&self) -> &str {
        "stdout"
    }
}
