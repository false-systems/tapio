package common

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxRetries     int              // Maximum number of retry attempts
	InitialDelay   time.Duration    // Initial delay between retries
	MaxDelay       time.Duration    // Maximum delay between retries
	Multiplier     float64          // Backoff multiplier
	Jitter         float64          // Jitter factor (0-1)
	RetryableError func(error) bool // Function to determine if error is retryable
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   5,
		InitialDelay: time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
		RetryableError: func(err error) bool {
			// By default, all errors are retryable
			return true
		},
	}
}

// RetryManager handles retry logic with exponential backoff
type RetryManager struct {
	config RetryConfig
	mu     sync.Mutex
	rand   *rand.Rand
}

// NewRetryManager creates a new retry manager
func NewRetryManager(config RetryConfig) *RetryManager {
	return &RetryManager{
		config: config,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// RetryOperation represents an operation that can be retried
type RetryOperation func(ctx context.Context) error

// Execute executes an operation with retry logic
func (rm *RetryManager) Execute(ctx context.Context, operation RetryOperation) error {
	var lastErr error

	for attempt := 0; attempt <= rm.config.MaxRetries; attempt++ {
		// Execute the operation
		err := operation(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !rm.config.RetryableError(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Check if this was the last attempt
		if attempt == rm.config.MaxRetries {
			break
		}

		// Calculate delay with exponential backoff
		delay := rm.calculateDelay(attempt)

		// Wait or return if context is cancelled
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next retry
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", rm.config.MaxRetries, lastErr)
}

// ExecuteAsync executes an operation asynchronously with retry logic
func (rm *RetryManager) ExecuteAsync(ctx context.Context, operation RetryOperation, callback func(error)) {
	go func() {
		err := rm.Execute(ctx, operation)
		if callback != nil {
			callback(err)
		}
	}()
}

// calculateDelay calculates the delay for the next retry attempt
func (rm *RetryManager) calculateDelay(attempt int) time.Duration {
	// Calculate base delay with exponential backoff
	baseDelay := float64(rm.config.InitialDelay) * math.Pow(rm.config.Multiplier, float64(attempt))

	// Cap at max delay
	if baseDelay > float64(rm.config.MaxDelay) {
		baseDelay = float64(rm.config.MaxDelay)
	}

	// Add jitter
	rm.mu.Lock()
	jitter := rm.rand.Float64() * rm.config.Jitter * baseDelay
	rm.mu.Unlock()

	// Randomly add or subtract jitter
	if rm.rand.Intn(2) == 0 {
		baseDelay += jitter
	} else {
		baseDelay -= jitter
	}

	return time.Duration(baseDelay)
}

// CircuitBreaker implements circuit breaker pattern for network failures
type CircuitBreaker struct {
	mu              sync.RWMutex
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	state           CircuitState
	config          CircuitBreakerConfig
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreakerConfig configures circuit breaker behavior
type CircuitBreakerConfig struct {
	FailureThreshold int           // Number of failures to open circuit
	SuccessThreshold int           // Number of successes to close circuit
	Timeout          time.Duration // Time to wait before moving from open to half-open
	MaxConcurrent    int           // Max concurrent requests in half-open state
}

// DefaultCircuitBreakerConfig returns default circuit breaker configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxConcurrent:    1,
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: config,
	}
}

// Execute executes an operation through the circuit breaker
func (cb *CircuitBreaker) Execute(ctx context.Context, operation func() error) error {
	if !cb.CanExecute() {
		return fmt.Errorf("circuit breaker is open")
	}

	err := operation()

	if err != nil {
		cb.RecordFailure()
	} else {
		cb.RecordSuccess()
	}

	return err
}

// CanExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if timeout has passed
		if time.Since(cb.lastFailureTime) > cb.config.Timeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = CircuitHalfOpen
			cb.mu.Unlock()
			cb.mu.RLock()
			return true
		}
		return false
	case CircuitHalfOpen:
		// Allow limited requests
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++
	cb.failureCount = 0

	if cb.state == CircuitHalfOpen && cb.successCount >= cb.config.SuccessThreshold {
		cb.state = CircuitClosed
		cb.successCount = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()
	cb.successCount = 0

	if cb.failureCount >= cb.config.FailureThreshold {
		cb.state = CircuitOpen
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.successCount = 0
}
