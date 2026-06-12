use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc::{Receiver, SyncSender, TrySendError, sync_channel};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use tapio_common::occurrence::{Occurrence, Severity};
use tapio_common::sink::{Sink, SinkError};
use tapio_wire::{EventBatchRequest, EventBatchResponse, EventSeverity, WIRE_VERSION, WireEvent};

use crate::controller::{ControllerConfig, ControllerState};

const DEFAULT_QUEUE_CAPACITY: usize = 4096;
const DEFAULT_MAX_REQUEST_BYTES: usize = 256 * 1024;
const MAX_EVENTS_RESPONSE_BYTES: usize = 16 * 1024;
const FLUSH_DEADLINE: Duration = Duration::from_secs(2);
const INITIAL_RETRY_BACKOFF: Duration = Duration::from_millis(100);
const MAX_RETRY_BACKOFF: Duration = Duration::from_secs(2);
const MAX_RETRIES: usize = 3;

pub struct ControllerSink {
    tx: SyncSender<WorkerMessage>,
    metrics: crate::metrics::TapioMetrics,
    worker: Mutex<Option<thread::JoinHandle<()>>>,
}

enum WorkerMessage {
    Event(WireEvent),
    Flush(SyncSender<Result<(), String>>),
    Shutdown,
}

struct Worker {
    controller: ControllerConfig,
    state: Arc<ControllerState>,
    metrics: crate::metrics::TapioMetrics,
    rx: Receiver<WorkerMessage>,
    sequence: AtomicU64,
    max_request_bytes: usize,
    buffer: Vec<WireEvent>,
}

impl ControllerSink {
    pub fn new(
        controller: ControllerConfig,
        state: Arc<ControllerState>,
        metrics: crate::metrics::TapioMetrics,
    ) -> Self {
        Self::with_limits(
            controller,
            state,
            metrics,
            DEFAULT_QUEUE_CAPACITY,
            DEFAULT_MAX_REQUEST_BYTES,
        )
    }

    fn with_limits(
        controller: ControllerConfig,
        state: Arc<ControllerState>,
        metrics: crate::metrics::TapioMetrics,
        queue_capacity: usize,
        max_request_bytes: usize,
    ) -> Self {
        let (tx, rx) = sync_channel(queue_capacity);
        let worker_metrics = metrics.clone();
        let worker_state = state.clone();
        let worker_controller = controller.clone();
        let handle = thread::spawn(move || {
            Worker {
                controller: worker_controller,
                state: worker_state,
                metrics: worker_metrics,
                rx,
                sequence: AtomicU64::new(1),
                max_request_bytes,
                buffer: Vec::new(),
            }
            .run();
        });
        Self {
            tx,
            metrics,
            worker: Mutex::new(Some(handle)),
        }
    }
}

impl Sink for ControllerSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        let event = occurrence_to_wire_event(occurrence, &self.metrics)?;
        try_enqueue_controller_event(&self.tx, event, &self.metrics);
        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        let (ack_tx, ack_rx) = sync_channel(1);
        let started = Instant::now();
        loop {
            match self.tx.try_send(WorkerMessage::Flush(ack_tx.clone())) {
                Ok(()) => break,
                Err(TrySendError::Full(_)) if started.elapsed() < FLUSH_DEADLINE => {
                    thread::sleep(Duration::from_millis(10));
                }
                Err(TrySendError::Full(_)) => {
                    return Err(SinkError::Send("controller sink flush deadline".into()));
                }
                Err(TrySendError::Disconnected(_)) => {
                    return Err(SinkError::Send("controller sink worker stopped".into()));
                }
            }
        }
        match ack_rx.recv_timeout(FLUSH_DEADLINE) {
            Ok(Ok(())) => Ok(()),
            Ok(Err(error)) => Err(SinkError::Send(error)),
            Err(error) => Err(SinkError::Send(format!("controller sink flush: {error}"))),
        }
    }

    fn name(&self) -> &str {
        "controller"
    }
}

impl Drop for ControllerSink {
    fn drop(&mut self) {
        let started = Instant::now();
        loop {
            match self.tx.try_send(WorkerMessage::Shutdown) {
                Ok(()) | Err(TrySendError::Disconnected(_)) => break,
                Err(TrySendError::Full(_)) if started.elapsed() < FLUSH_DEADLINE => {
                    thread::sleep(Duration::from_millis(10));
                }
                Err(TrySendError::Full(_)) => break,
            }
        }
        if let Ok(mut worker) = self.worker.lock()
            && let Some(handle) = worker.take()
        {
            let _ = handle.join();
        }
    }
}

