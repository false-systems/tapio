use std::net::SocketAddr;
use std::sync::Arc;

use prometheus::{IntCounterVec, IntGauge, Opts, Registry, TextEncoder};

/// All TAPIO Prometheus metrics, registered against a non-global Registry.
/// Fields are registered at startup but not yet wired to observers (audit issue #4).
#[derive(Clone)]
#[allow(dead_code)]
pub struct TapioMetrics {
    pub registry: Arc<Registry>,

    // Observer health
    pub events_total: IntCounterVec,
    pub anomalies_total: IntCounterVec,
    pub lost_events_total: IntCounterVec,
    pub drain_cap_total: IntCounterVec,
    pub enrichment_miss_total: IntCounterVec,

    // Sink health
    pub sink_writes_total: IntCounterVec,

    // K8s enrichment
    pub k8s_cache_size: IntGauge,
    pub k8s_reflector_up: IntGauge,
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
impl TapioMetrics {
    pub fn new() -> anyhow::Result<Self> {
        let registry = Registry::new();

        let events_total = IntCounterVec::new(
            Opts::new(
                "tapio_events_total",
                "Total events drained from ring buffer",
            ),
            &["observer"],
        )?;
        registry.register(Box::new(events_total.clone()))?;

        let anomalies_total = IntCounterVec::new(
            Opts::new(
                "tapio_anomalies_total",
                "Total anomalies detected and emitted",
            ),
            &["observer", "anomaly_type"],
        )?;
        registry.register(Box::new(anomalies_total.clone()))?;

        let lost_events_total = IntCounterVec::new(
            Opts::new(
                "tapio_lost_events_total",
                "Ring buffer reserve failures in eBPF (events dropped)",
            ),
            &["observer"],
        )?;
        registry.register(Box::new(lost_events_total.clone()))?;

        let drain_cap_total = IntCounterVec::new(
            Opts::new(
                "tapio_drain_cap_total",
                "Times ring buffer drain hit the per-tick cap",
            ),
            &["observer"],
        )?;
        registry.register(Box::new(drain_cap_total.clone()))?;

        let enrichment_miss_total = IntCounterVec::new(
            Opts::new(
                "tapio_enrichment_miss_total",
                "Enrichment lookups that returned no pod context",
            ),
            &["observer"],
        )?;
        registry.register(Box::new(enrichment_miss_total.clone()))?;

        let sink_writes_total = IntCounterVec::new(
            Opts::new(
                "tapio_sink_writes_total",
                "Total sink write attempts by result",
            ),
            &["sink", "result"],
        )?;
        registry.register(Box::new(sink_writes_total.clone()))?;

        let k8s_cache_size = IntGauge::new(
            "tapio_k8s_cache_size",
            "Current cgroup_id to pod map entries",
        )?;
        registry.register(Box::new(k8s_cache_size.clone()))?;

        let k8s_reflector_up = IntGauge::new(
            "tapio_k8s_reflector_up",
            "1 if K8s reflector is connected, 0 if not",
        )?;
        registry.register(Box::new(k8s_reflector_up.clone()))?;

        Ok(Self {
            registry: Arc::new(registry),
            events_total,
            anomalies_total,
            lost_events_total,
            drain_cap_total,
            enrichment_miss_total,
            sink_writes_total,
            k8s_cache_size,
            k8s_reflector_up,
        })
    }
}

/// Serve /metrics on the given port. Runs until the shutdown token is cancelled.
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub async fn serve(
    registry: Arc<Registry>,
    port: u16,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) -> anyhow::Result<()> {
    use axum::{Router, http::header, routing::get};

    let app = Router::new().route(
        "/metrics",
        get(move || {
            let registry = registry.clone();
            async move {
                let encoder = TextEncoder::new();
                let metric_families = registry.gather();
                let mut buffer = String::new();
                if let Err(e) = encoder.encode_utf8(&metric_families, &mut buffer) {
                    return (
                        axum::http::StatusCode::INTERNAL_SERVER_ERROR,
                        [(header::CONTENT_TYPE, "text/plain")],
                        format!("encoding error: {e}"),
                    );
                }
                (
                    axum::http::StatusCode::OK,
                    [(header::CONTENT_TYPE, "text/plain; version=0.0.4")],
                    buffer,
                )
            }
        }),
    );

    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    tracing::info!(%addr, "prometheus metrics endpoint starting");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            let _ = shutdown.changed().await;
        })
        .await?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metrics_register_without_panic() {
        let m = TapioMetrics::new().expect("metrics registration");
        m.events_total.with_label_values(&["network"]).inc();
        m.anomalies_total
            .with_label_values(&["network", "kernel.network.rst_storm"])
            .inc();
        m.sink_writes_total
            .with_label_values(&["stdout", "ok"])
            .inc();
        m.k8s_cache_size.set(42);
        m.k8s_reflector_up.set(1);

        let encoder = TextEncoder::new();
        let families = m.registry.gather();
        let mut buf = String::new();
        encoder
            .encode_utf8(&families, &mut buf)
            .expect("encode metrics");
        assert!(buf.contains("tapio_events_total"));
        assert!(buf.contains("tapio_anomalies_total"));
        assert!(buf.contains("tapio_k8s_cache_size 42"));
    }
}
