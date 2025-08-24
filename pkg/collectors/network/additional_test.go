package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// Test batch processor functionality
func TestBatchProcessor(t *testing.T) {
	processor := &MockEventProcessor{
		processedEvents: make([]*domain.CollectorEvent, 0),
	}

	bp := NewBatchProcessor(10, 100*time.Millisecond, zaptest.NewLogger(t))
	bp.SetProcessor(processor)

	// Add events
	for i := 0; i < 5; i++ {
		event := &domain.CollectorEvent{
			EventID:   "batch-" + string(rune(i)),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeNetworkConnection,
		}
		bp.AddEvent(event)
	}

	// Should have 5 pending
	assert.Equal(t, 5, bp.GetPendingCount())

	// Flush manually
	err := bp.Flush()
	assert.NoError(t, err)

	// Should have 0 pending after flush
	assert.Equal(t, 0, bp.GetPendingCount())

	// Check flush time was updated
	assert.True(t, bp.GetLastFlushTime().After(time.Now().Add(-1*time.Second)))
}

func TestBatchProcessorAutoFlush(t *testing.T) {
	processor := &MockEventProcessor{
		processedEvents: make([]*domain.CollectorEvent, 0),
	}

	bp := NewBatchProcessor(5, 50*time.Millisecond, zaptest.NewLogger(t))
	bp.SetProcessor(processor)

	// Start auto-flush
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bp.Start(ctx)

	// Add events to trigger batch size flush
	for i := 0; i < 6; i++ {
		event := &domain.CollectorEvent{
			EventID:   "auto-" + string(rune(i)),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeNetworkConnection,
		}
		bp.AddEvent(event)
	}

	// Wait for auto-flush
	time.Sleep(100 * time.Millisecond)

	// Should have flushed due to batch size
	assert.True(t, bp.GetPendingCount() < 5)

	bp.Stop()
}

// Test rate limiter functionality
func TestRateLimiterAllowance(t *testing.T) {
	limiter := NewRateLimiter(10, 1.0) // 10 events per second, no sampling

	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// Should allow around 10 (might be slightly different due to timing)
	assert.True(t, allowed >= 8 && allowed <= 12)
}

func TestRateLimiterSampling(t *testing.T) {
	limiter := NewRateLimiter(1000, 0.1) // 1000 eps, 10% sampling

	allowed := 0
	total := 1000
	for i := 0; i < total; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// With 10% sampling, should allow around 100
	assert.True(t, allowed >= 50 && allowed <= 150)
}

func TestRateLimiterStats(t *testing.T) {
	limiter := NewRateLimiter(5, 1.0)

	// Generate some traffic
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	stats := limiter.GetStats()
	assert.True(t, stats.AllowedCount > 0)
	assert.True(t, stats.DroppedCount > 0) // Some should be dropped due to rate limit
	assert.True(t, stats.CurrentRate > 0)
}

// Test event processing paths
func TestProcessEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BufferSize = 10

	collector, err := NewCollector("test-process", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Send events through channel
	for i := 0; i < 5; i++ {
		select {
		case collector.events <- &domain.CollectorEvent{
			EventID:   "evt-" + string(rune(i)),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeNetworkConnection,
		}:
		default:
		}
	}

	// Give time to process
	time.Sleep(100 * time.Millisecond)

	// Check stats were updated
	stats := collector.GetStatistics()
	assert.NotNil(t, stats)
}

// Test updateStatistics method
func TestUpdateStatistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	collector, err := NewCollector("test-stats-update", config, logger)
	require.NoError(t, err)

	// Set initial stats
	collector.stats = &NetworkCollectorStats{
		TotalEvents:      10,
		ConnectionEvents: 5,
		TCPEvents:        3,
		UDPEvents:        2,
	}

	// Update statistics
	collector.updateStatistics()

	// Check that update time was set
	assert.True(t, collector.stats.LastUpdate.After(time.Now().Add(-1*time.Second)))
}

// Test checkHealth method
func TestCheckHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BufferSize = 10

	collector, err := NewCollector("test-check-health", config, logger)
	require.NoError(t, err)

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Initial health check
	collector.checkHealth()
	health := collector.GetHealth()
	assert.Equal(t, "healthy", health.Status)
	assert.Empty(t, health.Issues)

	// Fill buffer to trigger degraded state
	for i := 0; i < 8; i++ {
		select {
		case collector.events <- &domain.CollectorEvent{
			EventID:   "health-" + string(rune(i)),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeNetworkConnection,
		}:
		default:
		}
	}

	// Check health again
	collector.checkHealth()
	health = collector.GetHealth()
	
	// Buffer usage should be high
	assert.True(t, health.BufferUsageRatio > 0.7)
}

// Test processBatches goroutine
func TestProcessBatches(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.EnableBatching = true
	config.BatchSize = 5
	config.FlushInterval = 50 * time.Millisecond

	collector, err := NewCollector("test-batches", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Send events
	for i := 0; i < 10; i++ {
		collector.processNetworkEvent(&domain.CollectorEvent{
			EventID:   "batch-" + string(rune(i)),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeNetworkConnection,
		})
	}

	// Wait for batch processing
	time.Sleep(200 * time.Millisecond)

	// Check stats
	stats := collector.GetStatistics()
	assert.True(t, stats.TotalEvents > 0)
}

// Test monitorHealth goroutine
func TestMonitorHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	collector, err := NewCollector("test-monitor", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Let health monitor run a cycle
	time.Sleep(100 * time.Millisecond)

	// Health should be updated
	health := collector.GetHealth()
	assert.NotNil(t, health)
	assert.True(t, health.LastCheck.After(time.Now().Add(-1*time.Second)))

	collector.Stop()
}

// Test K8s enrichment initialization
func TestK8sEnrichmentInit(t *testing.T) {
	enricher := NewK8sContextEnricher(zaptest.NewLogger(t))
	assert.NotNil(t, enricher)
	assert.True(t, enricher.IsEnabled())

	// Test cache operations
	cacheSize := enricher.GetCacheSize()
	assert.Equal(t, 0, cacheSize)

	// Test pod info retrieval (will be empty in test)
	podInfo := enricher.GetCachedPodInfo("test-namespace", "test-pod")
	assert.Nil(t, podInfo)
}

// Test K8s enrichment on events
func TestK8sEnrichment(t *testing.T) {
	logger := zaptest.NewLogger(t)
	enricher := NewK8sContextEnricher(logger)

	event := &domain.CollectorEvent{
		EventID:   "k8s-test",
		Timestamp: time.Now(),
		Source:    "test",
		Type:      domain.EventTypeNetworkConnection,
		Context: &domain.EventContext{
			PodName:   "test-pod",
			Namespace: "test-namespace",
		},
	}

	err := enricher.Enrich(event)
	// Should not error even if no K8s available
	assert.NoError(t, err)
}

// Test extractEventData helper
func TestExtractEventData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	collector, err := NewCollector("test-extract", config, logger)
	require.NoError(t, err)

	event := &domain.CollectorEvent{
		EventID:   "extract-test",
		Timestamp: time.Now(),
		Source:    "test",
		Type:      domain.EventTypeNetworkConnection,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				Protocol:   "tcp",
				SourceIP:   "192.168.1.1",
				SourcePort: 12345,
				DestIP:     "10.0.0.1",
				DestPort:   80,
			},
		},
	}

	data := collector.extractEventData(event)
	assert.NotNil(t, data)
	assert.Equal(t, "tcp", data["protocol"])
	assert.Equal(t, "192.168.1.1", data["src_ip"])
	assert.Equal(t, "12345", data["src_port"])
}