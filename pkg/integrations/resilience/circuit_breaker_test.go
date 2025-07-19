package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	tests := []struct {
		name           string
		config         CircuitBreakerConfig
		scenario       func(t *testing.T, cb *CircuitBreaker)
		expectedStates []State
	}{
		{
			name: "closed_to_open_on_max_failures",
			config: CircuitBreakerConfig{
				Name:         "test",
				MaxFailures:  3,
				ResetTimeout: 100 * time.Millisecond,
			},
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Generate failures to trigger open state
				for i := 0; i < 3; i++ {
					err := cb.Execute(context.Background(), func() error {
						return errors.New("test error")
					})
					if err == nil {
						t.Fatal("expected error")
					}
				}
			},
			expectedStates: []State{StateClosed, StateOpen},
		},
		{
			name: "open_to_half_open_after_timeout",
			config: CircuitBreakerConfig{
				Name:         "test",
				MaxFailures:  1,
				ResetTimeout: 100 * time.Millisecond,
			},
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Trigger open state
				cb.Execute(context.Background(), func() error {
					return errors.New("test error")
				})

				// Wait for reset timeout
				time.Sleep(150 * time.Millisecond)

				// Next call should be allowed (half-open)
				cb.Execute(context.Background(), func() error {
					return nil
				})
			},
			expectedStates: []State{StateClosed, StateOpen, StateHalfOpen, StateClosed},
		},
		{
			name: "half_open_to_closed_on_success",
			config: CircuitBreakerConfig{
				Name:             "test",
				MaxFailures:      1,
				ResetTimeout:     100 * time.Millisecond,
				HalfOpenMaxCalls: 1,
			},
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Trigger open state
				cb.Execute(context.Background(), func() error {
					return errors.New("test error")
				})

				// Wait for reset timeout
				time.Sleep(150 * time.Millisecond)

				// Successful call should close circuit
				cb.Execute(context.Background(), func() error {
					return nil
				})
			},
			expectedStates: []State{StateClosed, StateOpen, StateHalfOpen, StateClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var states []State
			var mu sync.Mutex

			tt.config.OnStateChange = func(old, new State) {
				mu.Lock()
				states = append(states, new)
				mu.Unlock()
			}

			cb := NewCircuitBreaker(tt.config)
			states = append(states, cb.GetState())

			tt.scenario(t, cb)

			// Allow time for state changes
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			defer mu.Unlock()

			if len(states) != len(tt.expectedStates) {
				t.Fatalf("expected %d states, got %d: %v", len(tt.expectedStates), len(states), states)
			}

			for i, expected := range tt.expectedStates {
				if states[i] != expected {
					t.Errorf("state %d: expected %v, got %v", i, expected, states[i])
				}
			}
		})
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "concurrent-test",
		MaxFailures:  10,
		ResetTimeout: 100 * time.Millisecond,
	})

	var (
		wg            sync.WaitGroup
		successCount  atomic.Int32
		failureCount  atomic.Int32
		rejectedCount atomic.Int32
	)

	// Run concurrent operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			err := cb.Execute(context.Background(), func() error {
				// Simulate mixed success/failure
				if i%3 == 0 {
					return errors.New("simulated error")
				}
				return nil
			})

			if err != nil {
				if err.Error() == "circuit breaker concurrent-test is open" {
					rejectedCount.Add(1)
				} else {
					failureCount.Add(1)
				}
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	metrics := cb.GetMetrics()
	total := successCount.Load() + failureCount.Load() + rejectedCount.Load()

	if total != 100 {
		t.Errorf("expected 100 total operations, got %d", total)
	}

	if metrics.TotalCalls != uint64(100-rejectedCount.Load()) {
		t.Errorf("metrics mismatch: total calls = %d, expected = %d",
			metrics.TotalCalls, 100-rejectedCount.Load())
	}
}

func TestCircuitBreaker_Fallback(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "fallback-test",
		MaxFailures:  1,
		ResetTimeout: 100 * time.Millisecond,
	})

	fallbackCalled := false
	fallback := func() error {
		fallbackCalled = true
		return nil
	}

	// First call fails and triggers open state
	err := cb.ExecuteWithFallback(context.Background(), func() error {
		return errors.New("primary failed")
	}, fallback)

	if err != nil {
		t.Errorf("unexpected error with fallback: %v", err)
	}

	if !fallbackCalled {
		t.Error("fallback was not called on failure")
	}

	// Reset flag
	fallbackCalled = false

	// Circuit should be open, fallback should be called
	err = cb.ExecuteWithFallback(context.Background(), func() error {
		t.Error("primary function should not be called when circuit is open")
		return nil
	}, fallback)

	if err != nil {
		t.Errorf("unexpected error with fallback: %v", err)
	}

	if !fallbackCalled {
		t.Error("fallback was not called when circuit was open")
	}
}

func TestCircuitBreaker_Metrics(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "metrics-test",
		MaxFailures:  5,
		ResetTimeout: 100 * time.Millisecond,
	})

	// Generate some successes
	for i := 0; i < 10; i++ {
		cb.Execute(context.Background(), func() error {
			return nil
		})
	}

	// Generate some failures
	for i := 0; i < 3; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	metrics := cb.GetMetrics()

	if metrics.Name != "metrics-test" {
		t.Errorf("expected name 'metrics-test', got %s", metrics.Name)
	}

	if metrics.TotalCalls != 13 {
		t.Errorf("expected 13 total calls, got %d", metrics.TotalCalls)
	}

	if metrics.TotalSuccesses != 10 {
		t.Errorf("expected 10 successes, got %d", metrics.TotalSuccesses)
	}

	if metrics.TotalFailures != 3 {
		t.Errorf("expected 3 failures, got %d", metrics.TotalFailures)
	}

	if metrics.State != "closed" {
		t.Errorf("expected state 'closed', got %s", metrics.State)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "reset-test",
		MaxFailures:  1,
		ResetTimeout: 1 * time.Hour, // Long timeout
	})

	// Trigger open state
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	if cb.GetState() != StateOpen {
		t.Error("circuit breaker should be open")
	}

	// Manual reset
	cb.Reset()

	if cb.GetState() != StateClosed {
		t.Error("circuit breaker should be closed after reset")
	}

	// Should be able to execute again
	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error after reset: %v", err)
	}
}
