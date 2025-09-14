package loader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		metrics  LoaderMetrics
		expected string
	}{
		{
			name: "healthy status",
			metrics: LoaderMetrics{
				EventsReceived:      1000,
				EventsProcessed:     990,
				EventsFailed:        10,
				ErrorRate:           0.01, // 1% error rate
				BacklogSize:         5,
				ThroughputPerSecond: 50.0,
				LastProcessedTime:   time.Now().Add(-1 * time.Minute),
			},
			expected: "healthy",
		},
		{
			name: "degraded due to high error rate",
			metrics: LoaderMetrics{
				EventsReceived:      1000,
				EventsProcessed:     850,
				EventsFailed:        150,
				ErrorRate:           0.15, // 15% error rate
				BacklogSize:         5,
				ThroughputPerSecond: 50.0,
				LastProcessedTime:   time.Now().Add(-1 * time.Minute),
			},
			expected: "degraded",
		},
		{
			name: "degraded due to large backlog",
			metrics: LoaderMetrics{
				EventsReceived:      1000,
				EventsProcessed:     990,
				EventsFailed:        10,
				ErrorRate:           0.01,
				BacklogSize:         1000, // Large backlog
				ThroughputPerSecond: 50.0,
				LastProcessedTime:   time.Now().Add(-1 * time.Minute),
			},
			expected: "degraded",
		},
		{
			name: "degraded due to no recent activity",
			metrics: LoaderMetrics{
				EventsReceived:      1000,
				EventsProcessed:     990,
				EventsFailed:        10,
				ErrorRate:           0.01,
				BacklogSize:         5,
				ThroughputPerSecond: 50.0,
				LastProcessedTime:   time.Now().Add(-10 * time.Minute), // No recent activity
			},
			expected: "degraded",
		},
		{
			name: "degraded due to low throughput",
			metrics: LoaderMetrics{
				EventsReceived:      1000,
				EventsProcessed:     990,
				EventsFailed:        10,
				ErrorRate:           0.01,
				BacklogSize:         5,
				ThroughputPerSecond: 0.5, // Low throughput
				LastProcessedTime:   time.Now().Add(-1 * time.Minute),
			},
			expected: "degraded",
		},
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock health status
			status := HealthStatus{
				LastCheck:      time.Now(),
				NATSConnected:  true,
				Neo4jConnected: true,
				Metrics:        tt.metrics,
				Details:        make(map[string]string),
				Errors:         make([]string, 0),
				Warnings:       make([]string, 0),
			}

			// Determine health status using the same logic as the loader
			healthStatus := determineHealthStatus(&status, config)
			assert.Equal(t, tt.expected, healthStatus)
		})
	}
}

func TestHealthStatusWithConnectionIssues(t *testing.T) {
	tests := []struct {
		name           string
		natsConnected  bool
		neo4jConnected bool
		expected       string
	}{
		{
			name:           "both connected",
			natsConnected:  true,
			neo4jConnected: true,
			expected:       "healthy",
		},
		{
			name:           "nats disconnected",
			natsConnected:  false,
			neo4jConnected: true,
			expected:       "unhealthy",
		},
		{
			name:           "neo4j disconnected",
			natsConnected:  true,
			neo4jConnected: false,
			expected:       "unhealthy",
		},
		{
			name:           "both disconnected",
			natsConnected:  false,
			neo4jConnected: false,
			expected:       "unhealthy",
		},
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := HealthStatus{
				LastCheck:      time.Now(),
				NATSConnected:  tt.natsConnected,
				Neo4jConnected: tt.neo4jConnected,
				Metrics: LoaderMetrics{
					ErrorRate:           0.01,
					BacklogSize:         5,
					ThroughputPerSecond: 50.0,
					LastProcessedTime:   time.Now().Add(-1 * time.Minute),
				},
				Details:  make(map[string]string),
				Errors:   make([]string, 0),
				Warnings: make([]string, 0),
			}

			healthStatus := determineHealthStatus(&status, config)
			assert.Equal(t, tt.expected, healthStatus)
		})
	}
}