impl Worker {
    fn run(&mut self) {
        loop {
            let (send_interval_ms, max_batch_events) = self.state.batching();
            let flush_interval = Duration::from_millis(send_interval_ms);
            match self.rx.recv_timeout(flush_interval) {
                Ok(WorkerMessage::Event(event)) => {
                    self.buffer.push(event);
                    if self.buffer.len() >= max_batch_events as usize {
                        let _ = self.flush_buffer(max_batch_events as usize);
                    }
                }
                Ok(WorkerMessage::Flush(ack)) => {
                    let result = self.flush_buffer(max_batch_events as usize);
                    let _ = ack.send(result);
                }
                Ok(WorkerMessage::Shutdown) => {
                    self.flush_buffer(max_batch_events as usize).ok();
                    break;
                }
                Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                    self.flush_buffer(max_batch_events as usize).ok();
                }
                Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                    self.flush_buffer(max_batch_events as usize).ok();
                    break;
                }
            }
        }
    }

    fn flush_buffer(&mut self, max_batch_events: usize) -> Result<(), String> {
        if self.buffer.is_empty() {
            return Ok(());
        }
        let events = std::mem::take(&mut self.buffer);
        let batches = split_events_by_size(events, max_batch_events, self.max_request_bytes);
        let mut last_error = None;
        for batch in batches {
            if let Err(error) = self.send_event_batch(batch) {
                last_error = Some(error);
            }
        }
        match last_error {
            Some(error) => Err(error),
            None => Ok(()),
        }
    }

    fn send_event_batch(&self, events: Vec<WireEvent>) -> Result<(), String> {
        let sequence = self.sequence.fetch_add(1, Ordering::Relaxed);
        let event_count = events.len() as u64;
        let request = EventBatchRequest {
            wire_version: WIRE_VERSION.into(),
            agent_id: self.controller.agent_id.clone(),
            node_name: self.controller.node_name.clone(),
            sequence,
            sent_at_unix_nanos: unix_now_nanos(),
            events,
        };
        request
            .validate(usize::MAX)
            .map_err(|error| error.to_string())?;
        let body =
            serde_json::to_vec(&request).map_err(|e| format!("encode EventBatchRequest: {e}"))?;

        let mut backoff = INITIAL_RETRY_BACKOFF;
        for attempt in 0..=MAX_RETRIES {
            match crate::httpc::post_json_response(
                &events_url(&self.controller),
                &body,
                MAX_EVENTS_RESPONSE_BYTES,
            ) {
                Ok(response) if matches!(response.status, 200..=299) => {
                    let response: EventBatchResponse = serde_json::from_slice(&response.body)
                        .map_err(|e| format!("decode EventBatchResponse: {e}"))?;
                    response.validate().map_err(|e| e.to_string())?;
                    tracing::debug!(
                        sequence,
                        accepted = response.accepted,
                        rejected = response.rejected,
                        "controller events response"
                    );
                    if response.rejected > 0 {
                        tracing::warn!(
                            sequence,
                            accepted = response.accepted,
                            rejected = response.rejected,
                            "controller rejected events"
                        );
                    }
                    return Ok(());
                }
                Ok(response) => {
                    if attempt == MAX_RETRIES {
                        return self
                            .drop_failed_batch(event_count, format!("HTTP {}", response.status));
                    }
                }
                Err(error) => {
                    if attempt == MAX_RETRIES {
                        return self.drop_failed_batch(event_count, error);
                    }
                }
            }
            thread::sleep(backoff);
            backoff = (backoff * 2).min(MAX_RETRY_BACKOFF);
        }

        self.drop_failed_batch(event_count, "retry budget exhausted".into())
    }

    fn drop_failed_batch(&self, event_count: u64, error: String) -> Result<(), String> {
        self.metrics
            .controller_send_failures_total
            .with_label_values(&["events"])
            .inc();
        self.metrics
            .sink_drops_total
            .with_label_values(&["controller", "send_failed"])
            .inc_by(event_count);
        tracing::warn!(
            error = %error,
            dropped = event_count,
            "controller sink: send failed, batch dropped"
        );
        Err(error)
    }
}

