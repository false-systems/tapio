package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, requests blocked
	CircuitHalfOpen                     // Testing if service recovered
)

// ResilientCollector provides fault-tolerant metric collection with circuit breaker pattern
type ResilientCollector struct {
	mu sync.RWMutex

	// Circuit breaker state
	state            CircuitState
	failures         int
	successThreshold int
	failureThreshold int
	lastFailureTime  time.Time
	timeout          time.Duration
	halfOpenTimeout  time.Duration

	// Batch collection
	batchSize     int
	batchInterval time.Duration
	batchBuffer   []MetricUpdate
	batchMu       sync.Mutex

	// Metrics about the collector itself
	circuitStateGauge   *prometheus.GaugeVec
	collectionErrors    *prometheus.CounterVec
	batchesProcessed    *prometheus.CounterVec
	collectionDuration  *prometheus.HistogramVec
	droppedMetrics      *prometheus.CounterVec
	circuitBreakerTrips *prometheus.CounterVec
}

// MetricUpdate represents a single metric update
type MetricUpdate struct {
	Name      string
	Value     float64
	Labels    map[string]string
	Timestamp time.Time
}

// ResilientCollectorConfig configures the resilient collector
type ResilientCollectorConfig struct {
	FailureThreshold int           // Number of failures before opening circuit
	SuccessThreshold int           // Number of successes in half-open before closing
	Timeout          time.Duration // Time to wait before half-opening
	HalfOpenTimeout  time.Duration // Timeout for half-open requests
	BatchSize        int           // Maximum batch size
	BatchInterval    time.Duration // Maximum time between batches
}

// DefaultResilientCollectorConfig returns sensible defaults
func DefaultResilientCollectorConfig() ResilientCollectorConfig {
	return ResilientCollectorConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		HalfOpenTimeout:  5 * time.Second,
		BatchSize:        100,
		BatchInterval:    5 * time.Second,
	}
}

// NewResilientCollector creates a new resilient metric collector
func NewResilientCollector(config ResilientCollectorConfig, registry *prometheus.Registry) *ResilientCollector {
	rc := &ResilientCollector{
		state:            CircuitClosed,
		failures:         0,
		successThreshold: config.SuccessThreshold,
		failureThreshold: config.FailureThreshold,
		timeout:          config.Timeout,
		halfOpenTimeout:  config.HalfOpenTimeout,
		batchSize:        config.BatchSize,
		batchInterval:    config.BatchInterval,
		batchBuffer:      make([]MetricUpdate, 0, config.BatchSize),

		// Metrics
		circuitStateGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_resilient_collector_circuit_state",
				Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
			},
			[]string{"collector"},
		),
		collectionErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_resilient_collector_errors_total",
				Help: "Total number of collection errors",
			},
			[]string{"collector", "error_type"},
		),
		batchesProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_resilient_collector_batches_total",
				Help: "Total number of batches processed",
			},
			[]string{"collector", "status"},
		),
		collectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_resilient_collector_duration_seconds",
				Help:    "Time taken to collect metrics",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"collector", "operation"},
		),
		droppedMetrics: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_resilient_collector_dropped_metrics_total",
				Help: "Total number of metrics dropped due to circuit breaker",
			},
			[]string{"collector", "reason"},
		),
		circuitBreakerTrips: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_resilient_collector_circuit_trips_total",
				Help: "Total number of times circuit breaker has tripped",
			},
			[]string{"collector", "from_state", "to_state"},
		),
	}

	// Register metrics
	if registry != nil {
		registry.MustRegister(
			rc.circuitStateGauge,
			rc.collectionErrors,
			rc.batchesProcessed,
			rc.collectionDuration,
			rc.droppedMetrics,
			rc.circuitBreakerTrips,
		)
	}

	return rc
}

// Collect attempts to collect a metric with circuit breaker protection
func (rc *ResilientCollector) Collect(ctx context.Context, update MetricUpdate) error {
	rc.mu.RLock()
	state := rc.state
	rc.mu.RUnlock()

	// Check circuit state
	switch state {
	case CircuitOpen:
		if rc.shouldAttemptReset() {
			rc.transitionToHalfOpen()
		} else {
			rc.droppedMetrics.WithLabelValues("metrics", "circuit_open").Inc()
			return fmt.Errorf("circuit breaker is open")
		}
	case CircuitHalfOpen:
		// Allow limited requests through
		ctx, cancel := context.WithTimeout(ctx, rc.halfOpenTimeout)
		defer cancel()
		return rc.tryCollect(ctx, update)
	case CircuitClosed:
		// Normal operation
		return rc.tryCollect(ctx, update)
	}

	return nil
}

