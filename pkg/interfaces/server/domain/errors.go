package domain

import (
	"errors"
	"fmt"
)

// ServerError represents a server-specific error
type ServerError struct {
	Code    ErrorCode
	Message string
	Cause   error
	Context map[string]interface{}
}

// ErrorCode represents server error codes
type ErrorCode string

const (
	// Server lifecycle errors
	ErrorCodeServerNotStarted     ErrorCode = "SERVER_NOT_STARTED"
	ErrorCodeServerAlreadyRunning ErrorCode = "SERVER_ALREADY_RUNNING"
	ErrorCodeServerStartupFailed  ErrorCode = "SERVER_STARTUP_FAILED"
	ErrorCodeServerShutdownFailed ErrorCode = "SERVER_SHUTDOWN_FAILED"

	// Request/Response errors
	ErrorCodeInvalidRequest      ErrorCode = "INVALID_REQUEST"
	ErrorCodeRequestTimeout      ErrorCode = "REQUEST_TIMEOUT"
	ErrorCodeRequestTooLarge     ErrorCode = "REQUEST_TOO_LARGE"
	ErrorCodeUnsupportedMethod   ErrorCode = "UNSUPPORTED_METHOD"
	ErrorCodeResponseFailed      ErrorCode = "RESPONSE_FAILED"
	ErrorCodeSerializationFailed ErrorCode = "SERIALIZATION_FAILED"

	// Connection errors
	ErrorCodeConnectionFailed   ErrorCode = "CONNECTION_FAILED"
	ErrorCodeConnectionTimeout  ErrorCode = "CONNECTION_TIMEOUT"
	ErrorCodeConnectionClosed   ErrorCode = "CONNECTION_CLOSED"
	ErrorCodeTooManyConnections ErrorCode = "TOO_MANY_CONNECTIONS"
	ErrorCodeConnectionNotFound ErrorCode = "CONNECTION_NOT_FOUND"

	// Endpoint errors
	ErrorCodeEndpointNotFound    ErrorCode = "ENDPOINT_NOT_FOUND"
	ErrorCodeEndpointUnavailable ErrorCode = "ENDPOINT_UNAVAILABLE"
	ErrorCodeEndpointFailed      ErrorCode = "ENDPOINT_FAILED"
	ErrorCodeEndpointTimeout     ErrorCode = "ENDPOINT_TIMEOUT"

	// Authentication/Authorization errors
	ErrorCodeUnauthorized         ErrorCode = "UNAUTHORIZED"
	ErrorCodeForbidden            ErrorCode = "FORBIDDEN"
	ErrorCodeAuthenticationFailed ErrorCode = "AUTHENTICATION_FAILED"
	ErrorCodeInvalidCredentials   ErrorCode = "INVALID_CREDENTIALS"
	ErrorCodeTokenExpired         ErrorCode = "TOKEN_EXPIRED"

	// Rate limiting errors
	ErrorCodeRateLimitExceeded ErrorCode = "RATE_LIMIT_EXCEEDED"
	ErrorCodeQuotaExceeded     ErrorCode = "QUOTA_EXCEEDED"
	ErrorCodeBandwidthExceeded ErrorCode = "BANDWIDTH_EXCEEDED"

	// Configuration errors
	ErrorCodeInvalidConfiguration ErrorCode = "INVALID_CONFIGURATION"
	ErrorCodeConfigurationFailed  ErrorCode = "CONFIGURATION_FAILED"
	ErrorCodeConfigNotFound       ErrorCode = "CONFIG_NOT_FOUND"

	// Resource errors
	ErrorCodeResourceNotFound    ErrorCode = "RESOURCE_NOT_FOUND"
	ErrorCodeResourceUnavailable ErrorCode = "RESOURCE_UNAVAILABLE"
	ErrorCodeResourceExhausted   ErrorCode = "RESOURCE_EXHAUSTED"
	ErrorCodeInsufficientMemory  ErrorCode = "INSUFFICIENT_MEMORY"
	ErrorCodeInsufficientStorage ErrorCode = "INSUFFICIENT_STORAGE"

	// Data errors
	ErrorCodeDataValidationFailed ErrorCode = "DATA_VALIDATION_FAILED"
	ErrorCodeDataCorrupted        ErrorCode = "DATA_CORRUPTED"
	ErrorCodeDataNotFound         ErrorCode = "DATA_NOT_FOUND"
	ErrorCodeDataConflict         ErrorCode = "DATA_CONFLICT"

	// Health check errors
	ErrorCodeHealthCheckFailed  ErrorCode = "HEALTH_CHECK_FAILED"
	ErrorCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrorCodeDependencyFailed   ErrorCode = "DEPENDENCY_FAILED"

	// Metrics errors
	ErrorCodeMetricsCollectionFailed ErrorCode = "METRICS_COLLECTION_FAILED"
	ErrorCodeMetricsNotAvailable     ErrorCode = "METRICS_NOT_AVAILABLE"

	// Generic errors
	ErrorCodeInternalError     ErrorCode = "INTERNAL_ERROR"
	ErrorCodeUnknownError      ErrorCode = "UNKNOWN_ERROR"
	ErrorCodeOperationFailed   ErrorCode = "OPERATION_FAILED"
	ErrorCodeNotImplemented    ErrorCode = "NOT_IMPLEMENTED"
	ErrorCodeDeprecatedFeature ErrorCode = "DEPRECATED_FEATURE"
	ErrorCodeAlreadyExists     ErrorCode = "ALREADY_EXISTS"
)

