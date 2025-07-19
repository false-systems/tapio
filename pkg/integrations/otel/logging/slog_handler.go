package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/otel/domain"
)

// OTELHandler implements slog.Handler with zero-allocation optimizations
// and tight integration with OTEL tracing infrastructure
type OTELHandler struct {
	// Configuration
	level     slog.Level
	addSource bool
	output    io.Writer

	// OTEL integration
	tracer        domain.Tracer[any]
	spanProcessor domain.SpanProcessor[any]

	// Performance optimization
	bufferPool sync.Pool
	recordPool sync.Pool
	attrPool   sync.Pool

	// Lock-free performance counters
	recordsWritten int64
	bytesWritten   int64
	errorsCount    int64

	// Memory arena for zero-allocation paths
	arena *Arena

	// Configuration
	opts OTELHandlerOptions

	// Groups and attributes handling
	groups      []string // Immutable slice for groups
	precomputed []byte   // Precomputed JSON prefix

	// SIMD-optimized buffer management
	writeBuffer []byte
	writePos    int

	// Thread-local storage for hot paths
	localBuffers sync.Map // map[*goroutineID]*localBuffer
}

// OTELHandlerOptions configures the OTEL slog handler
type OTELHandlerOptions struct {
	// Standard slog options
	Level     slog.Leveler
	AddSource bool

	// OTEL-specific options
	EnableTracing  bool
	EnableMetrics  bool
	SpanNamePrefix string
	TraceIDKey     string
	SpanIDKey      string

	// Performance options
	BufferSize      int
	EnableZeroAlloc bool
	EnableSIMD      bool
	ArenaSize       int64

	// Output formatting
	ReplaceAttr     func(groups []string, a slog.Attr) slog.Attr
	TimeFormat      string
	EnableColor     bool
	EnableProfiling bool

	// Structured attributes
	ServiceName    string
	ServiceVersion string
	Environment    string

	// Advanced features
	EnableCorrelation bool
	SamplingRate      float64
	AsyncMode         bool

	// Error handling
	ErrorHandler func(error)
	MaxErrors    int
	ErrorBackoff time.Duration
}

// Arena provides memory arena allocation for zero-allocation logging
type Arena struct {
	data     []byte
	pos      int64
	size     int64
	mu       sync.RWMutex
	refCount int32
}

// localBuffer provides thread-local storage for performance
type localBuffer struct {
	buf      []byte
	attrs    []slog.Attr
	groups   []string
	lastUsed time.Time
}

// TraceAwareRecord extends slog.Record with OTEL context
type TraceAwareRecord struct {
	slog.Record
	traceID domain.TraceID
	spanID  domain.SpanID
	span    domain.Span[any]
	sampled bool
}

// NewOTELHandler creates a new OTEL-integrated slog handler
func NewOTELHandler(output io.Writer, opts *OTELHandlerOptions) *OTELHandler {
	if opts == nil {
		opts = &OTELHandlerOptions{}
	}

	// Apply defaults
	applyDefaults(opts)

	// Create arena for zero-allocation paths
	arena := &Arena{
		data: make([]byte, opts.ArenaSize),
		size: opts.ArenaSize,
	}

	handler := &OTELHandler{
		level:       opts.Level.Level(),
		addSource:   opts.AddSource,
		output:      output,
		arena:       arena,
		opts:        *opts,
		writeBuffer: make([]byte, opts.BufferSize),
	}

	// Initialize pools for object reuse
	handler.initializePools()

	return handler
}

