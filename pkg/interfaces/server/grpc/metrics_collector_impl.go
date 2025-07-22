package grpc

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
)

// PrometheusMetricsCollector implements MetricsCollector with Prometheus-style metrics
type PrometheusMetricsCollector struct {
	logger *zap.Logger

	// Counters
	eventsTotal       map[string]*MetricCounter // by type
	eventsSource      map[string]*MetricCounter // by source
	correlationsTotal map[string]*MetricCounter // by pattern

	// Histograms
	latencyHistograms map[string]*MetricHistogram // by operation

	// Gauges
	activeStreams      atomic.Int64
	storageUtilization AtomicFloat64
	correlationCount   atomic.Int64

	// Configuration
	histogramBuckets []float64

	// Synchronization
	mu sync.RWMutex

	// Statistics
	startTime time.Time
}

// MetricCounter represents a counter metric
type MetricCounter struct {
	name   string
	labels map[string]string
	value  atomic.Uint64
}

// MetricHistogram represents a histogram metric
type MetricHistogram struct {
	name         string
	labels       map[string]string
	buckets      []float64
	bucketCounts []atomic.Uint64
	sum          AtomicFloat64
	count        atomic.Uint64
}

// MetricGauge represents a gauge metric
type MetricGauge struct {
	name   string
	labels map[string]string
	value  AtomicFloat64
}

// NewPrometheusMetricsCollector creates a new metrics collector
func NewPrometheusMetricsCollector(logger *zap.Logger) *PrometheusMetricsCollector {
	return &PrometheusMetricsCollector{
		logger:            logger,
		eventsTotal:       make(map[string]*MetricCounter),
		eventsSource:      make(map[string]*MetricCounter),
		correlationsTotal: make(map[string]*MetricCounter),
		latencyHistograms: make(map[string]*MetricHistogram),
		histogramBuckets:  []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, // seconds
		startTime:         time.Now(),
	}
}

// RecordEvent records an event metric
func (m *PrometheusMetricsCollector) RecordEvent(eventType string, source string) {
	// Record by type
	m.getOrCreateCounter("events_total", map[string]string{"type": eventType}).Inc()

	// Record by source
	m.getOrCreateCounter("events_by_source", map[string]string{"source": source}).Inc()
}

// RecordCorrelation records a correlation metric
func (m *PrometheusMetricsCollector) RecordCorrelation(pattern string, confidence float64) {
	labels := map[string]string{
		"pattern":          pattern,
		"confidence_level": m.getConfidenceLevel(confidence),
	}
	m.getOrCreateCounter("correlations_total", labels).Inc()
}

// RecordLatency records a latency measurement
func (m *PrometheusMetricsCollector) RecordLatency(operation string, duration time.Duration) {
	histogram := m.getOrCreateHistogram("operation_duration_seconds", map[string]string{"operation": operation})
	histogram.Observe(duration.Seconds())
}

// GetMetrics returns metrics for a component
func (m *PrometheusMetricsCollector) GetMetrics(component pb.TapioGetMetricsRequest_Component) ([]*pb.SystemMetric, error) {
	var metrics []*pb.SystemMetric

	switch component {
	case pb.TapioGetMetricsRequest_COMPONENT_ALL:
		metrics = append(metrics, m.getServerMetrics()...)
		metrics = append(metrics, m.getCollectorMetrics()...)
		metrics = append(metrics, m.getCorrelationMetrics()...)
		metrics = append(metrics, m.getStorageMetrics()...)

	case pb.TapioGetMetricsRequest_COMPONENT_SERVER:
		metrics = append(metrics, m.getServerMetrics()...)

	case pb.TapioGetMetricsRequest_COMPONENT_COLLECTORS:
		metrics = append(metrics, m.getCollectorMetrics()...)

	case pb.TapioGetMetricsRequest_COMPONENT_CORRELATION:
		metrics = append(metrics, m.getCorrelationMetrics()...)

	case pb.TapioGetMetricsRequest_COMPONENT_STORAGE:
		metrics = append(metrics, m.getStorageMetrics()...)
	}

	return metrics, nil
}

