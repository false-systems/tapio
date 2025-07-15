package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/exports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// OTELExportPlugin implements OpenTelemetry export
type OTELExportPlugin struct {
	name           string
	config         *OTELExportConfig
	tracer         trace.Tracer
	meter          metric.Meter
	traceExporter  sdktrace.SpanExporter
	tracerProvider *sdktrace.TracerProvider
	metrics        *OTELMetrics
	mutex          sync.RWMutex

	// Batching
	batchProcessor *BatchProcessor

	// Resource
	resource *resource.Resource
}

// OTELExportConfig configures the OTEL export plugin
type OTELExportConfig struct {
	// Connection settings
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"` // "grpc" or "http"
	Headers  map[string]string `json:"headers"`
	Timeout  time.Duration     `json:"timeout"`

	// TLS settings
	TLSEnabled         bool   `json:"tls_enabled"`
	TLSCertPath        string `json:"tls_cert_path"`
	TLSKeyPath         string `json:"tls_key_path"`
	TLSCAPath          string `json:"tls_ca_path"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`

	// Export settings
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`
	Environment    string `json:"environment"`

	// Batching settings
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`
	MaxQueueSize int           `json:"max_queue_size"`

	// Resource attributes
	ResourceAttributes map[string]string `json:"resource_attributes"`

	// Data mapping
	EnableTraces  bool `json:"enable_traces"`
	EnableMetrics bool `json:"enable_metrics"`
	EnableLogs    bool `json:"enable_logs"`
}

// OTELMetrics tracks plugin metrics
type OTELMetrics struct {
	ExportsTotal    int64
	ExportsSuccess  int64
	ExportsFailed   int64
	TracesExported  int64
	MetricsExported int64
	LogsExported    int64
	LastExportTime  time.Time
	mutex           sync.RWMutex
}

// BatchProcessor handles batching of OTEL data
type BatchProcessor struct {
	config    *OTELExportConfig
	plugin    *OTELExportPlugin
	batchChan chan interface{}
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewOTELExportPlugin creates a new OTEL export plugin
func NewOTELExportPlugin() *OTELExportPlugin {
	return &OTELExportPlugin{
		name: "otel-export",
		config: &OTELExportConfig{
			Endpoint:       "localhost:4317",
			Protocol:       "grpc",
			ServiceName:    "tapio",
			ServiceVersion: "1.0.0",
			Environment:    "production",
			BatchSize:      100,
			BatchTimeout:   5 * time.Second,
			MaxQueueSize:   1000,
			EnableTraces:   true,
			EnableMetrics:  true,
			EnableLogs:     true,
			Timeout:        30 * time.Second,
		},
		metrics: &OTELMetrics{},
	}
}

// Name returns the plugin name
func (p *OTELExportPlugin) Name() string {
	return p.name
}

// Start starts the plugin
func (p *OTELExportPlugin) Start(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Create resource
	res, err := p.createResource()
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}
	p.resource = res

	// Initialize trace exporter
	if p.config.EnableTraces {
		exporter, err := p.createTraceExporter(ctx)
		if err != nil {
			return fmt.Errorf("failed to create trace exporter: %w", err)
		}
		p.traceExporter = exporter

		// Create tracer provider
		p.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(p.traceExporter),
			sdktrace.WithResource(p.resource),
		)
		otel.SetTracerProvider(p.tracerProvider)
		p.tracer = p.tracerProvider.Tracer("tapio")
	}

	// Initialize metrics
	if p.config.EnableMetrics {
		p.meter = otel.Meter("tapio")
	}

	// Start batch processor
	p.batchProcessor = &BatchProcessor{
		config:    p.config,
		plugin:    p,
		batchChan: make(chan interface{}, p.config.MaxQueueSize),
		stopChan:  make(chan struct{}),
	}
	p.batchProcessor.Start(ctx)

	return nil
}

