package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// FileEmitter writes observer events to a file in JSON Lines format.
// This is a NON-CRITICAL emitter for debugging and testing.
type FileEmitter struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex
	closed bool
}

// NewFileEmitter creates a File emitter that writes to the given file path.
// path: File path to write events to (will be created if doesn't exist)
func NewFileEmitter(path string) (*FileEmitter, error) {
	// Create file (truncate if exists)
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	// Create buffered writer for performance
	writer := bufio.NewWriter(file)

	return &FileEmitter{
		file:   file,
		writer: writer,
		closed: false,
	}, nil
}

// Emit writes an observer event to the file as JSON.
// Format: JSON Lines (one event per line, newline-delimited)
func (e *FileEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("emitter is closed")
	}

	// Check context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write JSON + newline (JSON Lines format)
	if _, err := e.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}
	if _, err := e.writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush to ensure data is written to disk
	// (Important for debugging - we want events visible immediately)
	if err := e.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

	return nil
}

// Name returns the emitter name for logging and metrics.
func (e *FileEmitter) Name() string {
	return "file"
}

// IsCritical returns false - File emitter is non-critical (debug/testing only).
func (e *FileEmitter) IsCritical() bool {
	return false
}

// Close closes the file and flushes any buffered data.
func (e *FileEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil // Already closed
	}

	var err error

	// Flush buffered data
	if e.writer != nil {
		if flushErr := e.writer.Flush(); flushErr != nil {
			err = fmt.Errorf("failed to flush buffer: %w", flushErr)
		}
	}

	// Close file
	if e.file != nil {
		if closeErr := e.file.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("failed to close file: %w", closeErr)
			} else {
				err = fmt.Errorf("failed to close file: %w (previous error: %v)", closeErr, err)
			}
		}
	}

	e.closed = true
	return err
}
