package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricFreshness represents the freshness state of a metric
type MetricFreshness int

const (
	MetricFresh MetricFreshness = iota
	MetricStale
	MetricExpired
)

// StalenessTracker tracks metric age and freshness
type StalenessTracker struct {
	mu sync.RWMutex

	// Configuration
	staleThreshold   time.Duration
	expiredThreshold time.Duration
	cleanupInterval  time.Duration

	// Tracking data
	lastUpdate map[string]time.Time
	metadata   map[string]MetricMetadata

	// Metrics about staleness
	staleMetricsGauge    *prometheus.GaugeVec
	expiredMetricsGauge  *prometheus.GaugeVec
	metricAgeHistogram   *prometheus.HistogramVec
	confidenceScoreGauge *prometheus.GaugeVec
	lastSeenGauge        *prometheus.GaugeVec
}

// MetricMetadata contains additional metadata about a metric
type MetricMetadata struct {
	Source         string
	Labels         map[string]string
	LastValue      float64
	UpdateCount    int64
	FirstSeen      time.Time
	LastSeen       time.Time
	AverageLatency time.Duration
	IsReliable     bool
}

// StalenessConfig configures the staleness tracker
type StalenessConfig struct {
	StaleThreshold   time.Duration // When metric becomes stale
	ExpiredThreshold time.Duration // When metric is considered expired
	CleanupInterval  time.Duration // How often to clean expired metrics
}

// DefaultStalenessConfig returns sensible defaults
func DefaultStalenessConfig() StalenessConfig {
	return StalenessConfig{
		StaleThreshold:   30 * time.Second,
		ExpiredThreshold: 5 * time.Minute,
		CleanupInterval:  10 * time.Minute,
	}
}

// NewStalenessTracker creates a new metric staleness tracker
func NewStalenessTracker(config StalenessConfig, registry *prometheus.Registry) *StalenessTracker {
	st := &StalenessTracker{
		staleThreshold:   config.StaleThreshold,
		expiredThreshold: config.ExpiredThreshold,
		cleanupInterval:  config.CleanupInterval,
		lastUpdate:       make(map[string]time.Time),
		metadata:         make(map[string]MetricMetadata),

		// Metrics
		staleMetricsGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_staleness_tracker_stale_metrics",
				Help: "Number of stale metrics by source",
			},
			[]string{"source"},
		),
		expiredMetricsGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_staleness_tracker_expired_metrics",
				Help: "Number of expired metrics by source",
			},
			[]string{"source"},
		),
		metricAgeHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_staleness_tracker_metric_age_seconds",
				Help:    "Age of metrics in seconds",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
			},
			[]string{"source", "freshness"},
		),
		confidenceScoreGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_staleness_tracker_confidence_score",
				Help: "Confidence score based on metric freshness (0.0 to 1.0)",
			},
			[]string{"metric", "source"},
		),
		lastSeenGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_staleness_tracker_last_seen_timestamp",
				Help: "Unix timestamp of when metric was last seen",
			},
			[]string{"metric", "source"},
		),
	}

	// Register metrics
	if registry != nil {
		registry.MustRegister(
			st.staleMetricsGauge,
			st.expiredMetricsGauge,
			st.metricAgeHistogram,
			st.confidenceScoreGauge,
			st.lastSeenGauge,
		)
	}

	return st
}

// UpdateMetric updates the timestamp for a metric
func (st *StalenessTracker) UpdateMetric(metricKey string, source string, labels map[string]string, value float64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()

	// Update last seen time
	st.lastUpdate[metricKey] = now
	st.lastSeenGauge.WithLabelValues(metricKey, source).Set(float64(now.Unix()))

	// Update or create metadata
	meta, exists := st.metadata[metricKey]
	if !exists {
		meta = MetricMetadata{
			Source:      source,
			Labels:      labels,
			FirstSeen:   now,
			UpdateCount: 0,
			IsReliable:  true,
		}
	}

	// Update metadata
	meta.LastSeen = now
	meta.LastValue = value
	meta.UpdateCount++

	// Calculate average latency
	if meta.UpdateCount > 1 {
		avgInterval := now.Sub(meta.FirstSeen) / time.Duration(meta.UpdateCount-1)
		meta.AverageLatency = avgInterval
	}

	st.metadata[metricKey] = meta

	// Update confidence score
	confidence := st.calculateConfidence(metricKey)
	st.confidenceScoreGauge.WithLabelValues(metricKey, source).Set(confidence)
}

