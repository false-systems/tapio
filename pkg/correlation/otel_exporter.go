package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/events/opinionated"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// OTELExporter exports correlation and pattern detection data to OpenTelemetry
type OTELExporter struct {
	// OTEL components
	tracer trace.Tracer
	meter  metric.Meter

	// Exporters
	traceExporter  sdktrace.SpanExporter
	metricExporter sdkmetric.Exporter

	// Configuration
	config *OTELExporterConfig

	// Semantic grouping (HERO FEATURE!)
	semanticGrouper *SemanticEventGrouper

	// Metrics instruments
	patternMetrics     *PatternMetrics
	correlationMetrics *CorrelationMetrics
	performanceMetrics *PerformanceMetrics

	// State management
	running bool
	mutex   sync.RWMutex

	// Buffering for batch export
	eventBuffer       []*opinionated.OpinionatedEvent
	patternBuffer     []*types.PatternResult
	correlationBuffer []*domain.Correlation
	bufferMutex       sync.Mutex
}

// OTELExporterConfig configures the OTEL exporter
type OTELExporterConfig struct {
	// OTEL endpoint configuration
	OTLPEndpoint string            `json:"otlp_endpoint"`
	Headers      map[string]string `json:"headers"`
	Insecure     bool              `json:"insecure"`

	// Service identification
	ServiceName       string `json:"service_name"`
	ServiceVersion    string `json:"service_version"`
	ServiceNamespace  string `json:"service_namespace"`
	ServiceInstanceID string `json:"service_instance_id"`

	// Export configuration
	BatchTimeout       time.Duration `json:"batch_timeout"`
	BatchSize          int           `json:"batch_size"`
	MaxExportBatchSize int           `json:"max_export_batch_size"`
	ExportTimeout      time.Duration `json:"export_timeout"`

	// Feature flags
	ExportTraces  bool `json:"export_traces"`
	ExportMetrics bool `json:"export_metrics"`
	ExportLogs    bool `json:"export_logs"`

	// Pattern-specific configuration
	ExportPatternResults bool `json:"export_pattern_results"`
	ExportCorrelations   bool `json:"export_correlations"`
	ExportInsights       bool `json:"export_insights"`

	// Sampling configuration
	TraceSampleRate float64       `json:"trace_sample_rate"`
	MetricInterval  time.Duration `json:"metric_interval"`

	// Enterprise features
	EnrichWithMetadata bool              `json:"enrich_with_metadata"`
	CustomAttributes   map[string]string `json:"custom_attributes"`
	ResourceDetection  bool              `json:"resource_detection"`
}

// PatternMetrics defines pattern detection metrics for OTEL
type PatternMetrics struct {
	// Detection metrics
	PatternsDetected metric.Int64Counter     `json:"patterns_detected"`
	PatternAccuracy  metric.Float64Histogram `json:"pattern_accuracy"`
	DetectionLatency metric.Float64Histogram `json:"detection_latency"`
	FalsePositives   metric.Int64Counter     `json:"false_positives"`
	TruePositives    metric.Int64Counter     `json:"true_positives"`

	// Pattern type metrics
	MemoryLeakPatterns metric.Int64Counter `json:"memory_leak_patterns"`
	NetworkFailures    metric.Int64Counter `json:"network_failures"`
	StorageBottlenecks metric.Int64Counter `json:"storage_bottlenecks"`
	RuntimeFailures    metric.Int64Counter `json:"runtime_failures"`
	DependencyFailures metric.Int64Counter `json:"dependency_failures"`

	// Quality metrics
	DataQuality     metric.Float64Histogram `json:"data_quality"`
	ModelAccuracy   metric.Float64Histogram `json:"model_accuracy"`
	ConfidenceScore metric.Float64Histogram `json:"confidence_score"`
}

