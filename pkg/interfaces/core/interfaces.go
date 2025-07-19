package core

import (
	"context"
	"io"
)

// Interface defines the base contract for all user interfaces
type Interface interface {
	// Name returns the interface identifier
	Name() string
	
	// Initialize sets up the interface
	Initialize(ctx context.Context, config Config) error
	
	// Start begins serving the interface
	Start(ctx context.Context) error
	
	// Stop gracefully shuts down the interface
	Stop(ctx context.Context) error
	
	// Health checks the interface status
	Health(ctx context.Context) (*HealthStatus, error)
}

// Config provides interface configuration
type Config interface {
	Validate() error
}

// HealthStatus represents interface health
type HealthStatus struct {
	Healthy bool                   `json:"healthy"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// OutputFormatter formats data for display
type OutputFormatter interface {
	// Format formats the data according to the implementation
	Format(w io.Writer, data interface{}) error
	
	// FormatString returns formatted string
	FormatString(data interface{}) (string, error)
	
	// SupportsStreaming indicates if streaming is supported
	SupportsStreaming() bool
	
	// ContentType returns the MIME type of the output
	ContentType() string
}

// CLI represents command-line interface
type CLI interface {
	Interface
	
	// Execute runs the CLI with given arguments
	Execute(args []string) error
	
	// AddCommand adds a new command to the CLI
	AddCommand(cmd Command) error
}

// Command represents a CLI command
type Command interface {
	// Name returns the command name
	Name() string
	
	// Description returns command description
	Description() string
	
	// Execute runs the command
	Execute(ctx context.Context, args []string) error
	
	// Validate validates command arguments
	Validate(args []string) error
}

// Server represents a server interface (HTTP/gRPC)
type Server interface {
	Interface
	
	// ListenAndServe starts the server
	ListenAndServe() error
	
	// Shutdown gracefully shuts down the server
	Shutdown(ctx context.Context) error
	
	// Port returns the port the server is listening on
	Port() int
}

// ConfigManager manages configuration across interfaces
type ConfigManager interface {
	// Load loads configuration from various sources
	Load(ctx context.Context) error
	
	// Get retrieves a configuration value
	Get(key string) (interface{}, error)
	
	// Set sets a configuration value
	Set(key string, value interface{}) error
	
	// Watch watches for configuration changes
	Watch(ctx context.Context, callback func(key string, oldValue, newValue interface{})) error
	
	// Validate validates the entire configuration
	Validate() error
}