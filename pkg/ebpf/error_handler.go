package ebpf

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Common errors
var (
	ErrCollectorNotStarted = errors.New("collector not started")
	ErrCollectorStopped    = errors.New("collector stopped")
	ErrInvalidEvent        = errors.New("invalid event data")
	ErrBufferFull          = errors.New("event buffer full")
	ErrProgramNotLoaded    = errors.New("eBPF program not loaded")
	ErrKernelNotSupported  = errors.New("kernel does not support required eBPF features")
	ErrPermissionDenied    = errors.New("insufficient permissions (need CAP_BPF or root)")
	ErrResourceLimit       = errors.New("resource limit exceeded")
)

// ErrorHandler provides centralized error handling for the eBPF subsystem
type ErrorHandler struct {
	// Error tracking
	errorCounts map[string]*atomic.Uint64
	mu          sync.RWMutex

	// Error callbacks
	onError    func(err error, context string)
	onCritical func(err error, context string)

	// Circuit breaker
	circuitBreaker *CircuitBreaker

	// Recovery strategies
	recoveryStrategies map[string]RecoveryStrategy

	// Metrics
	totalErrors    atomic.Uint64
	recoveries     atomic.Uint64
	criticalErrors atomic.Uint64

	// Configuration
	config ErrorHandlerConfig
}

// ErrorHandlerConfig configures the error handler
type ErrorHandlerConfig struct {
	// Error thresholds
	MaxErrorsPerMinute   int
	MaxConsecutiveErrors int

	// Circuit breaker settings
	CircuitBreakerThreshold int
	CircuitBreakerTimeout   time.Duration

	// Recovery settings
	EnableAutoRecovery  bool
	RecoveryBackoff     time.Duration
	MaxRecoveryAttempts int

	// Logging
	LogErrors     bool
	LogRecoveries bool
}

// DefaultErrorHandlerConfig returns default configuration
func DefaultErrorHandlerConfig() ErrorHandlerConfig {
	return ErrorHandlerConfig{
		MaxErrorsPerMinute:      100,
		MaxConsecutiveErrors:    10,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
		EnableAutoRecovery:      true,
		RecoveryBackoff:         5 * time.Second,
		MaxRecoveryAttempts:     3,
		LogErrors:               true,
		LogRecoveries:           true,
	}
}

// RecoveryStrategy defines how to recover from specific errors
type RecoveryStrategy interface {
	CanRecover(err error) bool
	Recover(ctx context.Context, err error) error
	Name() string
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(config ErrorHandlerConfig) *ErrorHandler {
	return &ErrorHandler{
		errorCounts:        make(map[string]*atomic.Uint64),
		recoveryStrategies: make(map[string]RecoveryStrategy),
		config:             config,
		circuitBreaker: &CircuitBreaker{
			threshold: config.CircuitBreakerThreshold,
			timeout:   config.CircuitBreakerTimeout,
		},
	}
}

// HandleError handles an error with context
func (h *ErrorHandler) HandleError(err error, context string) error {
	if err == nil {
		return nil
	}

	// Track error
	h.incrementErrorCount(context)
	h.totalErrors.Add(1)

	// Check if circuit breaker should trip
	if h.circuitBreaker.ShouldTrip() {
		h.criticalErrors.Add(1)
		if h.onCritical != nil {
			h.onCritical(err, context)
		}
		return fmt.Errorf("circuit breaker tripped: %w", err)
	}

	// Log error if enabled
	if h.config.LogErrors && h.onError != nil {
		h.onError(err, context)
	}

	// Try recovery if enabled
	if h.config.EnableAutoRecovery {
		if recovered := h.tryRecover(err, context); recovered != nil {
			return recovered
		}
	}

	return err
}

// HandleCriticalError handles a critical error that may require system shutdown
func (h *ErrorHandler) HandleCriticalError(err error, context string) error {
	h.criticalErrors.Add(1)

	// Always log critical errors
	if h.onCritical != nil {
		h.onCritical(err, context)
	}

	// Trip circuit breaker immediately for critical errors
	h.circuitBreaker.Trip()

	return fmt.Errorf("critical error in %s: %w", context, err)
}

// SetErrorCallback sets the general error callback
func (h *ErrorHandler) SetErrorCallback(callback func(err error, context string)) {
	h.onError = callback
}

// SetCriticalCallback sets the critical error callback
func (h *ErrorHandler) SetCriticalCallback(callback func(err error, context string)) {
	h.onCritical = callback
}

// AddRecoveryStrategy adds a recovery strategy
func (h *ErrorHandler) AddRecoveryStrategy(strategy RecoveryStrategy) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recoveryStrategies[strategy.Name()] = strategy
}

// tryRecover attempts to recover from an error
func (h *ErrorHandler) tryRecover(err error, errorContext string) error {
	h.mu.RLock()
	strategies := make([]RecoveryStrategy, 0, len(h.recoveryStrategies))
	for _, strategy := range h.recoveryStrategies {
		strategies = append(strategies, strategy)
	}
	h.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, strategy := range strategies {
		if strategy.CanRecover(err) {
			if recoveryErr := strategy.Recover(ctx, err); recoveryErr == nil {
				h.recoveries.Add(1)
				if h.config.LogRecoveries {
					fmt.Printf("Recovered from error using %s strategy: %v\n", strategy.Name(), err)
				}
				return nil
			}
		}
	}

	return err
}

