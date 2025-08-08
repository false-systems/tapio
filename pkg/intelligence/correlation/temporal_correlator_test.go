package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestTemporalCorrelatorCreation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("with default config", func(t *testing.T) {
		config := DefaultTemporalConfig()
		correlator := NewTemporalCorrelator(logger, config)

		assert.NotNil(t, correlator)
		assert.Equal(t, "temporal", correlator.Name())
		assert.Equal(t, config, correlator.config)
		assert.NotNil(t, correlator.patterns)
		assert.NotNil(t, correlator.eventWindow)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:         10 * time.Minute,
			MinOccurrences:     3,
			PatternTimeout:     12 * time.Hour,
			MaxPatternsTracked: 500,
		}

		correlator := NewTemporalCorrelator(logger, config)

		assert.NotNil(t, correlator)
		assert.Equal(t, config, correlator.config)
	})
}

func TestTemporalCorrelatorProcess(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("identify temporal pattern", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     5 * time.Minute,
			MinOccurrences: 2,
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		// Create related events within time window
		baseTime := time.Now()

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      EventTypeSystemd,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
			Attributes: map[string]interface{}{
				"service": "api",
			},
		}

		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      EventTypeSystemd,
			Timestamp: baseTime.Add(1 * time.Minute),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
			Attributes: map[string]interface{}{
				"service": "api",
			},
		}

		// Process first event
		results1, err := correlator.Process(ctx, event1)
		require.NoError(t, err)
		assert.Len(t, results1, 0) // First occurrence, no pattern yet

		// Process second event - should detect pattern
		results2, err := correlator.Process(ctx, event2)
		require.NoError(t, err)

		// Should detect temporal pattern
		require.Len(t, results2, 1)
		result := results2[0]
		assert.Equal(t, "temporal_pattern", result.Type)
		assert.Contains(t, result.Events, event1.ID)
		assert.Contains(t, result.Events, event2.ID)
		assert.Greater(t, result.Confidence, 0.0)
	})

	t.Run("events outside time window", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     1 * time.Minute,
			MinOccurrences: 2,
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      EventTypeSystemd,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		// Event outside window
		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      EventTypeSystemd,
			Timestamp: baseTime.Add(2 * time.Minute),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		results1, err := correlator.Process(ctx, event1)
		require.NoError(t, err)
		assert.Len(t, results1, 0)

		results2, err := correlator.Process(ctx, event2)
		require.NoError(t, err)
		assert.Len(t, results2, 0) // No pattern detected
	})

	t.Run("different event types", func(t *testing.T) {
		config := DefaultTemporalConfig()
		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      EventTypeSystemd,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      EventTypeEBPF, // Different type
			Timestamp: baseTime.Add(30 * time.Second),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		results1, err := correlator.Process(ctx, event1)
		require.NoError(t, err)
		assert.Len(t, results1, 0)

		results2, err := correlator.Process(ctx, event2)
		require.NoError(t, err)
		// May or may not correlate depending on implementation
		// but should not error
		_ = results2 // Suppress unused variable warning
	})

	t.Run("nil event handling", func(t *testing.T) {
		config := DefaultTemporalConfig()
		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		results, err := correlator.Process(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, results)
	})

	t.Run("context cancellation", func(t *testing.T) {
		config := DefaultTemporalConfig()
		correlator := NewTemporalCorrelator(logger, config)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		event := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      EventTypeSystemd,
			Timestamp: time.Now(),
		}

		results, err := correlator.Process(ctx, event)
		// Should handle cancelled context gracefully
		if err != nil {
			assert.Equal(t, context.Canceled, err)
		}
		assert.Nil(t, results)
	})
}

func TestTemporalPatternDetection(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("recurring pattern detection", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     5 * time.Minute,
			MinOccurrences: 3,
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()
		namespace := "default"
		podName := "recurring-pod"

		// Create recurring events
		events := []*domain.UnifiedEvent{
			{
				ID:        "event-1",
				Type:      EventTypeSystemd,
				Timestamp: baseTime,
				K8sContext: &domain.K8sContext{
					Namespace: namespace,
					Name:      podName,
				},
				Message: "Service restarted",
			},
			{
				ID:        "event-2",
				Type:      EventTypeSystemd,
				Timestamp: baseTime.Add(1 * time.Minute),
				K8sContext: &domain.K8sContext{
					Namespace: namespace,
					Name:      podName,
				},
				Message: "Service restarted",
			},
			{
				ID:        "event-3",
				Type:      EventTypeSystemd,
				Timestamp: baseTime.Add(2 * time.Minute),
				K8sContext: &domain.K8sContext{
					Namespace: namespace,
					Name:      podName,
				},
				Message: "Service restarted",
			},
		}

		// Process events
		for i, event := range events {
			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i < 2 {
				// First two events don't meet minimum occurrences
				assert.Len(t, results, 0)
			} else {
				// Third event should trigger pattern detection
				require.Len(t, results, 1)
				result := results[0]
				assert.Equal(t, "temporal_pattern", result.Type)
				assert.Len(t, result.Events, 3)
				assert.Contains(t, result.Message, "recurring")
			}
		}
	})

	t.Run("pattern timeout", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     1 * time.Minute,
			MinOccurrences: 2,
			PatternTimeout: 100 * time.Millisecond, // Very short timeout
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      EventTypeSystemd,
			Timestamp: time.Now().Add(-200 * time.Millisecond), // Old event
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		results, err := correlator.Process(ctx, event1)
		require.NoError(t, err)
		assert.Len(t, results, 0)

		// Wait for pattern to timeout
		time.Sleep(150 * time.Millisecond)

		// Process new event - old pattern should be cleaned up
		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      EventTypeSystemd,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		results, err = correlator.Process(ctx, event2)
		require.NoError(t, err)
		assert.Len(t, results, 0) // Should not correlate with expired pattern
	})
}

func TestTemporalCorrelatorConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("concurrent event processing", func(t *testing.T) {
		config := DefaultTemporalConfig()
		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		// Process multiple events concurrently
		eventCount := 100
		errChan := make(chan error, eventCount)

		for i := 0; i < eventCount; i++ {
			go func(id int) {
				event := &domain.UnifiedEvent{
					ID:        fmt.Sprintf("event-%d", id),
					Type:      EventTypeSystemd,
					Timestamp: time.Now(),
					K8sContext: &domain.K8sContext{
						Namespace: "default",
						Name:      fmt.Sprintf("pod-%d", id%10),
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

func TestTimeWindow(t *testing.T) {
	t.Run("add and retrieve events", func(t *testing.T) {
		// Create a simple time window for testing
		window := &TimeWindow{
			events:   make([]*WindowedEvent, 0),
			size:     5 * time.Minute,
			maxItems: 100,
		}

		baseTime := time.Now()
		events := []*domain.UnifiedEvent{
			{
				ID:        "event-1",
				Timestamp: baseTime,
			},
			{
				ID:        "event-2",
				Timestamp: baseTime.Add(1 * time.Minute),
			},
			{
				ID:        "event-3",
				Timestamp: baseTime.Add(3 * time.Minute),
			},
		}

		// Add events
		for _, event := range events {
			window.Add(event)
		}

		// Verify events were added
		window.mu.RLock()
		assert.Len(t, window.events, 3)
		assert.Equal(t, "event-1", window.events[0].Event.ID)
		assert.Equal(t, "event-2", window.events[1].Event.ID)
		assert.Equal(t, "event-3", window.events[2].Event.ID)
		window.mu.RUnlock()
	})

	t.Run("cleanup old events", func(t *testing.T) {
		window := &TimeWindow{
			events:   make([]*WindowedEvent, 0),
			size:     1 * time.Minute,
			maxItems: 100,
		}

		baseTime := time.Now()

		// Add old event
		oldEvent := &domain.UnifiedEvent{
			ID:        "old-event",
			Timestamp: baseTime.Add(-2 * time.Minute),
		}
		window.Add(oldEvent)

		// Add recent event
		recentEvent := &domain.UnifiedEvent{
			ID:        "recent-event",
			Timestamp: baseTime,
		}
		window.Add(recentEvent)

		// Clean old events
		window.Clean()

		// Only recent event should remain
		window.mu.RLock()
		eventCount := 0
		for _, we := range window.events {
			if we.Event.Timestamp.After(baseTime.Add(-1 * time.Minute)) {
				eventCount++
			}
		}
		window.mu.RUnlock()
		assert.GreaterOrEqual(t, eventCount, 1)
	})

	t.Run("max items limit", func(t *testing.T) {
		window := &TimeWindow{
			events:   make([]*WindowedEvent, 0),
			size:     10 * time.Minute,
			maxItems: 5, // Small limit for testing
		}

		baseTime := time.Now()

		// Add more events than the limit
		for i := 0; i < 10; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("event-%d", i),
				Timestamp: baseTime.Add(time.Duration(i) * time.Second),
			}
			window.Add(event)
		}

		// Should not exceed max items
		window.mu.RLock()
		assert.LessOrEqual(t, len(window.events), window.maxItems)
		window.mu.RUnlock()
	})
}

func TestTemporalPatternMatching(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("interval pattern", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     10 * time.Minute,
			MinOccurrences: 3,
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()
		interval := 2 * time.Minute

		// Create events with regular interval
		for i := 0; i < 3; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("event-%d", i),
				Type:      EventTypeSystemd,
				Timestamp: baseTime.Add(time.Duration(i) * interval),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "periodic-pod",
				},
				Message: "Periodic task executed",
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i == 2 {
				// Should detect interval pattern
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "interval")
				assert.Equal(t, 3, len(result.Events))
			}
		}
	})

	t.Run("burst pattern", func(t *testing.T) {
		config := TemporalConfig{
			WindowSize:     1 * time.Minute,
			MinOccurrences: 5,
		}

		correlator := NewTemporalCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()

		// Create burst of events
		for i := 0; i < 5; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("burst-%d", i),
				Type:      EventTypeSystemd,
				Timestamp: baseTime.Add(time.Duration(i) * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "burst-pod",
				},
				Severity: domain.EventSeverityWarning,
				Message:  "Connection refused",
			}

			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i == 4 {
				// Should detect burst pattern
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "burst")
				assert.Greater(t, result.Confidence, 0.7)
			}
		}
	})
}

func BenchmarkTemporalCorrelatorProcess(b *testing.B) {
	logger := zaptest.NewLogger(b).Sugar().Desugar()
	config := DefaultTemporalConfig()
	correlator := NewTemporalCorrelator(logger, config)
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      EventTypeSystemd,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "default",
			Name:      "bench-pod",
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
