package resilience

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTimeoutManager_Execute(t *testing.T) {
	tests := []struct {
		name          string
		config        TimeoutConfig
		operation     func(ctx context.Context) error
		expectedError error
	}{
		{
			name: "successful_operation",
			config: TimeoutConfig{
				Timeout:    1 * time.Second,
				MaxRetries: 0,
			},
			operation: func(ctx context.Context) error {
				return nil
			},
			expectedError: nil,
		},
		{
			name: "timeout_exceeded",
			config: TimeoutConfig{
				Timeout:    100 * time.Millisecond,
				MaxRetries: 0,
			},
			operation: func(ctx context.Context) error {
				time.Sleep(200 * time.Millisecond)
				return nil
			},
			expectedError: ErrTimeout,
		},
		{
			name: "context_cancelled",
			config: TimeoutConfig{
				Timeout:    1 * time.Second,
				MaxRetries: 0,
			},
			operation: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			expectedError: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := NewTimeoutManager(tt.config)

			ctx := context.Background()
			if tt.expectedError == context.Canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			}

			err := tm.Execute(ctx, tt.name, tt.operation)

			if tt.expectedError != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.expectedError)
				} else if !errors.Is(err, tt.expectedError) {
					t.Errorf("expected error %v, got %v", tt.expectedError, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTimeoutManager_Retry(t *testing.T) {
	attemptCount := 0
	tm := NewTimeoutManager(TimeoutConfig{
		Timeout: 100 * time.Millisecond,
		RetryStrategy: &ExponentialBackoff{
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Multiplier:   2.0,
		},
		MaxRetries: 3,
	})

	err := tm.Execute(context.Background(), "retry-test", func(ctx context.Context) error {
		attemptCount++
		if attemptCount < 3 {
			return errors.New("transient error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if attemptCount != 3 {
		t.Errorf("expected 3 attempts, got %d", attemptCount)
	}

	metrics := tm.GetMetrics()
	if metrics.TotalRetries != 2 { // 3 attempts = 2 retries
		t.Errorf("expected 2 retries, got %d", metrics.TotalRetries)
	}
}

func TestExponentialBackoff(t *testing.T) {
	backoff := &ExponentialBackoff{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}

	tests := []struct {
		attempt     int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{0, 10 * time.Millisecond, 10 * time.Millisecond},
		{1, 20 * time.Millisecond, 20 * time.Millisecond},
		{2, 40 * time.Millisecond, 40 * time.Millisecond},
		{3, 80 * time.Millisecond, 80 * time.Millisecond},
		{4, 100 * time.Millisecond, 100 * time.Millisecond}, // Capped at max
		{5, 100 * time.Millisecond, 100 * time.Millisecond}, // Capped at max
	}

	for _, tt := range tests {
		delay := backoff.NextDelay(tt.attempt)
		if delay < tt.expectedMin || delay > tt.expectedMax {
			t.Errorf("attempt %d: expected delay between %v and %v, got %v",
				tt.attempt, tt.expectedMin, tt.expectedMax, delay)
		}
	}
}

func TestExponentialBackoff_WithJitter(t *testing.T) {
	backoff := &ExponentialBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}

	// Test that jitter produces different values
	delays := make(map[time.Duration]bool)
	for i := 0; i < 10; i++ {
		delay := backoff.NextDelay(2)
		delays[delay] = true
	}

	if len(delays) < 2 {
		t.Error("jitter should produce different delay values")
	}
}

func TestBoundedExecutor(t *testing.T) {
	be := NewBoundedExecutor(2, 200*time.Millisecond)

	var (
		running    int
		maxRunning int
		mu         sync.Mutex
	)

	// Start multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := be.Execute(context.Background(), func() error {
				mu.Lock()
				running++
				if running > maxRunning {
					maxRunning = running
				}
				mu.Unlock()

				time.Sleep(50 * time.Millisecond)

				mu.Lock()
				running--
				mu.Unlock()

				return nil
			})

			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	if maxRunning > 2 {
		t.Errorf("expected max concurrent executions of 2, got %d", maxRunning)
	}

	metrics := be.GetMetrics()
	if metrics.TotalExecutions != 5 {
		t.Errorf("expected 5 total executions, got %d", metrics.TotalExecutions)
	}
}

func TestBoundedExecutor_Timeout(t *testing.T) {
	be := NewBoundedExecutor(1, 50*time.Millisecond)

	// Block the executor
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		be.Execute(context.Background(), func() error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
	}()

	// Give the first goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// This should timeout waiting for semaphore
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := be.Execute(ctx, func() error {
		return nil
	})

	if !errors.Is(err, ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected timeout error, got %v", err)
	}

	wg.Wait()

	metrics := be.GetMetrics()
	// Either timeout or rejection is acceptable
	if metrics.RejectedExecutions == 0 && metrics.TotalExecutions < 2 {
		t.Errorf("expected either rejection or timeout, got %+v", metrics)
	}
}

func TestTimeoutManager_WithCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test-cb",
		MaxFailures:  2,
		ResetTimeout: 100 * time.Millisecond,
	})

	tm := NewTimeoutManager(TimeoutConfig{
		Timeout:        100 * time.Millisecond,
		MaxRetries:     0,
		CircuitBreaker: cb,
	})

	// Verify circuit breaker is integrated
	initialState := cb.GetState()
	if initialState != StateClosed {
		t.Errorf("expected initial state to be closed, got %s", initialState)
	}

	// Execute a successful operation to confirm integration
	err := tm.Execute(context.Background(), "test", func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error for successful operation: %v", err)
	}

	// Verify metrics were updated
	metrics := cb.GetMetrics()
	if metrics.TotalCalls == 0 {
		t.Error("circuit breaker was not used by timeout manager")
	}
}