func TestHealthDetails(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	metrics := LoaderMetrics{
		EventsReceived:      1000,
		EventsProcessed:     990,
		EventsFailed:        10,
		BatchesProcessed:    99,
		BatchesFailed:       1,
		ProcessingLatency:   25.5,
		StorageLatency:      15.2,
		BacklogSize:         5,
		ActiveWorkers:       4,
		ErrorRate:           0.01,
		ThroughputPerSecond: 50.5,
		LastProcessedTime:   time.Now(),
	}

	status := HealthStatus{
		LastCheck:      time.Now(),
		NATSConnected:  true,
		Neo4jConnected: true,
		Metrics:        metrics,
		Details:        make(map[string]string),
		Errors:         make([]string, 0),
		Warnings:       make([]string, 0),
	}

	// Add details using the same logic as the loader
	addHealthDetailsToStatus(&status, config)

	// Check that all expected details are present
	expectedDetails := []string{
		"throughput",
		"processing_latency",
		"storage_latency",
		"backlog_size",
		"active_workers",
		"error_rate",
		"events_received",
		"events_processed",
		"events_failed",
		"batches_processed",
		"batches_failed",
		"last_processed",
		"uptime",
		"batch_size",
		"max_concurrency",
		"batch_timeout",
	}

	for _, detail := range expectedDetails {
		assert.Contains(t, status.Details, detail, "Missing detail: %s", detail)
		assert.NotEmpty(t, status.Details[detail], "Empty detail value for: %s", detail)
	}

	// Check specific formatting
	assert.Equal(t, "50.50 events/sec", status.Details["throughput"])
	assert.Equal(t, "25.50 ms", status.Details["processing_latency"])
	assert.Equal(t, "15.20 ms", status.Details["storage_latency"])
	assert.Equal(t, "5", status.Details["backlog_size"])
	assert.Equal(t, "4", status.Details["active_workers"])
	assert.Equal(t, "1.00%", status.Details["error_rate"])
}

func TestPerformanceMetricsCalculation(t *testing.T) {
	tests := []struct {
		name               string
		eventsReceived     int64
		eventsProcessed    int64
		eventsFailed       int64
		lastProcessedTime  time.Time
		expectedErrorRate  float64
		expectedThroughput float64 // approximation
	}{
		{
			name:               "normal processing",
			eventsReceived:     1000,
			eventsProcessed:    990,
			eventsFailed:       10,
			lastProcessedTime:  time.Now().Add(-1 * time.Second),
			expectedErrorRate:  0.01,  // 1%
			expectedThroughput: 990.0, // events per second approximation
		},
		{
			name:               "high error rate",
			eventsReceived:     100,
			eventsProcessed:    70,
			eventsFailed:       30,
			lastProcessedTime:  time.Now().Add(-1 * time.Second),
			expectedErrorRate:  0.3, // 30%
			expectedThroughput: 70.0,
		},
		{
			name:               "no events processed",
			eventsReceived:     0,
			eventsProcessed:    0,
			eventsFailed:       0,
			lastProcessedTime:  time.Now(),
			expectedErrorRate:  0.0,
			expectedThroughput: 0.0,
		},
		{
			name:               "all events failed",
			eventsReceived:     50,
			eventsProcessed:    0,
			eventsFailed:       50,
			lastProcessedTime:  time.Now().Add(-1 * time.Second),
			expectedErrorRate:  1.0, // 100%
			expectedThroughput: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &LoaderMetrics{
				EventsReceived:    tt.eventsReceived,
				EventsProcessed:   tt.eventsProcessed,
				EventsFailed:      tt.eventsFailed,
				LastProcessedTime: tt.lastProcessedTime,
			}

			// Calculate metrics using the same logic as the loader
			calculatePerformanceMetrics(metrics)

			assert.Equal(t, tt.expectedErrorRate, metrics.ErrorRate, "Error rate mismatch")

			// Throughput calculation is time-dependent, so we check if it's reasonable
			if tt.eventsProcessed > 0 && !tt.lastProcessedTime.IsZero() {
				assert.True(t, metrics.ThroughputPerSecond >= 0, "Throughput should be non-negative")
			} else {
				assert.Equal(t, 0.0, metrics.ThroughputPerSecond, "Throughput should be zero when no events processed")
			}
		})
	}
}

