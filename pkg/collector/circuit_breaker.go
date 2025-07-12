package collector

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitBreakerState represents the circuit breaker state
type CircuitBreakerState int32

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
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

// CircuitBreaker provides circuit breaker pattern for fault tolerance
type CircuitBreaker struct {
	// Configuration
	failureThreshold int
	recoveryTimeout  time.Duration
	onStateChange    func(string)

	// State
	state           CircuitBreakerState
	failureCount    int32
	successCount    int32
	lastFailureTime time.Time
	nextAttemptTime time.Time

	// Synchronization
	mu sync.RWMutex
}

// CircuitBreakerConfig configures the circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	OnStateChange    func(string)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}

	return &CircuitBreaker{
		failureThreshold: config.FailureThreshold,
		recoveryTimeout:  config.RecoveryTimeout,
		onStateChange:    config.OnStateChange,
		state:            StateClosed,
	}
}

// Execute runs the given function if the circuit breaker allows it
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.canExecute() {
		return fmt.Errorf("circuit breaker is open")
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// canExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if now.After(cb.nextAttemptTime) {
			cb.mu.RUnlock()
			cb.mu.Lock()
			// Double-check after acquiring write lock
			if cb.state == StateOpen && now.After(cb.nextAttemptTime) {
				cb.state = StateHalfOpen
				if cb.onStateChange != nil {
					cb.onStateChange("half-open")
				}
			}
			cb.mu.Unlock()
			cb.mu.RLock()
			return cb.state == StateHalfOpen
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// recordFailure records a failure
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()
	atomic.AddInt32(&cb.failureCount, 1)

	if cb.state == StateHalfOpen {
		// Go back to open on any failure in half-open state
		cb.state = StateOpen
		cb.nextAttemptTime = time.Now().Add(cb.recoveryTimeout)
		if cb.onStateChange != nil {
			cb.onStateChange("open")
		}
	} else if int(atomic.LoadInt32(&cb.failureCount)) >= cb.failureThreshold {
		// Transition to open state
		cb.state = StateOpen
		cb.nextAttemptTime = time.Now().Add(cb.recoveryTimeout)
		if cb.onStateChange != nil {
			cb.onStateChange("open")
		}
	}
}

// recordSuccess records a success
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddInt32(&cb.successCount, 1)

	if cb.state == StateHalfOpen {
		// Transition back to closed state
		cb.state = StateClosed
		atomic.StoreInt32(&cb.failureCount, 0)
		if cb.onStateChange != nil {
			cb.onStateChange("closed")
		}
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state.String()
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":         cb.state.String(),
		"failure_count": atomic.LoadInt32(&cb.failureCount),
		"success_count": atomic.LoadInt32(&cb.successCount),
		"last_failure":  cb.lastFailureTime,
		"next_attempt":  cb.nextAttemptTime,
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	atomic.StoreInt32(&cb.failureCount, 0)
	atomic.StoreInt32(&cb.successCount, 0)
	if cb.onStateChange != nil {
		cb.onStateChange("closed")
	}
}