// GetFreshness returns the freshness state of a metric
func (st *StalenessTracker) GetFreshness(metricKey string) MetricFreshness {
	st.mu.RLock()
	defer st.mu.RUnlock()

	lastUpdate, exists := st.lastUpdate[metricKey]
	if !exists {
		return MetricExpired
	}

	age := time.Since(lastUpdate)

	switch {
	case age >= st.expiredThreshold:
		return MetricExpired
	case age >= st.staleThreshold:
		return MetricStale
	default:
		return MetricFresh
	}
}

// GetMetricAge returns the age of a metric
func (st *StalenessTracker) GetMetricAge(metricKey string) (time.Duration, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	lastUpdate, exists := st.lastUpdate[metricKey]
	if !exists {
		return 0, false
	}

	return time.Since(lastUpdate), true
}

// GetConfidence returns a confidence score based on metric freshness and reliability
func (st *StalenessTracker) GetConfidence(metricKey string) float64 {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return st.calculateConfidence(metricKey)
}

// calculateConfidence calculates confidence score (must be called with lock held)
func (st *StalenessTracker) calculateConfidence(metricKey string) float64 {
	lastUpdate, exists := st.lastUpdate[metricKey]
	if !exists {
		return 0.0
	}

	age := time.Since(lastUpdate)
	meta := st.metadata[metricKey]

	// Base confidence on freshness
	var confidence float64
	switch {
	case age < st.staleThreshold:
		confidence = 1.0
	case age < st.expiredThreshold:
		// Linear decay from 1.0 to 0.3
		stalePeriod := st.expiredThreshold - st.staleThreshold
		staleAge := age - st.staleThreshold
		confidence = 1.0 - (0.7 * (float64(staleAge) / float64(stalePeriod)))
	default:
		confidence = 0.0
	}

	// Adjust based on reliability
	if !meta.IsReliable {
		confidence *= 0.8
	}

	// Adjust based on update frequency
	if meta.UpdateCount < 10 {
		confidence *= 0.9
	}

	// Ensure confidence is in valid range
	if confidence < 0 {
		confidence = 0
	} else if confidence > 1 {
		confidence = 1
	}

	return confidence
}

// GetMetadata returns metadata for a metric
func (st *StalenessTracker) GetMetadata(metricKey string) (MetricMetadata, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	meta, exists := st.metadata[metricKey]
	return meta, exists
}

// MarkUnreliable marks a metric as unreliable
func (st *StalenessTracker) MarkUnreliable(metricKey string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if meta, exists := st.metadata[metricKey]; exists {
		meta.IsReliable = false
		st.metadata[metricKey] = meta
	}
}

// GetStaleMetrics returns all stale metrics
func (st *StalenessTracker) GetStaleMetrics() map[string]MetricInfo {
	st.mu.RLock()
	defer st.mu.RUnlock()

	stale := make(map[string]MetricInfo)
	now := time.Now()

	for key, lastUpdate := range st.lastUpdate {
		age := now.Sub(lastUpdate)
		if age >= st.staleThreshold && age < st.expiredThreshold {
			meta := st.metadata[key]
			stale[key] = MetricInfo{
				Key:        key,
				Age:        age,
				Freshness:  MetricStale,
				Confidence: st.calculateConfidence(key),
				Metadata:   meta,
			}
		}
	}

	return stale
}

// GetExpiredMetrics returns all expired metrics
func (st *StalenessTracker) GetExpiredMetrics() map[string]MetricInfo {
	st.mu.RLock()
	defer st.mu.RUnlock()

	expired := make(map[string]MetricInfo)
	now := time.Now()

	for key, lastUpdate := range st.lastUpdate {
		age := now.Sub(lastUpdate)
		if age >= st.expiredThreshold {
			meta := st.metadata[key]
			expired[key] = MetricInfo{
				Key:        key,
				Age:        age,
				Freshness:  MetricExpired,
				Confidence: 0.0,
				Metadata:   meta,
			}
		}
	}

	return expired
}

