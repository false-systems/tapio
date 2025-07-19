package monitoring

import (
	"context"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

// PerformanceMonitor tracks and analyzes performance metrics
type PerformanceMonitor struct {
	mu             sync.RWMutex
	metrics        map[string]*Metric
	samplingRate   float64
	bufferSize     int
	flushInterval  time.Duration
	onAlert        func(Alert)
	onMetricUpdate func(string, *Metric)
	alerts         []Alert
	running        bool
	stopCh         chan struct{}
	config         *MonitorConfig
}

// Metric represents a performance metric with statistical data
type Metric struct {
	Name       string            `json:"name"`
	Type       MetricType        `json:"type"`
	Value      float64           `json:"value"`
	Count      int64             `json:"count"`
	Sum        float64           `json:"sum"`
	Min        float64           `json:"min"`
	Max        float64           `json:"max"`
	Avg        float64           `json:"avg"`
	P50        float64           `json:"p50"`
	P95        float64           `json:"p95"`
	P99        float64           `json:"p99"`
	LastUpdate time.Time         `json:"last_update"`
	Unit       string            `json:"unit"`
	Labels     map[string]string `json:"labels,omitempty"`
	History    *CircularBuffer   `json:"-"`
	mu         sync.RWMutex
}

// MetricType defines the type of metric
type MetricType int

const (
	MetricTypeCounter MetricType = iota
	MetricTypeGauge
	MetricTypeHistogram
	MetricTypeSummary
	MetricTypeTiming
)

func (mt MetricType) String() string {
	switch mt {
	case MetricTypeCounter:
		return "counter"
	case MetricTypeGauge:
		return "gauge"
	case MetricTypeHistogram:
		return "histogram"
	case MetricTypeSummary:
		return "summary"
	case MetricTypeTiming:
		return "timing"
	default:
		return "unknown"
	}
}

// Alert and AlertLevel types are now defined in types.go

// Alert types are now defined in types.go

func (at AlertType) String() string {
	return string(at)
}

// MonitorConfig configures the performance monitor
type MonitorConfig struct {
	SamplingRate       float64              // Sampling rate for metrics (0.0-1.0)
	BufferSize         int                  // Size of metric history buffers
	FlushInterval      time.Duration        // How often to flush/process metrics
	EnableRuntimeStats bool                 // Collect Go runtime statistics
	EnableMemoryAlerts bool                 // Enable memory leak detection
	EnableCPUAlerts    bool                 // Enable CPU usage alerts
	Thresholds         map[string]Threshold // Alert thresholds per metric
}

// Threshold defines alert thresholds for metrics
type Threshold struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
	Unit     string  `json:"unit"`
}

// CircularBuffer implements a thread-safe circular buffer for metric history
type CircularBuffer struct {
	data  []float64
	size  int
	head  int
	count int
	mu    sync.RWMutex
}

// NewCircularBuffer creates a new circular buffer
func NewCircularBuffer(size int) *CircularBuffer {
	return &CircularBuffer{
		data: make([]float64, size),
		size: size,
	}
}

// Add adds a value to the buffer
func (cb *CircularBuffer) Add(value float64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.data[cb.head] = value
	cb.head = (cb.head + 1) % cb.size
	if cb.count < cb.size {
		cb.count++
	}
}

// GetValues returns all values in the buffer
func (cb *CircularBuffer) GetValues() []float64 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.count == 0 {
		return nil
	}

	result := make([]float64, cb.count)
	if cb.count < cb.size {
		copy(result, cb.data[:cb.count])
	} else {
		copy(result, cb.data[cb.head:])
		copy(result[cb.size-cb.head:], cb.data[:cb.head])
	}
	return result
}

// GetPercentile calculates the nth percentile
func (cb *CircularBuffer) GetPercentile(percentile float64) float64 {
	values := cb.GetValues()
	if len(values) == 0 {
		return 0
	}

	// Simple percentile calculation (for production, use a proper sorting algorithm)
	// This is a simplified implementation
	n := len(values)
	if n == 1 {
		return values[0]
	}

	// Sort values (simplified bubble sort for small datasets)
	sorted := make([]float64, len(values))
	copy(sorted, values)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	index := int(percentile * float64(n-1))
	if index >= n {
		index = n - 1
	}
	return sorted[index]
}

