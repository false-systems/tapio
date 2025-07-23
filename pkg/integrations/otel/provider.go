package otel

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds the configuration for OTEL providers
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	SamplingRate   float64
	MetricInterval time.Duration
	EnableTraces   bool
	EnableMetrics  bool
	EnableLogs     bool
	ResourceAttrs  []attribute.KeyValue
}

// Provider manages OTEL providers for traces, metrics, and logs
type Provider struct {
	config         *Config
	resource       *resource.Resource
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	traceExporter  sdktrace.SpanExporter
	metricExporter sdkmetric.Exporter
	shutdown       []func(context.Context) error
}

// NewProvider creates a new OTEL provider with the given configuration
func NewProvider(config *Config) (*Provider, error) {
	// Create resource with auto-detection and custom attributes
	res, err := createResource(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	provider := &Provider{
		config:   config,
		resource: res,
		shutdown: make([]func(context.Context) error, 0),
	}

	// Initialize trace provider if enabled
	if config.EnableTraces {
		if err := provider.initTraceProvider(); err != nil {
			return nil, fmt.Errorf("failed to initialize trace provider: %w", err)
		}
	}

	// Initialize metric provider if enabled
	if config.EnableMetrics {
		if err := provider.initMetricProvider(); err != nil {
			return nil, fmt.Errorf("failed to initialize metric provider: %w", err)
		}
	}

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider, nil
}

// initTraceProvider initializes the trace provider with OTLP exporter
func (p *Provider) initTraceProvider() error {
	ctx := context.Background()

	// Create gRPC connection
	conn, err := grpc.DialContext(ctx, p.config.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}
	p.traceExporter = traceExporter

	// Create sampler based on configuration
	sampler := createSampler(p.config.SamplingRate)

	// Create trace provider
	p.tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(p.resource),
		sdktrace.WithSampler(sampler),
	)

	// Register as global provider
	otel.SetTracerProvider(p.tracerProvider)

	// Add shutdown function
	p.shutdown = append(p.shutdown, func(ctx context.Context) error {
		return p.tracerProvider.Shutdown(ctx)
	})

	return nil
}

// initMetricProvider initializes the metric provider with OTLP exporter
func (p *Provider) initMetricProvider() error {
	ctx := context.Background()

	// Create OTLP metric exporter
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(p.config.OTLPEndpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("failed to create metric exporter: %w", err)
	}
	p.metricExporter = metricExporter

	// Create metric provider
	p.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(p.resource),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExporter,
				sdkmetric.WithInterval(p.config.MetricInterval),
			),
		),
		sdkmetric.WithView(createMetricViews()...),
	)

	// Register as global provider
	otel.SetMeterProvider(p.meterProvider)

	// Add shutdown function
	p.shutdown = append(p.shutdown, func(ctx context.Context) error {
		return p.meterProvider.Shutdown(ctx)
	})

	return nil
}

// Tracer returns a tracer with the given name and options
func (p *Provider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	if p.tracerProvider == nil {
		return otel.Tracer(name, opts...)
	}
	return p.tracerProvider.Tracer(name, opts...)
}

// Meter returns a meter with the given name and options
func (p *Provider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if p.meterProvider == nil {
		return otel.Meter(name, opts...)
	}
	return p.meterProvider.Meter(name, opts...)
}

// Shutdown gracefully shuts down all providers
func (p *Provider) Shutdown(ctx context.Context) error {
	for _, fn := range p.shutdown {
		if err := fn(ctx); err != nil {
			return err
		}
	}
	return nil
}

// createResource creates the OTEL resource with auto-detection
func createResource(config *Config) (*resource.Resource, error) {
	// Base attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion(config.ServiceVersion),
		semconv.DeploymentEnvironment(config.Environment),
		attribute.String("service.namespace", "tapio"),
	}

	// Add custom attributes
	attrs = append(attrs, config.ResourceAttrs...)

	// Create resource with auto-detection
	return resource.New(
		context.Background(),
		resource.WithAttributes(attrs...),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithTelemetrySDK(),
	)
}

// createSampler creates a sampler based on the sampling rate
func createSampler(rate float64) sdktrace.Sampler {
	if rate <= 0 {
		return sdktrace.NeverSample()
	}
	if rate >= 1 {
		return sdktrace.AlwaysSample()
	}

	// Use parent-based sampling with ratio
	return sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(rate),
		sdktrace.WithRemoteParentSampled(sdktrace.AlwaysSample()),
		sdktrace.WithRemoteParentNotSampled(sdktrace.NeverSample()),
		sdktrace.WithLocalParentSampled(sdktrace.AlwaysSample()),
		sdktrace.WithLocalParentNotSampled(sdktrace.TraceIDRatioBased(rate)),
	)
}

// createMetricViews creates metric views for better control
func createMetricViews() []sdkmetric.View {
	return []sdkmetric.View{
		// Event processing latency histogram with custom buckets
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:  "tapio.event.processing.duration",
				Scope: instrumentation.Scope{Name: "tapio.dataflow"},
			},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
				},
			},
		),
		// Batch size histogram
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:  "tapio.batch.size",
				Scope: instrumentation.Scope{Name: "tapio.collectors"},
			},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{1, 10, 50, 100, 500, 1000, 5000, 10000},
				},
			},
		),
	}
}
