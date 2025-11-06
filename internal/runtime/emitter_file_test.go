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

func TestFileEmitter_Name(t *testing.T) {
	emitter := NewFileEmitter("/tmp/test.log", false)
	assert.Equal(t, "file", emitter.Name())
}

func TestFileEmitter_IsCritical(t *testing.T) {
	t.Run("non-critical by default", func(t *testing.T) {
		emitter := NewFileEmitter("/tmp/test.log", false)
		assert.False(t, emitter.IsCritical())
	})

	t.Run("can be critical", func(t *testing.T) {
		emitter := NewFileEmitter("/tmp/test.log", true)
		assert.True(t, emitter.IsCritical())
	})
}

func TestFileEmitter_Emit_SingleEvent(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	// Create test event
	event := &domain.ObserverEvent{
		Type:      "network",
		Subtype:   "connection_refused",
		Timestamp: time.Now(),
	}

	// Emit event
	ctx := context.Background()
	err := emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Verify file exists and contains event
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	// Should be JSON + newline
	var readEvent domain.ObserverEvent
	err = json.Unmarshal(data, &readEvent)
	require.NoError(t, err)

	assert.Equal(t, event.Type, readEvent.Type)
	assert.Equal(t, event.Subtype, readEvent.Subtype)
}

func TestFileEmitter_Emit_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	ctx := context.Background()

	// Emit 3 events
	events := []*domain.ObserverEvent{
		{Type: "network", Subtype: "dns_query", Timestamp: time.Now()},
		{Type: "network", Subtype: "link_failure", Timestamp: time.Now()},
		{Type: "scheduler", Subtype: "pod_unschedulable", Timestamp: time.Now()},
	}

	for _, event := range events {
		err := emitter.Emit(ctx, event)
		require.NoError(t, err)
	}

	// Read file - should have 3 lines
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := splitLines(string(data))
	assert.Len(t, lines, 3)

	// Each line should be valid JSON
	for i, line := range lines {
		var readEvent domain.ObserverEvent
		err := json.Unmarshal([]byte(line), &readEvent)
		require.NoError(t, err, "line %d should be valid JSON", i)
		assert.Equal(t, events[i].Type, readEvent.Type)
		assert.Equal(t, events[i].Subtype, readEvent.Subtype)
	}
}

func TestFileEmitter_Emit_NilEvent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	ctx := context.Background()
	err := emitter.Emit(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil event")
}

func TestFileEmitter_Close_FlushesBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)

	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "close_test",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Close should flush
	err = emitter.Close()
	require.NoError(t, err)

	// Verify file has content
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestFileEmitter_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "cancelled",
		Timestamp: time.Now(),
	}

	// Should return context error
	err := emitter.Emit(ctx, event)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFileEmitter_InvalidPath(t *testing.T) {
	// Try to write to invalid path (directory that doesn't exist)
	emitter := NewFileEmitter("/nonexistent/directory/events.log", false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "invalid_path",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)
	assert.Error(t, err)
}

func TestFileEmitter_ConcurrentEmits(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "events.log")

	emitter := NewFileEmitter(filePath, false)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	ctx := context.Background()
	const numGoroutines = 10
	const eventsPerGoroutine = 100

	// Emit from multiple goroutines concurrently
	done := make(chan bool, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &domain.ObserverEvent{
					Type:      "test",
					Subtype:   "concurrent",
					Timestamp: time.Now(),
				}
				err := emitter.Emit(ctx, event)
				if err != nil {
					t.Errorf("goroutine %d emit %d failed: %v", id, j, err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Close to flush
	err := emitter.Close()
	require.NoError(t, err)

	// Verify we have all events
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := splitLines(string(data))
	expectedEvents := numGoroutines * eventsPerGoroutine
	assert.Equal(t, expectedEvents, len(lines))
}

// Helper to split lines (handles both \n and \r\n)
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	lines := []string{}
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			if current != "" {
				lines = append(lines, current)
			}
			current = ""
		} else if ch != '\r' {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
