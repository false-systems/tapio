package hardening

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter implements token bucket algorithm for rate limiting
type RateLimiter struct {
	mu          sync.Mutex
	maxTokens   int64
	tokens      int64
	refillRate  int64 // tokens per second
	lastRefill  time.Time
	metrics     *RateLimiterMetrics
	currentRate atomic.Int64 // Current rate limit
}

// RateLimiterMetrics tracks rate limiter statistics
type RateLimiterMetrics struct {
	mu             sync.RWMutex
	Allowed        uint64
	Limited        uint64
	CurrentTokens  int64
	UtilizationPct float64
}

// NewRateLimiter creates a new rate limiter with simple params
func NewRateLimiter(maxEventsPerSecond int64) *RateLimiter {
	if maxEventsPerSecond <= 0 {
		maxEventsPerSecond = 10000 // Default: 10k events/sec
	}

	rl := &RateLimiter{
		maxTokens:  maxEventsPerSecond,
		tokens:     maxEventsPerSecond,
		refillRate: maxEventsPerSecond,
		lastRefill: time.Now(),
		metrics:    &RateLimiterMetrics{},
	}
	rl.currentRate.Store(maxEventsPerSecond)
	return rl
}

// Allow checks if an event can be processed (with context support)
func (r *RateLimiter) Allow(ctx context.Context) bool {
	// Check context first
	select {
	case <-ctx.Done():
		return false
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens based on elapsed time
	r.refill()

	// Check if we have tokens available
	if r.tokens > 0 {
		r.tokens--
		r.updateMetrics(true)
		return true
	}

	r.updateMetrics(false)
	return false
}

// AllowN checks if N events can be processed
func (r *RateLimiter) AllowN(n int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= n {
		r.tokens -= n
		r.updateMetrics(true)
		return true
	}

	r.updateMetrics(false)
	return false
}

// refill adds tokens based on elapsed time
func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()

	if elapsed > 0 {
		tokensToAdd := int64(elapsed * float64(r.refillRate))
		r.tokens += tokensToAdd

		// Cap at max tokens
		if r.tokens > r.maxTokens {
			r.tokens = r.maxTokens
		}

		r.lastRefill = now
	}
}

// updateMetrics updates rate limiter metrics
func (r *RateLimiter) updateMetrics(allowed bool) {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()

	if allowed {
		r.metrics.Allowed++
	} else {
		r.metrics.Limited++
	}

	r.metrics.CurrentTokens = r.tokens
	if r.maxTokens > 0 {
		r.metrics.UtilizationPct = float64(r.maxTokens-r.tokens) / float64(r.maxTokens) * 100
	}
}

// GetMetrics returns current rate limiter metrics
func (r *RateLimiter) GetMetrics() RateLimiterMetrics {
	r.metrics.mu.RLock()
	defer r.metrics.mu.RUnlock()

	return RateLimiterMetrics{
		Allowed:        r.metrics.Allowed,
		Limited:        r.metrics.Limited,
		CurrentTokens:  r.metrics.CurrentTokens,
		UtilizationPct: r.metrics.UtilizationPct,
	}
}

// Stop gracefully stops the rate limiter
func (r *RateLimiter) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tokens = r.maxTokens
	r.lastRefill = time.Now()

	r.metrics.mu.Lock()
	r.metrics.Allowed = 0
	r.metrics.Limited = 0
	r.metrics.mu.Unlock()
}