// UpdateStats updates staleness statistics
func (st *StalenessTracker) UpdateStats() {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// Count metrics by source and freshness
	sourceStats := make(map[string]map[MetricFreshness]int)
	now := time.Now()

	for key, lastUpdate := range st.lastUpdate {
		age := now.Sub(lastUpdate)
		meta := st.metadata[key]

		if _, exists := sourceStats[meta.Source]; !exists {
			sourceStats[meta.Source] = make(map[MetricFreshness]int)
		}

		freshness := st.GetFreshness(key)
		sourceStats[meta.Source][freshness]++

		// Record age histogram
		freshnessLabel := "fresh"
		switch freshness {
		case MetricStale:
			freshnessLabel = "stale"
		case MetricExpired:
			freshnessLabel = "expired"
		}
		st.metricAgeHistogram.WithLabelValues(meta.Source, freshnessLabel).Observe(age.Seconds())
	}

	// Update gauges
	for source, stats := range sourceStats {
		st.staleMetricsGauge.WithLabelValues(source).Set(float64(stats[MetricStale]))
		st.expiredMetricsGauge.WithLabelValues(source).Set(float64(stats[MetricExpired]))
	}
}

// CleanupExpired removes expired metrics
func (st *StalenessTracker) CleanupExpired() int {
	st.mu.Lock()
	defer st.mu.Unlock()

	count := 0
	now := time.Now()

	for key, lastUpdate := range st.lastUpdate {
		if now.Sub(lastUpdate) >= st.expiredThreshold {
			delete(st.lastUpdate, key)
			delete(st.metadata, key)
			count++
		}
	}

	return count
}

// StartCleanupRoutine starts the periodic cleanup routine
func (st *StalenessTracker) StartCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(st.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := st.CleanupExpired()
			if count > 0 {
				fmt.Printf("[INFO] Cleaned up %d expired metrics\n", count)
			}
			st.UpdateStats()
		}
	}
}

// MetricInfo contains information about a metric
type MetricInfo struct {
	Key        string
	Age        time.Duration
	Freshness  MetricFreshness
	Confidence float64
	Metadata   MetricMetadata
}

// GetAllMetrics returns info for all tracked metrics
func (st *StalenessTracker) GetAllMetrics() map[string]MetricInfo {
	st.mu.RLock()
	defer st.mu.RUnlock()

	metrics := make(map[string]MetricInfo)
	now := time.Now()

	for key, lastUpdate := range st.lastUpdate {
		age := now.Sub(lastUpdate)
		meta := st.metadata[key]
		metrics[key] = MetricInfo{
			Key:        key,
			Age:        age,
			Freshness:  st.GetFreshness(key),
			Confidence: st.calculateConfidence(key),
			Metadata:   meta,
		}
	}

	return metrics
}

// GetSummary returns a summary of metric staleness
func (st *StalenessTracker) GetSummary() StalenessSummary {
	st.mu.RLock()
	defer st.mu.RUnlock()

	summary := StalenessSummary{
		TotalMetrics: len(st.lastUpdate),
		ByFreshness:  make(map[MetricFreshness]int),
		BySource:     make(map[string]int),
		AverageAge:   0,
		OldestMetric: "",
		OldestAge:    0,
	}

	now := time.Now()
	var totalAge time.Duration

	for key, lastUpdate := range st.lastUpdate {
		age := now.Sub(lastUpdate)
		totalAge += age

		freshness := st.GetFreshness(key)
		summary.ByFreshness[freshness]++

		meta := st.metadata[key]
		summary.BySource[meta.Source]++

		if age > summary.OldestAge {
			summary.OldestAge = age
			summary.OldestMetric = key
		}
	}

	if summary.TotalMetrics > 0 {
		summary.AverageAge = totalAge / time.Duration(summary.TotalMetrics)
	}

	return summary
}

// StalenessSummary provides a summary of metric staleness
type StalenessSummary struct {
	TotalMetrics int
	ByFreshness  map[MetricFreshness]int
	BySource     map[string]int
	AverageAge   time.Duration
	OldestMetric string
	OldestAge    time.Duration
}
