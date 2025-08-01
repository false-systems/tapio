package correlation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
)

func TestNewCorrelationMetrics(t *testing.T) {
	logger := zap.NewNop()

	// Set up test meter provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)
	assert.NotNil(t, metrics)

	// Verify all metrics are initialized
	assert.NotNil(t, metrics.eventsProcessed)
	assert.NotNil(t, metrics.eventsDropped)
	assert.NotNil(t, metrics.processingLatency)
	assert.NotNil(t, metrics.correlationsFound)
	assert.NotNil(t, metrics.correlationConfidence)
	assert.NotNil(t, metrics.patternsDetected)
	assert.NotNil(t, metrics.sequencesCompleted)
	assert.NotNil(t, metrics.temporalMatches)
	assert.NotNil(t, metrics.ownershipCacheHits)
	assert.NotNil(t, metrics.ownershipCacheMisses)
	assert.NotNil(t, metrics.selectorCacheHits)
	assert.NotNil(t, metrics.selectorCacheMisses)
	assert.NotNil(t, metrics.cacheEvictions)
	assert.NotNil(t, metrics.totalErrors)

	// Verify type-specific counters
	assert.Len(t, metrics.correlationsByType, 6)
	assert.Len(t, metrics.errorsByType, 5)
}

func TestRecordEventProcessed(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record some events
	metrics.RecordEventProcessed(ctx, "kubernetes", 100*time.Millisecond)
	metrics.RecordEventProcessed(ctx, "network", 50*time.Millisecond)
	metrics.RecordEventProcessed(ctx, "kubernetes", 150*time.Millisecond)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify counter incremented
	foundCounter := false
	foundHistogram := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.events.processed" {
				foundCounter = true
				data, ok := m.Data.(metricdata.Sum[int64])
				assert.True(t, ok)
				assert.Equal(t, 2, len(data.DataPoints)) // 2 event types
			}
			if m.Name == "tapio.correlation.processing.latency" {
				foundHistogram = true
				data, ok := m.Data.(metricdata.Histogram[float64])
				assert.True(t, ok)
				assert.Equal(t, 2, len(data.DataPoints)) // 2 event types
			}
		}
	}

	assert.True(t, foundCounter, "Should find events processed counter")
	assert.True(t, foundHistogram, "Should find processing latency histogram")
}

func TestRecordEventDropped(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record dropped events
	metrics.RecordEventDropped(ctx, "queue_full")
	metrics.RecordEventDropped(ctx, "invalid_format")
	metrics.RecordEventDropped(ctx, "queue_full")

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify counter
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.events.dropped" {
				found = true
				data, ok := m.Data.(metricdata.Sum[int64])
				assert.True(t, ok)
				assert.Equal(t, 2, len(data.DataPoints)) // 2 drop reasons
			}
		}
	}
	assert.True(t, found)
}

func TestRecordCorrelation(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record correlations
	metrics.RecordCorrelation(ctx, "ownership", 0.95, 3)
	metrics.RecordCorrelation(ctx, "temporal", 0.75, 2)
	metrics.RecordCorrelation(ctx, "ownership", 0.85, 4)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Check metrics were recorded
	foundCounter := false
	foundConfidence := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.correlations.found" {
				foundCounter = true
			}
			if m.Name == "tapio.correlation.confidence" {
				foundConfidence = true
			}
		}
	}

	assert.True(t, foundCounter)
	assert.True(t, foundConfidence)
}

func TestRecordPatternDetected(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record patterns
	metrics.RecordPatternDetected(ctx, "cascade_failure", 0.85, 5)
	metrics.RecordPatternDetected(ctx, "deployment_sequence", 0.92, 3)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify patterns recorded
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.patterns.detected" {
				found = true
			}
		}
	}
	assert.True(t, found)
}

func TestRecordSequenceCompleted(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record sequence completions
	metrics.RecordSequenceCompleted(ctx, 5, 30*time.Second)
	metrics.RecordSequenceCompleted(ctx, 3, 15*time.Second)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify recorded
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.sequences.completed" {
				found = true
			}
		}
	}
	assert.True(t, found)
}

func TestRecordTemporalMatch(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record temporal matches
	metrics.RecordTemporalMatch(ctx, 5*time.Second, 0.8)
	metrics.RecordTemporalMatch(ctx, 30*time.Second, 0.95)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify recorded
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.temporal.matches" {
				found = true
			}
		}
	}
	assert.True(t, found)
}

