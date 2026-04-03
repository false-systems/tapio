use std::sync::Mutex;
use std::time::{Duration, Instant};

use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

/// Sink that batches occurrences and POSTs them to POLKU's HTTP ingest endpoint.
/// Buffers events and flushes when buffer_size is reached or flush_interval elapses.
/// On send failure, retains the buffer for retry with exponential backoff.
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
    next_retry: Instant,
    backoff: Duration,
    max_buffer: usize,
}

const INITIAL_BACKOFF: Duration = Duration::from_secs(1);
const MAX_BACKOFF: Duration = Duration::from_secs(60);

impl PolkuSink {
    pub fn new(endpoint: &str, buffer_size: usize, flush_interval: Duration) -> Self {
        let now = Instant::now();
        Self {
            inner: Mutex::new(PolkuInner {
                endpoint: endpoint.to_string(),
                buffer: Vec::with_capacity(buffer_size),
                buffer_size,
                flush_interval,
                last_flush: now,
                next_retry: now,
                backoff: INITIAL_BACKOFF,
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

        // Flush if buffer is full or interval elapsed (respecting backoff)
        let now = Instant::now();
        if now >= inner.next_retry
            && (inner.buffer.len() >= inner.buffer_size
                || inner.last_flush.elapsed() >= inner.flush_interval)
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

    // Release mutex before blocking I/O by extracting what we need
    let endpoint = inner.endpoint.clone();

    match post_json(&endpoint, &payload) {
        Ok(()) => {
            tracing::debug!(count = inner.buffer.len(), "polku sink: batch sent");
            inner.buffer.clear();
            inner.last_flush = Instant::now();
            inner.backoff = INITIAL_BACKOFF;
        }
        Err(e) => {
            // Exponential backoff: don't retry on every send() during outages
            let now = Instant::now();
            inner.next_retry = now + inner.backoff;
            inner.backoff = (inner.backoff * 2).min(MAX_BACKOFF);
            tracing::warn!(
                error = %e,
                buffered = inner.buffer.len(),
                retry_in_secs = inner.backoff.as_secs(),
                "polku sink: send failed, backing off"
            );
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
        "POST {path} HTTP/1.1\r\nHost: {host_port}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
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
