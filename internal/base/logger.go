package base

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger creates a structured logger for an observer with OTEL trace context support.
// Uses zerolog for zero-allocation structured logging in hot paths (eBPF event processing).
//
// Default output: JSON to stdout with timestamps and observer name.
// For human-readable logs in development, set: TAPIO_LOG_FORMAT=console
//
// Example usage:
//
//	logger := base.NewLogger("network")
//	logger.Info().Str("event", "tcp_connect").Msg("connection established")
func NewLogger(observerName string) zerolog.Logger {
	// Check log format from environment
	format := os.Getenv("TAPIO_LOG_FORMAT")
	var output io.Writer = os.Stdout

	// Console format for development (human-readable with colors)
	if format == "console" {
		output = zerolog.ConsoleWriter{Out: os.Stdout}
	}

	// JSON format for production (default)
	return zerolog.New(output).
		With().
		Timestamp().
		Str("observer", observerName).
		Logger()
}

// WithTraceContext adds OTEL trace and span IDs to logger for distributed correlation.
// This enables correlating logs with OTEL traces across services.
//
// Example:
//
//	ctx := trace.ContextWithSpan(ctx, span)
//	logger := base.WithTraceContext(ctx, baseLogger)
//	logger.Info().Msg("processing event")  // Includes trace_id and span_id
func WithTraceContext(ctx context.Context, logger zerolog.Logger) zerolog.Logger {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return logger
	}

	return logger.With().
		Str("trace_id", spanCtx.TraceID().String()).
		Str("span_id", spanCtx.SpanID().String()).
		Logger()
}

// SetGlobalLogLevel sets the minimum log level for all loggers.
// Valid levels: trace, debug, info, warn, error, fatal, panic
//
// Can also be set via environment: TAPIO_LOG_LEVEL=debug
func SetGlobalLogLevel(level string) {
	switch level {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

func init() {
	// Set log level from environment on startup
	if level := os.Getenv("TAPIO_LOG_LEVEL"); level != "" {
		SetGlobalLogLevel(level)
	} else {
		// Default: info level
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