// CorrelationMetrics defines correlation engine metrics for OTEL
type CorrelationMetrics struct {
	// Correlation metrics
	CorrelationsFound   metric.Int64Counter     `json:"correlations_found"`
	CorrelationAccuracy metric.Float64Histogram `json:"correlation_accuracy"`
	InsightsGenerated   metric.Int64Counter     `json:"insights_generated"`

	// Processing metrics
	EventsProcessed   metric.Int64Counter       `json:"events_processed"`
	ProcessingLatency metric.Float64Histogram   `json:"processing_latency"`
	QueueDepth        metric.Int64UpDownCounter `json:"queue_depth"`

	// Rule execution metrics
	RulesExecuted     metric.Int64Counter     `json:"rules_executed"`
	RuleExecutionTime metric.Float64Histogram `json:"rule_execution_time"`
	RuleSuccessRate   metric.Float64Histogram `json:"rule_success_rate"`
}

// PerformanceMetrics defines system performance metrics for OTEL
type PerformanceMetrics struct {
	// System metrics
	CPUUsage       metric.Float64Histogram   `json:"cpu_usage"`
	MemoryUsage    metric.Int64UpDownCounter `json:"memory_usage"`
	GoroutineCount metric.Int64UpDownCounter `json:"goroutine_count"`

	// Cache metrics
	CacheHitRate   metric.Float64Histogram   `json:"cache_hit_rate"`
	CacheSize      metric.Int64UpDownCounter `json:"cache_size"`
	CacheEvictions metric.Int64Counter       `json:"cache_evictions"`

	// Integration metrics
	IntegrationLatency metric.Float64Histogram `json:"integration_latency"`
	FusionSuccess      metric.Int64Counter     `json:"fusion_success"`
	ValidationAccuracy metric.Float64Histogram `json:"validation_accuracy"`
}

// NewOTELExporter creates a new OTEL exporter
func NewOTELExporter(config *OTELExporterConfig, correlationEngine *Engine) (*OTELExporter, error) {
	if config == nil {
		config = DefaultOTELExporterConfig()
	}

	exporter := &OTELExporter{
		config:            config,
		eventBuffer:       make([]*opinionated.OpinionatedEvent, 0, config.BatchSize),
		patternBuffer:     make([]*types.PatternResult, 0, config.BatchSize),
		correlationBuffer: make([]*domain.Correlation, 0, config.BatchSize),
	}

	// Initialize semantic grouper for HERO-level correlation
	if correlationEngine != nil {
		exporter.semanticGrouper = NewSemanticEventGrouper(correlationEngine)
	}

	// Initialize OTEL components
	if err := exporter.initializeOTEL(); err != nil {
		return nil, fmt.Errorf("failed to initialize OTEL: %w", err)
	}

	// Initialize metrics
	if err := exporter.initializeMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return exporter, nil
}

// DefaultOTELExporterConfig returns default OTEL exporter configuration
func DefaultOTELExporterConfig() *OTELExporterConfig {
	return &OTELExporterConfig{
		OTLPEndpoint:         "localhost:4317",
		Insecure:             true,
		ServiceName:          "tapio-correlation-engine",
		ServiceVersion:       "1.0.0",
		ServiceNamespace:     "observability",
		BatchTimeout:         5 * time.Second,
		BatchSize:            100,
		MaxExportBatchSize:   512,
		ExportTimeout:        30 * time.Second,
		ExportTraces:         true,
		ExportMetrics:        true,
		ExportLogs:           true,
		ExportPatternResults: true,
		ExportCorrelations:   true,
		ExportInsights:       true,
		TraceSampleRate:      1.0,
		MetricInterval:       15 * time.Second,
		EnrichWithMetadata:   true,
		ResourceDetection:    true,
		CustomAttributes:     make(map[string]string),
	}
}

