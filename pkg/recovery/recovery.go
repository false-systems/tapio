package recovery

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

// RecoveryHandler handles panics and provides graceful recovery
type RecoveryHandler struct {
	mu              sync.RWMutex
	panicCount      int64
	lastPanic       time.Time
	maxPanics       int
	timeWindow      time.Duration
	onPanic         func(PanicInfo)
	onRecovery      func(RecoveryInfo)
	shutdownTimeout time.Duration
	logger          Logger
}

// PanicInfo contains information about a panic
type PanicInfo struct {
	Error     interface{}
	Stack     string
	Timestamp time.Time
	Goroutine int
	Function  string
	File      string
	Line      int
}

// RecoveryInfo contains information about a recovery attempt
type RecoveryInfo struct {
	PanicInfo    PanicInfo
	Recovered    bool
	AttemptCount int
	RecoveryTime time.Duration
	Action       string
}

// Logger interface for recovery logging
type Logger interface {
	Error(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// DefaultLogger provides a basic logger implementation
type DefaultLogger struct{}

func (l *DefaultLogger) Error(msg string, fields ...interface{}) {
	log.Printf("[ERROR] "+msg, fields...)
}

func (l *DefaultLogger) Warn(msg string, fields ...interface{}) {
	log.Printf("[WARN] "+msg, fields...)
}

func (l *DefaultLogger) Info(msg string, fields ...interface{}) {
	log.Printf("[INFO] "+msg, fields...)
}

func (l *DefaultLogger) Debug(msg string, fields ...interface{}) {
	log.Printf("[DEBUG] "+msg, fields...)
}

// RecoveryConfig configures the recovery handler
type RecoveryConfig struct {
	MaxPanics       int           // Maximum panics allowed in time window
	TimeWindow      time.Duration // Time window for panic counting
	ShutdownTimeout time.Duration // Timeout for graceful shutdown
	OnPanic         func(PanicInfo)
	OnRecovery      func(RecoveryInfo)
	Logger          Logger
}

// DefaultRecoveryConfig returns sensible defaults
func DefaultRecoveryConfig() *RecoveryConfig {
	return &RecoveryConfig{
		MaxPanics:       5,
		TimeWindow:      5 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
		Logger:          &DefaultLogger{},
	}
}

// NewRecoveryHandler creates a new recovery handler
func NewRecoveryHandler(config *RecoveryConfig) *RecoveryHandler {
	if config == nil {
		config = DefaultRecoveryConfig()
	}

	return &RecoveryHandler{
		maxPanics:       config.MaxPanics,
		timeWindow:      config.TimeWindow,
		onPanic:         config.OnPanic,
		onRecovery:      config.OnRecovery,
		shutdownTimeout: config.ShutdownTimeout,
		logger:          config.Logger,
	}
}

// WithRecover wraps a function with panic recovery
func (rh *RecoveryHandler) WithRecover(name string, fn func() error) error {
	defer func() {
		if r := recover(); r != nil {
			panicInfo := rh.capturePanicInfo(r)
			rh.handlePanic(panicInfo, name, "function_execution")
		}
	}()

	return fn()
}

// WithRecoverContext wraps a function with panic recovery and context support
func (rh *RecoveryHandler) WithRecoverContext(ctx context.Context, name string, fn func(context.Context) error) error {
	defer func() {
		if r := recover(); r != nil {
			panicInfo := rh.capturePanicInfo(r)
			rh.handlePanic(panicInfo, name, "context_function")
		}
	}()

	return fn(ctx)
}

// WithRecoverGoroutine wraps a goroutine with panic recovery
func (rh *RecoveryHandler) WithRecoverGoroutine(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicInfo := rh.capturePanicInfo(r)
				rh.handlePanic(panicInfo, name, "goroutine")
			}
		}()

		fn()
	}()
}

