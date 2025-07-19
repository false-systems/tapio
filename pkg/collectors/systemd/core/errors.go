package core

import "errors"

// Common errors
var (
	// Configuration errors
	ErrInvalidConfig     = errors.New("invalid configuration")
	ErrNoServicesToWatch = errors.New("no services configured to watch")

	// Runtime errors
	ErrAlreadyStarted  = errors.New("collector already started")
	ErrNotStarted      = errors.New("collector not started")
	ErrContextCanceled = errors.New("context canceled")
	ErrShuttingDown    = errors.New("collector shutting down")

	// D-Bus errors
	ErrDBusConnection   = errors.New("failed to connect to D-Bus")
	ErrDBusNotAvailable = errors.New("D-Bus not available")
	ErrDBusPermission   = errors.New("permission denied accessing D-Bus")
	ErrDBusTimeout      = errors.New("D-Bus operation timeout")

	// systemd errors
	ErrSystemdNotAvailable = errors.New("systemd not available")
	ErrSystemdVersion      = errors.New("systemd version not supported")
	ErrUnitNotFound        = errors.New("systemd unit not found")
	ErrInvalidUnitType     = errors.New("invalid systemd unit type")

	// Platform errors
	ErrPlatformNotSupported = errors.New("platform not supported")
	ErrLinuxOnly            = errors.New("systemd collector requires Linux")

	// Processing errors
	ErrEventDecoding     = errors.New("failed to decode event")
	ErrEventValidation   = errors.New("event validation failed")
	ErrBufferFull        = errors.New("event buffer full")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)
