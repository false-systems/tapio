use std::sync::atomic::{AtomicBool, Ordering};
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
    tx: Option<SyncSender<WorkerMessage>>,
    metrics: crate::metrics::TapioMetrics,
    shutdown: Arc<AtomicBool>,
    worker: Mutex<Option<thread::JoinHandle<()>>>,
}

enum WorkerMessage {
    Event(WireEvent),
    Flush(SyncSender<Result<(), String>>),
}

struct Worker {
    controller: ControllerConfig,
    state: Arc<ControllerState>,
    metrics: crate::metrics::TapioMetrics,
    rx: Receiver<WorkerMessage>,
    shutdown: Arc<AtomicBool>,
    sequence: u64,
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
        let shutdown = Arc::new(AtomicBool::new(false));
        let worker_shutdown = shutdown.clone();
        let handle = match thread::Builder::new()
            .name("controller-sink".into())
            .spawn(move || {
                Worker {
                    controller: worker_controller,
                    state: worker_state,
                    metrics: worker_metrics,
                    rx,
                    shutdown: worker_shutdown,
                    sequence: 1,
                    max_request_bytes,
                    buffer: Vec::new(),
                }
                .run();
            }) {
            Ok(handle) => handle,
            Err(error) => panic!("spawn controller sink worker: {error}"),
        };
        Self {
            tx: Some(tx),
            metrics,
            shutdown,
            worker: Mutex::new(Some(handle)),
        }
    }
}

impl Sink for ControllerSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        let event = occurrence_to_wire_event(occurrence)?;
        if let Some(tx) = &self.tx {
            try_enqueue_controller_event(tx, event, &self.metrics);
        }
        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        let Some(tx) = &self.tx else {
            return Err(SinkError::Send("controller sink worker stopped".into()));
        };
        let (ack_tx, ack_rx) = sync_channel(1);
        let started = Instant::now();
        loop {
            match tx.try_send(WorkerMessage::Flush(ack_tx.clone())) {
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
        self.shutdown.store(true, Ordering::Relaxed);
        self.tx.take();
        if let Ok(mut worker) = self.worker.lock()
            && let Some(handle) = worker.take()
        {
            let _ = handle.join();
        }
    }
}

