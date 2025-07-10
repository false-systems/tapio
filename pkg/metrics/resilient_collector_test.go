package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResilientCollector(t *testing.T) {
	registry := prometheus.NewRegistry()
	config := DefaultResilientCollectorConfig()

	collector := NewResilientCollector(config, registry)

	assert.NotNil(t, collector)
	assert.Equal(t, CircuitClosed, collector.state)
	assert.Equal(t, 0, collector.failures)
	assert.Equal(t, config, collector.config)
}

func TestCircuitBreakerStates(t *testing.T) {
	tests := []struct {
		name          string
		initialState  CircuitState
		failures      int
		expectedState CircuitState
		expectedAllow bool
	}{
		{
			name:          "closed_allows_calls",
			initialState:  CircuitClosed,
			failures:      0,
			expectedState: CircuitClosed,
			expectedAllow: true,
		},
		{
			name:          "closed_with_some_failures",
			initialState:  CircuitClosed,
			failures:      2,
			expectedState: CircuitClosed,
			expectedAllow: true,
		},
		{
			name:          "open_blocks_calls",
			initialState:  CircuitOpen,
			failures:      0,
			expectedState: CircuitOpen,
			expectedAllow: false,
		},
		{
			name:          "half_open_allows_one_call",
			initialState:  CircuitHalfOpen,
			failures:      0,
			expectedState: CircuitHalfOpen,
			expectedAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := &ResilientCollector{
				state:        tt.initialState,
				failures:     tt.failures,
				config:       DefaultResilientCollectorConfig(),
				lastFailTime: time.Now(),
			}

			allowed := collector.allowRequest()
			assert.Equal(t, tt.expectedAllow, allowed)
			assert.Equal(t, tt.expectedState, collector.state)
		})
	}
}

func TestCollectSingle(t *testing.T) {
	registry := prometheus.NewRegistry()
	config := DefaultResilientCollectorConfig()
	collector := NewResilientCollector(config, registry)

	t.Run("successful_collection", func(t *testing.T) {
		result := collector.Collect(func() error {
			return nil
		})

		assert.NoError(t, result.Error)
		assert.WithinDuration(t, time.Now(), result.Timestamp, time.Second)
		assert.True(t, result.Duration > 0)
		assert.Equal(t, CircuitClosed, collector.state)
	})

	t.Run("failed_collection", func(t *testing.T) {
		expectedErr := errors.New("collection failed")
		result := collector.Collect(func() error {
			return expectedErr
		})

		assert.Equal(t, expectedErr, result.Error)
		assert.Equal(t, 1, collector.failures)
	})

	t.Run("circuit_opens_after_threshold", func(t *testing.T) {
		// Force failures to open circuit
		for i := 0; i < config.FailureThreshold; i++ {
			collector.Collect(func() error {
				return errors.New("failure")
			})
		}

		assert.Equal(t, CircuitOpen, collector.state)

		// Next call should be blocked
		result := collector.Collect(func() error {
			return nil
		})

		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "circuit breaker is open")
	})
}

func TestCollectBatch(t *testing.T) {
	registry := prometheus.NewRegistry()
	config := DefaultResilientCollectorConfig()
	collector := NewResilientCollector(config, registry)

	t.Run("all_successful", func(t *testing.T) {
		funcs := []func() error{
			func() error { return nil },
			func() error { return nil },
			func() error { return nil },
		}

		results := collector.CollectBatch(funcs)

		require.Len(t, results, 3)
		for _, result := range results {
			assert.NoError(t, result.Error)
		}
		assert.Equal(t, 3, len(collector.batchQueue))
	})

	t.Run("mixed_results", func(t *testing.T) {
		expectedErr := errors.New("second fails")
		funcs := []func() error{
			func() error { return nil },
			func() error { return expectedErr },
			func() error { return nil },
		}

		results := collector.CollectBatch(funcs)

		require.Len(t, results, 3)
		assert.NoError(t, results[0].Error)
		assert.Equal(t, expectedErr, results[1].Error)
		assert.NoError(t, results[2].Error)
	})

	t.Run("circuit_open_blocks_batch", func(t *testing.T) {
		// Open the circuit
		collector.state = CircuitOpen
		collector.lastFailTime = time.Now()

		funcs := []func() error{
			func() error { return nil },
		}

		results := collector.CollectBatch(funcs)

		require.Len(t, results, 1)
		assert.Error(t, results[0].Error)
		assert.Contains(t, results[0].Error.Error(), "circuit breaker is open")
	})
}

func TestRecordSuccess(t *testing.T) {
	collector := &ResilientCollector{
		state:    CircuitClosed,
		failures: 3,
		config:   DefaultResilientCollectorConfig(),
		metrics: &collectorMetrics{
			successTotal: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	collector.recordSuccess()

	assert.Equal(t, 0, collector.failures)
	assert.Equal(t, CircuitClosed, collector.state)
}

func TestRecordFailure(t *testing.T) {
	config := DefaultResilientCollectorConfig()
	config.FailureThreshold = 3

	collector := &ResilientCollector{
		state:    CircuitClosed,
		failures: 2,
		config:   config,
		metrics: &collectorMetrics{
			failureTotal:  prometheus.NewCounter(prometheus.CounterOpts{}),
			circuitOpened: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	collector.recordFailure()

	assert.Equal(t, CircuitOpen, collector.state)
	assert.Equal(t, 3, collector.failures)
	assert.WithinDuration(t, time.Now(), collector.lastFailTime, time.Second)
}

func TestResetCircuit(t *testing.T) {
	collector := &ResilientCollector{
		state:    CircuitOpen,
		failures: 5,
		config:   DefaultResilientCollectorConfig(),
		metrics: &collectorMetrics{
			circuitClosed: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	collector.resetCircuit()

	assert.Equal(t, CircuitClosed, collector.state)
	assert.Equal(t, 0, collector.failures)
}

func TestHalfOpenTransitions(t *testing.T) {
	config := DefaultResilientCollectorConfig()
	config.SuccessThreshold = 2
	collector := &ResilientCollector{
		state:     CircuitHalfOpen,
		failures:  0,
		successes: 0,
		config:    config,
		metrics: &collectorMetrics{
			successTotal:  prometheus.NewCounter(prometheus.CounterOpts{}),
			failureTotal:  prometheus.NewCounter(prometheus.CounterOpts{}),
			circuitOpened: prometheus.NewCounter(prometheus.CounterOpts{}),
			circuitClosed: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	t.Run("success_moves_to_closed", func(t *testing.T) {
		collector.successes = 1
		collector.recordSuccess()

		assert.Equal(t, CircuitClosed, collector.state)
		assert.Equal(t, 0, collector.successes)
	})

	t.Run("failure_moves_to_open", func(t *testing.T) {
		collector.state = CircuitHalfOpen
		collector.recordFailure()

		assert.Equal(t, CircuitOpen, collector.state)
	})
}

func TestGetStats(t *testing.T) {
	collector := &ResilientCollector{
		state:    CircuitClosed,
		failures: 2,
		metrics: &collectorMetrics{
			successTotal: prometheus.NewCounter(prometheus.CounterOpts{}),
			failureTotal: prometheus.NewCounter(prometheus.CounterOpts{}),
		},
	}

	// Simulate some operations
	collector.metrics.successTotal.Add(10)
	collector.metrics.failureTotal.Add(2)

	stats := collector.GetStats()

	assert.Equal(t, "closed", stats.State)
	assert.Equal(t, 2, stats.Failures)
	// Note: Prometheus counter values can't be read directly in tests
	// In real usage, you'd use a test registry and gather metrics
}
