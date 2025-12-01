package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestFileEmitter_EmptyPath verifies that empty path is rejected.
func TestFileEmitter_EmptyPath(t *testing.T) {
	emitter, err := NewFileEmitter("")

	assert.Error(t, err, "empty path should return error")
	assert.Nil(t, emitter, "emitter should be nil on error")
	assert.Contains(t, err.Error(), "path is required", "error should mention path requirement")
}

// TestFileEmitter_BasicEmit verifies basic emitter creation and emission.
func TestFileEmitter_BasicEmit(t *testing.T) {
	// Create temp file
	tmpDir, err := os.MkdirTemp("", "test-file-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.json")

	// Create File emitter (this will fail - doesn't exist yet)
	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err, "NewFileEmitter should succeed")
	require.NotNil(t, emitter, "emitter should not be nil")
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Create a basic domain event
	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "connection_established",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Emit the event
	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "Emit should succeed")

	// Verify file was created and contains event
	data, err := os.ReadFile(filePath)
	require.NoError(t, err, "should be able to read file")
	assert.Contains(t, string(data), "test-123", "file should contain event ID")
}

// TestFileEmitter_Interface verifies FileEmitter implements Emitter interface.
func TestFileEmitter_Interface(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-file-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.json")
	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Verify interface methods
	assert.Equal(t, "file", emitter.Name())
	assert.False(t, emitter.IsCritical(), "File emitter should be non-critical")
}

// TestFileEmitter_JSONLines verifies JSON Lines format (one JSON per line).
func TestFileEmitter_JSONLines(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-file-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.json")
	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Emit multiple events
	ctx := context.Background()
	events := []*domain.ObserverEvent{
		{
			ID:        "event-1",
			Type:      string(domain.EventTypeNetwork),
			Subtype:   "dns_query",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			ID:        "event-2",
			Type:      string(domain.EventTypeContainer),
			Subtype:   "oom_kill",
			Source:    "test",
			Timestamp: time.Now(),
		},
	}

	for _, evt := range events {
		err = emitter.Emit(ctx, evt)
		require.NoError(t, err)
	}

	// Read file and verify JSON Lines format
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, 2, "should have 2 lines")

	// Verify each line is valid JSON
	for i, line := range lines {
		var evt domain.ObserverEvent
		err := json.Unmarshal([]byte(line), &evt)
		require.NoError(t, err, "line %d should be valid JSON", i)
		assert.Equal(t, events[i].ID, evt.ID)
	}
}

// TestFileEmitter_CloseFlushes verifies Close() flushes buffered data.
func TestFileEmitter_CloseFlushes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-file-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.json")
	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)

	// Emit event
	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-flush",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test",
		Timestamp: time.Now(),
	}
	err = emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Close should flush buffer
	err = emitter.Close()
	require.NoError(t, err)

	// Verify file contains event
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "test-flush")

	// Multiple Close() calls should be safe
	err = emitter.Close()
	assert.NoError(t, err, "Multiple Close() calls should be safe")
}

// TestFileEmitter_ContextCancellation verifies respect for context cancellation.
func TestFileEmitter_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-file-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.json")
	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-cancel",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test",
		Timestamp: time.Now(),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Emit should fail fast due to cancelled context
	err = emitter.Emit(ctx, event)
	assert.Error(t, err, "Emit should fail with cancelled context")
}
