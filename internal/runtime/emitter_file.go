package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// FileEmitter writes observer events to a file in JSON Lines format.
// Thread-safe for concurrent use.
//
// Format: Each event is written as a single JSON line:
//
//	{"type":"network","subtype":"dns_query","timestamp":"2025-01-05T10:30:00Z"}
//	{"type":"scheduler","subtype":"pod_unschedulable","timestamp":"2025-01-05T10:30:01Z"}
//
// Use cases:
//   - Debug: Write events to file for inspection
//   - Testing: Validate event emission in tests
//   - Backup: Secondary export alongside OTLP
type FileEmitter struct {
	filePath string
	critical bool
	mu       sync.Mutex
	file     *os.File
	writer   *bufio.Writer
}

// NewFileEmitter creates a new file emitter.
//
// Parameters:
//   - filePath: Path to output file (created if doesn't exist, appended if exists)
//   - critical: If true, emission failures fail the entire event processing
//
// The file is opened lazily on first Emit() call.
func NewFileEmitter(filePath string, critical bool) *FileEmitter {
	return &FileEmitter{
		filePath: filePath,
		critical: critical,
	}
}

// Name returns "file"
func (e *FileEmitter) Name() string {
	return "file"
}

// IsCritical returns true if this emitter is critical
func (e *FileEmitter) IsCritical() bool {
	return e.critical
}

// Emit writes an event to the file as a JSON line.
// Thread-safe: Can be called concurrently.
func (e *FileEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("cannot emit nil event")
	}

	// Check context cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Lazy initialization of file
	if e.file == nil {
		if err := e.openFile(); err != nil {
			return fmt.Errorf("failed to open file %s: %w", e.filePath, err)
		}
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write JSON + newline
	if _, err := e.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	if _, err := e.writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush immediately (ensures data is written for debugging)
	if err := e.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	return nil
}

// Close flushes any buffered data and closes the file.
// Should be called when shutting down the emitter.
func (e *FileEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.file == nil {
		return nil // Already closed or never opened
	}

	// Flush buffered data
	if e.writer != nil {
		if err := e.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush on close: %w", err)
		}
	}

	// Close file
	if err := e.file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	e.file = nil
	e.writer = nil

	return nil
}

// openFile opens the output file for appending.
// Must be called with e.mu held.
func (e *FileEmitter) openFile() error {
	// Create parent directories if they don't exist
	dir := filepath.Dir(e.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	file, err := os.OpenFile(e.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	e.file = file
	e.writer = bufio.NewWriter(file)

	return nil
}
