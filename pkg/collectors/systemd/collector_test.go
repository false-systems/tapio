package systemd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test-systemd", config)
	require.NoError(t, err)
	assert.NotNil(t, collector)
	assert.Equal(t, "test-systemd", collector.Name())
	assert.Equal(t, config.BufferSize, cap(collector.events))
}

func TestCollectorLifecycle(t *testing.T) {
	config := DefaultConfig()
	// Disable eBPF for testing (requires root)
	config.EnableEBPF = false
	// Disable journal for testing (requires systemd)
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Get events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Ensure channel is closed
	select {
	case _, ok := <-events:
		assert.False(t, ok, "events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Error("events channel was not closed")
	}
}

func TestEventTypeToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		eventType uint32
		expected  string
	}{
		{1, "exec"},
		{2, "exit"},
		{3, "kill"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.eventTypeToString(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNullTerminatedString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null terminated",
			input:    []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'},
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    []byte{'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "only null",
			input:    []byte{0},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.nullTerminatedString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