// initializeOTEL sets up OpenTelemetry exporters and providers
func (oe *OTELExporter) initializeOTEL() error {
	// Create resource
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(oe.config.ServiceName),
		semconv.ServiceVersionKey.String(oe.config.ServiceVersion),
		semconv.ServiceNamespaceKey.String(oe.config.ServiceNamespace),
		semconv.ServiceInstanceIDKey.String(oe.config.ServiceInstanceID),
		attribute.String("component", "correlation-engine"),
		attribute.String("feature", "pattern-detection"),
	)

	// Add custom attributes
	var attrs []attribute.KeyValue
	for key, value := range oe.config.CustomAttributes {
		attrs = append(attrs, attribute.String(key, value))
	}
	if len(attrs) > 0 {
		res, _ = resource.Merge(res, resource.NewWithAttributes(semconv.SchemaURL, attrs...))
	}

	// Initialize trace exporter if enabled
	if oe.config.ExportTraces {
		traceExporter, err := otlptracegrpc.New(
			context.Background(),
			otlptracegrpc.WithEndpoint(oe.config.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithHeaders(oe.config.Headers),
		)
		if err != nil {
			return fmt.Errorf("failed to create trace exporter: %w", err)
		}
		oe.traceExporter = traceExporter

		// Create trace provider
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter,
				sdktrace.WithBatchTimeout(oe.config.BatchTimeout),
				sdktrace.WithMaxExportBatchSize(oe.config.MaxExportBatchSize),
			),
			sdktrace.WithResource(res),
		)

		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})

		oe.tracer = otel.Tracer("tapio-correlation")
	}

	// Initialize metric exporter if enabled
	if oe.config.ExportMetrics {
		metricExporter, err := otlpmetricgrpc.New(
			context.Background(),
			otlpmetricgrpc.WithEndpoint(oe.config.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(),
			otlpmetricgrpc.WithHeaders(oe.config.Headers),
		)
		if err != nil {
			return fmt.Errorf("failed to create metric exporter: %w", err)
		}
		oe.metricExporter = metricExporter

		// Create metric provider with new SDK API
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(oe.config.MetricInterval))),
		)

		otel.SetMeterProvider(meterProvider)
		oe.meter = meterProvider.Meter("tapio-correlation")
	}

	return nil
}

