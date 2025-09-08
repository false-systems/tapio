package base

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

var (
	// ErrShutdownTimeout is returned when graceful shutdown times out
	ErrShutdownTimeout = errors.New("shutdown timeout exceeded")
)

// LifecycleManager handles goroutine lifecycle and graceful shutdown
type LifecycleManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	stopCh chan struct{}
	doneCh chan struct{}
	logger *zap.Logger

	// Track running goroutines
	runningGoroutines atomic.Int32
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager(ctx context.Context, logger *zap.Logger) *LifecycleManager {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)

	return &LifecycleManager{
		ctx:    ctx,
		cancel: cancel,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		logger: logger,
	}
}

// Start launches a goroutine with proper lifecycle management
func (lm *LifecycleManager) Start(name string, fn func()) {
	lm.wg.Add(1)
	lm.runningGoroutines.Add(1)

	go func() {
		defer lm.wg.Done()
		defer lm.runningGoroutines.Add(-1)

		if lm.logger != nil {
			lm.logger.Debug("Starting goroutine", zap.String("name", name))
			defer lm.logger.Debug("Goroutine stopped", zap.String("name", name))
		}

		fn()
	}()
}

// Stop initiates graceful shutdown
func (lm *LifecycleManager) Stop(timeout time.Duration) error {
	if lm.logger != nil {
		lm.logger.Info("Initiating graceful shutdown",
			zap.Int32("running_goroutines", lm.runningGoroutines.Load()),
			zap.Duration("timeout", timeout))
	}

	// Signal stop
	close(lm.stopCh)

	// Cancel context
	lm.cancel()

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		lm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if lm.logger != nil {
			lm.logger.Info("Graceful shutdown completed")
		}
		close(lm.doneCh)
		return nil
	case <-time.After(timeout):
		if lm.logger != nil {
			lm.logger.Warn("Shutdown timeout exceeded",
				zap.Int32("still_running", lm.runningGoroutines.Load()))
		}
		return ErrShutdownTimeout
	}
}

// Context returns the lifecycle context
func (lm *LifecycleManager) Context() context.Context {
	return lm.ctx
}

// StopChannel returns the stop signal channel
func (lm *LifecycleManager) StopChannel() <-chan struct{} {
	return lm.stopCh
}

// DoneChannel returns the done signal channel
func (lm *LifecycleManager) DoneChannel() <-chan struct{} {
	return lm.doneCh
}

// IsShuttingDown checks if shutdown has been initiated
func (lm *LifecycleManager) IsShuttingDown() bool {
	select {
	case <-lm.stopCh:
		return true
	default:
		return false
	}
}

// GetRunningGoroutines returns the number of running goroutines
func (lm *LifecycleManager) GetRunningGoroutines() int32 {
	return lm.runningGoroutines.Load()
}
