package logging

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger implements domain.Logger using uber/zap
type ZapLogger struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
	fields map[string]interface{}
}

// NewZapLogger creates a new zap logger
func NewZapLogger(config *domain.LoggingConfig) (*ZapLogger, error) {
	// Create encoder config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Choose encoder based on format
	var encoder zapcore.Encoder
	switch config.Format {
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	case "text", "structured":
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	default:
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Create output writer
	var writer zapcore.WriteSyncer
	switch config.Output {
	case "stdout":
		writer = zapcore.AddSync(os.Stdout)
	case "stderr":
		writer = zapcore.AddSync(os.Stderr)
	default:
		// File output
		file, err := os.OpenFile(config.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}

		// Handle rotation if enabled
		if config.Rotation {
			writer = zapcore.AddSync(&rotatingFileWriter{
				file:     file,
				maxSize:  config.MaxSize * 1024 * 1024, // Convert MB to bytes
				maxAge:   config.MaxAge,
				compress: config.Compress,
			})
		} else {
			writer = zapcore.AddSync(file)
		}
	}

	// Parse log level
	level := zapcore.InfoLevel
	switch config.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// Create core
	core := zapcore.NewCore(encoder, writer, level)

	// Create logger with options
	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.ErrorOutput(writer),
	)

	return &ZapLogger{
		logger: logger,
		sugar:  logger.Sugar(),
		fields: make(map[string]interface{}),
	}, nil
}

// Debug logs a debug message
func (l *ZapLogger) Debug(ctx context.Context, message string, fields ...interface{}) {
	l.logWithContext(ctx, zapcore.DebugLevel, message, fields...)
}

// Info logs an info message
func (l *ZapLogger) Info(ctx context.Context, message string, fields ...interface{}) {
	l.logWithContext(ctx, zapcore.InfoLevel, message, fields...)
}

// Warn logs a warning message
func (l *ZapLogger) Warn(ctx context.Context, message string, fields ...interface{}) {
	l.logWithContext(ctx, zapcore.WarnLevel, message, fields...)
}

// Error logs an error message
func (l *ZapLogger) Error(ctx context.Context, message string, fields ...interface{}) {
	l.logWithContext(ctx, zapcore.ErrorLevel, message, fields...)
}

// WithFields returns a logger with additional fields
func (l *ZapLogger) WithFields(fields map[string]interface{}) domain.Logger {
	newFields := make(map[string]interface{})

	// Copy existing fields
	for k, v := range l.fields {
		newFields[k] = v
	}

	// Add new fields
	for k, v := range fields {
		newFields[k] = v
	}

	// Create zap fields
	zapFields := make([]zap.Field, 0, len(newFields))
	for k, v := range newFields {
		zapFields = append(zapFields, zap.Any(k, v))
	}

	return &ZapLogger{
		logger: l.logger.With(zapFields...),
		sugar:  l.logger.With(zapFields...).Sugar(),
		fields: newFields,
	}
}

// WithError returns a logger with an error field
func (l *ZapLogger) WithError(err error) domain.Logger {
	return l.WithFields(map[string]interface{}{
		"error": err.Error(),
	})
}

// WithRequest returns a logger with request fields
func (l *ZapLogger) WithRequest(request *domain.Request) domain.Logger {
	if request == nil {
		return l
	}

	return l.WithFields(map[string]interface{}{
		"request_id":   request.ID,
		"request_type": string(request.Type),
		"source":       request.Source,
	})
}

// WithResponse returns a logger with response fields
func (l *ZapLogger) WithResponse(response *domain.Response) domain.Logger {
	if response == nil {
		return l
	}

	fields := map[string]interface{}{
		"response_id":     response.ID,
		"request_id":      response.RequestID,
		"response_type":   string(response.Type),
		"response_status": string(response.Status),
	}

	if response.Error != nil {
		fields["response_error"] = response.Error.Error()
	}

	return l.WithFields(fields)
}

// logWithContext logs a message with context
func (l *ZapLogger) logWithContext(ctx context.Context, level zapcore.Level, message string, fields ...interface{}) {
	// Extract context values
	contextFields := extractContextFields(ctx)

	// Convert variadic fields to map
	fieldMap := make(map[string]interface{})
	for i := 0; i < len(fields)-1; i += 2 {
		if key, ok := fields[i].(string); ok && i+1 < len(fields) {
			fieldMap[key] = fields[i+1]
		}
	}

	// Merge all fields
	allFields := make([]zap.Field, 0, len(contextFields)+len(fieldMap))

	// Add context fields
	for k, v := range contextFields {
		allFields = append(allFields, zap.Any(k, v))
	}

	// Add provided fields
	for k, v := range fieldMap {
		allFields = append(allFields, zap.Any(k, v))
	}

	// Log based on level
	switch level {
	case zapcore.DebugLevel:
		l.logger.Debug(message, allFields...)
	case zapcore.InfoLevel:
		l.logger.Info(message, allFields...)
	case zapcore.WarnLevel:
		l.logger.Warn(message, allFields...)
	case zapcore.ErrorLevel:
		l.logger.Error(message, allFields...)
	default:
		l.logger.Info(message, allFields...)
	}
}

// extractContextFields extracts relevant fields from context
func extractContextFields(ctx context.Context) map[string]interface{} {
	fields := make(map[string]interface{})

	// Extract common context values
	if requestID, ok := ctx.Value("request_id").(string); ok {
		fields["request_id"] = requestID
	}

	if traceID, ok := ctx.Value("trace_id").(string); ok {
		fields["trace_id"] = traceID
	}

	if userID, ok := ctx.Value("user_id").(string); ok {
		fields["user_id"] = userID
	}

	if correlationID, ok := ctx.Value("correlation_id").(string); ok {
		fields["correlation_id"] = correlationID
	}

	return fields
}

// Sync flushes any buffered log entries
func (l *ZapLogger) Sync() error {
	return l.logger.Sync()
}

// rotatingFileWriter implements log rotation
type rotatingFileWriter struct {
	file     *os.File
	maxSize  int
	maxAge   int
	compress bool
	size     int64
}

func (w *rotatingFileWriter) Write(p []byte) (n int, err error) {
	// Check if rotation is needed
	if w.size+int64(len(p)) > int64(w.maxSize) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFileWriter) Sync() error {
	return w.file.Sync()
}

func (w *rotatingFileWriter) rotate() error {
	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Rename current file with timestamp
	oldPath := w.file.Name()
	newPath := fmt.Sprintf("%s.%s", oldPath, time.Now().Format("20060102-150405"))

	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}

	// Compress if enabled
	if w.compress {
		// In production, would compress the file asynchronously
		// For now, just rename with .gz extension as placeholder
		os.Rename(newPath, newPath+".gz")
	}

	// Create new file
	file, err := os.OpenFile(oldPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	w.file = file
	w.size = 0

	// Clean up old files based on maxAge
	// In production, this would be done asynchronously

	return nil
}

// NewDevelopmentLogger creates a development logger with pretty printing
func NewDevelopmentLogger() *ZapLogger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	logger, _ := config.Build()

	return &ZapLogger{
		logger: logger,
		sugar:  logger.Sugar(),
		fields: make(map[string]interface{}),
	}
}

// NewProductionLogger creates a production logger with JSON output
func NewProductionLogger() *ZapLogger {
	config := zap.NewProductionConfig()
	config.Sampling = &zap.SamplingConfig{
		Initial:    100,
		Thereafter: 100,
	}

	logger, _ := config.Build()

	return &ZapLogger{
		logger: logger,
		sugar:  logger.Sugar(),
		fields: make(map[string]interface{}),
	}
}
