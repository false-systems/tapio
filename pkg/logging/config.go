package logging

import (
	"log/slog"
	"os"
	"strings"
)

// ConfigFromEnvironment creates logging configuration from environment variables
func ConfigFromEnvironment() *Config {
	config := DefaultConfig()

	// Log level
	if level := os.Getenv("TAPIO_LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "debug":
			config.Level = slog.LevelDebug
		case "info":
			config.Level = slog.LevelInfo
		case "warn", "warning":
			config.Level = slog.LevelWarn
		case "error":
			config.Level = slog.LevelError
		}
	}

	// Log format
	if format := os.Getenv("TAPIO_LOG_FORMAT"); format != "" {
		config.Format = strings.ToLower(format)
	}

	// Log output
	if output := os.Getenv("TAPIO_LOG_OUTPUT"); output != "" {
		config.Output = strings.ToLower(output)
	}

	// Environment
	if env := os.Getenv("TAPIO_ENVIRONMENT"); env != "" {
		config.Environment = env
	}

	// Service name
	if service := os.Getenv("TAPIO_SERVICE_NAME"); service != "" {
		config.ServiceName = service
	}

	// Performance settings
	if async := os.Getenv("TAPIO_LOG_ASYNC"); async == "true" {
		config.Async = true
	}

	// Security settings
	if redact := os.Getenv("TAPIO_LOG_REDACT_SECRETS"); redact == "false" {
		config.RedactSecrets = false
	}

	return config
}

// GetLoggerForEnvironment returns a logger configured for the specified environment
func GetLoggerForEnvironment(env string) *Logger {
	switch strings.ToLower(env) {
	case "development", "dev":
		return Development
	case "production", "prod":
		return Production
	case "performance", "perf":
		return HighPerformance
	case "compliance", "audit":
		return Compliance
	default:
		return Production
	}
}

// MustGetLogger creates a logger and panics on error
func MustGetLogger(config *Config) *Logger {
	logger := NewLogger(config)
	if logger == nil {
		panic("failed to create logger")
	}
	return logger
}

// SetGlobalLogger sets the default slog logger
func SetGlobalLogger(logger *Logger) {
	slog.SetDefault(logger.Logger)
}

// InitializeLogging initializes the global logger based on environment
func InitializeLogging() {
	config := ConfigFromEnvironment()
	logger := NewLogger(config)
	SetGlobalLogger(logger)
}
