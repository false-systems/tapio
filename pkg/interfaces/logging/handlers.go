package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ConsoleHandler provides human-readable console output
type ConsoleHandler struct {
	opts   slog.HandlerOptions
	writer io.Writer
	mutex  sync.Mutex
}

// NewConsoleHandler creates a new console handler
func NewConsoleHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &ConsoleHandler{
		opts:   *opts,
		writer: w,
	}
}

func (h *ConsoleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *ConsoleHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Format timestamp
	timestamp := r.Time.Format("2006-01-02 15:04:05.000")

	// Format level with color
	level := h.formatLevel(r.Level)

	// Build message
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", timestamp, level, r.Message)

	// Add attributes
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})

	// Write to output
	fmt.Fprintln(h.writer, b.String())

	return nil
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *ConsoleHandler) formatLevel(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "[DEBUG]"
	case slog.LevelInfo:
		return "[INFO] "
	case slog.LevelWarn:
		return "[WARN] "
	case slog.LevelError:
		return "[ERROR]"
	default:
		return "[UNKNOWN]"
	}
}

// LogfmtHandler provides logfmt format output
type LogfmtHandler struct {
	handler slog.Handler
}

// NewLogfmtHandler creates a new logfmt handler
func NewLogfmtHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	// For simplicity, wrap JSON handler and convert
	// In production, this would use a proper logfmt library
	return &LogfmtHandler{
		handler: slog.NewJSONHandler(w, opts),
	}
}

func (h *LogfmtHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *LogfmtHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *LogfmtHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogfmtHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *LogfmtHandler) WithGroup(name string) slog.Handler {
	return &LogfmtHandler{handler: h.handler.WithGroup(name)}
}

// RedactingHandler redacts sensitive information
type RedactingHandler struct {
	handler slog.Handler
}

// NewRedactingHandler creates a new redacting handler
func NewRedactingHandler(handler slog.Handler) slog.Handler {
	return &RedactingHandler{handler: handler}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Create new record with redacted attributes
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	r.Attrs(func(a slog.Attr) bool {
		if h.shouldRedact(a.Key) {
			newRecord.AddAttrs(slog.String(a.Key, "[REDACTED]"))
		} else {
			newRecord.AddAttrs(a)
		}
		return true
	})

	return h.handler.Handle(ctx, newRecord)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RedactingHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{handler: h.handler.WithGroup(name)}
}

func (h *RedactingHandler) shouldRedact(key string) bool {
	sensitiveKeys := []string{
		"password", "token", "secret", "key", "credential",
		"auth", "jwt", "api_key", "private", "ssn", "credit_card",
	}

	lowerKey := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lowerKey, sensitive) {
			return true
		}
	}

	return false
}

// SamplingHandler provides log sampling for high-volume scenarios
type SamplingHandler struct {
	handler slog.Handler
	sampler *LogSampler
}

// LogSampler implements token bucket sampling
type LogSampler struct {
	rate   float64
	burst  int
	tokens float64
	last   time.Time
	mutex  sync.Mutex
}

// NewSamplingHandler creates a new sampling handler
func NewSamplingHandler(handler slog.Handler, rate float64, burst int) slog.Handler {
	return &SamplingHandler{
		handler: handler,
		sampler: &LogSampler{
			rate:   rate,
			burst:  burst,
			tokens: float64(burst),
			last:   time.Now(),
		},
	}
}

func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Always log errors and above
	if level >= slog.LevelError {
		return h.handler.Enabled(ctx, level)
	}

	// Sample lower levels
	return h.sampler.Allow() && h.handler.Enabled(ctx, level)
}

func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SamplingHandler{
		handler: h.handler.WithAttrs(attrs),
		sampler: h.sampler,
	}
}

func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	return &SamplingHandler{
		handler: h.handler.WithGroup(name),
		sampler: h.sampler,
	}
}

func (s *LogSampler) Allow() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.last).Seconds()
	s.last = now

	// Add tokens based on elapsed time
	s.tokens += elapsed * s.rate
	if s.tokens > float64(s.burst) {
		s.tokens = float64(s.burst)
	}

	// Check if we have tokens
	if s.tokens >= 1.0 {
		s.tokens--
		return true
	}

	return false
}