// initializeMetrics creates all metric instruments
func (oe *OTELExporter) initializeMetrics() error {
	if !oe.config.ExportMetrics {
		return nil
	}

	var err error

	// Initialize pattern metrics
	oe.patternMetrics = &PatternMetrics{}

	oe.patternMetrics.PatternsDetected, err = oe.meter.Int64Counter(
		"tapio_patterns_detected_total",
		metric.WithDescription("Total number of patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create patterns_detected counter: %w", err)
	}

	oe.patternMetrics.PatternAccuracy, err = oe.meter.Float64Histogram(
		"tapio_pattern_accuracy",
		metric.WithDescription("Pattern detection accuracy"),
	)
	if err != nil {
		return fmt.Errorf("failed to create pattern_accuracy histogram: %w", err)
	}

	oe.patternMetrics.DetectionLatency, err = oe.meter.Float64Histogram(
		"tapio_pattern_detection_latency_seconds",
		metric.WithDescription("Pattern detection latency in seconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to create detection_latency histogram: %w", err)
	}

	oe.patternMetrics.FalsePositives, err = oe.meter.Int64Counter(
		"tapio_pattern_false_positives_total",
		metric.WithDescription("Total number of false positive pattern detections"),
	)
	if err != nil {
		return fmt.Errorf("failed to create false_positives counter: %w", err)
	}

	oe.patternMetrics.TruePositives, err = oe.meter.Int64Counter(
		"tapio_pattern_true_positives_total",
		metric.WithDescription("Total number of true positive pattern detections"),
	)
	if err != nil {
		return fmt.Errorf("failed to create true_positives counter: %w", err)
	}

	// Pattern type counters
	oe.patternMetrics.MemoryLeakPatterns, err = oe.meter.Int64Counter(
		"tapio_memory_leak_patterns_total",
		metric.WithDescription("Total number of memory leak patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create memory_leak_patterns counter: %w", err)
	}

	oe.patternMetrics.NetworkFailures, err = oe.meter.Int64Counter(
		"tapio_network_failure_patterns_total",
		metric.WithDescription("Total number of network failure patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create network_failures counter: %w", err)
	}

	oe.patternMetrics.StorageBottlenecks, err = oe.meter.Int64Counter(
		"tapio_storage_bottleneck_patterns_total",
		metric.WithDescription("Total number of storage bottleneck patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create storage_bottlenecks counter: %w", err)
	}

	oe.patternMetrics.RuntimeFailures, err = oe.meter.Int64Counter(
		"tapio_runtime_failure_patterns_total",
		metric.WithDescription("Total number of runtime failure patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create runtime_failures counter: %w", err)
	}

	oe.patternMetrics.DependencyFailures, err = oe.meter.Int64Counter(
		"tapio_dependency_failure_patterns_total",
		metric.WithDescription("Total number of dependency failure patterns detected"),
	)
	if err != nil {
		return fmt.Errorf("failed to create dependency_failures counter: %w", err)
	}

	// Quality metrics
	oe.patternMetrics.DataQuality, err = oe.meter.Float64Histogram(
		"tapio_pattern_data_quality",
		metric.WithDescription("Quality of data used for pattern detection"),
	)
	if err != nil {
		return fmt.Errorf("failed to create data_quality histogram: %w", err)
	}

	oe.patternMetrics.ModelAccuracy, err = oe.meter.Float64Histogram(
		"tapio_pattern_model_accuracy",
		metric.WithDescription("Accuracy of pattern detection models"),
	)
	if err != nil {
		return fmt.Errorf("failed to create model_accuracy histogram: %w", err)
	}

	oe.patternMetrics.ConfidenceScore, err = oe.meter.Float64Histogram(
		"tapio_pattern_confidence_score",
		metric.WithDescription("Confidence score of pattern detections"),
	)
	if err != nil {
		return fmt.Errorf("failed to create confidence_score histogram: %w", err)
	}

	// Initialize correlation metrics
	oe.correlationMetrics = &CorrelationMetrics{}

	oe.correlationMetrics.CorrelationsFound, err = oe.meter.Int64Counter(
		"tapio_correlations_found_total",
		metric.WithDescription("Total number of correlations found"),
	)
	if err != nil {
		return fmt.Errorf("failed to create correlations_found counter: %w", err)
	}

	oe.correlationMetrics.InsightsGenerated, err = oe.meter.Int64Counter(
		"tapio_insights_generated_total",
		metric.WithDescription("Total number of insights generated"),
	)
	if err != nil {
		return fmt.Errorf("failed to create insights_generated counter: %w", err)
	}

	oe.correlationMetrics.EventsProcessed, err = oe.meter.Int64Counter(
		"tapio_events_processed_total",
		metric.WithDescription("Total number of events processed"),
	)
	if err != nil {
		return fmt.Errorf("failed to create events_processed counter: %w", err)
	}

	oe.correlationMetrics.ProcessingLatency, err = oe.meter.Float64Histogram(
		"tapio_processing_latency_seconds",
		metric.WithDescription("Event processing latency in seconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to create processing_latency histogram: %w", err)
	}

	// Initialize performance metrics
	oe.performanceMetrics = &PerformanceMetrics{}

	oe.performanceMetrics.CPUUsage, err = oe.meter.Float64Histogram(
		"tapio_cpu_usage_percent",
		metric.WithDescription("CPU usage percentage"),
	)
	if err != nil {
		return fmt.Errorf("failed to create cpu_usage histogram: %w", err)
	}

	oe.performanceMetrics.MemoryUsage, err = oe.meter.Int64UpDownCounter(
		"tapio_memory_usage_bytes",
		metric.WithDescription("Memory usage in bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to create memory_usage counter: %w", err)
	}

	return nil
}

// ExportEvent exports an opinionated event to OTEL with semantic grouping
func (oe *OTELExporter) ExportEvent(ctx context.Context, event *opinionated.OpinionatedEvent) error {
	if !oe.config.ExportTraces {
		return nil
	}

	// Use semantic grouping for intelligent correlation
	if oe.semanticGrouper != nil {
		return oe.semanticGrouper.ProcessEventWithSemanticGrouping(ctx, event)
	}

	// Fallback to basic event export if semantic grouping not available
	return oe.exportBasicEvent(ctx, event)
}

