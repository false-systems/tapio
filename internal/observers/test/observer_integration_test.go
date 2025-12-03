package test

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
	"github.com/yairfalse/tapio/internal/runtime"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestObserverRuntime_Integration verifies the complete workflow:
// TestObserver → ObserverRuntime → FileEmitter
func TestObserverRuntime_Integration(t *testing.T) {
	// Create temp directory for file emitter
	tmpDir, err := os.MkdirTemp("", "integration-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	filePath := filepath.Join(tmpDir, "events.jsonl")

	// Create TestObserver
	observer, err := NewTestObserver("integration-test")
	require.NoError(t, err)

	// Create FileEmitter
	emitter, err := runtime.NewFileEmitter(filePath)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Create ObserverRuntime with TestObserver and FileEmitter
	// Disable sampling to ensure test event is not dropped
	rt, err := runtime.NewObserverRuntime(
		observer,
		runtime.WithEmitters(emitter),
		runtime.WithSamplingDisabled(),
	)
	require.NoError(t, err)
	require.NotNil(t, rt)

	// Start runtime in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- rt.Run(ctx)
	}()

	// Wait briefly for runtime to start
	time.Sleep(100 * time.Millisecond)

	// Create test event
	testEvent := &domain.ObserverEvent{
		ID:        "test-event-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "test_connection",
		Source:    "integration-test",
		Timestamp: time.Now(),
	}

	// Marshal to raw bytes (simulating what TestObserver would receive)
	rawEvent, err := json.Marshal(testEvent)
	require.NoError(t, err)

	// Process event through runtime
	err = rt.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Wait for event to be drained and emitted
	// Drain runs every 10ms, add some buffer for safety
	time.Sleep(100 * time.Millisecond)

	// Stop runtime
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("runtime error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("runtime did not stop in time")
	}

	// Verify event was written to file
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Greater(t, len(lines), 0, "should have at least one event")

	// Parse and verify event
	var emittedEvent domain.ObserverEvent
	err = json.Unmarshal([]byte(lines[0]), &emittedEvent)
	require.NoError(t, err)

	assert.Equal(t, testEvent.ID, emittedEvent.ID)
	assert.Equal(t, testEvent.Type, emittedEvent.Type)
	assert.Equal(t, testEvent.Subtype, emittedEvent.Subtype)
	assert.Equal(t, testEvent.Source, emittedEvent.Source)
}
