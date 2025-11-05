package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// FileEmitter writes events to a file as JSON (one event per line).
// Non-critical emitter for debug/testing purposes.
type FileEmitter struct {
	path          string
	bufferSize    int
	flushInterval time.Duration
	file          *os.File
	writer        *bufio.Writer
	mu            sync.Mutex
	closed        bool
}

// FileOption configures a file emitter
type FileOption func(*FileEmitter)

// WithBufferSize sets the buffer size for file writes
func WithBufferSize(size int) FileOption {
	return func(e *FileEmitter) {
		e.bufferSize = size
	}
}

// WithFlushInterval sets the flush interval for buffered writes
func WithFlushInterval(interval time.Duration) FileOption {
	return func(e *FileEmitter) {
		e.flushInterval = interval
	}
}

// NewFileEmitter creates a new file emitter
func NewFileEmitter(path string, opts ...FileOption) (*FileEmitter, error) {
	if path == "" {
		return nil, fmt.Errorf("file path is required")
	}

	emitter := &FileEmitter{
		path:          path,
		bufferSize:    4096,
		flushInterval: 1 * time.Second,
	}

	// Apply options
	for _, opt := range opts {
		opt(emitter)
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for append
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	emitter.file = file
	emitter.writer = bufio.NewWriterSize(file, emitter.bufferSize)

	return emitter, nil
}

// Emit writes an event to the file as JSON
func (e *FileEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("cannot emit nil event")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("emitter is closed")
	}
	e.mu.Unlock()

	// Check context
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	// Marshal event as JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write JSON line
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, err := e.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	if _, err := e.writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush immediately for now (REFACTOR phase will add buffering)
	if err := e.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	return nil
}

// Name returns "file"
func (e *FileEmitter) Name() string {
	return "file"
}

// IsCritical returns false (file emitter is non-critical)
func (e *FileEmitter) IsCritical() bool {
	return false
}

// Close closes the file and flushes any buffered events
func (e *FileEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil // Idempotent
	}

	e.closed = true

	// Flush buffer
	if err := e.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Close file
	if err := e.file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	return nil
}
