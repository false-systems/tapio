package core

import (
	"time"
)

// OutputFormat represents supported output formats
type OutputFormat string

const (
	OutputFormatJSON       OutputFormat = "json"
	OutputFormatYAML       OutputFormat = "yaml"
	OutputFormatTable      OutputFormat = "table"
	OutputFormatHuman      OutputFormat = "human"
	OutputFormatPrometheus OutputFormat = "prometheus"
)

// ServerConfig represents server configuration
type ServerConfig struct {
	Host           string        `json:"host" yaml:"host"`
	Port           int           `json:"port" yaml:"port"`
	ReadTimeout    time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout" yaml:"write_timeout"`
	MaxHeaderBytes int           `json:"max_header_bytes" yaml:"max_header_bytes"`
	TLSEnabled     bool          `json:"tls_enabled" yaml:"tls_enabled"`
	TLSCertFile    string        `json:"tls_cert_file,omitempty" yaml:"tls_cert_file,omitempty"`
	TLSKeyFile     string        `json:"tls_key_file,omitempty" yaml:"tls_key_file,omitempty"`
}

// CLIConfig represents CLI configuration
type CLIConfig struct {
	OutputFormat OutputFormat `json:"output_format" yaml:"output_format"`
	NoColor      bool         `json:"no_color" yaml:"no_color"`
	Verbose      bool         `json:"verbose" yaml:"verbose"`
	Interactive  bool         `json:"interactive" yaml:"interactive"`
}

// OutputConfig represents output formatting configuration
type OutputConfig struct {
	Format      OutputFormat `json:"format" yaml:"format"`
	PrettyPrint bool         `json:"pretty_print" yaml:"pretty_print"`
	ShowHeaders bool         `json:"show_headers" yaml:"show_headers"`
	TimeFormat  string       `json:"time_format" yaml:"time_format"`
}

// InterfaceError represents an interface-specific error
type InterfaceError struct {
	Interface string
	Operation string
	Err       error
}

func (e InterfaceError) Error() string {
	return e.Interface + " interface failed during " + e.Operation + ": " + e.Err.Error()
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return "validation error: " + e.Field + " - " + e.Message
}
