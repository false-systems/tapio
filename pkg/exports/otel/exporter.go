package otel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// Exporter provides a high-level interface for exporting Tapio data to OTEL
type Exporter struct {
	traceExporter *TraceExporter
	spanGenerator *SpanGenerator
	config        *ExporterConfig
	
	// OTEL SDK components
	tracerProvider *trace.TracerProvider
	shutdown       chan struct{}
	running        bool
	mutex          sync.RWMutex
}

// ExporterConfig configures the complete OTEL exporter
type ExporterConfig struct {
	// Service identification
	ServiceName     string
	ServiceVersion  string
	ServiceInstance string
	
	// OTEL endpoint
	OTLPEndpoint string
	Headers      map[string]string
	Insecure     bool
	
	// Export behavior
	BatchTimeout    time.Duration
	BatchSize       int
	ExportTimeout   time.Duration
	
	// Trace configuration
	TraceConfig *TraceConfig
	SpanConfig  *SpanConfig
	
	// Performance and reliability
	MaxConcurrentExports int
	RetryConfig          *RetryConfig
	SamplingRate         float64
	
	// Resource attributes
	ResourceAttributes map[string]string
}

// RetryConfig configures retry behavior for failed exports
type RetryConfig struct {
	MaxRetries      int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// NewExporter creates a new complete OTEL exporter
func NewExporter(config *ExporterConfig) (*Exporter, error) {
	if config == nil {
		config = DefaultExporterConfig()
	}
	
	// Initialize OTEL SDK
	tracerProvider, err := initializeTracerProvider(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tracer provider: %w", err)
	}
	
	// Set global tracer provider
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	
	// Create trace exporter and span generator
	tracer := tracerProvider.Tracer("tapio-exports")
	traceExporter := NewTraceExporter(config.TraceConfig)
	spanGenerator := NewSpanGenerator(tracer, config.SpanConfig)
	
	return &Exporter{
		traceExporter:  traceExporter,
		spanGenerator:  spanGenerator,
		config:         config,
		tracerProvider: tracerProvider,
		shutdown:       make(chan struct{}),
	}, nil
}

// DefaultExporterConfig returns sensible defaults for the exporter
func DefaultExporterConfig() *ExporterConfig {
	return &ExporterConfig{
		ServiceName:     "tapio-correlation-engine",
		ServiceVersion:  "1.0.0",
		ServiceInstance: fmt.Sprintf("instance-%d", time.Now().Unix()),
		OTLPEndpoint:    "http://localhost:4318/v1/traces",
		Insecure:        true,
		BatchTimeout:    5 * time.Second,
		BatchSize:       100,
		ExportTimeout:   10 * time.Second,
		TraceConfig:     DefaultTraceConfig(),
		SpanConfig:      DefaultSpanConfig(),
		MaxConcurrentExports: 10,
		RetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			Multiplier:      2.0,
		},
		SamplingRate: 1.0,
		ResourceAttributes: map[string]string{
			"tapio.component": "correlation-engine",
			"tapio.version":   "1.0.0",
		},
	}
}

// initializeTracerProvider sets up the OTEL tracer provider with proper configuration
func initializeTracerProvider(config *ExporterConfig) (*trace.TracerProvider, error) {
	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(config.OTLPEndpoint),
		otlptracehttp.WithHeaders(config.Headers),
		otlptracehttp.WithTimeout(config.ExportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}
	
	// Create resource with service information
	resourceAttrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.ServiceVersion),
			semconv.ServiceInstanceIDKey.String(config.ServiceInstance),
		),
	}
	
	// Add custom resource attributes
	if len(config.ResourceAttributes) > 0 {
		attrs := make([]resource.Option, 0, len(config.ResourceAttributes))
		for key, value := range config.ResourceAttributes {
			attrs = append(attrs, resource.WithAttributes(
				resource.NewAttribute(key, value),
			))
		}
		resourceAttrs = append(resourceAttrs, attrs...)
	}
	
	res, err := resource.New(context.Background(), resourceAttrs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	
	// Create tracer provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter,
			trace.WithBatchTimeout(config.BatchTimeout),
			trace.WithMaxExportBatchSize(config.BatchSize),
		),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(config.SamplingRate)),
	)
	
	return tp, nil
}

// Start starts the exporter
func (e *Exporter) Start(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	if e.running {
		return fmt.Errorf("exporter already running")
	}
	
	e.running = true
	return nil
}

// Stop stops the exporter and flushes remaining data
func (e *Exporter) Stop(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	if !e.running {
		return nil
	}
	
	e.running = false
	close(e.shutdown)
	
	// Shutdown tracer provider to flush remaining spans
	if err := e.tracerProvider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}
	
	return nil
}

