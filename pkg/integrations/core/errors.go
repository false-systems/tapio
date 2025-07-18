package core

import "fmt"

var (
	// ErrNotInitialized indicates the integration is not initialized
	ErrNotInitialized = fmt.Errorf("integration not initialized")
	
	// ErrAlreadyInitialized indicates the integration is already initialized
	ErrAlreadyInitialized = fmt.Errorf("integration already initialized")
	
	// ErrConnectionFailed indicates connection to external system failed
	ErrConnectionFailed = fmt.Errorf("connection to external system failed")
	
	// ErrExportFailed indicates export operation failed
	ErrExportFailed = fmt.Errorf("export operation failed")
	
	// ErrInvalidConfig indicates invalid configuration
	ErrInvalidConfig = fmt.Errorf("invalid configuration")
	
	// ErrNotSupported indicates operation not supported
	ErrNotSupported = fmt.Errorf("operation not supported")
)