func TestCacheMetrics(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record cache operations
	metrics.RecordCacheHit(ctx, "ownership")
	metrics.RecordCacheHit(ctx, "ownership")
	metrics.RecordCacheMiss(ctx, "ownership")
	metrics.RecordCacheHit(ctx, "selector")
	metrics.RecordCacheMiss(ctx, "selector")
	metrics.RecordCacheEviction(ctx, "ownership", 10)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Check cache metrics
	foundOwnershipHits := false
	foundSelectorHits := false
	foundEvictions := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "tapio.correlation.k8s.ownership.cache.hits":
				foundOwnershipHits = true
			case "tapio.correlation.k8s.selector.cache.hits":
				foundSelectorHits = true
			case "tapio.correlation.cache.evictions":
				foundEvictions = true
			}
		}
	}

	assert.True(t, foundOwnershipHits)
	assert.True(t, foundSelectorHits)
	assert.True(t, foundEvictions)
}

func TestRecordError(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Record errors
	metrics.RecordError(ctx, "processing", errors.New("invalid event format"))
	metrics.RecordError(ctx, "k8s_api", errors.New("connection refused"))
	metrics.RecordError(ctx, "processing", errors.New("nil pointer"))

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify error metrics
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tapio.correlation.errors.total" {
				found = true
			}
		}
	}
	assert.True(t, found)
}

func TestObservableMetrics(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Update observable values
	metrics.UpdateQueueSize(42)
	metrics.UpdateActiveCorrelations(10)
	metrics.UpdateRelationshipCount(100)
	metrics.UpdateMemoryUsage(1024 * 1024) // 1MB
	metrics.UpdateGoroutineCount(25)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Check observable gauges
	foundQueue := false
	foundActive := false
	foundRelationships := false
	foundMemory := false
	foundGoroutines := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "tapio.correlation.queue.size":
				foundQueue = true
				data, ok := m.Data.(metricdata.Gauge[int64])
				assert.True(t, ok)
				if len(data.DataPoints) > 0 {
					assert.Equal(t, int64(42), data.DataPoints[0].Value)
				}
			case "tapio.correlation.active":
				foundActive = true
			case "tapio.correlation.k8s.relationships.loaded":
				foundRelationships = true
			case "tapio.correlation.memory.usage":
				foundMemory = true
			case "tapio.correlation.goroutines":
				foundGoroutines = true
			}
		}
	}

	assert.True(t, foundQueue)
	assert.True(t, foundActive)
	assert.True(t, foundRelationships)
	assert.True(t, foundMemory)
	assert.True(t, foundGoroutines)
}

func TestGetCacheStats(t *testing.T) {
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Set some values
	metrics.UpdateQueueSize(50)
	metrics.UpdateActiveCorrelations(15)
	metrics.UpdateRelationshipCount(200)
	metrics.UpdateMemoryUsage(2048)
	metrics.UpdateGoroutineCount(30)

	// Get stats
	stats := metrics.GetCacheStats()

	assert.Equal(t, int64(50), stats["queue_size"])
	assert.Equal(t, int64(15), stats["active_correlations"])
	assert.Equal(t, int64(200), stats["relationships"])
	assert.Equal(t, int64(2048), stats["memory_bytes"])
	assert.Equal(t, int64(30), stats["goroutines"])
}

// Integration test showing how metrics would be used
func TestMetricsIntegration(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	metrics, err := NewCorrelationMetrics(logger)
	require.NoError(t, err)

	// Simulate processing events
	start := time.Now()

	// Process event
	metrics.RecordEventProcessed(ctx, "kubernetes", time.Since(start))

	// Check cache
	metrics.RecordCacheMiss(ctx, "ownership")

	// Find correlation
	metrics.RecordCorrelation(ctx, "ownership", 0.9, 2)

	// Detect pattern
	metrics.RecordPatternDetected(ctx, "pod_restart_loop", 0.85, 3)

	// Complete sequence
	metrics.RecordSequenceCompleted(ctx, 4, 45*time.Second)

	// Update gauges
	metrics.UpdateQueueSize(10)
	metrics.UpdateActiveCorrelations(5)

	// Collect all metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify we have metrics
	assert.NotEmpty(t, rm.ScopeMetrics)

	// Count total metrics
	totalMetrics := 0
	for _, sm := range rm.ScopeMetrics {
		totalMetrics += len(sm.Metrics)
	}
	assert.Greater(t, totalMetrics, 10) // Should have many metrics
}
