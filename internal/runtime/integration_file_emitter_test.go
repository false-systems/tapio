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

// TestIntegration_FileEmitter_EndToEnd validates the full pipeline:
// ObserverRuntime → Processor → FileEmitter
func TestIntegration_FileEmitter_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "integration.log")

	// Create mock processor
	processor := &mockProcessor{
		name: "integration-test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			// Just unmarshal and return
			var event domain.ObserverEvent
			if err := json.Unmarshal(rawEvent, &event); err != nil {
				return nil, err
			}
			return &event, nil
		},
	}

	// Create file emitter (critical so errors fail)
	fileEmitter := NewFileEmitter(filePath, true)
	defer func() {
		if err := fileEmitter.Close(); err != nil {
			t.Logf("fileEmitter close failed: %v", err)
		}
	}()

	// Create runtime with file emitter
	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(fileEmitter),
	)
	require.NoError(t, err)

	// Start runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("runtime.Run() failed: %v", err)
		}
	}()

	// Wait for runtime to be ready
	time.Sleep(100 * time.Millisecond)

	// Process events
	events := []*domain.ObserverEvent{
		{Type: "network", Subtype: "dns_query", Timestamp: time.Now()},
		{Type: "network", Subtype: "link_failure", Timestamp: time.Now()},
		{Type: "scheduler", Subtype: "pod_unschedulable", Timestamp: time.Now()},
	}

	for _, event := range events {
		rawEvent, err := json.Marshal(event)
		require.NoError(t, err)

		err = runtime.ProcessEvent(ctx, rawEvent)
		require.NoError(t, err)
	}

	// Stop runtime
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Verify file has all events
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := splitLines(string(data))
	assert.Len(t, lines, 3)

	for i, line := range lines {
		var readEvent domain.ObserverEvent
		err := json.Unmarshal([]byte(line), &readEvent)
		require.NoError(t, err)
		assert.Equal(t, events[i].Type, readEvent.Type)
		assert.Equal(t, events[i].Subtype, readEvent.Subtype)
	}
}

// TestIntegration_FileEmitter_MultipleEmitters validates that FileEmitter
// works alongside other emitters (fan-out pattern)
func TestIntegration_FileEmitter_MultipleEmitters(t *testing.T) {
	tmpDir := t.TempDir()
	file1Path := filepath.Join(tmpDir, "emitter1.log")
	file2Path := filepath.Join(tmpDir, "emitter2.log")

	processor := &mockProcessor{
		name: "multi-emitter-test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			var event domain.ObserverEvent
			if err := json.Unmarshal(rawEvent, &event); err != nil {
				return nil, err
			}
			return &event, nil
		},
	}

	// Create two file emitters
	emitter1 := NewFileEmitter(file1Path, false)
	defer func() {
		if err := emitter1.Close(); err != nil {
			t.Logf("emitter1 close failed: %v", err)
		}
	}()

	emitter2 := NewFileEmitter(file2Path, false)
	defer func() {
		if err := emitter2.Close(); err != nil {
			t.Logf("emitter2 close failed: %v", err)
		}
	}()

	// Create runtime with both emitters
	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(emitter1, emitter2),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("runtime.Run() failed: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Process event
	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "fan_out",
		Timestamp: time.Now(),
	}

	rawEvent, err := json.Marshal(event)
	require.NoError(t, err)

	err = runtime.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err)

	cancel()
	time.Sleep(100 * time.Millisecond)

	// Both files should have the event
	for _, path := range []string{file1Path, file2Path} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)

		lines := splitLines(string(data))
		require.Len(t, lines, 1)

		var readEvent domain.ObserverEvent
		err = json.Unmarshal([]byte(lines[0]), &readEvent)
		require.NoError(t, err)
		assert.Equal(t, event.Type, readEvent.Type)
		assert.Equal(t, event.Subtype, readEvent.Subtype)
	}
}

// TestIntegration_FileEmitter_CriticalFailure validates that critical
// file emitter failures stop event processing
func TestIntegration_FileEmitter_CriticalFailure(t *testing.T) {
	// Use invalid path to force failure
	invalidPath := "/nonexistent/directory/cannot/write/here.log"

	processor := &mockProcessor{
		name: "critical-failure-test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			var event domain.ObserverEvent
			if err := json.Unmarshal(rawEvent, &event); err != nil {
				return nil, err
			}
			return &event, nil
		},
	}

	// Critical emitter with invalid path
	emitter := NewFileEmitter(invalidPath, true)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(emitter),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("runtime.Run() failed: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Process event - should fail because emitter is critical
	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "should_fail",
		Timestamp: time.Now(),
	}

	rawEvent, err := json.Marshal(event)
	require.NoError(t, err)

	err = runtime.ProcessEvent(ctx, rawEvent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "critical emitter")
}

// TestIntegration_FileEmitter_HighThroughput validates FileEmitter
// can handle high event rates without data loss
func TestIntegration_FileEmitter_HighThroughput(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "throughput.log")

	processor := &mockProcessor{
		name: "throughput-test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			var event domain.ObserverEvent
			if err := json.Unmarshal(rawEvent, &event); err != nil {
				return nil, err
			}
			return &event, nil
		},
	}

	emitter := NewFileEmitter(filePath, true)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("emitter close failed: %v", err)
		}
	}()

	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(emitter),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("runtime.Run() failed: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Process 1000 events as fast as possible
	const numEvents = 1000

	for i := 0; i < numEvents; i++ {
		event := &domain.ObserverEvent{
			Type:      "test",
			Subtype:   "throughput",
			Timestamp: time.Now(),
		}

		rawEvent, err := json.Marshal(event)
		require.NoError(t, err)

		err = runtime.ProcessEvent(ctx, rawEvent)
		require.NoError(t, err)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	// Verify all events were written
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := splitLines(string(data))
	assert.Len(t, lines, numEvents, "should have all %d events", numEvents)
}