// DefaultMonitorConfig returns sensible defaults
func DefaultMonitorConfig() *MonitorConfig {
	return &MonitorConfig{
		SamplingRate:       1.0,
		BufferSize:         1000,
		FlushInterval:      10 * time.Second,
		EnableRuntimeStats: true,
		EnableMemoryAlerts: true,
		EnableCPUAlerts:    true,
		Thresholds: map[string]Threshold{
			"memory_usage_mb": {
				Warning:  100,
				Critical: 200,
				Unit:     "MB",
			},
			"goroutines": {
				Warning:  1000,
				Critical: 5000,
				Unit:     "count",
			},
			"gc_pause_ms": {
				Warning:  100,
				Critical: 500,
				Unit:     "ms",
			},
			"request_duration_ms": {
				Warning:  1000,
				Critical: 5000,
				Unit:     "ms",
			},
		},
	}
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(config *MonitorConfig) *PerformanceMonitor {
	if config == nil {
		config = DefaultMonitorConfig()
	}

	pm := &PerformanceMonitor{
		metrics:       make(map[string]*Metric),
		samplingRate:  config.SamplingRate,
		bufferSize:    config.BufferSize,
		flushInterval: config.FlushInterval,
		stopCh:        make(chan struct{}),
		config:        config,
	}

	return pm
}

// Start starts the performance monitor
func (pm *PerformanceMonitor) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return nil
	}

	pm.running = true

	// Start metric collection goroutine
	go pm.collectMetrics(ctx)

	// Start runtime stats collection if enabled
	if pm.config.EnableRuntimeStats {
		go pm.collectRuntimeStats(ctx)
	}

	// Start alert processing
	go pm.processAlerts(ctx)

	return nil
}

// Stop stops the performance monitor
func (pm *PerformanceMonitor) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	pm.running = false
	close(pm.stopCh)
	return nil
}

// RecordMetric records a metric value
func (pm *PerformanceMonitor) RecordMetric(name string, value float64, metricType MetricType, unit string, labels map[string]string) {
	// Apply sampling
	if pm.samplingRate < 1.0 && rand.Float64() > pm.samplingRate {
		return
	}

	pm.mu.Lock()
	metric, exists := pm.metrics[name]
	if !exists {
		metric = &Metric{
			Name:    name,
			Type:    metricType,
			Unit:    unit,
			Labels:  labels,
			History: NewCircularBuffer(pm.bufferSize),
			Min:     value,
			Max:     value,
		}
		pm.metrics[name] = metric
	}
	pm.mu.Unlock()

	// Update metric
	metric.mu.Lock()
	defer metric.mu.Unlock()

	metric.Value = value
	metric.Count++
	metric.Sum += value
	metric.LastUpdate = time.Now()

	if value < metric.Min {
		metric.Min = value
	}
	if value > metric.Max {
		metric.Max = value
	}

	metric.Avg = metric.Sum / float64(metric.Count)
	metric.History.Add(value)

	// Calculate percentiles
	metric.P50 = metric.History.GetPercentile(0.50)
	metric.P95 = metric.History.GetPercentile(0.95)
	metric.P99 = metric.History.GetPercentile(0.99)

	// Check for alerts
	pm.checkThresholds(name, metric)

	// Call metric update callback
	if pm.onMetricUpdate != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Log panic but don't crash
				}
			}()
			pm.onMetricUpdate(name, metric)
		}()
	}
}

// Counter increments a counter metric
func (pm *PerformanceMonitor) Counter(name string, labels map[string]string) {
	pm.RecordMetric(name, 1, MetricTypeCounter, "count", labels)
}

// Gauge sets a gauge metric value
func (pm *PerformanceMonitor) Gauge(name string, value float64, unit string, labels map[string]string) {
	pm.RecordMetric(name, value, MetricTypeGauge, unit, labels)
}

// Timing records a timing metric
func (pm *PerformanceMonitor) Timing(name string, duration time.Duration, labels map[string]string) {
	pm.RecordMetric(name, float64(duration.Nanoseconds())/1e6, MetricTypeTiming, "ms", labels)
}

// MeasureFunc measures the execution time of a function
func (pm *PerformanceMonitor) MeasureFunc(name string, fn func(), labels map[string]string) {
	start := time.Now()
	defer func() {
		pm.Timing(name, time.Since(start), labels)
	}()
	fn()
}