// tryCollect attempts to add metric to batch
func (rc *ResilientCollector) tryCollect(ctx context.Context, update MetricUpdate) error {
	rc.batchMu.Lock()
	defer rc.batchMu.Unlock()

	// Check if batch is full
	if len(rc.batchBuffer) >= rc.batchSize {
		// Process batch immediately
		if err := rc.processBatch(ctx); err != nil {
			rc.recordFailure()
			return err
		}
		rc.recordSuccess()
	}

	// Add to batch
	rc.batchBuffer = append(rc.batchBuffer, update)
	return nil
}

// ProcessBatch processes the current batch of metrics
func (rc *ResilientCollector) processBatch(ctx context.Context) error {
	if len(rc.batchBuffer) == 0 {
		return nil
	}

	start := time.Now()
	defer func() {
		rc.collectionDuration.WithLabelValues("metrics", "batch_process").Observe(time.Since(start).Seconds())
	}()

	// In a real implementation, this would send metrics to the actual collector
	// For now, we simulate processing
	select {
	case <-ctx.Done():
		rc.batchesProcessed.WithLabelValues("metrics", "timeout").Inc()
		return ctx.Err()
	case <-time.After(10 * time.Millisecond): // Simulate processing time
		// Success
		rc.batchesProcessed.WithLabelValues("metrics", "success").Inc()
		rc.batchBuffer = rc.batchBuffer[:0] // Clear buffer
		return nil
	}
}

// StartBatchProcessor starts the background batch processor
func (rc *ResilientCollector) StartBatchProcessor(ctx context.Context) {
	ticker := time.NewTicker(rc.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Process remaining batch
			rc.batchMu.Lock()
			if len(rc.batchBuffer) > 0 {
				_ = rc.processBatch(context.Background())
			}
			rc.batchMu.Unlock()
			return
		case <-ticker.C:
			rc.batchMu.Lock()
			if len(rc.batchBuffer) > 0 {
				if err := rc.processBatch(ctx); err != nil {
					rc.recordFailure()
					rc.collectionErrors.WithLabelValues("metrics", "batch_timeout").Inc()
				} else {
					rc.recordSuccess()
				}
			}
			rc.batchMu.Unlock()
		}
	}
}

// Circuit breaker state management

func (rc *ResilientCollector) shouldAttemptReset() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return time.Since(rc.lastFailureTime) >= rc.timeout
}

func (rc *ResilientCollector) transitionToHalfOpen() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.state != CircuitOpen {
		return
	}

	rc.state = CircuitHalfOpen
	rc.failures = 0
	rc.circuitStateGauge.WithLabelValues("metrics").Set(float64(rc.state))
	rc.circuitBreakerTrips.WithLabelValues("metrics", "open", "half_open").Inc()
}

func (rc *ResilientCollector) recordSuccess() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	switch rc.state {
	case CircuitHalfOpen:
		rc.failures = 0
		if rc.failures >= rc.successThreshold {
			// Transition to closed
			rc.state = CircuitClosed
			rc.circuitStateGauge.WithLabelValues("metrics").Set(float64(rc.state))
			rc.circuitBreakerTrips.WithLabelValues("metrics", "half_open", "closed").Inc()
		}
	case CircuitClosed:
		rc.failures = 0
	}
}

func (rc *ResilientCollector) recordFailure() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.failures++
	rc.lastFailureTime = time.Now()

	switch rc.state {
	case CircuitClosed:
		if rc.failures >= rc.failureThreshold {
			// Transition to open
			rc.state = CircuitOpen
			rc.circuitStateGauge.WithLabelValues("metrics").Set(float64(rc.state))
			rc.circuitBreakerTrips.WithLabelValues("metrics", "closed", "open").Inc()
		}
	case CircuitHalfOpen:
		// Any failure in half-open state reopens the circuit
		rc.state = CircuitOpen
		rc.circuitStateGauge.WithLabelValues("metrics").Set(float64(rc.state))
		rc.circuitBreakerTrips.WithLabelValues("metrics", "half_open", "open").Inc()
	}
}

// GetState returns the current circuit state
func (rc *ResilientCollector) GetState() CircuitState {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.state
}

// GetStats returns current collector statistics
func (rc *ResilientCollector) GetStats() CollectorStats {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	rc.batchMu.Lock()
	batchSize := len(rc.batchBuffer)
	rc.batchMu.Unlock()

	return CollectorStats{
		State:           rc.state,
		Failures:        rc.failures,
		LastFailureTime: rc.lastFailureTime,
		BatchSize:       batchSize,
	}
}

// CollectorStats represents collector statistics
type CollectorStats struct {
	State           CircuitState
	Failures        int
	LastFailureTime time.Time
	BatchSize       int
}

// Reset resets the circuit breaker state
func (rc *ResilientCollector) Reset() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.state = CircuitClosed
	rc.failures = 0
	rc.lastFailureTime = time.Time{}
	rc.circuitStateGauge.WithLabelValues("metrics").Set(float64(rc.state))
}