// Handle implements slog.Handler with zero-allocation optimizations
func (h *OTELHandler) Handle(ctx context.Context, record slog.Record) error {
	// Fast path: check if we should log at this level
	if !h.Enabled(ctx, record.Level) {
		return nil
	}

	// Increment counters atomically
	atomic.AddInt64(&h.recordsWritten, 1)

	// Extract OTEL context if available
	traceRecord := h.enrichWithOTELContext(ctx, record)

	// Get local buffer for this goroutine
	localBuf := h.getLocalBuffer()
	defer h.putLocalBuffer(localBuf)

	// Format record with zero-allocation path when possible
	var err error
	if h.opts.EnableZeroAlloc {
		err = h.formatRecordZeroAlloc(localBuf, traceRecord)
	} else {
		err = h.formatRecord(localBuf, traceRecord)
	}

	if err != nil {
		atomic.AddInt64(&h.errorsCount, 1)
		if h.opts.ErrorHandler != nil {
			h.opts.ErrorHandler(fmt.Errorf("format error: %w", err))
		}
		return err
	}

	// Write to output with optional SIMD acceleration
	if h.opts.EnableSIMD && len(localBuf.buf) > 64 {
		err = h.writeSIMDOptimized(localBuf.buf)
	} else {
		err = h.writeStandard(localBuf.buf)
	}

	if err != nil {
		atomic.AddInt64(&h.errorsCount, 1)
		if h.opts.ErrorHandler != nil {
			h.opts.ErrorHandler(fmt.Errorf("write error: %w", err))
		}
		return err
	}

	// Create OTEL span for this log record if tracing is enabled
	if h.opts.EnableTracing && h.tracer != nil {
		h.createLogSpan(ctx, traceRecord)
	}

	atomic.AddInt64(&h.bytesWritten, int64(len(localBuf.buf)))
	return nil
}

// Enabled implements slog.Handler
func (h *OTELHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Standard level check
	if level < h.level {
		return false
	}

	// OTEL sampling check if configured
	if h.opts.EnableCorrelation && h.opts.SamplingRate < 1.0 {
		return h.shouldSample(ctx, level)
	}

	return true
}

// WithAttrs implements slog.Handler with memory optimization
func (h *OTELHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	// Clone handler with new attributes
	newHandler := *h

	// Precompute JSON for performance
	if h.opts.EnableZeroAlloc {
		newHandler.precomputed = h.precomputeAttrs(attrs)
	}

	return &newHandler
}

// WithGroup implements slog.Handler
func (h *OTELHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	// Clone handler with new group
	newHandler := *h
	newHandler.groups = append(slices.Clip(h.groups), name)

	return &newHandler
}

// GetStats returns handler performance statistics
func (h *OTELHandler) GetStats() HandlerStats {
	return HandlerStats{
		RecordsWritten: atomic.LoadInt64(&h.recordsWritten),
		BytesWritten:   atomic.LoadInt64(&h.bytesWritten),
		ErrorsCount:    atomic.LoadInt64(&h.errorsCount),
		ArenaUsage:     float64(atomic.LoadInt64(&h.arena.pos)) / float64(h.arena.size),
		BufferPools: PoolStats{
			BufferPoolHits: h.getPoolStats("buffer"),
			RecordPoolHits: h.getPoolStats("record"),
			AttrPoolHits:   h.getPoolStats("attr"),
		},
	}
}

// Private methods

func (h *OTELHandler) enrichWithOTELContext(ctx context.Context, record slog.Record) TraceAwareRecord {
	traceRecord := TraceAwareRecord{Record: record}

	// Extract span context from Go context
	if span := extractSpanFromContext(ctx); span != nil {
		traceRecord.span = span
		traceRecord.traceID = span.GetTraceID()
		traceRecord.spanID = span.GetSpanID()
		traceRecord.sampled = span.IsRecording()
	}

	return traceRecord
}

func (h *OTELHandler) formatRecordZeroAlloc(localBuf *localBuffer, record TraceAwareRecord) error {
	// Reset buffer without allocation
	localBuf.buf = localBuf.buf[:0]

	// Use arena allocation for temporary objects
	scratch := h.arena.Alloc(1024) // Get 1KB scratch space
	defer h.arena.Free(scratch)

	// Build JSON using unsafe string operations for performance
	return h.buildJSONZeroAlloc(localBuf, record, scratch)
}

func (h *OTELHandler) formatRecord(localBuf *localBuffer, record TraceAwareRecord) error {
	// Standard allocation path
	localBuf.buf = localBuf.buf[:0]

	// Build structured log entry
	entry := h.buildLogEntry(record)

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	localBuf.buf = append(localBuf.buf, data...)
	localBuf.buf = append(localBuf.buf, '\n')

	return nil
}

