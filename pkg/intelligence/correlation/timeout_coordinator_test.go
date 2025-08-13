package correlation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
)

func TestNewTimeoutCoordinator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := DefaultTimeoutConfig()

	coordinator := NewTimeoutCoordinator(logger, tracer, config)

	assert.NotNil(t, coordinator)
	assert.Equal(t, config, coordinator.GetTimeoutConfig())
}

func TestDefaultTimeoutConfig(t *testing.T) {
	config := DefaultTimeoutConfig()

	assert.Equal(t, DefaultProcessingTimeout, config.ProcessingTimeout)
	assert.Equal(t, DefaultProcessingTimeout, config.CorrelatorTimeout)
	assert.Equal(t, DefaultStorageTimeout, config.StorageTimeout)
	assert.Equal(t, DefaultProcessingTimeout, config.QueueTimeout)
}

func TestTimeoutLevel_String(t *testing.T) {
	tests := []struct {
		level    TimeoutLevel
		expected string
	}{
		{ProcessingLevel, "processing"},
		{CorrelatorLevel, "correlator"},
		{StorageLevel, "storage"},
		{QueueLevel, "queue"},
		{TimeoutLevel(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("level_%d", int(tt.level)), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestCreateProcessingContext(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	timeoutCtx := coordinator.CreateProcessingContext(parentCtx)
	defer timeoutCtx.Cancel()

	assert.NotNil(t, timeoutCtx.Context)
	assert.NotNil(t, timeoutCtx.Cancel)
	assert.Equal(t, ProcessingLevel, timeoutCtx.Level)
	assert.Equal(t, config.ProcessingTimeout, timeoutCtx.Timeout)

	// Verify timeout is set
	deadline, ok := timeoutCtx.Context.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

func TestCreateCorrelatorContext(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		CorrelatorTimeout: 200 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	timeoutCtx := coordinator.CreateCorrelatorContext(parentCtx, "test-correlator")
	defer timeoutCtx.Cancel()

	assert.NotNil(t, timeoutCtx.Context)
	assert.NotNil(t, timeoutCtx.Cancel)
	assert.Equal(t, CorrelatorLevel, timeoutCtx.Level)
	assert.Equal(t, config.CorrelatorTimeout, timeoutCtx.Timeout)

	// Verify timeout is set
	deadline, ok := timeoutCtx.Context.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

func TestCreateStorageContext(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		StorageTimeout: 300 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	timeoutCtx := coordinator.CreateStorageContext(parentCtx)
	defer timeoutCtx.Cancel()

	assert.NotNil(t, timeoutCtx.Context)
	assert.NotNil(t, timeoutCtx.Cancel)
	assert.Equal(t, StorageLevel, timeoutCtx.Level)
	assert.Equal(t, config.StorageTimeout, timeoutCtx.Timeout)

	// Verify timeout is set
	deadline, ok := timeoutCtx.Context.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

func TestCreateQueueContext(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		QueueTimeout: 50 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	timeoutCtx := coordinator.CreateQueueContext(parentCtx)
	defer timeoutCtx.Cancel()

	assert.NotNil(t, timeoutCtx.Context)
	assert.NotNil(t, timeoutCtx.Cancel)
	assert.Equal(t, QueueLevel, timeoutCtx.Level)
	assert.Equal(t, config.QueueTimeout, timeoutCtx.Timeout)

	// Verify timeout is set
	deadline, ok := timeoutCtx.Context.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

func TestWaitWithTimeout_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	operation := func() error {
		time.Sleep(10 * time.Millisecond) // Short operation
		return nil
	}

	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	assert.NoError(t, err)
}

func TestWaitWithTimeout_OperationError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	expectedErr := errors.New("operation failed")
	operation := func() error {
		return expectedErr
	}

	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	assert.Equal(t, expectedErr, err)
}

func TestWaitWithTimeout_Timeout(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 50 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	operation := func() error {
		time.Sleep(200 * time.Millisecond) // Long operation
		return nil
	}

	start := time.Now()
	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	duration := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "processing timeout")
	assert.True(t, duration < 100*time.Millisecond) // Should timeout quickly
}

func TestWaitWithTimeout_ParentContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx, cancel := context.WithCancel(context.Background())
	engineCtx := context.Background()

	operation := func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	// Cancel parent context after short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	assert.Equal(t, context.Canceled, err)
}

func TestWaitWithTimeout_EngineContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx, cancel := context.WithCancel(context.Background())

	operation := func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	// Cancel engine context after short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "engine is shutting down")
}

func TestWaitWithTimeout_UnsupportedLevel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := DefaultTimeoutConfig()

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	operation := func() error {
		return nil
	}

	err := coordinator.WaitWithTimeout(parentCtx, engineCtx, TimeoutLevel(999), operation)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported timeout level")
}

func TestExecuteWithRetry_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	attempts := 0
	operation := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}

	err := coordinator.ExecuteWithRetry(
		parentCtx, engineCtx, ProcessingLevel, operation,
		3, 10*time.Millisecond,
	)

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestExecuteWithRetry_MaxRetriesExceeded(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	attempts := 0
	operation := func() error {
		attempts++
		return errors.New("persistent failure")
	}

	err := coordinator.ExecuteWithRetry(
		parentCtx, engineCtx, ProcessingLevel, operation,
		2, 10*time.Millisecond,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "operation failed after 2 retries")
	assert.Equal(t, 3, attempts) // Initial attempt + 2 retries
}

func TestExecuteWithRetry_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx, cancel := context.WithCancel(context.Background())
	engineCtx := context.Background()

	attempts := 0
	operation := func() error {
		attempts++
		return errors.New("failure")
	}

	// Cancel context after first attempt completes but before retry
	go func() {
		time.Sleep(15 * time.Millisecond) // Before retry delay finishes
		cancel()
	}()

	err := coordinator.ExecuteWithRetry(
		parentCtx, engineCtx, ProcessingLevel, operation,
		5, 20*time.Millisecond,
	)

	assert.Equal(t, context.Canceled, err)
	// Can be 1 or 2 depending on timing, but should not reach max retries
	assert.LessOrEqual(t, attempts, 2)
}

