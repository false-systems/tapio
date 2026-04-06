use std::sync::Mutex;
use std::time::{Duration, Instant};

use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

/// Sink that batches occurrences and POSTs them to POLKU's HTTP ingest endpoint.
/// Buffers events and flushes when buffer_size is reached or flush_interval elapses.
/// On send failure, batch is lost (logged). Exponential backoff prevents tight retry loops.
/// If the buffer exceeds 10x buffer_size, drops oldest events.
pub struct PolkuSink {
    endpoint: String,
    buffer_size: usize,
    inner: Mutex<PolkuInner>,
}

struct PolkuInner {
    buffer: Vec<Occurrence>,
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
            endpoint: endpoint.to_string(),
            buffer_size,
            inner: Mutex::new(PolkuInner {
                buffer: Vec::with_capacity(buffer_size),
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
        let batch = {
            let mut inner = self
                .inner
                .lock()
                .map_err(|e| SinkError::Send(e.to_string()))?;

            // Backpressure: drop oldest events if buffer exceeds max
            if inner.buffer.len() >= inner.max_buffer {
                let drain_count = inner.buffer.len() - self.buffer_size;
                inner.buffer.drain(..drain_count);
                tracing::warn!(
                    dropped = drain_count,
                    "polku sink buffer overflow, dropped oldest events"
                );
            }

            inner.buffer.push(occurrence.clone());

            // Check if we should flush (respecting backoff)
            let now = Instant::now();
            let should_flush = now >= inner.next_retry
                && (inner.buffer.len() >= self.buffer_size
                    || inner.last_flush.elapsed() >= inner.flush_interval);

            if should_flush {
                // Take the buffer out — lock released when this block ends
                let batch: Vec<Occurrence> = inner.buffer.drain(..).collect();
                inner.last_flush = now;
                Some(batch)
            } else {
                None
            }
        }; // lock released here — before any I/O

        if let Some(batch) = batch {
            self.post_batch(batch);
        }

        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        let batch = {
            let mut inner = self
                .inner
                .lock()
                .map_err(|e| SinkError::Send(e.to_string()))?;
            if inner.buffer.is_empty() {
                return Ok(());
            }
            let batch: Vec<Occurrence> = inner.buffer.drain(..).collect();
            inner.last_flush = Instant::now();
            batch
        }; // lock released here

        self.post_batch(batch);
        Ok(())
    }

    fn name(&self) -> &str {
        "polku"
    }
}

impl PolkuSink {
    /// POST batch to endpoint. On failure, batch is lost (logged + backoff updated).
    fn post_batch(&self, batch: Vec<Occurrence>) {
        if batch.is_empty() {
            return;
        }

        let payload = match serde_json::to_vec(&batch) {
            Ok(p) => p,
            Err(e) => {
                tracing::warn!(error = %e, "polku sink: failed to serialize batch");
                return;
            }
        };

        match post_json(&self.endpoint, &payload) {
            Ok(()) => {
                tracing::debug!(count = batch.len(), "polku sink: batch sent");
                if let Ok(mut inner) = self.inner.lock() {
                    inner.backoff = INITIAL_BACKOFF;
                }
            }
            Err(e) => {
                if let Ok(mut inner) = self.inner.lock() {
                    let now = Instant::now();
                    inner.next_retry = now + inner.backoff;
                    inner.backoff = (inner.backoff * 2).min(MAX_BACKOFF);
                }
                tracing::warn!(
                    error = %e,
                    dropped = batch.len(),
                    "polku sink: send failed, batch dropped"
                );
            }
        }
    }
}

/// Minimal HTTP POST using std::net::TcpStream.
fn post_json(endpoint: &str, body: &[u8]) -> Result<(), String> {
    use std::io::{Read, Write};
    use std::net::TcpStream;

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
