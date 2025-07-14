package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Logger provides structured logging with production features
type Logger struct {
	*slog.Logger
	config *Config
	attrs  []slog.Attr
	mutex  sync.RWMutex
}

// Config defines logger configuration
type Config struct {
	// General settings
	Level      slog.Level `yaml:"level"`
	Format     string     `yaml:"format"` // json, logfmt, console
	Output     string     `yaml:"output"` // stdout, stderr, file
	Filename   string     `yaml:"filename"`
	MaxSize    int        `yaml:"max_size"`    // MB
	MaxBackups int        `yaml:"max_backups"`
	MaxAge     int        `yaml:"max_age"`     // days
	
	// Performance settings
	Async         bool          `yaml:"async"`
	BufferSize    int           `yaml:"buffer_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	
	// Production features
	Sampling       bool           `yaml:"sampling"`
	SampleRate     float64        `yaml:"sample_rate"`
	SampleBurst    int            `yaml:"sample_burst"`
	RedactSecrets  bool           `yaml:"redact_secrets"`
	IncludeSource  bool           `yaml:"include_source"`
	IncludeCaller  bool           `yaml:"include_caller"`
	
	// Context settings
	TraceIDField   string   `yaml:"trace_id_field"`
	SpanIDField    string   `yaml:"span_id_field"`
	ServiceName    string   `yaml:"service_name"`
	Environment    string   `yaml:"environment"`
	DefaultFields  map[string]interface{} `yaml:"default_fields"`
}

// NewLogger creates a new production-ready logger
func NewLogger(cfg *Config) *Logger {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Create handler based on configuration
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.IncludeSource,
	}

	// Select output writer
	var output io.Writer
	switch cfg.Output {
	case "stderr":
		output = os.Stderr
	case "file":
		// In production, this would use a file rotation library
		file, err := os.OpenFile(cfg.Filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			output = os.Stdout
		} else {
			output = file
		}
	default:
		output = os.Stdout
	}

	// Create handler based on format
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(output, opts)
	case "logfmt":
		handler = NewLogfmtHandler(output, opts)
	case "console":
		handler = NewConsoleHandler(output, opts)
	default:
		handler = slog.NewJSONHandler(output, opts)
	}

	// Wrap with production handlers
	if cfg.RedactSecrets {
		handler = NewRedactingHandler(handler)
	}

	if cfg.Sampling && cfg.SampleRate < 1.0 {
		handler = NewSamplingHandler(handler, cfg.SampleRate, cfg.SampleBurst)
	}

	if cfg.Async {
		handler = NewAsyncHandler(handler, cfg.BufferSize)
	}

	// Create logger
	logger := slog.New(handler)

	// Add default attributes
	attrs := []slog.Attr{
		slog.String("service", cfg.ServiceName),
		slog.String("environment", cfg.Environment),
	}
	
	for k, v := range cfg.DefaultFields {
		attrs = append(attrs, slog.Any(k, v))
	}

	logger = logger.With(attrs...)

	return &Logger{
		Logger: logger,
		config: cfg,
		attrs:  attrs,
	}
}

// DefaultConfig returns default logger configuration
func DefaultConfig() *Config {
	return &Config{
		Level:          slog.LevelInfo,
		Format:         "json",
		Output:         "stdout",
		Async:          true,
		BufferSize:     1024,
		FlushInterval:  time.Second,
		Sampling:       false,
		SampleRate:     1.0,
		RedactSecrets:  true,
		IncludeSource:  false,
		IncludeCaller:  true,
		TraceIDField:   "trace_id",
		SpanIDField:    "span_id",
		ServiceName:    "tapio",
		Environment:    "production",
		DefaultFields:  make(map[string]interface{}),
	}
}

// WithComponent creates a child logger with component context
func (l *Logger) WithComponent(component string) *Logger {
	return l.With(slog.String("component", component))
}

// WithContext creates a child logger with context values
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := []slog.Attr{}

	// Extract trace context
	if traceID := ctx.Value(l.config.TraceIDField); traceID != nil {
		attrs = append(attrs, slog.String(l.config.TraceIDField, fmt.Sprint(traceID)))
	}
	if spanID := ctx.Value(l.config.SpanIDField); spanID != nil {
		attrs = append(attrs, slog.String(l.config.SpanIDField, fmt.Sprint(spanID)))
	}

	// Extract user context
	if userID := ctx.Value("user_id"); userID != nil {
		attrs = append(attrs, slog.String("user_id", fmt.Sprint(userID)))
	}

	return l.With(attrs...)
}

// With creates a child logger with additional attributes
func (l *Logger) With(args ...interface{}) *Logger {
	newLogger := &Logger{
		Logger: l.Logger.With(args...),
		config: l.config,
		attrs:  append(l.attrs, argsToAttrs(args...)...),
	}
	return newLogger
}

// WithFields creates a child logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	attrs := make([]slog.Attr, 0, len(fields))
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	return l.With(attrs...)
}

// Error logs an error with additional context
func (l *Logger) Error(msg string, args ...interface{}) {
	// Include caller information for errors
	if l.config.IncludeCaller {
		_, file, line, ok := runtime.Caller(1)
		if ok {
			args = append(args, slog.String("caller", fmt.Sprintf("%s:%d", file, line)))
		}
	}
	l.Logger.Error(msg, args...)
}

// Audit logs security audit events
func (l *Logger) Audit(event string, user string, args ...interface{}) {
	auditArgs := []interface{}{
		slog.String("audit_event", event),
		slog.String("user", user),
		slog.Time("timestamp", time.Now()),
	}
	auditArgs = append(auditArgs, args...)
	l.Info("AUDIT", auditArgs...)
}

// Performance logs performance metrics
func (l *Logger) Performance(operation string, duration time.Duration, args ...interface{}) {
	perfArgs := []interface{}{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
	}
	perfArgs = append(perfArgs, args...)
	l.Info("PERFORMANCE", perfArgs...)
}

// Security logs security events
func (l *Logger) Security(event string, severity string, args ...interface{}) {
	secArgs := []interface{}{
		slog.String("security_event", event),
		slog.String("severity", severity),
		slog.Time("timestamp", time.Now()),
	}
	secArgs = append(secArgs, args...)
	
	switch strings.ToLower(severity) {
	case "critical", "high":
		l.Error("SECURITY", secArgs...)
	case "medium":
		l.Warn("SECURITY", secArgs...)
	default:
		l.Info("SECURITY", secArgs...)
	}
}

// Metric logs application metrics
func (l *Logger) Metric(name string, value interface{}, args ...interface{}) {
	metricArgs := []interface{}{
		slog.String("metric_name", name),
		slog.Any("metric_value", value),
		slog.Time("timestamp", time.Now()),
	}
	metricArgs = append(metricArgs, args...)
	l.Info("METRIC", metricArgs...)
}

// argsToAttrs converts variadic arguments to slog attributes
func argsToAttrs(args ...interface{}) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args)/2)
	for i := 0; i < len(args)-1; i += 2 {
		if key, ok := args[i].(string); ok {
			attrs = append(attrs, slog.Any(key, args[i+1]))
		}
	}
	return attrs
}

// Production logger presets

var (
	// Development provides a development-friendly logger
	Development = NewLogger(&Config{
		Level:         slog.LevelDebug,
		Format:        "console",
		Output:        "stdout",
		Async:         false,
		RedactSecrets: false,
		IncludeCaller: true,
		Environment:   "development",
	})

	// Production provides a production-optimized logger
	Production = NewLogger(&Config{
		Level:          slog.LevelInfo,
		Format:         "json",
		Output:         "stdout",
		Async:          true,
		BufferSize:     4096,
		Sampling:       true,
		SampleRate:     0.1,
		SampleBurst:    100,
		RedactSecrets:  true,
		IncludeCaller:  false,
		Environment:    "production",
	})

	// HighPerformance provides maximum performance logging
	HighPerformance = NewLogger(&Config{
		Level:          slog.LevelWarn,
		Format:         "json",
		Output:         "stdout",
		Async:          true,
		BufferSize:     8192,
		Sampling:       true,
		SampleRate:     0.01,
		SampleBurst:    10,
		RedactSecrets:  true,
		IncludeCaller:  false,
		IncludeSource:  false,
		Environment:    "production",
	})

	// Compliance provides compliance-focused logging
	Compliance = NewLogger(&Config{
		Level:          slog.LevelInfo,
		Format:         "json",
		Output:         "file",
		Filename:       "/var/log/tapio/audit.log",
		MaxSize:        100,
		MaxBackups:     10,
		MaxAge:         90,
		Async:          false,
		RedactSecrets:  true,
		IncludeCaller:  true,
		IncludeSource:  true,
		Environment:    "production",
	})
)