// Stop stops the plugin
func (p *OTELExportPlugin) Stop(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Stop batch processor
	if p.batchProcessor != nil {
		p.batchProcessor.Stop()
	}

	// Shutdown tracer provider
	if p.tracerProvider != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.tracerProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown tracer provider: %w", err)
		}
	}

	return nil
}

// Configure configures the plugin
func (p *OTELExportPlugin) Configure(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Convert map to config struct
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	newConfig := &OTELExportConfig{}
	if err := json.Unmarshal(data, newConfig); err != nil {
		return err
	}

	// Validate configuration
	if newConfig.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if newConfig.Protocol != "grpc" && newConfig.Protocol != "http" {
		newConfig.Protocol = "grpc"
	}

	p.config = newConfig
	return nil
}

// ValidateConfig validates the plugin configuration
func (p *OTELExportPlugin) ValidateConfig() error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.config.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if p.config.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be positive")
	}

	if p.config.MaxQueueSize <= 0 {
		return fmt.Errorf("max_queue_size must be positive")
	}

	return nil
}

// GetConfigSchema returns the configuration schema
func (p *OTELExportPlugin) GetConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"endpoint": map[string]interface{}{
				"type":        "string",
				"description": "OTEL collector endpoint",
				"default":     "localhost:4317",
			},
			"protocol": map[string]interface{}{
				"type":        "string",
				"description": "Protocol to use (grpc or http)",
				"enum":        []string{"grpc", "http"},
				"default":     "grpc",
			},
			"service_name": map[string]interface{}{
				"type":        "string",
				"description": "Service name for OTEL resource",
				"default":     "tapio",
			},
			"batch_size": map[string]interface{}{
				"type":        "integer",
				"description": "Batch size for exports",
				"default":     100,
			},
			"enable_traces": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable trace export",
				"default":     true,
			},
			"enable_metrics": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable metrics export",
				"default":     true,
			},
		},
		"required": []string{"endpoint"},
	}
}

// Export exports data to OpenTelemetry
func (p *OTELExportPlugin) Export(ctx context.Context, data exports.ExportData) error {
	p.metrics.mutex.Lock()
	p.metrics.ExportsTotal++
	p.metrics.mutex.Unlock()

	start := time.Now()

	// Route to appropriate exporter based on data type
	var err error
	switch data.Type {
	case exports.DataTypeEvents:
		err = p.exportEvents(ctx, data)
	case exports.DataTypeMetrics:
		err = p.exportMetrics(ctx, data)
	case exports.DataTypeDriftReport, exports.DataTypeCorrelation, exports.DataTypePatternResult:
		err = p.exportAsTrace(ctx, data)
	default:
		err = p.exportGeneric(ctx, data)
	}

	if err != nil {
		p.metrics.mutex.Lock()
		p.metrics.ExportsFailed++
		p.metrics.mutex.Unlock()
		return err
	}

	p.metrics.mutex.Lock()
	p.metrics.ExportsSuccess++
	p.metrics.LastExportTime = time.Now()
	p.metrics.mutex.Unlock()

	// Call callback if provided
	if data.Callback != nil {
		data.Callback(&exports.ExportResult{
			Success:  true,
			Duration: time.Since(start),
			Details: map[string]interface{}{
				"endpoint": p.config.Endpoint,
				"protocol": p.config.Protocol,
			},
		})
	}

	return nil
}

// SupportedFormats returns supported export formats
func (p *OTELExportPlugin) SupportedFormats() []exports.ExportFormat {
	return []exports.ExportFormat{
		exports.FormatOTEL,
	}
}

// SupportedDataTypes returns supported data types
func (p *OTELExportPlugin) SupportedDataTypes() []exports.DataType {
	return []exports.DataType{
		exports.DataTypeDriftReport,
		exports.DataTypeSnapshot,
		exports.DataTypeCorrelation,
		exports.DataTypeMetrics,
		exports.DataTypeEvents,
		exports.DataTypePatternResult,
		exports.DataTypeAutoFix,
	}
}