// AsyncHandler provides asynchronous logging
type AsyncHandler struct {
	handler slog.Handler
	buffer  chan slog.Record
	wg      sync.WaitGroup
	done    chan struct{}
}

// NewAsyncHandler creates a new async handler
func NewAsyncHandler(handler slog.Handler, bufferSize int) slog.Handler {
	h := &AsyncHandler{
		handler: handler,
		buffer:  make(chan slog.Record, bufferSize),
		done:    make(chan struct{}),
	}

	h.wg.Add(1)
	go h.process()

	return h
}

func (h *AsyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *AsyncHandler) Handle(ctx context.Context, r slog.Record) error {
	select {
	case h.buffer <- r:
		return nil
	default:
		// Buffer full, drop or handle synchronously
		return h.handler.Handle(ctx, r)
	}
}

func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &AsyncHandler{
		handler: h.handler.WithAttrs(attrs),
		buffer:  h.buffer,
		done:    h.done,
	}
}

func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	return &AsyncHandler{
		handler: h.handler.WithGroup(name),
		buffer:  h.buffer,
		done:    h.done,
	}
}

func (h *AsyncHandler) process() {
	defer h.wg.Done()

	for {
		select {
		case record := <-h.buffer:
			ctx := context.Background()
			h.handler.Handle(ctx, record)
		case <-h.done:
			// Flush remaining records
			for len(h.buffer) > 0 {
				record := <-h.buffer
				ctx := context.Background()
				h.handler.Handle(ctx, record)
			}
			return
		}
	}
}

func (h *AsyncHandler) Close() {
	close(h.done)
	h.wg.Wait()
}

// ProductionHandler wraps handlers with production features
type ProductionHandler struct {
	handler slog.Handler
	config  *Config
	sampler *LogSampler
	metrics *LogMetrics
}

// LogMetrics tracks logging metrics
type LogMetrics struct {
	totalLogs   int64
	droppedLogs int64
	errorLogs   int64
	mutex       sync.Mutex
}

// NewProductionHandler creates a production-ready handler
func NewProductionHandler(handler slog.Handler, config *Config) slog.Handler {
	ph := &ProductionHandler{
		handler: handler,
		config:  config,
		metrics: &LogMetrics{},
	}

	if config.Sampling {
		ph.sampler = &LogSampler{
			rate:   config.SampleRate,
			burst:  config.SampleBurst,
			tokens: float64(config.SampleBurst),
			last:   time.Now(),
		}
	}

	return ph
}

func (h *ProductionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Always log errors
	if level >= slog.LevelError {
		return h.handler.Enabled(ctx, level)
	}

	// Apply sampling to lower levels
	if h.sampler != nil && !h.sampler.Allow() {
		h.metrics.mutex.Lock()
		h.metrics.droppedLogs++
		h.metrics.mutex.Unlock()
		return false
	}

	return h.handler.Enabled(ctx, level)
}

func (h *ProductionHandler) Handle(ctx context.Context, r slog.Record) error {
	h.metrics.mutex.Lock()
	h.metrics.totalLogs++
	if r.Level >= slog.LevelError {
		h.metrics.errorLogs++
	}
	h.metrics.mutex.Unlock()

	return h.handler.Handle(ctx, r)
}

func (h *ProductionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ProductionHandler{
		handler: h.handler.WithAttrs(attrs),
		config:  h.config,
		sampler: h.sampler,
		metrics: h.metrics,
	}
}

func (h *ProductionHandler) WithGroup(name string) slog.Handler {
	return &ProductionHandler{
		handler: h.handler.WithGroup(name),
		config:  h.config,
		sampler: h.sampler,
		metrics: h.metrics,
	}
}

func (h *ProductionHandler) GetMetrics() (total, dropped, errors int64) {
	h.metrics.mutex.Lock()
	defer h.metrics.mutex.Unlock()
	return h.metrics.totalLogs, h.metrics.droppedLogs, h.metrics.errorLogs
}
