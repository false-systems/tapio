package correlation

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestPerformanceCorrelatorCreation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("create new correlator", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)

		assert.NotNil(t, correlator)
		assert.Equal(t, "performance", correlator.Name())
		assert.NotNil(t, correlator.recentEvents)
		assert.NotNil(t, correlator.serviceConnections)
		assert.Equal(t, 5*time.Minute, correlator.recentEvents.ttl)
	})
}

func TestPerformanceCorrelatorProcess(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("detect high CPU usage", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		// High CPU event
		event := &domain.UnifiedEvent{
			ID:        "cpu-event-1",
			Type:      EventTypeKubelet,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityWarning,
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "api-pod",
				NodeName:  "node-1",
			},
			Attributes: map[string]interface{}{
				"cpu_usage":    "95",
				"memory_usage": "60",
				"metric_type":  "resource",
			},
			Message: "High CPU usage detected",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		// Should detect performance issue
		require.Len(t, results, 1)
		result := results[0]
		assert.Equal(t, "performance_issue", result.Type)
		assert.Contains(t, result.Message, "CPU")
		assert.Equal(t, domain.EventSeverityWarning, result.Impact.Severity)
		assert.Contains(t, result.Impact.Resources, "production/api-pod")
	})

	t.Run("detect high memory usage", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		event := &domain.UnifiedEvent{
			ID:        "mem-event-1",
			Type:      EventTypeKubelet,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityError,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "database-pod",
				NodeName:  "node-2",
			},
			Attributes: map[string]interface{}{
				"cpu_usage":    "40",
				"memory_usage": "98",
				"metric_type":  "resource",
			},
			Message: "Memory exhaustion imminent",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		require.Len(t, results, 1)
		result := results[0]
		assert.Equal(t, "performance_issue", result.Type)
		assert.Contains(t, result.Message, "Memory")
		assert.Greater(t, result.Confidence, 0.8)
	})

	t.Run("detect network performance issues", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		// Network latency event
		event := &domain.UnifiedEvent{
			ID:        "net-event-1",
			Type:      EventTypeEBPF,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "frontend-pod",
			},
			Attributes: map[string]interface{}{
				"latency_ms":       "500",
				"packet_loss":      "5",
				"connection":       "frontend->backend",
				"src_ip":           TestSrcIP,
				"dst_ip":           TestDstIP,
				"connection_count": "100",
			},
			Message: "High network latency detected",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		require.Len(t, results, 1)
		result := results[0]
		assert.Equal(t, "performance_issue", result.Type)
		assert.Contains(t, result.Message, "latency")
		assert.Contains(t, result.Details, "500ms")
	})

	t.Run("correlate multiple performance events", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		baseTime := time.Now()

		// First event - high CPU
		event1 := &domain.UnifiedEvent{
			ID:        "perf-1",
			Type:      EventTypeKubelet,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "api-pod",
			},
			Attributes: map[string]interface{}{
				"cpu_usage": "85",
			},
			Message: "CPU usage high",
		}

		// Second event - high memory for same pod
		event2 := &domain.UnifiedEvent{
			ID:        "perf-2",
			Type:      EventTypeKubelet,
			Timestamp: baseTime.Add(30 * time.Second),
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "api-pod",
			},
			Attributes: map[string]interface{}{
				"memory_usage": "90",
			},
			Message: "Memory usage high",
		}

		// Process first event
		results1, err := correlator.Process(ctx, event1)
		require.NoError(t, err)
		assert.Len(t, results1, 1)

		// Process second event - should correlate with first
		results2, err := correlator.Process(ctx, event2)
		require.NoError(t, err)
		require.Len(t, results2, 1)

		result := results2[0]
		assert.Contains(t, result.Events, "perf-1")
		assert.Contains(t, result.Events, "perf-2")
		assert.Contains(t, result.Message, "resource pressure")
	})

	t.Run("nil event handling", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		results, err := correlator.Process(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, results)
	})

	t.Run("non-performance event", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		// Event without performance metrics
		event := &domain.UnifiedEvent{
			ID:        "non-perf",
			Type:      EventTypeK8s,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "config-map",
				Kind:      "ConfigMap",
			},
			Message: "ConfigMap updated",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)
		assert.Len(t, results, 0) // No performance correlation
	})
}

