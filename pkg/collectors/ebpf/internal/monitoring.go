package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// MonitoringManager provides comprehensive monitoring and metrics
type MonitoringManager struct {
	config        *MonitoringConfig
	metricsStore  *MetricsStore
	healthChecker *HealthChecker
	alertManager  *AlertManager
	dashboardData *DashboardData
	mu            sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// MonitoringConfig defines monitoring parameters
type MonitoringConfig struct {
	// Metrics collection
	MetricsInterval   time.Duration
	MetricsRetention  time.Duration
	MaxMetricsPerType int

	// Health checking
	HealthCheckInterval time.Duration
	HealthTimeout       time.Duration
	UnhealthyThreshold  int

	// Alerting
	EnableAlerting  bool
	AlertThresholds map[string]float64
	AlertCooldown   time.Duration

	// Dashboard
	DashboardInterval time.Duration
	HistogramBuckets  []float64

	// Export
	EnablePrometheus bool
	PrometheusPort   int
	EnableStatsD     bool
	StatsDAddress    string
}

// MetricsStore stores time-series metrics
type MetricsStore struct {
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
	rates      map[string]*Rate
	mu         sync.RWMutex
	maxPerType int
	retention  time.Duration
}

// Counter is a monotonically increasing metric
type Counter struct {
	value       atomic.Uint64
	labels      map[string]string
	lastReset   atomic.Value // time.Time
	description string
}

// Gauge is a metric that can go up or down
type Gauge struct {
	value       atomic.Value // float64
	labels      map[string]string
	lastUpdate  atomic.Value // time.Time
	description string
}

// Histogram tracks distribution of values
type Histogram struct {
	buckets     []float64
	counts      []atomic.Uint64
	sum         atomic.Value // float64
	count       atomic.Uint64
	labels      map[string]string
	description string
	mu          sync.Mutex
}

// Rate tracks events per time unit
type Rate struct {
	events      *CircularBuffer
	window      time.Duration
	labels      map[string]string
	description string
	mu          sync.Mutex
}

// Rate returns the current rate
func (r *Rate) Rate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Simplified rate calculation
	return 0.0
}

// HealthChecker monitors system health
type HealthChecker struct {
	checks        map[string]HealthCheck
	lastResults   map[string]*HealthResult
	failureCounts map[string]int
	threshold     int
	timeout       time.Duration
	mu            sync.RWMutex
}

// HealthCheck defines a health check function
type HealthCheck func(ctx context.Context) error

// HealthResult stores health check result
type HealthResult struct {
	Status    core.HealthStatus
	Message   string
	Timestamp time.Time
	Duration  time.Duration
	Error     error
}

// AlertManager handles alerts
type AlertManager struct {
	rules     map[string]*AlertRule
	active    map[string]*Alert
	history   *CircularBuffer
	cooldowns map[string]time.Time
	mu        sync.RWMutex
	alertCh   chan *Alert
}

// AlertRule defines alert conditions
type AlertRule struct {
	Name      string
	Condition func(metrics map[string]interface{}) bool
	Severity  AlertSeverity
	Message   string
	Cooldown  time.Duration
	Actions   []AlertAction
}

// Alert represents an active alert
type Alert struct {
	ID        string
	Rule      string
	Severity  AlertSeverity
	Message   string
	Timestamp time.Time
	Labels    map[string]string
	Value     interface{}
}

// AlertSeverity levels
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertAction defines what to do when alert fires
type AlertAction func(alert *Alert) error

// DashboardData provides real-time dashboard metrics
type DashboardData struct {
	EventRate     *RateTracker
	ErrorRate     *RateTracker
	Latency       *LatencyTracker
	ResourceUsage map[string]float64
	SystemHealth  core.HealthStatus
	ActiveAlerts  int
	LastUpdate    time.Time
	mu            sync.RWMutex
}

// RateTracker tracks rate over time
type RateTracker struct {
	points     []RatePoint
	maxPoints  int
	currentIdx int
	mu         sync.Mutex
}

// RatePoint is a rate measurement at a point in time
type RatePoint struct {
	Timestamp time.Time
	Rate      float64
}

// LatencyTracker tracks latency percentiles
type LatencyTracker struct {
	histogram   *Histogram
	percentiles []float64
	mu          sync.Mutex
}

// CircularBuffer is a fixed-size circular buffer
type CircularBuffer struct {
	items []interface{}
	size  int
	head  int
	tail  int
	count int
	mu    sync.Mutex
}