// MeasureFuncWithError measures function execution and records errors
func (pm *PerformanceMonitor) MeasureFuncWithError(name string, fn func() error, labels map[string]string) error {
	start := time.Now()
	defer func() {
		pm.Timing(name, time.Since(start), labels)
	}()

	err := fn()
	if err != nil {
		errorLabels := make(map[string]string)
		for k, v := range labels {
			errorLabels[k] = v
		}
		errorLabels["error"] = "true"
		pm.Counter(name+"_errors", errorLabels)
	}

	return err
}

// GetMetric returns a metric by name
func (pm *PerformanceMonitor) GetMetric(name string) (*Metric, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	metric, exists := pm.metrics[name]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	metric.mu.RLock()
	defer metric.mu.RUnlock()

	return &Metric{
		Name:       metric.Name,
		Type:       metric.Type,
		Value:      metric.Value,
		Count:      metric.Count,
		Sum:        metric.Sum,
		Min:        metric.Min,
		Max:        metric.Max,
		Avg:        metric.Avg,
		P50:        metric.P50,
		P95:        metric.P95,
		P99:        metric.P99,
		LastUpdate: metric.LastUpdate,
		Unit:       metric.Unit,
		Labels:     metric.Labels,
	}, true
}

// GetAllMetrics returns all metrics
func (pm *PerformanceMonitor) GetAllMetrics() map[string]*Metric {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*Metric)
	for name := range pm.metrics {
		if m, exists := pm.GetMetric(name); exists {
			result[name] = m
		}
	}
	return result
}

// GetAlerts returns current alerts
func (pm *PerformanceMonitor) GetAlerts() []Alert {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]Alert, len(pm.alerts))
	copy(result, pm.alerts)
	return result
}

// OnAlert sets the alert callback
func (pm *PerformanceMonitor) OnAlert(callback func(Alert)) {
	pm.onAlert = callback
}

// OnMetricUpdate sets the metric update callback
func (pm *PerformanceMonitor) OnMetricUpdate(callback func(string, *Metric)) {
	pm.onMetricUpdate = callback
}

// collectMetrics periodically processes metrics
func (pm *PerformanceMonitor) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(pm.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.flushMetrics()
		}
	}
}

// collectRuntimeStats collects Go runtime statistics
func (pm *PerformanceMonitor) collectRuntimeStats(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.collectMemoryStats()
			pm.collectGCStats()
		}
	}
}

// collectMemoryStats collects memory statistics
func (pm *PerformanceMonitor) collectMemoryStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	pm.Gauge("memory_alloc_mb", float64(m.Alloc)/1024/1024, "MB", nil)
	pm.Gauge("memory_sys_mb", float64(m.Sys)/1024/1024, "MB", nil)
	pm.Gauge("memory_heap_alloc_mb", float64(m.HeapAlloc)/1024/1024, "MB", nil)
	pm.Gauge("memory_heap_sys_mb", float64(m.HeapSys)/1024/1024, "MB", nil)
	pm.Gauge("memory_heap_objects", float64(m.HeapObjects), "count", nil)
	pm.Gauge("goroutines", float64(runtime.NumGoroutine()), "count", nil)
	pm.Counter("mallocs_total", map[string]string{"value": string(rune(int(m.Mallocs)))})
	pm.Counter("frees_total", map[string]string{"value": string(rune(int(m.Frees)))})

	// Check for memory leaks
	if pm.config.EnableMemoryAlerts {
		pm.checkMemoryLeaks(&m)
	}
}

// collectGCStats collects garbage collection statistics
func (pm *PerformanceMonitor) collectGCStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	pm.Gauge("gc_cycles", float64(m.NumGC), "count", nil)
	pm.Gauge("gc_pause_ms", float64(m.PauseTotalNs)/1e6, "ms", nil)

	if m.NumGC > 0 {
		// Calculate average GC pause time
		avgPause := float64(m.PauseTotalNs) / float64(m.NumGC) / 1e6
		pm.Gauge("gc_pause_avg_ms", avgPause, "ms", nil)
	}
}