// exportBasicEvent exports event without semantic grouping (fallback)
func (oe *OTELExporter) exportBasicEvent(ctx context.Context, event *opinionated.OpinionatedEvent) error {
	// Create trace span for the event
	ctx, span := oe.tracer.Start(ctx, "correlation.event.process",
		trace.WithAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.category", string(event.Category)),
			attribute.String("event.severity", string(event.Severity)),
			attribute.Float64("event.confidence", float64(event.Confidence)),
			attribute.String("event.source_collector", event.Source.Collector),
			attribute.String("event.source_component", event.Source.Component),
		),
	)
	defer span.End()

	// Add semantic context if available
	if event.Semantic != nil {
		span.SetAttributes(
			attribute.String("semantic.intent", event.Semantic.Intent),
			// attribute.Float64("semantic.confidence", float64(event.Semantic.Confidence)), // Field not available
			attribute.StringSlice("semantic.ontology_tags", event.Semantic.OntologyTags),
		)
	}

	// Add behavioral context if available
	if event.Behavioral != nil {
		span.SetAttributes(
			// attribute.Float64("behavioral.anomaly_score", float64(event.Behavioral.AnomalyScore)), // Field not available
			// attribute.Float64("behavioral.trust_score", float64(event.Behavioral.TrustScore)), // Field not available
		)
	}

	// Add temporal context if available
	if event.Temporal != nil {
		span.SetAttributes(
			// attribute.Bool("temporal.is_periodic", event.Temporal.IsPeriodic), // Field not available
			// attribute.Float64("temporal.frequency_hz", float64(event.Temporal.FrequencyHz)), // Field not available
		)
	}

	// Buffer event for batch export
	oe.bufferEvent(event)

	// Record metrics
	oe.correlationMetrics.EventsProcessed.Add(ctx, 1)

	return nil
}

// ExportPatternResult exports a pattern detection result to OTEL
func (oe *OTELExporter) ExportPatternResult(ctx context.Context, result *types.PatternResult) error {
	if !oe.config.ExportPatternResults {
		return nil
	}

	// Create trace span for pattern detection
	ctx, span := oe.tracer.Start(ctx, "correlation.pattern.detected",
		trace.WithAttributes(
			attribute.String("pattern.id", result.PatternID),
			attribute.String("pattern.name", result.PatternName),
			attribute.Bool("pattern.detected", result.Detected),
			attribute.Float64("pattern.confidence", result.Confidence),
			attribute.String("pattern.severity", string(result.Severity)),
			attribute.Int64("pattern.processing_time_ms", result.ProcessingTime.Milliseconds()),
			attribute.Float64("pattern.data_quality", result.DataQuality),
			attribute.Float64("pattern.model_accuracy", result.ModelAccuracy),
		),
	)
	defer span.End()

	// Add root cause information if available
	if result.RootCause != nil {
		span.SetAttributes(
			// Root cause fields not available on interface{} type
			// attribute.String("pattern.root_cause.event_type", result.RootCause.EventType),
			// attribute.String("pattern.root_cause.entity_type", result.RootCause.Entity.Type),
			// attribute.Float64("pattern.root_cause.confidence", result.RootCause.Confidence),
		)
	}

	// Add impact information - commenting out fields not available on interface{} type
	span.SetAttributes(
		// attribute.Int("pattern.impact.affected_services", result.Impact.AffectedServices),
		// attribute.Int("pattern.impact.affected_pods", result.Impact.AffectedPods),
		// attribute.Int("pattern.impact.affected_nodes", result.Impact.AffectedNodes),
		// attribute.Float64("pattern.impact.performance_degradation", result.Impact.PerformanceDegradation),
	)

	// Add predictions information
	if len(result.Predictions) > 0 {
		// Prediction fields not available on interface{} type
		span.SetAttributes(
			attribute.Int("pattern.predictions.count", len(result.Predictions)),
		)
	}

	// Set span status based on detection
	if result.Detected {
		span.SetStatus(codes.Ok, "Pattern successfully detected")
	} else {
		span.SetStatus(codes.Ok, "No pattern detected")
	}

	// Buffer pattern result for batch export
	oe.bufferPatternResult(result)

	// Record pattern metrics
	if result.Detected {
		oe.patternMetrics.PatternsDetected.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("pattern_type", result.PatternID),
				attribute.String("severity", string(result.Severity)),
			),
		)

		// Record pattern type-specific metrics
		switch result.PatternID {
		case "memory_leak_oom_cascade":
			oe.patternMetrics.MemoryLeakPatterns.Add(ctx, 1)
		case "network_failure_cascade":
			oe.patternMetrics.NetworkFailures.Add(ctx, 1)
		case "storage_io_bottleneck":
			oe.patternMetrics.StorageBottlenecks.Add(ctx, 1)
		case "container_runtime_failure":
			oe.patternMetrics.RuntimeFailures.Add(ctx, 1)
		case "service_dependency_failure":
			oe.patternMetrics.DependencyFailures.Add(ctx, 1)
		}

		// Record quality metrics
		oe.patternMetrics.PatternAccuracy.Record(ctx, result.ModelAccuracy)
		oe.patternMetrics.DataQuality.Record(ctx, result.DataQuality)
		oe.patternMetrics.ConfidenceScore.Record(ctx, result.Confidence)
	}

	// Record detection latency
	oe.patternMetrics.DetectionLatency.Record(ctx, result.ProcessingTime.Seconds())

	return nil
}