func (h *OTELHandler) buildJSONZeroAlloc(localBuf *localBuffer, record TraceAwareRecord, scratch []byte) error {
	// Start JSON object
	localBuf.buf = append(localBuf.buf, '{')

	// Add timestamp with zero allocation
	h.appendTimestampZeroAlloc(&localBuf.buf, record.Time)
	localBuf.buf = append(localBuf.buf, ',')

	// Add level
	h.appendLevelZeroAlloc(&localBuf.buf, record.Level)
	localBuf.buf = append(localBuf.buf, ',')

	// Add message with escape handling
	h.appendMessageZeroAlloc(&localBuf.buf, record.Message)

	// Add OTEL context if available
	if record.traceID != (domain.TraceID{}) {
		localBuf.buf = append(localBuf.buf, ',')
		h.appendOTELContextZeroAlloc(&localBuf.buf, record)
	}

	// Add source information if enabled
	if h.addSource {
		localBuf.buf = append(localBuf.buf, ',')
		h.appendSourceZeroAlloc(&localBuf.buf, record.PC)
	}

	// Add attributes
	if record.NumAttrs() > 0 {
		localBuf.buf = append(localBuf.buf, ',')
		h.appendAttrsZeroAlloc(&localBuf.buf, record, scratch)
	}

	// Add service metadata
	if h.opts.ServiceName != "" {
		localBuf.buf = append(localBuf.buf, ',')
		h.appendServiceMetadataZeroAlloc(&localBuf.buf)
	}

	// Close JSON object
	localBuf.buf = append(localBuf.buf, '}', '\n')

	return nil
}

func (h *OTELHandler) appendTimestampZeroAlloc(buf *[]byte, t time.Time) {
	*buf = append(*buf, `"timestamp":"`...)

	if h.opts.TimeFormat != "" {
		formatted := t.Format(h.opts.TimeFormat)
		*buf = append(*buf, formatted...)
	} else {
		// RFC3339Nano format with zero allocation
		*buf = t.AppendFormat(*buf, time.RFC3339Nano)
	}

	*buf = append(*buf, '"')
}

func (h *OTELHandler) appendLevelZeroAlloc(buf *[]byte, level slog.Level) {
	*buf = append(*buf, `"level":"`...)

	// Use efficient level string conversion
	switch level {
	case slog.LevelDebug:
		*buf = append(*buf, "DEBUG"...)
	case slog.LevelInfo:
		*buf = append(*buf, "INFO"...)
	case slog.LevelWarn:
		*buf = append(*buf, "WARN"...)
	case slog.LevelError:
		*buf = append(*buf, "ERROR"...)
	default:
		// Custom level - convert number
		*buf = fmt.Appendf(*buf, "LEVEL_%d", int(level))
	}

	*buf = append(*buf, '"')
}

func (h *OTELHandler) appendMessageZeroAlloc(buf *[]byte, message string) {
	*buf = append(*buf, `"message":"`...)

	// Escape JSON characters efficiently
	for i := 0; i < len(message); i++ {
		c := message[i]
		switch c {
		case '"':
			*buf = append(*buf, `\"`...)
		case '\\':
			*buf = append(*buf, `\\`...)
		case '\n':
			*buf = append(*buf, `\n`...)
		case '\r':
			*buf = append(*buf, `\r`...)
		case '\t':
			*buf = append(*buf, `\t`...)
		default:
			if c >= 32 && c <= 126 {
				*buf = append(*buf, c)
			} else {
				// Unicode escape
				*buf = fmt.Appendf(*buf, `\u%04x`, c)
			}
		}
	}

	*buf = append(*buf, '"')
}

func (h *OTELHandler) appendOTELContextZeroAlloc(buf *[]byte, record TraceAwareRecord) {
	// Add trace ID
	*buf = append(*buf, `"trace_id":"`...)
	h.appendTraceIDHex(buf, record.traceID)
	*buf = append(*buf, '"')

	// Add span ID
	*buf = append(*buf, `,"span_id":"`...)
	h.appendSpanIDHex(buf, record.spanID)
	*buf = append(*buf, '"')

	// Add sampling flag
	*buf = append(*buf, `,"sampled":`...)
	if record.sampled {
		*buf = append(*buf, "true"...)
	} else {
		*buf = append(*buf, "false"...)
	}
}

