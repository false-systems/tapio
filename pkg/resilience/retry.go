package resilience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Retryer implements retry logic with various strategies
type Retryer struct {
	config        RetryConfig
	attemptCount  atomic.Uint64
	successCount  atomic.Uint64
	failureCount  atomic.Uint64
	totalDuration atomic.Int64
}

// RetryConfig configures retry behavior
type RetryConfig struct {
	// Basic settings
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	
	// Backoff settings
	BackoffStrategy BackoffStrategy
	Multiplier      float64
	Jitter          float64
	
	// Circuit breaker integration
	UseCircuitBreaker bool
	CircuitBreaker    *CircuitBreaker
	
	// Retry conditions
	RetryableErrors   []error
	RetryableChecker  func(error) bool
	OnRetry           func(attempt int, err error)
}

// BackoffStrategy defines the backoff strategy
type BackoffStrategy string

const (
	BackoffConstant     BackoffStrategy = "constant"
	BackoffLinear       BackoffStrategy = "linear"
	BackoffExponential  BackoffStrategy = "exponential"
	BackoffFibonacci    BackoffStrategy = "fibonacci"
	BackoffRandom       BackoffStrategy = "random"
)

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:      3,
		InitialDelay:     100 * time.Millisecond,
		MaxDelay:         30 * time.Second,
		BackoffStrategy:  BackoffExponential,
		Multiplier:       2.0,
		Jitter:           0.1,
		RetryableChecker: defaultRetryableChecker,
	}
}

// NewRetryer creates a new retryer
func NewRetryer(config RetryConfig) *Retryer {
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}
	if config.InitialDelay == 0 {
		config.InitialDelay = 100 * time.Millisecond
	}
	if config.MaxDelay == 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.Multiplier == 0 {
		config.Multiplier = 2.0
	}
	if config.RetryableChecker == nil {
		config.RetryableChecker = defaultRetryableChecker
	}
	
	return &Retryer{
		config: config,
	}
}

// Execute executes a function with retry logic
func (r *Retryer) Execute(ctx context.Context, fn func() error) error {
	_, err := r.ExecuteWithResult(ctx, func() (interface{}, error) {
		return nil, fn()
	})
	return err
}

// ExecuteWithResult executes a function that returns a result with retry logic
func (r *Retryer) ExecuteWithResult(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	startTime := time.Now()
	defer func() {
		r.totalDuration.Add(int64(time.Since(startTime)))
	}()
	
	var lastErr error
	fibPrev, fibCurr := 0, 1
	
	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		r.attemptCount.Add(1)
		
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}
		
		// Execute with circuit breaker if configured
		var result interface{}
		var err error
		
		if r.config.UseCircuitBreaker && r.config.CircuitBreaker != nil {
			cbErr := r.config.CircuitBreaker.Execute(ctx, func() error {
				result, err = fn()
				return err
			})
			if cbErr != nil {
				err = cbErr
			}
		} else {
			result, err = fn()
		}
		
		if err == nil {
			r.successCount.Add(1)
			return result, nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !r.isRetryable(err) {
			r.failureCount.Add(1)
			return nil, err
		}
		
		// Don't delay on last attempt
		if attempt < r.config.MaxAttempts {
			// Calculate delay
			delay := r.calculateDelay(attempt, fibPrev, fibCurr)
			if r.config.BackoffStrategy == BackoffFibonacci {
				fibPrev, fibCurr = fibCurr, fibPrev+fibCurr
			}
			
			// Apply jitter
			if r.config.Jitter > 0 {
				delay = r.applyJitter(delay)
			}
			
			// Ensure delay doesn't exceed max
			if delay > r.config.MaxDelay {
				delay = r.config.MaxDelay
			}
			
			// Call retry callback
			if r.config.OnRetry != nil {
				r.config.OnRetry(attempt, err)
			}
			
			// Wait before next attempt
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	
	r.failureCount.Add(1)
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// calculateDelay calculates the delay for the next retry
func (r *Retryer) calculateDelay(attempt int, fibPrev, fibCurr int) time.Duration {
	switch r.config.BackoffStrategy {
	case BackoffConstant:
		return r.config.InitialDelay
		
	case BackoffLinear:
		return time.Duration(attempt) * r.config.InitialDelay
		
	case BackoffExponential:
		delay := float64(r.config.InitialDelay) * math.Pow(r.config.Multiplier, float64(attempt-1))
		return time.Duration(delay)
		
	case BackoffFibonacci:
		return time.Duration(fibCurr) * r.config.InitialDelay
		
	case BackoffRandom:
		max := float64(r.config.MaxDelay)
		return time.Duration(rand.Float64() * max)
		
	default:
		return r.config.InitialDelay
	}
}

// applyJitter applies jitter to the delay
func (r *Retryer) applyJitter(delay time.Duration) time.Duration {
	jitter := r.config.Jitter
	if jitter > 1 {
		jitter = 1
	}
	
	// Calculate jitter range
	maxJitter := float64(delay) * jitter
	jitterValue := (rand.Float64() * 2 * maxJitter) - maxJitter
	
	newDelay := float64(delay) + jitterValue
	if newDelay < 0 {
		newDelay = 0
	}
	
	return time.Duration(newDelay)
}

// isRetryable checks if an error is retryable
func (r *Retryer) isRetryable(err error) bool {
	// Check specific errors
	for _, retryableErr := range r.config.RetryableErrors {
		if errors.Is(err, retryableErr) {
			return true
		}
	}
	
	// Use custom checker
	return r.config.RetryableChecker(err)
}

// GetMetrics returns retry metrics
func (r *Retryer) GetMetrics() RetryMetrics {
	attempts := r.attemptCount.Load()
	successes := r.successCount.Load()
	failures := r.failureCount.Load()
	totalDuration := time.Duration(r.totalDuration.Load())
	
	var avgDuration time.Duration
	if attempts > 0 {
		avgDuration = totalDuration / time.Duration(attempts)
	}
	
	var successRate float64
	if attempts > 0 {
		successRate = float64(successes) / float64(attempts)
	}
	
	return RetryMetrics{
		TotalAttempts:   attempts,
		SuccessCount:    successes,
		FailureCount:    failures,
		SuccessRate:     successRate,
		TotalDuration:   totalDuration,
		AverageDuration: avgDuration,
	}
}

// Reset resets retry metrics
func (r *Retryer) Reset() {
	r.attemptCount.Store(0)
	r.successCount.Store(0)
	r.failureCount.Store(0)
	r.totalDuration.Store(0)
}

// RetryMetrics contains retry metrics
type RetryMetrics struct {
	TotalAttempts   uint64
	SuccessCount    uint64
	FailureCount    uint64
	SuccessRate     float64
	TotalDuration   time.Duration
	AverageDuration time.Duration
}

// defaultRetryableChecker is the default retryable error checker
func defaultRetryableChecker(err error) bool {
	// Retry on temporary errors
	type temporary interface {
		Temporary() bool
	}
	if temp, ok := err.(temporary); ok && temp.Temporary() {
		return true
	}
	
	// Retry on timeout errors
	type timeout interface {
		Timeout() bool
	}
	if to, ok := err.(timeout); ok && to.Timeout() {
		return true
	}
	
	// Don't retry on context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	
	return false
}

// RetryGroup manages multiple retryers
type RetryGroup struct {
	retryers map[string]*Retryer
	mutex    sync.RWMutex
}

// NewRetryGroup creates a new retry group
func NewRetryGroup() *RetryGroup {
	return &RetryGroup{
		retryers: make(map[string]*Retryer),
	}
}

// Add adds a retryer to the group
func (g *RetryGroup) Add(name string, retryer *Retryer) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.retryers[name] = retryer
}

// Get gets a retryer by name
func (g *RetryGroup) Get(name string) (*Retryer, bool) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	retryer, exists := g.retryers[name]
	return retryer, exists
}