// ExportCorrelation exports a correlation result to OTEL
func (oe *OTELExporter) ExportCorrelation(ctx context.Context, correlation *domain.Correlation) error {
	if !oe.config.ExportCorrelations {
		return nil
	}

	// Create trace span for correlation
	ctx, span := oe.tracer.Start(ctx, "correlation.correlation.found",
		trace.WithAttributes(
			// attribute.String("correlation.type", correlation.Type), // Field not available
			// attribute.Float64("correlation.confidence", correlation.Confidence), // Field not available  
			// attribute.Float64("correlation.strength", correlation.Strength), // Field not available
		),
	)
	defer span.End()

	// Buffer correlation for batch export
	oe.bufferCorrelation(correlation)

	// Record correlation metrics
	oe.correlationMetrics.CorrelationsFound.Add(ctx, 1,
		metric.WithAttributes(
			// attribute.String("correlation_type", correlation.Type), // Field not available
		),
	)

	return nil
}

// ExportIntegratedResult exports an integrated result to OTEL
func (oe *OTELExporter) ExportIntegratedResult(ctx context.Context, result *IntegratedResult) error {
	// Create trace span for integrated processing
	ctx, span := oe.tracer.Start(ctx, "correlation.integrated.process",
		trace.WithAttributes(
			attribute.String("event.id", result.Event.ID),
			attribute.Int("correlations.count", len(result.CorrelationResults)),
			attribute.Int("patterns.count", len(result.PatternResults)),
			attribute.Int("insights.count", len(result.FusedInsights)),
			attribute.Int64("processing.time_ms", result.ProcessingTime.Milliseconds()),
		),
	)
	defer span.End()

	// Export individual components
	if err := oe.ExportEvent(ctx, result.Event); err != nil {
		span.RecordError(err)
	}

	for _, patternResult := range result.PatternResults {
		if err := oe.ExportPatternResult(ctx, patternResult); err != nil {
			span.RecordError(err)
		}
	}

	for _, correlation := range result.CorrelationResults {
		if err := oe.ExportCorrelation(ctx, correlation); err != nil {
			span.RecordError(err)
		}
	}

	// Record integration metrics
	oe.correlationMetrics.ProcessingLatency.Record(ctx, result.ProcessingTime.Seconds())

	if len(result.FusedInsights) > 0 {
		oe.correlationMetrics.InsightsGenerated.Add(ctx, int64(len(result.FusedInsights)))
	}

	return nil
}

