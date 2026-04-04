use std::io::Write;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use prost::Message;
use tapio_common::occurrence::{Occurrence, Severity};
use tapio_common::sink::{Sink, SinkError};

// ── Minimal OTLP protobuf types (hand-written to avoid vendoring .proto files) ──
// These match opentelemetry/proto/logs/v1/logs.proto and common/v1/common.proto.

#[derive(Clone, Message)]
struct ExportLogsServiceRequest {
    #[prost(message, repeated, tag = "1")]
    resource_logs: Vec<ResourceLogs>,
}

#[derive(Clone, Message)]
struct ResourceLogs {
    #[prost(message, optional, tag = "1")]
    resource: Option<Resource>,
    #[prost(message, repeated, tag = "2")]
    scope_logs: Vec<ScopeLogs>,
}

#[derive(Clone, Message)]
struct Resource {
    #[prost(message, repeated, tag = "1")]
    attributes: Vec<KeyValue>,
}

#[derive(Clone, Message)]
struct ScopeLogs {
    #[prost(message, repeated, tag = "1")]
    log_records: Vec<LogRecord>,
}

#[derive(Clone, Message)]
struct LogRecord {
    #[prost(fixed64, tag = "1")]
    time_unix_nano: u64,
    #[prost(int32, tag = "2")]
    severity_number: i32,
    #[prost(message, optional, tag = "5")]
    body: Option<AnyValue>,
    #[prost(message, repeated, tag = "6")]
    attributes: Vec<KeyValue>,
}

#[derive(Clone, Message)]
struct AnyValue {
    #[prost(string, tag = "1")]
    string_value: String,
}

#[derive(Clone, Message)]
struct KeyValue {
    #[prost(string, tag = "1")]
    key: String,
    #[prost(message, optional, tag = "2")]
    value: Option<AnyValue>,
}

#[derive(Clone, Copy, Debug)]
#[repr(i32)]
enum SeverityNumber {
    Info = 9,
    Warn = 13,
    Error = 17,
    Fatal = 21,
}

fn severity_to_otlp(s: &Severity) -> i32 {
    match s {
        Severity::Debug | Severity::Info => SeverityNumber::Info as i32,
        Severity::Warning => SeverityNumber::Warn as i32,
        Severity::Error => SeverityNumber::Error as i32,
        Severity::Critical => SeverityNumber::Fatal as i32,
    }
}

fn kv(key: &str, val: &str) -> KeyValue {
    KeyValue {
        key: key.into(),
        value: Some(AnyValue {
            string_value: val.into(),
        }),
    }
}

// ── Occurrence → OTLP LogRecord mapping ──

fn occurrence_to_log_record(occ: &Occurrence) -> LogRecord {
    let mut attrs = vec![
        kv("occurrence.id", &occ.id),
        kv("anomaly.type", &occ.occurrence_type),
        kv("tapio.source", &occ.source),
    ];

    if let Some(ref ctx) = occ.context {
        if let Some(ref node) = ctx.node {
            attrs.push(kv("k8s.node.name", node));
        }
        if let Some(ref ns) = ctx.namespace {
            attrs.push(kv("k8s.namespace.name", ns));
        }
        for entity in &ctx.entities {
            if entity.kind == "pod"
                && let Some(ref name) = entity.name
            {
                attrs.push(kv("k8s.pod.name", name));
            }
        }
    }

    let body_text = occ
        .error
        .as_ref()
        .map(|e| e.message.clone())
        .unwrap_or_default();

    // Parse timestamp from RFC3339 to unix nanos
    let time_unix_nano = chrono::DateTime::parse_from_rfc3339(&occ.timestamp)
        .map(|dt| dt.timestamp_nanos_opt().unwrap_or(0) as u64)
        .unwrap_or(0);

    LogRecord {
        time_unix_nano,
        severity_number: severity_to_otlp(&occ.severity),
        body: Some(AnyValue {
            string_value: body_text,
        }),
        attributes: attrs,
    }
}

// ── GrafanaSink ──

pub struct GrafanaSink {
    endpoint: String,
    auth_header: Option<String>,
    batch_size: usize,
    flush_interval: Duration,
    retry_backoff_ms: Vec<u64>,
    inner: Mutex<SinkInner>,
}

