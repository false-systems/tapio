package core

import "errors"

// Common errors
var (
	// Configuration errors
	ErrInvalidConfig    = errors.New("invalid configuration")
	ErrInvalidCursor    = errors.New("invalid cursor")
	ErrInvalidTimeRange = errors.New("invalid time range")

	// Runtime errors
	ErrAlreadyStarted  = errors.New("collector already started")
	ErrNotStarted      = errors.New("collector not started")
	ErrContextCanceled = errors.New("context canceled")
	ErrShuttingDown    = errors.New("collector shutting down")

	// Journal errors
	ErrJournalNotOpen      = errors.New("journal not open")
	ErrJournalOpen         = errors.New("journal already open")
	ErrJournalNotAvailable = errors.New("journal not available")
	ErrJournalCorrupted    = errors.New("journal corrupted")
	ErrJournalEnd          = errors.New("reached end of journal")

	// Read errors
	ErrReadTimeout    = errors.New("read timeout")
	ErrSeekFailed     = errors.New("seek operation failed")
	ErrCursorNotFound = errors.New("cursor not found")
	ErrNoMoreEntries  = errors.New("no more entries")

	// Permission errors
	ErrPermissionDenied       = errors.New("permission denied accessing journal")
	ErrInsufficientPrivileges = errors.New("insufficient privileges")

	// Platform errors
	ErrPlatformNotSupported = errors.New("platform not supported")
	ErrLinuxOnly            = errors.New("journald collector requires Linux")

	// Processing errors
	ErrEntryDecoding     = errors.New("failed to decode entry")
	ErrEntryValidation   = errors.New("entry validation failed")
	ErrBufferFull        = errors.New("event buffer full")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// Cursor management errors
	ErrCursorSave        = errors.New("failed to save cursor")
	ErrCursorLoad        = errors.New("failed to load cursor")
	ErrCursorFileCorrupt = errors.New("cursor file corrupted")
)