// ExportValidationResult exports a pattern validation result to OTEL
func (oe *OTELExporter) ExportValidationResult(ctx context.Context, validation *types.ValidationRun) error {
	// Create trace span for validation
	ctx, span := oe.tracer.Start(ctx, "correlation.pattern.validation",
		trace.WithAttributes(
			attribute.String("validation.run_id", validation.ID),
			attribute.StringSlice("validation.patterns_run", validation.PatternsRun),
			attribute.Int("validation.events_scanned", validation.EventsScanned),
			attribute.Int("validation.results_count", len(validation.Results)),
			attribute.Int("validation.errors_count", len(validation.Errors)),
			attribute.String("validation.start_time", validation.StartTime.Format(time.RFC3339)),
			attribute.String("validation.end_time", validation.EndTime.Format(time.RFC3339)),
			// Fields not available in types.ValidationRun - commented out:
			// attribute.String("validation.pattern_id", validation.PatternID),
			// attribute.String("validation.status", validation.Status),
			// attribute.Int("validation.total_samples", validation.TotalSamples),
			// attribute.Int("validation.true_positives", validation.TruePositives),
			// attribute.Int("validation.false_positives", validation.FalsePositives),
			// attribute.Int("validation.true_negatives", validation.TrueNegatives),
			// attribute.Int("validation.false_negatives", validation.FalseNegatives),
			// attribute.Float64("validation.accuracy", validation.Accuracy),
			// attribute.Float64("validation.precision", validation.Precision),
			// attribute.Float64("validation.recall", validation.Recall),
			// attribute.Float64("validation.f1_score", validation.F1Score),
		),
	)
	defer span.End()

	// Record validation metrics using available data
	if !validation.EndTime.IsZero() {
		duration := validation.EndTime.Sub(validation.StartTime)
		span.SetAttributes(
			attribute.Float64("validation.duration_seconds", duration.Seconds()),
		)

		// Record patterns processed
		for _, patternID := range validation.PatternsRun {
			oe.performanceMetrics.ValidationAccuracy.Record(ctx, 1.0, // Placeholder accuracy
				metric.WithAttributes(
					attribute.String("pattern_type", patternID),
				),
			)
		}

		// Record basic metrics from results
		detectedCount := 0
		for _, result := range validation.Results {
			if result.Detected {
				detectedCount++
			}
		}
		
		// Record pattern detections as placeholder for true/false positives
		oe.patternMetrics.TruePositives.Add(ctx, int64(detectedCount))
		oe.patternMetrics.FalsePositives.Add(ctx, int64(len(validation.Results)-detectedCount))
	}

	return nil
}

// RecordPerformanceMetrics records system performance metrics
func (oe *OTELExporter) RecordPerformanceMetrics(ctx context.Context, stats *IntegratedEngineStats) error {
	if !oe.config.ExportMetrics {
		return nil
	}

	// Record cache metrics if available
	if stats.PatternCacheStats != nil {
		oe.performanceMetrics.CacheHitRate.Record(ctx, stats.PatternCacheStats.CacheHitRate)
		oe.performanceMetrics.CacheSize.Add(ctx, int64(stats.PatternCacheStats.CacheSize))
		oe.performanceMetrics.CacheEvictions.Add(ctx, int64(stats.PatternCacheStats.EvictionCount))
	}

	// Record integration performance
	if stats.PatternIntegrationStats != nil {
		oe.performanceMetrics.IntegrationLatency.Record(ctx,
			stats.PatternIntegrationStats.AvgIntegrationTime.Seconds())
		oe.performanceMetrics.FusionSuccess.Add(ctx,
			int64(stats.PatternIntegrationStats.PatternsFused))
	}

	return nil
}

// Buffer management methods

func (oe *OTELExporter) bufferEvent(event *opinionated.OpinionatedEvent) {
	oe.bufferMutex.Lock()
	defer oe.bufferMutex.Unlock()

	oe.eventBuffer = append(oe.eventBuffer, event)

	if len(oe.eventBuffer) >= oe.config.BatchSize {
		go oe.flushEventBuffer()
	}
}

