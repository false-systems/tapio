package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the circuit breaker state
type State int32

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
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

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name             string
	maxFailures      uint32
	resetTimeout     time.Duration
	halfOpenMaxCalls uint32

	mu              sync.RWMutex
	state           State
	failures        uint32
	lastFailureTime time.Time
	successCount    uint32

	// Metrics
	totalCalls     atomic.Uint64
	totalFailures  atomic.Uint64
	totalSuccesses atomic.Uint64
	openTime       atomic.Int64

	// Callbacks
	onStateChange func(oldState, newState State)
}

// CircuitBreakerConfig holds configuration for circuit breaker
type CircuitBreakerConfig struct {
	Name             string
	MaxFailures      uint32
	ResetTimeout     time.Duration
	HalfOpenMaxCalls uint32
	OnStateChange    func(oldState, newState State)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}
	if config.ResetTimeout == 0 {
		config.ResetTimeout = 60 * time.Second
	}
	if config.HalfOpenMaxCalls == 0 {
		config.HalfOpenMaxCalls = 1
	}

	return &CircuitBreaker{
		name:             config.Name,
		maxFailures:      config.MaxFailures,
		resetTimeout:     config.ResetTimeout,
		halfOpenMaxCalls: config.HalfOpenMaxCalls,
		state:            StateClosed,
		onStateChange:    config.OnStateChange,
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	if !cb.canExecute() {
		return fmt.Errorf("circuit breaker %s is open", cb.name)
	}

	cb.totalCalls.Add(1)

	err := fn()
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// ExecuteWithFallback runs the function with a fallback on circuit open
func (cb *CircuitBreaker) ExecuteWithFallback(ctx context.Context, fn func() error, fallback func() error) error {
	if !cb.canExecute() {
		if fallback != nil {
			return fallback()
		}
		return fmt.Errorf("circuit breaker %s is open", cb.name)
	}

	cb.totalCalls.Add(1)

	err := fn()
	if err != nil {
		cb.recordFailure()
		if fallback != nil {
			return fallback()
		}
		return err
	}

	cb.recordSuccess()
	return nil
}

// canExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	switch state {
	case StateClosed:
		return true
	case StateOpen:
		return cb.shouldAttemptReset()
	case StateHalfOpen:
		cb.mu.RLock()
		canTry := cb.successCount < cb.halfOpenMaxCalls
		cb.mu.RUnlock()
		return canTry
	default:
		return false
	}
}

// shouldAttemptReset checks if we should try to reset from open state
func (cb *CircuitBreaker) shouldAttemptReset() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != StateOpen {
		return false
	}

	if time.Since(cb.lastFailureTime) > cb.resetTimeout {
		cb.changeState(StateHalfOpen)
		return true
	}

	return false
}

// recordSuccess records a successful execution
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses.Add(1)

	switch cb.state {
	case StateClosed:
		cb.failures = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.failures = 0
			cb.changeState(StateClosed)
		}
	}
}

// recordFailure records a failed execution
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures.Add(1)
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.maxFailures {
			cb.changeState(StateOpen)
			cb.openTime.Store(time.Now().Unix())
		}
	case StateHalfOpen:
		cb.changeState(StateOpen)
		cb.openTime.Store(time.Now().Unix())
	}
}

// changeState changes the circuit breaker state
func (cb *CircuitBreaker) changeState(newState State) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState
	cb.successCount = 0

	if cb.onStateChange != nil {
		cb.onStateChange(oldState, newState)
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics
func (cb *CircuitBreaker) GetMetrics() Metrics {
	cb.mu.RLock()
	state := cb.state
	failures := cb.failures
	cb.mu.RUnlock()

	openDuration := time.Duration(0)
	if openTime := cb.openTime.Load(); openTime > 0 && state == StateOpen {
		openDuration = time.Since(time.Unix(openTime, 0))
	}

	return Metrics{
		Name:            cb.name,
		State:           state.String(),
		TotalCalls:      cb.totalCalls.Load(),
		TotalSuccesses:  cb.totalSuccesses.Load(),
		TotalFailures:   cb.totalFailures.Load(),
		CurrentFailures: uint64(failures),
		OpenDuration:    openDuration,
	}
}

// Metrics represents circuit breaker metrics
type Metrics struct {
	Name            string
	State           string
	TotalCalls      uint64
	TotalSuccesses  uint64
	TotalFailures   uint64
	CurrentFailures uint64
	OpenDuration    time.Duration
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.successCount = 0
	cb.changeState(StateClosed)
}
