package correlation

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TimeoutCoordinator manages timeout contexts for different operation levels
type TimeoutCoordinator struct {
	logger *zap.Logger
	tracer trace.Tracer
	config TimeoutConfig
}

// TimeoutConfig contains timeout durations for different operations
type TimeoutConfig struct {
	ProcessingTimeout time.Duration
	CorrelatorTimeout time.Duration
	StorageTimeout    time.Duration
	QueueTimeout      time.Duration
}

// TimeoutContext represents a timeout-aware context with cancellation
type TimeoutContext struct {
	Context context.Context
	Cancel  context.CancelFunc
	Level   TimeoutLevel
	Timeout time.Duration
}

// TimeoutLevel represents the different timeout levels
type TimeoutLevel int

const (
	ProcessingLevel TimeoutLevel = iota
	CorrelatorLevel
	StorageLevel
	QueueLevel
)

// String returns the string representation of the timeout level
func (t TimeoutLevel) String() string {
	switch t {
	case ProcessingLevel:
		return "processing"
	case CorrelatorLevel:
		return "correlator"
	case StorageLevel:
		return "storage"
	case QueueLevel:
		return "queue"
	default:
		return "unknown"
	}
}

// NewTimeoutCoordinator creates a new timeout coordinator
func NewTimeoutCoordinator(logger *zap.Logger, tracer trace.Tracer, config TimeoutConfig) *TimeoutCoordinator {
	return &TimeoutCoordinator{
		logger: logger,
		tracer: tracer,
		config: config,
	}
}

// DefaultTimeoutConfig returns a default timeout configuration
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		ProcessingTimeout: DefaultProcessingTimeout,
		CorrelatorTimeout: DefaultProcessingTimeout,
		StorageTimeout:    DefaultStorageTimeout,
		QueueTimeout:      DefaultProcessingTimeout,
	}
}

// CreateProcessingContext creates a timeout context for event processing
func (tc *TimeoutCoordinator) CreateProcessingContext(parentCtx context.Context) *TimeoutContext {
	ctx, cancel := context.WithTimeout(parentCtx, tc.config.ProcessingTimeout)
	return &TimeoutContext{
		Context: ctx,
		Cancel:  cancel,
		Level:   ProcessingLevel,
		Timeout: tc.config.ProcessingTimeout,
	}
}

// CreateCorrelatorContext creates a timeout context for correlator processing
func (tc *TimeoutCoordinator) CreateCorrelatorContext(parentCtx context.Context, correlatorName string) *TimeoutContext {
	ctx, span := tc.tracer.Start(parentCtx, fmt.Sprintf("correlation.%s.timeout", correlatorName))

	// Set timeout attributes
	span.SetAttributes(
		attribute.String("timeout.level", CorrelatorLevel.String()),
		attribute.Float64("timeout.duration_ms", tc.config.CorrelatorTimeout.Seconds()*1000),
		attribute.String("correlator", correlatorName),
	)

	timeoutCtx, cancel := context.WithTimeout(ctx, tc.config.CorrelatorTimeout)

	// Wrap cancel to end span
	wrappedCancel := func() {
		span.End()
		cancel()
	}

	return &TimeoutContext{
		Context: timeoutCtx,
		Cancel:  wrappedCancel,
		Level:   CorrelatorLevel,
		Timeout: tc.config.CorrelatorTimeout,
	}
}

// CreateStorageContext creates a timeout context for storage operations
func (tc *TimeoutCoordinator) CreateStorageContext(parentCtx context.Context) *TimeoutContext {
	ctx, cancel := context.WithTimeout(parentCtx, tc.config.StorageTimeout)
	return &TimeoutContext{
		Context: ctx,
		Cancel:  cancel,
		Level:   StorageLevel,
		Timeout: tc.config.StorageTimeout,
	}
}

// CreateQueueContext creates a timeout context for queue operations
func (tc *TimeoutCoordinator) CreateQueueContext(parentCtx context.Context) *TimeoutContext {
	ctx, cancel := context.WithTimeout(parentCtx, tc.config.QueueTimeout)
	return &TimeoutContext{
		Context: ctx,
		Cancel:  cancel,
		Level:   QueueLevel,
		Timeout: tc.config.QueueTimeout,
	}
}