// NewMonitoringManager creates a new monitoring manager
func NewMonitoringManager(config *MonitoringConfig) *MonitoringManager {
	if config == nil {
		config = DefaultMonitoringConfig()
	}

	mm := &MonitoringManager{
		config:        config,
		metricsStore:  NewMetricsStore(config.MaxMetricsPerType, config.MetricsRetention),
		healthChecker: NewHealthChecker(config.UnhealthyThreshold, config.HealthTimeout),
		alertManager:  NewAlertManager(),
		dashboardData: NewDashboardData(),
		stopCh:        make(chan struct{}),
	}

	// Register default health checks
	mm.registerDefaultHealthChecks()

	// Register default alert rules
	mm.registerDefaultAlertRules()

	// Start background workers
	mm.wg.Add(3)
	go mm.collectMetrics()
	go mm.runHealthChecks()
	go mm.updateDashboard()

	return mm
}

// DefaultMonitoringConfig returns production defaults
func DefaultMonitoringConfig() *MonitoringConfig {
	return &MonitoringConfig{
		MetricsInterval:     10 * time.Second,
		MetricsRetention:    1 * time.Hour,
		MaxMetricsPerType:   1000,
		HealthCheckInterval: 30 * time.Second,
		HealthTimeout:       5 * time.Second,
		UnhealthyThreshold:  3,
		EnableAlerting:      true,
		AlertThresholds: map[string]float64{
			"error_rate":    0.05, // 5% error rate
			"memory_usage":  0.8,  // 80% memory
			"cpu_usage":     0.8,  // 80% CPU
			"event_dropped": 0.01, // 1% dropped
		},
		AlertCooldown:     5 * time.Minute,
		DashboardInterval: 1 * time.Second,
		HistogramBuckets:  []float64{0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000},
		EnablePrometheus:  true,
		PrometheusPort:    9090,
		EnableStatsD:      false,
		StatsDAddress:     "localhost:8125",
	}
}

// RecordEvent records an event metric
func (mm *MonitoringManager) RecordEvent(eventType string, labels map[string]string) {
	mm.metricsStore.IncrementCounter("events_total", labels)
	mm.metricsStore.IncrementCounter(fmt.Sprintf("events_%s", eventType), labels)

	// Update dashboard
	mm.dashboardData.EventRate.Record(1)
}

// RecordError records an error metric
func (mm *MonitoringManager) RecordError(errorType string, labels map[string]string) {
	mm.metricsStore.IncrementCounter("errors_total", labels)
	mm.metricsStore.IncrementCounter(fmt.Sprintf("errors_%s", errorType), labels)

	// Update dashboard
	mm.dashboardData.ErrorRate.Record(1)

	// Check alert conditions
	mm.checkErrorAlert()
}

// RecordLatency records a latency measurement
func (mm *MonitoringManager) RecordLatency(operation string, duration time.Duration, labels map[string]string) {
	ms := float64(duration.Milliseconds())

	mm.metricsStore.ObserveHistogram(fmt.Sprintf("latency_%s", operation), ms, labels)
	mm.metricsStore.SetGauge(fmt.Sprintf("latency_%s_last", operation), ms, labels)

	// Update dashboard
	mm.dashboardData.Latency.Record(ms)
}

// RecordResourceUsage records resource metrics
func (mm *MonitoringManager) RecordResourceUsage(usage map[string]float64) {
	mm.dashboardData.mu.Lock()
	mm.dashboardData.ResourceUsage = usage
	mm.dashboardData.LastUpdate = time.Now()
	mm.dashboardData.mu.Unlock()

	// Record in metrics store
	for resource, value := range usage {
		mm.metricsStore.SetGauge(fmt.Sprintf("resource_%s", resource), value, nil)
	}

	// Check resource alerts
	mm.checkResourceAlerts(usage)
}

// GetMetrics returns current metrics
func (mm *MonitoringManager) GetMetrics() map[string]interface{} {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	metrics := make(map[string]interface{})

	// Add counter metrics
	for name, counter := range mm.metricsStore.counters {
		metrics[name] = counter.value.Load()
	}

	// Add gauge metrics
	for name, gauge := range mm.metricsStore.gauges {
		if val := gauge.value.Load(); val != nil {
			metrics[name] = val.(float64)
		}
	}

	// Add histogram summaries
	for name, hist := range mm.metricsStore.histograms {
		metrics[name+"_count"] = hist.count.Load()
		if sum := hist.sum.Load(); sum != nil {
			metrics[name+"_sum"] = sum.(float64)
		}
		metrics[name+"_p50"] = hist.Percentile(0.5)
		metrics[name+"_p95"] = hist.Percentile(0.95)
		metrics[name+"_p99"] = hist.Percentile(0.99)
	}

	// Add rate metrics
	for name, rate := range mm.metricsStore.rates {
		metrics[name+"_rate"] = rate.Rate()
	}

	// Add health status
	metrics["health_status"] = mm.GetHealthStatus()

	// Add alert count
	metrics["active_alerts"] = len(mm.alertManager.active)

	return metrics
}