// Health returns metrics collector health
func (m *PrometheusMetricsCollector) Health() HealthStatus {
	m.mu.RLock()
	counterCount := len(m.eventsTotal) + len(m.eventsSource) + len(m.correlationsTotal)
	histogramCount := len(m.latencyHistograms)
	m.mu.RUnlock()

	totalMetrics := counterCount + histogramCount + 3 // +3 for gauges

	status := pb.HealthStatus_STATUS_HEALTHY
	message := "Metrics collector is healthy"

	// Check if we have too many metrics (cardinality)
	if totalMetrics > 10000 {
		status = pb.HealthStatus_STATUS_DEGRADED
		message = fmt.Sprintf("High metric cardinality: %d metrics", totalMetrics)
	}

	uptime := time.Since(m.startTime)

	return HealthStatus{
		Status:      status,
		Message:     message,
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"total_metrics":     float64(totalMetrics),
			"counter_metrics":   float64(counterCount),
			"histogram_metrics": float64(histogramCount),
			"gauge_metrics":     3,
			"uptime_seconds":    uptime.Seconds(),
		},
	}
}

// UpdateGauge updates a gauge metric
func (m *PrometheusMetricsCollector) UpdateGauge(name string, value float64) {
	switch name {
	case "active_streams":
		m.activeStreams.Store(int64(value))
	case "storage_utilization":
		m.storageUtilization.Store(value)
	case "correlation_count":
		m.correlationCount.Store(int64(value))
	}
}

// getOrCreateCounter gets or creates a counter metric
func (m *PrometheusMetricsCollector) getOrCreateCounter(name string, labels map[string]string) *MetricCounter {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.metricKey(name, labels)

	var counterMap map[string]*MetricCounter
	switch name {
	case "events_total":
		counterMap = m.eventsTotal
	case "events_by_source":
		counterMap = m.eventsSource
	case "correlations_total":
		counterMap = m.correlationsTotal
	default:
		// Create new map if needed
		counterMap = make(map[string]*MetricCounter)
	}

	if counter, exists := counterMap[key]; exists {
		return counter
	}

	counter := &MetricCounter{
		name:   name,
		labels: labels,
	}
	counterMap[key] = counter

	return counter
}

// getOrCreateHistogram gets or creates a histogram metric
func (m *PrometheusMetricsCollector) getOrCreateHistogram(name string, labels map[string]string) *MetricHistogram {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.metricKey(name, labels)

	if histogram, exists := m.latencyHistograms[key]; exists {
		return histogram
	}

	bucketCounts := make([]atomic.Uint64, len(m.histogramBuckets))
	histogram := &MetricHistogram{
		name:         name,
		labels:       labels,
		buckets:      m.histogramBuckets,
		bucketCounts: bucketCounts,
	}
	m.latencyHistograms[key] = histogram

	return histogram
}

// metricKey creates a unique key for a metric
func (m *PrometheusMetricsCollector) metricKey(name string, labels map[string]string) string {
	key := name
	for k, v := range labels {
		key += fmt.Sprintf("_%s_%s", k, v)
	}
	return key
}

// getConfidenceLevel converts confidence to a level
func (m *PrometheusMetricsCollector) getConfidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence >= 0.7:
		return "medium"
	default:
		return "low"
	}
}

// getServerMetrics returns server-specific metrics
func (m *PrometheusMetricsCollector) getServerMetrics() []*pb.SystemMetric {
	metrics := []*pb.SystemMetric{
		{
			Name:      "server_active_streams",
			Component: "server",
			Value:     float64(m.activeStreams.Load()),
			Unit:      "count",
			Labels: map[string]string{
				"service": "tapio",
			},
		},
		{
			Name:      "server_uptime_seconds",
			Component: "server",
			Value:     time.Since(m.startTime).Seconds(),
			Unit:      "seconds",
			Labels: map[string]string{
				"service": "tapio",
			},
		},
	}

	// Add event counters
	m.mu.RLock()
	for _, counter := range m.eventsTotal {
		metrics = append(metrics, &pb.SystemMetric{
			Name:      counter.name,
			Component: "server",
			Value:     float64(counter.value.Load()),
			Unit:      "count",
			Labels:    counter.labels,
		})
	}
	m.mu.RUnlock()

	return metrics
}

// getCollectorMetrics returns collector-specific metrics
func (m *PrometheusMetricsCollector) getCollectorMetrics() []*pb.SystemMetric {
	var metrics []*pb.SystemMetric

	m.mu.RLock()
	for _, counter := range m.eventsSource {
		metrics = append(metrics, &pb.SystemMetric{
			Name:      "collector_events_total",
			Component: "collectors",
			Value:     float64(counter.value.Load()),
			Unit:      "count",
			Labels:    counter.labels,
		})
	}
	m.mu.RUnlock()

	return metrics
}