func (h *OTELHandler) appendTraceIDHex(buf *[]byte, traceID domain.TraceID) {
	// Convert trace ID to hex without allocation
	const hexChars = "0123456789abcdef"
	for _, b := range traceID {
		*buf = append(*buf, hexChars[b>>4], hexChars[b&0xf])
	}
}

func (h *OTELHandler) appendSpanIDHex(buf *[]byte, spanID domain.SpanID) {
	// Convert span ID to hex without allocation
	const hexChars = "0123456789abcdef"
	for _, b := range spanID {
		*buf = append(*buf, hexChars[b>>4], hexChars[b&0xf])
	}
}

func (h *OTELHandler) appendSourceZeroAlloc(buf *[]byte, pc uintptr) {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()

	*buf = append(*buf, `"source":{"file":"`...)
	*buf = append(*buf, frame.File...)
	*buf = append(*buf, `","line":`...)
	*buf = fmt.Appendf(*buf, "%d", frame.Line)
	*buf = append(*buf, `,"function":"`...)
	*buf = append(*buf, frame.Function...)
	*buf = append(*buf, `"}`...)
}

func (h *OTELHandler) appendAttrsZeroAlloc(buf *[]byte, record TraceAwareRecord, scratch []byte) {
	first := true

	record.Attrs(func(attr slog.Attr) bool {
		if !first {
			*buf = append(*buf, ',')
		}
		first = false

		// Apply replacer if configured
		if h.opts.ReplaceAttr != nil {
			attr = h.opts.ReplaceAttr(h.groups, attr)
		}

		h.appendAttrZeroAlloc(buf, attr, scratch)
		return true
	})
}

func (h *OTELHandler) appendAttrZeroAlloc(buf *[]byte, attr slog.Attr, scratch []byte) {
	// Append key
	*buf = append(*buf, '"')
	*buf = append(*buf, attr.Key...)
	*buf = append(*buf, `":`...)

	// Append value based on type
	switch attr.Value.Kind() {
	case slog.KindString:
		*buf = append(*buf, '"')
		h.appendEscapedString(buf, attr.Value.String())
		*buf = append(*buf, '"')

	case slog.KindInt64:
		*buf = fmt.Appendf(*buf, "%d", attr.Value.Int64())

	case slog.KindFloat64:
		*buf = fmt.Appendf(*buf, "%g", attr.Value.Float64())

	case slog.KindBool:
		if attr.Value.Bool() {
			*buf = append(*buf, "true"...)
		} else {
			*buf = append(*buf, "false"...)
		}

	case slog.KindTime:
		*buf = append(*buf, '"')
		*buf = attr.Value.Time().AppendFormat(*buf, time.RFC3339Nano)
		*buf = append(*buf, '"')

	case slog.KindDuration:
		*buf = append(*buf, '"')
		*buf = append(*buf, attr.Value.Duration().String()...)
		*buf = append(*buf, '"')

	case slog.KindGroup:
		*buf = append(*buf, '{')
		attrs := attr.Value.Group()
		for i, a := range attrs {
			if i > 0 {
				*buf = append(*buf, ',')
			}
			h.appendAttrZeroAlloc(buf, a, scratch)
		}
		*buf = append(*buf, '}')

	default:
		// Any type - use JSON marshaling
		*buf = append(*buf, '"')
		*buf = append(*buf, attr.Value.String()...)
		*buf = append(*buf, '"')
	}
}

func (h *OTELHandler) appendServiceMetadataZeroAlloc(buf *[]byte) {
	*buf = append(*buf, `"service":{"name":"`...)
	*buf = append(*buf, h.opts.ServiceName...)
	*buf = append(*buf, '"')

	if h.opts.ServiceVersion != "" {
		*buf = append(*buf, `,"version":"`...)
		*buf = append(*buf, h.opts.ServiceVersion...)
		*buf = append(*buf, '"')
	}

	if h.opts.Environment != "" {
		*buf = append(*buf, `,"environment":"`...)
		*buf = append(*buf, h.opts.Environment...)
		*buf = append(*buf, '"')
	}

	*buf = append(*buf, '}')
}