// GetHealthStatus returns overall health status
func (mm *MonitoringManager) GetHealthStatus() core.HealthStatus {
	mm.healthChecker.mu.RLock()
	defer mm.healthChecker.mu.RUnlock()

	worstStatus := core.HealthStatusHealthy

	for _, result := range mm.healthChecker.lastResults {
		if result.Status == core.HealthStatusUnhealthy {
			return core.HealthStatusUnhealthy
		}
		if result.Status == core.HealthStatusDegraded && worstStatus == core.HealthStatusHealthy {
			worstStatus = core.HealthStatusDegraded
		}
	}

	return worstStatus
}

// GetDashboard returns dashboard data
func (mm *MonitoringManager) GetDashboard() map[string]interface{} {
	mm.dashboardData.mu.RLock()
	defer mm.dashboardData.mu.RUnlock()

	return map[string]interface{}{
		"event_rate":     mm.dashboardData.EventRate.Current(),
		"error_rate":     mm.dashboardData.ErrorRate.Current(),
		"latency_p50":    mm.dashboardData.Latency.Percentile(0.5),
		"latency_p95":    mm.dashboardData.Latency.Percentile(0.95),
		"latency_p99":    mm.dashboardData.Latency.Percentile(0.99),
		"resource_usage": mm.dashboardData.ResourceUsage,
		"system_health":  mm.dashboardData.SystemHealth,
		"active_alerts":  mm.dashboardData.ActiveAlerts,
		"last_update":    mm.dashboardData.LastUpdate,
	}
}

// Stop gracefully stops monitoring
func (mm *MonitoringManager) Stop() {
	close(mm.stopCh)
	mm.wg.Wait()
}

// Background workers

func (mm *MonitoringManager) collectMetrics() {
	defer mm.wg.Done()

	ticker := time.NewTicker(mm.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mm.stopCh:
			return
		case <-ticker.C:
			mm.metricsStore.Cleanup()
		}
	}
}

func (mm *MonitoringManager) runHealthChecks() {
	defer mm.wg.Done()

	ticker := time.NewTicker(mm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mm.stopCh:
			return
		case <-ticker.C:
			mm.healthChecker.RunChecks(context.Background())
			mm.dashboardData.mu.Lock()
			mm.dashboardData.SystemHealth = mm.GetHealthStatus()
			mm.dashboardData.mu.Unlock()
		}
	}
}

func (mm *MonitoringManager) updateDashboard() {
	defer mm.wg.Done()

	ticker := time.NewTicker(mm.config.DashboardInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mm.stopCh:
			return
		case <-ticker.C:
			mm.dashboardData.mu.Lock()
			mm.dashboardData.ActiveAlerts = len(mm.alertManager.active)
			mm.dashboardData.LastUpdate = time.Now()
			mm.dashboardData.mu.Unlock()
		}
	}
}

// Helper methods

