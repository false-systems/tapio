package pipeline

import (
	"sync"
	"sync/atomic"
	"time"
)

// PipelineMetrics contains comprehensive performance metrics for the pipeline
type PipelineMetrics struct {
	// Event processing metrics
	EventsReceived  int64 `json:"events_received"`
	EventsProcessed int64 `json:"events_processed"`
	EventsDropped   int64 `json:"events_dropped"`
	EventsFailed    int64 `json:"events_failed"`

	// Stage-specific metrics
	EventsValidated    int64 `json:"events_validated"`
	EventsContextBuilt int64 `json:"events_context_built"`
	EventsCorrelated   int64 `json:"events_correlated"`

	// Error metrics by stage
	ValidationErrors  int64 `json:"validation_errors"`
	ContextErrors     int64 `json:"context_errors"`
	CorrelationErrors int64 `json:"correlation_errors"`

	// Performance metrics
	ThroughputPerSecond float64       `json:"throughput_per_second"`
	AverageLatency      time.Duration `json:"average_latency"`
	P50Latency          time.Duration `json:"p50_latency"`
	P95Latency          time.Duration `json:"p95_latency"`
	P99Latency          time.Duration `json:"p99_latency"`
	MaxLatency          time.Duration `json:"max_latency"`

	// Resource utilization
	ActiveWorkers   int     `json:"active_workers"`
	QueueDepth      int     `json:"queue_depth"`
	QueueCapacity   int     `json:"queue_capacity"`
	MemoryUsageMB   float64 `json:"memory_usage_mb"`
	CPUUsagePercent float64 `json:"cpu_usage_percent"`

	// Circuit breaker metrics
	CircuitBreakerState string `json:"circuit_breaker_state"`
	CircuitBreakerTrips int64  `json:"circuit_breaker_trips"`

	// Timing information
	StartTime      time.Time     `json:"start_time"`
	Uptime         time.Duration `json:"uptime"`
	LastResetTime  time.Time     `json:"last_reset_time"`
	LastUpdateTime time.Time     `json:"last_update_time"`

	// Batch processing metrics
	BatchesProcessed int64   `json:"batches_processed"`
	AverageBatchSize float64 `json:"average_batch_size"`
	MaxBatchSize     int     `json:"max_batch_size"`

	// Error rate calculation
	ErrorRate            float64 `json:"error_rate"`
	ValidationErrorRate  float64 `json:"validation_error_rate"`
	ContextErrorRate     float64 `json:"context_error_rate"`
	CorrelationErrorRate float64 `json:"correlation_error_rate"`
}

// MetricsCollector handles metric collection and calculation
type MetricsCollector struct {
	metrics *PipelineMetrics
	mu      sync.RWMutex

	// Latency tracking
	latencyBuckets     []time.Duration
	latencyMu          sync.Mutex
	maxLatencyTracking int

	// Rate calculation
	lastCalculation time.Time
	lastProcessed   int64

	// Batch tracking
	totalBatchSize int64
	batchCount     int64
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: &PipelineMetrics{
			StartTime:      time.Now(),
			LastResetTime:  time.Now(),
			LastUpdateTime: time.Now(),
		},
		latencyBuckets:     make([]time.Duration, 0, 10000),
		maxLatencyTracking: 10000,
		lastCalculation:    time.Now(),
	}
}

// IncrementReceived increments the events received counter
func (mc *MetricsCollector) IncrementReceived(count int64) {
	atomic.AddInt64(&mc.metrics.EventsReceived, count)
}

// IncrementProcessed increments the events processed counter
func (mc *MetricsCollector) IncrementProcessed(count int64) {
	atomic.AddInt64(&mc.metrics.EventsProcessed, count)
}

// IncrementDropped increments the events dropped counter
func (mc *MetricsCollector) IncrementDropped(count int64) {
	atomic.AddInt64(&mc.metrics.EventsDropped, count)
}

// IncrementFailed increments the events failed counter
func (mc *MetricsCollector) IncrementFailed(count int64) {
	atomic.AddInt64(&mc.metrics.EventsFailed, count)
}

// IncrementValidated increments the events validated counter
func (mc *MetricsCollector) IncrementValidated(count int64) {
	atomic.AddInt64(&mc.metrics.EventsValidated, count)
}

// IncrementContextBuilt increments the events context built counter
func (mc *MetricsCollector) IncrementContextBuilt(count int64) {
	atomic.AddInt64(&mc.metrics.EventsContextBuilt, count)
}

// IncrementCorrelated increments the events correlated counter
func (mc *MetricsCollector) IncrementCorrelated(count int64) {
	atomic.AddInt64(&mc.metrics.EventsCorrelated, count)
}

// IncrementValidationErrors increments the validation errors counter
func (mc *MetricsCollector) IncrementValidationErrors(count int64) {
	atomic.AddInt64(&mc.metrics.ValidationErrors, count)
}

// IncrementContextErrors increments the context errors counter
func (mc *MetricsCollector) IncrementContextErrors(count int64) {
	atomic.AddInt64(&mc.metrics.ContextErrors, count)
}

// IncrementCorrelationErrors increments the correlation errors counter
func (mc *MetricsCollector) IncrementCorrelationErrors(count int64) {
	atomic.AddInt64(&mc.metrics.CorrelationErrors, count)
}

