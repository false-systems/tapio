package template

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollector_New(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid buffer size",
			config: Config{
				BufferSize:   0,
				Workers:      1,
				PollInterval: time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid workers",
			config: Config{
				BufferSize:   1000,
				Workers:      0,
				PollInterval: time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New("test", tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, c)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, c)
				assert.Equal(t, "test", c.Name())
			}
		})
	}
}

func TestCollector_StartStop(t *testing.T) {
	c, err := New("test", DefaultConfig())
	require.NoError(t, err)

	// Test Start
	ctx := context.Background()
	err = c.Start(ctx)
	assert.NoError(t, err)

	// Test double start
	err = c.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Test Stop
	err = c.Stop()
	assert.NoError(t, err)
}

func TestCollector_Events(t *testing.T) {
	config := DefaultConfig()
	config.PollInterval = 10 * time.Millisecond

	c, err := New("test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)

	// Collect events
	events := make([]interface{}, 0)
	done := make(chan bool)

	go func() {
		for event := range c.Events() {
			events = append(events, event)
			if len(events) >= 3 {
				done <- true
				return
			}
		}
	}()

	select {
	case <-done:
		// Got enough events
	case <-time.After(200 * time.Millisecond):
		// Timeout is OK, we just want some events
	}

	err = c.Stop()
	assert.NoError(t, err)

	// Verify we got events
	assert.GreaterOrEqual(t, len(events), 1)
}

func TestCollector_CreateEvent(t *testing.T) {
	c, err := New("test", DefaultConfig())
	require.NoError(t, err)

	// Test event creation
	event := c.createEvent("test_event", map[string]interface{}{
		"key": "value",
	})

	// Verify standard fields
	assert.Equal(t, "test", event.Type)
	assert.NotEmpty(t, event.TraceID)
	assert.NotEmpty(t, event.SpanID)
	assert.NotZero(t, event.Timestamp)

	// Verify metadata
	assert.Equal(t, "test", event.Metadata["collector_name"])
	assert.Equal(t, "test_event", event.Metadata["event_type"])
}

func TestCollector_Stats(t *testing.T) {
	config := DefaultConfig()
	config.PollInterval = 10 * time.Millisecond

	c, err := New("test", config)
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)

	// Let it generate some events
	time.Sleep(50 * time.Millisecond)

	// Get stats
	stats := c.GetStats()
	assert.Contains(t, stats, "events_generated")
	assert.Contains(t, stats, "events_dropped")
	assert.Contains(t, stats, "errors")
	assert.Contains(t, stats, "last_event_time")

	// Verify some events were generated
	generated := stats["events_generated"].(uint64)
	assert.Greater(t, generated, uint64(0))

	err = c.Stop()
	assert.NoError(t, err)
}

func TestCollector_IsHealthy(t *testing.T) {
	c, err := New("test", DefaultConfig())
	require.NoError(t, err)

	// Initially not healthy (no events)
	assert.False(t, c.IsHealthy())

	// Update stats to simulate recent event
	c.updateStats(true)

	// Now should be healthy
	assert.True(t, c.IsHealthy())

	// Simulate old event
	c.mu.Lock()
	c.stats.LastEventTime = time.Now().Add(-2 * time.Minute)
	c.mu.Unlock()

	// Should be unhealthy
	assert.False(t, c.IsHealthy())
}

func TestCollector_BufferFull(t *testing.T) {
	config := DefaultConfig()
	config.BufferSize = 1                      // Very small buffer
	config.PollInterval = 1 * time.Millisecond // Fast generation

	c, err := New("test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	// Let it run to fill buffer
	time.Sleep(20 * time.Millisecond)

	// Check stats
	stats := c.GetStats()
	dropped := stats["events_dropped"].(uint64)
	assert.Greater(t, dropped, uint64(0), "Should have dropped events")

	err = c.Stop()
	assert.NoError(t, err)
}