// WithRecoverHTTPHandler wraps an HTTP handler with panic recovery
func (rh *RecoveryHandler) WithRecoverHTTPHandler(name string, fn func()) func() {
	return func() {
		defer func() {
			if r := recover(); r != nil {
				panicInfo := rh.capturePanicInfo(r)
				rh.handlePanic(panicInfo, name, "http_handler")
			}
		}()

		fn()
	}
}

// capturePanicInfo captures detailed information about a panic
func (rh *RecoveryHandler) capturePanicInfo(r interface{}) PanicInfo {
	stack := string(debug.Stack())

	// Parse stack trace to get function info
	pc, file, line, ok := runtime.Caller(4) // Skip recovery frames
	var function string
	if ok {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			function = fn.Name()
		}
	}

	return PanicInfo{
		Error:     r,
		Stack:     stack,
		Timestamp: time.Now(),
		Goroutine: runtime.NumGoroutine(),
		Function:  function,
		File:      file,
		Line:      line,
	}
}

// handlePanic processes a panic and decides on recovery action
func (rh *RecoveryHandler) handlePanic(panicInfo PanicInfo, name, action string) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	// Increment panic counter
	rh.panicCount++
	rh.lastPanic = panicInfo.Timestamp

	// Log the panic
	rh.logger.Error("Panic recovered",
		"component", name,
		"action", action,
		"error", panicInfo.Error,
		"function", panicInfo.Function,
		"file", fmt.Sprintf("%s:%d", panicInfo.File, panicInfo.Line),
		"goroutines", panicInfo.Goroutine,
	)

	if rh.logger != nil {
		rh.logger.Debug("Panic stack trace", "stack", panicInfo.Stack)
	}

	// Call panic callback if configured
	if rh.onPanic != nil {
		safeCall(func() { rh.onPanic(panicInfo) }, rh.logger)
	}

	// Check if we've exceeded panic threshold
	if rh.shouldShutdown() {
		rh.logger.Error("Too many panics detected, initiating shutdown",
			"panic_count", rh.panicCount,
			"time_window", rh.timeWindow,
			"max_panics", rh.maxPanics,
		)

		go rh.gracefulShutdown()
		return
	}

	// Create recovery info
	recoveryInfo := RecoveryInfo{
		PanicInfo:    panicInfo,
		Recovered:    true,
		AttemptCount: 1,
		Action:       action,
	}

	// Call recovery callback if configured
	if rh.onRecovery != nil {
		safeCall(func() { rh.onRecovery(recoveryInfo) }, rh.logger)
	}

	rh.logger.Info("Successfully recovered from panic",
		"component", name,
		"action", action,
	)
}

// shouldShutdown determines if the application should shutdown due to too many panics
func (rh *RecoveryHandler) shouldShutdown() bool {
	if rh.maxPanics <= 0 {
		return false // No limit
	}

	// Check if we're within the time window
	if time.Since(rh.lastPanic) > rh.timeWindow {
		rh.panicCount = 1 // Reset counter
		return false
	}

	return rh.panicCount >= int64(rh.maxPanics)
}

// gracefulShutdown initiates a graceful shutdown
func (rh *RecoveryHandler) gracefulShutdown() {
	rh.logger.Warn("Initiating graceful shutdown due to excessive panics")

	// Give components time to shutdown gracefully
	time.Sleep(rh.shutdownTimeout)

	rh.logger.Error("Forcing application exit due to excessive panics")
	// In a real application, you might want to call os.Exit(1) here
	// For this example, we'll just log the intent
}

// safeCall executes a function safely, recovering from any panics
func safeCall(fn func(), logger Logger) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Error("Panic in callback function", "error", r)
			}
		}
	}()

	fn()
}

// GetStats returns recovery statistics
func (rh *RecoveryHandler) GetStats() map[string]interface{} {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	return map[string]interface{}{
		"panic_count":     rh.panicCount,
		"last_panic":      rh.lastPanic,
		"max_panics":      rh.maxPanics,
		"time_window":     rh.timeWindow,
		"should_shutdown": rh.shouldShutdown(),
	}
}

