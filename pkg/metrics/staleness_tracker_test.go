package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStalenessTracker(t *testing.T) {
	registry := prometheus.NewRegistry()
	config := DefaultStalenessConfig()

	tracker := NewStalenessTracker(config, registry)

	assert.NotNil(t, tracker)
	assert.Equal(t, config.StaleThreshold, tracker.staleThreshold)
	assert.Equal(t, config.ExpiredThreshold, tracker.expiredThreshold)
	assert.NotNil(t, tracker.lastUpdate)
	assert.NotNil(t, tracker.metadata)
}

func TestUpdateMetric(t *testing.T) {
	tracker := &StalenessTracker{
		lastUpdate: make(map[string]time.Time),
		metadata:   make(map[string]MetricMetadata),
		metrics: &stalenessMetrics{
			totalMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			updatesTotal:   prometheus.NewCounter(prometheus.CounterOpts{}),
			staleMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			expiredMetrics: prometheus.NewGauge(prometheus.GaugeOpts{}),
		},
	}

	now := time.Now()
	labels := map[string]string{"test": "value"}

	tracker.UpdateMetric("metric1", 42.0, labels)

	assert.Contains(t, tracker.lastUpdate, "metric1")
	assert.WithinDuration(t, now, tracker.lastUpdate["metric1"], time.Second)

	metadata, exists := tracker.metadata["metric1"]
	require.True(t, exists)
	assert.Equal(t, 42.0, metadata.Value)
	assert.Equal(t, labels, metadata.Labels)
}

func TestGetMetricAge(t *testing.T) {
	tracker := &StalenessTracker{
		lastUpdate: make(map[string]time.Time),
	}

	t.Run("existing_metric", func(t *testing.T) {
		past := time.Now().Add(-5 * time.Minute)
		tracker.lastUpdate["metric1"] = past

		age := tracker.GetMetricAge("metric1")
		assert.InDelta(t, 5*time.Minute.Seconds(), age.Seconds(), 1)
	})

	t.Run("non_existing_metric", func(t *testing.T) {
		age := tracker.GetMetricAge("nonexistent")
		assert.Equal(t, time.Duration(0), age)
	})
}