// HealthCheck performs a health check
func (p *OTELExportPlugin) HealthCheck(ctx context.Context) (*exports.HealthStatus, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Check if exporter is initialized
	healthy := true
	message := "OTEL export plugin is healthy"

	if p.config.EnableTraces && p.traceExporter == nil {
		healthy = false
		message = "Trace exporter not initialized"
	}

	p.metrics.mutex.RLock()
	metrics := *p.metrics
	p.metrics.mutex.RUnlock()

	return &exports.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Message:   message,
		Details: map[string]interface{}{
			"endpoint":         p.config.Endpoint,
			"protocol":         p.config.Protocol,
			"exports_total":    metrics.ExportsTotal,
			"exports_success":  metrics.ExportsSuccess,
			"exports_failed":   metrics.ExportsFailed,
			"traces_exported":  metrics.TracesExported,
			"metrics_exported": metrics.MetricsExported,
			"last_export":      metrics.LastExportTime,
		},
		ResourceUsage: &exports.ResourceUsage{
			MemoryMB:      5.0, // Estimate
			CPUPercent:    0.5,
			ExportsPerSec: p.calculateExportRate(),
		},
	}, nil
}

// GetMetrics returns plugin metrics
func (p *OTELExportPlugin) GetMetrics() map[string]interface{} {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()

	return map[string]interface{}{
		"exports_total":    p.metrics.ExportsTotal,
		"exports_success":  p.metrics.ExportsSuccess,
		"exports_failed":   p.metrics.ExportsFailed,
		"traces_exported":  p.metrics.TracesExported,
		"metrics_exported": p.metrics.MetricsExported,
		"logs_exported":    p.metrics.LogsExported,
		"last_export_time": p.metrics.LastExportTime,
		"export_rate":      p.calculateExportRate(),
	}
}

// createResource creates OTEL resource
func (p *OTELExportPlugin) createResource() (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", p.config.ServiceName),
		attribute.String("service.version", p.config.ServiceVersion),
		attribute.String("deployment.environment", p.config.Environment),
	}

	// Add custom resource attributes
	for k, v := range p.config.ResourceAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.NewWithAttributes(
		"https://opentelemetry.io/schemas/1.0.0",
		attrs...,
	), nil
}

// createTraceExporter creates the trace exporter based on protocol
func (p *OTELExportPlugin) createTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	var opts []otlptrace.Option

	if p.config.Protocol == "grpc" {
		grpcOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(p.config.Endpoint),
			otlptracegrpc.WithTimeout(p.config.Timeout),
		}

		if !p.config.TLSEnabled {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
		}

		if p.config.Headers != nil {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithHeaders(p.config.Headers))
		}

		return otlptracegrpc.New(ctx, grpcOpts...)
	}

	// HTTP exporter
	httpOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(p.config.Endpoint),
		otlptracehttp.WithTimeout(p.config.Timeout),
	}

	if !p.config.TLSEnabled {
		httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
	}

	if p.config.Headers != nil {
		httpOpts = append(httpOpts, otlptracehttp.WithHeaders(p.config.Headers))
	}

	return otlptracehttp.New(ctx, httpOpts...)
}

// exportEvents exports events as traces
func (p *OTELExportPlugin) exportEvents(ctx context.Context, data exports.ExportData) error {
	if !p.config.EnableTraces || p.tracer == nil {
		return nil
	}

	// Create span for each event
	if events, ok := data.Content.([]interface{}); ok {
		for _, event := range events {
			if e, ok := event.(map[string]interface{}); ok {
				spanName := fmt.Sprintf("event.%v", e["type"])
				_, span := p.tracer.Start(ctx, spanName)

				// Add event attributes
				for k, v := range e {
					span.SetAttributes(attribute.String(fmt.Sprintf("event.%s", k), fmt.Sprintf("%v", v)))
				}

				// Add tags
				for k, v := range data.Tags {
					span.SetAttributes(attribute.String(fmt.Sprintf("tag.%s", k), v))
				}

				span.End()
			}
		}

		p.metrics.mutex.Lock()
		p.metrics.TracesExported += int64(len(events))
		p.metrics.mutex.Unlock()
	}

	return nil
}

