package recovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRecoveryHandler_WithRecover(t *testing.T) {
	config := DefaultRecoveryConfig()
	handler := NewRecoveryHandler(config)

	// Test normal execution
	err := handler.WithRecover("test", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Test error return
	expectedErr := errors.New("test error")
	err = handler.WithRecover("test", func() error {
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	// Test panic recovery
	panicCaught := false
	config.OnPanic = func(info PanicInfo) {
		panicCaught = true
	}
	handler = NewRecoveryHandler(config)

	err = handler.WithRecover("test", func() error {
		panic("test panic")
	})

	// Give some time for the panic handler to execute
	time.Sleep(10 * time.Millisecond)

	if !panicCaught {
		t.Error("expected panic to be caught")
	}
}

func TestRecoveryHandler_WithRecoverGoroutine(t *testing.T) {
	config := DefaultRecoveryConfig()
	panicCaught := false
	var wg sync.WaitGroup

	config.OnPanic = func(info PanicInfo) {
		panicCaught = true
		wg.Done()
	}

	handler := NewRecoveryHandler(config)

	wg.Add(1)
	handler.WithRecoverGoroutine("test-goroutine", func() {
		panic("goroutine panic")
	})

	// Wait for panic to be handled
	wg.Wait()

	if !panicCaught {
		t.Error("expected goroutine panic to be caught")
	}
}

func TestRecoveryHandler_PanicThreshold(t *testing.T) {
	config := &RecoveryConfig{
		MaxPanics:  2,
		TimeWindow: 1 * time.Second,
		Logger:     &TestLogger{},
	}

	handler := NewRecoveryHandler(config)

	// First panic should be recovered
	handler.WithRecover("test", func() error {
		panic("first panic")
	})

	stats := handler.GetStats()
	if stats["panic_count"].(int64) != 1 {
		t.Errorf("expected panic count 1, got %v", stats["panic_count"])
	}

	// Second panic should still be recovered but trigger shutdown check
	handler.WithRecover("test", func() error {
		panic("second panic")
	})

	stats = handler.GetStats()
	if stats["panic_count"].(int64) != 2 {
		t.Errorf("expected panic count 2, got %v", stats["panic_count"])
	}

	if !stats["should_shutdown"].(bool) {
		t.Error("expected should_shutdown to be true after exceeding threshold")
	}
}

func TestRecoveryHandler_PanicTimeWindow(t *testing.T) {
	config := &RecoveryConfig{
		MaxPanics:  2,
		TimeWindow: 50 * time.Millisecond,
		Logger:     &TestLogger{},
	}

	handler := NewRecoveryHandler(config)

	// First panic
	handler.WithRecover("test", func() error {
		panic("first panic")
	})

	// Wait for time window to expire
	time.Sleep(60 * time.Millisecond)

	// Second panic should reset the counter
	handler.WithRecover("test", func() error {
		panic("second panic")
	})

	stats := handler.GetStats()
	if stats["panic_count"].(int64) != 1 {
		t.Errorf("expected panic count to reset to 1, got %v", stats["panic_count"])
	}
}

func TestCircuitBreaker_BasicFlow(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker(config)

	// Initially closed
	if cb.GetState() != CircuitStateClosed {
		t.Errorf("expected initial state to be closed, got %v", cb.GetState())
	}

	// Successful execution
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Failed executions to trigger open state
	for i := 0; i < int(config.FailureThreshold); i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	// Should be open now
	if cb.GetState() != CircuitStateOpen {
		t.Errorf("expected state to be open, got %v", cb.GetState())
	}

	// Execution should be rejected
	err = cb.Execute(func() error {
		return nil
	})
	if err == nil {
		t.Error("expected execution to be rejected when circuit is open")
	}
}

func TestCircuitBreaker_HalfOpenFlow(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     10 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	}
	cb := NewCircuitBreaker(config)

	// Trigger open state
	for i := 0; i < int(config.FailureThreshold); i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	// Wait for reset timeout
	time.Sleep(20 * time.Millisecond)

	// First execution should be allowed (moves to half-open)
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected execution to be allowed after timeout, got %v", err)
	}

	if cb.GetState() != CircuitStateHalfOpen {
		t.Errorf("expected state to be half-open, got %v", cb.GetState())
	}

	// Another successful execution should close the circuit
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected execution to succeed, got %v", err)
	}

	if cb.GetState() != CircuitStateClosed {
		t.Errorf("expected state to be closed, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	stateChanges := []string{}
	config := &CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     10 * time.Millisecond,
		HalfOpenMaxCalls: 1,
		OnStateChange: func(from, to CircuitState) {
			stateChanges = append(stateChanges, from.String()+"->"+to.String())
		},
	}

	cb := NewCircuitBreaker(config)

	// Trigger state changes
	cb.Execute(func() error {
		return errors.New("failure")
	})

	// Wait for callback to execute
	time.Sleep(50 * time.Millisecond)

	// Should have closed->open transition
	if len(stateChanges) == 0 {
		t.Error("expected state change callback to be called")
	}

	if stateChanges[0] != "closed->open" {
		t.Errorf("expected 'closed->open' transition, got %v", stateChanges[0])
	}
}

func TestWithRetry_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	err := WithRetry(ctx, config, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary failure")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected retry to succeed, got %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestWithRetry_MaxAttemptsExceeded(t *testing.T) {
	ctx := context.Background()
	config := &RetryConfig{
		MaxAttempts:  2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Backoff: func(attempt int, delay time.Duration) time.Duration {
			return delay
		},
		ShouldRetry: func(err error) bool {
			return true
		},
	}

	attempts := 0
	err := WithRetry(ctx, config, func() error {
		attempts++
		return errors.New("persistent failure")
	})

	if err == nil {
		t.Error("expected retry to fail after max attempts")
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := DefaultRetryConfig()

	attempts := 0
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := WithRetry(ctx, config, func() error {
		attempts++
		time.Sleep(20 * time.Millisecond) // Longer than cancellation delay
		return errors.New("failure")
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt before cancellation, got %d", attempts)
	}
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	config := &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Backoff: func(attempt int, delay time.Duration) time.Duration {
			return delay
		},
		ShouldRetry: func(err error) bool {
			return err.Error() != "non-retryable"
		},
	}

	attempts := 0
	err := WithRetry(ctx, config, func() error {
		attempts++
		return errors.New("non-retryable")
	})

	if err == nil {
		t.Error("expected non-retryable error to fail immediately")
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	// Execute some operations
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return errors.New("error") })

	stats := cb.GetStats()

	if stats["success_count"].(int64) != 1 {
		t.Errorf("expected success_count 1, got %v", stats["success_count"])
	}

	if stats["failure_count"].(int64) != 1 {
		t.Errorf("expected failure_count 1, got %v", stats["failure_count"])
	}

	if stats["state"].(string) != "closed" {
		t.Errorf("expected state 'closed', got %v", stats["state"])
	}
}

func TestRecoveryHandlerStats(t *testing.T) {
	handler := NewRecoveryHandler(DefaultRecoveryConfig())

	// Trigger a panic
	handler.WithRecover("test", func() error {
		panic("test panic")
	})

	stats := handler.GetStats()

	if stats["panic_count"].(int64) != 1 {
		t.Errorf("expected panic_count 1, got %v", stats["panic_count"])
	}

	if stats["max_panics"].(int) != 5 {
		t.Errorf("expected max_panics 5, got %v", stats["max_panics"])
	}
}

// TestLogger is a test implementation of the Logger interface
type TestLogger struct {
	messages []string
}

func (l *TestLogger) Error(msg string, fields ...interface{}) {
	l.messages = append(l.messages, "ERROR: "+msg)
}

func (l *TestLogger) Warn(msg string, fields ...interface{}) {
	l.messages = append(l.messages, "WARN: "+msg)
}

func (l *TestLogger) Info(msg string, fields ...interface{}) {
	l.messages = append(l.messages, "INFO: "+msg)
}

func (l *TestLogger) Debug(msg string, fields ...interface{}) {
	l.messages = append(l.messages, "DEBUG: "+msg)
}

// Benchmark tests
func BenchmarkRecoveryHandler_WithRecover(b *testing.B) {
	handler := NewRecoveryHandler(DefaultRecoveryConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.WithRecover("benchmark", func() error {
			return nil
		})
	}
}

func BenchmarkCircuitBreaker_Execute(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Execute(func() error {
			return nil
		})
	}
}

func BenchmarkWithRetry(b *testing.B) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WithRetry(ctx, config, func() error {
			return nil
		})
	}
}