func TestIsStale(t *testing.T) {
	tracker := &StalenessTracker{
		staleThreshold: 5 * time.Minute,
		lastUpdate:     make(map[string]time.Time),
	}

	tests := []struct {
		name     string
		metricID string
		age      time.Duration
		expected bool
	}{
		{
			name:     "fresh_metric",
			metricID: "fresh",
			age:      1 * time.Minute,
			expected: false,
		},
		{
			name:     "stale_metric",
			metricID: "stale",
			age:      10 * time.Minute,
			expected: true,
		},
		{
			name:     "exactly_at_threshold",
			metricID: "threshold",
			age:      5 * time.Minute,
			expected: false,
		},
		{
			name:     "non_existing_metric",
			metricID: "nonexistent",
			age:      0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.age > 0 {
				tracker.lastUpdate[tt.metricID] = time.Now().Add(-tt.age)
			}

			result := tracker.IsStale(tt.metricID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsExpired(t *testing.T) {
	tracker := &StalenessTracker{
		expiredThreshold: 30 * time.Minute,
		lastUpdate:       make(map[string]time.Time),
	}

	tests := []struct {
		name     string
		metricID string
		age      time.Duration
		expected bool
	}{
		{
			name:     "fresh_metric",
			metricID: "fresh",
			age:      5 * time.Minute,
			expected: false,
		},
		{
			name:     "expired_metric",
			metricID: "expired",
			age:      45 * time.Minute,
			expected: true,
		},
		{
			name:     "non_existing_metric",
			metricID: "nonexistent",
			age:      0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.age > 0 {
				tracker.lastUpdate[tt.metricID] = time.Now().Add(-tt.age)
			}

			result := tracker.IsExpired(tt.metricID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetConfidenceScore(t *testing.T) {
	tracker := &StalenessTracker{
		staleThreshold:   5 * time.Minute,
		expiredThreshold: 30 * time.Minute,
		lastUpdate:       make(map[string]time.Time),
	}

	tests := []struct {
		name     string
		metricID string
		age      time.Duration
		minScore float64
		maxScore float64
	}{
		{
			name:     "very_fresh_metric",
			metricID: "fresh",
			age:      10 * time.Second,
			minScore: 0.95,
			maxScore: 1.0,
		},
		{
			name:     "slightly_old_metric",
			metricID: "old",
			age:      3 * time.Minute,
			minScore: 0.6,
			maxScore: 0.8,
		},
		{
			name:     "stale_metric",
			metricID: "stale",
			age:      10 * time.Minute,
			minScore: 0.3,
			maxScore: 0.5,
		},
		{
			name:     "expired_metric",
			metricID: "expired",
			age:      35 * time.Minute,
			minScore: 0.0,
			maxScore: 0.1,
		},
		{
			name:     "non_existing_metric",
			metricID: "nonexistent",
			age:      0,
			minScore: 0.0,
			maxScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.age > 0 {
				tracker.lastUpdate[tt.metricID] = time.Now().Add(-tt.age)
			}

			score := tracker.GetConfidenceScore(tt.metricID)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore)
		})
	}
}

func TestCleanupExpired(t *testing.T) {
	tracker := &StalenessTracker{
		expiredThreshold: 30 * time.Minute,
		lastUpdate:       make(map[string]time.Time),
		metadata:         make(map[string]MetricMetadata),
		metrics: &stalenessMetrics{
			cleanupTotal: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	// Add metrics with different ages
	now := time.Now()
	tracker.lastUpdate["fresh"] = now.Add(-5 * time.Minute)
	tracker.metadata["fresh"] = MetricMetadata{Value: 1.0}

	tracker.lastUpdate["expired1"] = now.Add(-45 * time.Minute)
	tracker.metadata["expired1"] = MetricMetadata{Value: 2.0}

	tracker.lastUpdate["expired2"] = now.Add(-60 * time.Minute)
	tracker.metadata["expired2"] = MetricMetadata{Value: 3.0}

	removed := tracker.CleanupExpired()

	assert.Equal(t, 2, removed)
	assert.Contains(t, tracker.lastUpdate, "fresh")
	assert.NotContains(t, tracker.lastUpdate, "expired1")
	assert.NotContains(t, tracker.lastUpdate, "expired2")
	assert.Contains(t, tracker.metadata, "fresh")
	assert.NotContains(t, tracker.metadata, "expired1")
	assert.NotContains(t, tracker.metadata, "expired2")
}

func TestGetStaleMetrics(t *testing.T) {
	tracker := &StalenessTracker{
		staleThreshold: 5 * time.Minute,
		lastUpdate:     make(map[string]time.Time),
		metadata:       make(map[string]MetricMetadata),
	}

	now := time.Now()

	// Add fresh metric
	tracker.lastUpdate["fresh"] = now.Add(-1 * time.Minute)
	tracker.metadata["fresh"] = MetricMetadata{
		Value:  1.0,
		Labels: map[string]string{"status": "fresh"},
	}

	// Add stale metrics
	tracker.lastUpdate["stale1"] = now.Add(-10 * time.Minute)
	tracker.metadata["stale1"] = MetricMetadata{
		Value:  2.0,
		Labels: map[string]string{"status": "stale"},
	}

	tracker.lastUpdate["stale2"] = now.Add(-15 * time.Minute)
	tracker.metadata["stale2"] = MetricMetadata{
		Value:  3.0,
		Labels: map[string]string{"status": "very_stale"},
	}

	staleMetrics := tracker.GetStaleMetrics()

	assert.Len(t, staleMetrics, 2)
	assert.Contains(t, staleMetrics, "stale1")
	assert.Contains(t, staleMetrics, "stale2")
	assert.NotContains(t, staleMetrics, "fresh")
}

func TestGetMetricMetadata(t *testing.T) {
	tracker := &StalenessTracker{
		metadata: make(map[string]MetricMetadata),
	}

	expected := MetricMetadata{
		Value:  42.0,
		Labels: map[string]string{"test": "data"},
	}
	tracker.metadata["metric1"] = expected

	t.Run("existing_metric", func(t *testing.T) {
		meta, exists := tracker.GetMetricMetadata("metric1")
		assert.True(t, exists)
		assert.Equal(t, expected, meta)
	})

	t.Run("non_existing_metric", func(t *testing.T) {
		_, exists := tracker.GetMetricMetadata("nonexistent")
		assert.False(t, exists)
	})
}

func TestGetStats(t *testing.T) {
	tracker := &StalenessTracker{
		staleThreshold:   5 * time.Minute,
		expiredThreshold: 30 * time.Minute,
		lastUpdate:       make(map[string]time.Time),
		metadata:         make(map[string]MetricMetadata),
		metrics: &stalenessMetrics{
			totalMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			staleMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			expiredMetrics: prometheus.NewGauge(prometheus.GaugeOpts{}),
		},
	}

	now := time.Now()

	// Add metrics in different states
	tracker.lastUpdate["fresh1"] = now.Add(-1 * time.Minute)
	tracker.lastUpdate["fresh2"] = now.Add(-2 * time.Minute)
	tracker.lastUpdate["stale1"] = now.Add(-10 * time.Minute)
	tracker.lastUpdate["stale2"] = now.Add(-15 * time.Minute)
	tracker.lastUpdate["expired1"] = now.Add(-45 * time.Minute)

	// Update internal metrics
	tracker.updateMetrics()

	stats := tracker.GetStats()

	assert.Equal(t, 5, stats.TotalCount)
	assert.Equal(t, 2, stats.StaleCount)
	assert.Equal(t, 1, stats.ExpiredCount)
	assert.InDelta(t, 0.4, stats.StaleRatio, 0.01)
}

func TestConcurrentAccess(t *testing.T) {
	tracker := &StalenessTracker{
		lastUpdate: make(map[string]time.Time),
		metadata:   make(map[string]MetricMetadata),
		metrics: &stalenessMetrics{
			totalMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			updatesTotal:   prometheus.NewCounter(prometheus.CounterOpts{}),
			staleMetrics:   prometheus.NewGauge(prometheus.GaugeOpts{}),
			expiredMetrics: prometheus.NewGauge(prometheus.GaugeOpts{}),
		},
	}

	// Run concurrent updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			metricID := fmt.Sprintf("metric_%d", id)
			tracker.UpdateMetric(metricID, float64(id), nil)
			tracker.GetMetricAge(metricID)
			tracker.IsStale(metricID)
			tracker.GetConfidenceScore(metricID)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all metrics were recorded
	assert.Len(t, tracker.lastUpdate, 10)
}