func (oe *OTELExporter) bufferPatternResult(result *types.PatternResult) {
	oe.bufferMutex.Lock()
	defer oe.bufferMutex.Unlock()

	oe.patternBuffer = append(oe.patternBuffer, result)

	if len(oe.patternBuffer) >= oe.config.BatchSize {
		go oe.flushPatternBuffer()
	}
}

func (oe *OTELExporter) bufferCorrelation(correlation *domain.Correlation) {
	oe.bufferMutex.Lock()
	defer oe.bufferMutex.Unlock()

	oe.correlationBuffer = append(oe.correlationBuffer, correlation)

	if len(oe.correlationBuffer) >= oe.config.BatchSize {
		go oe.flushCorrelationBuffer()
	}
}

func (oe *OTELExporter) flushEventBuffer() {
	oe.bufferMutex.Lock()
	events := make([]*opinionated.OpinionatedEvent, len(oe.eventBuffer))
	copy(events, oe.eventBuffer)
	oe.eventBuffer = oe.eventBuffer[:0]
	oe.bufferMutex.Unlock()

	// Process events in batch
	for _, event := range events {
		// Export to log exporter or custom handler
		oe.exportEventToLog(event)
	}
}

func (oe *OTELExporter) flushPatternBuffer() {
	oe.bufferMutex.Lock()
	patterns := make([]*types.PatternResult, len(oe.patternBuffer))
	copy(patterns, oe.patternBuffer)
	oe.patternBuffer = oe.patternBuffer[:0]
	oe.bufferMutex.Unlock()

	// Process patterns in batch
	for _, pattern := range patterns {
		// Export to log exporter or custom handler
		oe.exportPatternToLog(pattern)
	}
}

func (oe *OTELExporter) flushCorrelationBuffer() {
	oe.bufferMutex.Lock()
	correlations := make([]*domain.Correlation, len(oe.correlationBuffer))
	copy(correlations, oe.correlationBuffer)
	oe.correlationBuffer = oe.correlationBuffer[:0]
	oe.bufferMutex.Unlock()

	// Process correlations in batch
	for _, correlation := range correlations {
		// Export to log exporter or custom handler
		oe.exportCorrelationToLog(correlation)
	}
}

// Start starts the OTEL exporter
func (oe *OTELExporter) Start(ctx context.Context) error {
	oe.mutex.Lock()
	defer oe.mutex.Unlock()

	if oe.running {
		return fmt.Errorf("OTEL exporter already running")
	}

	oe.running = true

	// Start periodic flush goroutine
	go oe.periodicFlush(ctx)

	return nil
}

// Stop stops the OTEL exporter
func (oe *OTELExporter) Stop(ctx context.Context) error {
	oe.mutex.Lock()
	defer oe.mutex.Unlock()

	if !oe.running {
		return nil
	}

	oe.running = false

	// Flush remaining buffers
	oe.flushEventBuffer()
	oe.flushPatternBuffer()
	oe.flushCorrelationBuffer()

	// Shutdown exporters
	if oe.traceExporter != nil {
		oe.traceExporter.Shutdown(ctx)
	}

	if oe.metricExporter != nil {
		oe.metricExporter.Shutdown(ctx)
	}

	return nil
}

// periodicFlush flushes buffers periodically
func (oe *OTELExporter) periodicFlush(ctx context.Context) {
	ticker := time.NewTicker(oe.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			oe.flushEventBuffer()
			oe.flushPatternBuffer()
			oe.flushCorrelationBuffer()

		case <-ctx.Done():
			return
		}
	}
}

// Placeholder implementations for log export
func (oe *OTELExporter) exportEventToLog(event *opinionated.OpinionatedEvent) {
	// Would export to OTEL logs exporter
}

func (oe *OTELExporter) exportPatternToLog(pattern *types.PatternResult) {
	// Would export to OTEL logs exporter
}

func (oe *OTELExporter) exportCorrelationToLog(correlation *domain.Correlation) {
	// Would export to OTEL logs exporter
}
