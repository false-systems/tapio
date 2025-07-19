package correlation

import (
	"sync"
	"sync/atomic"
	"time"

	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation/domain"
)

// PrometheusMetricsCollector provides production-ready metrics collection
// with Prometheus-compatible metrics and efficient aggregation
type PrometheusMetricsCollector struct {
	// Counters
	eventsProcessed   atomic.Int64
	correlationsFound atomic.Int64
	insightsGenerated atomic.Int64
	rulesExecuted     atomic.Int64
	rulesFailed       atomic.Int64

	// Histograms (using exponential buckets)
	ruleExecutionTimes *TimeHistogram
	correlationLatency *TimeHistogram

	// Gauges
	activeRules        atomic.Int32
	activeCorrelations atomic.Int32
	queueDepth         atomic.Int32

	// Rate calculation
	rateCalculator *RateCalculator

	// Labels for metrics
	labels map[string]string
	mu     sync.RWMutex
}

// TimeHistogram provides efficient histogram for timing metrics
type TimeHistogram struct {
	buckets []float64 // Bucket boundaries in milliseconds
	counts  []atomic.Int64
	sum     atomic.Int64
	count   atomic.Int64
	mu      sync.RWMutex
}

// RateCalculator calculates rates over time windows
type RateCalculator struct {
	windows map[string]*TimeWindow
	mu      sync.RWMutex
}

// TimeWindow tracks events in a time window
type TimeWindow struct {
	events     []time.Time
	windowSize time.Duration
	mu         sync.Mutex
}

// NewPrometheusMetricsCollector creates a production metrics collector
func NewPrometheusMetricsCollector() *PrometheusMetricsCollector {
	return &PrometheusMetricsCollector{
		ruleExecutionTimes: NewTimeHistogram([]float64{
			0.1, 0.5, 1, 5, 10, 50, 100, 500, 1000, 5000, // milliseconds
		}),
		correlationLatency: NewTimeHistogram([]float64{
			1, 5, 10, 50, 100, 500, 1000, 5000, 10000, // milliseconds
		}),
		rateCalculator: NewRateCalculator(),
		labels:         make(map[string]string),
	}
}

// RecordEngineStats implements MetricsCollector interface
func (m *PrometheusMetricsCollector) RecordEngineStats(stats corrDomain.Stats) {
	// Update counters
	m.eventsProcessed.Store(int64(stats.EventsProcessed))
	m.correlationsFound.Store(int64(stats.CorrelationsFound))
	// InsightsGenerated is tracked separately, not in Stats

	// Update gauges
	m.activeRules.Store(int32(stats.RulesActive))
	m.activeCorrelations.Store(int32(stats.CorrelationsActive))

	// Record processing rate
	m.rateCalculator.RecordEvent("processing_rate", time.Now())
}

// RecordRuleExecution implements MetricsCollector interface
func (m *PrometheusMetricsCollector) RecordRuleExecution(ruleID string, duration time.Duration, success bool) {
	// Record execution time
	m.ruleExecutionTimes.Observe(float64(duration.Milliseconds()))

	// Update counters
	m.rulesExecuted.Add(1)
	if !success {
		m.rulesFailed.Add(1)
	}

	// Record rule-specific metrics
	m.mu.Lock()
	m.labels[ruleID] = formatLabel(ruleID, success)
	m.mu.Unlock()

	// Track execution rate
	m.rateCalculator.RecordEvent("rule_execution", time.Now())
}

// RecordCorrelation records correlation-specific metrics
func (m *PrometheusMetricsCollector) RecordCorrelation(correlationID string, latency time.Duration, eventCount int) {
	m.correlationLatency.Observe(float64(latency.Milliseconds()))
	m.correlationsFound.Add(1)

	// Track correlation patterns
	m.rateCalculator.RecordEvent("correlation", time.Now())
}

// RecordInsight records insight generation metrics
func (m *PrometheusMetricsCollector) RecordInsight(insightType string, confidence float64) {
	m.insightsGenerated.Add(1)

	// Track insight generation rate
	m.rateCalculator.RecordEvent("insight_"+insightType, time.Now())
}

// RecordCorrelationFound implements MetricsCollector interface
func (m *PrometheusMetricsCollector) RecordCorrelationFound(correlationType string, confidence float64) {
	m.correlationsFound.Add(1)

	// Track correlation type
	m.mu.Lock()
	m.labels["correlation_"+correlationType] = formatLabel(correlationType, true)
	m.mu.Unlock()

	// Track correlation rate by type
	m.rateCalculator.RecordEvent("correlation_"+correlationType, time.Now())
}

// RecordEventProcessed implements MetricsCollector interface
func (m *PrometheusMetricsCollector) RecordEventProcessed(eventType string, source string, processingTime time.Duration) {
	m.eventsProcessed.Add(1)

	// Track processing time
	m.ruleExecutionTimes.Observe(float64(processingTime.Milliseconds()))

	// Track event processing rate
	m.rateCalculator.RecordEvent("event_processed", time.Now())

	// Track by event type
	m.mu.Lock()
	m.labels["event_"+eventType] = formatLabel(eventType, true)
	m.mu.Unlock()
}