// getCorrelationMetrics returns correlation-specific metrics
func (m *PrometheusMetricsCollector) getCorrelationMetrics() []*pb.SystemMetric {
	metrics := []*pb.SystemMetric{
		{
			Name:      "correlation_active_count",
			Component: "correlation",
			Value:     float64(m.correlationCount.Load()),
			Unit:      "count",
			Labels: map[string]string{
				"engine": "real_time",
			},
		},
	}

	m.mu.RLock()
	for _, counter := range m.correlationsTotal {
		metrics = append(metrics, &pb.SystemMetric{
			Name:      counter.name,
			Component: "correlation",
			Value:     float64(counter.value.Load()),
			Unit:      "count",
			Labels:    counter.labels,
		})
	}
	m.mu.RUnlock()

	return metrics
}

// getStorageMetrics returns storage-specific metrics
func (m *PrometheusMetricsCollector) getStorageMetrics() []*pb.SystemMetric {
	return []*pb.SystemMetric{
		{
			Name:      "storage_utilization_percent",
			Component: "storage",
			Value:     m.storageUtilization.Load(),
			Unit:      "percent",
			Labels: map[string]string{
				"type": "memory",
			},
		},
	}
}

// Counter methods
func (c *MetricCounter) Inc() {
	c.value.Add(1)
}

func (c *MetricCounter) Add(delta uint64) {
	c.value.Add(delta)
}

func (c *MetricCounter) Get() uint64 {
	return c.value.Load()
}

// Histogram methods
func (h *MetricHistogram) Observe(value float64) {
	// Update sum and count
	h.sum.Add(value)
	h.count.Add(1)

	// Update bucket counts
	for i, threshold := range h.buckets {
		if value <= threshold {
			h.bucketCounts[i].Add(1)
		}
	}
}

func (h *MetricHistogram) GetPercentile(p float64) float64 {
	if p < 0 || p > 1 {
		return 0
	}

	totalCount := h.count.Load()
	if totalCount == 0 {
		return 0
	}

	targetCount := uint64(float64(totalCount) * p)
	cumulativeCount := uint64(0)

	for i, bucketCount := range h.bucketCounts {
		cumulativeCount += bucketCount.Load()
		if cumulativeCount >= targetCount {
			if i == 0 {
				return h.buckets[i] / 2
			}
			// Linear interpolation between buckets
			return (h.buckets[i-1] + h.buckets[i]) / 2
		}
	}

	return h.buckets[len(h.buckets)-1]
}

// GetStatistics returns detailed metrics statistics
func (m *PrometheusMetricsCollector) GetStatistics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate total events
	totalEvents := uint64(0)
	for _, counter := range m.eventsTotal {
		totalEvents += counter.Get()
	}

	// Calculate total correlations
	totalCorrelations := uint64(0)
	for _, counter := range m.correlationsTotal {
		totalCorrelations += counter.Get()
	}

	// Get histogram statistics
	histogramStats := make(map[string]interface{})
	for key, histogram := range m.latencyHistograms {
		count := histogram.count.Load()
		if count > 0 {
			histogramStats[key] = map[string]interface{}{
				"count": count,
				"sum":   histogram.sum.Load(),
				"avg":   histogram.sum.Load() / float64(count),
				"p50":   histogram.GetPercentile(0.5),
				"p90":   histogram.GetPercentile(0.9),
				"p99":   histogram.GetPercentile(0.99),
			}
		}
	}

	return map[string]interface{}{
		"total_events":        totalEvents,
		"total_correlations":  totalCorrelations,
		"active_streams":      m.activeStreams.Load(),
		"storage_utilization": m.storageUtilization.Load(),
		"correlation_count":   m.correlationCount.Load(),
		"uptime_seconds":      time.Since(m.startTime).Seconds(),
		"histograms":          histogramStats,
		"metric_count": map[string]int{
			"counters":   len(m.eventsTotal) + len(m.eventsSource) + len(m.correlationsTotal),
			"histograms": len(m.latencyHistograms),
			"gauges":     3,
		},
	}
}