fn try_enqueue_controller_event(
    tx: &SyncSender<WorkerMessage>,
    event: WireEvent,
    metrics: &crate::metrics::TapioMetrics,
) {
    match tx.try_send(WorkerMessage::Event(event)) {
        Ok(()) => {}
        Err(TrySendError::Full(_)) => {
            metrics
                .sink_drops_total
                .with_label_values(&["controller", "queue_full"])
                .inc();
            tracing::warn!("controller sink queue full, dropped event");
        }
        Err(TrySendError::Disconnected(_)) => {
            metrics
                .sink_drops_total
                .with_label_values(&["controller", "worker_stopped"])
                .inc();
            tracing::warn!("controller sink worker stopped, dropped event");
        }
    }
}

fn occurrence_to_wire_event(
    occurrence: &Occurrence,
    metrics: &crate::metrics::TapioMetrics,
) -> Result<WireEvent, SinkError> {
    let observer = observer_from_type(&occurrence.occurrence_type)?;
    Ok(WireEvent {
        event_type: occurrence.occurrence_type.clone(),
        timestamp_unix_nanos: timestamp_to_unix_nanos(&occurrence.timestamp),
        observer,
        severity: map_severity(&occurrence.severity, metrics),
        facts: occurrence
            .data
            .clone()
            .unwrap_or_else(|| serde_json::Value::Object(serde_json::Map::new())),
    })
}

fn observer_from_type(event_type: &str) -> Result<String, SinkError> {
    let mut parts = event_type.split('.');
    match (parts.next(), parts.next(), parts.next(), parts.next()) {
        (Some("kernel"), Some(observer), Some(_), None) if !observer.is_empty() => {
            Ok(observer.to_string())
        }
        _ => Err(SinkError::Serialization(format!(
            "invalid kernel event type: {event_type}"
        ))),
    }
}

fn map_severity(severity: &Severity, _metrics: &crate::metrics::TapioMetrics) -> EventSeverity {
    match severity {
        Severity::Debug => EventSeverity::Debug,
        Severity::Info => EventSeverity::Info,
        Severity::Warning => EventSeverity::Warning,
        Severity::Error => EventSeverity::Error,
        Severity::Critical => EventSeverity::Critical,
    }
}

#[cfg(test)]
fn map_severity_str(value: &str, metrics: &crate::metrics::TapioMetrics) -> EventSeverity {
    match value {
        "debug" => EventSeverity::Debug,
        "info" => EventSeverity::Info,
        "warning" => EventSeverity::Warning,
        "error" => EventSeverity::Error,
        "critical" => EventSeverity::Critical,
        _ => {
            metrics
                .malformed_events_total
                .with_label_values(&["controller"])
                .inc();
            EventSeverity::Info
        }
    }
}

fn split_events_by_size(
    events: Vec<WireEvent>,
    max_batch_events: usize,
    max_request_bytes: usize,
) -> Vec<Vec<WireEvent>> {
    let mut batches = Vec::new();
    let mut current = Vec::new();
    for event in events {
        let mut candidate = current.clone();
        candidate.push(event.clone());
        if !current.is_empty()
            && (candidate.len() > max_batch_events
                || serialized_batch_len(&candidate) > max_request_bytes)
        {
            batches.push(std::mem::take(&mut current));
        }
        current.push(event);
    }
    if !current.is_empty() {
        batches.push(current);
    }
    batches
}

fn serialized_batch_len(events: &[WireEvent]) -> usize {
    let request = EventBatchRequest {
        wire_version: WIRE_VERSION.into(),
        agent_id: "agent".into(),
        node_name: "node".into(),
        sequence: 1,
        sent_at_unix_nanos: 0,
        events: events.to_vec(),
    };
    serde_json::to_vec(&request)
        .map(|bytes| bytes.len())
        .unwrap_or(usize::MAX)
}

fn timestamp_to_unix_nanos(timestamp: &str) -> u64 {
    let Some((date, time)) = timestamp.split_once('T') else {
        return 0;
    };
    let Some((year, month, day)) = parse_date(date) else {
        return 0;
    };
    let time = time.strip_suffix("+00:00").unwrap_or(time);
    let (clock, fraction) = time
        .split_once('.')
        .map_or((time, ""), |(clock, fraction)| (clock, fraction));
    let Some((hour, minute, second)) = parse_time(clock) else {
        return 0;
    };
    let days = days_from_civil(year, month, day);
    if days < 0 {
        return 0;
    }
    let nanos = fraction_nanos(fraction);
    (days as u64)
        .saturating_mul(86_400)
        .saturating_add(hour * 3_600 + minute * 60 + second)
        .saturating_mul(1_000_000_000)
        .saturating_add(nanos)
}

