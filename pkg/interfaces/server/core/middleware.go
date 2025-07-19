package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

// MiddlewareManager manages middleware execution
type MiddlewareManager struct {
	middleware     []domain.Middleware
	middlewareMap  map[string]domain.Middleware
	mu             sync.RWMutex
	logger         domain.Logger
	eventPublisher domain.EventPublisher
}

// NewMiddlewareManager creates a new middleware manager
func NewMiddlewareManager(logger domain.Logger, eventPublisher domain.EventPublisher) *MiddlewareManager {
	return &MiddlewareManager{
		middleware:     make([]domain.Middleware, 0),
		middlewareMap:  make(map[string]domain.Middleware),
		logger:         logger,
		eventPublisher: eventPublisher,
	}
}

// RegisterMiddleware registers a new middleware
func (m *MiddlewareManager) RegisterMiddleware(ctx context.Context, middleware domain.Middleware) error {
	if middleware == nil {
		return domain.ErrInvalidRequest("middleware cannot be nil")
	}

	name := middleware.Name()
	if name == "" {
		return domain.ErrInvalidRequest("middleware name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if middleware already exists
	if _, exists := m.middlewareMap[name]; exists {
		return domain.NewServerError(domain.ErrorCodeAlreadyExists, fmt.Sprintf("middleware already registered: %s", name))
	}

	// Add middleware
	m.middleware = append(m.middleware, middleware)
	m.middlewareMap[name] = middleware

	// Sort middleware by priority
	sort.Slice(m.middleware, func(i, j int) bool {
		return m.middleware[i].Priority() < m.middleware[j].Priority()
	})

	// Log registration
	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("middleware registered: %s (priority: %d)", name, middleware.Priority()))
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        m.generateEventID(),
			Type:      domain.EventTypeRequest,
			Severity:  domain.SeverityInfo,
			Source:    "middleware_manager",
			Message:   fmt.Sprintf("middleware registered: %s", name),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"middleware_name": name,
				"priority":        middleware.Priority(),
			},
			Context: ctx,
		}

		m.eventPublisher.PublishEvent(ctx, event)
	}

	return nil
}

// UnregisterMiddleware unregisters a middleware
func (m *MiddlewareManager) UnregisterMiddleware(ctx context.Context, name string) error {
	if name == "" {
		return domain.ErrInvalidRequest("middleware name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if middleware exists
	middleware, exists := m.middlewareMap[name]
	if !exists {
		return domain.ErrResourceNotFound(fmt.Sprintf("middleware not found: %s", name))
	}

	// Remove from map
	delete(m.middlewareMap, name)

	// Remove from slice
	for i, mw := range m.middleware {
		if mw.Name() == name {
			m.middleware = append(m.middleware[:i], m.middleware[i+1:]...)
			break
		}
	}

	// Log unregistration
	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("middleware unregistered: %s", name))
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        m.generateEventID(),
			Type:      domain.EventTypeRequest,
			Severity:  domain.SeverityInfo,
			Source:    "middleware_manager",
			Message:   fmt.Sprintf("middleware unregistered: %s", name),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"middleware_name": name,
				"priority":        middleware.Priority(),
			},
			Context: ctx,
		}

		m.eventPublisher.PublishEvent(ctx, event)
	}

	return nil
}

// ExecuteMiddleware executes all middleware in order
func (m *MiddlewareManager) ExecuteMiddleware(ctx context.Context, request *domain.Request, response *domain.Response) error {
	m.mu.RLock()
	middleware := make([]domain.Middleware, len(m.middleware))
	copy(middleware, m.middleware)
	m.mu.RUnlock()

	// Create middleware chain
	return m.executeMiddlewareChain(ctx, request, response, middleware, 0)
}

// executeMiddlewareChain executes middleware chain recursively
func (m *MiddlewareManager) executeMiddlewareChain(ctx context.Context, request *domain.Request, response *domain.Response, middleware []domain.Middleware, index int) error {
	// If we've reached the end of the chain, return
	if index >= len(middleware) {
		return nil
	}

	// Get current middleware
	current := middleware[index]

	// Create next function
	next := func() error {
		return m.executeMiddlewareChain(ctx, request, response, middleware, index+1)
	}

	// Execute current middleware
	start := time.Now()
	err := current.Execute(ctx, request, response, next)
	duration := time.Since(start)

	// Log middleware execution
	if m.logger != nil {
		if err != nil {
			m.logger.WithError(err).Error(ctx, fmt.Sprintf("middleware execution failed: %s", current.Name()))
		} else {
			m.logger.Debug(ctx, fmt.Sprintf("middleware executed: %s (duration: %v)", current.Name(), duration))
		}
	}

	// Publish event for failures
	if err != nil && m.eventPublisher != nil {
		event := &domain.Event{
			ID:        m.generateEventID(),
			Type:      domain.EventTypeError,
			Severity:  domain.SeverityError,
			Source:    "middleware_manager",
			Message:   fmt.Sprintf("middleware execution failed: %s", current.Name()),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"middleware_name": current.Name(),
				"error":           err.Error(),
				"duration":        duration.String(),
			},
			Context: ctx,
		}

		m.eventPublisher.PublishEvent(ctx, event)
	}

	return err
}

