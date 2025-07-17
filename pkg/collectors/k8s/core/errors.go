package core

import "errors"

// Common errors
var (
	// Configuration errors
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrMissingKubeConfig  = errors.New("kubeconfig not found and not running in cluster")
	ErrInvalidNamespace   = errors.New("invalid namespace")
	
	// Runtime errors
	ErrAlreadyStarted     = errors.New("collector already started")
	ErrNotStarted         = errors.New("collector not started")
	ErrContextCanceled    = errors.New("context canceled")
	ErrShuttingDown       = errors.New("collector shutting down")
	
	// Connection errors
	ErrConnectionFailed   = errors.New("failed to connect to Kubernetes API")
	ErrAuthFailed         = errors.New("authentication failed")
	ErrUnauthorized       = errors.New("unauthorized access to Kubernetes API")
	ErrAPINotReachable    = errors.New("Kubernetes API not reachable")
	
	// Watch errors
	ErrWatchFailed        = errors.New("watch operation failed")
	ErrWatchTimeout       = errors.New("watch timeout")
	ErrResourceNotFound   = errors.New("resource not found")
	ErrTooManyRequests    = errors.New("too many requests to API server")
	
	// Processing errors
	ErrEventDecoding      = errors.New("failed to decode event")
	ErrEventValidation    = errors.New("event validation failed")
	ErrBufferFull         = errors.New("event buffer full")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
)