fn parse_date(date: &str) -> Option<(i64, i64, i64)> {
    let mut parts = date.split('-');
    let year = parts.next()?.parse().ok()?;
    let month = parts.next()?.parse().ok()?;
    let day = parts.next()?.parse().ok()?;
    Some((year, month, day))
}

fn parse_time(time: &str) -> Option<(u64, u64, u64)> {
    let mut parts = time.split(':');
    let hour = parts.next()?.parse().ok()?;
    let minute = parts.next()?.parse().ok()?;
    let second = parts.next()?.parse().ok()?;
    Some((hour, minute, second))
}

fn fraction_nanos(fraction: &str) -> u64 {
    let mut value = 0u64;
    let mut digits = 0;
    for byte in fraction.bytes().take(9) {
        if !byte.is_ascii_digit() {
            break;
        }
        value = value
            .saturating_mul(10)
            .saturating_add(u64::from(byte - b'0'));
        digits += 1;
    }
    for _ in digits..9 {
        value = value.saturating_mul(10);
    }
    value
}

fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let year = year - i64::from(month <= 2);
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let yoe = year - era * 400;
    let month = month + if month > 2 { -3 } else { 9 };
    let doy = (153 * month + 2) / 5 + day - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

fn unix_now_nanos() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| {
            duration
                .as_secs()
                .saturating_mul(1_000_000_000)
                .saturating_add(u64::from(duration.subsec_nanos()))
        })
        .unwrap_or(0)
}

