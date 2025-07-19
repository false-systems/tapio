package correlation

import (
	"context"

	corrDomain "github.com/yairfalse/tapio/pkg/correlation/domain"
	serverDomain "github.com/yairfalse/tapio/pkg/server/domain"
)

// LoggerAdapter adapts server domain logger to correlation domain logger
type LoggerAdapter struct {
	logger serverDomain.Logger
}

// NewLoggerAdapter creates a new logger adapter
func NewLoggerAdapter(logger serverDomain.Logger) corrDomain.Logger {
	return &LoggerAdapter{logger: logger}
}

// Debug logs debug messages
func (l *LoggerAdapter) Debug(msg string, args ...interface{}) {
	// Use background context for correlation logger compatibility
	l.logger.Debug(context.Background(), msg, args...)
}

// Info logs info messages
func (l *LoggerAdapter) Info(msg string, args ...interface{}) {
	l.logger.Info(context.Background(), msg, args...)
}

// Warn logs warning messages
func (l *LoggerAdapter) Warn(msg string, args ...interface{}) {
	l.logger.Warn(context.Background(), msg, args...)
}

// Error logs error messages
func (l *LoggerAdapter) Error(msg string, args ...interface{}) {
	l.logger.Error(context.Background(), msg, args...)
}

// With returns a logger with additional context fields
func (l *LoggerAdapter) With(args ...interface{}) corrDomain.Logger {
	// Since the server logger doesn't support With, we return the same logger
	// In a real implementation, you might want to store the fields and append them to messages
	return l
}
