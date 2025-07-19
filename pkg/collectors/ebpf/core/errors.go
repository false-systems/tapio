package core

import "errors"

// Common errors
var (
	// Configuration errors
	ErrInvalidConfig   = errors.New("invalid configuration")
	ErrMissingRequired = errors.New("missing required configuration")

	// Runtime errors
	ErrAlreadyStarted  = errors.New("collector already started")
	ErrNotStarted      = errors.New("collector not started")
	ErrContextCanceled = errors.New("context canceled")
	ErrShuttingDown    = errors.New("collector shutting down")

	// Permission errors
	ErrInsufficientPrivileges = errors.New("insufficient privileges (requires CAP_SYS_ADMIN)")
	ErrKernelNotSupported     = errors.New("kernel version not supported")
	ErrBPFNotSupported        = errors.New("BPF not supported on this system")

	// Resource errors
	ErrOutOfMemory       = errors.New("out of memory")
	ErrTooManyPrograms   = errors.New("too many programs loaded")
	ErrMapCreationFailed = errors.New("failed to create BPF map")

	// Processing errors
	ErrEventDecoding     = errors.New("failed to decode event")
	ErrEventValidation   = errors.New("event validation failed")
	ErrBufferFull        = errors.New("event buffer full")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)
