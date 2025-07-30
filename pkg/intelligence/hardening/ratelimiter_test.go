package hardening

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_NewRateLimiter(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected int64
	}{
		{"positive rate", 1000, 1000},
		{"zero rate", 0, 10000},
		{"negative rate", -100, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.input)
			if rl.maxTokens != tt.expected {
				t.Errorf("NewRateLimiter() maxTokens = %d, want %d", rl.maxTokens, tt.expected)
			}
			if rl.tokens != tt.expected {
				t.Errorf("NewRateLimiter() tokens = %d, want %d", rl.tokens, tt.expected)
			}
			if rl.refillRate != tt.expected {
				t.Errorf("NewRateLimiter() refillRate = %d, want %d", rl.refillRate, tt.expected)
			}
		})
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	ctx := context.Background()
	rl := NewRateLimiter(2) // 2 tokens per second

	// First two requests should be allowed
	if !rl.Allow(ctx) {
		t.Error("First request should be allowed")
	}
	if !rl.Allow(ctx) {
		t.Error("Second request should be allowed")
	}

	// Third request should be denied (no tokens left)
	if rl.Allow(ctx) {
		t.Error("Third request should be denied")
	}

	// Verify metrics
	metrics := rl.GetMetrics()
	if metrics.Allowed != 2 {
		t.Errorf("Expected 2 allowed requests, got %d", metrics.Allowed)
	}
	if metrics.Limited != 1 {
		t.Errorf("Expected 1 limited request, got %d", metrics.Limited)
	}
}

func TestRateLimiter_AllowWithRefill(t *testing.T) {
	ctx := context.Background()
	rl := NewRateLimiter(10) // 10 tokens per second

	// Consume all tokens
	for i := 0; i < 10; i++ {
		if !rl.Allow(ctx) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// Should be denied now
	if rl.Allow(ctx) {
		t.Error("Request should be denied after consuming all tokens")
	}

	// Wait for refill (slightly more than 100ms for 1 token)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(ctx) {
		t.Error("Request should be allowed after refill")
	}
}

func TestRateLimiter_AllowN(t *testing.T) {
	rl := NewRateLimiter(10)

	// Request 5 tokens - should be allowed
	if !rl.AllowN(5) {
		t.Error("Request for 5 tokens should be allowed")
	}

	// Request 6 tokens - should be denied (only 5 left)
	if rl.AllowN(6) {
		t.Error("Request for 6 tokens should be denied")
	}

	// Request 5 tokens - should be allowed
	if !rl.AllowN(5) {
		t.Error("Request for remaining 5 tokens should be allowed")
	}
}

func TestRateLimiter_ContextCancellation(t *testing.T) {
	rl := NewRateLimiter(100)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Should be denied due to cancelled context
	if rl.Allow(ctx) {
		t.Error("Request should be denied when context is cancelled")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000)
	ctx := context.Background()

	const numGoroutines = 100
	const requestsPerGoroutine = 50

	var wg sync.WaitGroup
	allowedCount := int64(0)
	deniedCount := int64(0)

	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localAllowed := 0
			localDenied := 0

			for j := 0; j < requestsPerGoroutine; j++ {
				if rl.Allow(ctx) {
					localAllowed++
				} else {
					localDenied++
				}
			}

			mu.Lock()
			allowedCount += int64(localAllowed)
			deniedCount += int64(localDenied)
			mu.Unlock()
		}()
	}

	wg.Wait()

	totalRequests := int64(numGoroutines * requestsPerGoroutine)
	if allowedCount+deniedCount != totalRequests {
		t.Errorf("Total processed requests %d != expected %d", allowedCount+deniedCount, totalRequests)
	}

	// With 1000 tokens, we should allow at least 1000 requests
	if allowedCount < 1000 {
		t.Errorf("Should have allowed at least 1000 requests, got %d", allowedCount)
	}
}

func TestRateLimiter_MetricsAccuracy(t *testing.T) {
	rl := NewRateLimiter(5)
	ctx := context.Background()

	// Make some requests
	allowed := 0
	denied := 0

	for i := 0; i < 10; i++ {
		if rl.Allow(ctx) {
			allowed++
		} else {
			denied++
		}
	}

	metrics := rl.GetMetrics()

	if int(metrics.Allowed) != allowed {
		t.Errorf("Metrics allowed %d != actual allowed %d", metrics.Allowed, allowed)
	}
	if int(metrics.Limited) != denied {
		t.Errorf("Metrics limited %d != actual denied %d", metrics.Limited, denied)
	}

	// Check utilization percentage
	if metrics.UtilizationPct < 0 || metrics.UtilizationPct > 100 {
		t.Errorf("Utilization percentage %f should be between 0-100", metrics.UtilizationPct)
	}
}

func TestRateLimiter_Stop(t *testing.T) {
	rl := NewRateLimiter(10)
	ctx := context.Background()

	// Consume some tokens
	rl.Allow(ctx)
	rl.Allow(ctx)

	// Stop should reset the limiter
	rl.Stop()

	// All tokens should be available again
	for i := 0; i < 10; i++ {
		if !rl.Allow(ctx) {
			t.Errorf("Request %d should be allowed after stop", i+1)
		}
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(10)

	// Consume all tokens
	rl.mu.Lock()
	rl.tokens = 0
	rl.lastRefill = time.Now().Add(-time.Second) // 1 second ago
	rl.mu.Unlock()

	ctx := context.Background()

	// Should be allowed due to refill
	if !rl.Allow(ctx) {
		t.Error("Request should be allowed after refill")
	}
}

func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter(1000000) // High limit to avoid blocking
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow(ctx)
		}
	})
}

func BenchmarkRateLimiter_AllowN(b *testing.B) {
	rl := NewRateLimiter(1000000) // High limit to avoid blocking

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.AllowN(1)
		}
	})
}
