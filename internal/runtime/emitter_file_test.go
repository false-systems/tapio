package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test file emitter creation
func TestNewFileEmitter(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()

	assert.Equal(t, "file", emitter.Name())
	assert.False(t, emitter.IsCritical()) // File emitter is non-critical
}

// RED: Test file emitter with invalid path fails
func TestNewFileEmitter_InvalidPath(t *testing.T) {
	emitter, err := NewFileEmitter("")
	assert.Error(t, err)
	assert.Nil(t, emitter)
	assert.Contains(t, err.Error(), "path")
}

// RED: Test file emitter emits event as JSON
func TestFileEmitter_Emit(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer emitter.Close()

	event := &domain.ObserverEvent{
		Type:      "network",
		Subtype:   "dns_query",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol: "DNS",
			SrcIP:    "10.0.0.1",
			DstIP:    "8.8.8.8",
			SrcPort:  12345,
			DstPort:  53,
		},
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Verify event was written
	err = emitter.Close()
	require.NoError(t, err)

	// Read file and verify JSON
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	var readEvent domain.ObserverEvent
	err = json.Unmarshal(data, &readEvent)
	require.NoError(t, err)

	assert.Equal(t, event.Type, readEvent.Type)
	assert.Equal(t, event.Subtype, readEvent.Subtype)
}

// RED: Test file emitter respects context cancellation
func TestFileEmitter_Emit_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer emitter.Close()

	event := &domain.ObserverEvent{
		Type:    "test",
		Subtype: "event",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = emitter.Emit(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

// RED: Test file emitter handles nil event
func TestFileEmitter_Emit_NilEvent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer emitter.Close()

	ctx := context.Background()
	err = emitter.Emit(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// RED: Test file emitter close is idempotent
func TestFileEmitter_Close_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)

	// Close twice should not panic
	err = emitter.Close()
	assert.NoError(t, err)

	err = emitter.Close()
	assert.NoError(t, err) // Second close should also succeed
}

// RED: Test file emitter rejects emit after close
func TestFileEmitter_Emit_AfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)

	err = emitter.Close()
	require.NoError(t, err)

	event := &domain.ObserverEvent{
		Type:    "test",
		Subtype: "event",
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// RED: Test file emitter with custom options
func TestNewFileEmitter_WithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath,
		WithBufferSize(1024),
		WithFlushInterval(100*time.Millisecond),
	)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}

// RED: Test file emitter writes multiple events
func TestFileEmitter_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer emitter.Close()

	ctx := context.Background()

	// Emit 3 events
	for i := 0; i < 3; i++ {
		event := &domain.ObserverEvent{
			Type:    "test",
			Subtype: "event",
		}
		err = emitter.Emit(ctx, event)
		require.NoError(t, err)
	}

	// Close to flush
	err = emitter.Close()
	require.NoError(t, err)

	// Verify all 3 events written
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	// Count newlines (each event is one line)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	assert.Equal(t, 3, lines)
}

// RED: Test file emitter creates directory if needed
func TestFileEmitter_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir", "events.json")

	emitter, err := NewFileEmitter(filePath)
	require.NoError(t, err)
	defer emitter.Close()

	// Verify directory was created
	dir := filepath.Dir(filePath)
	_, err = os.Stat(dir)
	assert.NoError(t, err)
}