func TestIsTimeoutError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := DefaultTimeoutConfig()

	coordinator := NewTimeoutCoordinator(logger, tracer, config)

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"timeout error", errors.New("timeout"), true},
		{"processing timeout", errors.New("processing timeout"), true},
		{"correlator timeout", errors.New("correlator timeout"), true},
		{"storage timeout", errors.New("storage timeout"), true},
		{"queue timeout", errors.New("queue timeout"), true},
		{"other error", errors.New("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coordinator.IsTimeoutError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateTimeoutConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	initialConfig := DefaultTimeoutConfig()

	coordinator := NewTimeoutCoordinator(logger, tracer, initialConfig)

	newConfig := TimeoutConfig{
		ProcessingTimeout: 1 * time.Second,
		CorrelatorTimeout: 2 * time.Second,
		StorageTimeout:    3 * time.Second,
		QueueTimeout:      4 * time.Second,
	}

	coordinator.UpdateTimeoutConfig(newConfig)

	assert.Equal(t, newConfig, coordinator.GetTimeoutConfig())
}

// Benchmark tests for timeout operations
func BenchmarkCreateProcessingContext(b *testing.B) {
	logger := zaptest.NewLogger(b)
	tracer := otel.Tracer("benchmark")
	config := DefaultTimeoutConfig()
	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		timeoutCtx := coordinator.CreateProcessingContext(parentCtx)
		timeoutCtx.Cancel()
	}
}

func BenchmarkWaitWithTimeout_FastOperation(b *testing.B) {
	logger := zaptest.NewLogger(b)
	tracer := otel.Tracer("benchmark")
	config := DefaultTimeoutConfig()
	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()
	engineCtx := context.Background()

	operation := func() error {
		return nil
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = coordinator.WaitWithTimeout(parentCtx, engineCtx, ProcessingLevel, operation)
	}
}

// Integration test with real timeout scenarios
func TestTimeoutCoordinator_Integration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("integration")
	config := TimeoutConfig{
		ProcessingTimeout: 100 * time.Millisecond,
		CorrelatorTimeout: 80 * time.Millisecond,
		StorageTimeout:    60 * time.Millisecond,
		QueueTimeout:      40 * time.Millisecond,
	}

	coordinator := NewTimeoutCoordinator(logger, tracer, config)

	t.Run("processing context hierarchy", func(t *testing.T) {
		parentCtx := context.Background()

		// Create processing context
		procCtx := coordinator.CreateProcessingContext(parentCtx)
		defer procCtx.Cancel()

		// Create correlator context from processing context
		corrCtx := coordinator.CreateCorrelatorContext(procCtx.Context, "test")
		defer corrCtx.Cancel()

		// Create storage context from correlator context
		storeCtx := coordinator.CreateStorageContext(corrCtx.Context)
		defer storeCtx.Cancel()

		// All contexts should be valid
		assert.NoError(t, procCtx.Context.Err())
		assert.NoError(t, corrCtx.Context.Err())
		assert.NoError(t, storeCtx.Context.Err())

		// Storage context should have shortest deadline
		procDeadline, _ := procCtx.Context.Deadline()
		corrDeadline, _ := corrCtx.Context.Deadline()
		storeDeadline, _ := storeCtx.Context.Deadline()

		assert.True(t, storeDeadline.Before(corrDeadline))
		assert.True(t, corrDeadline.Before(procDeadline))
	})

	t.Run("timeout cascade", func(t *testing.T) {
		parentCtx := context.Background()
		engineCtx := context.Background()

		// Test that shorter timeouts fire first
		levels := []TimeoutLevel{QueueLevel, StorageLevel, CorrelatorLevel, ProcessingLevel}

		for _, level := range levels {
			operation := func() error {
				time.Sleep(200 * time.Millisecond) // Longer than any timeout
				return nil
			}

			err := coordinator.WaitWithTimeout(parentCtx, engineCtx, level, operation)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "timeout")
		}
	})
}

func TestTimeoutContext_Cancel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	config := DefaultTimeoutConfig()

	coordinator := NewTimeoutCoordinator(logger, tracer, config)
	parentCtx := context.Background()

	timeoutCtx := coordinator.CreateProcessingContext(parentCtx)

	// Context should be valid initially
	assert.NoError(t, timeoutCtx.Context.Err())

	// Cancel the context
	timeoutCtx.Cancel()

	// Context should be cancelled
	assert.Error(t, timeoutCtx.Context.Err())
}