// ExportCorrelationResult exports a single correlation result with full tracing
func (e *Exporter) ExportCorrelationResult(ctx context.Context, result *correlation.Result) error {
	if !e.isRunning() {
		return fmt.Errorf("exporter not running")
	}
	
	// Create root span for the correlation
	ctx, rootSpan := e.spanGenerator.GenerateCorrelationSpan(ctx, result)
	defer rootSpan.End()
	
	// Export using trace exporter for detailed analysis
	if err := e.traceExporter.ExportCorrelationResult(ctx, result); err != nil {
		rootSpan.RecordError(err)
		return fmt.Errorf("failed to export correlation result: %w", err)
	}
	
	return nil
}

// ExportCorrelationBatch exports multiple correlation results efficiently
func (e *Exporter) ExportCorrelationBatch(ctx context.Context, results []*correlation.Result) error {
	if !e.isRunning() {
		return fmt.Errorf("exporter not running")
	}
	
	if len(results) == 0 {
		return nil
	}
	
	// Use batch export for efficiency
	return e.traceExporter.ExportBatch(ctx, results)
}

// ExportRuleExecution exports rule execution metrics and traces
func (e *Exporter) ExportRuleExecution(ctx context.Context, ruleID, ruleName string, duration time.Duration, matched bool, err error) error {
	if !e.isRunning() {
		return nil // Silently skip if not running
	}
	
	// Create span for rule execution
	startTime := time.Now().Add(-duration)
	ctx, span := e.spanGenerator.GenerateRuleExecutionSpan(ctx, ruleID, ruleName, startTime)
	
	// Finish the span with results
	e.spanGenerator.FinishRuleExecutionSpan(span, duration, matched, err)
	
	return nil
}

// ExportEventProcessing exports event processing traces
func (e *Exporter) ExportEventProcessing(ctx context.Context, event *correlation.Event, processingTime time.Duration, correlations []*correlation.Result) error {
	if !e.isRunning() {
		return nil
	}
	
	// Create span for event processing
	ctx, eventSpan := e.spanGenerator.GenerateEventSpan(ctx, event, nil)
	defer eventSpan.End()
	
	// Record processing time
	eventSpan.SetAttributes(
		resource.NewAttribute("processing.duration_ms", processingTime.Milliseconds()),
		resource.NewAttribute("processing.correlations_found", len(correlations)),
	)
	
	// Create child spans for each correlation found
	for _, correlation := range correlations {
		_, correlationSpan := e.spanGenerator.GenerateCorrelationSpan(ctx, correlation)
		correlationSpan.End()
	}
	
	return nil
}

// ExportTimeline exports a timeline of events as a trace
func (e *Exporter) ExportTimeline(ctx context.Context, timeline []correlation.TimelineEntry, correlationID string) error {
	if !e.isRunning() {
		return nil
	}
	
	// Create spans for each timeline entry
	for _, entry := range timeline {
		ctx, span := e.spanGenerator.GenerateTimelineSpan(ctx, &entry, correlationID)
		span.End()
	}
	
	return nil
}

// GetMetrics returns export metrics for monitoring
func (e *Exporter) GetMetrics() ExportMetrics {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	
	// This would typically aggregate metrics from internal components
	return ExportMetrics{
		Running:        e.running,
		LastExportTime: time.Now(), // Would track actual last export time
	}
}

// ExportMetrics provides metrics about export operations
type ExportMetrics struct {
	Running           bool
	ExportsTotal      int64
	ExportErrors      int64
	LastExportTime    time.Time
	AvgExportLatency  time.Duration
}

// isRunning safely checks if the exporter is running
func (e *Exporter) isRunning() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.running
}

// SetSamplingRate dynamically updates the sampling rate
func (e *Exporter) SetSamplingRate(rate float64) error {
	if rate < 0 || rate > 1 {
		return fmt.Errorf("sampling rate must be between 0 and 1")
	}
	
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	e.config.SamplingRate = rate
	// Note: In a real implementation, you'd need to recreate the tracer provider
	// with the new sampling rate or use a dynamic sampler
	
	return nil
}

// GetConfig returns the current exporter configuration
func (e *Exporter) GetConfig() *ExporterConfig {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	
	// Return a copy to prevent external modification
	configCopy := *e.config
	return &configCopy
}

// ForceFlush forces immediate export of any buffered spans
func (e *Exporter) ForceFlush(ctx context.Context) error {
	if !e.isRunning() {
		return fmt.Errorf("exporter not running")
	}
	
	return e.tracerProvider.ForceFlush(ctx)
}