func TestPerformanceThresholds(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("CPU threshold detection", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		testCases := []struct {
			cpuUsage string
			expected bool
			severity domain.EventSeverity
		}{
			{"50", false, ""},
			{"75", false, ""},
			{"80", true, domain.EventSeverityWarning},
			{"90", true, domain.EventSeverityError},
			{"95", true, domain.EventSeverityCritical},
		}

		for _, tc := range testCases {
			event := &domain.UnifiedEvent{
				ID:        "cpu-test-" + tc.cpuUsage,
				Type:      EventTypeKubelet,
				Timestamp: time.Now(),
				K8sContext: &domain.K8sContext{
					Namespace: "test",
					Name:      "test-pod",
				},
				Attributes: map[string]interface{}{
					"cpu_usage": tc.cpuUsage,
				},
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if tc.expected {
				require.Len(t, results, 1)
				if tc.severity != "" {
					assert.Equal(t, tc.severity, results[0].Impact.Severity)
				}
			} else {
				assert.Len(t, results, 0)
			}
		}
	})

	t.Run("memory threshold detection", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		testCases := []struct {
			memUsage string
			expected bool
			severity domain.EventSeverity
		}{
			{"60", false, ""},
			{"79", false, ""},
			{"85", true, domain.EventSeverityWarning},
			{"92", true, domain.EventSeverityError},
			{"98", true, domain.EventSeverityCritical},
		}

		for _, tc := range testCases {
			event := &domain.UnifiedEvent{
				ID:        "mem-test-" + tc.memUsage,
				Type:      EventTypeKubelet,
				Timestamp: time.Now(),
				K8sContext: &domain.K8sContext{
					Namespace: "test",
					Name:      "test-pod",
				},
				Attributes: map[string]interface{}{
					"memory_usage": tc.memUsage,
				},
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if tc.expected {
				require.Len(t, results, 1)
				if tc.severity != "" {
					assert.Equal(t, tc.severity, results[0].Impact.Severity)
				}
			} else {
				assert.Len(t, results, 0)
			}
		}
	})
}

func TestServiceConnectionTracking(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("track connection failures", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		baseTime := time.Now()

		// Multiple connection failure events
		for i := 0; i < 5; i++ {
			event := &domain.UnifiedEvent{
				ID:        "conn-fail-" + strconv.Itoa(i),
				Type:      EventTypeEBPF,
				Timestamp: baseTime.Add(time.Duration(i) * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "frontend-pod",
				},
				Attributes: map[string]interface{}{
					"connection": "frontend->backend",
					"status":     "failed",
					"src_ip":     TestSrcIP,
					"dst_ip":     TestDstIP,
				},
				Message: "Connection failed",
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i >= 2 { // After multiple failures
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "connection")
				assert.Contains(t, result.Details, "failures")
			}
		}
	})

	t.Run("connection state management", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		// Successful connection
		successEvent := &domain.UnifiedEvent{
			ID:        "conn-success",
			Type:      EventTypeEBPF,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "app-pod",
			},
			Attributes: map[string]interface{}{
				"connection": "app->database",
				"status":     "established",
				"latency_ms": "5",
			},
			Message: "Connection established",
		}

		results, err := correlator.Process(ctx, successEvent)
		require.NoError(t, err)
		assert.Len(t, results, 0) // No issue with successful connection

		// Check connection state
		correlator.connMu.RLock()
		state, exists := correlator.serviceConnections["app->database"]
		correlator.connMu.RUnlock()

		assert.True(t, exists)
		assert.Equal(t, "app", state.Source)
		assert.Equal(t, "database", state.Destination)
		assert.Equal(t, 0, state.FailureCount)
	})
}

func TestRecentEventsCache(t *testing.T) {
	t.Run("add and retrieve events", func(t *testing.T) {
		cache := &RecentEventsCache{
			events: make(map[string][]*domain.UnifiedEvent),
			ttl:    1 * time.Minute,
		}

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Timestamp: time.Now().Add(10 * time.Second),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		key := "default/pod-1"

		cache.mu.Lock()
		cache.events[key] = append(cache.events[key], event1, event2)
		cache.mu.Unlock()

		cache.mu.RLock()
		events := cache.events[key]
		cache.mu.RUnlock()

		assert.Len(t, events, 2)
		assert.Equal(t, "event-1", events[0].ID)
		assert.Equal(t, "event-2", events[1].ID)
	})

	t.Run("cleanup old events", func(t *testing.T) {
		cache := &RecentEventsCache{
			events: make(map[string][]*domain.UnifiedEvent),
			ttl:    100 * time.Millisecond,
		}

		oldEvent := &domain.UnifiedEvent{
			ID:        "old-event",
			Timestamp: time.Now().Add(-200 * time.Millisecond),
		}

		recentEvent := &domain.UnifiedEvent{
			ID:        "recent-event",
			Timestamp: time.Now(),
		}

		key := "test/pod"

		cache.mu.Lock()
		cache.events[key] = []*domain.UnifiedEvent{oldEvent, recentEvent}
		cache.mu.Unlock()

		// Simulate cleanup
		cache.mu.Lock()
		now := time.Now()
		for k, events := range cache.events {
			var filtered []*domain.UnifiedEvent
			for _, e := range events {
				if now.Sub(e.Timestamp) <= cache.ttl {
					filtered = append(filtered, e)
				}
			}
			if len(filtered) > 0 {
				cache.events[k] = filtered
			} else {
				delete(cache.events, k)
			}
		}
		cache.mu.Unlock()

		cache.mu.RLock()
		events := cache.events[key]
		cache.mu.RUnlock()

		assert.Len(t, events, 1)
		assert.Equal(t, "recent-event", events[0].ID)
	})
}

func TestPerformanceCorrelatorConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("concurrent event processing", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		eventCount := 100
		errChan := make(chan error, eventCount)

		for i := 0; i < eventCount; i++ {
			go func(id int) {
				event := &domain.UnifiedEvent{
					ID:        "concurrent-" + strconv.Itoa(id),
					Type:      EventTypeKubelet,
					Timestamp: time.Now(),
					K8sContext: &domain.K8sContext{
						Namespace: "test",
						Name:      "pod-" + strconv.Itoa(id%10),
					},
					Attributes: map[string]interface{}{
						"cpu_usage":    strconv.Itoa(70 + id%30),
						"memory_usage": strconv.Itoa(60 + id%40),
					},
				}

				_, err := correlator.Process(ctx, event)
				errChan <- err
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < eventCount; i++ {
			err := <-errChan
			assert.NoError(t, err)
		}
	})
}

func TestPerformancePatternDetection(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("detect resource exhaustion pattern", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		baseTime := time.Now()

		// Gradually increasing resource usage
		for i := 0; i < 5; i++ {
			event := &domain.UnifiedEvent{
				ID:        "exhaustion-" + strconv.Itoa(i),
				Type:      EventTypeKubelet,
				Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "leaky-pod",
				},
				Attributes: map[string]interface{}{
					"memory_usage": strconv.Itoa(70 + i*5), // 70, 75, 80, 85, 90
				},
				Message: "Memory usage increasing",
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i >= 2 && 70+i*5 >= 80 { // Memory >= 80%
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "Memory")

				if i == 4 { // Last iteration with 90% memory
					assert.Contains(t, result.Details, "exhaustion")
				}
			}
		}
	})

	t.Run("detect cascading performance issues", func(t *testing.T) {
		correlator := NewPerformanceCorrelator(logger)
		ctx := context.Background()

		baseTime := time.Now()

		// Database performance issue
		dbEvent := &domain.UnifiedEvent{
			ID:        "cascade-1",
			Type:      EventTypeKubelet,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "database-pod",
			},
			Attributes: map[string]interface{}{
				"cpu_usage":  "95",
				"disk_io":    "high",
				"query_time": "5000", // 5 seconds
			},
			Message: "Database performance degraded",
		}

		// API performance issue (caused by DB)
		apiEvent := &domain.UnifiedEvent{
			ID:        "cascade-2",
			Type:      EventTypeKubelet,
			Timestamp: baseTime.Add(10 * time.Second),
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "api-pod",
			},
			Attributes: map[string]interface{}{
				"response_time": "3000", // 3 seconds
				"queue_size":    "500",
			},
			Message: "API response time increased",
		}

		// Frontend timeout (caused by API)
		frontendEvent := &domain.UnifiedEvent{
			ID:        "cascade-3",
			Type:      EventTypeEBPF,
			Timestamp: baseTime.Add(20 * time.Second),
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "frontend-pod",
			},
			Attributes: map[string]interface{}{
				"timeout_count": "50",
				"error_rate":    "25",
			},
			Message: "Frontend experiencing timeouts",
		}

		// Process cascade
		results1, err := correlator.Process(ctx, dbEvent)
		require.NoError(t, err)
		assert.Len(t, results1, 1)

		results2, err := correlator.Process(ctx, apiEvent)
		require.NoError(t, err)
		assert.Len(t, results2, 1)

		results3, err := correlator.Process(ctx, frontendEvent)
		require.NoError(t, err)
		require.Len(t, results3, 1)

		// Final result should show cascade
		result := results3[0]
		assert.Contains(t, result.Message, "cascade")
		assert.Contains(t, result.Events, "cascade-1")
		assert.Contains(t, result.Events, "cascade-2")
		assert.Contains(t, result.Events, "cascade-3")
	})
}

func BenchmarkPerformanceCorrelatorProcess(b *testing.B) {
	logger := zaptest.NewLogger(b).Sugar().Desugar()
	correlator := NewPerformanceCorrelator(logger)
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      EventTypeKubelet,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "benchmark",
			Name:      "bench-pod",
		},
		Attributes: map[string]interface{}{
			"cpu_usage":    "85",
			"memory_usage": "75",
		},
		Message: "Benchmark event",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := correlator.Process(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
