use std::sync::Mutex;
use std::time::{Duration, Instant};

use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

/// Sink that batches anomaly events and POSTs them as JSON to an HTTP endpoint.
/// Buffers events and flushes when buffer_size is reached or flush_interval elapses.
/// On send failure, the batch is lost (logged). Exponential backoff prevents tight retry loops.
/// If the buffer exceeds 10x buffer_size, drops oldest events.
pub struct HttpSink {
    endpoint: String,
    buffer_size: usize,
    inner: Mutex<HttpInner>,
}

struct HttpInner {
    buffer: Vec<Occurrence>,
    flush_interval: Duration,
    last_flush: Instant,
    next_retry: Instant,
    backoff: Duration,
    max_buffer: usize,
}

const INITIAL_BACKOFF: Duration = Duration::from_secs(1);
const MAX_BACKOFF: Duration = Duration::from_secs(60);

impl HttpSink {
    pub fn new(endpoint: &str, buffer_size: usize, flush_interval: Duration) -> Self {
        let now = Instant::now();
        Self {
            endpoint: endpoint.to_string(),
            buffer_size,
            inner: Mutex::new(HttpInner {
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

impl Sink for HttpSink {
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
                    "http sink buffer overflow, dropped oldest events"
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
            self.post_batch(batch)?;
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

        self.post_batch(batch)
    }

    fn name(&self) -> &str {
        "http"
    }
}

impl HttpSink {
    /// POST batch to endpoint. On failure, batch is lost (logged + backoff updated).
    fn post_batch(&self, batch: Vec<Occurrence>) -> Result<(), SinkError> {
        if batch.is_empty() {
            return Ok(());
        }

        let payload = match serde_json::to_vec(&batch) {
            Ok(p) => p,
            Err(e) => {
                tracing::warn!(error = %e, "http sink: failed to serialize batch");
                return Err(SinkError::Serialization(e.to_string()));
            }
        };

        match crate::httpc::post_json(&self.endpoint, &payload) {
            Ok(()) => {
                tracing::debug!(count = batch.len(), "http sink: batch sent");
                if let Ok(mut inner) = self.inner.lock() {
                    inner.backoff = INITIAL_BACKOFF;
                }
                Ok(())
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
                    "http sink: send failed, batch dropped"
                );
                Err(SinkError::Send(format!(
                    "http sink: send failed, dropped {} events: {e}",
                    batch.len()
                )))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::httpc::post_json;
    use tapio_common::occurrence::{Outcome, Severity};

    fn occurrence() -> Occurrence {
        Occurrence::new(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
        )
    }

    #[test]
    fn name_is_stable() {
        let sink = HttpSink::new("http://localhost:8765", 100, Duration::from_secs(1));
        assert_eq!(sink.name(), "http");
    }

    #[test]
    fn buffers_below_batch_size_without_sending() {
        let sink = HttpSink::new("http://127.0.0.1:1", 100, Duration::from_secs(3600));
        let occ = occurrence();
        // Below batch size and inside flush interval — should buffer, never attempt I/O.
        assert!(sink.send(&occ).is_ok());
    }

    #[test]
    fn failed_send_sets_backoff_and_drops_batch() {
        let sink = HttpSink::new("http://127.0.0.1:1", 1, Duration::from_secs(3600));
        let occ = occurrence();

        assert!(sink.send(&occ).is_err());

        let inner = sink.inner.lock().unwrap();
        assert_eq!(inner.buffer.len(), 0);
        assert!(inner.next_retry > Instant::now());
        assert_eq!(inner.backoff, INITIAL_BACKOFF * 2);
    }

    #[test]
    fn backoff_prevents_immediate_retry_and_keeps_buffered_events() {
        let sink = HttpSink::new("http://127.0.0.1:1", 1, Duration::from_secs(3600));
        let occ = occurrence();

        assert!(sink.send(&occ).is_err());
        assert!(sink.send(&occ).is_ok());

        let inner = sink.inner.lock().unwrap();
        assert_eq!(inner.buffer.len(), 1);
        assert!(inner.next_retry > Instant::now());
    }

    #[test]
    fn buffer_overflow_drops_oldest_while_in_backoff() {
        let sink = HttpSink::new("http://127.0.0.1:1", 1, Duration::from_secs(3600));
        let occ = occurrence();

        assert!(sink.send(&occ).is_err());
        for _ in 0..12 {
            assert!(sink.send(&occ).is_ok());
        }

        let inner = sink.inner.lock().unwrap();
        assert!(inner.buffer.len() <= inner.max_buffer);
        assert!(inner.buffer.len() < 12);
    }

    #[test]
    fn post_json_accepts_http_endpoint() {
        use std::io::{Read, Write};
        use std::net::TcpListener;

        // This test needs real loopback networking. Some restricted CI
        // sandboxes (seccomp / no-new-privileges / no loopback) reject the
        // bind with EPERM. That is a host limitation, not a code defect, so we
        // skip loudly rather than fail — UNLESS TAPIO_LEAN_REQUIRE_NET is set,
        // which forces the test to run (and fail) so strict CI cannot pass
        // while silently skipping coverage.
        let require_net = std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_some();
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(e) if e.kind() == std::io::ErrorKind::PermissionDenied && !require_net => {
                eprintln!(
                    "SKIP post_json_accepts_http_endpoint: loopback bind not permitted \
                     in this host/sandbox ({e}); set TAPIO_LEAN_REQUIRE_NET=1 to require it"
                );
                return;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener.local_addr().unwrap();
        let handle = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 512];
            let n = stream.read(&mut buf).unwrap();
            let request = String::from_utf8_lossy(&buf[..n]);
            assert!(request.starts_with("POST /v1/occurrences HTTP/1.1"));
            stream
                .write_all(b"HTTP/1.1 204 No Content\r\n\r\n")
                .unwrap();
        });

        post_json(&format!("http://{addr}"), b"[]").unwrap();
        handle.join().unwrap();
    }

    #[test]
    fn post_json_rejects_https_endpoint() {
        let err = post_json("https://127.0.0.1:4318", b"[]").unwrap_err();
        assert!(err.contains("http://"));
    }
}