func (mm *MonitoringManager) registerDefaultHealthChecks() {
	// Memory health check
	mm.healthChecker.Register("memory", func(ctx context.Context) error {
		usage := mm.dashboardData.ResourceUsage["memory_percent"]
		if usage > 90 {
			return fmt.Errorf("memory usage critical: %.2f%%", usage)
		}
		if usage > 75 {
			return fmt.Errorf("memory usage high: %.2f%%", usage)
		}
		return nil
	})

	// CPU health check
	mm.healthChecker.Register("cpu", func(ctx context.Context) error {
		usage := mm.dashboardData.ResourceUsage["cpu_percent"]
		if usage > 90 {
			return fmt.Errorf("CPU usage critical: %.2f%%", usage)
		}
		if usage > 75 {
			return fmt.Errorf("CPU usage high: %.2f%%", usage)
		}
		return nil
	})

	// Event processing health check
	mm.healthChecker.Register("events", func(ctx context.Context) error {
		errorRate := mm.dashboardData.ErrorRate.Current()
		if errorRate > 0.1 { // 10% error rate
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
		return nil
	})
}

func (mm *MonitoringManager) registerDefaultAlertRules() {
	// High error rate alert
	mm.alertManager.AddRule(&AlertRule{
		Name: "high_error_rate",
		Condition: func(metrics map[string]interface{}) bool {
			if rate, ok := metrics["error_rate"].(float64); ok {
				return rate > mm.config.AlertThresholds["error_rate"]
			}
			return false
		},
		Severity: AlertSeverityError,
		Message:  "Error rate exceeds threshold",
		Cooldown: mm.config.AlertCooldown,
	})

	// High memory usage alert
	mm.alertManager.AddRule(&AlertRule{
		Name: "high_memory_usage",
		Condition: func(metrics map[string]interface{}) bool {
			if usage, ok := metrics["memory_percent"].(float64); ok {
				return usage > mm.config.AlertThresholds["memory_usage"]
			}
			return false
		},
		Severity: AlertSeverityCritical,
		Message:  "Memory usage exceeds threshold",
		Cooldown: mm.config.AlertCooldown,
	})
}

func (mm *MonitoringManager) checkErrorAlert() {
	errorRate := mm.dashboardData.ErrorRate.Current()
	metrics := map[string]interface{}{
		"error_rate": errorRate,
	}
	mm.alertManager.Check(metrics)
}

func (mm *MonitoringManager) checkResourceAlerts(usage map[string]float64) {
	metrics := make(map[string]interface{})
	for k, v := range usage {
		metrics[k] = v
	}
	mm.alertManager.Check(metrics)
}

// MetricsStore implementation

func NewMetricsStore(maxPerType int, retention time.Duration) *MetricsStore {
	return &MetricsStore{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
		rates:      make(map[string]*Rate),
		maxPerType: maxPerType,
		retention:  retention,
	}
}

func (ms *MetricsStore) IncrementCounter(name string, labels map[string]string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := ms.makeKey(name, labels)
	if counter, ok := ms.counters[key]; ok {
		counter.value.Add(1)
	} else {
		counter := &Counter{
			labels:      labels,
			description: name,
		}
		counter.value.Store(1)
		counter.lastReset.Store(time.Now())
		ms.counters[key] = counter
	}
}

func (ms *MetricsStore) SetGauge(name string, value float64, labels map[string]string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := ms.makeKey(name, labels)
	if gauge, ok := ms.gauges[key]; ok {
		gauge.value.Store(value)
		gauge.lastUpdate.Store(time.Now())
	} else {
		gauge := &Gauge{
			labels:      labels,
			description: name,
		}
		gauge.value.Store(value)
		gauge.lastUpdate.Store(time.Now())
		ms.gauges[key] = gauge
	}
}

func (ms *MetricsStore) ObserveHistogram(name string, value float64, labels map[string]string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := ms.makeKey(name, labels)
	if hist, ok := ms.histograms[key]; ok {
		hist.Observe(value)
	} else {
		hist := NewHistogram(
			[]float64{0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000},
			labels,
			name,
		)
		hist.Observe(value)
		ms.histograms[key] = hist
	}
}

func (ms *MetricsStore) makeKey(name string, labels map[string]string) string {
	// Simple key generation - in production use proper label encoding
	return name
}

func (ms *MetricsStore) Cleanup() {
	// Cleanup old metrics based on retention
	// This is simplified - in production, implement proper cleanup
}

// Additional helper implementations

func NewHealthChecker(threshold int, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		checks:        make(map[string]HealthCheck),
		lastResults:   make(map[string]*HealthResult),
		failureCounts: make(map[string]int),
		threshold:     threshold,
		timeout:       timeout,
	}
}

func (hc *HealthChecker) Register(name string, check HealthCheck) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
}

func (hc *HealthChecker) RunChecks(ctx context.Context) {
	hc.mu.Lock()
	checks := make(map[string]HealthCheck)
	for name, check := range hc.checks {
		checks[name] = check
	}
	hc.mu.Unlock()

	for name, check := range checks {
		result := hc.runCheck(ctx, name, check)

		hc.mu.Lock()
		hc.lastResults[name] = result
		if result.Error != nil {
			hc.failureCounts[name]++
		} else {
			hc.failureCounts[name] = 0
		}
		hc.mu.Unlock()
	}
}

func (hc *HealthChecker) runCheck(ctx context.Context, name string, check HealthCheck) *HealthResult {
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	start := time.Now()
	err := check(checkCtx)
	duration := time.Since(start)

	result := &HealthResult{
		Timestamp: start,
		Duration:  duration,
		Error:     err,
	}

	if err != nil {
		if hc.failureCounts[name] >= hc.threshold {
			result.Status = core.HealthStatusUnhealthy
			result.Message = fmt.Sprintf("Check failed %d times: %v", hc.failureCounts[name], err)
		} else {
			result.Status = core.HealthStatusDegraded
			result.Message = fmt.Sprintf("Check failed: %v", err)
		}
	} else {
		result.Status = core.HealthStatusHealthy
		result.Message = "Check passed"
	}

	return result
}

