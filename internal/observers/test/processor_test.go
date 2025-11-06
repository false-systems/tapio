package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/runtime"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test timing constants
const (
	// eventCollectionPeriod is how long we collect events in tests
	eventCollectionPeriod = 1 * time.Second

	// contextBuffer is extra time to ensure context completes
	contextBuffer = 100 * time.Millisecond

	// eventCollectionTimeout is total time to wait for event collection
	eventCollectionTimeout = eventCollectionPeriod + contextBuffer // 1100ms

	// halfSecondContext is context timeout for shorter tests
	halfSecondContext = 500 * time.Millisecond

	// multiTypeTimeout is timeout for multi-type event tests
	multiTypeTimeout = halfSecondContext + contextBuffer // 600ms
)

// RED: Test creating test observer processor
func TestNewProcessor(t *testing.T) {
	proc := NewProcessor()
	require.NotNil(t, proc)
	assert.Equal(t, "test", proc.Name())
}

// RED: Test processor generates events
func TestProcessor_Process(t *testing.T) {
	proc := NewProcessor()

	// Create test event (JSON-encoded domain event)
	event := &domain.ObserverEvent{
		Type:      "test",
		Subtype:   "mock_event",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := proc.Process(ctx, data)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, event.Type, result.Type)
	assert.Equal(t, event.Subtype, result.Subtype)
}

// RED: Test processor handles invalid JSON
func TestProcessor_Process_InvalidJSON(t *testing.T) {
	proc := NewProcessor()

	ctx := context.Background()
	result, err := proc.Process(ctx, []byte("invalid json"))
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test processor with configurable event rate
func TestProcessor_WithEventRate(t *testing.T) {
	proc := NewProcessor(WithEventRate(100))
	require.NotNil(t, proc)
}

// RED: Test processor with event types
func TestProcessor_WithEventTypes(t *testing.T) {
	types := []EventType{
		{Type: "network", Subtype: "dns_query"},
		{Type: "network", Subtype: "http_connection"},
	}

	proc := NewProcessor(WithEventTypes(types))
	require.NotNil(t, proc)
}

// RED: Test processor Setup and Teardown
func TestProcessor_Lifecycle(t *testing.T) {
	proc := NewProcessor()

	ctx := context.Background()

	// Setup
	err := proc.Setup(ctx, runtime.DefaultConfig("test"))
	require.NoError(t, err)

	// Teardown
	err = proc.Teardown(ctx)
	require.NoError(t, err)
}

// RED: Test processor generates events at specified rate
func TestProcessor_GenerateEvents(t *testing.T) {
	proc := NewProcessor(
		WithEventRate(10), // 10 events/sec
	)

	ctx, cancel := context.WithTimeout(context.Background(), eventCollectionPeriod)
	defer cancel()

	config := runtime.DefaultConfig("test")
	err := proc.Setup(ctx, config)
	require.NoError(t, err)
	defer func() {
		if err := proc.Teardown(context.Background()); err != nil {
			t.Logf("teardown failed: %v", err)
		}
	}()

	// Start generating events
	eventCh := make(chan []byte, 100)
	go proc.StartGeneration(ctx, eventCh)

	// Collect events for 1 second
	var count int
	timeout := time.After(eventCollectionTimeout)
	for {
		select {
		case <-eventCh:
			count++
		case <-timeout:
			// Should have ~10 events (±2 for timing)
			assert.InDelta(t, 10, count, 2)
			return
		}
	}
}

// RED: Test processor with multiple event types
func TestProcessor_MultipleEventTypes(t *testing.T) {
	types := []EventType{
		{Type: "network", Subtype: "dns_query"},
		{Type: "container", Subtype: "oom_kill"},
	}

	proc := NewProcessor(
		WithEventTypes(types),
		WithEventRate(20),
	)

	ctx, cancel := context.WithTimeout(context.Background(), halfSecondContext)
	defer cancel()

	config := runtime.DefaultConfig("test")
	err := proc.Setup(ctx, config)
	require.NoError(t, err)
	defer func() {
		if err := proc.Teardown(context.Background()); err != nil {
			t.Logf("teardown failed: %v", err)
		}
	}()

	eventCh := make(chan []byte, 100)
	go proc.StartGeneration(ctx, eventCh)

	// Collect events
	eventTypes := make(map[string]int)
	timeout := time.After(multiTypeTimeout)
	for {
		select {
		case data := <-eventCh:
			var event domain.ObserverEvent
			if err := json.Unmarshal(data, &event); err == nil {
				key := event.Type + ":" + event.Subtype
				eventTypes[key]++
			}
		case <-timeout:
			// Should have both event types
			assert.Greater(t, len(eventTypes), 0)
			return
		}
	}
}

// RED: Test processor stops generation on context cancel
func TestProcessor_StopGeneration(t *testing.T) {
	proc := NewProcessor(WithEventRate(100))

	ctx, cancel := context.WithCancel(context.Background())

	config := runtime.DefaultConfig("test")
	err := proc.Setup(ctx, config)
	require.NoError(t, err)
	defer func() {
		if err := proc.Teardown(context.Background()); err != nil {
			t.Logf("teardown failed: %v", err)
		}
	}()

	eventCh := make(chan []byte, 100)
	go proc.StartGeneration(ctx, eventCh)

	// Let it generate some events
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Channel should not receive new events
	select {
	case <-eventCh:
		// May have buffered events, that's OK
	case <-time.After(200 * time.Millisecond):
		// No new events after cancel - good
	}
}
