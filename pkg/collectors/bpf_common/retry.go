package bpf_common

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts     int           // Maximum number of retry attempts
	InitialDelay    time.Duration // Initial delay between retries
	MaxDelay        time.Duration // Maximum delay between retries
	BackoffFactor   float64       // Exponential backoff factor (typically 2.0)
	JitterFactor    float64       // Jitter factor (0-1) to randomize delays
	RetryableErrors []error       // Specific errors that should trigger retry
}

// DefaultRetryConfig returns sensible defaults for retry behavior
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0.1, // 10% jitter
	}
}

// RetryableFunc is a function that can be retried
type RetryableFunc func(ctx context.Context) error

// RetryWithBackoff executes a function with exponential backoff retry
func RetryWithBackoff(ctx context.Context, config RetryConfig, fn RetryableFunc) error {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// Execute the function
		err := fn(ctx)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if this is the last attempt
		if attempt == config.MaxAttempts-1 {
			break
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}

		// Calculate delay with exponential backoff
		delay := calculateDelay(attempt, config)

		// Wait with context cancellation support
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled during backoff: %w", ctx.Err())
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxAttempts, lastErr)
}

// calculateDelay calculates the delay for the next retry attempt
func calculateDelay(attempt int, config RetryConfig) time.Duration {
	// Calculate base delay with exponential backoff
	baseDelay := float64(config.InitialDelay) * math.Pow(config.BackoffFactor, float64(attempt))

	// Apply max delay cap
	if baseDelay > float64(config.MaxDelay) {
		baseDelay = float64(config.MaxDelay)
	}

	// Add jitter to avoid thundering herd
	if config.JitterFactor > 0 {
		jitter := baseDelay * config.JitterFactor * (rand.Float64()*2 - 1) // Random between -jitter and +jitter
		baseDelay += jitter

		// Ensure delay is still positive
		if baseDelay < 0 {
			baseDelay = float64(config.InitialDelay)
		}
	}

	return time.Duration(baseDelay)
}

// CircuitBreaker provides circuit breaker pattern for handling repeated failures
type CircuitBreaker struct {
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenRequests int

	failures    int
	lastFailure time.Time
	state       CircuitState
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, reject requests
	CircuitHalfOpen                     // Testing if service recovered
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		halfOpenRequests: 1, // Allow 1 request in half-open state
		state:            CircuitClosed,
	}
}

// Execute runs a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn RetryableFunc) error {
	// Check circuit state
	switch cb.getState() {
	case CircuitOpen:
		return fmt.Errorf("circuit breaker is open")
	case CircuitHalfOpen:
		// Allow limited requests through
		if cb.halfOpenRequests <= 0 {
			return fmt.Errorf("circuit breaker is half-open, request rejected")
		}
		cb.halfOpenRequests--
	}

	// Execute the function
	err := fn(ctx)

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// getState returns the current circuit state
func (cb *CircuitBreaker) getState() CircuitState {
	if cb.state == CircuitOpen {
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.halfOpenRequests = 1
		}
	}
	return cb.state
}

// recordFailure records a failure and potentially opens the circuit
func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = CircuitOpen
	}
}

// recordSuccess records a success and potentially closes the circuit
func (cb *CircuitBreaker) recordSuccess() {
	cb.failures = 0
	cb.state = CircuitClosed
}

// IsOpen returns true if the circuit is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.getState() == CircuitOpen
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.failures = 0
	cb.state = CircuitClosed
}