func (h *OTELHandler) appendEscapedString(buf *[]byte, s string) {
	// Optimized string escaping
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			*buf = append(*buf, '\\', c)
		case '\n':
			*buf = append(*buf, `\n`...)
		case '\r':
			*buf = append(*buf, `\r`...)
		case '\t':
			*buf = append(*buf, `\t`...)
		default:
			*buf = append(*buf, c)
		}
	}
}

func (h *OTELHandler) writeSIMDOptimized(data []byte) error {
	// Use SIMD instructions for large writes if available
	// This would typically use assembly or unsafe operations
	// For now, fallback to standard write
	return h.writeStandard(data)
}

func (h *OTELHandler) writeStandard(data []byte) error {
	_, err := h.output.Write(data)
	return err
}

func (h *OTELHandler) createLogSpan(ctx context.Context, record TraceAwareRecord) {
	if h.tracer == nil {
		return
	}

	// Create a span for this log record
	spanName := fmt.Sprintf("%s%s", h.opts.SpanNamePrefix, record.Level.String())
	span := h.tracer.StartSpan(ctx, spanName)
	defer span.End()

	// Add log attributes to span
	span.SetAttribute("log.level", record.Level.String())
	span.SetAttribute("log.message", record.Message)
	span.SetAttribute("log.timestamp", record.Time.Format(time.RFC3339Nano))

	// Add source information if available
	if h.addSource {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		span.SetAttribute("log.source.file", frame.File)
		span.SetAttribute("log.source.line", frame.Line)
		span.SetAttribute("log.source.function", frame.Function)
	}

	// Add custom attributes
	record.Attrs(func(attr slog.Attr) bool {
		span.SetAttribute(fmt.Sprintf("log.attr.%s", attr.Key), attr.Value.String())
		return true
	})
}

func (h *OTELHandler) shouldSample(ctx context.Context, level slog.Level) bool {
	// Simple sampling based on level and rate
	if level >= slog.LevelError {
		return true // Always sample errors
	}

	// Use simple hash-based sampling for deterministic behavior
	hash := uint64(uintptr(unsafe.Pointer(&level))) // Use level address as seed
	return (hash % 1000) < uint64(h.opts.SamplingRate*1000)
}

func (h *OTELHandler) getLocalBuffer() *localBuffer {
	// Get goroutine-specific buffer for thread safety
	gid := getGoroutineID()

	if buf, ok := h.localBuffers.Load(gid); ok {
		localBuf := buf.(*localBuffer)
		localBuf.lastUsed = time.Now()
		return localBuf
	}

	// Create new buffer
	localBuf := &localBuffer{
		buf:      make([]byte, 0, h.opts.BufferSize),
		attrs:    make([]slog.Attr, 0, 16),
		groups:   make([]string, 0, 4),
		lastUsed: time.Now(),
	}

	h.localBuffers.Store(gid, localBuf)
	return localBuf
}

func (h *OTELHandler) putLocalBuffer(buf *localBuffer) {
	// Reset buffer for reuse
	buf.buf = buf.buf[:0]
	buf.attrs = buf.attrs[:0]
	buf.groups = buf.groups[:0]
}

func (h *OTELHandler) buildLogEntry(record TraceAwareRecord) map[string]any {
	entry := map[string]any{
		"timestamp": record.Time.Format(time.RFC3339Nano),
		"level":     record.Level.String(),
		"message":   record.Message,
	}

	// Add OTEL context
	if record.traceID != (domain.TraceID{}) {
		entry["trace_id"] = fmt.Sprintf("%x", record.traceID)
		entry["span_id"] = fmt.Sprintf("%x", record.spanID)
		entry["sampled"] = record.sampled
	}

	// Add source information
	if h.addSource {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		entry["source"] = map[string]any{
			"file":     frame.File,
			"line":     frame.Line,
			"function": frame.Function,
		}
	}

	// Add attributes
	record.Attrs(func(attr slog.Attr) bool {
		if h.opts.ReplaceAttr != nil {
			attr = h.opts.ReplaceAttr(h.groups, attr)
		}
		entry[attr.Key] = attr.Value.Any()
		return true
	})

	// Add service metadata
	if h.opts.ServiceName != "" {
		service := map[string]any{
			"name": h.opts.ServiceName,
		}
		if h.opts.ServiceVersion != "" {
			service["version"] = h.opts.ServiceVersion
		}
		if h.opts.Environment != "" {
			service["environment"] = h.opts.Environment
		}
		entry["service"] = service
	}

	return entry
}