struct SinkInner {
    buffer: Vec<Occurrence>,
    last_flush: Instant,
}

impl GrafanaSink {
    pub fn new(
        endpoint: &str,
        auth_header: Option<String>,
        batch_size: usize,
        flush_interval: Duration,
    ) -> Self {
        Self {
            endpoint: endpoint.trim_end_matches('/').to_string(),
            auth_header,
            batch_size,
            flush_interval,
            retry_backoff_ms: vec![500, 1000, 2000],
            inner: Mutex::new(SinkInner {
                buffer: Vec::with_capacity(batch_size),
                last_flush: Instant::now(),
            }),
        }
    }

    fn export_batch(&self, batch: Vec<Occurrence>) -> Result<(), SinkError> {
        if batch.is_empty() {
            return Ok(());
        }

        let log_records: Vec<LogRecord> = batch.iter().map(occurrence_to_log_record).collect();

        let request = ExportLogsServiceRequest {
            resource_logs: vec![ResourceLogs {
                resource: Some(Resource {
                    attributes: vec![
                        kv("service.name", "tapio"),
                        kv("service.version", env!("CARGO_PKG_VERSION")),
                    ],
                }),
                scope_logs: vec![ScopeLogs { log_records }],
            }],
        };

        let proto_bytes = request.encode_to_vec();

        // Gzip compress
        let mut encoder = flate2::write::GzEncoder::new(Vec::new(), flate2::Compression::default());
        encoder
            .write_all(&proto_bytes)
            .map_err(|e| SinkError::Serialization(e.to_string()))?;
        let compressed = encoder
            .finish()
            .map_err(|e| SinkError::Serialization(e.to_string()))?;

        let url = format!("{}/v1/logs", self.endpoint);

        for (attempt, &backoff_ms) in std::iter::once(&0u64)
            .chain(self.retry_backoff_ms.iter())
            .enumerate()
        {
            if attempt > 0 {
                std::thread::sleep(Duration::from_millis(backoff_ms));
                tracing::debug!(attempt, "retrying OTLP export");
            }

            match self.http_post(&url, &compressed) {
                Ok(status) if (200..300).contains(&status) => return Ok(()),
                Ok(status) if status == 429 || status >= 500 => {
                    tracing::warn!(status, attempt, "OTLP export retryable error");
                    continue;
                }
                Ok(status) => {
                    return Err(SinkError::Send(format!(
                        "OTLP export failed: HTTP {status}"
                    )));
                }
                Err(e) => {
                    if attempt < self.retry_backoff_ms.len() {
                        tracing::warn!(error = %e, attempt, "OTLP export connection error, retrying");
                        continue;
                    }
                    return Err(e);
                }
            }
        }

        Err(SinkError::Send("OTLP export: retries exhausted".into()))
    }

    fn http_post(&self, url: &str, body: &[u8]) -> Result<u16, SinkError> {
        use std::io::{BufRead, BufReader};
        use std::net::TcpStream;

        let url_parsed: Vec<&str> = url
            .strip_prefix("http://")
            .or_else(|| url.strip_prefix("https://"))
            .unwrap_or(url)
            .splitn(2, '/')
            .collect();

        let host = url_parsed[0];
        let path = if url_parsed.len() > 1 {
            format!("/{}", url_parsed[1])
        } else {
            "/".into()
        };

        let addr = if host.contains(':') {
            host.to_string()
        } else {
            format!("{host}:80")
        };

        let mut stream = TcpStream::connect_timeout(
            &addr
                .parse()
                .map_err(|e| SinkError::Connection(format!("invalid address {addr}: {e}")))?,
            Duration::from_secs(10),
        )
        .map_err(|e| SinkError::Connection(e.to_string()))?;

        stream.set_read_timeout(Some(Duration::from_secs(10))).ok();

        let mut request = format!(
            "POST {path} HTTP/1.1\r\n\
             Host: {host}\r\n\
             Content-Type: application/x-protobuf\r\n\
             Content-Encoding: gzip\r\n\
             Content-Length: {}\r\n",
            body.len()
        );

        if let Some(ref auth) = self.auth_header {
            request.push_str(&format!("Authorization: {auth}\r\n"));
        }

        request.push_str("Connection: close\r\n\r\n");

        stream
            .write_all(request.as_bytes())
            .map_err(|e| SinkError::Send(e.to_string()))?;
        stream
            .write_all(body)
            .map_err(|e| SinkError::Send(e.to_string()))?;
        stream.flush().map_err(|e| SinkError::Send(e.to_string()))?;

        let mut reader = BufReader::new(&stream);
        let mut status_line = String::new();
        reader
            .read_line(&mut status_line)
            .map_err(|e| SinkError::Send(e.to_string()))?;

        // Parse "HTTP/1.1 200 OK"
        let status = status_line
            .split_whitespace()
            .nth(1)
            .and_then(|s| s.parse::<u16>().ok())
            .unwrap_or(0);

        Ok(status)
    }
}

