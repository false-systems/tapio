package base

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
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

// InitTelemetry sets up OpenTelemetry SDK with resource, exporters, and providers
func InitTelemetry(ctx context.Context, config *TelemetryConfig) (*TelemetryShutdown, error) {
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

	// 2. Set up trace exporter
	traceExporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
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

	// 4. Set up metric exporter
	metricExporterOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(config.OTLPEndpoint),
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

	// 5. Create meter provider with periodic reader
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(
			metric.NewPeriodicReader(
				metricExporter,
				metric.WithInterval(config.MetricInterval),
			),
		),
	)
	otel.SetMeterProvider(mp)

	// 6. Set up context propagation for distributed tracing
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &TelemetryShutdown{
		tracerProvider: tp,
		meterProvider:  mp,
	}, nil
}

// Shutdown gracefully shuts down telemetry and flushes pending data
func (ts *TelemetryShutdown) Shutdown(ctx context.Context) error {
	if ts == nil {
		return nil
	}

	var firstErr error

	if ts.tracerProvider != nil {
		if err := ts.tracerProvider.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("tracer provider shutdown failed: %w", err)
		}
	}

	if ts.meterProvider != nil {
		if err := ts.meterProvider.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("meter provider shutdown failed: %w", err)
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
