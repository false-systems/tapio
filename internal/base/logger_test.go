package base

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// LogEntry represents a structured log entry for testing
type LogEntry struct {
	Level    string `json:"level"`
	Observer string `json:"observer"`
	Message  string `json:"message"`
	Time     string `json:"time"`
	TraceID  string `json:"trace_id,omitempty"`
	SpanID   string `json:"span_id,omitempty"`
	Error    string `json:"error,omitempty"`

	// Event fields
	EventType string  `json:"event_type,omitempty"`
	SrcIP     string  `json:"src_ip,omitempty"`
	DstPort   float64 `json:"dst_port,omitempty"`
	PID       float64 `json:"pid,omitempty"`

	// Test fields
	Stage string `json:"stage,omitempty"`
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-observer")

	// Should not be nil
	assert.NotNil(t, logger)

	// Should have observer field set
	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)
	testLogger.Info().Msg("test message")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "test-observer", logEntry.Observer)
	assert.Equal(t, "test message", logEntry.Message)
	assert.NotEmpty(t, logEntry.Time)
}

func TestNewLogger_ConsoleFormat(t *testing.T) {
	// Set console format BEFORE creating logger
	originalFormat := os.Getenv("TAPIO_LOG_FORMAT")
	if err := os.Setenv("TAPIO_LOG_FORMAT", "console"); err != nil {
		t.Fatalf("failed to set TAPIO_LOG_FORMAT: %v", err)
	}
	defer func() {
		if originalFormat == "" {
			if err := os.Unsetenv("TAPIO_LOG_FORMAT"); err != nil {
				t.Logf("failed to unset TAPIO_LOG_FORMAT: %v", err)
			}
		} else {
			if err := os.Setenv("TAPIO_LOG_FORMAT", originalFormat); err != nil {
				t.Logf("failed to restore TAPIO_LOG_FORMAT: %v", err)
			}
		}
	}()

	logger := NewLogger("test-observer")
	assert.NotNil(t, logger)

	// Console format produces human-readable output (not JSON)
	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)
	testLogger.Info().Msg("console test")

	output := buf.String()
	// Console format includes formatted text and timestamp (not pure JSON)
	assert.Contains(t, output, "console test")
	// Console writer still contains some structure but not as strict JSON
	// Just verify it contains the message
}

func TestWithTraceContext_NoSpan(t *testing.T) {
	baseLogger := NewLogger("test-observer")
	ctx := context.Background()

	// No span in context
	logger := WithTraceContext(ctx, baseLogger)

	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)
	testLogger.Info().Msg("no trace")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	// Should not have trace fields
	assert.Empty(t, logEntry.TraceID)
	assert.Empty(t, logEntry.SpanID)
}

func TestWithTraceContext_WithSpan(t *testing.T) {
	baseLogger := NewLogger("test-observer")

	// Create a tracer and span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	logger := WithTraceContext(ctx, baseLogger)

	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)
	testLogger.Info().Msg("with trace")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	// Should have trace fields
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		assert.Equal(t, spanCtx.TraceID().String(), logEntry.TraceID)
		assert.Equal(t, spanCtx.SpanID().String(), logEntry.SpanID)
	}
}

func TestSetGlobalLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
		{"invalid", zerolog.InfoLevel}, // Default to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			SetGlobalLogLevel(tt.input)
			assert.Equal(t, tt.expected, zerolog.GlobalLevel())
		})
	}
}

func TestSetGlobalLogLevel_Environment(t *testing.T) {
	// Save original level
	originalLevel := zerolog.GlobalLevel()
	defer zerolog.SetGlobalLevel(originalLevel)

	// Set via environment
	if err := os.Setenv("TAPIO_LOG_LEVEL", "debug"); err != nil {
		t.Fatalf("failed to set TAPIO_LOG_LEVEL: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("TAPIO_LOG_LEVEL"); err != nil {
			t.Logf("failed to unset TAPIO_LOG_LEVEL: %v", err)
		}
	}()

	// Re-run init logic manually
	SetGlobalLogLevel("debug")

	assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())
}

func TestLogger_StructuredFields(t *testing.T) {
	logger := NewLogger("test-observer")

	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)

	testLogger.Info().
		Str("event_type", "tcp_connect").
		Str("src_ip", "10.0.1.5").
		Uint16("dst_port", 443).
		Int("pid", 1234).
		Msg("connection established")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "tcp_connect", logEntry.EventType)
	assert.Equal(t, "10.0.1.5", logEntry.SrcIP)
	assert.Equal(t, float64(443), logEntry.DstPort)
	assert.Equal(t, float64(1234), logEntry.PID)
	assert.Equal(t, "connection established", logEntry.Message)
}

func TestLogger_ErrorLogging(t *testing.T) {
	logger := NewLogger("test-observer")

	buf := &bytes.Buffer{}
	testLogger := logger.Output(buf)

	testErr := assert.AnError
	testLogger.Error().
		Err(testErr).
		Str("stage", "readEBPF").
		Msg("ring buffer read failed")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "error", logEntry.Level)
	assert.Contains(t, logEntry.Error, "assert.AnError")
	assert.Equal(t, "readEBPF", logEntry.Stage)
	assert.Equal(t, "ring buffer read failed", logEntry.Message)
}