impl Sink for GrafanaSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        let mut inner = self
            .inner
            .lock()
            .map_err(|e| SinkError::Send(e.to_string()))?;

        inner.buffer.push(occurrence.clone());

        let should_flush = inner.buffer.len() >= self.batch_size
            || inner.last_flush.elapsed() >= self.flush_interval;

        if should_flush {
            let batch: Vec<Occurrence> = inner.buffer.drain(..).collect();
            inner.last_flush = Instant::now();
            drop(inner); // release lock before network I/O
            self.export_batch(batch)?;
        }

        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        let mut inner = self
            .inner
            .lock()
            .map_err(|e| SinkError::Send(e.to_string()))?;

        if inner.buffer.is_empty() {
            return Ok(());
        }

        let batch: Vec<Occurrence> = inner.buffer.drain(..).collect();
        inner.last_flush = Instant::now();
        drop(inner);
        self.export_batch(batch)
    }

    fn name(&self) -> &str {
        "grafana"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tapio_common::occurrence::Outcome;

    #[test]
    fn occurrence_maps_to_log_record() {
        let occ = Occurrence::new(
            "kernel.container.oom_kill",
            Severity::Critical,
            Outcome::Failure,
        )
        .with_error("OOM_KILL", "Container killed by OOM killer");

        let record = occurrence_to_log_record(&occ);
        assert_eq!(record.severity_number, SeverityNumber::Fatal as i32);
        assert!(record.body.is_some());
        assert!(record.attributes.iter().any(|kv| kv.key == "anomaly.type"
            && kv.value.as_ref().map(|v| v.string_value.as_str())
                == Some("kernel.container.oom_kill")));
    }

    #[test]
    fn batch_encodes_to_valid_protobuf() {
        let occ = Occurrence::new("kernel.storage.io_error", Severity::Error, Outcome::Failure);

        let records = vec![occurrence_to_log_record(&occ)];
        let request = ExportLogsServiceRequest {
            resource_logs: vec![ResourceLogs {
                resource: Some(Resource {
                    attributes: vec![kv("service.name", "tapio")],
                }),
                scope_logs: vec![ScopeLogs {
                    log_records: records,
                }],
            }],
        };

        let bytes = request.encode_to_vec();
        assert!(!bytes.is_empty());

        // Verify it round-trips
        let decoded = ExportLogsServiceRequest::decode(bytes.as_slice()).unwrap();
        assert_eq!(decoded.resource_logs.len(), 1);
        assert_eq!(decoded.resource_logs[0].scope_logs[0].log_records.len(), 1);
    }

    #[test]
    fn gzip_compression_reduces_size() {
        let occ = Occurrence::new(
            "kernel.network.retransmit_spike",
            Severity::Warning,
            Outcome::Failure,
        )
        .with_error("RETRANSMIT", "TCP retransmit spike");

        // Build a reasonably-sized batch
        let records: Vec<LogRecord> = (0..50).map(|_| occurrence_to_log_record(&occ)).collect();
        let request = ExportLogsServiceRequest {
            resource_logs: vec![ResourceLogs {
                resource: Some(Resource {
                    attributes: vec![kv("service.name", "tapio")],
                }),
                scope_logs: vec![ScopeLogs {
                    log_records: records,
                }],
            }],
        };

        let proto_bytes = request.encode_to_vec();
        let mut encoder = flate2::write::GzEncoder::new(Vec::new(), flate2::Compression::default());
        encoder.write_all(&proto_bytes).unwrap();
        let compressed = encoder.finish().unwrap();

        assert!(compressed.len() < proto_bytes.len());
    }
}
