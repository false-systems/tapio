package behavior

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CircuitState represents the state of the circuit breaker
type CircuitState int32

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitStateClosed:
		return "closed"
	case CircuitStateOpen:
		return "open"
	case CircuitStateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	MaxFailures  int
	ResetTimeout time.Duration
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration

	state         atomic.Int32
	failures      atomic.Int32
	lastFailTime  atomic.Value // stores time.Time
	halfOpenCalls atomic.Int32

	mu sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxFailures:  config.MaxFailures,
		resetTimeout: config.ResetTimeout,
	}
	cb.state.Store(int32(CircuitStateClosed))
	cb.lastFailTime.Store(time.Time{})
	return cb
}

// Execute runs a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() (*domain.PredictionResult, error)) (*domain.PredictionResult, error) {
	state := CircuitState(cb.state.Load())

	switch state {
	case CircuitStateOpen:
		// Check if we should transition to half-open
		lastFail := cb.lastFailTime.Load().(time.Time)
		if time.Since(lastFail) > cb.resetTimeout {
			cb.setState(CircuitStateHalfOpen)
			cb.halfOpenCalls.Store(0)
		} else {
			return nil, fmt.Errorf("circuit breaker is open")
		}

	case CircuitStateHalfOpen:
		// Limit concurrent calls in half-open state
		calls := cb.halfOpenCalls.Add(1)
		if calls > 3 {
			cb.halfOpenCalls.Add(-1)
			return nil, fmt.Errorf("circuit breaker half-open limit reached")
		}
		defer cb.halfOpenCalls.Add(-1)
	}

	// Execute the function
	result, err := fn()

	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	cb.recordSuccess()
	return result, nil
}

// recordFailure records a failure and potentially opens the circuit
func (cb *CircuitBreaker) recordFailure() {
	failures := cb.failures.Add(1)
	cb.lastFailTime.Store(time.Now())

	if failures >= int32(cb.maxFailures) {
		cb.setState(CircuitStateOpen)
	}
}

// recordSuccess records a success and potentially closes the circuit
func (cb *CircuitBreaker) recordSuccess() {
	state := CircuitState(cb.state.Load())

	if state == CircuitStateHalfOpen {
		// Success in half-open state, close the circuit
		cb.setState(CircuitStateClosed)
		cb.failures.Store(0)
	}
}

// setState atomically sets the circuit breaker state
func (cb *CircuitBreaker) setState(state CircuitState) {
	cb.state.Store(int32(state))
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() string {
	return CircuitState(cb.state.Load()).String()
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.setState(CircuitStateClosed)
	cb.failures.Store(0)
	cb.halfOpenCalls.Store(0)
}
