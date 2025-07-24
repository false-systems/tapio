package internal

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterSimple(t *testing.T) {
	// Test NewRateLimiterSimple
	rl := NewRateLimiterSimple(100)
	if rl == nil {
		t.Fatal("NewRateLimiterSimple returned nil")
	}

	// Test Allow with context
	ctx := context.Background()
	allowed := rl.Allow(ctx)
	if !allowed {
		t.Error("First request should be allowed")
	}

	// Test that rate limiter respects the limit
	count := 0
	for i := 0; i < 150; i++ {
		if rl.Allow(ctx) {
			count++
		}
	}

	// We started with 100 tokens and already used 1
	// So we should have allowed 99 more
	if count != 99 {
		t.Errorf("Expected 99 more requests to be allowed, got %d", count)
	}
}

func TestRateLimiterWithCancelledContext(t *testing.T) {
	rl := NewRateLimiterSimple(100)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return false for cancelled context
	allowed := rl.Allow(ctx)
	if allowed {
		t.Error("Should not allow with cancelled context")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	rl := NewRateLimiterSimple(10)

	// Use all tokens
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		rl.Allow(ctx)
	}

	// Should be rate limited now
	if rl.Allow(ctx) {
		t.Error("Should be rate limited after using all tokens")
	}

	// Wait for refill
	time.Sleep(1100 * time.Millisecond)

	// Should have new tokens
	if !rl.Allow(ctx) {
		t.Error("Should have tokens after refill")
	}
}