// WaitWithTimeout executes an operation with timeout coordination
// It handles context cancellation from multiple sources gracefully
func (tc *TimeoutCoordinator) WaitWithTimeout(
	parentCtx context.Context,
	engineCtx context.Context,
	level TimeoutLevel,
	operation func() error,
) error {
	var timeoutCtx *TimeoutContext

	// Create appropriate timeout context based on level
	switch level {
	case ProcessingLevel:
		timeoutCtx = tc.CreateProcessingContext(parentCtx)
	case CorrelatorLevel:
		timeoutCtx = tc.CreateCorrelatorContext(parentCtx, "default")
	case StorageLevel:
		timeoutCtx = tc.CreateStorageContext(parentCtx)
	case QueueLevel:
		timeoutCtx = tc.CreateQueueContext(parentCtx)
	default:
		return fmt.Errorf("unsupported timeout level: %v", level)
	}
	defer timeoutCtx.Cancel()

	// Channel to receive operation result
	done := make(chan error, 1)

	// Execute operation in goroutine
	go func() {
		done <- operation()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		return err
	case <-timeoutCtx.Context.Done():
		tc.logger.Warn("Operation timed out",
			zap.String("timeout_level", level.String()),
			zap.Duration("timeout", timeoutCtx.Timeout),
		)
		return fmt.Errorf("%s timeout after %v", level.String(), timeoutCtx.Timeout)
	case <-parentCtx.Done():
		tc.logger.Debug("Operation cancelled by parent context",
			zap.String("timeout_level", level.String()),
		)
		return parentCtx.Err()
	case <-engineCtx.Done():
		tc.logger.Debug("Operation cancelled by engine shutdown",
			zap.String("timeout_level", level.String()),
		)
		return fmt.Errorf("engine is shutting down")
	}
}

// ExecuteWithRetry executes an operation with timeout and retry logic
func (tc *TimeoutCoordinator) ExecuteWithRetry(
	parentCtx context.Context,
	engineCtx context.Context,
	level TimeoutLevel,
	operation func() error,
	maxRetries int,
	retryDelay time.Duration,
) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry, respecting context cancellation
			select {
			case <-time.After(retryDelay):
			case <-parentCtx.Done():
				return parentCtx.Err()
			case <-engineCtx.Done():
				return fmt.Errorf("engine is shutting down")
			}

			tc.logger.Debug("Retrying operation",
				zap.String("timeout_level", level.String()),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Error(lastErr),
			)
		}

		err := tc.WaitWithTimeout(parentCtx, engineCtx, level, operation)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Don't retry on context cancellation
		if parentCtx.Err() != nil || engineCtx.Err() != nil {
			return err
		}
	}

	tc.logger.Error("Operation failed after all retries",
		zap.String("timeout_level", level.String()),
		zap.Int("max_retries", maxRetries),
		zap.Error(lastErr),
	)

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}

// IsTimeoutError checks if an error is a timeout error
func (tc *TimeoutCoordinator) IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context deadline exceeded
	if err == context.DeadlineExceeded {
		return true
	}

	// Check for timeout error messages
	errMsg := err.Error()
	return errMsg == "timeout" ||
		errMsg == "context deadline exceeded" ||
		errMsg == "processing timeout" ||
		errMsg == "correlator timeout" ||
		errMsg == "storage timeout" ||
		errMsg == "queue timeout"
}

// GetTimeoutConfig returns the current timeout configuration
func (tc *TimeoutCoordinator) GetTimeoutConfig() TimeoutConfig {
	return tc.config
}

// UpdateTimeoutConfig updates the timeout configuration
func (tc *TimeoutCoordinator) UpdateTimeoutConfig(config TimeoutConfig) {
	tc.config = config
	tc.logger.Info("Timeout configuration updated",
		zap.Duration("processing_timeout", config.ProcessingTimeout),
		zap.Duration("correlator_timeout", config.CorrelatorTimeout),
		zap.Duration("storage_timeout", config.StorageTimeout),
		zap.Duration("queue_timeout", config.QueueTimeout),
	)
}