impl Worker {
    fn run(&mut self) {
        let mut next_flush_at: Option<Instant> = None;
        loop {
            let (send_interval_ms, max_batch_events) = self.state.batching();
            let flush_interval = Duration::from_millis(send_interval_ms);

            if self.shutdown.load(Ordering::Relaxed) {
                self.flush_buffer_once(max_batch_events as usize).ok();
                break;
            }

            let timeout = next_flush_at
                .map(|deadline| deadline.saturating_duration_since(Instant::now()))
                .unwrap_or(flush_interval);

            match self.rx.recv_timeout(timeout) {
                Ok(WorkerMessage::Event(event)) => {
                    if self.buffer.is_empty() {
                        next_flush_at = Some(Instant::now() + flush_interval);
                    }
                    self.buffer.push(event);
                    if self.buffer.len() >= max_batch_events as usize {
                        let _ = self.flush_buffer(max_batch_events as usize, MAX_RETRIES);
                        next_flush_at = None;
                    }
                }
                Ok(WorkerMessage::Flush(ack)) => {
                    let result = self.flush_buffer(max_batch_events as usize, MAX_RETRIES);
                    next_flush_at = None;
                    let _ = ack.send(result);
                }
                Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                    self.flush_buffer(max_batch_events as usize, MAX_RETRIES)
                        .ok();
                    next_flush_at = None;
                }
                Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                    self.flush_buffer_once(max_batch_events as usize).ok();
                    break;
                }
            }
        }
    }

    fn flush_buffer_once(&mut self, max_batch_events: usize) -> Result<(), String> {
        self.flush_buffer(max_batch_events, 0)
    }

    fn flush_buffer(&mut self, max_batch_events: usize, max_retries: usize) -> Result<(), String> {
        if self.buffer.is_empty() {
            return Ok(());
        }
        let events = std::mem::take(&mut self.buffer);
        let batches = split_events_by_size(events, max_batch_events, self.max_request_bytes);
        let mut last_error = None;
        for batch in batches {
            if let Err(error) = self.send_event_batch(batch, max_retries) {
                last_error = Some(error);
            }
        }
        match last_error {
            Some(error) => Err(error),
            None => Ok(()),
        }
    }

    fn send_event_batch(
        &mut self,
        events: Vec<WireEvent>,
        max_retries: usize,
    ) -> Result<(), String> {
        let sequence = self.sequence;
        self.sequence = self.sequence.saturating_add(1);
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
        for attempt in 0..=max_retries {
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
                    if attempt == max_retries {
                        return self
                            .drop_failed_batch(event_count, format!("HTTP {}", response.status));
                    }
                }
                Err(error) => {
                    if attempt == max_retries {
                        return self.drop_failed_batch(event_count, error);
                    }
                }
            }
            if self.shutdown.load(Ordering::Relaxed) {
                return self.drop_failed_batch(event_count, "shutdown retry skipped".into());
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

fn occurrence_to_wire_event(occurrence: &Occurrence) -> Result<WireEvent, SinkError> {
    let observer = observer_from_type(&occurrence.occurrence_type)?;
    Ok(WireEvent {
        event_type: occurrence.occurrence_type.clone(),
        timestamp_unix_nanos: timestamp_to_unix_nanos(&occurrence.timestamp),
        observer,
        severity: map_severity(&occurrence.severity),
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

fn map_severity(severity: &Severity) -> EventSeverity {
    match severity {
        Severity::Debug => EventSeverity::Debug,
        Severity::Info => EventSeverity::Info,
        Severity::Warning => EventSeverity::Warning,
        Severity::Error => EventSeverity::Error,
        Severity::Critical => EventSeverity::Critical,
    }
}

fn split_events_by_size(
    events: Vec<WireEvent>,
    max_batch_events: usize,
    max_request_bytes: usize,
) -> Vec<Vec<WireEvent>> {
    let mut batches = Vec::new();
    let mut current = Vec::new();
    let mut current_len = empty_batch_len();
    for event in events {
        let event_len = serde_json::to_vec(&event)
            .map(|bytes| bytes.len())
            .unwrap_or(usize::MAX);
        let candidate_len = batch_len_with_added_event(current_len, current.len(), event_len);
        if !current.is_empty()
            && (current.len() + 1 > max_batch_events || candidate_len > max_request_bytes)
        {
            batches.push(std::mem::take(&mut current));
            current_len = empty_batch_len();
        }
        current_len = batch_len_with_added_event(current_len, current.len(), event_len);
        current.push(event);
    }
    if !current.is_empty() {
        batches.push(current);
    }
    batches
}

fn empty_batch_len() -> usize {
    let request = EventBatchRequest {
        wire_version: WIRE_VERSION.into(),
        agent_id: "agent".into(),
        node_name: "node".into(),
        sequence: 1,
        sent_at_unix_nanos: 0,
        events: Vec::new(),
    };
    serde_json::to_vec(&request)
        .map(|bytes| bytes.len())
        .unwrap_or(usize::MAX)
}

fn batch_len_with_added_event(
    current_len: usize,
    current_events: usize,
    event_len: usize,
) -> usize {
    current_len
        .saturating_sub(1)
        .saturating_add(usize::from(current_events > 0))
        .saturating_add(event_len)
        .saturating_add(1)
}

fn timestamp_to_unix_nanos(timestamp: &str) -> u64 {
    let Some((date, time)) = timestamp.split_once('T') else {
        return 0;
    };
    let Some((year, month, day)) = parse_date(date) else {
        return 0;
    };
    let time = time
        .strip_suffix("+00:00")
        .or_else(|| time.strip_suffix('Z'))
        .unwrap_or(time);
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
        let event = occurrence_to_wire_event(&occurrence()).expect("wire event");
        let json = serde_json::to_string(&event).expect("serialize wire event");
        let parsed: WireEvent = serde_json::from_str(&json).expect("deserialize wire event");

        assert_eq!(parsed.event_type, "kernel.network.connection_refused");
        assert_eq!(parsed.observer, "network");
        assert_eq!(parsed.severity, EventSeverity::Warning);
        assert_eq!(parsed.timestamp_unix_nanos, 1_700_000_000_123_456_789);
        assert_eq!(parsed.facts["dst_port"], 5432);
    }

    #[test]
    fn queue_full_counts_drop() {
        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let (tx, _rx) = sync_channel(1);
        let event = occurrence_to_wire_event(&occurrence()).expect("wire event");
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
        let event = occurrence_to_wire_event(&occurrence()).expect("wire event");
        let batches = split_events_by_size(vec![event.clone(), event.clone(), event], 2, 800);
        assert!(batches.iter().all(|batch| batch.len() <= 2));
        assert!(batches.len() >= 2);
    }

    #[test]
    fn steady_trickle_flushes_on_interval() {
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
            let (mut stream, _) = listener.accept().expect("accept request");
            let request = read_http_request(&mut stream);
            let request = String::from_utf8_lossy(&request);
            let body = request.split("\r\n\r\n").nth(1).expect("request body");
            let batch: EventBatchRequest = serde_json::from_str(body).expect("valid event batch");
            stream
                .write_all(
                    b"HTTP/1.1 200 OK\r\nContent-Length: 78\r\n\r\n{\"wire_version\":\"tapio-wire/v1\",\"accepted\":5,\"rejected\":0,\"next_config_version\":\"1\"}",
                )
                .expect("write response");
            batch.events.len()
        });

        let metrics = crate::metrics::TapioMetrics::new().expect("metrics");
        let state = ControllerState::new("1");
        state.set_batching(50, 100);
        let sink = ControllerSink::with_limits(
            controller(format!("http://{addr}")),
            state,
            metrics,
            DEFAULT_QUEUE_CAPACITY,
            DEFAULT_MAX_REQUEST_BYTES,
        );
        let occurrence = occurrence();
        for _ in 0..5 {
            sink.send(&occurrence).expect("enqueue event");
            std::thread::sleep(Duration::from_millis(5));
        }

        let event_count = handle.join().expect("server");
        assert_eq!(event_count, 5);
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
        let mut worker = Worker {
            controller: controller(format!("http://{addr}")),
            state,
            metrics: metrics.clone(),
            rx: sync_channel(1).1,
            shutdown: Arc::new(AtomicBool::new(false)),
            sequence: 1,
            max_request_bytes: DEFAULT_MAX_REQUEST_BYTES,
            buffer: Vec::new(),
        };
        let event = occurrence_to_wire_event(&occurrence()).expect("wire event");
        assert!(worker.send_event_batch(vec![event], MAX_RETRIES).is_err());
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
        let mut worker = Worker {
            controller: controller(format!("http://{addr}")),
            state: ControllerState::new("1"),
            metrics: metrics.clone(),
            rx: sync_channel(1).1,
            shutdown: Arc::new(AtomicBool::new(false)),
            sequence: 1,
            max_request_bytes: DEFAULT_MAX_REQUEST_BYTES,
            buffer: Vec::new(),
        };
        let event = occurrence_to_wire_event(&occurrence()).expect("wire event");
        assert!(
            worker
                .send_event_batch(vec![event.clone()], MAX_RETRIES)
                .is_err()
        );
        worker
            .send_event_batch(vec![event], MAX_RETRIES)
            .expect("second batch");
        let sequences = handle.join().expect("server");
        assert_eq!(sequences, vec![1, 1, 1, 1, 2]);
    }

    #[test]
    fn timestamp_parser_accepts_z_without_fraction() {
        assert_eq!(
            timestamp_to_unix_nanos("2023-11-14T22:13:20Z"),
            1_700_000_000_000_000_000
        );
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
