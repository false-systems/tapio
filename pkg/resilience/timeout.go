package resilience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"
)

var (
	ErrTimeout            = errors.New("operation timed out")
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
)

// TimeoutConfig configures timeout behavior
type TimeoutConfig struct {
	Timeout        time.Duration
	RetryStrategy  RetryStrategy
	MaxRetries     int
	CircuitBreaker *CircuitBreaker
}

// RetryStrategy defines how retries are performed
type RetryStrategy interface {
	NextDelay(attempt int) time.Duration
	ShouldRetry(err error, attempt int) bool
}

// ExponentialBackoff implements exponential backoff retry strategy
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       bool
}

// NextDelay calculates the next delay for exponential backoff
func (e *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	if e.InitialDelay == 0 {
		e.InitialDelay = 100 * time.Millisecond
	}
	if e.MaxDelay == 0 {
		e.MaxDelay = 30 * time.Second
	}
	if e.Multiplier == 0 {
		e.Multiplier = 2.0
	}

	delay := float64(e.InitialDelay) * math.Pow(e.Multiplier, float64(attempt))
	if delay > float64(e.MaxDelay) {
		delay = float64(e.MaxDelay)
	}

	if e.Jitter {
		jitter := rand.Float64() * 0.3 * delay
		delay = delay + jitter
	}

	return time.Duration(delay)
}

// ShouldRetry determines if the operation should be retried
func (e *ExponentialBackoff) ShouldRetry(err error, attempt int) bool {
	if err == nil {
		return false
	}

	// Don't retry context cancellations
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Always retry timeouts
	if errors.Is(err, ErrTimeout) {
		return true
	}

	// Default to retry
	return true
}

// LinearBackoff implements linear backoff retry strategy
type LinearBackoff struct {
	Delay time.Duration
}

// NextDelay returns constant delay for linear backoff
func (l *LinearBackoff) NextDelay(attempt int) time.Duration {
	if l.Delay == 0 {
		l.Delay = time.Second
	}
	return l.Delay
}

// ShouldRetry determines if the operation should be retried
func (l *LinearBackoff) ShouldRetry(err error, attempt int) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return false
	}

	return true
}

// TimeoutManager manages timeouts and retries
type TimeoutManager struct {
	config         TimeoutConfig
	totalAttempts  atomic.Uint64
	totalTimeouts  atomic.Uint64
	totalRetries   atomic.Uint64
	totalSuccesses atomic.Uint64
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(config TimeoutConfig) *TimeoutManager {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.RetryStrategy == nil {
		config.RetryStrategy = &ExponentialBackoff{
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			Multiplier:   2.0,
			Jitter:       true,
		}
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	return &TimeoutManager{
		config: config,
	}
}

// Execute runs a function with timeout and retry logic
func (tm *TimeoutManager) Execute(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	tm.totalAttempts.Add(1)

	var lastErr error

	for attempt := 0; attempt <= tm.config.MaxRetries; attempt++ {
		if attempt > 0 {
			tm.totalRetries.Add(1)
			delay := tm.config.RetryStrategy.NextDelay(attempt - 1)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Execute with circuit breaker if configured
		var err error
		if tm.config.CircuitBreaker != nil {
			err = tm.config.CircuitBreaker.Execute(ctx, func() error {
				return tm.executeWithTimeout(ctx, name, fn)
			})
		} else {
			err = tm.executeWithTimeout(ctx, name, fn)
		}

		if err == nil {
			tm.totalSuccesses.Add(1)
			return nil
		}

		lastErr = err

		// Check if we should retry
		if !tm.config.RetryStrategy.ShouldRetry(err, attempt) {
			break
		}

		// Don't retry if context is done
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	if lastErr == nil {
		lastErr = ErrMaxRetriesExceeded
	}

	return fmt.Errorf("%s failed after %d attempts: %w", name, tm.config.MaxRetries+1, lastErr)
}

// executeWithTimeout executes the function with a timeout
func (tm *TimeoutManager) executeWithTimeout(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, tm.config.Timeout)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- fn(timeoutCtx)
	}()

	select {
	case <-timeoutCtx.Done():
		tm.totalTimeouts.Add(1)
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s: %w", name, ErrTimeout)
		}
		return timeoutCtx.Err()
	case err := <-done:
		return err
	}
}

// GetMetrics returns timeout manager metrics
func (tm *TimeoutManager) GetMetrics() TimeoutMetrics {
	return TimeoutMetrics{
		TotalAttempts:  tm.totalAttempts.Load(),
		TotalTimeouts:  tm.totalTimeouts.Load(),
		TotalRetries:   tm.totalRetries.Load(),
		TotalSuccesses: tm.totalSuccesses.Load(),
	}
}

// TimeoutMetrics represents timeout manager metrics
type TimeoutMetrics struct {
	TotalAttempts  uint64
	TotalTimeouts  uint64
	TotalRetries   uint64
	TotalSuccesses uint64
}

// BoundedExecutor limits concurrent executions with timeout
type BoundedExecutor struct {
	semaphore          chan struct{}
	timeout            time.Duration
	totalExecutions    atomic.Uint64
	activeExecutions   atomic.Int32
	rejectedExecutions atomic.Uint64
}

// NewBoundedExecutor creates a new bounded executor
func NewBoundedExecutor(maxConcurrent int, timeout time.Duration) *BoundedExecutor {
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &BoundedExecutor{
		semaphore: make(chan struct{}, maxConcurrent),
		timeout:   timeout,
	}
}

// Execute runs a function with bounded concurrency
func (be *BoundedExecutor) Execute(ctx context.Context, fn func() error) error {
	be.totalExecutions.Add(1)

	// Try to acquire semaphore
	select {
	case be.semaphore <- struct{}{}:
		be.activeExecutions.Add(1)
		defer func() {
			<-be.semaphore
			be.activeExecutions.Add(-1)
		}()
	case <-ctx.Done():
		be.rejectedExecutions.Add(1)
		return ctx.Err()
	case <-time.After(be.timeout):
		be.rejectedExecutions.Add(1)
		return ErrTimeout
	}

	// Execute with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, be.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case <-timeoutCtx.Done():
		return ErrTimeout
	case err := <-done:
		return err
	}
}

// GetMetrics returns bounded executor metrics
func (be *BoundedExecutor) GetMetrics() BoundedExecutorMetrics {
	return BoundedExecutorMetrics{
		TotalExecutions:    be.totalExecutions.Load(),
		ActiveExecutions:   int32(be.activeExecutions.Load()),
		RejectedExecutions: be.rejectedExecutions.Load(),
		MaxConcurrent:      cap(be.semaphore),
	}
}

// BoundedExecutorMetrics represents bounded executor metrics
type BoundedExecutorMetrics struct {
	TotalExecutions    uint64
	ActiveExecutions   int32
	RejectedExecutions uint64
	MaxConcurrent      int
}