// GetOrCreate gets or creates a retryer
func (g *RetryGroup) GetOrCreate(name string, config RetryConfig) *Retryer {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	
	if retryer, exists := g.retryers[name]; exists {
		return retryer
	}
	
	retryer := NewRetryer(config)
	g.retryers[name] = retryer
	return retryer
}

// GetMetrics returns metrics for all retryers
func (g *RetryGroup) GetMetrics() map[string]RetryMetrics {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	
	metrics := make(map[string]RetryMetrics)
	for name, retryer := range g.retryers {
		metrics[name] = retryer.GetMetrics()
	}
	return metrics
}

// ResetAll resets all retryers
func (g *RetryGroup) ResetAll() {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	
	for _, retryer := range g.retryers {
		retryer.Reset()
	}
}

// WithRetry is a convenience function for simple retry logic
func WithRetry(ctx context.Context, fn func() error, opts ...RetryOption) error {
	config := DefaultRetryConfig()
	for _, opt := range opts {
		opt(&config)
	}
	
	retryer := NewRetryer(config)
	return retryer.Execute(ctx, fn)
}

// RetryOption configures retry behavior
type RetryOption func(*RetryConfig)

// WithMaxAttempts sets max retry attempts
func WithMaxAttempts(attempts int) RetryOption {
	return func(c *RetryConfig) {
		c.MaxAttempts = attempts
	}
}

// WithBackoff sets backoff strategy
func WithBackoff(strategy BackoffStrategy) RetryOption {
	return func(c *RetryConfig) {
		c.BackoffStrategy = strategy
	}
}

// WithDelay sets initial delay
func WithDelay(delay time.Duration) RetryOption {
	return func(c *RetryConfig) {
		c.InitialDelay = delay
	}
}

// WithJitter sets jitter percentage
func WithJitter(jitter float64) RetryOption {
	return func(c *RetryConfig) {
		c.Jitter = jitter
	}
}

// WithCircuitBreaker enables circuit breaker
func WithCircuitBreaker(cb *CircuitBreaker) RetryOption {
	return func(c *RetryConfig) {
		c.UseCircuitBreaker = true
		c.CircuitBreaker = cb
	}
}

// WithRetryableChecker sets custom retryable checker
func WithRetryableChecker(checker func(error) bool) RetryOption {
	return func(c *RetryConfig) {
		c.RetryableChecker = checker
	}
}