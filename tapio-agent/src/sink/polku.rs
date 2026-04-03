use std::sync::Mutex;
use std::time::{Duration, Instant};

use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

/// Sink that batches occurrences and POSTs them to POLKU's HTTP ingest endpoint.
/// Buffers events and flushes when buffer_size is reached or flush_interval elapses.
/// On send failure, retains the buffer for retry on next flush.
/// If the buffer exceeds 10x buffer_size, drops oldest events.
pub struct PolkuSink {
    inner: Mutex<PolkuInner>,
}

struct PolkuInner {
    endpoint: String,
    buffer: Vec<Occurrence>,
    buffer_size: usize,
    flush_interval: Duration,
    last_flush: Instant,
    max_buffer: usize,
}

impl PolkuSink {
    pub fn new(endpoint: &str, buffer_size: usize, flush_interval: Duration) -> Self {
        Self {
            inner: Mutex::new(PolkuInner {
                endpoint: endpoint.to_string(),
                buffer: Vec::with_capacity(buffer_size),
                buffer_size,
                flush_interval,
                last_flush: Instant::now(),
                max_buffer: buffer_size * 10,
            }),
        }
    }
}

impl Sink for PolkuSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        let mut inner = self
            .inner
            .lock()
            .map_err(|e| SinkError::Send(e.to_string()))?;

        // Backpressure: drop oldest events if buffer exceeds max
        if inner.buffer.len() >= inner.max_buffer {
            let drain_count = inner.buffer.len() - inner.buffer_size;
            inner.buffer.drain(..drain_count);
            tracing::warn!(
                dropped = drain_count,
                "polku sink buffer overflow, dropped oldest events"
            );
        }

        inner.buffer.push(occurrence.clone());

        // Flush if buffer is full or interval elapsed
        if inner.buffer.len() >= inner.buffer_size
            || inner.last_flush.elapsed() >= inner.flush_interval
        {
            flush_inner(&mut inner);
        }

        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        let mut inner = self
            .inner
            .lock()
            .map_err(|e| SinkError::Send(e.to_string()))?;
        flush_inner(&mut inner);
        Ok(())
    }

    fn name(&self) -> &str {
        "polku"
    }
}

fn flush_inner(inner: &mut PolkuInner) {
    if inner.buffer.is_empty() {
        return;
    }

    let payload = match serde_json::to_vec(&inner.buffer) {
        Ok(p) => p,
        Err(e) => {
            tracing::warn!(error = %e, "polku sink: failed to serialize batch");
            return;
        }
    };

    // Synchronous HTTP POST — the Sink trait is sync.
    // Uses a minimal TCP connection to avoid pulling in reqwest/hyper.
    match post_json(&inner.endpoint, &payload) {
        Ok(()) => {
            tracing::debug!(count = inner.buffer.len(), "polku sink: batch sent");
            inner.buffer.clear();
            inner.last_flush = Instant::now();
        }
        Err(e) => {
            // Retain buffer for retry on next flush
            tracing::warn!(error = %e, buffered = inner.buffer.len(), "polku sink: send failed, will retry");
        }
    }
}

/// Minimal HTTP POST using std::net::TcpStream.
/// Avoids adding reqwest/hyper as dependencies.
fn post_json(endpoint: &str, body: &[u8]) -> Result<(), String> {
    use std::io::{Read, Write};
    use std::net::TcpStream;

    // Parse endpoint: http://host:port/path
    let url = endpoint
        .strip_prefix("http://")
        .ok_or_else(|| format!("endpoint must start with http://: {endpoint}"))?;

    let (host_port, path) = match url.find('/') {
        Some(i) => (&url[..i], &url[i..]),
        None => (url, "/v1/occurrences"),
    };

    let mut stream =
        TcpStream::connect(host_port).map_err(|e| format!("connect to {host_port}: {e}"))?;
    stream.set_write_timeout(Some(Duration::from_secs(5))).ok();
    stream.set_read_timeout(Some(Duration::from_secs(5))).ok();

    let request = format!(
        "POST {path} HTTP/1.1\r\n\
         Host: {host_port}\r\n\
         Content-Type: application/json\r\n\
         Content-Length: {}\r\n\
         Connection: close\r\n\
         \r\n",
        body.len()
    );

    stream
        .write_all(request.as_bytes())
        .map_err(|e| format!("write request: {e}"))?;
    stream
        .write_all(body)
        .map_err(|e| format!("write body: {e}"))?;

    // Read response status line
    let mut response = [0u8; 256];
    let n = stream
        .read(&mut response)
        .map_err(|e| format!("read response: {e}"))?;
    let status_line = String::from_utf8_lossy(&response[..n]);

    if !status_line.starts_with("HTTP/1.1 2") && !status_line.starts_with("HTTP/1.0 2") {
        return Err(format!(
            "POLKU returned non-2xx: {}",
            status_line.lines().next().unwrap_or("empty")
        ));
    }

    Ok(())
}
