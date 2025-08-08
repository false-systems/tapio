package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ErrLoggerRequired is returned when logger is not provided
var ErrLoggerRequired = errors.New("logger is required")

// Config holds OpenTelemetry configuration
type Config struct {
	ServiceName      string
	ServiceVersion   string
	Environment      string
	OTLPEndpoint     string            // For OTLP exporter (traces & metrics)
	OTLPHeaders      map[string]string // Custom headers for OTLP
	TLSInsecure      bool              // Whether to use insecure TLS (default: false for security)
	PrometheusPort   int               // For Prometheus metrics
	EnableTraces     bool
	EnableMetrics    bool
	EnablePrometheus bool
	Logger           *zap.Logger
}

// DefaultConfig returns default telemetry configuration with secure defaults
func DefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName:      serviceName,
		ServiceVersion:   "1.0.0",
		Environment:      "production",
		OTLPEndpoint:     "", // Empty means disabled
		OTLPHeaders:      make(map[string]string),
		TLSInsecure:      false, // Secure by default
		PrometheusPort:   9090,
		EnableTraces:     true,
		EnableMetrics:    true,
		EnablePrometheus: true,
	}
}

// Provider holds all OpenTelemetry providers
type Provider struct {
	config         *Config
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	promExporter   *prometheus.Exporter
	logger         *zap.Logger
}

// NewProvider creates and initializes OpenTelemetry provider
func NewProvider(ctx context.Context, config *Config) (*Provider, error) {
	if config.Logger == nil {
		config.Logger, _ = zap.NewProduction()
	}

	// Create resource with K8s attributes
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(config.Environment),
			attribute.String("tapio.component", config.ServiceName),
		),
		resource.WithProcessPID(),
		resource.WithHost(),
		resource.WithContainer(),
		resource.WithOS(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	provider := &Provider{
		config: config,
		logger: config.Logger,
	}

	// Initialize tracing
	if config.EnableTraces {
		if err := provider.initTracing(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to init tracing: %w", err)
		}
	}

	// Initialize metrics
	if config.EnableMetrics {
		if err := provider.initMetrics(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to init metrics: %w", err)
		}
	}

	// Set global providers
	otel.SetTracerProvider(provider.tracerProvider)
	otel.SetMeterProvider(provider.meterProvider)

	// Set up W3C Trace Context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider, nil
}

func (p *Provider) initTracing(ctx context.Context, res *resource.Resource) error {
	var exporter sdktrace.SpanExporter
	var err error

	if p.config.OTLPEndpoint != "" {
		// Create OTLP trace exporter with secure defaults
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(p.config.OTLPEndpoint),
		}

		// Add custom headers if provided
		if len(p.config.OTLPHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(p.config.OTLPHeaders))
		}

		// Only use insecure connection if explicitly configured (not recommended for production)
		if p.config.TLSInsecure {
			p.logger.Warn("Using insecure TLS connection for OTLP traces - not recommended for production",
				zap.String("environment", p.config.Environment))
			opts = append(opts, otlptracegrpc.WithInsecure())
		} else {
			p.logger.Info("Using secure TLS connection for OTLP traces")
		}

		exporter, err = otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
	} else {
		// No exporter if OTLP endpoint not configured
		p.logger.Warn("No OTLP endpoint configured, traces will not be exported")
		// Create a no-op tracer provider
		p.tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		return nil
	}

	// Create trace provider with batch processor
	p.tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // For now, sample everything
	)

	return nil
}

func (p *Provider) initMetrics(ctx context.Context, res *resource.Resource) error {
	var readers []sdkmetric.Reader

	// Create Prometheus exporter if enabled
	if p.config.EnablePrometheus {
		promExporter, err := prometheus.New()
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		p.promExporter = promExporter
		readers = append(readers, promExporter)
	}

	// Create OTLP metrics exporter if endpoint is configured
	if p.config.OTLPEndpoint != "" {
		// Configure OTLP metrics exporter with secure defaults
		opts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(p.config.OTLPEndpoint),
		}

		// Add custom headers if provided
		if len(p.config.OTLPHeaders) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(p.config.OTLPHeaders))
		}

		// Only use insecure connection if explicitly configured
		if p.config.TLSInsecure {
			p.logger.Warn("Using insecure TLS connection for OTLP metrics - not recommended for production",
				zap.String("environment", p.config.Environment))
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		} else {
			p.logger.Info("Using secure TLS connection for OTLP metrics")
		}

		exporter, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return fmt.Errorf("failed to create OTLP metric exporter: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(30*time.Second),
		))
	}

	// Create meter provider with options
	opts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}
	for _, reader := range readers {
		opts = append(opts, sdkmetric.WithReader(reader))
	}
	p.meterProvider = sdkmetric.NewMeterProvider(opts...)

	return nil
}

// Shutdown gracefully shuts down all providers
func (p *Provider) Shutdown(ctx context.Context) error {
	var err error

	if p.tracerProvider != nil {
		if shutdownErr := p.tracerProvider.Shutdown(ctx); shutdownErr != nil {
			err = fmt.Errorf("failed to shutdown tracer provider: %w", shutdownErr)
		}
	}

	if p.meterProvider != nil {
		if shutdownErr := p.meterProvider.Shutdown(ctx); shutdownErr != nil {
			if err != nil {
				err = fmt.Errorf("%v; failed to shutdown meter provider: %w", err, shutdownErr)
			} else {
				err = fmt.Errorf("failed to shutdown meter provider: %w", shutdownErr)
			}
		}
	}

	return err
}

// PrometheusHandler returns the Prometheus HTTP handler
func (p *Provider) PrometheusHandler() interface{} {
	if p.promExporter != nil {
		return p.promExporter
	}
	return nil
}

// GetTracer returns a tracer with the given name
func GetTracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// GetMeter returns a meter with the given name
func GetMeter(name string) metric.Meter {
	return otel.Meter(name)
}