// Error implements the error interface
func (e *ServerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *ServerError) Unwrap() error {
	return e.Cause
}

// WithContext adds context to the error
func (e *ServerError) WithContext(key string, value interface{}) *ServerError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// NewServerError creates a new server error
func NewServerError(code ErrorCode, message string) *ServerError {
	return &ServerError{
		Code:    code,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// NewServerErrorWithCause creates a new server error with a cause
func NewServerErrorWithCause(code ErrorCode, message string, cause error) *ServerError {
	return &ServerError{
		Code:    code,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// Pre-defined error constructors for common cases

// ErrServerNotStarted creates a server not started error
func ErrServerNotStarted() *ServerError {
	return NewServerError(ErrorCodeServerNotStarted, "server has not been started")
}

// ErrServerAlreadyRunning creates a server already running error
func ErrServerAlreadyRunning() *ServerError {
	return NewServerError(ErrorCodeServerAlreadyRunning, "server is already running")
}

// ErrInvalidRequest creates an invalid request error
func ErrInvalidRequest(message string) *ServerError {
	return NewServerError(ErrorCodeInvalidRequest, message)
}

// ErrRequestTimeout creates a request timeout error
func ErrRequestTimeout() *ServerError {
	return NewServerError(ErrorCodeRequestTimeout, "request timed out")
}

// ErrConnectionFailed creates a connection failed error
func ErrConnectionFailed(cause error) *ServerError {
	return NewServerErrorWithCause(ErrorCodeConnectionFailed, "connection failed", cause)
}

// ErrConnectionNotFound creates a connection not found error
func ErrConnectionNotFound(connectionID string) *ServerError {
	return NewServerError(ErrorCodeConnectionNotFound, fmt.Sprintf("connection not found: %s", connectionID))
}

// ErrEndpointNotFound creates an endpoint not found error
func ErrEndpointNotFound(endpoint string) *ServerError {
	return NewServerError(ErrorCodeEndpointNotFound, fmt.Sprintf("endpoint not found: %s", endpoint))
}

// ErrEndpointUnavailable creates an endpoint unavailable error
func ErrEndpointUnavailable(endpoint string) *ServerError {
	return NewServerError(ErrorCodeEndpointUnavailable, fmt.Sprintf("endpoint unavailable: %s", endpoint))
}

// ErrUnauthorized creates an unauthorized error
func ErrUnauthorized() *ServerError {
	return NewServerError(ErrorCodeUnauthorized, "unauthorized access")
}

// ErrForbidden creates a forbidden error
func ErrForbidden() *ServerError {
	return NewServerError(ErrorCodeForbidden, "access forbidden")
}

// ErrRateLimitExceeded creates a rate limit exceeded error
func ErrRateLimitExceeded() *ServerError {
	return NewServerError(ErrorCodeRateLimitExceeded, "rate limit exceeded")
}

// ErrInvalidConfiguration creates an invalid configuration error
func ErrInvalidConfiguration(message string) *ServerError {
	return NewServerError(ErrorCodeInvalidConfiguration, message)
}

// ErrResourceNotFound creates a resource not found error
func ErrResourceNotFound(resource string) *ServerError {
	return NewServerError(ErrorCodeResourceNotFound, fmt.Sprintf("resource not found: %s", resource))
}

// ErrResourceExhausted creates a resource exhausted error
func ErrResourceExhausted(resource string) *ServerError {
	return NewServerError(ErrorCodeResourceExhausted, fmt.Sprintf("resource exhausted: %s", resource))
}

// ErrDataValidationFailed creates a data validation failed error
func ErrDataValidationFailed(message string) *ServerError {
	return NewServerError(ErrorCodeDataValidationFailed, message)
}

// ErrHealthCheckFailed creates a health check failed error
func ErrHealthCheckFailed(component string, cause error) *ServerError {
	return NewServerErrorWithCause(ErrorCodeHealthCheckFailed, fmt.Sprintf("health check failed for component: %s", component), cause)
}

// ErrServiceUnavailable creates a service unavailable error
func ErrServiceUnavailable() *ServerError {
	return NewServerError(ErrorCodeServiceUnavailable, "service unavailable")
}

// ErrInternalError creates an internal error
func ErrInternalError(cause error) *ServerError {
	return NewServerErrorWithCause(ErrorCodeInternalError, "internal server error", cause)
}

// ErrNotImplemented creates a not implemented error
func ErrNotImplemented(feature string) *ServerError {
	return NewServerError(ErrorCodeNotImplemented, fmt.Sprintf("feature not implemented: %s", feature))
}

// Common error checking functions

// IsServerError checks if an error is a ServerError
func IsServerError(err error) bool {
	_, ok := err.(*ServerError)
	return ok
}

// AsServerError converts an error to a ServerError if possible
func AsServerError(err error) (*ServerError, bool) {
	if serverErr, ok := err.(*ServerError); ok {
		return serverErr, true
	}
	return nil, false
}

// HasErrorCode checks if an error has a specific error code
func HasErrorCode(err error, code ErrorCode) bool {
	if serverErr, ok := AsServerError(err); ok {
		return serverErr.Code == code
	}
	return false
}

// WrapError wraps a generic error as a ServerError
func WrapError(err error, code ErrorCode, message string) *ServerError {
	return NewServerErrorWithCause(code, message, err)
}

// Common error validation functions

// ValidateNotNil validates that a value is not nil
func ValidateNotNil(value interface{}, field string) error {
	if value == nil {
		return ErrDataValidationFailed(fmt.Sprintf("%s cannot be nil", field))
	}
	return nil
}

// ValidateNotEmpty validates that a string is not empty
func ValidateNotEmpty(value, field string) error {
	if value == "" {
		return ErrDataValidationFailed(fmt.Sprintf("%s cannot be empty", field))
	}
	return nil
}

// ValidatePositive validates that a number is positive
func ValidatePositive(value int, field string) error {
	if value <= 0 {
		return ErrDataValidationFailed(fmt.Sprintf("%s must be positive", field))
	}
	return nil
}

// ValidateRange validates that a number is within a range
func ValidateRange(value, min, max int, field string) error {
	if value < min || value > max {
		return ErrDataValidationFailed(fmt.Sprintf("%s must be between %d and %d", field, min, max))
	}
	return nil
}

// Error aggregation for multiple validation errors
type ValidationErrors struct {
	Errors []error
}

func (ve *ValidationErrors) Error() string {
	if len(ve.Errors) == 0 {
		return "no validation errors"
	}

	msg := "validation errors: "
	for i, err := range ve.Errors {
		if i > 0 {
			msg += "; "
		}
		msg += err.Error()
	}
	return msg
}

func (ve *ValidationErrors) Add(err error) {
	if err != nil {
		ve.Errors = append(ve.Errors, err)
	}
}

func (ve *ValidationErrors) HasErrors() bool {
	return len(ve.Errors) > 0
}

func (ve *ValidationErrors) ToError() error {
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// NewValidationErrors creates a new validation errors container
func NewValidationErrors() *ValidationErrors {
	return &ValidationErrors{
		Errors: make([]error, 0),
	}
}

// Error constants for common scenarios
var (
	ErrTimeout                  = errors.New("operation timed out")
	ErrCancelled                = errors.New("operation was cancelled")
	ErrNotFound                 = errors.New("resource not found")
	ErrAlreadyExists            = errors.New("resource already exists")
	ErrInvalidArgument          = errors.New("invalid argument")
	ErrPermissionDenied         = errors.New("permission denied")
	ErrUnauthenticated          = errors.New("unauthenticated")
	ErrUnavailable              = errors.New("service unavailable")
	ErrUnimplemented            = errors.New("feature not implemented")
	ErrInternal                 = errors.New("internal error")
	ErrResourceExhaustedGeneric = errors.New("resource exhausted")
	ErrOutOfRange               = errors.New("value out of range")
	ErrDataLoss                 = errors.New("data loss")
)