// ConfigureMiddleware configures a specific middleware
func (m *MiddlewareManager) ConfigureMiddleware(ctx context.Context, name string, config map[string]interface{}) error {
	if name == "" {
		return domain.ErrInvalidRequest("middleware name cannot be empty")
	}

	m.mu.RLock()
	middleware, exists := m.middlewareMap[name]
	m.mu.RUnlock()

	if !exists {
		return domain.ErrResourceNotFound(fmt.Sprintf("middleware not found: %s", name))
	}

	// Configure middleware
	if err := middleware.Configure(ctx, config); err != nil {
		return domain.NewServerErrorWithCause(domain.ErrorCodeConfigurationFailed, fmt.Sprintf("failed to configure middleware: %s", name), err)
	}

	// Log configuration
	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("middleware configured: %s", name))
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        m.generateEventID(),
			Type:      domain.EventTypeRequest,
			Severity:  domain.SeverityInfo,
			Source:    "middleware_manager",
			Message:   fmt.Sprintf("middleware configured: %s", name),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"middleware_name": name,
				"config":          config,
			},
			Context: ctx,
		}

		m.eventPublisher.PublishEvent(ctx, event)
	}

	return nil
}

// GetMiddleware returns a specific middleware by name
func (m *MiddlewareManager) GetMiddleware(name string) (domain.Middleware, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	middleware, exists := m.middlewareMap[name]
	return middleware, exists
}

// GetAllMiddleware returns all registered middleware
func (m *MiddlewareManager) GetAllMiddleware() []domain.Middleware {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]domain.Middleware, len(m.middleware))
	copy(result, m.middleware)
	return result
}

// generateEventID generates a unique event ID
func (m *MiddlewareManager) generateEventID() string {
	return fmt.Sprintf("event-%d", time.Now().UnixNano())
}

// Common middleware implementations

// LoggingMiddleware logs request and response information
type LoggingMiddleware struct {
	logger domain.Logger
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger domain.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

// Execute executes the logging middleware
func (m *LoggingMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	start := time.Now()

	// Log request
	if m.logger != nil {
		m.logger.WithRequest(request).Info(ctx, "request received")
	}

	// Execute next middleware
	err := next()

	// Log response
	duration := time.Since(start)
	if m.logger != nil {
		if err != nil {
			m.logger.WithRequest(request).WithError(err).Error(ctx, fmt.Sprintf("request failed (duration: %v)", duration))
		} else {
			m.logger.WithRequest(request).WithResponse(response).Info(ctx, fmt.Sprintf("request completed (duration: %v)", duration))
		}
	}

	return err
}

// Name returns the middleware name
func (m *LoggingMiddleware) Name() string {
	return "logging"
}

// Priority returns the middleware priority
func (m *LoggingMiddleware) Priority() int {
	return 1000 // High priority to log everything
}

// Configure configures the middleware
func (m *LoggingMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	// No configuration needed for basic logging
	return nil
}

// MetricsMiddleware collects request/response metrics
type MetricsMiddleware struct {
	metricsCollector domain.MetricsCollector
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metricsCollector domain.MetricsCollector) *MetricsMiddleware {
	return &MetricsMiddleware{metricsCollector: metricsCollector}
}

// Execute executes the metrics middleware
func (m *MetricsMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	// Record request
	if m.metricsCollector != nil {
		m.metricsCollector.RecordRequest(ctx, request)
	}

	// Execute next middleware
	err := next()

	// Record response or error
	if m.metricsCollector != nil {
		if err != nil {
			m.metricsCollector.RecordError(ctx, err)
		} else {
			m.metricsCollector.RecordResponse(ctx, response)
		}
	}

	return err
}

// Name returns the middleware name
func (m *MetricsMiddleware) Name() string {
	return "metrics"
}

// Priority returns the middleware priority
func (m *MetricsMiddleware) Priority() int {
	return 500 // Medium priority
}

// Configure configures the middleware
func (m *MetricsMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	// No configuration needed for basic metrics
	return nil
}

// ValidationMiddleware validates requests and responses
type ValidationMiddleware struct {
	validator domain.Validator
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware(validator domain.Validator) *ValidationMiddleware {
	return &ValidationMiddleware{validator: validator}
}

// Execute executes the validation middleware
func (m *ValidationMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	// Validate request
	if m.validator != nil {
		if err := m.validator.ValidateRequest(ctx, request); err != nil {
			return domain.NewServerErrorWithCause(domain.ErrorCodeDataValidationFailed, "request validation failed", err)
		}
	}

	// Execute next middleware
	err := next()

	// Validate response if no error
	if err == nil && response != nil && m.validator != nil {
		if err := m.validator.ValidateResponse(ctx, response); err != nil {
			return domain.NewServerErrorWithCause(domain.ErrorCodeDataValidationFailed, "response validation failed", err)
		}
	}

	return err
}

// Name returns the middleware name
func (m *ValidationMiddleware) Name() string {
	return "validation"
}

// Priority returns the middleware priority
func (m *ValidationMiddleware) Priority() int {
	return 100 // High priority to validate early
}

// Configure configures the middleware
func (m *ValidationMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	// No configuration needed for basic validation
	return nil
}

// SecurityMiddleware handles authentication and authorization
type SecurityMiddleware struct {
	securityManager domain.SecurityManager
}

// NewSecurityMiddleware creates a new security middleware
func NewSecurityMiddleware(securityManager domain.SecurityManager) *SecurityMiddleware {
	return &SecurityMiddleware{securityManager: securityManager}
}

// Execute executes the security middleware
func (m *SecurityMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	// Check authorization
	if m.securityManager != nil {
		authorized, err := m.securityManager.Authorize(ctx, request)
		if err != nil {
			return domain.NewServerErrorWithCause(domain.ErrorCodeAuthenticationFailed, "authorization failed", err)
		}

		if !authorized {
			return domain.ErrUnauthorized()
		}
	}

	// Execute next middleware
	return next()
}

// Name returns the middleware name
func (m *SecurityMiddleware) Name() string {
	return "security"
}

// Priority returns the middleware priority
func (m *SecurityMiddleware) Priority() int {
	return 50 // Very high priority for security
}

// Configure configures the middleware
func (m *SecurityMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	// No configuration needed for basic security
	return nil
}