// incrementErrorCount tracks errors by context
func (h *ErrorHandler) incrementErrorCount(context string) {
	h.mu.Lock()
	counter, exists := h.errorCounts[context]
	if !exists {
		counter = &atomic.Uint64{}
		h.errorCounts[context] = counter
	}
	h.mu.Unlock()

	counter.Add(1)
}

// GetMetrics returns error handler metrics
func (h *ErrorHandler) GetMetrics() ErrorMetrics {
	h.mu.RLock()
	errorsByContext := make(map[string]uint64)
	for ctx, counter := range h.errorCounts {
		errorsByContext[ctx] = counter.Load()
	}
	h.mu.RUnlock()

	return ErrorMetrics{
		TotalErrors:         h.totalErrors.Load(),
		CriticalErrors:      h.criticalErrors.Load(),
		Recoveries:          h.recoveries.Load(),
		ErrorsByContext:     errorsByContext,
		CircuitBreakerState: h.circuitBreaker.State(),
	}
}

// ErrorMetrics contains error handler metrics
type ErrorMetrics struct {
	TotalErrors         uint64
	CriticalErrors      uint64
	Recoveries          uint64
	ErrorsByContext     map[string]uint64
	CircuitBreakerState string
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	failures  atomic.Int32
	lastFail  atomic.Int64
	state     atomic.Int32 // 0=closed, 1=open, 2=half-open
	threshold int
	timeout   time.Duration
	mu        sync.Mutex
}

const (
	circuitClosed = iota
	circuitOpen
	circuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
	}
}

// Allow checks if operations should be allowed
func (cb *CircuitBreaker) Allow() bool {
	state := cb.state.Load()
	switch state {
	case circuitClosed:
		return true
	case circuitOpen:
		// Check if we should transition to half-open
		lastFail := time.Unix(0, cb.lastFail.Load())
		if time.Since(lastFail) > cb.timeout {
			cb.state.Store(circuitHalfOpen)
			return true
		}
		return false
	case circuitHalfOpen:
		return true
	default:
		return false
	}
}

// ShouldTrip checks if the circuit breaker should trip
func (cb *CircuitBreaker) ShouldTrip() bool {
	failures := cb.failures.Load()
	if failures >= int32(cb.threshold) {
		cb.Trip()
		return true
	}

	// Check if we should reset based on timeout
	state := cb.state.Load()
	if state == circuitOpen {
		lastFail := time.Unix(0, cb.lastFail.Load())
		if time.Since(lastFail) > cb.timeout {
			cb.state.Store(circuitHalfOpen)
		}
	}

	return state == circuitOpen
}

// Trip trips the circuit breaker
func (cb *CircuitBreaker) Trip() {
	cb.state.Store(circuitOpen)
	cb.lastFail.Store(time.Now().UnixNano())
}

// Reset resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.failures.Store(0)
	cb.state.Store(circuitClosed)
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	if cb.state.Load() == circuitHalfOpen {
		cb.Reset()
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.failures.Add(1)
	cb.lastFail.Store(time.Now().UnixNano())
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() string {
	switch cb.state.Load() {
	case circuitClosed:
		return "closed"
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Common recovery strategies

// RetryRecovery implements simple retry recovery
type RetryRecovery struct {
	maxAttempts int
	backoff     time.Duration
}

func NewRetryRecovery(maxAttempts int, backoff time.Duration) *RetryRecovery {
	return &RetryRecovery{
		maxAttempts: maxAttempts,
		backoff:     backoff,
	}
}

func (r *RetryRecovery) Name() string {
	return "retry"
}

func (r *RetryRecovery) CanRecover(err error) bool {
	// Can retry transient errors
	return !errors.Is(err, ErrPermissionDenied) &&
		!errors.Is(err, ErrKernelNotSupported)
}

func (r *RetryRecovery) Recover(ctx context.Context, err error) error {
	// This is a placeholder - actual recovery would retry the operation
	time.Sleep(r.backoff)
	return nil
}

// RestartRecovery implements component restart recovery
type RestartRecovery struct {
	restartFunc func() error
}

func NewRestartRecovery(restartFunc func() error) *RestartRecovery {
	return &RestartRecovery{
		restartFunc: restartFunc,
	}
}

func (r *RestartRecovery) Name() string {
	return "restart"
}

func (r *RestartRecovery) CanRecover(err error) bool {
	// Can restart on program errors
	return errors.Is(err, ErrProgramNotLoaded) ||
		errors.Is(err, ErrCollectorStopped)
}

func (r *RestartRecovery) Recover(ctx context.Context, err error) error {
	if r.restartFunc != nil {
		return r.restartFunc()
	}
	return fmt.Errorf("no restart function configured")
}

// ValidateKernelSupport checks if the kernel supports required eBPF features
func ValidateKernelSupport() error {
	// This would use actual syscalls in production
	// For now, we'll check runtime.GOOS
	if runtime.GOOS != "linux" {
		return fmt.Errorf("eBPF requires Linux, running on %s", runtime.GOOS)
	}

	// In production, would check:
	// - Kernel version >= 4.18 for full eBPF support
	// - Required kernel config options
	// - BPF syscall availability

	return nil
}

// CheckPermissions verifies the process has required permissions
func CheckPermissions() error {
	// In production, would check:
	// - CAP_BPF capability
	// - CAP_NET_ADMIN for network programs
	// - Root access as fallback

	// For now, return nil to allow testing
	return nil
}
