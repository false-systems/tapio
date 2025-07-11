package k8s

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResilientClient_WithRetry(t *testing.T) {
	tests := []struct {
		name        string
		config      *ResilientConfig
		mockError   error
		expectRetry bool
		expectError bool
	}{
		{
			name:        "timeout error should retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewTimeoutError("timeout", 1),
			expectRetry: true,
			expectError: false,
		},
		{
			name:        "server timeout should retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewServerTimeout(schema.GroupResource{}, "get", 1),
			expectRetry: true,
			expectError: false,
		},
		{
			name:        "service unavailable should retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewServiceUnavailable("unavailable"),
			expectRetry: true,
			expectError: false,
		},
		{
			name:        "too many requests should retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewTooManyRequestsError("rate limited"),
			expectRetry: true,
			expectError: false,
		},
		{
			name:        "internal error should retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewInternalError(errors.New("internal")),
			expectRetry: true,
			expectError: false,
		},
		{
			name:        "not found should not retry",
			config:      DefaultResilientConfig(),
			mockError:   apierrors.NewNotFound(schema.GroupResource{}, "test"),
			expectRetry: false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &ResilientClient{
				Client:         &Client{},
				rateLimiter:    rate.NewLimiter(rate.Limit(tt.config.RateLimit), tt.config.RateBurst),
				requestTracker: &RequestTracker{inFlight: make(map[string]*InFlightRequest)},
			}

			retryCount := 0
			err := client.WithRetry(context.Background(), func() error {
				retryCount++
				if retryCount <= 3 {
					return tt.mockError
				}
				return nil
			})

			if tt.expectRetry && retryCount <= 1 {
				t.Errorf("Expected retry, but function was called %d times", retryCount)
			}

			if tt.expectError && err == nil {
				t.Errorf("Expected error, but got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}
		})
	}
}

func TestResilientClient_ExecuteWithDedup(t *testing.T) {
	client := &ResilientClient{
		Client: &Client{},
		requestTracker: &RequestTracker{
			inFlight:    make(map[string]*InFlightRequest),
			dedupWindow: 1 * time.Second,
		},
	}

	key := "test-key"
	executeCount := 0
	expectedResult := "test-result"

	fn := func() (interface{}, error) {
		executeCount++
		time.Sleep(100 * time.Millisecond)
		return expectedResult, nil
	}

	// Start multiple concurrent requests with the same key
	results := make([]interface{}, 3)
	errors := make([]error, 3)
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errors[index] = client.ExecuteWithDedup(context.Background(), key, fn)
		}(i)
	}

	wg.Wait()

	// Verify deduplication worked
	if executeCount != 1 {
		t.Errorf("Expected function to be executed once, but was executed %d times", executeCount)
	}

	// Verify all requests got the same result
	for i, result := range results {
		if errors[i] != nil {
			t.Errorf("Request %d failed with error: %v", i, errors[i])
		}
		if result != expectedResult {
			t.Errorf("Request %d got result %v, expected %v", i, result, expectedResult)
		}
	}
}

func TestResilientClient_RateLimit(t *testing.T) {
	config := &ResilientConfig{
		RateLimit: 2,
		RateBurst: 2,
	}

	client := &ResilientClient{
		Client:      &Client{},
		rateLimiter: rate.NewLimiter(rate.Limit(config.RateLimit), config.RateBurst),
		requestTracker: &RequestTracker{
			inFlight: make(map[string]*InFlightRequest),
		},
	}

	start := time.Now()
	requests := 5

	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := client.WithRetry(context.Background(), func() error {
				return nil
			})
			if err != nil {
				t.Errorf("Request failed: %v", err)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// With rate limit of 2/sec, 5 requests should take at least 2 seconds
	expectedMinDuration := 2 * time.Second
	if elapsed < expectedMinDuration {
		t.Errorf("Rate limiting not working. Expected at least %v, got %v", expectedMinDuration, elapsed)
	}
}

func TestRequestTracker_Cleanup(t *testing.T) {
	rt := &RequestTracker{
		inFlight:    make(map[string]*InFlightRequest),
		dedupWindow: 100 * time.Millisecond,
	}

	// Add expired request
	expiredReq := &InFlightRequest{
		key:        "expired",
		expiry:     time.Now().Add(-1 * time.Minute),
		references: 0,
	}
	rt.inFlight["expired"] = expiredReq

	// Add active request
	activeReq := &InFlightRequest{
		key:        "active",
		expiry:     time.Now().Add(1 * time.Minute),
		references: 1,
	}
	rt.inFlight["active"] = activeReq

	rt.cleanup()

	if _, exists := rt.inFlight["expired"]; exists {
		t.Error("Expired request should have been cleaned up")
	}

	if _, exists := rt.inFlight["active"]; !exists {
		t.Error("Active request should not have been cleaned up")
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name  string
		error error
		want  bool
	}{
		{
			name:  "timeout error",
			error: apierrors.NewTimeoutError("timeout", 1),
			want:  true,
		},
		{
			name:  "server timeout",
			error: apierrors.NewServerTimeout(schema.GroupResource{}, "get", 1),
			want:  true,
		},
		{
			name:  "service unavailable",
			error: apierrors.NewServiceUnavailable("unavailable"),
			want:  true,
		},
		{
			name:  "too many requests",
			error: apierrors.NewTooManyRequestsError("rate limited"),
			want:  true,
		},
		{
			name:  "internal error",
			error: apierrors.NewInternalError(errors.New("internal")),
			want:  true,
		},
		{
			name:  "not found",
			error: apierrors.NewNotFound(schema.GroupResource{}, "test"),
			want:  false,
		},
		{
			name:  "forbidden",
			error: apierrors.NewForbidden(schema.GroupResource{}, "test", errors.New("forbidden")),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.error); got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkResilientClient_ExecuteWithDedup(b *testing.B) {
	client := &ResilientClient{
		Client: &Client{},
		requestTracker: &RequestTracker{
			inFlight:    make(map[string]*InFlightRequest),
			dedupWindow: 1 * time.Second,
		},
	}

	fn := func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return "result", nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = client.ExecuteWithDedup(context.Background(), "benchmark-key", fn)
		}
	})
}

func BenchmarkResilientClient_WithRetry(b *testing.B) {
	client := &ResilientClient{
		Client:      &Client{},
		rateLimiter: rate.NewLimiter(rate.Inf, 1000),
		requestTracker: &RequestTracker{
			inFlight: make(map[string]*InFlightRequest),
		},
	}

	fn := func() error {
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.WithRetry(context.Background(), fn)
	}
}