// checkMemoryLeaks detects potential memory leaks
func (pm *PerformanceMonitor) checkMemoryLeaks(m *runtime.MemStats) {
	memUsageMB := float64(m.Alloc) / 1024 / 1024

	// Simple heuristic: if memory usage keeps growing without significant GC
	if metric, exists := pm.GetMetric("memory_alloc_mb"); exists {
		values := metric.History.GetValues()
		if len(values) >= 10 {
			// Check if memory usage is consistently increasing
			increasing := 0
			for i := 1; i < len(values); i++ {
				if values[i] > values[i-1] {
					increasing++
				}
			}

			// If 80% of recent samples show increasing memory, alert
			if float64(increasing)/float64(len(values)-1) > 0.8 && memUsageMB > 50 {
				pm.createAlert("memory_leak_detection", AlertLevelWarning, AlertTypeResourceLeak,
					memUsageMB, 0, "Potential memory leak detected: consistently increasing memory usage",
					map[string]interface{}{
						"trend_percentage": float64(increasing) / float64(len(values)-1) * 100,
						"sample_count":     len(values),
					})
			}
		}
	}
}

// checkThresholds checks if metric values exceed configured thresholds
func (pm *PerformanceMonitor) checkThresholds(name string, metric *Metric) {
	threshold, exists := pm.config.Thresholds[name]
	if !exists {
		return
	}

	if metric.Value >= threshold.Critical {
		pm.createAlert(name, AlertLevelCritical, AlertTypeThreshold,
			metric.Value, threshold.Critical,
			"Metric exceeded critical threshold",
			map[string]interface{}{
				"metric_type": metric.Type.String(),
				"unit":        metric.Unit,
			})
	} else if metric.Value >= threshold.Warning {
		pm.createAlert(name, AlertLevelWarning, AlertTypeThreshold,
			metric.Value, threshold.Warning,
			"Metric exceeded warning threshold",
			map[string]interface{}{
				"metric_type": metric.Type.String(),
				"unit":        metric.Unit,
			})
	}
}

// createAlert creates and processes a new alert
func (pm *PerformanceMonitor) createAlert(metric string, level AlertLevel, alertType AlertType, value, threshold float64, message string, context map[string]interface{}) {
	alert := Alert{
		ID:        generateAlertID(),
		Timestamp: time.Now(),
		Level:     level,
		Type:      alertType,
		Metric:    metric,
		Value:     value,
		Threshold: threshold,
		Message:   message,
		Context:   context,
	}

	pm.mu.Lock()
	pm.alerts = append(pm.alerts, alert)
	// Keep only recent alerts (last 100)
	if len(pm.alerts) > 100 {
		pm.alerts = pm.alerts[len(pm.alerts)-100:]
	}
	pm.mu.Unlock()

	// Call alert callback
	if pm.onAlert != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Log panic but don't crash
				}
			}()
			pm.onAlert(alert)
		}()
	}
}

// processAlerts processes and manages alerts
func (pm *PerformanceMonitor) processAlerts(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.cleanupOldAlerts()
		}
	}
}

// cleanupOldAlerts removes old resolved alerts
func (pm *PerformanceMonitor) cleanupOldAlerts() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	filtered := make([]Alert, 0)

	for _, alert := range pm.alerts {
		// Keep unresolved alerts or recently resolved alerts
		if !alert.Resolved || (alert.ResolvedAt != nil && alert.ResolvedAt.After(cutoff)) {
			filtered = append(filtered, alert)
		}
	}

	pm.alerts = filtered
}

// flushMetrics performs periodic metric maintenance
func (pm *PerformanceMonitor) flushMetrics() {
	// This could include:
	// - Sending metrics to external systems
	// - Calculating rolling averages
	// - Cleaning up old metric data
	// - Detecting anomalies
}

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	return time.Now().Format("20060102150405") + "-" + string(rune(rand.Intn(1000)))
}

// GetSummary returns a performance summary
func (pm *PerformanceMonitor) GetSummary() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	summary := map[string]interface{}{
		"total_metrics":  len(pm.metrics),
		"active_alerts":  len(pm.alerts),
		"sampling_rate":  pm.samplingRate,
		"buffer_size":    pm.bufferSize,
		"flush_interval": pm.flushInterval,
		"runtime_stats":  pm.config.EnableRuntimeStats,
		"memory_alerts":  pm.config.EnableMemoryAlerts,
		"running":        pm.running,
	}

	// Add current runtime stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	summary["current_memory_mb"] = float64(m.Alloc) / 1024 / 1024
	summary["current_goroutines"] = runtime.NumGoroutine()
	summary["current_gc_cycles"] = m.NumGC

	return summary
}
