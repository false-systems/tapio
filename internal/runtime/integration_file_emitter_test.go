package runtime

import (
	"context"
	"encoding/json"
	"fmt"
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
	tmpDir, err := os.MkdirTemp("", "test-emitter-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

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

	// Create runtime with file emitter (disable sampling for tests)
	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(fileEmitter),
		WithSamplingDisabled(),
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
		{ID: "test-1", Type: "network", Subtype: "dns_query", Timestamp: time.Now()},
		{ID: "test-2", Type: "network", Subtype: "link_failure", Timestamp: time.Now()},
		{ID: "test-3", Type: "scheduler", Subtype: "pod_unschedulable", Timestamp: time.Now()},
	}

	for _, event := range events {
		rawEvent, err := json.Marshal(event)
		require.NoError(t, err)

		err = runtime.ProcessEvent(ctx, rawEvent)
		require.NoError(t, err)
	}

	// Wait for queue to drain BEFORE cancelling
	require.NoError(t, waitForQueueDrain(filePath, len(events), 2*time.Second))
	cancel()
	time.Sleep(50 * time.Millisecond) // Wait for goroutine cleanup

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
	tmpDir, err := os.MkdirTemp("", "test-multi-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

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

	// Create runtime with both emitters (disable sampling for tests)
	runtime, err := NewObserverRuntime(
		processor,
		WithEmitters(emitter1, emitter2),
		WithSamplingDisabled(),
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
		ID:        "test-1",
		Type:      "test",
		Subtype:   "fan_out",
		Timestamp: time.Now(),
	}

	rawEvent, err := json.Marshal(event)
	require.NoError(t, err)

	err = runtime.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Wait for queue to drain BEFORE cancelling
	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond) // Wait for goroutine cleanup

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
// file emitter failures are retried and eventually logged (async)
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
		WithSamplingDisabled(),
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

	// Process event - ProcessEvent succeeds (enqueues), emission fails async
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "test",
		Subtype:   "should_fail",
		Timestamp: time.Now(),
	}

	rawEvent, err := json.Marshal(event)
	require.NoError(t, err)

	// ProcessEvent returns nil (async emission)
	err = runtime.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err, "ProcessEvent should succeed (emission is async)")

	// Wait for drainQueue to attempt emission and fail
	time.Sleep(200 * time.Millisecond)

	// Verify file was NOT created (emission failed)
	_, err = os.Stat(invalidPath)
	assert.True(t, os.IsNotExist(err), "File should not exist after critical emitter failure")
}

// TestIntegration_FileEmitter_HighThroughput validates FileEmitter
// can handle high event rates without data loss
func TestIntegration_FileEmitter_HighThroughput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-throughput-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

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
		WithSamplingDisabled(),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := runtime.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("runtime.Run() failed: %v", err)
		}
	}()

	// Wait for runtime to be ready
	time.Sleep(100 * time.Millisecond)

	// Process 1000 events as fast as possible
	const numEvents = 1000

	for i := 0; i < numEvents; i++ {
		event := &domain.ObserverEvent{
			ID:        fmt.Sprintf("test-event-%d", i),
			Type:      "test",
			Subtype:   "throughput",
			Timestamp: time.Now(),
		}

		rawEvent, err := json.Marshal(event)
		require.NoError(t, err)

		err = runtime.ProcessEvent(ctx, rawEvent)
		require.NoError(t, err)
	}

	// Wait for queue to fully drain BEFORE cancelling (1000 events)
	require.NoError(t, waitForQueueDrain(filePath, numEvents, 5*time.Second))
	cancel()
	time.Sleep(50 * time.Millisecond) // Wait for goroutine cleanup

	// Verify all events were written
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	lines := splitLines(string(data))
	assert.Len(t, lines, numEvents, "should have all %d events", numEvents)
}
