package internal

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitBreakerState represents the circuit breaker state
type CircuitBreakerState int32

const (
	StateClosed   CircuitBreakerState = iota // Normal operation
	StateOpen                                // Circuit broken, rejecting requests
	StateHalfOpen                            // Testing if service recovered
)

// CircuitBreaker implements circuit breaker pattern for CNI monitors
type CircuitBreaker struct {
	// Configuration
	name             string
	failureThreshold int
	successThreshold int
	timeout          time.Duration

	// State
	state        atomic.Int32
	failures     atomic.Int32
	successes    atomic.Int32
	lastFailTime atomic.Value // time.Time

	// Metrics
	mu      sync.RWMutex
	metrics CircuitBreakerMetrics
}

// CircuitBreakerMetrics tracks circuit breaker statistics
type CircuitBreakerMetrics struct {
	TotalRequests   uint64
	SuccessCount    uint64
	FailureCount    uint64
	RejectedCount   uint64
	StateChanges    uint64
	LastStateChange time.Time
	CurrentState    string
}

// ErrCircuitBreakerOpen indicates the circuit breaker is open
var ErrCircuitBreakerOpen = fmt.Errorf("circuit breaker is open")

// NewCircuitBreaker creates a new circuit breaker for a monitor
func NewCircuitBreaker(name string, failureThreshold int, timeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:             name,
		failureThreshold: failureThreshold,
		successThreshold: failureThreshold / 2, // Half of failure threshold
		timeout:          timeout,
	}
	cb.lastFailTime.Store(time.Time{})
	return cb
}

// Call executes the function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.canExecute() {
		cb.recordRejection()
		return fmt.Errorf("%s: %w", cb.name, ErrCircuitBreakerOpen)
	}

	err := fn()
	cb.recordResult(err)
	return err
}

// canExecute checks if request can proceed
func (cb *CircuitBreaker) canExecute() bool {
	state := CircuitBreakerState(cb.state.Load())

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if timeout has passed
		lastFail := cb.lastFailTime.Load().(time.Time)
		if time.Since(lastFail) > cb.timeout {
			cb.transitionTo(StateHalfOpen)
			return true
		}
		return false

	case StateHalfOpen:
		return true

	default:
		return false
	}
}

// recordResult records the result of a call
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	cb.metrics.TotalRequests++
	cb.mu.Unlock()

	state := CircuitBreakerState(cb.state.Load())

	if err != nil {
		cb.recordFailure()

		switch state {
		case StateClosed:
			if cb.failures.Load() >= int32(cb.failureThreshold) {
				cb.transitionTo(StateOpen)
			}

		case StateHalfOpen:
			cb.transitionTo(StateOpen)
		}
	} else {
		cb.recordSuccess()

		switch state {
		case StateHalfOpen:
			if cb.successes.Load() >= int32(cb.successThreshold) {
				cb.transitionTo(StateClosed)
			}
		}
	}
}

// recordFailure records a failure
func (cb *CircuitBreaker) recordFailure() {
	cb.failures.Add(1)
	cb.lastFailTime.Store(time.Now())

	cb.mu.Lock()
	cb.metrics.FailureCount++
	cb.mu.Unlock()
}

// recordSuccess records a success
func (cb *CircuitBreaker) recordSuccess() {
	cb.successes.Add(1)

	cb.mu.Lock()
	cb.metrics.SuccessCount++
	cb.mu.Unlock()
}

// recordRejection records a rejected request
func (cb *CircuitBreaker) recordRejection() {
	cb.mu.Lock()
	cb.metrics.TotalRequests++
	cb.metrics.RejectedCount++
	cb.mu.Unlock()
}

// transitionTo transitions to a new state
func (cb *CircuitBreaker) transitionTo(newState CircuitBreakerState) {
	oldState := CircuitBreakerState(cb.state.Swap(int32(newState)))

	if oldState != newState {
		// Reset counters on state change
		switch newState {
		case StateClosed:
			cb.failures.Store(0)

		case StateHalfOpen:
			cb.successes.Store(0)
			cb.failures.Store(0)
		}

		cb.mu.Lock()
		cb.metrics.StateChanges++
		cb.metrics.LastStateChange = time.Now()
		cb.metrics.CurrentState = cb.getStateName(newState)
		cb.mu.Unlock()
	}
}

// getStateName returns human-readable state name
func (cb *CircuitBreaker) getStateName(state CircuitBreakerState) string {
	switch state {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// GetState returns current state
func (cb *CircuitBreaker) GetState() string {
	state := CircuitBreakerState(cb.state.Load())
	return cb.getStateName(state)
}

// IsOpen returns true if circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return CircuitBreakerState(cb.state.Load()) == StateOpen
}

// GetMetrics returns circuit breaker metrics
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	metrics := cb.metrics
	metrics.CurrentState = cb.GetState()
	return metrics
}

// GetMetricsMap returns metrics as map for monitoring
func (cb *CircuitBreaker) GetMetricsMap() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"name":              cb.name,
		"total_requests":    cb.metrics.TotalRequests,
		"success_count":     cb.metrics.SuccessCount,
		"failure_count":     cb.metrics.FailureCount,
		"rejected_count":    cb.metrics.RejectedCount,
		"state_changes":     cb.metrics.StateChanges,
		"current_state":     cb.GetState(),
		"last_state_change": cb.metrics.LastStateChange,
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.transitionTo(StateClosed)
	cb.failures.Store(0)
	cb.successes.Store(0)
	cb.lastFailTime.Store(time.Time{})
}