fn events_url(controller: &ControllerConfig) -> String {
    format!("{}/v1/events", controller.endpoint.trim_end_matches('/'))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use tapio_common::occurrence::{Outcome, Severity};
    use tapio_wire::EventBatchRequest;

    fn occurrence() -> Occurrence {
        Occurrence::new_at(
            "kernel.network.connection_refused",
            Severity::Warning,
            Outcome::Failure,
            1_700_000_000_123_456_789,
        )
        .with_data(serde_json::json!({"dst_port": 5432}))
    }

    fn controller(endpoint: String) -> ControllerConfig {
        ControllerConfig {
            endpoint,
            agent_id: "node/worker-1".into(),
            node_name: "worker-1".into(),
            poll_interval: Duration::from_secs(30),
            heartbeat_interval: Duration::from_secs(30),
        }
    }

    #[test]
    fn occurrence_maps_to_wire_event() {
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let event = occurrence_to_wire_event(&occurrence(), &metrics).expect("wire event");
        let json = serde_json::to_string(&event).expect("serialize wire event");
        let parsed: WireEvent = serde_json::from_str(&json).expect("deserialize wire event");

        assert_eq!(parsed.event_type, "kernel.network.connection_refused");
        assert_eq!(parsed.observer, "network");
        assert_eq!(parsed.severity, EventSeverity::Warning);
        assert_eq!(parsed.timestamp_unix_nanos, 1_700_000_000_123_456_789);
        assert_eq!(parsed.facts["dst_port"], 5432);
    }

    #[test]
    fn unknown_severity_maps_to_info_and_counts_malformed() {
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        assert_eq!(map_severity_str("notice", &metrics), EventSeverity::Info);
        assert_eq!(
            metrics
                .malformed_events_total
                .with_label_values(&["controller"])
                .value(),
            1
        );
    }

    #[test]
    fn queue_full_counts_drop() {
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let (tx, _rx) = sync_channel(1);
        let event = occurrence_to_wire_event(&occurrence(), &metrics).expect("wire event");
        try_enqueue_controller_event(&tx, event.clone(), &metrics);
        try_enqueue_controller_event(&tx, event, &metrics);

        assert_eq!(
            metrics
                .sink_drops_total
                .with_label_values(&["controller", "queue_full"])
                .value(),
            1
        );
    }

    #[test]
    fn batch_splitting_respects_event_limit_and_size() {
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let event = occurrence_to_wire_event(&occurrence(), &metrics).expect("wire event");
        let batches = split_events_by_size(vec![event.clone(), event.clone(), event], 2, 800);
        assert!(batches.iter().all(|batch| batch.len() <= 2));
        assert!(batches.len() >= 2);
    }

    #[test]
    fn event_send_non_2xx_counts_failure_and_drop() {
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(e)
                if e.kind() == std::io::ErrorKind::PermissionDenied
                    && std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_none() =>
            {
                eprintln!("SKIP controller sink loopback test: loopback bind not permitted ({e})");
                return;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener.local_addr().expect("local addr");
        let handle = std::thread::spawn(move || {
            for _ in 0..=MAX_RETRIES {
                let (mut stream, _) = listener.accept().expect("accept request");
                let mut buf = [0u8; 4096];
                let _ = stream.read(&mut buf).expect("read request");
                stream
                    .write_all(b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n")
                    .expect("write response");
            }
        });
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let state = ControllerState::new("1");
        let worker = Worker {
            controller: controller(format!("http://{addr}")),
            state,
            metrics: metrics.clone(),
            rx: sync_channel(1).1,
            sequence: AtomicU64::new(1),
            max_request_bytes: DEFAULT_MAX_REQUEST_BYTES,
            buffer: Vec::new(),
        };
        let event = occurrence_to_wire_event(&occurrence(), &metrics).expect("wire event");
        assert!(worker.send_event_batch(vec![event]).is_err());
        handle.join().expect("server");
        assert_eq!(
            metrics
                .controller_send_failures_total
                .with_label_values(&["events"])
                .value(),
            1
        );
        assert_eq!(
            metrics
                .sink_drops_total
                .with_label_values(&["controller", "send_failed"])
                .value(),
            1
        );
    }

    #[test]
    fn sequence_is_monotonic_across_failed_sends() {
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(e)
                if e.kind() == std::io::ErrorKind::PermissionDenied
                    && std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_none() =>
            {
                eprintln!("SKIP controller sink loopback test: loopback bind not permitted ({e})");
                return;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener.local_addr().expect("local addr");
        let handle = std::thread::spawn(move || {
            let mut sequences = Vec::new();
            for idx in 0..=(MAX_RETRIES + 1) {
                let (mut stream, _) = listener.accept().expect("accept request");
                let request = read_http_request(&mut stream);
                let request = String::from_utf8_lossy(&request);
                let body = request.split("\r\n\r\n").nth(1).expect("request body");
                let batch: EventBatchRequest =
                    serde_json::from_str(body).expect("valid event batch");
                sequences.push(batch.sequence);
                if idx <= MAX_RETRIES {
                    stream
                        .write_all(
                            b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n",
                        )
                        .expect("write response");
                } else {
                    stream
                        .write_all(
                        b"HTTP/1.1 200 OK\r\nContent-Length: 78\r\n\r\n{\"wire_version\":\"tapio-wire/v1\",\"accepted\":1,\"rejected\":0,\"next_config_version\":\"1\"}",
                    )
                        .expect("write response");
                }
            }
            sequences
        });
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let worker = Worker {
            controller: controller(format!("http://{addr}")),
            state: ControllerState::new("1"),
            metrics: metrics.clone(),
            rx: sync_channel(1).1,
            sequence: AtomicU64::new(1),
            max_request_bytes: DEFAULT_MAX_REQUEST_BYTES,
            buffer: Vec::new(),
        };
        let event = occurrence_to_wire_event(&occurrence(), &metrics).expect("wire event");
        assert!(worker.send_event_batch(vec![event.clone()]).is_err());
        worker.send_event_batch(vec![event]).expect("second batch");
        let sequences = handle.join().expect("server");
        assert_eq!(sequences, vec![1, 1, 1, 1, 2]);
    }

    fn read_http_request(stream: &mut std::net::TcpStream) -> Vec<u8> {
        let mut request = Vec::new();
        let mut chunk = [0u8; 1024];
        loop {
            let n = stream.read(&mut chunk).expect("read request");
            if n == 0 {
                break;
            }
            request.extend_from_slice(&chunk[..n]);
            let Some(split) = request.windows(4).position(|window| window == b"\r\n\r\n") else {
                continue;
            };
            let headers = String::from_utf8_lossy(&request[..split]);
            let content_length = headers
                .lines()
                .find_map(|line| line.strip_prefix("Content-Length: "))
                .and_then(|value| value.parse::<usize>().ok())
                .unwrap_or(0);
            if request.len() >= split + 4 + content_length {
                break;
            }
        }
        request
    }
}