// Reset resets the panic counter (useful for testing)
func (rh *RecoveryHandler) Reset() {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.panicCount = 0
	rh.lastPanic = time.Time{}
}

// CircuitBreaker provides circuit breaker functionality for error handling
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            CircuitState
	failureCount     int64
	successCount     int64
	lastFailureTime  time.Time
	lastSuccessTime  time.Time
	failureThreshold int64
	resetTimeout     time.Duration
	halfOpenMaxCalls int64
	halfOpenCalls    int64
	onStateChange    func(from, to CircuitState)
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitStateClosed:
		return "closed"
	case CircuitStateOpen:
		return "open"
	case CircuitStateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures a circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold int64         // Number of failures to trigger open state
	ResetTimeout     time.Duration // Time to wait before trying half-open
	HalfOpenMaxCalls int64         // Max calls allowed in half-open state
	OnStateChange    func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold: 5,
		ResetTimeout:     60 * time.Second,
		HalfOpenMaxCalls: 3,
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		state:            CircuitStateClosed,
		failureThreshold: config.FailureThreshold,
		resetTimeout:     config.ResetTimeout,
		halfOpenMaxCalls: config.HalfOpenMaxCalls,
		onStateChange:    config.OnStateChange,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return fmt.Errorf("circuit breaker is open")
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// allowRequest determines if a request should be allowed
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitStateClosed:
		return true
	case CircuitStateOpen:
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.changeState(CircuitStateHalfOpen)
			cb.halfOpenCalls = 0
			return true
		}
		return false
	case CircuitStateHalfOpen:
		if cb.halfOpenCalls < cb.halfOpenMaxCalls {
			cb.halfOpenCalls++
			return true
		}
		return false
	default:
		return false
	}
}

// recordSuccess records a successful execution
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++
	cb.lastSuccessTime = time.Now()

	if cb.state == CircuitStateHalfOpen {
		if cb.halfOpenCalls >= cb.halfOpenMaxCalls {
			cb.changeState(CircuitStateClosed)
			cb.failureCount = 0
		}
	}
}

// recordFailure records a failed execution
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitStateClosed && cb.failureCount >= cb.failureThreshold {
		cb.changeState(CircuitStateOpen)
	} else if cb.state == CircuitStateHalfOpen {
		cb.changeState(CircuitStateOpen)
	}
}

// changeState changes the circuit breaker state
func (cb *CircuitBreaker) changeState(newState CircuitState) {
	oldState := cb.state
	cb.state = newState

	if cb.onStateChange != nil {
		go safeCall(func() { cb.onStateChange(oldState, newState) }, nil)
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state.String(),
		"failure_count":     cb.failureCount,
		"success_count":     cb.successCount,
		"last_failure_time": cb.lastFailureTime,
		"last_success_time": cb.lastSuccessTime,
		"half_open_calls":   cb.halfOpenCalls,
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := cb.state
	cb.state = CircuitStateClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.halfOpenCalls = 0

	if cb.onStateChange != nil && oldState != CircuitStateClosed {
		go safeCall(func() { cb.onStateChange(oldState, CircuitStateClosed) }, nil)
	}
}

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Backoff      func(attempt int, delay time.Duration) time.Duration
	ShouldRetry  func(error) bool
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Backoff: func(attempt int, delay time.Duration) time.Duration {
			// Exponential backoff with jitter
			backoff := delay * time.Duration(1<<attempt)
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			return backoff
		},
		ShouldRetry: func(err error) bool {
			// Retry on most errors except context cancellation
			return err != nil && err != context.Canceled
		},
	}
}

// WithRetry executes a function with retry logic
func WithRetry(ctx context.Context, config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if we should retry
		if !config.ShouldRetry(err) {
			return err
		}

		// Calculate next delay
		if attempt < config.MaxAttempts-1 {
			delay = config.Backoff(attempt, delay)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, lastErr)
}