// RecordLatency records a processing latency
func (mc *MetricsCollector) RecordLatency(latency time.Duration) {
	mc.latencyMu.Lock()
	defer mc.latencyMu.Unlock()

	// Add to buckets
	if len(mc.latencyBuckets) < mc.maxLatencyTracking {
		mc.latencyBuckets = append(mc.latencyBuckets, latency)
	} else {
		// Replace oldest entry (simple circular buffer)
		mc.latencyBuckets[len(mc.latencyBuckets)%mc.maxLatencyTracking] = latency
	}

	// Update max latency
	if latency > mc.metrics.MaxLatency {
		mc.metrics.MaxLatency = latency
	}
}

// RecordBatch records a batch processing
func (mc *MetricsCollector) RecordBatch(size int) {
	atomic.AddInt64(&mc.metrics.BatchesProcessed, 1)
	atomic.AddInt64(&mc.totalBatchSize, int64(size))
	atomic.AddInt64(&mc.batchCount, 1)

	mc.mu.Lock()
	if size > mc.metrics.MaxBatchSize {
		mc.metrics.MaxBatchSize = size
	}
	mc.mu.Unlock()
}

// UpdateQueueMetrics updates queue-related metrics
func (mc *MetricsCollector) UpdateQueueMetrics(depth, capacity int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics.QueueDepth = depth
	mc.metrics.QueueCapacity = capacity
}

// UpdateWorkerMetrics updates worker-related metrics
func (mc *MetricsCollector) UpdateWorkerMetrics(active int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics.ActiveWorkers = active
}

// UpdateCircuitBreakerState updates circuit breaker state
func (mc *MetricsCollector) UpdateCircuitBreakerState(state string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics.CircuitBreakerState = state
}

// IncrementCircuitBreakerTrips increments circuit breaker trips
func (mc *MetricsCollector) IncrementCircuitBreakerTrips() {
	atomic.AddInt64(&mc.metrics.CircuitBreakerTrips, 1)
}

// Calculate performs metric calculations
func (mc *MetricsCollector) Calculate() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	mc.metrics.LastUpdateTime = now
	mc.metrics.Uptime = now.Sub(mc.metrics.StartTime)

	// Calculate throughput
	duration := now.Sub(mc.lastCalculation)
	if duration > 0 {
		currentProcessed := atomic.LoadInt64(&mc.metrics.EventsProcessed)
		delta := currentProcessed - mc.lastProcessed
		mc.metrics.ThroughputPerSecond = float64(delta) / duration.Seconds()
		mc.lastProcessed = currentProcessed
		mc.lastCalculation = now
	}

	// Calculate error rates
	totalEvents := float64(atomic.LoadInt64(&mc.metrics.EventsReceived))
	if totalEvents > 0 {
		mc.metrics.ErrorRate = float64(atomic.LoadInt64(&mc.metrics.EventsFailed)) / totalEvents
		mc.metrics.ValidationErrorRate = float64(atomic.LoadInt64(&mc.metrics.ValidationErrors)) / totalEvents
		mc.metrics.ContextErrorRate = float64(atomic.LoadInt64(&mc.metrics.ContextErrors)) / totalEvents
		mc.metrics.CorrelationErrorRate = float64(atomic.LoadInt64(&mc.metrics.CorrelationErrors)) / totalEvents
	}

	// Calculate average batch size
	batchCount := atomic.LoadInt64(&mc.batchCount)
	if batchCount > 0 {
		mc.metrics.AverageBatchSize = float64(atomic.LoadInt64(&mc.totalBatchSize)) / float64(batchCount)
	}

	// Calculate latency percentiles
	mc.calculateLatencyPercentiles()
}

// calculateLatencyPercentiles calculates latency percentiles
func (mc *MetricsCollector) calculateLatencyPercentiles() {
	mc.latencyMu.Lock()
	defer mc.latencyMu.Unlock()

	if len(mc.latencyBuckets) == 0 {
		return
	}

	// Create a copy and sort
	latencies := make([]time.Duration, len(mc.latencyBuckets))
	copy(latencies, mc.latencyBuckets)
	sortDurations(latencies)

	// Calculate percentiles
	mc.metrics.P50Latency = percentile(latencies, 0.50)
	mc.metrics.P95Latency = percentile(latencies, 0.95)
	mc.metrics.P99Latency = percentile(latencies, 0.99)

	// Calculate average
	var sum time.Duration
	for _, d := range latencies {
		sum += d
	}
	mc.metrics.AverageLatency = sum / time.Duration(len(latencies))
}

// GetMetrics returns a copy of the current metrics
func (mc *MetricsCollector) GetMetrics() PipelineMetrics {
	mc.Calculate()

	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return *mc.metrics
}

// Reset resets all metrics
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Preserve start time
	startTime := mc.metrics.StartTime

	// Reset metrics
	mc.metrics = &PipelineMetrics{
		StartTime:      startTime,
		LastResetTime:  time.Now(),
		LastUpdateTime: time.Now(),
	}

	// Reset tracking
	mc.latencyBuckets = mc.latencyBuckets[:0]
	mc.lastCalculation = time.Now()
	mc.lastProcessed = 0
	mc.totalBatchSize = 0
	mc.batchCount = 0
}

// Helper functions

// sortDurations sorts a slice of durations
func sortDurations(durations []time.Duration) {
	// Simple insertion sort for small slices
	for i := 1; i < len(durations); i++ {
		key := durations[i]
		j := i - 1
		for j >= 0 && durations[j] > key {
			durations[j+1] = durations[j]
			j--
		}
		durations[j+1] = key
	}
}

// percentile calculates the percentile value
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := int(float64(len(sorted)-1) * p)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}
