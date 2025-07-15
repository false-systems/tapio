package installer

import (
	"fmt"
	"sync"
	"time"
)

// circuitBreaker implements the CircuitBreaker interface
type circuitBreaker struct {
	mu              sync.Mutex
	maxFailures     int
	resetTimeout    time.Duration
	failures        int
	lastFailureTime time.Time
	state           circuitState
	successCount    int
	halfOpenLimit   int
}

// circuitState represents the circuit breaker state
type circuitState int

const (
	stateClosed circuitState = iota
	stateOpen
	stateHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) CircuitBreaker {
	return &circuitBreaker{
		maxFailures:   maxFailures,
		resetTimeout:  resetTimeout,
		state:         stateClosed,
		halfOpenLimit: 1,
	}
}

// Execute runs the function with circuit breaker protection
func (cb *circuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()

	// Check if circuit should transition from open to half-open
	if cb.state == stateOpen {
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = stateHalfOpen
			cb.successCount = 0
		}
	}

	// Check current state
	switch cb.state {
	case stateOpen:
		cb.mu.Unlock()
		return fmt.Errorf("circuit breaker is open")

	case stateHalfOpen:
		// Allow limited requests in half-open state
		if cb.successCount >= cb.halfOpenLimit {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker is half-open, limit reached")
		}
	}

	cb.mu.Unlock()

	// Execute the function
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}

	return err
}

// IsOpen returns if the circuit is open
func (cb *circuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return cb.state == stateOpen
}

// Reset resets the circuit breaker
func (cb *circuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.successCount = 0
	cb.state = stateClosed
	cb.lastFailureTime = time.Time{}
}

// onSuccess handles successful execution
func (cb *circuitBreaker) onSuccess() {
	switch cb.state {
	case stateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenLimit {
			// Transition to closed
			cb.state = stateClosed
			cb.failures = 0
		}
	case stateClosed:
		cb.failures = 0
	}
}

// onFailure handles failed execution
func (cb *circuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case stateHalfOpen:
		// Immediately open on failure in half-open state
		cb.state = stateOpen
	case stateClosed:
		if cb.failures >= cb.maxFailures {
			cb.state = stateOpen
		}
	}
}

// AdaptiveCircuitBreaker adjusts its parameters based on performance
type AdaptiveCircuitBreaker struct {
	*circuitBreaker
	metrics       *circuitMetrics
	adaptInterval time.Duration
	lastAdaptTime time.Time
}

// circuitMetrics tracks circuit breaker performance
type circuitMetrics struct {
	mu                sync.RWMutex
	totalRequests     int64
	failedRequests    int64
	successRequests   int64
	avgResponseTime   time.Duration
	lastResponseTimes []time.Duration
	maxHistory        int
}

// NewAdaptiveCircuitBreaker creates a circuit breaker that adapts to conditions
func NewAdaptiveCircuitBreaker(initialMaxFailures int, initialTimeout time.Duration) CircuitBreaker {
	return &AdaptiveCircuitBreaker{
		circuitBreaker: &circuitBreaker{
			maxFailures:   initialMaxFailures,
			resetTimeout:  initialTimeout,
			state:         stateClosed,
			halfOpenLimit: 1,
		},
		metrics: &circuitMetrics{
			lastResponseTimes: make([]time.Duration, 0, 100),
			maxHistory:        100,
		},
		adaptInterval: 1 * time.Minute,
		lastAdaptTime: time.Now(),
	}
}

// Execute runs the function with adaptive circuit breaker protection
func (acb *AdaptiveCircuitBreaker) Execute(fn func() error) error {
	start := time.Now()

	// Execute with base circuit breaker
	err := acb.circuitBreaker.Execute(fn)

	// Record metrics
	duration := time.Since(start)
	acb.recordMetric(duration, err)

	// Adapt parameters if needed
	if time.Since(acb.lastAdaptTime) > acb.adaptInterval {
		acb.adapt()
	}

	return err
}

// recordMetric records execution metrics
func (acb *AdaptiveCircuitBreaker) recordMetric(duration time.Duration, err error) {
	acb.metrics.mu.Lock()
	defer acb.metrics.mu.Unlock()

	acb.metrics.totalRequests++
	if err != nil {
		acb.metrics.failedRequests++
	} else {
		acb.metrics.successRequests++
	}

	// Update response times
	acb.metrics.lastResponseTimes = append(acb.metrics.lastResponseTimes, duration)
	if len(acb.metrics.lastResponseTimes) > acb.metrics.maxHistory {
		acb.metrics.lastResponseTimes = acb.metrics.lastResponseTimes[1:]
	}

	// Calculate average response time
	var total time.Duration
	for _, d := range acb.metrics.lastResponseTimes {
		total += d
	}
	if len(acb.metrics.lastResponseTimes) > 0 {
		acb.metrics.avgResponseTime = total / time.Duration(len(acb.metrics.lastResponseTimes))
	}
}

// adapt adjusts circuit breaker parameters based on metrics
func (acb *AdaptiveCircuitBreaker) adapt() {
	acb.metrics.mu.RLock()
	failureRate := float64(acb.metrics.failedRequests) / float64(acb.metrics.totalRequests)
	avgResponse := acb.metrics.avgResponseTime
	acb.metrics.mu.RUnlock()

	acb.mu.Lock()
	defer acb.mu.Unlock()

	// Adjust max failures based on failure rate
	if failureRate > 0.5 {
		// High failure rate - be more aggressive
		acb.maxFailures = max(1, acb.maxFailures-1)
	} else if failureRate < 0.1 {
		// Low failure rate - be more lenient
		acb.maxFailures = min(10, acb.maxFailures+1)
	}

	// Adjust reset timeout based on response times
	if avgResponse > 10*time.Second {
		// Slow responses - increase timeout
		acb.resetTimeout = minDuration(5*time.Minute, acb.resetTimeout+30*time.Second)
	} else if avgResponse < 1*time.Second {
		// Fast responses - decrease timeout
		acb.resetTimeout = maxDuration(30*time.Second, acb.resetTimeout-30*time.Second)
	}

	acb.lastAdaptTime = time.Now()
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maxDuration returns the maximum of two durations
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// minDuration returns the minimum of two durations
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