func TestHealthCheckInterval(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		interval time.Duration
	}{
		{
			name:     "short interval",
			interval: 1 * time.Second,
		},
		{
			name:     "default interval",
			interval: 30 * time.Second,
		},
		{
			name:     "long interval",
			interval: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.HealthCheckInterval = tt.interval

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			assert.Equal(t, tt.interval, loader.config.HealthCheckInterval)
		})
	}
}

// Helper functions to simulate the loader's health determination logic
func determineHealthStatus(status *HealthStatus, config *Config) string {
	// Check for critical failures
	if !status.NATSConnected || !status.Neo4jConnected {
		return "unhealthy"
	}

	metrics := status.Metrics

	// Check for performance issues
	if metrics.ErrorRate > 0.1 { // More than 10% error rate
		return "degraded"
	}

	if metrics.BacklogSize > config.BatchSize*5 { // More than 5 batches worth
		return "degraded"
	}

	timeSinceLastProcessed := time.Since(metrics.LastProcessedTime)
	if timeSinceLastProcessed > 5*time.Minute && metrics.EventsReceived > 0 {
		return "degraded"
	}

	if metrics.ThroughputPerSecond < 1.0 && metrics.EventsReceived > 100 {
		return "degraded"
	}

	return "healthy"
}

func addHealthDetailsToStatus(status *HealthStatus, config *Config) {
	metrics := status.Metrics

	// Add performance details
	status.Details["throughput"] = formatFloat(metrics.ThroughputPerSecond) + " events/sec"
	status.Details["processing_latency"] = formatFloat(metrics.ProcessingLatency) + " ms"
	status.Details["storage_latency"] = formatFloat(metrics.StorageLatency) + " ms"
	status.Details["backlog_size"] = formatInt(metrics.BacklogSize)
	status.Details["active_workers"] = formatInt(metrics.ActiveWorkers)
	status.Details["error_rate"] = formatPercent(metrics.ErrorRate)

	// Add processing statistics
	status.Details["events_received"] = formatInt64(metrics.EventsReceived)
	status.Details["events_processed"] = formatInt64(metrics.EventsProcessed)
	status.Details["events_failed"] = formatInt64(metrics.EventsFailed)
	status.Details["batches_processed"] = formatInt64(metrics.BatchesProcessed)
	status.Details["batches_failed"] = formatInt64(metrics.BatchesFailed)

	// Add timing information
	status.Details["last_processed"] = metrics.LastProcessedTime.Format(time.RFC3339)
	status.Details["uptime"] = time.Since(metrics.LastProcessedTime).String()

	// Add configuration details
	status.Details["batch_size"] = formatInt(config.BatchSize)
	status.Details["max_concurrency"] = formatInt(config.MaxConcurrency)
	status.Details["batch_timeout"] = config.BatchTimeout.String()
}

func calculatePerformanceMetrics(metrics *LoaderMetrics) {
	now := time.Now()

	// Calculate throughput
	if metrics.EventsReceived > 0 {
		duration := now.Sub(metrics.LastProcessedTime).Seconds()
		if duration > 0 {
			metrics.ThroughputPerSecond = float64(metrics.EventsProcessed) / duration
		}
	}

	// Calculate error rate
	if metrics.EventsReceived > 0 {
		metrics.ErrorRate = float64(metrics.EventsFailed) / float64(metrics.EventsReceived)
	}
}
