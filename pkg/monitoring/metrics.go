package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector provides comprehensive metrics collection
type MetricsCollector struct {
	// Event processing metrics
	EventsProcessed    *prometheus.CounterVec
	EventsDropped      *prometheus.CounterVec
	ProcessingDuration *prometheus.HistogramVec
	ProcessingErrors   *prometheus.CounterVec
	
	// System metrics
	ActiveConnections  *prometheus.GaugeVec
	MemoryUsage        *prometheus.GaugeVec
	CPUUsage           *prometheus.GaugeVec
	GoroutineCount     prometheus.Gauge
	
	// Business metrics
	CorrelationsActive   *prometheus.GaugeVec
	CorrelationsCreated  *prometheus.CounterVec
	InsightsGenerated    *prometheus.CounterVec
	SignalToNoiseRatio   *prometheus.GaugeVec
	
	// Performance metrics
	CacheHitRate      *prometheus.GaugeVec
	QueueDepth        *prometheus.GaugeVec
	ResponseTime      *prometheus.HistogramVec
	ThroughputRate    *prometheus.GaugeVec
	
	// eBPF metrics
	EBPFProgramsLoaded  prometheus.Gauge
	EBPFEventsReceived  *prometheus.CounterVec
	EBPFBufferUtilization *prometheus.GaugeVec
	EBPFDroppedEvents   *prometheus.CounterVec
	
	// Custom metrics registry
	customMetrics map[string]prometheus.Collector
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(config MetricsConfig) *MetricsCollector {
	mc := &MetricsCollector{
		customMetrics: make(map[string]prometheus.Collector),
	}

	// Initialize event processing metrics
	mc.EventsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_events_processed_total",
			Help: "Total number of events processed",
		},
		[]string{"node", "type", "source"},
	)

	mc.EventsDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_events_dropped_total",
			Help: "Total number of events dropped",
		},
		[]string{"node", "reason"},
	)

	mc.ProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tapio_event_processing_duration_seconds",
			Help:    "Event processing duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		},
		[]string{"stage", "type"},
	)

	mc.ProcessingErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_processing_errors_total",
			Help: "Total number of processing errors",
		},
		[]string{"stage", "error_type"},
	)

	// Initialize system metrics
	mc.ActiveConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_active_connections",
			Help: "Number of active connections",
		},
		[]string{"type", "state"},
	)

	mc.MemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_memory_usage_bytes",
			Help: "Memory usage in bytes",
		},
		[]string{"component", "type"},
	)

	mc.CPUUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_cpu_usage_percent",
			Help: "CPU usage percentage",
		},
		[]string{"component", "core"},
	)

	mc.GoroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tapio_goroutines_total",
			Help: "Total number of goroutines",
		},
	)

	// Initialize business metrics
	mc.CorrelationsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_correlations_active",
			Help: "Number of active correlations",
		},
		[]string{"type", "status"},
	)

	mc.CorrelationsCreated = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_correlations_created_total",
			Help: "Total number of correlations created",
		},
		[]string{"type", "confidence"},
	)

	mc.InsightsGenerated = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_insights_generated_total",
			Help: "Total number of insights generated",
		},
		[]string{"category", "severity"},
	)

	mc.SignalToNoiseRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_signal_to_noise_ratio",
			Help: "Signal to noise ratio",
		},
		[]string{"source", "type"},
	)

	// Initialize performance metrics
	mc.CacheHitRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_cache_hit_rate",
			Help: "Cache hit rate percentage",
		},
		[]string{"cache_name"},
	)

	mc.QueueDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_queue_depth",
			Help: "Current queue depth",
		},
		[]string{"queue_name", "priority"},
	)

	mc.ResponseTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tapio_response_time_seconds",
			Help:    "Response time in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"endpoint", "method"},
	)

	mc.ThroughputRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_throughput_rate",
			Help: "Current throughput rate per second",
		},
		[]string{"component", "operation"},
	)

	// Initialize eBPF metrics
	mc.EBPFProgramsLoaded = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tapio_ebpf_programs_loaded",
			Help: "Number of eBPF programs loaded",
		},
	)

	mc.EBPFEventsReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_ebpf_events_received_total",
			Help: "Total number of eBPF events received",
		},
		[]string{"program", "type"},
	)

	mc.EBPFBufferUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_ebpf_buffer_utilization_percent",
			Help: "eBPF ring buffer utilization percentage",
		},
		[]string{"cpu", "buffer_type"},
	)

	mc.EBPFDroppedEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tapio_ebpf_events_dropped_total",
			Help: "Total number of eBPF events dropped",
		},
		[]string{"program", "reason"},
	)

	return mc
}

// RegisterCustomMetric registers a custom metric
func (mc *MetricsCollector) RegisterCustomMetric(name string, metric prometheus.Collector) error {
	if _, exists := mc.customMetrics[name]; exists {
		return fmt.Errorf("metric %s already registered", name)
	}

	if err := prometheus.Register(metric); err != nil {
		return fmt.Errorf("failed to register metric %s: %w", name, err)
	}

	mc.customMetrics[name] = metric
	return nil
}

// RecordEventProcessed records a processed event
func (mc *MetricsCollector) RecordEventProcessed(node, eventType, source string) {
	mc.EventsProcessed.WithLabelValues(node, eventType, source).Inc()
}

