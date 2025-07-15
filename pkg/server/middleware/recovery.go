package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

// RecoveryMiddleware handles panic recovery
type RecoveryMiddleware struct {
	logger           domain.Logger
	eventPublisher   domain.EventPublisher
	metricsCollector domain.MetricsCollector
	stackTrace       bool

	// Custom panic handler
	panicHandler func(ctx context.Context, err interface{}, stack []byte)
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(
	logger domain.Logger,
	eventPublisher domain.EventPublisher,
	metricsCollector domain.MetricsCollector,
) *RecoveryMiddleware {
	return &RecoveryMiddleware{
		logger:           logger,
		eventPublisher:   eventPublisher,
		metricsCollector: metricsCollector,
		stackTrace:       true,
	}
}

// Execute implements the middleware interface
func (m *RecoveryMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	defer func() {
		if err := recover(); err != nil {
			m.handlePanic(ctx, err, request, response)
		}
	}()

	return next()
}

// handlePanic handles a panic
func (m *RecoveryMiddleware) handlePanic(ctx context.Context, err interface{}, request *domain.Request, response *domain.Response) {
	// Get stack trace
	stack := debug.Stack()

	// Log the panic
	if m.logger != nil {
		m.logger.Error(ctx, fmt.Sprintf("panic recovered: %v\n%s", err, string(stack)))
	}

	// Record metrics
	if m.metricsCollector != nil {
		m.metricsCollector.RecordError(ctx, fmt.Errorf("panic: %v", err))
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        fmt.Sprintf("panic-%d", time.Now().UnixNano()),
			Type:      domain.EventTypeError,
			Severity:  domain.SeverityCritical,
			Source:    "recovery_middleware",
			Message:   fmt.Sprintf("panic recovered: %v", err),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"error":        fmt.Sprintf("%v", err),
				"request_id":   request.ID,
				"request_type": request.Type,
				"stack_trace":  string(stack),
			},
			Context: ctx,
		}
		m.eventPublisher.PublishEvent(ctx, event)
	}

	// Call custom panic handler if set
	if m.panicHandler != nil {
		m.panicHandler(ctx, err, stack)
	}

	// Update response
	response.Type = domain.ResponseTypeError
	response.Status = domain.ResponseStatusError
	response.Error = domain.NewServerError(domain.ErrorCodeInternalError, "internal server error")

	// In production, don't expose stack trace to client
	errorData := map[string]interface{}{
		"error": "internal server error",
		"code":  domain.ErrorCodeInternalError,
	}

	// Add stack trace in development
	if m.stackTrace && ctx.Value("environment") == "development" {
		errorData["stack_trace"] = string(stack)
	}

	response.Data = errorData
}

// Name returns the middleware name
func (m *RecoveryMiddleware) Name() string {
	return "recovery"
}

// Priority returns the middleware priority (highest priority)
func (m *RecoveryMiddleware) Priority() int {
	return 1000 // Execute first to catch all panics
}

// Configure configures the middleware
func (m *RecoveryMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	if stackTrace, ok := config["stack_trace"].(bool); ok {
		m.stackTrace = stackTrace
	}

	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("recovery middleware configured: stackTrace=%v", m.stackTrace))
	}

	return nil
}

// SetPanicHandler sets a custom panic handler
func (m *RecoveryMiddleware) SetPanicHandler(handler func(ctx context.Context, err interface{}, stack []byte)) {
	m.panicHandler = handler
}

// HTTPMiddleware returns an HTTP handler middleware for recovery
func (m *RecoveryMiddleware) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					m.handleHTTPPanic(w, r, err)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// handleHTTPPanic handles panic in HTTP context
func (m *RecoveryMiddleware) handleHTTPPanic(w http.ResponseWriter, r *http.Request, err interface{}) {
	// Get stack trace
	stack := debug.Stack()

	// Log the panic
	if m.logger != nil {
		m.logger.Error(r.Context(), fmt.Sprintf("HTTP panic recovered: %v\n%s", err, string(stack)))
	}

	// Record metrics
	if m.metricsCollector != nil {
		m.metricsCollector.RecordError(r.Context(), fmt.Errorf("HTTP panic: %v", err))
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        fmt.Sprintf("http-panic-%d", time.Now().UnixNano()),
			Type:      domain.EventTypeError,
			Severity:  domain.SeverityCritical,
			Source:    "recovery_middleware",
			Message:   fmt.Sprintf("HTTP panic recovered: %v", err),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"error":       fmt.Sprintf("%v", err),
				"method":      r.Method,
				"path":        r.URL.Path,
				"remote_addr": r.RemoteAddr,
				"stack_trace": string(stack),
			},
			Context: r.Context(),
		}
		m.eventPublisher.PublishEvent(r.Context(), event)
	}

	// Set error response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)

	// Response body
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    http.StatusInternalServerError,
			"message": "Internal Server Error",
		},
	}

	// Add stack trace in development
	if m.stackTrace && r.Header.Get("X-Environment") == "development" {
		response["error"].(map[string]interface{})["stack_trace"] = string(stack)
	}

	json.NewEncoder(w).Encode(response)
}

// RecoveryStats tracks recovery statistics
type RecoveryStats struct {
	TotalPanics    int64
	LastPanic      time.Time
	LastPanicError string
}

// GetStats returns recovery statistics
func (m *RecoveryMiddleware) GetStats() *RecoveryStats {
	// In production, this would track actual statistics
	return &RecoveryStats{
		TotalPanics:    0,
		LastPanic:      time.Time{},
		LastPanicError: "",
	}
}
