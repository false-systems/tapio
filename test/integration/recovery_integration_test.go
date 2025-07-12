package integration

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/recovery"
)

func TestRecoveryIntegrationFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test complete recovery workflow
	config := &recovery.RecoveryConfig{
		MaxPanics:       3,
		TimeWindow:      1 * time.Second,
		ShutdownTimeout: 100 * time.Millisecond,
		Logger:          &TestLogger{t: t},
	}

	handler := recovery.NewRecoveryHandler(config)

	t.Run("normal operation without panics", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		// Run 10 concurrent operations
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				err := handler.WithRecover("test-operation", func() error {
					time.Sleep(10 * time.Millisecond)
					return nil
				})
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Should have no errors
		var errorList []error
		for err := range errors {
			errorList = append(errorList, err)
		}

		if len(errorList) > 0 {
			t.Errorf("expected no errors in normal operation, got %d errors", len(errorList))
		}

		stats := handler.GetStats()
		if stats["panic_count"].(int64) != 0 {
			t.Errorf("expected 0 panics, got %v", stats["panic_count"])
		}
	})

	t.Run("recovery from single panic", func(t *testing.T) {
		var panicCaught bool
		var recoveryInfo recovery.RecoveryInfo

		config.OnPanic = func(info recovery.PanicInfo) {
			panicCaught = true
		}
		config.OnRecovery = func(info recovery.RecoveryInfo) {
			recoveryInfo = info
		}

		handler := recovery.NewRecoveryHandler(config)

		// This should trigger a panic but recover
		err := handler.WithRecover("panic-test", func() error {
			panic("test panic for recovery")
		})

		// Give callbacks time to execute
		time.Sleep(50 * time.Millisecond)

		if !panicCaught {
			t.Error("expected panic to be caught")
		}

		if !recoveryInfo.Recovered {
			t.Error("expected recovery to be successful")
		}

		if recoveryInfo.PanicInfo.Error != "test panic for recovery" {
			t.Errorf("expected panic message 'test panic for recovery', got %v", recoveryInfo.PanicInfo.Error)
		}

		stats := handler.GetStats()
		if stats["panic_count"].(int64) != 1 {
			t.Errorf("expected 1 panic, got %v", stats["panic_count"])
		}
	})

	t.Run("shutdown threshold protection", func(t *testing.T) {
		var shutdownTriggered bool

		config := &recovery.RecoveryConfig{
			MaxPanics:       2,
			TimeWindow:      500 * time.Millisecond,
			ShutdownTimeout: 50 * time.Millisecond,
			Logger:          &TestLogger{t: t},
		}

		handler := recovery.NewRecoveryHandler(config)

		// Trigger multiple panics quickly
		for i := 0; i < 3; i++ {
			handler.WithRecover("panic-test", func() error {
				panic("repeated panic")
			})
		}

		time.Sleep(100 * time.Millisecond)

		stats := handler.GetStats()
		if !stats["should_shutdown"].(bool) {
			t.Error("expected shutdown to be triggered after exceeding panic threshold")
		}
	})
}

func TestCircuitBreakerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	config := &recovery.CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     100 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	}

	cb := recovery.NewCircuitBreaker(config)

	t.Run("normal operation flow", func(t *testing.T) {
		// Should start in closed state
		if cb.GetState() != recovery.CircuitStateClosed {
			t.Errorf("expected initial state to be closed, got %v", cb.GetState())
		}

		// Successful operations should keep it closed
		for i := 0; i < 5; i++ {
			err := cb.Execute(func() error {
				return nil
			})
			if err != nil {
				t.Errorf("unexpected error in successful operation: %v", err)
			}
		}

		if cb.GetState() != recovery.CircuitStateClosed {
			t.Errorf("expected state to remain closed after successful operations, got %v", cb.GetState())
		}
	})

	t.Run("failure threshold and recovery", func(t *testing.T) {
		// Reset circuit breaker
		cb.Reset()

		// Trigger failures to open circuit
		for i := 0; i < int(config.FailureThreshold); i++ {
			cb.Execute(func() error {
				return errors.New("simulated failure")
			})
		}

		// Should be open now
		if cb.GetState() != recovery.CircuitStateOpen {
			t.Errorf("expected state to be open after failures, got %v", cb.GetState())
		}

		// Execution should be rejected
		err := cb.Execute(func() error {
			return nil
		})
		if err == nil {
			t.Error("expected execution to be rejected when circuit is open")
		}

		// Wait for reset timeout
		time.Sleep(150 * time.Millisecond)

		// Should allow execution and move to half-open
		err = cb.Execute(func() error {
			return nil
		})
		if err != nil {
			t.Errorf("expected execution to be allowed after timeout, got: %v", err)
		}

		if cb.GetState() != recovery.CircuitStateHalfOpen {
			t.Errorf("expected state to be half-open, got %v", cb.GetState())
		}

		// Another successful execution should close it
		err = cb.Execute(func() error {
			return nil
		})
		if err != nil {
			t.Errorf("expected execution to succeed in half-open state, got: %v", err)
		}

		if cb.GetState() != recovery.CircuitStateClosed {
			t.Errorf("expected state to be closed after successful half-open executions, got %v", cb.GetState())
		}
	})

	t.Run("state change notifications", func(t *testing.T) {
		stateChanges := make(chan string, 10)

		config := &recovery.CircuitBreakerConfig{
			FailureThreshold: 2,
			ResetTimeout:     50 * time.Millisecond,
			HalfOpenMaxCalls: 1,
			OnStateChange: func(from, to recovery.CircuitState) {
				stateChanges <- from.String() + "->" + to.String()
			},
		}

		cb := recovery.NewCircuitBreaker(config)

		// Trigger state change: closed -> open
		cb.Execute(func() error { return errors.New("failure 1") })
		cb.Execute(func() error { return errors.New("failure 2") })

		// Wait for reset and trigger state change: open -> half-open -> closed
		time.Sleep(60 * time.Millisecond)
		cb.Execute(func() error { return nil })

		// Give callbacks time to execute
		time.Sleep(50 * time.Millisecond)
		close(stateChanges)

		// Check state changes
		changes := []string{}
		for change := range stateChanges {
			changes = append(changes, change)
		}

		if len(changes) < 2 {
			t.Errorf("expected at least 2 state changes, got %d: %v", len(changes), changes)
		}

		// Should include closed->open transition
		foundClosedToOpen := false
		for _, change := range changes {
			if change == "closed->open" {
				foundClosedToOpen = true
				break
			}
		}
		if !foundClosedToOpen {
			t.Errorf("expected 'closed->open' transition, got changes: %v", changes)
		}
	})
}

func TestRetryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	t.Run("successful retry after transient failures", func(t *testing.T) {
		config := &recovery.RetryConfig{
			MaxAttempts:  3,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Backoff: func(attempt int, delay time.Duration) time.Duration {
				return delay * 2
			},
			ShouldRetry: func(err error) bool {
				return err.Error() == "transient"
			},
		}

		attempts := 0
		start := time.Now()

		err := recovery.WithRetry(ctx, config, func() error {
			attempts++
			if attempts < 3 {
				return errors.New("transient")
			}
			return nil
		})

		duration := time.Since(start)

		if err != nil {
			t.Errorf("expected retry to succeed, got error: %v", err)
		}

		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}

		// Should have taken at least the delay time
		expectedMinDuration := 10*time.Millisecond + 20*time.Millisecond // first retry delay + second retry delay
		if duration < expectedMinDuration {
			t.Errorf("retry completed too quickly: %v (expected at least %v)", duration, expectedMinDuration)
		}
	})

	t.Run("context cancellation during retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		config := &recovery.RetryConfig{
			MaxAttempts:  5,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     1 * time.Second,
			Backoff: func(attempt int, delay time.Duration) time.Duration {
				return delay
			},
			ShouldRetry: func(err error) bool {
				return true
			},
		}

		attempts := 0

		// Cancel context after short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		err := recovery.WithRetry(ctx, config, func() error {
			attempts++
			return errors.New("persistent failure")
		})
		duration := time.Since(start)

		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}

		if attempts > 2 {
			t.Errorf("expected few attempts due to cancellation, got %d", attempts)
		}

		// Should have been cancelled relatively quickly
		if duration > 200*time.Millisecond {
			t.Errorf("retry took too long after cancellation: %v", duration)
		}
	})

	t.Run("exponential backoff behavior", func(t *testing.T) {
		config := &recovery.RetryConfig{
			MaxAttempts:  4,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Backoff: func(attempt int, delay time.Duration) time.Duration {
				return delay * 2
			},
			ShouldRetry: func(err error) bool {
				return true
			},
		}

		attempts := 0
		start := time.Now()

		err := recovery.WithRetry(ctx, config, func() error {
			attempts++
			return errors.New("always fails")
		})

		duration := time.Since(start)

		if err == nil {
			t.Error("expected retry to eventually fail")
		}

		if attempts != 4 {
			t.Errorf("expected 4 attempts, got %d", attempts)
		}

		// Expected delays: 10ms, 20ms, 40ms (capped at 100ms)
		expectedMinDuration := 70 * time.Millisecond
		if duration < expectedMinDuration {
			t.Errorf("retry completed too quickly: %v (expected at least %v)", duration, expectedMinDuration)
		}
	})
}

func TestRecoveryPerformanceAndStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	config := recovery.DefaultRecoveryConfig()
	config.Logger = &TestLogger{t: t}
	handler := recovery.NewRecoveryHandler(config)

	t.Run("high concurrency recovery", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		operationsPerGoroutine := 10

		start := time.Now()

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				for j := 0; j < operationsPerGoroutine; j++ {
					handler.WithRecover("concurrent-test", func() error {
						// Occasionally panic to test recovery
						if (id+j)%50 == 0 {
							panic("scheduled panic for testing")
						}

						// Do some work
						time.Sleep(time.Millisecond)
						return nil
					})
				}
			}(i)
		}

		wg.Wait()
		duration := time.Since(start)

		t.Logf("Completed %d operations across %d goroutines in %v",
			numGoroutines*operationsPerGoroutine, numGoroutines, duration)

		stats := handler.GetStats()
		expectedPanics := int64(numGoroutines * operationsPerGoroutine / 50)
		actualPanics := stats["panic_count"].(int64)

		if actualPanics < expectedPanics-2 || actualPanics > expectedPanics+2 {
			t.Errorf("expected approximately %d panics, got %d", expectedPanics, actualPanics)
		}
	})

	t.Run("memory usage under load", func(t *testing.T) {
		// Force GC before measurement
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)
		initialMem := m1.Alloc

		// Create many recovery handlers and use them
		handlers := make([]*recovery.RecoveryHandler, 100)
		for i := range handlers {
			handlers[i] = recovery.NewRecoveryHandler(config)
		}

		// Use all handlers
		for i := 0; i < 1000; i++ {
			handler := handlers[i%len(handlers)]
			handler.WithRecover("memory-test", func() error {
				if i%100 == 0 {
					panic("memory test panic")
				}
				return nil
			})
		}

		// Force GC after operations
		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)
		finalMem := m2.Alloc

		memGrowth := finalMem - initialMem
		maxAllowedGrowth := uint64(50 * 1024 * 1024) // 50MB

		if memGrowth > maxAllowedGrowth {
			t.Errorf("memory usage grew by %d bytes (>%d allowed)", memGrowth, maxAllowedGrowth)
		}

		t.Logf("Memory growth: %d bytes", memGrowth)
	})
}

func TestCircuitBreakerStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test circuit breaker under various load conditions
	t.Run("rapid state transitions", func(t *testing.T) {
		config := &recovery.CircuitBreakerConfig{
			FailureThreshold: 5,
			ResetTimeout:     50 * time.Millisecond,
			HalfOpenMaxCalls: 3,
		}

		cb := recovery.NewCircuitBreaker(config)

		// Rapidly alternate between success and failure
		for cycle := 0; cycle < 10; cycle++ {
			// Cause failures to open circuit
			for i := 0; i < 6; i++ {
				cb.Execute(func() error {
					return errors.New("failure")
				})
			}

			// Wait for reset timeout
			time.Sleep(60 * time.Millisecond)

			// Execute successful operations to close circuit
			for i := 0; i < 3; i++ {
				err := cb.Execute(func() error {
					return nil
				})
				if err != nil && cycle > 0 { // Allow first cycle to potentially fail
					t.Errorf("unexpected error in success phase of cycle %d: %v", cycle, err)
				}
			}
		}

		// Should end up in closed state
		if cb.GetState() != recovery.CircuitStateClosed {
			t.Errorf("expected final state to be closed, got %v", cb.GetState())
		}
	})

	t.Run("concurrent circuit breaker access", func(t *testing.T) {
		config := recovery.DefaultCircuitBreakerConfig()
		cb := recovery.NewCircuitBreaker(config)

		var wg sync.WaitGroup
		numGoroutines := 50
		operationsPerGoroutine := 20

		start := time.Now()

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				for j := 0; j < operationsPerGoroutine; j++ {
					// Mix of success and failure
					cb.Execute(func() error {
						if (id+j)%7 == 0 {
							return errors.New("concurrent failure")
						}
						return nil
					})
				}
			}(i)
		}

		wg.Wait()
		duration := time.Since(start)

		t.Logf("Completed %d concurrent circuit breaker operations in %v",
			numGoroutines*operationsPerGoroutine, duration)

		// Circuit breaker should still be functional
		stats := cb.GetStats()
		if stats["failure_count"].(int64) == 0 {
			t.Error("expected some failures in concurrent test")
		}
		if stats["success_count"].(int64) == 0 {
			t.Error("expected some successes in concurrent test")
		}
	})
}

// TestLogger implements the recovery.Logger interface for testing
type TestLogger struct {
	t        *testing.T
	mu       sync.Mutex
	messages []string
}

func (l *TestLogger) Error(msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "ERROR: "+msg)
	if l.t != nil {
		l.t.Logf("ERROR: "+msg, fields...)
	}
}

func (l *TestLogger) Warn(msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "WARN: "+msg)
	if l.t != nil {
		l.t.Logf("WARN: "+msg, fields...)
	}
}

func (l *TestLogger) Info(msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "INFO: "+msg)
	if l.t != nil {
		l.t.Logf("INFO: "+msg, fields...)
	}
}

func (l *TestLogger) Debug(msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "DEBUG: "+msg)
	if l.t != nil {
		l.t.Logf("DEBUG: "+msg, fields...)
	}
}

func (l *TestLogger) GetMessages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.messages))
	copy(result, l.messages)
	return result
}