// exportMetrics exports metrics
func (p *OTELExportPlugin) exportMetrics(ctx context.Context, data exports.ExportData) error {
	if !p.config.EnableMetrics || p.meter == nil {
		return nil
	}

	// Convert metrics to OTEL metrics
	// This is a simplified implementation
	if metrics, ok := data.Content.(map[string]interface{}); ok {
		for name, value := range metrics {
			if v, ok := value.(float64); ok {
				// Create a gauge for each metric
				gauge, err := p.meter.Float64ObservableGauge(
					fmt.Sprintf("tapio.%s", name),
					metric.WithDescription(fmt.Sprintf("Tapio metric: %s", name)),
				)
				if err == nil {
					_, err = p.meter.RegisterCallback(
						func(_ context.Context, o metric.Observer) error {
							o.ObserveFloat64(gauge, v)
							return nil
						},
						gauge,
					)
				}
			}
		}

		p.metrics.mutex.Lock()
		p.metrics.MetricsExported++
		p.metrics.mutex.Unlock()
	}

	return nil
}

// exportAsTrace exports data as a trace
func (p *OTELExportPlugin) exportAsTrace(ctx context.Context, data exports.ExportData) error {
	if !p.config.EnableTraces || p.tracer == nil {
		return nil
	}

	spanName := fmt.Sprintf("%s.export", data.Type)
	ctx, span := p.tracer.Start(ctx, spanName)
	defer span.End()

	// Add data attributes
	span.SetAttributes(
		attribute.String("export.type", string(data.Type)),
		attribute.String("export.format", string(data.Format)),
		attribute.String("export.source", data.Source),
		attribute.Int64("export.timestamp", data.Timestamp.UnixNano()),
	)

	// Add tags
	for k, v := range data.Tags {
		span.SetAttributes(attribute.String(fmt.Sprintf("tag.%s", k), v))
	}

	// Add metadata
	for k, v := range data.Metadata {
		span.SetAttributes(attribute.String(fmt.Sprintf("meta.%s", k), fmt.Sprintf("%v", v)))
	}

	// Add content summary
	if content, err := json.Marshal(data.Content); err == nil {
		if len(content) > 1000 {
			content = content[:1000]
		}
		span.SetAttributes(attribute.String("export.content_preview", string(content)))
	}

	p.metrics.mutex.Lock()
	p.metrics.TracesExported++
	p.metrics.mutex.Unlock()

	return nil
}

// exportGeneric exports generic data
func (p *OTELExportPlugin) exportGeneric(ctx context.Context, data exports.ExportData) error {
	// Export as trace by default
	return p.exportAsTrace(ctx, data)
}

// calculateExportRate calculates exports per second
func (p *OTELExportPlugin) calculateExportRate() float64 {
	// Simple rate calculation - in production would use a sliding window
	return 0.0
}

// BatchProcessor implementation

func (bp *BatchProcessor) Start(ctx context.Context) {
	bp.wg.Add(1)
	go bp.run(ctx)
}

func (bp *BatchProcessor) Stop() {
	close(bp.stopChan)
	bp.wg.Wait()
}

func (bp *BatchProcessor) run(ctx context.Context) {
	defer bp.wg.Done()

	batch := make([]interface{}, 0, bp.config.BatchSize)
	ticker := time.NewTicker(bp.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			bp.flush(batch)
			return
		case <-bp.stopChan:
			bp.flush(batch)
			return
		case data := <-bp.batchChan:
			batch = append(batch, data)
			if len(batch) >= bp.config.BatchSize {
				bp.flush(batch)
				batch = make([]interface{}, 0, bp.config.BatchSize)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				bp.flush(batch)
				batch = make([]interface{}, 0, bp.config.BatchSize)
			}
		}
	}
}

func (bp *BatchProcessor) flush(batch []interface{}) {
	if len(batch) == 0 {
		return
	}

	// Process batch
	// This is where actual batched export would happen
	// For now, this is a placeholder
}