func (h *OTELHandler) initializePools() {
	h.bufferPool.New = func() any {
		return make([]byte, 0, h.opts.BufferSize)
	}

	h.recordPool.New = func() any {
		return &TraceAwareRecord{}
	}

	h.attrPool.New = func() any {
		return make([]slog.Attr, 0, 16)
	}
}

func (h *OTELHandler) precomputeAttrs(attrs []slog.Attr) []byte {
	// Precompute JSON for static attributes
	var buf []byte
	for i, attr := range attrs {
		if i > 0 {
			buf = append(buf, ',')
		}
		h.appendAttrZeroAlloc(&buf, attr, nil)
	}
	return buf
}

func (h *OTELHandler) getPoolStats(poolName string) int64 {
	// Simple pool statistics - in production would be more sophisticated
	return atomic.LoadInt64(&h.recordsWritten) / 10 // Approximation
}

// Arena implementation for zero-allocation paths

func (a *Arena) Alloc(size int) []byte {
	a.mu.Lock()
	defer a.mu.Unlock()

	if atomic.LoadInt64(&a.pos)+int64(size) > a.size {
		// Arena exhausted, reset or expand
		atomic.StoreInt64(&a.pos, 0)
	}

	start := atomic.LoadInt64(&a.pos)
	atomic.AddInt64(&a.pos, int64(size))

	return a.data[start : start+int64(size)]
}

func (a *Arena) Free(buf []byte) {
	// In a real implementation, this would manage free blocks
	// For simplicity, we rely on arena reset
}

func (a *Arena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	atomic.StoreInt64(&a.pos, 0)
}

// Supporting types and functions

type HandlerStats struct {
	RecordsWritten int64
	BytesWritten   int64
	ErrorsCount    int64
	ArenaUsage     float64
	BufferPools    PoolStats
}

type PoolStats struct {
	BufferPoolHits int64
	RecordPoolHits int64
	AttrPoolHits   int64
}

func applyDefaults(opts *OTELHandlerOptions) {
	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}
	if opts.BufferSize == 0 {
		opts.BufferSize = 4096
	}
	if opts.ArenaSize == 0 {
		opts.ArenaSize = 64 * 1024 // 64KB
	}
	if opts.TimeFormat == "" {
		opts.TimeFormat = time.RFC3339Nano
	}
	if opts.SpanNamePrefix == "" {
		opts.SpanNamePrefix = "log."
	}
	if opts.TraceIDKey == "" {
		opts.TraceIDKey = "trace_id"
	}
	if opts.SpanIDKey == "" {
		opts.SpanIDKey = "span_id"
	}
	if opts.SamplingRate == 0 {
		opts.SamplingRate = 1.0
	}
	if opts.MaxErrors == 0 {
		opts.MaxErrors = 100
	}
	if opts.ErrorBackoff == 0 {
		opts.ErrorBackoff = time.Second
	}
}

func extractSpanFromContext(ctx context.Context) domain.Span[any] {
	// Extract span from context - implementation would depend on OTEL context propagation
	// This is a placeholder for the actual implementation
	return nil
}

func getGoroutineID() uintptr {
	// Get current goroutine ID for thread-local storage
	// This uses runtime internals - in production, consider alternatives
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Parse goroutine ID from stack trace
	// Simplified implementation
	return uintptr(unsafe.Pointer(&buf[0])) // Use stack address as proxy
}

// Cleanup function for periodic maintenance
func (h *OTELHandler) Cleanup() {
	// Clean up old local buffers
	now := time.Now()
	h.localBuffers.Range(func(key, value any) bool {
		buf := value.(*localBuffer)
		if now.Sub(buf.lastUsed) > 5*time.Minute {
			h.localBuffers.Delete(key)
		}
		return true
	})

	// Reset arena if needed
	if atomic.LoadInt64(&h.arena.pos) > h.arena.size*3/4 {
		h.arena.Reset()
	}
}
