package base

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

// TelemetryConfig holds OTEL configuration
type TelemetryConfig struct {
	// OTLP endpoint (e.g., "localhost:4317")
	OTLPEndpoint string
	// Use insecure connection (default: false = TLS enabled)
	Insecure bool

	// Prometheus configuration
	PrometheusEnabled bool // Enable Prometheus /metrics endpoint
	PrometheusPort    int  // Port for Prometheus scraping (default: 9090)

	// Kubernetes context
	ClusterID string
	Namespace string
	NodeName  string

	// Service identification
	ServiceName string
	Version     string

	// Sampling rate (0.0 to 1.0)
	TraceSampleRate float64
	// Metric export interval
	MetricInterval time.Duration
}

// TelemetryShutdown holds cleanup functions
type TelemetryShutdown struct {
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
	loggerProvider *log.LoggerProvider
	httpServer     *http.Server
}

// LoadTelemetryConfigFromEnv loads config from environment variables
func LoadTelemetryConfigFromEnv() *TelemetryConfig {
	config := &TelemetryConfig{
		OTLPEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		Insecure:        getEnv("OTEL_EXPORTER_OTLP_INSECURE", "false") == "true",
		ClusterID:       getEnv("TAPIO_CLUSTER_ID", "default"),
		Namespace:       getEnv("TAPIO_NAMESPACE", "tapio-system"),
		NodeName:        getEnv("TAPIO_NODE_NAME", getHostname()),
		ServiceName:     getEnv("TAPIO_SERVICE_NAME", "tapio-observer"),
		Version:         getEnv("TAPIO_VERSION", "dev"),
		TraceSampleRate: 0.1,              // 10% sampling
		MetricInterval:  10 * time.Second, // Export every 10s
	}

	return config
}

// InitTelemetry sets up OpenTelemetry SDK with resource, exporters, and providers.
//
// When config.PrometheusEnabled is true, it starts an HTTP server on config.PrometheusPort
// exposing three endpoints:
//   - /metrics - Prometheus scrape endpoint
//   - /health  - Always returns 200 OK (liveness probe)
//   - /ready   - Returns 200 if all observers healthy, 503 otherwise (readiness probe)
//
// The observers parameter is optional and used by the /ready endpoint to check health.
// Pass nil if health checks are not needed (e.g., for self-monitoring).
func InitTelemetry(ctx context.Context, config *TelemetryConfig, observers []Observer) (*TelemetryShutdown, error) {
	if config == nil {
		return nil, fmt.Errorf("telemetry config is nil")
	}

	// 1. Create resource with cluster/namespace/node context
	res, err := CreateTapioResource(ctx, TapioResourceConfig{
		ClusterID:   config.ClusterID,
		Namespace:   config.Namespace,
		NodeName:    config.NodeName,
		ServiceName: config.ServiceName,
		Version:     config.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 2. Set up trace exporter with retry
	traceExporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  5 * time.Minute,
		}),
	}

	if config.Insecure {
		traceExporterOpts = append(traceExporterOpts, otlptracegrpc.WithInsecure())
	} else {
		traceExporterOpts = append(traceExporterOpts, otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	traceExporter, err := otlptracegrpc.New(ctx, traceExporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// 3. Create tracer provider with sampling
	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(5*time.Second),
			trace.WithMaxExportBatchSize(512),
		),
		trace.WithSampler(
			trace.ParentBased(
				trace.TraceIDRatioBased(config.TraceSampleRate),
			),
		),
	)
	otel.SetTracerProvider(tp)

	// 4. Set up metric readers (OTLP and/or Prometheus)
	var metricReaders []metric.Reader

	// 4a. OTLP metric exporter with retry (always enabled)
	metricExporterOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(config.OTLPEndpoint),
		otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  5 * time.Minute,
		}),
	}

	if config.Insecure {
		metricExporterOpts = append(metricExporterOpts, otlpmetricgrpc.WithInsecure())
	} else {
		metricExporterOpts = append(metricExporterOpts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, metricExporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	metricReaders = append(metricReaders, metric.NewPeriodicReader(
		metricExporter,
		metric.WithInterval(config.MetricInterval),
	))

	// 4b. Prometheus exporter (optional)
	var httpServer *http.Server
	if config.PrometheusEnabled {
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
		}
		metricReaders = append(metricReaders, promExporter)

		// Start HTTP server for Prometheus scraping
		if config.PrometheusPort == 0 {
			config.PrometheusPort = 9090
		}

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/health", healthHandler)
		mux.HandleFunc("/ready", readyHandler(observers))

		httpServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", config.PrometheusPort),
			Handler: mux,
		}

		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				stdlog.Printf("prometheus HTTP server error: %v", err)
			}
		}()
	}

	// 5. Create meter provider with all readers
	meterOpts := []metric.Option{metric.WithResource(res)}
	for _, reader := range metricReaders {
		meterOpts = append(meterOpts, metric.WithReader(reader))
	}
	mp := metric.NewMeterProvider(meterOpts...)
	otel.SetMeterProvider(mp)

	// 6. Set up log exporter with retry (for full ObserverEvent)
	logExporterOpts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(config.OTLPEndpoint),
		otlploggrpc.WithRetry(otlploggrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  5 * time.Minute,
		}),
	}

	if config.Insecure {
		logExporterOpts = append(logExporterOpts, otlploggrpc.WithInsecure())
	} else {
		logExporterOpts = append(logExporterOpts, otlploggrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	logExporter, err := otlploggrpc.New(ctx, logExporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	// 7. Create logger provider
	lp := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
	)
	global.SetLoggerProvider(lp)

	// 8. Set up context propagation for distributed tracing
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &TelemetryShutdown{
		tracerProvider: tp,
		meterProvider:  mp,
		loggerProvider: lp,
		httpServer:     httpServer,
	}, nil
}

// Shutdown gracefully shuts down telemetry and flushes pending data
func (ts *TelemetryShutdown) Shutdown(ctx context.Context) error {
	if ts == nil {
		return nil
	}

	var firstErr error

	// Shutdown HTTP server first (stop accepting new requests)
	if ts.httpServer != nil {
		if err := ts.httpServer.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("HTTP server shutdown failed: %w", err)
		}
	}

	if ts.tracerProvider != nil {
		if err := ts.tracerProvider.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("tracer provider shutdown failed: %w", err)
			}
		}
	}

	if ts.meterProvider != nil {
		if err := ts.meterProvider.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("meter provider shutdown failed: %w", err)
			}
		}
	}

	if ts.loggerProvider != nil {
		if err := ts.loggerProvider.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("logger provider shutdown failed: %w", err)
			}
		}
	}

	return firstErr
}

// getEnv gets environment variable with default fallback
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getHostname returns the system hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// healthHandler always returns 200 OK if the HTTP server is running
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		stdlog.Printf("failed to write health response: %v", err)
	}
}

// readyHandler returns 200 if all observers are healthy, 503 otherwise
func readyHandler(observers []Observer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		// If no observers, consider ready (nothing to check)
		if len(observers) == 0 {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("Ready")); err != nil {
				stdlog.Printf("failed to write ready response: %v", err)
			}
			return
		}

		// Check if all observers are healthy
		allHealthy := true
		for _, obs := range observers {
			if !obs.IsHealthy() {
				allHealthy = false
				break
			}
		}

		if allHealthy {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("Ready")); err != nil {
				stdlog.Printf("failed to write ready response: %v", err)
			}
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte("Not Ready")); err != nil {
				stdlog.Printf("failed to write not ready response: %v", err)
			}
		}
	}
}
