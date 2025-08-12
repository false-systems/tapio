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

func TestSequenceCorrelatorCreation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("with default config", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)

		assert.NotNil(t, correlator)
		assert.Equal(t, "sequence", correlator.Name())
		assert.Equal(t, *config, correlator.config)
		assert.NotNil(t, correlator.sequences)
		assert.NotNil(t, correlator.patterns)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := SequenceConfig{
			MaxSequenceAge:     15 * time.Minute,
			MaxSequenceGap:     3 * time.Minute,
			MinSequenceLength:  3,
			MaxActiveSequences: 2000,
		}

		correlator := NewSequenceCorrelator(logger, config)

		assert.NotNil(t, correlator)
		assert.Equal(t, config, correlator.config)
	})
}

func TestSequenceCorrelatorProcess(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("detect deployment sequence", func(t *testing.T) {
		config := SequenceConfig{
			MaxSequenceAge:    5 * time.Minute,
			MaxSequenceGap:    1 * time.Minute,
			MinSequenceLength: 3,
		}

		correlator := NewSequenceCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()

		// Typical deployment sequence
		events := []*domain.UnifiedEvent{
			{
				ID:        "event-1",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime,
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-deployment",
					Kind:      "Deployment",
				},
				Message: "Deployment updated",
			},
			{
				ID:        "event-2",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(10 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-pod-xyz",
					Kind:      "Pod",
				},
				Message: "Pod created",
			},
			{
				ID:        "event-3",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(20 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-pod-xyz",
					Kind:      "Pod",
				},
				Message: "Pod ready",
			},
		}

		var lastResult []*CorrelationResult

		// Process events in sequence
		for i, event := range events {
			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i < 2 {
				// Building sequence
				assert.Len(t, results, 0)
			} else {
				// Sequence complete
				require.Len(t, results, 1)
				result := results[0]
				assert.Equal(t, "sequence_match", result.Type)
				assert.Len(t, result.Events, 3)
				assert.Contains(t, result.Message, "deployment")
				lastResult = results
			}
		}

		// Verify the final result
		if lastResult != nil && len(lastResult) > 0 {
			result := lastResult[0]
			assert.Contains(t, result.Events, "event-1")
			assert.Contains(t, result.Events, "event-2")
			assert.Contains(t, result.Events, "event-3")
		}
	})

	t.Run("detect error cascade sequence", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)
		ctx := context.Background()

		baseTime := time.Now()

		// Error cascade sequence
		events := []*domain.UnifiedEvent{
			{
				ID:        "error-1",
				Type:      domain.EventTypeSystem,
				Timestamp: baseTime,
				Severity:  domain.EventSeverityError,
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "database-pod",
				},
				Message: "Connection pool exhausted",
			},
			{
				ID:        "error-2",
				Type:      domain.EventTypeSystem,
				Timestamp: baseTime.Add(5 * time.Second),
				Severity:  domain.EventSeverityError,
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "api-pod",
				},
				Message: "Database connection timeout",
			},
			{
				ID:        "error-3",
				Type:      domain.EventTypeSystem,
				Timestamp: baseTime.Add(10 * time.Second),
				Severity:  domain.EventSeverityError,
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "frontend-pod",
				},
				Message: "API unavailable",
			},
		}

		// Process cascade
		for i, event := range events {
			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i == 2 {
				// Should detect error cascade
				require.Len(t, results, 1)
				result := results[0]
				assert.Equal(t, "sequence_match", result.Type)
				assert.Contains(t, result.Message, "cascade")
				assert.Equal(t, domain.EventSeverityError, result.Impact.Severity)
			}
		}
	})

	t.Run("sequence gap timeout", func(t *testing.T) {
		config := SequenceConfig{
			MaxSequenceGap:    30 * time.Second,
			MinSequenceLength: 2,
		}

		correlator := NewSequenceCorrelator(logger, config)
		ctx := context.Background()

		baseTime := time.Now()

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		}

		// Event with gap larger than MaxSequenceGap
		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime.Add(1 * time.Minute), // Gap too large
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
		assert.Len(t, results2, 0) // Sequence broken due to gap
	})

	t.Run("nil event handling", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)
		ctx := context.Background()

		results, err := correlator.Process(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, results)
	})

	t.Run("context cancellation", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now(),
		}

		results, err := correlator.Process(ctx, event)
		if err != nil {
			assert.Equal(t, context.Canceled, err)
		}
		assert.Nil(t, results)
	})
}

func TestSequencePatternMatching(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("restart sequence pattern", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)
		ctx := context.Background()

		// Add restart pattern
		correlator.patterns = append(correlator.patterns, &SequencePattern{
			Name: "pod_restart",
			Steps: []PatternStep{
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"terminating"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"creating"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"running"}},
			},
		})

		baseTime := time.Now()

		events := []*domain.UnifiedEvent{
			{
				ID:        "restart-1",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime,
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "app-pod",
				},
				Message: "Pod terminating",
			},
			{
				ID:        "restart-2",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(5 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "app-pod",
				},
				Message: "Pod creating",
			},
			{
				ID:        "restart-3",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(10 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "app-pod",
				},
				Message: "Pod running",
			},
		}

		for i, event := range events {
			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i == 2 {
				// Should match restart pattern
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "restart")
				assert.Equal(t, 3, len(result.Events))
			}
		}
	})

	t.Run("scaling sequence pattern", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)
		ctx := context.Background()

		baseTime := time.Now()

		// Scaling sequence
		events := []*domain.UnifiedEvent{
			{
				ID:        "scale-1",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime,
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-hpa",
					Kind:      "HorizontalPodAutoscaler",
				},
				Message: "HPA triggered scale up",
			},
			{
				ID:        "scale-2",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(2 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-deployment",
					Kind:      "Deployment",
				},
				Message: "Deployment scaled to 5 replicas",
			},
			{
				ID:        "scale-3",
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(5 * time.Second),
				K8sContext: &domain.K8sContext{
					Namespace: "production",
					Name:      "api-pod-new",
					Kind:      "Pod",
				},
				Message: "New pod created",
			},
		}

		for i, event := range events {
			results, err := correlator.Process(ctx, event)
			require.NoError(t, err)

			if i == 2 {
				// Should detect scaling sequence
				require.Len(t, results, 1)
				result := results[0]
				assert.Contains(t, result.Message, "scaling")
				assert.Greater(t, result.Confidence, 0.5)
			}
		}
	})
}

func TestSequenceCorrelatorConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("concurrent sequence processing", func(t *testing.T) {
		config := DefaultSequenceConfig()
		correlator := NewSequenceCorrelator(logger, *config)
		ctx := context.Background()

		// Create multiple concurrent sequences
		sequenceCount := 10
		eventsPerSequence := 3

		errChan := make(chan error, sequenceCount*eventsPerSequence)

		for seq := 0; seq < sequenceCount; seq++ {
			go func(seqID int) {
				baseTime := time.Now()
				namespace := fmt.Sprintf("namespace-%d", seqID)

				for i := 0; i < eventsPerSequence; i++ {
					event := &domain.UnifiedEvent{
						ID:        fmt.Sprintf("seq-%d-event-%d", seqID, i),
						Type:      domain.EventTypeKubernetes,
						Timestamp: baseTime.Add(time.Duration(i) * time.Second),
						K8sContext: &domain.K8sContext{
							Namespace: namespace,
							Name:      fmt.Sprintf("pod-%d", i),
						},
						Message: fmt.Sprintf("Step %d", i),
					}

					_, err := correlator.Process(ctx, event)
					errChan <- err
				}
			}(seq)
		}

		// Wait for all events
		for i := 0; i < sequenceCount*eventsPerSequence; i++ {
			err := <-errChan
			assert.NoError(t, err)
		}
	})
}

func TestSequenceCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("cleanup old sequences", func(t *testing.T) {
		config := SequenceConfig{
			MaxSequenceAge:     100 * time.Millisecond,
			MinSequenceLength:  2,
			MaxActiveSequences: 10,
		}

		correlator := NewSequenceCorrelator(logger, config)
		ctx := context.Background()

		// Create an old event
		oldEvent := &domain.UnifiedEvent{
			ID:        "old-event",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now().Add(-200 * time.Millisecond),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "old-pod",
			},
		}

		_, err := correlator.Process(ctx, oldEvent)
		require.NoError(t, err)

		// Wait for cleanup
		time.Sleep(150 * time.Millisecond)

		// Process new event to trigger cleanup
		newEvent := &domain.UnifiedEvent{
			ID:        "new-event",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "new-pod",
			},
		}

		_, err = correlator.Process(ctx, newEvent)
		require.NoError(t, err)

		// Old sequence should be cleaned up
		correlator.sequencesMu.RLock()
		sequenceCount := len(correlator.sequences)
		correlator.sequencesMu.RUnlock()

		assert.LessOrEqual(t, sequenceCount, 1)
	})

	t.Run("max active sequences limit", func(t *testing.T) {
		config := SequenceConfig{
			MaxActiveSequences: 5,
			MinSequenceLength:  2,
		}

		correlator := NewSequenceCorrelator(logger, config)
		ctx := context.Background()

		// Create more sequences than the limit
		for i := 0; i < 10; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("event-%d", i),
				Type:      domain.EventTypeKubernetes,
				Timestamp: time.Now(),
				K8sContext: &domain.K8sContext{
					Namespace: fmt.Sprintf("namespace-%d", i),
					Name:      fmt.Sprintf("pod-%d", i),
				},
			}

			_, err := correlator.Process(ctx, event)
			require.NoError(t, err)
		}

		// Check that sequences are limited
		correlator.sequencesMu.RLock()
		sequenceCount := len(correlator.sequences)
		correlator.sequencesMu.RUnlock()

		assert.LessOrEqual(t, sequenceCount, config.MaxActiveSequences)
	})
}

func TestEventSequence(t *testing.T) {
	t.Run("add and match events", func(t *testing.T) {
		sequence := &EventSequence{
			ID:        "test-seq",
			Events:    []*domain.UnifiedEvent{},
			StartTime: time.Now(),
		}

		event1 := &domain.UnifiedEvent{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now(),
		}

		event2 := &domain.UnifiedEvent{
			ID:        "event-2",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now().Add(1 * time.Second),
		}

		sequence.Events = append(sequence.Events, event1, event2)

		assert.Len(t, sequence.Events, 2)
		assert.Equal(t, "event-1", sequence.Events[0].ID)
		assert.Equal(t, "event-2", sequence.Events[1].ID)
	})

	t.Run("sequence duration", func(t *testing.T) {
		startTime := time.Now()
		sequence := &EventSequence{
			ID:         "test-seq",
			StartTime:  startTime,
			LastUpdate: startTime.Add(5 * time.Second),
		}

		duration := sequence.LastUpdate.Sub(sequence.StartTime)
		assert.Equal(t, 5*time.Second, duration)
	})
}

func BenchmarkSequenceCorrelatorProcess(b *testing.B) {
	logger := zaptest.NewLogger(b).Sugar().Desugar()
	config := DefaultSequenceConfig()
	correlator := NewSequenceCorrelator(logger, *config)
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "benchmark",
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
