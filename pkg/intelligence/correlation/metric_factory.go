package correlation

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// EngineOTELMetrics holds all OpenTelemetry metrics for the correlation engine
type EngineOTELMetrics struct {
	// Core processing metrics
	EventsProcessedCtr   metric.Int64Counter
	ErrorsTotalCtr       metric.Int64Counter
	ProcessingTimeHist   metric.Float64Histogram
	CorrelationsFoundCtr metric.Int64Counter

	// Queue and worker metrics
	QueueDepthGauge    metric.Int64UpDownCounter
	ActiveWorkersGauge metric.Int64UpDownCounter

	// Storage worker pool metrics
	StorageQueueDepthGauge metric.Int64UpDownCounter
	StorageWorkersGauge    metric.Int64UpDownCounter
	StorageProcessedCtr    metric.Int64Counter
	StorageRejectedCtr     metric.Int64Counter
	StorageLatencyHist     metric.Float64Histogram
}

// MetricFactory creates and initializes OpenTelemetry metrics for the correlation engine
type MetricFactory struct {
	logger *zap.Logger
	meter  metric.Meter
}

// NewMetricFactory creates a new metric factory with the given component name
func NewMetricFactory(componentName string, logger *zap.Logger) *MetricFactory {
	return &MetricFactory{
		logger: logger,
		meter:  otel.Meter(componentName),
	}
}

// CreateEngineMetrics initializes all correlation engine metrics
// This replaces the complex metric initialization in the Engine constructor
func (f *MetricFactory) CreateEngineMetrics() (*EngineOTELMetrics, error) {
	metrics := &EngineOTELMetrics{}

	// Initialize core processing metrics
	if err := f.initProcessingMetrics(metrics); err != nil {
		return nil, err
	}

	// Initialize queue and worker metrics
	if err := f.initQueueMetrics(metrics); err != nil {
		return nil, err
	}

	// Initialize storage worker pool metrics
	if err := f.initStorageMetrics(metrics); err != nil {
		return nil, err
	}

	f.logger.Info("Engine metrics initialized successfully",
		zap.String("component", "correlation-engine"))

	return metrics, nil
}

// initProcessingMetrics initializes core event processing metrics
func (f *MetricFactory) initProcessingMetrics(metrics *EngineOTELMetrics) error {
	var err error

	// Events processed counter
	metrics.EventsProcessedCtr, err = f.meter.Int64Counter(
		"correlation_events_processed_total",
		metric.WithDescription("Total events processed by correlation engine"),
	)
	if err != nil {
		f.logger.Warn("Failed to create events counter", zap.Error(err))
		return err
	}

	// Errors counter
	metrics.ErrorsTotalCtr, err = f.meter.Int64Counter(
		"correlation_errors_total",
		metric.WithDescription("Total errors in correlation engine"),
	)
	if err != nil {
		f.logger.Warn("Failed to create errors counter", zap.Error(err))
		return err
	}

	// Processing time histogram
	metrics.ProcessingTimeHist, err = f.meter.Float64Histogram(
		"correlation_processing_duration_ms",
		metric.WithDescription("Processing duration for correlation engine in milliseconds"),
	)
	if err != nil {
		f.logger.Warn("Failed to create processing time histogram", zap.Error(err))
		return err
	}

	// Correlations found counter
	metrics.CorrelationsFoundCtr, err = f.meter.Int64Counter(
		"correlation_correlations_found_total",
		metric.WithDescription("Total correlations found by correlation engine"),
	)
	if err != nil {
		f.logger.Warn("Failed to create correlations found counter", zap.Error(err))
		return err
	}

	return nil
}

// initQueueMetrics initializes queue depth and worker metrics
func (f *MetricFactory) initQueueMetrics(metrics *EngineOTELMetrics) error {
	var err error

	// Queue depth gauge
	metrics.QueueDepthGauge, err = f.meter.Int64UpDownCounter(
		"correlation_queue_depth",
		metric.WithDescription("Current depth of event processing queue"),
	)
	if err != nil {
		f.logger.Warn("Failed to create queue depth gauge", zap.Error(err))
		return err
	}

	// Active workers gauge
	metrics.ActiveWorkersGauge, err = f.meter.Int64UpDownCounter(
		"correlation_active_workers",
		metric.WithDescription("Number of active correlation workers"),
	)
	if err != nil {
		f.logger.Warn("Failed to create active workers gauge", zap.Error(err))
		return err
	}

	return nil
}

// initStorageMetrics initializes storage worker pool metrics
func (f *MetricFactory) initStorageMetrics(metrics *EngineOTELMetrics) error {
	var err error

	// Storage queue depth gauge
	metrics.StorageQueueDepthGauge, err = f.meter.Int64UpDownCounter(
		"correlation_storage_queue_depth",
		metric.WithDescription("Current depth of storage job queue"),
	)
	if err != nil {
		f.logger.Warn("Failed to create storage queue depth gauge", zap.Error(err))
		return err
	}

	// Storage workers gauge
	metrics.StorageWorkersGauge, err = f.meter.Int64UpDownCounter(
		"correlation_storage_workers",
		metric.WithDescription("Number of active storage workers"),
	)
	if err != nil {
		f.logger.Warn("Failed to create storage workers gauge", zap.Error(err))
		return err
	}

	// Storage operations processed counter
	metrics.StorageProcessedCtr, err = f.meter.Int64Counter(
		"correlation_storage_processed_total",
		metric.WithDescription("Total storage operations processed"),
	)
	if err != nil {
		f.logger.Warn("Failed to create storage processed counter", zap.Error(err))
		return err
	}

	// Storage operations rejected counter
	metrics.StorageRejectedCtr, err = f.meter.Int64Counter(
		"correlation_storage_rejected_total",
		metric.WithDescription("Total storage operations rejected due to queue full"),
	)
	if err != nil {
		f.logger.Warn("Failed to create storage rejected counter", zap.Error(err))
		return err
	}

	// Storage latency histogram
	metrics.StorageLatencyHist, err = f.meter.Float64Histogram(
		"correlation_storage_latency_ms",
		metric.WithDescription("Storage operation latency in milliseconds"),
	)
	if err != nil {
		f.logger.Warn("Failed to create storage latency histogram", zap.Error(err))
		return err
	}

	return nil
}

// CreateTestMetrics creates a minimal metric set for testing
// This returns nil metrics to avoid OTEL setup complexity in tests
func CreateTestMetrics() *EngineOTELMetrics {
	return &EngineOTELMetrics{
		// All metrics will be nil, engine code checks for nil before use
		// This follows the graceful degradation pattern already in the engine
	}
}