// GetMetrics returns Prometheus-compatible metrics
func (m *PrometheusMetricsCollector) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// Counters
	metrics["events_processed_total"] = m.eventsProcessed.Load()
	metrics["correlations_found_total"] = m.correlationsFound.Load()
	metrics["insights_generated_total"] = m.insightsGenerated.Load()
	metrics["rules_executed_total"] = m.rulesExecuted.Load()
	metrics["rules_failed_total"] = m.rulesFailed.Load()

	// Gauges
	metrics["active_rules"] = m.activeRules.Load()
	metrics["active_correlations"] = m.activeCorrelations.Load()
	metrics["queue_depth"] = m.queueDepth.Load()

	// Histograms
	metrics["rule_execution_time_ms"] = m.ruleExecutionTimes.GetQuantiles()
	metrics["correlation_latency_ms"] = m.correlationLatency.GetQuantiles()

	// Rates
	metrics["events_per_second"] = m.rateCalculator.GetRate("processing_rate", time.Second)
	metrics["correlations_per_minute"] = m.rateCalculator.GetRate("correlation", time.Minute)
	metrics["rules_per_second"] = m.rateCalculator.GetRate("rule_execution", time.Second)

	// Labels
	m.mu.RLock()
	metrics["rule_labels"] = m.copyLabels()
	m.mu.RUnlock()

	return metrics
}

// TimeHistogram implementation

func NewTimeHistogram(buckets []float64) *TimeHistogram {
	h := &TimeHistogram{
		buckets: buckets,
		counts:  make([]atomic.Int64, len(buckets)+1), // +1 for overflow bucket
	}
	return h
}

func (h *TimeHistogram) Observe(value float64) {
	// Find the right bucket
	bucket := len(h.buckets)
	for i, boundary := range h.buckets {
		if value <= boundary {
			bucket = i
			break
		}
	}

	// Update counts
	h.counts[bucket].Add(1)
	h.count.Add(1)
	h.sum.Add(int64(value))
}

func (h *TimeHistogram) GetQuantiles() map[string]float64 {
	quantiles := make(map[string]float64)
	total := h.count.Load()

	if total == 0 {
		return quantiles
	}

	// Calculate common quantiles
	targets := []struct {
		name     string
		quantile float64
	}{
		{"p50", 0.5},
		{"p90", 0.9},
		{"p95", 0.95},
		{"p99", 0.99},
		{"p999", 0.999},
	}

	cumulative := int64(0)
	bucketIdx := 0

	for _, target := range targets {
		targetCount := int64(float64(total) * target.quantile)

		// Find the bucket containing the target quantile
		for bucketIdx < len(h.counts) && cumulative < targetCount {
			cumulative += h.counts[bucketIdx].Load()
			if cumulative >= targetCount {
				if bucketIdx < len(h.buckets) {
					quantiles[target.name] = h.buckets[bucketIdx]
				} else {
					quantiles[target.name] = h.buckets[len(h.buckets)-1] * 2 // Overflow estimate
				}
				break
			}
			bucketIdx++
		}
	}

	// Add mean
	if total > 0 {
		quantiles["mean"] = float64(h.sum.Load()) / float64(total)
	}

	return quantiles
}

// RateCalculator implementation

func NewRateCalculator() *RateCalculator {
	return &RateCalculator{
		windows: make(map[string]*TimeWindow),
	}
}

func (r *RateCalculator) RecordEvent(metric string, timestamp time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.windows[metric]; !exists {
		r.windows[metric] = &TimeWindow{
			events:     make([]time.Time, 0, 1000),
			windowSize: time.Minute * 5,
		}
	}

	window := r.windows[metric]
	window.mu.Lock()
	defer window.mu.Unlock()

	// Add new event
	window.events = append(window.events, timestamp)

	// Clean old events
	cutoff := timestamp.Add(-window.windowSize)
	i := 0
	for i < len(window.events) && window.events[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		window.events = window.events[i:]
	}
}

func (r *RateCalculator) GetRate(metric string, period time.Duration) float64 {
	r.mu.RLock()
	window, exists := r.windows[metric]
	r.mu.RUnlock()

	if !exists {
		return 0
	}

	window.mu.Lock()
	defer window.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-period)
	count := 0

	for i := len(window.events) - 1; i >= 0; i-- {
		if window.events[i].After(cutoff) {
			count++
		} else {
			break
		}
	}

	return float64(count) / period.Seconds()
}

// Helper functions

func formatLabel(ruleID string, success bool) string {
	status := "success"
	if !success {
		status = "failed"
	}
	return ruleID + ":" + status
}

func (m *PrometheusMetricsCollector) copyLabels() map[string]string {
	labels := make(map[string]string)
	for k, v := range m.labels {
		labels[k] = v
	}
	return labels
}