func NewAlertManager() *AlertManager {
	return &AlertManager{
		rules:     make(map[string]*AlertRule),
		active:    make(map[string]*Alert),
		history:   NewCircularBuffer(100),
		cooldowns: make(map[string]time.Time),
		alertCh:   make(chan *Alert, 100),
	}
}

func (am *AlertManager) AddRule(rule *AlertRule) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.rules[rule.Name] = rule
}

func (am *AlertManager) Check(metrics map[string]interface{}) {
	am.mu.Lock()
	defer am.mu.Unlock()

	for name, rule := range am.rules {
		// Check cooldown
		if cooldown, ok := am.cooldowns[name]; ok && time.Now().Before(cooldown) {
			continue
		}

		if rule.Condition(metrics) {
			alert := &Alert{
				ID:        fmt.Sprintf("%s-%d", name, time.Now().Unix()),
				Rule:      name,
				Severity:  rule.Severity,
				Message:   rule.Message,
				Timestamp: time.Now(),
			}

			am.active[name] = alert
			am.history.Add(alert)
			am.cooldowns[name] = time.Now().Add(rule.Cooldown)

			// Execute actions
			for _, action := range rule.Actions {
				go action(alert)
			}
		} else {
			delete(am.active, name)
		}
	}
}

func NewDashboardData() *DashboardData {
	return &DashboardData{
		EventRate:     NewRateTracker(60),
		ErrorRate:     NewRateTracker(60),
		Latency:       NewLatencyTracker(),
		ResourceUsage: make(map[string]float64),
		SystemHealth:  core.HealthStatusHealthy,
		LastUpdate:    time.Now(),
	}
}

func NewHistogram(buckets []float64, labels map[string]string, description string) *Histogram {
	return &Histogram{
		buckets:     buckets,
		counts:      make([]atomic.Uint64, len(buckets)+1),
		labels:      labels,
		description: description,
	}
}

func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Find bucket
	bucketIdx := len(h.buckets)
	for i, boundary := range h.buckets {
		if value <= boundary {
			bucketIdx = i
			break
		}
	}

	h.counts[bucketIdx].Add(1)
	h.count.Add(1)

	if sum := h.sum.Load(); sum != nil {
		h.sum.Store(sum.(float64) + value)
	} else {
		h.sum.Store(value)
	}
}

func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	total := h.count.Load()
	if total == 0 {
		return 0
	}

	target := uint64(float64(total) * p)
	cumulative := uint64(0)

	for i, count := range h.counts {
		cumulative += count.Load()
		if cumulative >= target {
			if i < len(h.buckets) {
				return h.buckets[i]
			}
			return h.buckets[len(h.buckets)-1]
		}
	}

	return h.buckets[len(h.buckets)-1]
}

func NewRateTracker(maxPoints int) *RateTracker {
	return &RateTracker{
		points:    make([]RatePoint, maxPoints),
		maxPoints: maxPoints,
	}
}

func (rt *RateTracker) Record(value float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.points[rt.currentIdx] = RatePoint{
		Timestamp: time.Now(),
		Rate:      value,
	}
	rt.currentIdx = (rt.currentIdx + 1) % rt.maxPoints
}

func (rt *RateTracker) Current() float64 {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Calculate rate over last minute
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	var sum float64
	var count int

	for _, point := range rt.points {
		if point.Timestamp.After(cutoff) {
			sum += point.Rate
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		histogram:   NewHistogram([]float64{0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000}, nil, "latency"),
		percentiles: []float64{0.5, 0.9, 0.95, 0.99},
	}
}

func (lt *LatencyTracker) Record(latency float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.histogram.Observe(latency)
}

func (lt *LatencyTracker) Percentile(p float64) float64 {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	return lt.histogram.Percentile(p)
}

func NewCircularBuffer(size int) *CircularBuffer {
	return &CircularBuffer{
		items: make([]interface{}, size),
		size:  size,
	}
}

func (cb *CircularBuffer) Add(item interface{}) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.items[cb.tail] = item
	cb.tail = (cb.tail + 1) % cb.size

	if cb.count < cb.size {
		cb.count++
	} else {
		cb.head = (cb.head + 1) % cb.size
	}
}