// RecordEventDropped records a dropped event
func (mc *MetricsCollector) RecordEventDropped(node, reason string) {
	mc.EventsDropped.WithLabelValues(node, reason).Inc()
}

// RecordProcessingDuration records processing duration
func (mc *MetricsCollector) RecordProcessingDuration(stage, eventType string, duration float64) {
	mc.ProcessingDuration.WithLabelValues(stage, eventType).Observe(duration)
}

// RecordProcessingError records a processing error
func (mc *MetricsCollector) RecordProcessingError(stage, errorType string) {
	mc.ProcessingErrors.WithLabelValues(stage, errorType).Inc()
}

// UpdateActiveConnections updates active connection count
func (mc *MetricsCollector) UpdateActiveConnections(connType, state string, count float64) {
	mc.ActiveConnections.WithLabelValues(connType, state).Set(count)
}

// UpdateMemoryUsage updates memory usage
func (mc *MetricsCollector) UpdateMemoryUsage(component, memType string, bytes float64) {
	mc.MemoryUsage.WithLabelValues(component, memType).Set(bytes)
}

// UpdateCPUUsage updates CPU usage
func (mc *MetricsCollector) UpdateCPUUsage(component, core string, percent float64) {
	mc.CPUUsage.WithLabelValues(component, core).Set(percent)
}

// UpdateGoroutineCount updates goroutine count
func (mc *MetricsCollector) UpdateGoroutineCount(count float64) {
	mc.GoroutineCount.Set(count)
}

// RecordCorrelationCreated records a correlation creation
func (mc *MetricsCollector) RecordCorrelationCreated(corrType, confidence string) {
	mc.CorrelationsCreated.WithLabelValues(corrType, confidence).Inc()
}

// UpdateActiveCorrelations updates active correlation count
func (mc *MetricsCollector) UpdateActiveCorrelations(corrType, status string, count float64) {
	mc.CorrelationsActive.WithLabelValues(corrType, status).Set(count)
}

// RecordInsightGenerated records an insight generation
func (mc *MetricsCollector) RecordInsightGenerated(category, severity string) {
	mc.InsightsGenerated.WithLabelValues(category, severity).Inc()
}

// UpdateSignalToNoiseRatio updates signal to noise ratio
func (mc *MetricsCollector) UpdateSignalToNoiseRatio(source, dataType string, ratio float64) {
	mc.SignalToNoiseRatio.WithLabelValues(source, dataType).Set(ratio)
}

// UpdateCacheHitRate updates cache hit rate
func (mc *MetricsCollector) UpdateCacheHitRate(cacheName string, rate float64) {
	mc.CacheHitRate.WithLabelValues(cacheName).Set(rate)
}

// UpdateQueueDepth updates queue depth
func (mc *MetricsCollector) UpdateQueueDepth(queueName, priority string, depth float64) {
	mc.QueueDepth.WithLabelValues(queueName, priority).Set(depth)
}

// RecordResponseTime records response time
func (mc *MetricsCollector) RecordResponseTime(endpoint, method string, duration float64) {
	mc.ResponseTime.WithLabelValues(endpoint, method).Observe(duration)
}

// UpdateThroughputRate updates throughput rate
func (mc *MetricsCollector) UpdateThroughputRate(component, operation string, rate float64) {
	mc.ThroughputRate.WithLabelValues(component, operation).Set(rate)
}

// UpdateEBPFProgramsLoaded updates eBPF programs loaded count
func (mc *MetricsCollector) UpdateEBPFProgramsLoaded(count float64) {
	mc.EBPFProgramsLoaded.Set(count)
}

// RecordEBPFEventReceived records an eBPF event
func (mc *MetricsCollector) RecordEBPFEventReceived(program, eventType string) {
	mc.EBPFEventsReceived.WithLabelValues(program, eventType).Inc()
}

// UpdateEBPFBufferUtilization updates eBPF buffer utilization
func (mc *MetricsCollector) UpdateEBPFBufferUtilization(cpu, bufferType string, percent float64) {
	mc.EBPFBufferUtilization.WithLabelValues(cpu, bufferType).Set(percent)
}

// RecordEBPFEventDropped records a dropped eBPF event
func (mc *MetricsCollector) RecordEBPFEventDropped(program, reason string) {
	mc.EBPFDroppedEvents.WithLabelValues(program, reason).Inc()
}

// MetricsConfig defines metrics configuration
type MetricsConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Address           string            `yaml:"address"`
	Path              string            `yaml:"path"`
	PushGateway       string            `yaml:"push_gateway"`
	PushInterval      time.Duration     `yaml:"push_interval"`
	CustomLabels      map[string]string `yaml:"custom_labels"`
	HistogramBuckets  []float64         `yaml:"histogram_buckets"`
	EnableGoMetrics   bool              `yaml:"enable_go_metrics"`
	EnableProcessMetrics bool           `yaml:"enable_process_metrics"`
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:              true,
		Address:              ":9090",
		Path:                 "/metrics",
		EnableGoMetrics:      true,
		EnableProcessMetrics: true,
		CustomLabels:         make(map[string]string),
	}
}