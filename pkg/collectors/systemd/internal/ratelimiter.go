package internal

import (
	"sync"
	"time"
)

// RateLimiter implements token bucket algorithm for rate limiting
type RateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time

	// Metrics
	allowedCount uint64
	limitedCount uint64
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(eventsPerSecond int) *RateLimiter {
	maxTokens := float64(eventsPerSecond)
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: maxTokens,
		lastRefill: time.Now(),
	}
}

// Allow checks if an event can be processed
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens += elapsed * r.refillRate
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}
	r.lastRefill = now

	// Check if we have tokens
	if r.tokens >= 1.0 {
		r.tokens--
		r.allowedCount++
		return true
	}

	r.limitedCount++
	return false
}

// Metrics returns rate limiting metrics
func (r *RateLimiter) Metrics() (allowed, limited uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.allowedCount, r.limitedCount
}
