package internal

import (
	"errors"
	"sync"
	"time"
)

var (
	// ErrCircuitBreakerOpen is returned when circuit breaker is open
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
)

// CircuitBreakerState represents the circuit breaker state
type CircuitBreakerState string

const (
	StateClosed   CircuitBreakerState = "closed"
	StateOpen     CircuitBreakerState = "open"
	StateHalfOpen CircuitBreakerState = "half_open"
)

// CircuitBreaker implements circuit breaker pattern for D-Bus failures
type CircuitBreaker struct {
	mu                  sync.Mutex
	state               CircuitBreakerState
	failureCount        int
	consecutiveFailures int
	successCount        int
	failureThreshold    int
	successThreshold    int
	timeout             time.Duration
	lastFailureTime     time.Time
	halfOpenAttempts    int
	maxHalfOpenAttempts int
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:               StateClosed,
		failureThreshold:    5,
		successThreshold:    3,
		timeout:             30 * time.Second,
		maxHalfOpenAttempts: 3,
	}
}

// Call executes the function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	state := cb.state
	cb.mu.Unlock()

	switch state {
	case StateOpen:
		return cb.handleOpenState()
	case StateHalfOpen:
		return cb.handleHalfOpenState(fn)
	default:
		return cb.handleClosedState(fn)
	}
}

func (cb *CircuitBreaker) handleOpenState() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if time.Since(cb.lastFailureTime) > cb.timeout {
		cb.state = StateHalfOpen
		cb.halfOpenAttempts = 0
		return nil
	}

	return ErrCircuitBreakerOpen
}

func (cb *CircuitBreaker) handleHalfOpenState(fn func() error) error {
	cb.mu.Lock()
	cb.halfOpenAttempts++
	attempts := cb.halfOpenAttempts
	cb.mu.Unlock()

	if attempts > cb.maxHalfOpenAttempts {
		cb.mu.Lock()
		cb.state = StateOpen
		cb.lastFailureTime = time.Now()
		cb.mu.Unlock()
		return ErrCircuitBreakerOpen
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.state = StateOpen
		cb.lastFailureTime = time.Now()
		cb.consecutiveFailures = 0
		return err
	}

	cb.successCount++
	if cb.successCount >= cb.successThreshold {
		cb.state = StateClosed
		cb.successCount = 0
		cb.consecutiveFailures = 0
	}

	return nil
}

func (cb *CircuitBreaker) handleClosedState(fn func() error) error {
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.consecutiveFailures++
		if cb.consecutiveFailures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.lastFailureTime = time.Now()
		}
		return err
	}

	cb.consecutiveFailures = 0
	return nil
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Metrics returns circuit breaker metrics
func (cb *CircuitBreaker) Metrics() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return map[string]interface{}{
		"state":                string(cb.state),
		"failure_count":        cb.failureCount,
		"consecutive_failures": cb.consecutiveFailures,
		"last_failure_time":    cb.lastFailureTime,
	}
}
